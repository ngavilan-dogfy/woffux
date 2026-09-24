package cmd

import (
	"fmt"
	"strings"
	"time"

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

		printScheduleOverview(cfg)
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

		scheduleResult, err := scheduleWizard(true)
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
			if err := newForm(
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

var scheduleSetCmd = &cobra.Command{
	Use:   "set <week>",
	Short: `Set the schedule from text, e.g. "L-J 8:30-13:30 14:15-17:30, V 8-15"`,
	Long: `Set the schedule in one line.

Days: mon tue wed thu fri (or lunes…, L M X J V), ranges (mon-thu, L-J),
lists (L+X+V) or "weekdays". Blocks: 9-14, 8:30-13:30, 0830-1330, 15h-18h.
Separate groups with commas. Days you don't mention are days off.

Examples:
  woffux schedule set "mon-thu 8:30-13:30 14:15-17:30, fri 8-15"
  woffux schedule set "L-V 9-14 15-18"
  woffux schedule set "weekdays 8-15"`,
	Args:         cobra.MinimumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		s, err := config.ParseScheduleText(strings.Join(args, " "))
		if err != nil {
			return err
		}
		cfg.Schedule = s
		cfg.Normalize()
		if err := config.Save(cfg); err != nil {
			return fmt.Errorf("save config: %w", err)
		}
		fmt.Printf("  %s Schedule saved: %s\n", stIn.Render("✓"), weekOneLine(s))
		printWeek(s)
		if cfg.GithubFork != "" {
			fmt.Printf("  Run %s so the GitHub backup uses it too.\n\n", stBold.Render("woffux schedule push"))
		}
		return nil
	},
}

// printScheduleOverview shows the week, timing, seasons and presets.
func printScheduleOverview(cfg *config.Config) {
	title := "Your week"
	if cfg.ActiveSchedule != "" {
		title += stFaint.Render(" · preset ") + stBrand.Render(cfg.ActiveSchedule)
	}
	fmt.Println()
	fmt.Println("  " + stBold.Render(title))
	printWeek(cfg.Schedule)
	if cfg.Timing.Active() {
		fmt.Printf("  %s %s\n", stFaint.Render("Timing   "), stText.Render("natural · "+cfg.Timing.Describe()))
	} else {
		fmt.Printf("  %s %s\n", stFaint.Render("Timing   "), stText.Render("exact minute")+stFaint.Render("  (woffux timing natural)"))
	}
	for _, p := range cfg.Seasons.Periods {
		fmt.Printf("  %s %s\n", stFaint.Render("Seasonal "), stText.Render(fmt.Sprintf("%s from %s to %s, otherwise %s", p.Preset, dayMonth(p.From), dayMonth(p.To), orDefaultStr(cfg.Seasons.Default, "current"))))
	}
	if when, preset, ok := cfg.NextSeasonChange(time.Now()); ok {
		fmt.Printf("  %s %s\n", stFaint.Render("Next     "), stText.Render(fmt.Sprintf("switches to %s on %s", preset, when.Format("Mon 2 Jan 2006"))))
	}
	if names := cfg.SchedulePresetNames(); len(names) > 0 {
		fmt.Printf("  %s %s\n", stFaint.Render("Presets  "), stText.Render(strings.Join(names, ", ")))
	}
	fmt.Printf("\n  %s\n\n", stFaint.Render("Change it: woffux schedule edit · woffux schedule set \"L-V 9-14 15-18\""))
}

func init() {
	scheduleCmd.AddCommand(scheduleSetCmd)
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
