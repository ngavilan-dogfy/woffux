package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/ngavilan-dogfy/woffux/internal/agent"
)

var Version = "dev"

var (
	releasesAPI      = "https://api.github.com/repos/ngavilan-dogfy/woffux/releases/latest"
	releasesWeb      = "https://github.com/ngavilan-dogfy/woffux/releases/latest"
	updateHTTPClient = &http.Client{Timeout: 20 * time.Second}
)

type githubRelease struct {
	TagName string               `json:"tag_name"`
	Assets  []githubReleaseAsset `json:"assets"`
}

type githubReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

var updateYes bool

var updateCmd = &cobra.Command{
	Use:     "update",
	Aliases: []string{"upgrade"},
	Short:   "Update woffux to the latest version",
	RunE: func(cmd *cobra.Command, args []string) error {
		u := newUpdater(updateYes || !isTTY())
		p := tea.NewProgram(u)
		u.program = p
		if _, err := p.Run(); err != nil {
			return err
		}
		if u.phase != updReady {
			if u.tmpPath != "" {
				os.Remove(u.tmpPath)
			}
			return nil
		}
		return installUpdate(u.tmpPath, u.release.TagName)
	},
}

// installUpdate swaps the verified binary into place (asking for sudo only
// when the install directory needs it) and restarts the local agent so it
// runs the new version.
func installUpdate(tmpPath, tag string) error {
	target := updateInstallPath()
	if err := exec.Command("mv", tmpPath, target).Run(); err != nil {
		fmt.Printf("  %s %s needs admin rights — macOS will ask for your password.\n", sInfo, target)
		sudo := exec.Command("sudo", "mv", tmpPath, target)
		sudo.Stdin, sudo.Stdout, sudo.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := sudo.Run(); err != nil {
			uiErr("Install failed. Try: sudo mv %q %q", tmpPath, target)
			return nil
		}
	}
	uiOK("woffux %s installed", tag)
	// Restart the local agent only if it runs this very binary, and let
	// the new binary do it so the agent points at the installed path.
	if agent.Supported() && agent.Installed() && samePath(agent.InstalledBinary(), target) {
		if err := exec.Command(target, "agent", "on").Run(); err == nil {
			uiOK("This Mac's signer restarted on the new version")
		}
	}
	fmt.Println(uiIndent + stFaint.Render("  Open the dashboard with: woffux"))
	fmt.Println()
	return nil
}

func currentVersionIsLatest(current, latest string) bool {
	current = normalizeVersion(current)
	latest = normalizeVersion(latest)
	return current != "" && latest != "" && current == latest
}

func normalizeVersion(version string) string {
	version = strings.TrimSpace(version)
	version = strings.TrimPrefix(version, "v")
	version = strings.TrimPrefix(version, "V")
	return version
}

// fetchLatestTag queries the GitHub API directly (no gh CLI needed).
func fetchLatestTag() (string, error) {
	release, err := fetchLatestReleaseWithFallback()
	if err != nil {
		return "", err
	}
	return release.TagName, nil
}

// fetchLatestReleaseWithFallback asks the API first and, when it fails
// (anonymous calls are limited to 60/hour per IP, so shared office or VPN
// IPs hit 403 often), reads the tag from the public releases page instead.
func fetchLatestReleaseWithFallback() (githubRelease, error) {
	release, apiErr := fetchLatestRelease(releasesAPI)
	if apiErr == nil {
		return release, nil
	}
	release, webErr := fetchLatestReleaseFromWeb(releasesWeb)
	if webErr != nil {
		return githubRelease{}, fmt.Errorf("%v; fallback: %v", apiErr, webErr)
	}
	return release, nil
}

