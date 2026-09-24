package cmd

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/huh/spinner"
	"github.com/spf13/cobra"

	"github.com/ngavilan-dogfy/woffux/internal/agent"
	"github.com/ngavilan-dogfy/woffux/internal/config"
	gh "github.com/ngavilan-dogfy/woffux/internal/github"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "See all your settings in one place",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		viewConfig(cfg)
		return nil
	},
}

// viewConfig shows every setting, grouped the way people think about them.
func viewConfig(cfg *config.Config) {
	uiTitle("Settings", "~/.woffux.yaml")

	uiSection("Account")
	uiRow("Email", stText.Render(cfg.WoffuEmail))
	uiRow("Company", stText.Render(strings.TrimPrefix(cfg.WoffuCompanyURL, "https://")))
	uiRow("Password", stFaint.Render("•••••••• in the system keychain"))

	uiSection("Places")
	uiRow("Office", stText.Render(fmt.Sprintf("%.5f, %.5f", cfg.Latitude, cfg.Longitude))+stFaint.Render("  office days"))
	uiRow("Home", stText.Render(fmt.Sprintf("%.5f, %.5f", cfg.HomeLatitude, cfg.HomeLongitude))+stFaint.Render("  telework days"))

	uiSection("Schedule")
	week := weekOneLine(cfg.Schedule)
	if cfg.ActiveSchedule != "" {
		week += stFaint.Render("  (" + cfg.ActiveSchedule + ")")
	}
	uiRow("Week", stText.Render(week))
	for _, p := range cfg.Seasons.Periods {
		uiRow("Seasonal", stText.Render(fmt.Sprintf("%s %s → %s", p.Preset, dayMonth(p.From), dayMonth(p.To))))
	}
	if cfg.Timing.Active() {
		uiRow("Timing", stText.Render("natural · "+cfg.Timing.Describe()))
	} else {
		uiRow("Timing", stText.Render("exact minute"))
	}
	if names := cfg.SchedulePresetNames(); len(names) > 0 {
		uiRow("Presets", stSubtle.Render(strings.Join(names, ", ")))
	}

	uiSection("Who signs")
	if agent.Supported() {
		if agent.Installed() && agent.Loaded() {
			uiRow("This Mac", stIn.Render("● on"))
		} else {
			uiRow("This Mac", stFaint.Render("○ off"))
		}
	}
	switch {
	case cfg.GithubFork == "":
		uiRow("GitHub", stFaint.Render("○ not set up"))
	default:
		state := stFaint.Render("○ off")
		if on, err := gh.IsAutoSignEnabled(cfg.GithubFork); err == nil && on {
			state = stIn.Render("● on")
		}
		uiRow("GitHub", state+stFaint.Render("  "+cfg.GithubFork))
	}

	uiSection("Notifications")
	if cfg.Telegram.BotToken != "" {
		uiRow("Telegram", stIn.Render("● on"))
	} else {
		uiRow("Telegram", stFaint.Render("○ off"))
	}
	uiHint("woffux config edit", "woffux setup")
}

var configEditCmd = &cobra.Command{
	Use:   "edit",
	Short: "Change any setting",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		for {
			done, err := editOneSetting(cfg)
			if err != nil || done {
				return err
			}
		}
	},
}

