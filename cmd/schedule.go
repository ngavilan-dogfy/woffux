package cmd

import (
	"fmt"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/huh/spinner"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/ngavilan-dogfy/woffux/internal/config"
	gh "github.com/ngavilan-dogfy/woffux/internal/github"
)

var scheduleJSONFlag bool

var scheduleCmd = &cobra.Command{
	Use:   "schedule",
	Short: "View or edit auto-sign schedule",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		// JSON output
		if scheduleJSONFlag {
			return printJSON(scheduleToJSON(cfg))
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

		scheduleResult, err := scheduleWizard()
		if err != nil {
			return err
		}

		if err := applyScheduleWizardResult(cfg, scheduleResult); err != nil {
			return err
		}

		if err := config.Save(cfg); err != nil {
			return fmt.Errorf("save config: %w", err)
		}

		fmt.Printf("  %s Schedule saved!\n", sOk)
		if msg := scheduleWizardSavedPresetMessage(scheduleResult); msg != "" {
			fmt.Print(msg)
		}

		// Offer to sync workflows when GitHub is configured.
		if cfg.GithubFork != "" {
			var push bool
			if err := huh.NewForm(
				huh.NewGroup(
					huh.NewConfirm().
						Title(fmt.Sprintf("Push to %s?", cfg.GithubFork)).
						Affirmative("Yes").
						Negative("Skip").
						Value(&push),
				),
			).Run(); err != nil {
				return err
			}

			if !push {
				return nil
			}

			var pushErr error
			var reloaded bool
			spinner.New().
				Title("Pushing workflows...").
				Action(func() { reloaded, pushErr = gh.SyncWorkflowsAndRefresh(cfg) }).
				Run()

			if pushErr != nil {
				fmt.Printf("  %s Push failed: %s\n", sWarn, pushErr)
			} else {
				fmt.Printf("  %s Workflows updated!\n", sOk)
				if reloaded {
					fmt.Printf("  %s Cron triggers refreshed!\n", sOk)
				} else {
					fmt.Printf("  %s Auto-sign disabled, cron reload skipped.\n", sWarn)
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
		if cfg.GithubFork == "" {
			return fmt.Errorf("no github fork configured — run 'woffux setup' first")
		}

		var pushErr error
		var reloaded bool
		spinner.New().
			Title(fmt.Sprintf("Pushing to %s...", cfg.GithubFork)).
			Action(func() { reloaded, pushErr = gh.SyncWorkflowsAndRefresh(cfg) }).
			Run()

		if pushErr != nil {
			return pushErr
		}
		fmt.Printf("  %s Workflows updated!\n", sOk)
		if reloaded {
			fmt.Printf("  %s Cron triggers refreshed!\n", sOk)
		} else {
			fmt.Printf("  %s Auto-sign disabled, cron reload skipped.\n", sWarn)
		}
		return nil
	},
}

var scheduleListCmd = &cobra.Command{
	Use:   "list",
	Short: "List saved schedule presets",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		if len(cfg.SavedSchedules) == 0 {
			fmt.Println("  No saved presets. Use 'woffux schedule save <name>' to save the current schedule.")
			return nil
		}

		sIn := lipgloss.NewStyle().Foreground(lipgloss.Color("82"))
		sOut := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
		sActive := lipgloss.NewStyle().Foreground(lipgloss.Color("82")).Bold(true)

		for _, name := range cfg.SchedulePresetNames() {
			s := cfg.SavedSchedules[name]
			label := sBold.Render(name)
			if name == cfg.ActiveSchedule {
				label += sActive.Render(" (active)")
			}
			fmt.Printf("\n  %s\n", label)
			printScheduleVisual(s, sIn, sOut)
		}
		fmt.Println()
		return nil
	},
}

var scheduleSaveCmd = &cobra.Command{
	Use:   "save <name>",
	Short: "Save current schedule as a named preset",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		name := config.NormalizePresetName(args[0])
		if err := cfg.SaveSchedulePreset(name, cfg.Schedule); err != nil {
			return err
		}
		cfg.ActiveSchedule = name

		if err := config.Save(cfg); err != nil {
			return fmt.Errorf("save config: %w", err)
		}

		fmt.Printf("  %s Saved as \"%s\"\n", sOk, name)
		return nil
	},
}

var scheduleLoadCmd = &cobra.Command{
	Use:   "load <name>",
	Short: "Load a saved schedule preset",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		name := config.NormalizePresetName(args[0])
		if !cfg.LoadSchedulePreset(name) {
			return fmt.Errorf("preset \"%s\" not found. Use 'woffux schedule list' to see available presets", name)
		}

		if err := config.Save(cfg); err != nil {
			return fmt.Errorf("save config: %w", err)
		}

		sIn := lipgloss.NewStyle().Foreground(lipgloss.Color("82"))
		sOut := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))

		fmt.Printf("  %s Loaded \"%s\"\n\n", sOk, name)
		printScheduleVisual(cfg.Schedule, sIn, sOut)
		fmt.Println()

		// Sync workflows if configured
		if cfg.GithubFork != "" {
			var pushErr error
			var reloaded bool
			spinner.New().
				Title("Pushing workflows...").
				Action(func() { reloaded, pushErr = gh.SyncWorkflowsAndRefresh(cfg) }).
				Run()

			if pushErr != nil {
				fmt.Printf("  %s Push failed: %s\n", sWarn, pushErr)
			} else {
				fmt.Printf("  %s Workflows updated!\n", sOk)
				if reloaded {
					fmt.Printf("  %s Cron triggers refreshed!\n", sOk)
				} else {
					fmt.Printf("  %s Auto-sign disabled, cron reload skipped.\n", sWarn)
				}
			}
		}

		return nil
	},
}

var scheduleDeleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Delete a saved schedule preset",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		name := config.NormalizePresetName(args[0])
		if _, ok := cfg.SavedSchedules[name]; !ok {
			return fmt.Errorf("preset \"%s\" not found", name)
		}

		delete(cfg.SavedSchedules, name)
		if cfg.ActiveSchedule == name {
			cfg.ActiveSchedule = ""
		}

		if err := config.Save(cfg); err != nil {
			return fmt.Errorf("save config: %w", err)
		}

		fmt.Printf("  %s Deleted \"%s\"\n", sOk, name)
		return nil
	},
}

func init() {
	scheduleCmd.Flags().BoolVar(&scheduleJSONFlag, "json", false, "Output as JSON")
	scheduleCmd.AddCommand(scheduleEditCmd)
	scheduleCmd.AddCommand(schedulePushCmd)
	scheduleCmd.AddCommand(scheduleListCmd)
	scheduleCmd.AddCommand(scheduleSaveCmd)
	scheduleCmd.AddCommand(scheduleLoadCmd)
	scheduleCmd.AddCommand(scheduleDeleteCmd)
}

// scheduleToJSON builds a structured map for JSON output.
func scheduleToJSON(cfg *config.Config) map[string]interface{} {
	result := map[string]interface{}{
		"timezone": cfg.Timezone,
		"days":     daySchedulesToJSON(cfg.Schedule),
	}
	if cfg.ActiveSchedule != "" {
		result["active_preset"] = cfg.ActiveSchedule
	}
	return result
}

func daySchedulesToJSON(s config.Schedule) map[string]interface{} {
	return map[string]interface{}{
		"monday":    dayToJSON(s.Monday),
		"tuesday":   dayToJSON(s.Tuesday),
		"wednesday": dayToJSON(s.Wednesday),
		"thursday":  dayToJSON(s.Thursday),
		"friday":    dayToJSON(s.Friday),
	}
}

func dayToJSON(d config.DaySchedule) map[string]interface{} {
	result := map[string]interface{}{
		"enabled": d.Enabled,
	}
	if d.Enabled {
		var times []string
		for _, t := range d.Times {
			times = append(times, t.Time)
		}
		result["times"] = times
	}
	return result
}