// fetchLatestReleaseFromWeb resolves the latest tag from the redirect of
// github.com/<repo>/releases/latest and builds the standard asset URLs.
func fetchLatestReleaseFromWeb(webURL string) (githubRelease, error) {
	client := *updateHTTPClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Get(webURL)
	if err != nil {
		return githubRelease{}, fmt.Errorf("cannot reach GitHub: %w", err)
	}
	defer resp.Body.Close()

	loc := resp.Header.Get("Location")
	idx := strings.LastIndex(loc, "/tag/")
	if resp.StatusCode < 300 || resp.StatusCode >= 400 || idx == -1 {
		return githubRelease{}, fmt.Errorf("releases page returned %d", resp.StatusCode)
	}
	tag := strings.TrimSpace(loc[idx+len("/tag/"):])
	if tag == "" {
		return githubRelease{}, fmt.Errorf("no releases found")
	}
	base := strings.TrimSuffix(loc[:idx], "/releases") + "/releases/download/" + tag + "/"
	release := githubRelease{TagName: tag}
	for _, name := range []string{"woffux-darwin-arm64", "woffux-darwin-amd64", "woffux-linux-amd64", "woffux-linux-arm64"} {
		release.Assets = append(release.Assets, githubReleaseAsset{Name: name, BrowserDownloadURL: base + name})
	}
	return release, nil
}

// githubToken returns a token for authenticated API calls, if one is set.
func githubToken() string {
	for _, k := range []string{"GITHUB_TOKEN", "GH_TOKEN"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

// fetchLatestRelease queries the GitHub API directly (no gh CLI needed).
func fetchLatestRelease(apiURL string) (githubRelease, error) {
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return githubRelease{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if tok := githubToken(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}

	resp, err := updateHTTPClient.Do(req)
	if err != nil {
		return githubRelease{}, fmt.Errorf("cannot reach GitHub: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return githubRelease{}, fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return githubRelease{}, fmt.Errorf("parse response: %w", err)
	}

	release.TagName = strings.TrimSpace(release.TagName)
	if release.TagName == "" {
		return githubRelease{}, fmt.Errorf("no releases found")
	}
	return release, nil
}

func (r githubRelease) DownloadURL(assetName string) (string, error) {
	for _, asset := range r.Assets {
		if asset.Name == assetName && strings.TrimSpace(asset.BrowserDownloadURL) != "" {
			return strings.TrimSpace(asset.BrowserDownloadURL), nil
		}
	}
	return "", fmt.Errorf("release %s does not include %s yet", r.TagName, assetName)
}

// downloadFile downloads a URL to a local path using net/http.
func downloadFile(dst, url string) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("create download request: %w", err)
	}
	req.Header.Set("Accept", "application/octet-stream")

	resp, err := updateHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("download returned HTTP %d", resp.StatusCode)
	}

	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer out.Close()

	n, err := io.Copy(out, resp.Body)
	if err != nil {
		return fmt.Errorf("write file: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("downloaded file is empty")
	}

	return nil
}

func releaseAssetName(goos, goarch string) (string, error) {
	switch goos {
	case "darwin", "linux":
	default:
		return "", fmt.Errorf("unsupported OS for auto-update: %s", goos)
	}

	switch goarch {
	case "arm64":
	case "amd64":
	default:
		return "", fmt.Errorf("unsupported architecture for auto-update: %s", goarch)
	}

	return fmt.Sprintf("woffux-%s-%s", goos, goarch), nil
}

func updateInstallPath() string {
	currentPath, err := os.Executable()
	if err == nil && !shouldAvoidSelfReplace(currentPath) {
		return currentPath
	}

	if path, err := exec.LookPath("woffux"); err == nil && !shouldAvoidSelfReplace(path) {
		return path
	}

	return "/usr/local/bin/woffux"
}

func shouldAvoidSelfReplace(path string) bool {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "." || filepath.Base(path) != "woffux" {
		return true
	}

	if strings.Contains(path, string(filepath.Separator)+"go-build"+string(filepath.Separator)) {
		return true
	}

	tmpDir := os.TempDir()
	if tmpDir == "" {
		return false
	}
	rel, err := filepath.Rel(tmpDir, path)
	return err == nil && rel != "." && !strings.HasPrefix(rel, "..")
}

func init() {
	updateCmd.Flags().BoolVarP(&updateYes, "yes", "y", false, "Update without asking")
}

func samePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	if err1 != nil || err2 != nil {
		return a == b
	}
	return ra == rb
}
