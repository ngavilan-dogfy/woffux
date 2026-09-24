package cmd

import (
	"fmt"
	"sort"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/huh/spinner"
	"github.com/spf13/cobra"

	"github.com/ngavilan-dogfy/woffux/internal/config"
	gh "github.com/ngavilan-dogfy/woffux/internal/github"
)

var autoCmd = &cobra.Command{
	Use:   "auto",
	Short: "View or toggle auto-signing",
	Long:  "Check if GitHub Actions auto-signing is enabled, or turn it on/off.",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		if cfg.GithubFork == "" {
			fmt.Println()
			fmt.Printf("  %s Auto-signing is not set up.\n", sWarn)
			fmt.Printf("  Run %s to configure GitHub Actions.\n\n", sBold.Render("woffux setup"))
			return nil
		}

		return showAutoStatus(cfg.GithubFork, cfg)
	},
}

var autoOnCmd = &cobra.Command{
	Use:   "on",
	Short: "Enable auto-signing",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		if cfg.GithubFork == "" {
			fmt.Printf("\n  %s Run %s first.\n\n", sWarn, sBold.Render("woffux setup"))
			return nil
		}

		var enableErr error
		spinner.New().
			Title("Syncing and enabling auto-sign...").
			Action(func() { enableErr = gh.EnableAndRefreshAutoSign(cfg) }).
			Run()

		if enableErr != nil {
			uiErr("Could not enable: %s", enableErr)
			return nil
		}

		fmt.Println()
		uiOK("GitHub backup signer on")
		return showAutoStatus(cfg.GithubFork, cfg)
	},
}

var autoOffCmd = &cobra.Command{
	Use:   "off",
	Short: "Disable auto-signing",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		if cfg.GithubFork == "" {
			fmt.Printf("\n  %s Run %s first.\n\n", sWarn, sBold.Render("woffux setup"))
			return nil
		}

		var confirm bool
		if err := newForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title("Turn off the GitHub backup signer?").
					Description("If this Mac's agent is off too, nothing will sign for you.").
					Affirmative("Disable").
					Negative("Cancel").
					Value(&confirm),
			),
		).Run(); err != nil {
			return err
		}

		if !confirm {
			return nil
		}

		var disableErr error
		spinner.New().
			Title("Disabling auto-sign...").
			Action(func() { disableErr = gh.DisableAutoSign(cfg.GithubFork) }).
			Run()

		if disableErr != nil {
			uiErr("Could not disable: %s", disableErr)
			return nil
		}

		fmt.Println()
		uiOK("GitHub backup signer off")
		fmt.Println()
		return nil
	},
}

func init() {
	autoCmd.AddCommand(autoOnCmd)
	autoCmd.AddCommand(autoOffCmd)
}

func showAutoStatus(repo string, cfg *config.Config) error {
	var workflows []gh.WorkflowStatus
	var statusErr error
	var inSync bool
	var syncErr error

	spinner.New().
		Title("Checking workflows...").
		Action(func() {
			workflows, statusErr = gh.GetAutoSignStatus(repo)
			if cfg != nil {
				inSync, syncErr = gh.CheckWorkflowSync(repo, cfg)
			}
		}).
		Run()

	if statusErr != nil {
		fmt.Printf("\n  %s Could not check status: %s\n\n", sWarn, statusErr)
		return nil
	}

	uiTitle("GitHub backup signer", repo)
	signing := false
	sort.SliceStable(workflows, func(i, j int) bool { return workflows[i].Name == "Auto Sign" })
	for _, w := range workflows {
		switch w.Name {
		case "Auto Sign":
			signing = w.State == "active"
			if signing {
				uiRow("Signing", stIn.Render("● on")+stFaint.Render("  signs when this Mac can't, a few minutes after it"))
			} else {
				uiRow("Signing", stFaint.Render("○ off"))
			}
		case "Keepalive":
			if w.State == "active" {
				uiRow("Keepalive", stSubtle.Render("on")+stFaint.Render("  stops GitHub pausing it after 60 days"))
			} else {
				uiRow("Keepalive", stOut.Render("off")+stFaint.Render("  GitHub may pause signing after 60 days"))
			}
		}
	}
	if createdAt, conclusion, ok, err := gh.LastScheduledRun(repo); err == nil && ok {
		if t, perr := time.Parse(time.RFC3339, createdAt); perr == nil {
			res := stIn.Render("ok")
			if conclusion != "success" {
				res = stBad.Render(orDefaultStr(conclusion, "running"))
			}
			uiRow("Last run", stText.Render(t.Local().Format("Mon 2 Jan 15:04"))+"  "+res)
		}
	}
	if cfg != nil {
		switch {
		case syncErr != nil:
			uiRow("Settings", stOut.Render("couldn't check: "+syncErr.Error()))
		case inSync:
			uiRow("Settings", stIn.Render("✓ up to date"))
		default:
			uiRow("Settings", stOut.Render("! outdated")+stFaint.Render("  run woffux sync"))
		}
	}
	if signing {
		uiHint("woffux auto off", "woffux open github")
	} else {
		uiHint("woffux auto on", "woffux open github")
	}
	return nil
}
