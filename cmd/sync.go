package cmd

import (
	"fmt"

	"github.com/charmbracelet/huh/spinner"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/ngavilan-dogfy/woffuk-cli/internal/config"
	gh "github.com/ngavilan-dogfy/woffuk-cli/internal/github"
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Push local config to GitHub so auto-signing uses your latest settings",
	Long: `Sync pushes your local configuration to GitHub Actions.

What it does:
  1. Updates GitHub secrets (email, password, coordinates, telegram)
  2. Regenerates workflow files from your schedule
  3. Pushes updated workflows to your fork

When to use it:
  • After changing your password, coordinates, or schedule
  • After editing ~/.woffuk.yaml manually
  • If auto-signing is using outdated settings

Your local config (~/.woffuk.yaml) is the source of truth.
This command makes GitHub match it.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		if cfg.GithubFork == "" {
			fmt.Println()
			fmt.Printf("  %s GitHub is not configured.\n",
				lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render("!"))
			fmt.Printf("  Run %s to set up auto-signing.\n\n",
				lipgloss.NewStyle().Bold(true).Render("woffuk setup"))
			return nil
		}

		password, err := config.GetPassword(cfg.WoffuEmail)
		if err != nil {
			return fmt.Errorf("cannot get password from keychain: %w\n\n  Run 'woffuk setup' to reconfigure", err)
		}

		sLabel := lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Width(22)
		sOkIcon := lipgloss.NewStyle().Foreground(lipgloss.Color("#22c55e")).Bold(true)
		sErrIcon := lipgloss.NewStyle().Foreground(lipgloss.Color("#ef4444")).Bold(true)

		fmt.Println()
		fmt.Printf("  Syncing local config → %s\n\n", lipgloss.NewStyle().Bold(true).Render(cfg.GithubFork))

		// Step 1: Secrets
		var secretsErr error
		spinner.New().
			Title("Updating GitHub secrets...").
			Action(func() { secretsErr = gh.SyncSecrets(cfg, password) }).
			Run()

		if secretsErr != nil {
			fmt.Printf("  %s %s%s\n", sErrIcon.Render("✗"), sLabel.Render("Secrets"), secretsErr)
		} else {
			details := fmt.Sprintf("email, password, office (%.4f,%.4f), home (%.4f,%.4f)",
				cfg.Latitude, cfg.Longitude, cfg.HomeLatitude, cfg.HomeLongitude)
			if cfg.Telegram.BotToken != "" {
				details += ", telegram"
			}
			fmt.Printf("  %s %s%s\n", sOkIcon.Render("✓"), sLabel.Render("Secrets"), lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render(details))
		}

		// Step 2: Workflows
		var workflowsErr error
		spinner.New().
			Title("Updating workflows...").
			Action(func() { workflowsErr = gh.SyncWorkflows(cfg) }).
			Run()

		if workflowsErr != nil {
			fmt.Printf("  %s %s%s\n", sErrIcon.Render("✗"), sLabel.Render("Workflows"), workflowsErr)
		} else {
			// Count enabled days and signs
			days := 0
			signs := 0
			for _, d := range []config.DaySchedule{cfg.Schedule.Monday, cfg.Schedule.Tuesday, cfg.Schedule.Wednesday, cfg.Schedule.Thursday, cfg.Schedule.Friday} {
				if d.Enabled {
					days++
					signs += len(d.Times)
				}
			}
			fmt.Printf("  %s %s%s\n", sOkIcon.Render("✓"), sLabel.Render("Workflows"),
				lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render(fmt.Sprintf("%d days, %d signs/day, tz=%s", days, signs/days, cfg.Timezone)))
		}

		// Summary
		fmt.Println()
		if secretsErr == nil && workflowsErr == nil {
			fmt.Printf("  %s GitHub is up to date. Auto-signing will use these settings.\n\n", sOkIcon.Render("✓"))
		} else {
			fmt.Printf("  %s Some items failed. Run %s to retry.\n\n",
				sErrIcon.Render("!"),
				lipgloss.NewStyle().Bold(true).Render("woffuk sync"))
		}

		return nil
	},
}
