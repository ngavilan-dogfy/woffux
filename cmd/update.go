package cmd

import (
	"fmt"
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

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update woffuk to the latest version",
	RunE: func(cmd *cobra.Command, args []string) error {
		sLabel := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))

		fmt.Printf("\n  %s %s\n", sLabel.Render("Current version:"), sBold.Render(Version))

		// Check latest version
		var latestTag string
		var checkErr error

		spinner.New().
			Title("Checking for updates...").
			Action(func() {
				out, err := exec.Command("gh", "api",
					"repos/ngavilan-dogfy/woffuk-cli/releases/latest",
					"--jq", ".tag_name").Output()
				if err != nil {
					checkErr = err
					return
				}
				latestTag = strings.TrimSpace(string(out))
			}).
			Run()

		if checkErr != nil {
			fmt.Printf("  %s Could not check for updates.\n\n", sWarn)
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

		// Download and replace
		binary := fmt.Sprintf("woffuk-%s-%s", runtime.GOOS, goArch())
		url := fmt.Sprintf("https://github.com/ngavilan-dogfy/woffuk-cli/releases/download/%s/%s", latestTag, binary)

		// Find current binary path
		currentPath, err := os.Executable()
		if err != nil {
			currentPath = "/usr/local/bin/woffuk"
		}

		var updateErr error
		spinner.New().
			Title(fmt.Sprintf("Downloading %s...", latestTag)).
			Action(func() {
				// Download to temp
				dl := exec.Command("curl", "-fsSL", url, "-o", "/tmp/woffuk-update")
				if out, err := dl.CombinedOutput(); err != nil {
					updateErr = fmt.Errorf("download failed: %s", string(out))
					return
				}

				// Make executable
				exec.Command("chmod", "+x", "/tmp/woffuk-update").Run()

				// Replace binary (may need sudo)
				mv := exec.Command("mv", "/tmp/woffuk-update", currentPath)
				if err := mv.Run(); err != nil {
					// Try with sudo
					mv = exec.Command("sudo", "mv", "/tmp/woffuk-update", currentPath)
					mv.Stdin = os.Stdin
					mv.Stdout = os.Stdout
					mv.Stderr = os.Stderr
					updateErr = mv.Run()
				}
			}).
			Run()

		if updateErr != nil {
			fmt.Printf("  %s Update failed: %s\n\n",
				lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("✗"), updateErr)
			return nil
		}

		fmt.Printf("\n  %s Updated to %s\n\n", sOk, sBold.Render(latestTag))
		return nil
	},
}

func goArch() string {
	switch runtime.GOARCH {
	case "arm64":
		return "arm64"
	default:
		return "amd64"
	}
}
