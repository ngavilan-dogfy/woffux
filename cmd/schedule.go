package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/huh/spinner"
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
		uiTitle("Schedule presets", fmt.Sprintf("%d saved", len(cfg.SavedSchedules)))
		if len(cfg.SavedSchedules) == 0 {
			uiLine(stFaint.Render("None yet. Save the current week with: woffux schedule save <name>"))
			fmt.Println()
			return nil
		}
		for _, name := range cfg.SchedulePresetNames() {
			printPresetRow(cfg, name)
		}
		uiHint("woffux schedule load", "woffux schedule save <name>", "woffux schedule delete")
		return nil
	},
}

// printPresetRow renders one preset: name, badges, the week as text, hours.
func printPresetRow(cfg *config.Config, name string) {
	s := cfg.SavedSchedules[name]
	mark, st := stFaint.Render("○"), stText
	if name == cfg.ActiveSchedule {
		mark, st = stIn.Render("●"), stBold
	}
	badges := ""
	if name == cfg.ActiveSchedule {
		badges += stIn.Render("  in use")
	}
	for _, p := range cfg.Seasons.Periods {
		if p.Preset == name {
			badges += stOut.Render(fmt.Sprintf("  %s → %s", dayMonth(p.From), dayMonth(p.To)))
		}
	}
	if cfg.Seasons.Default == name && len(cfg.Seasons.Periods) > 0 {
		badges += stFaint.Render("  rest of the year")
	}
	uiLine(mark + " " + st.Render(name) + badges)
	uiLine("  " + stSubtle.Render(config.ScheduleText(s)) + stFaint.Render("  ·  "+config.FormatMinutes(s.WeekMinutes())+"/week"))
}

// pickPreset asks which preset to act on when no name was given.
func pickPreset(cfg *config.Config, title string) (string, error) {
	names := cfg.SchedulePresetNames()
	if len(names) == 0 {
		return "", fmt.Errorf("no saved presets yet — save one with: woffux schedule save <name>")
	}
	var opts []huh.Option[string]
	for _, n := range names {
		s := cfg.SavedSchedules[n]
		label := fmt.Sprintf("%-14s %s", n, stFaint.Render(config.ScheduleText(s)+" · "+config.FormatMinutes(s.WeekMinutes())))
		if n == cfg.ActiveSchedule {
			label += stIn.Render("  in use")
		}
		opts = append(opts, huh.NewOption(label, n))
	}
	var name string
	err := newForm(huh.NewGroup(huh.NewSelect[string]().Title(title).Options(opts...).Value(&name))).Run()
	return name, err
}

var scheduleSaveCmd = &cobra.Command{
	Use:          "save [name]",
	Short:        "Save the current week as a named preset",
	Args:         cobra.MaximumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		name := ""
		if len(args) == 1 {
			name = args[0]
		} else if err := newForm(huh.NewGroup(huh.NewInput().
			Title("Name this schedule").
			Description(weekOneLine(cfg.Schedule)).
			Placeholder("e.g. winter, summer, 4-days").
			Value(&name).
			Validate(func(s string) error {
				if config.NormalizePresetName(s) == "" {
					return fmt.Errorf("give it a name")
				}
				return nil
			}))).Run(); err != nil {
			return err
		}
		name = config.NormalizePresetName(name)
		if _, exists := cfg.SavedSchedules[name]; exists && len(args) == 0 {
			overwrite := false
			if err := newForm(huh.NewGroup(huh.NewConfirm().Title(fmt.Sprintf("Replace the existing %q?", name)).
				Affirmative("Replace").Negative("Cancel").Value(&overwrite))).Run(); err != nil || !overwrite {
				return err
			}
		}
		if err := cfg.SaveSchedulePreset(name, cfg.Schedule); err != nil {
			return err
		}
		cfg.ActiveSchedule = name
		if err := config.Save(cfg); err != nil {
			return fmt.Errorf("save config: %w", err)
		}
		uiOK("Saved as %q — switch back any time with: woffux schedule load %s", name, name)
		fmt.Println()
		return nil
	},
}

var scheduleLoadCmd = &cobra.Command{
	Use:          "load [name]",
	Short:        "Switch to a saved preset",
	Args:         cobra.MaximumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		name := ""
		if len(args) == 1 {
			name = config.NormalizePresetName(args[0])
		} else if name, err = pickPreset(cfg, "Switch to which schedule?"); err != nil {
			return err
		}
		if !cfg.LoadSchedulePreset(name) {
			return fmt.Errorf("no preset called %q — see: woffux schedule list", name)
		}
		if err := config.Save(cfg); err != nil {
			return fmt.Errorf("save config: %w", err)
		}
		uiOK("Now using %q", name)
		printWeek(cfg.Schedule)
		if len(cfg.Seasons.Periods) > 0 {
			uiWarn("Seasonal switching is on: on its next date woffux will switch presets again.")
		}
		if cfg.GithubFork != "" {
			var pushErr error
			spinner.New().Title("Updating the GitHub backup…").
				Action(func() { _, pushErr = gh.SyncWorkflowsAndRefresh(cfg) }).Run()
			if pushErr != nil {
				uiErr("GitHub update failed: %s — run woffux sync", pushErr)
			} else {
				uiOK("GitHub backup updated")
			}
		}
		fmt.Println()
		return nil
	},
}

var scheduleDeleteCmd = &cobra.Command{
	Use:          "delete [name]",
	Short:        "Delete a saved preset",
	Args:         cobra.MaximumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		name := ""
		if len(args) == 1 {
			name = config.NormalizePresetName(args[0])
		} else if name, err = pickPreset(cfg, "Delete which preset?"); err != nil {
			return err
		}
		if _, ok := cfg.SavedSchedules[name]; !ok {
			return fmt.Errorf("no preset called %q — see: woffux schedule list", name)
		}
		for _, p := range cfg.Seasons.Periods {
			if p.Preset == name || cfg.Seasons.Default == name {
				return fmt.Errorf("%q is used by your seasonal schedule — change it first with: woffux schedule edit", name)
			}
		}
		if len(args) == 0 {
			ok := false
			if err := newForm(huh.NewGroup(huh.NewConfirm().Title(fmt.Sprintf("Delete %q?", name)).
				Description(config.ScheduleText(cfg.SavedSchedules[name])).
				Affirmative("Delete").Negative("Keep it").Value(&ok))).Run(); err != nil || !ok {
				return err
			}
		}
		delete(cfg.SavedSchedules, name)
		if cfg.ActiveSchedule == name {
			cfg.ActiveSchedule = ""
		}
		if err := config.Save(cfg); err != nil {
			return fmt.Errorf("save config: %w", err)
		}
		uiOK("Deleted %q (your current week didn't change)", name)
		fmt.Println()
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
