package cmd

import (
	"fmt"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/huh/spinner"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/ngavilan-dogfy/woffuk-cli/internal/config"
	gh "github.com/ngavilan-dogfy/woffuk-cli/internal/github"
)

var scheduleCmd = &cobra.Command{
	Use:   "schedule",
	Short: "View or edit auto-sign schedule",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		sIn := lipgloss.NewStyle().Foreground(lipgloss.Color("82"))
		sOut := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))

		fmt.Printf("Timezone: %s\n\n", cfg.Timezone)
		fmt.Printf("  %s = clock in    %s = clock out\n\n", sIn.Render("▶ IN"), sOut.Render("■ OUT"))
		printScheduleVisual(cfg.Schedule, sIn, sOut)
		fmt.Println()
		return nil
	},
}

var scheduleEditCmd = &cobra.Command{
	Use:   "edit",
	Short: "Edit auto-sign schedule interactively",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		schedule, tz, err := scheduleWizard()
		if err != nil {
			return err
		}

		cfg.Schedule = schedule
		if tz != "" {
			cfg.Timezone = tz
		}

		if err := config.Save(cfg); err != nil {
			return fmt.Errorf("save config: %w", err)
		}

		fmt.Printf("  %s Schedule saved!\n", sOk)

		// Push to GitHub if configured
		if cfg.GithubFork != "" {
			var push bool
			huh.NewForm(
				huh.NewGroup(
					huh.NewConfirm().
						Title(fmt.Sprintf("Push to %s?", cfg.GithubFork)).
						Affirmative("Yes").
						Negative("Skip").
						Value(&push),
				),
			).Run()

			if push {
				var pushErr error
				spinner.New().
					Title("Pushing workflows...").
					Action(func() { pushErr = gh.SyncWorkflows(cfg) }).
					Run()

				if pushErr != nil {
					fmt.Printf("  %s Push failed: %s\n", sWarn, pushErr)
				} else {
					fmt.Printf("  %s Workflows updated!\n", sOk)
				}
			}
		}

		return nil
	},
}

var schedulePushCmd = &cobra.Command{
	Use:   "push",
	Short: "Push current schedule as GitHub Actions workflows",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		var pushErr error
		spinner.New().
			Title(fmt.Sprintf("Pushing to %s...", cfg.GithubFork)).
			Action(func() { pushErr = gh.SyncWorkflows(cfg) }).
			Run()

		if pushErr != nil {
			return pushErr
		}
		fmt.Printf("  %s Workflows updated!\n", sOk)
		return nil
	},
}

func init() {
	scheduleCmd.AddCommand(scheduleEditCmd)
	scheduleCmd.AddCommand(schedulePushCmd)
}
