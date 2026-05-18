package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/huh/spinner"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var Version = "dev"

var (
	releasesAPI      = "https://api.github.com/repos/ngavilan-dogfy/woffux/releases/latest"
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

var updateCmd = &cobra.Command{
	Use:     "update",
	Aliases: []string{"upgrade"},
	Short:   "Update woffux to the latest version",
	RunE: func(cmd *cobra.Command, args []string) error {
		sLabel := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))

		fmt.Printf("\n  %s %s\n", sLabel.Render("Current version:"), sBold.Render(Version))

		assetName, err := releaseAssetName(runtime.GOOS, runtime.GOARCH)
		if err != nil {
			return err
		}

		var release githubRelease
		var checkErr error

		spinner.New().
			Title("Checking for updates...").
			Action(func() {
				release, checkErr = fetchLatestRelease(releasesAPI)
			}).
			Run()

		if checkErr != nil {
			fmt.Printf("  %s Could not check for updates: %s\n\n", sWarn, checkErr)
			return nil
		}

		latestTag := release.TagName
		fmt.Printf("  %s %s\n\n", sLabel.Render("Latest version: "), sBold.Render(latestTag))

		if currentVersionIsLatest(Version, latestTag) {
			fmt.Printf("  %s You're on the latest version.\n\n", sOk)
			return nil
		}

		var confirm bool
		if err := huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title(fmt.Sprintf("Update to %s?", latestTag)).
					Affirmative("Update").
					Negative("Cancel").
					Value(&confirm),
			),
		).Run(); err != nil {
			return err
		}

		if !confirm {
			return nil
		}

		url, err := release.DownloadURL(assetName)
		if err != nil {
			fmt.Printf("  %s %s\n\n", sWarn, err)
			fmt.Printf("  Download manually: https://github.com/ngavilan-dogfy/woffux/releases/tag/%s\n\n", latestTag)
			return nil
		}

		tmp, err := os.CreateTemp("", "woffux-update-*")
		if err != nil {
			return fmt.Errorf("create temp file: %w", err)
		}
		tmpPath := tmp.Name()
		if err := tmp.Close(); err != nil {
			os.Remove(tmpPath)
			return fmt.Errorf("close temp file: %w", err)
		}
		keepTemp := false
		defer func() {
			if !keepTemp {
				os.Remove(tmpPath)
			}
		}()

		var downloadErr error
		spinner.New().
			Title(fmt.Sprintf("Downloading %s...", latestTag)).
			Action(func() {
				downloadErr = downloadFile(tmpPath, url)
			}).
			Run()

		if downloadErr != nil {
			fmt.Printf("  %s %s\n\n",
				lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("✗"), downloadErr)
			return nil
		}

		if err := os.Chmod(tmpPath, 0755); err != nil {
			return fmt.Errorf("make binary executable: %w", err)
		}

		// Install — needs to happen outside spinner so sudo can prompt
		currentPath := updateInstallPath()

		// Try without sudo first
		mv := exec.Command("mv", tmpPath, currentPath)
		if err := mv.Run(); err != nil {
			// Needs sudo — tell the user and run with TTY
			fmt.Printf("  %s Installing to %s (requires sudo)\n", sInfo, currentPath)
			sudoMv := exec.Command("sudo", "mv", tmpPath, currentPath)
			sudoMv.Stdin = os.Stdin
			sudoMv.Stdout = os.Stdout
			sudoMv.Stderr = os.Stderr
			if err := sudoMv.Run(); err != nil {
				keepTemp = true
				fmt.Printf("\n  %s Install failed. Try manually:\n",
					lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("✗"))
				fmt.Printf("    sudo mv %q %q\n\n", tmpPath, currentPath)
				return nil
			}
		}

		fmt.Printf("\n  %s Updated to %s\n\n", sOk, sBold.Render(latestTag))
		return nil
	},
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
	release, err := fetchLatestRelease(releasesAPI)
	if err != nil {
		return "", err
	}
	return release.TagName, nil
}

// fetchLatestRelease queries the GitHub API directly (no gh CLI needed).
func fetchLatestRelease(apiURL string) (githubRelease, error) {
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return githubRelease{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

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
