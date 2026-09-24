package cmd

import (
	"fmt"
	"github.com/charmbracelet/huh"
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
	Long: `woffux — your Woffu clock-ins on autopilot.

Run woffux with no arguments for the dashboard (setup starts the first time).

Today and your data
  woffux status              Is today a working day? What's coming up
  woffux today               Today's signs and hours worked
  woffux history             Sign history, hours per day
  woffux calendar            The month: office, remote, time off, holidays
  woffux holidays            This year's company holidays
  woffux events              Vacation days and hours left
  woffux requests            Your requests, grouped by status
  woffux whoami              Your Woffu profile

Do things
  woffux sign                Clock in/out now (asks first; -y to skip)
  woffux request             Request days off or telework (pick days from a list)
  woffux request cancel      Cancel requests (pick from a list, or pass an ID)
  woffux open [page]         Open Woffu in the browser (docs, calendar, github)

Your schedule
  woffux schedule            Your week, timing, seasons and presets
  woffux schedule set "…"    Set the week from text: "L-J 8:30-13:30 14:15-17:30, V 8-15"
  woffux schedule edit       Guided editor (templates, text, day by day, summer hours)
  woffux schedule list|load|save|delete   Presets
  woffux timing [preset]     Natural timing: natural, relaxed, exact, custom

Who signs for you
  woffux agent on|off|status This Mac (on time while it's awake)
  woffux auto on|off         GitHub backup signer
  woffux sync                Push settings to the GitHub backup

Settings
  woffux setup               Guided setup (re-run any time)
  woffux config              All settings at a glance
  woffux config edit         Change any setting
  woffux update              Update to the latest version

Output: colours in a terminal, TSV when piped, --json for scripts.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDashboard()
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
	rootCmd.AddCommand(agentCmd)
	rootCmd.AddCommand(updateCmd)
}

// loadConfigOrSetup loads config + password, or guides user to setup.
func loadConfigOrSetup() (*config.Config, string, error) {
	hint := lipgloss.NewStyle().Foreground(obFaint)

	cfg, err := config.Load()
	if err != nil && isTTY() {
		// First run: don't send people off to read docs — start setup.
		start := true
		fmt.Println()
		if ferr := newForm(huh.NewGroup(huh.NewConfirm().
			Title("woffux isn't set up yet").
			Description("Setup takes about three minutes and signs nothing.").
			Affirmative("Set it up now").Negative("Not now").Value(&start))).Run(); ferr == nil && start {
			setupOpensDashboard = false // we open it right after
			if serr := runSetup(nil, nil); serr != nil {
				return nil, "", serr
			}
			cfg, err = config.Load()
		}
	}
	if err != nil {
		fmt.Println()
		fmt.Printf("  %s No config found. Run %s to get started.\n\n",
			lipgloss.NewStyle().Foreground(obOut).Render("!"),
			lipgloss.NewStyle().Bold(true).Render("woffux setup"))
		fmt.Println(hint.Render("  This is a one-time setup that configures your Woffu credentials,"))
		fmt.Println(hint.Render("  GPS coordinates, and GitHub Actions for auto-signing."))
		fmt.Println()
		return nil, "", fmt.Errorf("run 'woffux setup' first")
	}

	if cfg.WoffuEmail == "" || cfg.WoffuCompanyURL == "" {
		fmt.Println()
		fmt.Printf("  %s Config is incomplete. Run %s to reconfigure.\n\n",
			lipgloss.NewStyle().Foreground(obOut).Render("!"),
			lipgloss.NewStyle().Bold(true).Render("woffux setup"))
		return nil, "", fmt.Errorf("incomplete config — run 'woffux setup'")
	}

	password, err := config.GetPassword(cfg.WoffuEmail)
	if err != nil {
		fmt.Println()
		fmt.Printf("  %s Password not found in keychain for %s.\n",
			lipgloss.NewStyle().Foreground(obOut).Render("!"),
			cfg.WoffuEmail)
		fmt.Printf("  Run %s to reconfigure.\n\n",
			lipgloss.NewStyle().Bold(true).Render("woffux setup"))
		return nil, "", fmt.Errorf("password not in keychain — run 'woffux setup'")
	}

	return cfg, password, nil
}

// runDashboard opens the interactive TUI (running setup first if needed).
func runDashboard() error {
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
}
