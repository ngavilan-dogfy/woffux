package cmd

import (
	"fmt"
	"os/exec"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/ngavilan-dogfy/woffux/internal/config"
)

var openCmd = &cobra.Command{
	Use:   "open [page]",
	Short: "Open Woffu in browser",
	Long: `Open Woffu pages in your browser.

Examples:
  woffux open              Dashboard
  woffux open docs         Personal documents
  woffux open calendar     Calendar view
  woffux open github       GitHub fork actions`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		page := ""
		if len(args) > 0 {
			page = args[0]
		}

		var url string
		switch page {
		case "docs", "documents":
			url = cfg.WoffuCompanyURL + "/v2/personal/documents"
		case "calendar", "cal":
			url = cfg.WoffuCompanyURL + "/v2/personal/calendar"
		case "github", "gh":
			if cfg.GithubFork != "" {
				url = "https://github.com/" + cfg.GithubFork + "/actions"
			} else {
				return fmt.Errorf("no GitHub fork configured — run 'woffux setup'")
			}
		case "profile":
			url = cfg.WoffuCompanyURL + "/v2/personal/profile"
		case "":
			url = cfg.WoffuCompanyURL + "/v2"
		default:
			url = cfg.WoffuCompanyURL + "/v2/" + page
		}

		fmt.Printf("  Opening %s\n", url)
		return openBrowser(url)
	},
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	}
	if cmd == nil {
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
	return cmd.Start()
}
