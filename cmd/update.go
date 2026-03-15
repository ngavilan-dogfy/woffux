package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/huh/spinner"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var Version = "dev"

const releasesAPI = "https://api.github.com/repos/ngavilan-dogfy/woffux/releases/latest"

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update woffux to the latest version",
	RunE: func(cmd *cobra.Command, args []string) error {
		sLabel := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))

		fmt.Printf("\n  %s %s\n", sLabel.Render("Current version:"), sBold.Render(Version))

		// Check latest version
		var latestTag string
		var checkErr error

		spinner.New().
			Title("Checking for updates...").
			Action(func() {
				latestTag, checkErr = fetchLatestTag()
			}).
			Run()

		if checkErr != nil {
			fmt.Printf("  %s Could not check for updates: %s\n\n", sWarn, checkErr)
			return nil
		}

		fmt.Printf("  %s %s\n\n", sLabel.Render("Latest version: "), sBold.Render(latestTag))

		if latestTag == Version {
			fmt.Printf("  %s You're on the latest version.\n\n", sOk)
			return nil
		}

		var confirm bool
		huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title(fmt.Sprintf("Update to %s?", latestTag)).
					Affirmative("Update").
					Negative("Cancel").
					Value(&confirm),
			),
		).Run()

		if !confirm {
			return nil
		}

		// Download
		binary := fmt.Sprintf("woffux-%s-%s", runtime.GOOS, goArch())
		url := fmt.Sprintf("https://github.com/ngavilan-dogfy/woffux/releases/download/%s/%s", latestTag, binary)

		var downloadErr error
		spinner.New().
			Title(fmt.Sprintf("Downloading %s...", latestTag)).
			Action(func() {
				downloadErr = downloadFile("/tmp/woffux-update", url)
			}).
			Run()

		if downloadErr != nil {
			fmt.Printf("  %s %s\n\n",
				lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("✗"), downloadErr)
			return nil
		}

		os.Chmod("/tmp/woffux-update", 0755)

		// Install — needs to happen outside spinner so sudo can prompt
		currentPath, err := os.Executable()
		if err != nil {
			currentPath = "/usr/local/bin/woffux"
		}

		// Try without sudo first
		mv := exec.Command("mv", "/tmp/woffux-update", currentPath)
		if err := mv.Run(); err != nil {
			// Needs sudo — tell the user and run with TTY
			fmt.Printf("  %s Installing to %s (requires sudo)\n", sInfo, currentPath)
			sudoMv := exec.Command("sudo", "mv", "/tmp/woffux-update", currentPath)
			sudoMv.Stdin = os.Stdin
			sudoMv.Stdout = os.Stdout
			sudoMv.Stderr = os.Stderr
			if err := sudoMv.Run(); err != nil {
				fmt.Printf("\n  %s Install failed. Try manually:\n",
					lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("✗"))
				fmt.Printf("    sudo mv /tmp/woffux-update %s\n\n", currentPath)
				return nil
			}
		}

		fmt.Printf("\n  %s Updated to %s\n\n", sOk, sBold.Render(latestTag))
		return nil
	},
}

// fetchLatestTag queries the GitHub API directly (no gh CLI needed).
func fetchLatestTag() (string, error) {
	req, err := http.NewRequest("GET", releasesAPI, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("cannot reach GitHub: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	tag := strings.TrimSpace(release.TagName)
	if tag == "" {
		return "", fmt.Errorf("no releases found")
	}
	return tag, nil
}

// downloadFile downloads a URL to a local path using net/http.
func downloadFile(dst, url string) error {
	resp, err := http.Get(url)
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

	if _, err := io.Copy(out, resp.Body); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	return nil
}

func goArch() string {
	switch runtime.GOARCH {
	case "arm64":
		return "arm64"
	default:
		return "amd64"
	}
}
