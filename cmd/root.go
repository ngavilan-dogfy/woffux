package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/ngavilan-dogfy/woffux/internal/config"
	"github.com/ngavilan-dogfy/woffux/internal/selfupdate"
	"github.com/ngavilan-dogfy/woffux/internal/tui"
	"github.com/ngavilan-dogfy/woffux/internal/woffu"
)

var rootCmd = &cobra.Command{
	Use:          "woffux",
	Short:        "Woffu time tracking CLI",
	SilenceUsage: true,
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
  woffux doctor              Check everything, with a fix for each problem
  woffux update              Update to the latest version

Output: colours in a terminal, TSV when piped, --json for scripts.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDashboard()
	},
}

// quietError fails a command (exit 1) without printing it again: the
// command already explained the problem.
type quietError struct{ error }

func Execute() {
	rootCmd.SilenceErrors = true
	rootCmd.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
		return fmt.Errorf("%w (see 'woffux %s --help')", err, strings.TrimSpace(strings.TrimPrefix(c.CommandPath(), "woffux")))
	})
	notifier := startNotifier()
	err := rootCmd.Execute()
	if err != nil {
		var quiet quietError
		if !errors.As(err, &quiet) {
			printError(err)
		}
	}
	notifier.Print(os.Stderr)
	if err != nil {
		os.Exit(1)
	}
}

// printError reports a failure on stderr: one styled line for a person, a
// plain "Error: …" line for logs (the local agent's log reads these), and a
// JSON object when the command was asked for JSON.
func printError(err error) {
	switch {
	case wantsJSON():
		b, _ := json.Marshal(map[string]string{"error": err.Error()})
		fmt.Fprintln(os.Stderr, string(b))
	case term.IsTerminal(int(os.Stderr.Fd())):
		fmt.Fprintln(os.Stderr, uiIndent+stBad.Render("✗ ")+stText.Render(err.Error()))
		fmt.Fprintln(os.Stderr)
	default:
		fmt.Fprintln(os.Stderr, "Error:", err)
	}
}

// startNotifier looks for a newer release in the background (cached, at
// most once a day) when a person is at a terminal; the notice prints after
// the command, on stderr. The dashboard has its own.
func startNotifier() *selfupdate.Notifier {
	if len(os.Args) < 2 || !isTTY() || !term.IsTerminal(int(os.Stderr.Fd())) || wantsJSON() {
		return nil
	}
	switch os.Args[1] {
	case "update", "upgrade", "version", "--version", "-v", "completion", "__complete", "__completeNoDesc":
		return nil
	}
	return selfupdate.StartNotifier(updateTool(), cacheDir())
}

// wantsJSON reports whether --json appeared anywhere on the command line.
func wantsJSON() bool {
	for _, a := range os.Args[1:] {
		if a == "--json" {
			return true
		}
	}
	return false
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

// loadConfigOrSetup loads the settings and the password, offering the
// setup when there are none and a person is at the terminal.
func loadConfigOrSetup() (*config.Config, string, error) {
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
		return nil, "", errors.New("woffux isn't set up yet — run 'woffux setup'")
	}
	if cfg.WoffuEmail == "" || cfg.WoffuCompanyURL == "" {
		return nil, "", errors.New("your settings are incomplete — run 'woffux setup'")
	}
	password, err := config.GetPassword(cfg.WoffuEmail)
	if err != nil {
		return nil, "", fmt.Errorf("there's no Woffu password for %s in the keychain — run 'woffux setup'", cfg.WoffuEmail)
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

	tui.AppVersion = currentBuild().Version
	tui.CheckLatest = func() (string, error) {
		rel, err := selfupdate.Latest(updateTool())
		return rel.Tag, err
	}
	model := tui.NewDashboard(client, companyClient, cfg, password)
	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return err
	}
	return nil
}