// editOneSetting shows the settings menu, applies one change and returns
// done=true when the user picks "Done".
func editOneSetting(cfg *config.Config) (bool, error) {
	timingText := "exact minute"
	if cfg.Timing.Active() {
		timingText = cfg.Timing.Short()
	}
	signers := "nobody (manual)"
	mac := agent.Supported() && agent.Installed() && agent.Loaded()
	switch {
	case mac && cfg.GithubFork != "":
		signers = "this Mac + GitHub"
	case mac:
		signers = "this Mac"
	case cfg.GithubFork != "":
		signers = "GitHub"
	}
	tg := "off"
	if cfg.Telegram.BotToken != "" {
		tg = "on"
	}
	opt := func(label, value, key string) huh.Option[string] {
		return huh.NewOption(fmt.Sprintf("%-15s %s", label, stFaint.Render(value)), key)
	}
	var field string
	if err := newForm(huh.NewGroup(huh.NewSelect[string]().
		Title("What would you like to change?").
		Options(
			opt("Schedule", config.ScheduleText(cfg.Schedule), "schedule"),
			opt("Natural timing", timingText, "timing"),
			opt("Who signs", signers, "signers"),
			opt("Office", fmt.Sprintf("%.4f, %.4f", cfg.Latitude, cfg.Longitude), "office"),
			opt("Home", fmt.Sprintf("%.4f, %.4f", cfg.HomeLatitude, cfg.HomeLongitude), "home"),
			opt("Telegram", tg, "telegram"),
			opt("Email", cfg.WoffuEmail, "email"),
			opt("Password", "••••••••", "password"),
			huh.NewOption(stSubtle.Render("Done"), "done"),
		).
		Value(&field))).Run(); err != nil {
		return true, err
	}

	syncNeeded := false
	var password string
	switch field {
	case "done":
		return true, nil
	case "schedule":
		res, err := scheduleWizard(true)
		if err != nil {
			return false, nil
		}
		if err := applyScheduleWizardResult(cfg, res); err != nil {
			return true, err
		}
		syncNeeded = true
	case "timing":
		t, err := timingWizard(cfg.Timing, cfg.Schedule)
		if err != nil {
			return false, nil
		}
		cfg.Timing = t
		syncNeeded = true
	case "signers":
		plan, err := signerChoice(cfg)
		if err != nil {
			return false, nil
		}
		pw, _ := config.GetPassword(cfg.WoffuEmail)
		applySigners(cfg, pw, plan)
	case "office", "home":
		lat, lon := cfg.Latitude, cfg.Longitude
		title := "Office location"
		if field == "home" {
			lat, lon, title = cfg.HomeLatitude, cfg.HomeLongitude, "Home location (for telework days)"
		}
		nlat, nlon, err := locationPickerWithMap(title, lat, lon)
		if err != nil {
			return false, nil
		}
		if field == "home" {
			cfg.HomeLatitude, cfg.HomeLongitude = nlat, nlon
		} else {
			cfg.Latitude, cfg.Longitude = nlat, nlon
		}
		syncNeeded = true
	case "telegram":
		tgCfg, err := telegramSetup()
		if err != nil {
			return false, nil
		}
		cfg.Telegram = tgCfg
		syncNeeded = true
	case "email":
		email := cfg.WoffuEmail
		if err := newForm(huh.NewGroup(huh.NewInput().Title("Woffu email").Value(&email).
			Validate(func(s string) error {
				if extractCompany(s) == "" {
					return fmt.Errorf("enter a valid email")
				}
				return nil
			}))).Run(); err != nil || email == cfg.WoffuEmail {
			return false, nil
		}
		if pw, err := config.GetPassword(cfg.WoffuEmail); err == nil {
			_ = config.SetPassword(email, pw)
		} else {
			uiWarn("The password wasn't copied to the new email — set it next.")
		}
		cfg.WoffuEmail = email
		cfg.WoffuCompanyURL = "https://" + extractCompany(email) + ".woffu.com"
		syncNeeded = true
	case "password":
		if err := newForm(huh.NewGroup(huh.NewInput().Title("New Woffu password").
			Description("Stored in the system keychain.").
			EchoMode(huh.EchoModePassword).Value(&password))).Run(); err != nil || password == "" {
			return false, nil
		}
		if err := config.SetPassword(cfg.WoffuEmail, password); err != nil {
			return true, fmt.Errorf("save password: %w", err)
		}
		uiOK("Password updated in the keychain")
		syncNeeded = true
	}

	if field != "password" && field != "signers" {
		if err := config.Save(cfg); err != nil {
			return true, fmt.Errorf("save config: %w", err)
		}
		uiOK("Saved")
	}
	if syncNeeded && cfg.GithubFork != "" {
		offerSync(cfg, password)
	}
	fmt.Println()
	return false, nil
}

// offerSync pushes changed settings to the GitHub backup, explaining why.
func offerSync(cfg *config.Config, password string) {
	sync := true
	if err := newForm(huh.NewGroup(huh.NewConfirm().
		Title("Update the GitHub backup too?").
		Description("It keeps using the old settings until it's synced.").
		Affirmative("Sync now").Negative("Later (woffux sync)").Value(&sync))).Run(); err != nil || !sync {
		uiWarn("GitHub still has the old settings — run woffux sync when ready.")
		return
	}
	if err := checkGhInstalled(); err != nil {
		uiWarn("Couldn't sync: %s", err)
		return
	}
	if password == "" {
		pw, err := config.GetPassword(cfg.WoffuEmail)
		if err != nil {
			uiErr("Couldn't read the password: %s", err)
			return
		}
		password = pw
	}
	var syncErr error
	spinner.New().Title("Syncing to GitHub…").Action(func() { syncErr = syncGitHubConfig(cfg, password) }).Run()
	if syncErr != nil {
		uiErr("GitHub sync failed: %s", syncErr)
		return
	}
	uiOK("GitHub backup updated")
}

func init() {
	configCmd.AddCommand(configEditCmd)
}

func scheduleSummary(s config.Schedule) string {
	days, signs := scheduleStats(s)
	if days == 0 {
		return "all days off"
	}
	dayLabel := "days"
	if days == 1 {
		dayLabel = "day"
	}
	return fmt.Sprintf("%d %s, %d signs", days, dayLabel, signs)
}
