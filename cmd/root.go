package cmd

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/ngavilan-dogfy/woffux/internal/config"
	"github.com/ngavilan-dogfy/woffux/internal/tui"
	"github.com/ngavilan-dogfy/woffux/internal/woffu"
)

var rootCmd = &cobra.Command{
	Use:     "woffux",
	Short:   "Woffu time tracking CLI",
	Version: Version,
	Long: `A CLI tool for Woffu time tracking. Fully scriptable and pipe-friendly.

All commands auto-detect TTY:
  • Terminal  → colored, human-friendly output
  • Piped     → machine-readable TSV
  • --json    → structured JSON

Querying:
  woffux status              Today's signing status
  woffux today               Detailed day info + sign slots
  woffux events              Available vacations, hours, etc.
  woffux requests            Your requests (vacations, telework, absences)
  woffux history             Sign history (clock in/out records)
  woffux calendar            Working days, holidays, telework
  woffux holidays            Company holidays
  woffux schedule            View auto-sign schedule
  woffux whoami              Current user profile

Actions:
  woffux sign                Clock in/out right now
  woffux sign --force        Sign even on non-working days
  woffux request             Create a request (telework, vacation, absence)
  woffux request cancel <id> Cancel a request
  woffux auto                Check auto-signing status
  woffux auto on/off         Toggle auto-signing
  woffux open [page]         Open Woffu in browser (docs, calendar, github)

Configuration:
  woffux setup               Full setup wizard
  woffux config              View all settings
  woffux config edit         Change any individual setting
  woffux schedule edit       Edit schedule and push to GitHub
  woffux sync                Re-sync secrets + workflows
  woffux update              Update to latest version (alias: upgrade)

Output modes (on most commands):
  --json                     Structured JSON for scripting
  --plain                    TSV for awk/grep/cut
  (auto-detects piped output → TSV)`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, password, err := loadConfigOrSetup()
		if err != nil {
			return err
		}

		client := woffu.NewWoffuClient(cfg.WoffuURL)
		companyClient := woffu.NewCompanyClient(cfg.WoffuCompanyURL)

		model := tui.NewDashboard(client, companyClient, cfg, password)
		p := tea.NewProgram(model, tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			return err
		}
		return nil
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(signCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(eventsCmd)
	rootCmd.AddCommand(historyCmd)
	rootCmd.AddCommand(calendarCmd)
	rootCmd.AddCommand(requestsCmd)
	rootCmd.AddCommand(requestCmd)
	rootCmd.AddCommand(holidaysCmd)
	rootCmd.AddCommand(todayCmd)
	rootCmd.AddCommand(whoamiCmd)
	rootCmd.AddCommand(openCmd)
	rootCmd.AddCommand(setupCmd)
	rootCmd.AddCommand(syncCmd)
	rootCmd.AddCommand(scheduleCmd)
	rootCmd.AddCommand(configCmd)
	rootCmd.AddCommand(autoCmd)
	rootCmd.AddCommand(updateCmd)
}

// loadConfigOrSetup loads config + password, or guides user to setup.
func loadConfigOrSetup() (*config.Config, string, error) {
	hint := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))

	cfg, err := config.Load()
	if err != nil {
		fmt.Println()
		fmt.Printf("  %s No config found. Run %s to get started.\n\n",
			lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render("!"),
			lipgloss.NewStyle().Bold(true).Render("woffux setup"))
		fmt.Println(hint.Render("  This is a one-time setup that configures your Woffu credentials,"))
		fmt.Println(hint.Render("  GPS coordinates, and GitHub Actions for auto-signing."))
		fmt.Println()
		return nil, "", fmt.Errorf("run 'woffux setup' first")
	}

	if cfg.WoffuEmail == "" || cfg.WoffuCompanyURL == "" {
		fmt.Println()
		fmt.Printf("  %s Config is incomplete. Run %s to reconfigure.\n\n",
			lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render("!"),
			lipgloss.NewStyle().Bold(true).Render("woffux setup"))
		return nil, "", fmt.Errorf("incomplete config — run 'woffux setup'")
	}

	password, err := config.GetPassword(cfg.WoffuEmail)
	if err != nil {
		fmt.Println()
		fmt.Printf("  %s Password not found in keychain for %s.\n",
			lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render("!"),
			cfg.WoffuEmail)
		fmt.Printf("  Run %s to reconfigure.\n\n",
			lipgloss.NewStyle().Bold(true).Render("woffux setup"))
		return nil, "", fmt.Errorf("password not in keychain — run 'woffux setup'")
	}

	return cfg, password, nil
}
