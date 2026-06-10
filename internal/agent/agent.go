// Package agent manages the local launchd auto-sign agent. GitHub Actions
// cron schedules fire hours late or not at all, so a local agent is the
// primary signer; the GitHub workflow stays as fallback.
package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const Label = "dev.woffux.agent"

// Minutes is offset from the GitHub watchdog (7,22,37,52) so local and
// remote runs don't race each other.
var Minutes = []int{1, 16, 31, 46}

// Supported reports whether the local agent works on this OS.
func Supported() bool {
	return runtime.GOOS == "darwin"
}

func PlistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", Label+".plist"), nil
}

func LogPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Logs", "woffux-agent.log")
}

// MinutesLabel renders the run minutes as ":01 :16 :31 :46".
func MinutesLabel() string {
	parts := make([]string, 0, len(Minutes))
	for _, m := range Minutes {
		parts = append(parts, fmt.Sprintf(":%02d", m))
	}
	return strings.Join(parts, " ")
}

func plistContent(binPath string) string {
	var intervals strings.Builder
	for _, m := range Minutes {
		fmt.Fprintf(&intervals, `    <dict>
      <key>Minute</key>
      <integer>%d</integer>
    </dict>
`, m)
	}

	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>sign</string>
    <string>--scheduled</string>
  </array>
  <key>StartCalendarInterval</key>
  <array>
%s  </array>
  <key>RunAtLoad</key>
  <false/>
  <key>StandardOutPath</key>
  <string>%s</string>
  <key>StandardErrorPath</key>
  <string>%s</string>
</dict>
</plist>
`, Label, binPath, intervals.String(), LogPath(), LogPath())
}

// Installed reports whether the agent plist exists.
func Installed() bool {
	path, err := PlistPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

// Loaded reports whether launchd currently has the agent bootstrapped.
func Loaded() bool {
	if !Supported() {
		return false
	}
	err := exec.Command("launchctl", "print", fmt.Sprintf("gui/%d/%s", os.Getuid(), Label)).Run()
	return err == nil
}

// Install writes the plist pointing at the current woffux binary and
// (re)bootstraps it into launchd.
func Install() error {
	if !Supported() {
		return fmt.Errorf("the local agent uses launchd and only works on macOS")
	}

	binPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve woffux binary path: %w", err)
	}
	binPath, err = filepath.EvalSymlinks(binPath)
	if err != nil {
		return fmt.Errorf("resolve woffux binary path: %w", err)
	}

	plistPath, err := PlistPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(plistPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(plistPath, []byte(plistContent(binPath)), 0o644); err != nil {
		return fmt.Errorf("write plist: %w", err)
	}

	// Reload: bootout is allowed to fail when the agent wasn't loaded.
	domain := fmt.Sprintf("gui/%d", os.Getuid())
	_ = exec.Command("launchctl", "bootout", domain+"/"+Label).Run()
	if out, err := exec.Command("launchctl", "bootstrap", domain, plistPath).CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl bootstrap: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// Uninstall stops the agent and removes its plist.
func Uninstall() error {
	if !Supported() {
		return fmt.Errorf("the local agent uses launchd and only works on macOS")
	}

	plistPath, err := PlistPath()
	if err != nil {
		return err
	}
	domain := fmt.Sprintf("gui/%d", os.Getuid())
	_ = exec.Command("launchctl", "bootout", domain+"/"+Label).Run()
	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// RecentLog returns the last n lines of the agent log.
func RecentLog(n int) []string {
	data, err := os.ReadFile(LogPath())
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}
