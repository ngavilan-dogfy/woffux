package cmd

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/huh/spinner"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/ngavilan-dogfy/woffux/internal/agent"
	"github.com/ngavilan-dogfy/woffux/internal/config"
	"github.com/ngavilan-dogfy/woffux/internal/geocode"
	gh "github.com/ngavilan-dogfy/woffux/internal/github"
	"github.com/ngavilan-dogfy/woffux/internal/timing"
	"github.com/ngavilan-dogfy/woffux/internal/woffu"
)

var (
	sOk    = lipgloss.NewStyle().Foreground(lipgloss.Color("82")).SetString("✓")
	sInfo  = lipgloss.NewStyle().Foreground(lipgloss.Color("39")).SetString("→")
	sWarn  = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).SetString("!")
	sCoord = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	sDim   = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	sBold  = lipgloss.NewStyle().Bold(true)
)

var errNoSelection = fmt.Errorf("no selection")

// setupOpensDashboard makes setup offer the dashboard at the end; off when
// setup was started from the dashboard itself.
var setupOpensDashboard = true

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Interactive setup wizard",
	RunE:  runSetup,
}

func runSetup(cmd *cobra.Command, args []string) error {
	existing, _ := config.Load()
	printWelcome(existing != nil)

	// ── 1. Woffu account ───────────────────────────────────────────

	stepHeader(1, "Sign in with the same email and password you use on Woffu. The password is stored in your system keychain, never in a file.")
	email, password, _, companyURL, profile, err := loginFlow(existing)
	if err != nil {
		return err
	}
	fmt.Printf("  %s Signed in as %s\n", stIn.Render("✓"), stBold.Render(profile.FullName))
	fmt.Printf("    %s\n", stFaint.Render(strings.Trim(strings.Join([]string{profile.CompanyName, profile.DepartmentName, profile.JobTitle}, " · "), " ·")))

	// ── 2. Locations ───────────────────────────────────────────────

	stepHeader(2, "Woffu records where each sign comes from. woffux uses your office on office days and your home on telework days — it reads which is which from your Woffu calendar.")
	officeLat, officeLon, err := resolveOffice(profile, existing)
	if err != nil {
		return err
	}
	defaultHomeLat, defaultHomeLon := 0.0, 0.0
	if existing != nil && coordsConfigured(existing.HomeLatitude, existing.HomeLongitude) {
		defaultHomeLat, defaultHomeLon = existing.HomeLatitude, existing.HomeLongitude
	}
	homeLat, homeLon, err := locationPickerWithMap("Home location (for telework days)", defaultHomeLat, defaultHomeLon)
	if err != nil {
		return err
	}

	// ── 3. Work schedule ───────────────────────────────────────────

	stepHeader(3, "When do you clock in and out? Holidays, vacation and absences from your Woffu calendar are skipped automatically — you only describe a normal week.")
	scheduleResult, err := scheduleWizard()
	if err != nil {
		return err
	}

	// ── 4. Natural timing ──────────────────────────────────────────

	stepHeader(4, "Signing at exactly 08:30:00 every day looks robotic. Natural timing moves each sign a few minutes — in a bit early, out a bit late — so your day is never shorter than planned. Every signer agrees on the same moment.")
	var currentTiming timing.Settings
	if existing != nil {
		currentTiming = existing.Timing
	}
	timingCfg, err := timingWizard(currentTiming, scheduleResult.Schedule)
	if err != nil {
		return err
	}

	// ── 5. Who signs ───────────────────────────────────────────────

	stepHeader(5, "Something has to be awake to sign. This Mac signs on time whenever it's on; GitHub Actions works while it sleeps, but its timers often run late. Using both is safest — they never double-sign.")
	signers, err := signerChoice(existing)
	if err != nil {
		return err
	}

	// ── 6. Notifications ───────────────────────────────────────────

	stepHeader(6, "Optional: a Telegram message each time woffux signs (or can't), so you're never left wondering.")
	telegramCfg := config.TelegramConfig{}
	if existing != nil {
		telegramCfg = existing.Telegram
	}
	if telegramCfg.BotToken == "" {
		if telegramCfg, err = telegramSetup(); err != nil {
			return err
		}
	} else {
		keep := true
		if err := newForm(huh.NewGroup(huh.NewConfirm().
			Title("Keep Telegram notifications?").Affirmative("Keep").Negative("Change").Value(&keep))).Run(); err != nil {
			return err
		}
		if !keep {
			if telegramCfg, err = telegramSetup(); err != nil {
				return err
			}
		}
	}

	// ── Save ───────────────────────────────────────────────────────

	cfg := &config.Config{
		WoffuURL:        "https://app.woffu.com/api",
		WoffuCompanyURL: companyURL,
		WoffuEmail:      email,
		Latitude:        officeLat,
		Longitude:       officeLon,
		HomeLatitude:    homeLat,
		HomeLongitude:   homeLon,
		Telegram:        telegramCfg,
		Timing:          timingCfg,
	}
	if existing != nil {
		cfg.GithubFork = existing.GithubFork
		cfg.SavedSchedules = config.CloneSchedulePresets(existing.SavedSchedules)
		cfg.ActiveSchedule = existing.ActiveSchedule
		cfg.RandomDelaySecs = existing.RandomDelaySecs
		cfg.Seasons = existing.Seasons
	}
	if err := applyScheduleWizardResult(cfg, scheduleResult); err != nil {
		return err
	}

	var saveErr, keyErr error
	spinner.New().Title("Saving…").Action(func() {
		saveErr = config.Save(cfg)
		keyErr = config.SetPassword(email, password)
	}).Run()
	if saveErr != nil {
		return fmt.Errorf("could not save config: %w", saveErr)
	}
	fmt.Println()
	fmt.Printf("  %s Settings saved to ~/.woffux.yaml\n", stIn.Render("✓"))
	if keyErr != nil {
		fmt.Printf("  %s Could not save the password to the keychain: %s\n", stBad.Render("!"), keyErr)
	} else {
		fmt.Printf("  %s Password in the keychain\n", stIn.Render("✓"))
	}

	applySigners(cfg, password, signers)
	offerClaudeSkill()
	printSetupSummary(cfg, profile, signers)

	if !setupOpensDashboard {
		return nil
	}
	open := true
	if err := newForm(huh.NewGroup(huh.NewConfirm().
		Title("Open the dashboard now?").Affirmative("Open it").Negative("Later").Value(&open))).Run(); err != nil {
		return nil
	}
	if open {
		return runDashboard()
	}
	fmt.Printf("  Run %s any time to open it.\n\n", stBold.Render("woffux"))
	return nil
}

// resolveOffice takes the office coordinates from Woffu, or finds them.
func resolveOffice(profile *woffu.UserProfile, existing *config.Config) (float64, float64, error) {
	if profile.OfficeLatitude != nil && profile.OfficeLongitude != nil {
		lat, lon := *profile.OfficeLatitude, *profile.OfficeLongitude
		fmt.Printf("  %s Office %s found in Woffu %s\n", stIn.Render("✓"), stBold.Render(profile.OfficeName), stFaint.Render(fmt.Sprintf("(%.4f, %.4f)", lat, lon)))
		return lat, lon, nil
	}
	fmt.Printf("  %s Woffu doesn't know where %s is — let's find it.\n", stOut.Render("!"), stBold.Render(orDefaultStr(profile.OfficeName, "your office")))
	var results []geocode.Result
	spinner.New().Title(fmt.Sprintf("Searching \"%s\"…", profile.OfficeName)).
		Action(func() { results, _ = geocode.Search(profile.OfficeName, 5) }).Run()
	defer time.Sleep(time.Second) // Nominatim rate limit
	if len(results) > 0 {
		return pickFromResults(results, "Office location")
	}
	defaultLat, defaultLon := 0.0, 0.0
	if existing != nil && coordsConfigured(existing.Latitude, existing.Longitude) {
		defaultLat, defaultLon = existing.Latitude, existing.Longitude
	}
	return locationPickerWithMap("Office location", defaultLat, defaultLon)
}

// signerPlan is who signs automatically.
type signerPlan struct {
	mac    bool
	github bool
}

func (s signerPlan) String() string {
	switch {
	case s.mac && s.github:
		return "This Mac, with GitHub as backup"
	case s.mac:
		return "This Mac"
	case s.github:
		return "GitHub Actions"
	}
	return "nobody — you sign by hand (s in the dashboard)"
}

func signerChoice(existing *config.Config) (signerPlan, error) {
	choice := "github"
	var opts []huh.Option[string]
	if agent.Supported() {
		choice = "mac"
		if existing != nil && existing.GithubFork != "" {
			choice = "both"
		}
		opts = append(opts,
			huh.NewOption("This Mac "+stFaint.Render("— recommended · on time while it's awake"), "mac"),
			huh.NewOption("This Mac + GitHub backup "+stFaint.Render("— also covers a sleeping Mac · needs gh · public fork"), "both"),
		)
	}
	opts = append(opts,
		huh.NewOption("Only GitHub Actions "+stFaint.Render("— no computer needed · may sign late · public fork"), "github"),
		huh.NewOption("Nobody, I'll sign by hand "+stFaint.Render("— woffux just shows your status"), "manual"),
	)
	if err := newForm(huh.NewGroup(huh.NewSelect[string]().
		Title("Who should sign for you?").Options(opts...).Value(&choice))).Run(); err != nil {
		return signerPlan{}, err
	}
	return signerPlan{mac: choice == "mac" || choice == "both", github: choice == "both" || choice == "github"}, nil
}

// applySigners installs/removes the local agent and sets up GitHub.
func applySigners(cfg *config.Config, password string, plan signerPlan) {
	if agent.Supported() {
		switch {
		case plan.mac:
			if err := agent.Install(); err != nil {
				fmt.Printf("  %s Couldn't start the local agent: %s\n", stBad.Render("!"), err)
			} else {
				fmt.Printf("  %s This Mac will sign for you %s\n", stIn.Render("✓"), stFaint.Render("(log: "+agent.LogPath()+")"))
			}
		case agent.Installed():
			if err := agent.Uninstall(); err == nil {
				fmt.Printf("  %s Local agent removed\n", stIn.Render("✓"))
			}
		}
	}

	if !plan.github {
		if cfg.GithubFork != "" {
			if enabled, err := gh.IsAutoSignEnabled(cfg.GithubFork); err == nil && enabled {
				if err := gh.DisableAutoSign(cfg.GithubFork); err == nil {
					fmt.Printf("  %s GitHub signing paused on %s\n", stIn.Render("✓"), cfg.GithubFork)
				}
			}
		}
		return
	}
	if err := checkGhInstalled(); err != nil {
		fmt.Printf("  %s Skipped GitHub: %s — run %s later.\n", stOut.Render("!"), err, stBold.Render("woffux setup"))
		return
	}
	if cfg.GithubFork != "" {
		var ghErr error
		spinner.New().Title("Updating GitHub Actions…").Action(func() {
			ghErr = syncGitHubConfig(cfg, password)
			if ghErr == nil {
				ghErr = gh.EnableAndRefreshAutoSign(cfg)
			}
		}).Run()
		if ghErr != nil {
			fmt.Printf("  %s GitHub update failed: %s\n", stBad.Render("!"), ghErr)
			return
		}
		fmt.Printf("  %s GitHub backup up to date on %s\n", stIn.Render("✓"), cfg.GithubFork)
		return
	}
	var forkName string
	var ghErr error
	spinner.New().Title("Creating your private signer on GitHub…").Action(func() {
		forkName, ghErr = gh.ForkAndSetup(cfg, password)
	}).Run()
	if ghErr != nil {
		fmt.Printf("  %s GitHub setup failed: %s\n", stBad.Render("!"), ghErr)
		return
	}
	cfg.GithubFork = forkName
	if err := config.Save(cfg); err != nil {
		fmt.Printf("  %s Couldn't save the fork name: %s\n", stBad.Render("!"), err)
	}
	fmt.Printf("  %s GitHub signer ready on %s\n", stIn.Render("✓"), forkName)
}

func offerClaudeSkill() {
	if !claudeCodeDetected() {
		return
	}
	install := true
	if err := newForm(huh.NewGroup(huh.NewConfirm().
		Title("Add /woffux to Claude Code?").
		Description("Ask Claude to check your status, sign or request days off.").
		Affirmative("Add it").Negative("Skip").Value(&install))).Run(); err != nil || !install {
		return
	}
	if err := installClaudeSkill(); err != nil {
		fmt.Printf("  %s Skill install failed: %s\n", stBad.Render("!"), err)
		return
	}
	fmt.Printf("  %s Claude Code skill installed — use %s\n", stIn.Render("✓"), stBold.Render("/woffux"))
}

// printSetupSummary ends setup with everything in one place and the very
// next automatic sign, so the user knows exactly what will happen.
func printSetupSummary(cfg *config.Config, profile *woffu.UserProfile, signers signerPlan) {
	row := func(k, v string) string { return stFaint.Render(fmt.Sprintf("%-10s", k)) + " " + v }
	lines := []string{
		stIn.Bold(true).Render("✓ All set") + stText.Render(", "+firstName(profile.FullName)),
		"",
		row("Account", stText.Render(profile.FullName)+stFaint.Render(" · "+profile.CompanyName)),
		row("Week", stText.Render(weekOneLine(cfg.Schedule))),
	}
	if len(cfg.Seasons.Periods) > 0 {
		p := cfg.Seasons.Periods[0]
		if s, ok := cfg.SavedSchedules[p.Preset]; ok {
			lines = append(lines, row("Summer", stText.Render(dayMonth(p.From)+" → "+dayMonth(p.To)+": "+config.ScheduleText(s))))
		}
	}
	timingText := "exact"
	if cfg.Timing.Active() {
		timingText = cfg.Timing.Describe()
	}
	lines = append(lines,
		row("Timing", stText.Render(timingText)),
		row("Signs", stText.Render(signers.String())),
	)
	if cfg.Telegram.BotToken != "" {
		lines = append(lines, row("Telegram", stText.Render("on")))
	}
	if when, dir, ok := nextAutomaticSign(cfg, time.Now()); ok && (signers.mac || signers.github) {
		st := stIn
		if dir == "OUT" {
			st = stOut
		}
		lines = append(lines, "", row("Next sign", st.Bold(true).Render(dir)+stBold.Render(" "+when.Format("Mon 2 Jan · 15:04"))+stFaint.Render("  (holidays and days off are skipped)")))
	}
	fmt.Println()
	fmt.Println(lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(obIn).Padding(1, 2).MarginLeft(2).Render(strings.Join(lines, "\n")))
	fmt.Println()
}

// nextAutomaticSign finds the next scheduled sign moment (ignoring the
// Woffu calendar, which setup doesn't load).
func nextAutomaticSign(cfg *config.Config, now time.Time) (time.Time, string, bool) {
	for d := 0; d < 14; d++ {
		date := now.AddDate(0, 0, d)
		probe := *cfg
		probe.ApplySeasons(date)
		day := probe.Schedule.Day(date.Weekday())
		if !day.Enabled {
			continue
		}
		var ev []timing.Event
		for i, e := range day.Times {
			m, _ := minutesOf(e.Time)
			ev = append(ev, timing.Event{Minute: m, In: i%2 == 0})
		}
		for i, t := range cfg.Timing.Targets(date, ev) {
			if t.After(now) {
				dir := "IN"
				if !ev[i].In {
					dir = "OUT"
				}
				return t, dir, true
			}
		}
	}
	return time.Time{}, "", false
}

func firstName(full string) string {
	f := strings.Fields(full)
	if len(f) == 0 {
		return "friend"
	}
	r := []rune(strings.ToLower(f[0]))
	return strings.ToUpper(string(r[0])) + string(r[1:])
}

func pickFromResults(results []geocode.Result, title string) (float64, float64, error) {
	if len(results) == 1 {
		r := results[0]
		var confirm bool
		if err := newForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title(fmt.Sprintf("Is this your %s?", strings.ToLower(title))).
					Description(fmt.Sprintf("%s  %s", r.DisplayName, sCoord.Render(fmt.Sprintf("(%.4f, %.4f)", r.Lat, r.Lon)))).
					Affirmative("Yes").
					Negative("Search manually").
					Value(&confirm),
			),
		).Run(); err != nil {
			return 0, 0, err
		}

		if confirm {
			fmt.Printf("  %s %s\n\n", sOk, sCoord.Render(fmt.Sprintf("%.4f, %.4f", r.Lat, r.Lon)))
			return r.Lat, r.Lon, nil
		}
		return 0, 0, errNoSelection
	}

	options := make([]huh.Option[int], 0, len(results)+1)
	for i, r := range results {
		options = append(options, huh.NewOption(
			fmt.Sprintf("%s  %s", r.DisplayName, sCoord.Render(fmt.Sprintf("(%.4f, %.4f)", r.Lat, r.Lon))),
			i,
		))
	}
	options = append(options, huh.NewOption(sDim.Render("None — search manually"), -1))

	var choice int
	if err := newForm(
		huh.NewGroup(
			huh.NewSelect[int]().
				Title(title).
				Options(options...).
				Value(&choice),
		),
	).Run(); err != nil {
		return 0, 0, err
	}

	if choice == -1 {
		return 0, 0, errNoSelection
	}

	r := results[choice]
	fmt.Printf("  %s %s\n\n", sOk, sCoord.Render(fmt.Sprintf("%.4f, %.4f", r.Lat, r.Lon)))
	return r.Lat, r.Lon, nil
}

func locationPickerWithMap(title string, defaultLat, defaultLon float64) (float64, float64, error) {
	for {
		var method string
		options := []huh.Option[string]{
			huh.NewOption("Paste a Google Maps URL", "gmaps"),
			huh.NewOption("Enter coordinates manually", "manual"),
		}
		if coordsConfigured(defaultLat, defaultLon) {
			options = append([]huh.Option[string]{
				huh.NewOption(fmt.Sprintf("Keep current coordinates (%.4f, %.4f)", defaultLat, defaultLon), "current"),
			}, options...)
		}
		err := newForm(
			huh.NewGroup(
				huh.NewSelect[string]().
					Title(title).
					Options(options...).
					Value(&method),
			),
		).Run()
		if err != nil {
			return 0, 0, err
		}

		switch method {
		case "current":
			return defaultLat, defaultLon, nil
		case "gmaps":
			lat, lon, err := googleMapsURLPicker(title)
			if err == nil {
				return lat, lon, nil
			}
			if !errors.Is(err, errNoSelection) {
				// Real error (not just "try again"), but don't crash — loop back
			}

		case "manual":
			lat, lon, err := manualCoordsPicker(title)
			if err == nil {
				return lat, lon, nil
			}
		}
	}
}

func googleMapsURLPicker(title string) (float64, float64, error) {
	fmt.Println()
	fmt.Printf("  %s Open Google Maps, find your location, and copy the URL from the browser.\n", sInfo)
	fmt.Println()

	var openGmaps bool
	if err := newForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Open Google Maps?").
				Affirmative("Open").
				Negative("I have the URL").
				Value(&openGmaps),
		),
	).Run(); err != nil {
		return 0, 0, err
	}

	if openGmaps {
		openURL("https://www.google.com/maps")
		fmt.Printf("  %s Opened. Find your location and copy the URL.\n\n", sInfo)
	}

	for {
		var url string
		err := newForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Paste Google Maps URL").
					Placeholder("https://www.google.com/maps/place/...").
					Value(&url),
			),
		).Run()
		if err != nil {
			return 0, 0, err
		}

		lat, lon, err := geocode.ParseGoogleMapsURL(url)
		if err != nil {
			fmt.Printf("  %s %s\n\n", lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("✗"), err)
			continue
		}

		fmt.Printf("  %s %s\n\n", sOk, sCoord.Render(fmt.Sprintf("%.6f, %.6f", lat, lon)))
		return lat, lon, nil
	}
}

func manualCoordsPicker(title string) (float64, float64, error) {
	var latStr, lonStr string

	err := newForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Latitude").
				Placeholder("41.353186").
				Value(&latStr).
				Validate(func(s string) error {
					lat, err := strconv.ParseFloat(s, 64)
					if err != nil {
						return fmt.Errorf("enter a valid number")
					}
					if lat < -90 || lat > 90 {
						return fmt.Errorf("latitude must be between -90 and 90")
					}
					return nil
				}),
			huh.NewInput().
				Title("Longitude").
				Placeholder("2.144802").
				Value(&lonStr).
				Validate(func(s string) error {
					lon, err := strconv.ParseFloat(s, 64)
					if err != nil {
						return fmt.Errorf("enter a valid number")
					}
					if lon < -180 || lon > 180 {
						return fmt.Errorf("longitude must be between -180 and 180")
					}
					return nil
				}),
		).Title(title),
	).Run()
	if err != nil {
		return 0, 0, err
	}

	lat, _ := strconv.ParseFloat(latStr, 64)
	lon, _ := strconv.ParseFloat(lonStr, 64)

	fmt.Printf("  %s %s\n\n", sOk, sCoord.Render(fmt.Sprintf("%.6f, %.6f", lat, lon)))
	return lat, lon, nil
}

func coordsConfigured(lat, lon float64) bool {
	return lat != 0 || lon != 0
}

func scheduleWizardSavedPresetMessage(result scheduleWizardResult) string {
	if result.SavedPreset == "" {
		return ""
	}
	return fmt.Sprintf("  %s Saved preset \"%s\"\n", sOk, result.SavedPreset)
}

func customScheduleWizard(sIn, sOut lipgloss.Style) (config.Schedule, error) {
	schedule := config.Schedule{}

	dayNames := []string{"Monday", "Tuesday", "Wednesday", "Thursday", "Friday"}
	dayPtrs := []*config.DaySchedule{
		&schedule.Monday, &schedule.Tuesday, &schedule.Wednesday,
		&schedule.Thursday, &schedule.Friday,
	}

	// Initialize all as disabled
	for _, dp := range dayPtrs {
		dp.Enabled = false
	}

	remaining := make([]bool, 5)
	for i := range remaining {
		remaining[i] = true
	}

	for {
		// Show which days still need configuring
		var pendingDays []string
		for i, r := range remaining {
			if r {
				pendingDays = append(pendingDays, dayNames[i])
			}
		}
		if len(pendingDays) == 0 {
			break
		}

		// Multi-select days for this group
		var selectedDays []int
		options := make([]huh.Option[int], 0)
		for i, r := range remaining {
			if r {
				options = append(options, huh.NewOption(dayNames[i], i))
			}
		}

		err := newForm(
			huh.NewGroup(
				huh.NewMultiSelect[int]().
					Title("Select days to configure together").
					Description(fmt.Sprintf("%d days remaining", len(pendingDays))).
					Options(options...).
					Value(&selectedDays),
			),
		).Run()
		if err != nil {
			return config.Schedule{}, err
		}

		if len(selectedDays) == 0 {
			// Mark remaining as off
			for i, r := range remaining {
				if r {
					dayPtrs[i].Enabled = false
					remaining[i] = false
				}
			}
			break
		}

		// Build label for this group
		var groupNames []string
		for _, idx := range selectedDays {
			groupNames = append(groupNames, dayNames[idx][:3])
		}

		// Ask for blocks
		fmt.Printf("\n  %s\n", sBold.Render(strings.Join(groupNames, ", ")))
		daySchedule, err := editBlocks(sIn, sOut, "08:30", "13:30", "14:15", "17:30")
		if err != nil {
			return config.Schedule{}, err
		}

		// Apply to selected days
		for _, idx := range selectedDays {
			*dayPtrs[idx] = daySchedule
			remaining[idx] = false
		}

		// Check if any remaining
		anyLeft := false
		for _, r := range remaining {
			if r {
				anyLeft = true
				break
			}
		}
		if !anyLeft {
			break
		}

		fmt.Println()
	}

	return schedule, nil
}

func editBlocks(sIn, sOut lipgloss.Style, defaults ...string) (config.DaySchedule, error) {
	// First ask how many blocks
	var numBlocksStr string
	if err := newForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("How many time blocks?").
				Options(
					huh.NewOption(fmt.Sprintf("1 block  (%s → %s)", sIn.Render("IN"), sOut.Render("OUT")), "1"),
					huh.NewOption(fmt.Sprintf("2 blocks (%s → %s → %s → %s)", sIn.Render("IN"), sOut.Render("OUT"), sIn.Render("IN"), sOut.Render("OUT")), "2"),
					huh.NewOption("Day off", "off"),
				).
				Value(&numBlocksStr),
		),
	).Run(); err != nil {
		return config.DaySchedule{}, err
	}

	if numBlocksStr == "off" {
		return config.DaySchedule{Enabled: false}, nil
	}

	numBlocks := 1
	if numBlocksStr == "2" {
		numBlocks = 2
	}

	times := make([]string, numBlocks*2)
	// Set defaults
	for i := range times {
		if i < len(defaults) {
			times[i] = defaults[i]
		}
	}

	fields := make([]huh.Field, 0, len(times))
	for i := range times {
		label := sIn.Render("▶ IN ")
		if i%2 == 1 {
			label = sOut.Render("■ OUT")
		}
		blockNum := (i / 2) + 1
		title := fmt.Sprintf("%s  Block %d", label, blockNum)

		idx := i
		fields = append(fields, huh.NewInput().
			Title(title).
			Placeholder("HH:MM").
			Value(&times[idx]).
			Validate(func(s string) error {
				if s == "" {
					return fmt.Errorf("enter a time like 08:30")
				}
				return validateClockTime(s)
			}))
	}

	err := newForm(huh.NewGroup(fields...)).Run()
	if err != nil {
		return config.DaySchedule{}, err
	}

	var entries []config.ScheduleEntry
	for _, t := range times {
		if t != "" {
			entries = append(entries, config.ScheduleEntry{Time: t})
		}
	}
	if err := validateScheduleEntries(entries); err != nil {
		return config.DaySchedule{}, err
	}

	return config.DaySchedule{Enabled: true, Times: entries}, nil
}

func validateClockTime(value string) error {
	if len(value) != 5 || value[2] != ':' {
		return fmt.Errorf("format: HH:MM")
	}
	if _, err := time.Parse("15:04", value); err != nil {
		return fmt.Errorf("format: HH:MM")
	}
	return nil
}

func validateScheduleEntries(entries []config.ScheduleEntry) error {
	var previous time.Time
	for i, entry := range entries {
		t, err := time.Parse("15:04", entry.Time)
		if err != nil {
			return fmt.Errorf("invalid time %q", entry.Time)
		}
		if i > 0 && !t.After(previous) {
			return fmt.Errorf("times must be in chronological order")
		}
		previous = t
	}
	return nil
}

func printScheduleVisual(s config.Schedule, sIn, sOut lipgloss.Style) {
	printDayVisual("Mon", s.Monday, sIn, sOut)
	printDayVisual("Tue", s.Tuesday, sIn, sOut)
	printDayVisual("Wed", s.Wednesday, sIn, sOut)
	printDayVisual("Thu", s.Thursday, sIn, sOut)
	printDayVisual("Fri", s.Friday, sIn, sOut)
}

func printDayVisual(name string, day config.DaySchedule, sIn, sOut lipgloss.Style) {
	if !day.Enabled {
		fmt.Printf("  %s  off\n", name)
		return
	}
	fmt.Printf("  %s  ", name)
	for i, t := range day.Times {
		if i%2 == 0 {
			fmt.Printf("%s %s  ", sIn.Render("▶"), t.Time)
		} else {
			fmt.Printf("%s %s  ", sOut.Render("■"), t.Time)
		}
	}
	fmt.Println()
}

// checkGhInstalled verifies gh CLI is available and authenticated.
func checkGhInstalled() error {
	sErr := lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)

	// Check if gh is installed
	_, err := exec.LookPath("gh")
	if err != nil {
		fmt.Println()
		fmt.Printf("  %s GitHub CLI (gh) is not installed.\n\n", sErr.Render("✗"))
		fmt.Printf("  woffux needs %s to set up auto-signing via GitHub Actions.\n\n", sBold.Render("gh"))

		detected := detectOS()

		fmt.Println("  Install it:")
		switch detected {
		case "mac":
			fmt.Printf("    %s\n\n", sBold.Render("brew install gh"))
		case "debian":
			fmt.Printf("    %s\n\n", sBold.Render("sudo apt install gh"))
		case "fedora":
			fmt.Printf("    %s\n\n", sBold.Render("sudo dnf install gh"))
		default:
			fmt.Printf("    %s\n\n", sBold.Render("https://cli.github.com"))
		}

		fmt.Printf("  Then authenticate: %s\n\n", sBold.Render("gh auth login"))
		return fmt.Errorf("gh CLI required — install it and run 'woffux setup' again")
	}

	// Check if gh is authenticated
	authOut, authErr := exec.Command("gh", "auth", "status").CombinedOutput()
	if authErr != nil {
		fmt.Println()
		fmt.Printf("  %s GitHub CLI is installed but not authenticated.\n\n", sErr.Render("✗"))
		fmt.Printf("  Run: %s\n\n", sBold.Render("gh auth login"))

		var doLogin bool
		if err := newForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title("Run 'gh auth login' now?").
					Affirmative("Yes").
					Negative("I'll do it later").
					Value(&doLogin),
			),
		).Run(); err != nil {
			return err
		}

		if doLogin {
			loginCmd := exec.Command("gh", "auth", "login")
			loginCmd.Stdin = os.Stdin
			loginCmd.Stdout = os.Stdout
			loginCmd.Stderr = os.Stderr
			if err := loginCmd.Run(); err != nil {
				return fmt.Errorf("gh auth login failed — try manually and run 'woffux setup' again")
			}
			fmt.Println()
		} else {
			return fmt.Errorf("run 'gh auth login' first, then 'woffux setup'")
		}
	}
	_ = authOut

	fmt.Printf("  %s GitHub CLI ready\n\n", sOk)
	return nil
}

func detectOS() string {
	switch runtime.GOOS {
	case "darwin":
		return "mac"
	case "linux":
		// Try to detect distro
		if data, err := os.ReadFile("/etc/os-release"); err == nil {
			s := string(data)
			if strings.Contains(s, "Ubuntu") || strings.Contains(s, "Debian") {
				return "debian"
			}
			if strings.Contains(s, "Fedora") || strings.Contains(s, "Red Hat") {
				return "fedora"
			}
		}
		return "linux"
	default:
		return runtime.GOOS
	}
}

// telegramSetup guides the user through Telegram bot configuration.
func telegramSetup() (config.TelegramConfig, error) {
	var wantTelegram bool

	err := newForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Enable Telegram notifications?").
				Description("Get a message every time you clock in/out").
				Affirmative("Yes").
				Negative("Skip").
				Value(&wantTelegram),
		),
	).Run()
	if err != nil {
		return config.TelegramConfig{}, err
	}

	if !wantTelegram {
		return config.TelegramConfig{}, nil
	}

	// Step 1: Create bot
	fmt.Println()
	fmt.Printf("  %s Step 1: Create a Telegram bot\n", sBold.Render("1."))
	fmt.Printf("     Open Telegram and search for %s\n", sBold.Render("@BotFather"))
	fmt.Printf("     Send %s and follow the instructions\n", sBold.Render("/newbot"))
	fmt.Printf("     Copy the token it gives you (looks like %s)\n\n", sDim.Render("123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11"))

	var openBotFather bool
	if err := newForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Open @BotFather in browser?").
				Affirmative("Open").
				Negative("I already have a token").
				Value(&openBotFather),
		),
	).Run(); err != nil {
		return config.TelegramConfig{}, err
	}

	if openBotFather {
		openURL("https://t.me/BotFather")
		fmt.Printf("  %s Opened in browser. Create the bot and come back.\n\n", sInfo)
	}

	var token string
	err = newForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Bot Token").
				Placeholder("123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11").
				Value(&token).
				Validate(func(s string) error {
					if !strings.Contains(s, ":") {
						return fmt.Errorf("token should contain a colon (:)")
					}
					return nil
				}),
		),
	).Run()
	if err != nil {
		return config.TelegramConfig{}, err
	}

	// Step 2: Get chat ID
	fmt.Println()
	fmt.Printf("  %s Step 2: Get your Chat ID\n", sBold.Render("2."))
	fmt.Printf("     Open Telegram and search for %s\n", sBold.Render("@userinfobot"))
	fmt.Printf("     Send any message — it will reply with your ID (a number like %s)\n\n", sDim.Render("987654321"))

	var openUserInfo bool
	if err := newForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Open @userinfobot in browser?").
				Affirmative("Open").
				Negative("I already have my ID").
				Value(&openUserInfo),
		),
	).Run(); err != nil {
		return config.TelegramConfig{}, err
	}

	if openUserInfo {
		openURL("https://t.me/userinfobot")
		fmt.Printf("  %s Opened in browser. Get your ID and come back.\n\n", sInfo)
	}

	var chatID string
	err = newForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Chat ID").
				Placeholder("987654321").
				Value(&chatID).
				Validate(func(s string) error {
					if s == "" {
						return fmt.Errorf("chat ID cannot be empty")
					}
					return nil
				}),
		),
	).Run()
	if err != nil {
		return config.TelegramConfig{}, err
	}

	// Step 3: Test it
	fmt.Println()
	testCfg := config.TelegramConfig{BotToken: token, ChatID: chatID}

	var testResult error
	spinner.New().
		Title("Sending test message...").
		Action(func() {
			testResult = sendTestTelegram(testCfg)
		}).
		Run()

	if testResult != nil {
		fmt.Printf("  %s Test failed: %s\n", lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("✗"), testResult)
		fmt.Printf("     Check your token and chat ID. You can reconfigure later in ~/.woffux.yaml\n\n")
	} else {
		fmt.Printf("  %s Test message sent! Check your Telegram.\n\n", sOk)
	}

	return testCfg, nil
}

func sendTestTelegram(cfg config.TelegramConfig) error {
	// Reuse the notify package
	body := fmt.Sprintf(`{"chat_id":"%s","text":"✅ woffux connected! You'll receive notifications here."}`, cfg.ChatID)
	resp, err := http.Post(
		fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", cfg.BotToken),
		"application/json",
		strings.NewReader(body),
	)
	if err != nil {
		return fmt.Errorf("connection error: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 {
		return fmt.Errorf("invalid bot token")
	}
	if resp.StatusCode == 400 {
		return fmt.Errorf("invalid chat ID — make sure you messaged the bot first")
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("Telegram API returned %d", resp.StatusCode)
	}
	return nil
}

func openURL(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	}
	if cmd != nil {
		cmd.Start()
	}
}

// loginFlow handles the full login with retries and error-specific re-prompts.
func loginFlow(existing *config.Config) (email, password, company, companyURL string, profile *woffu.UserProfile, err error) {
	sErr := lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)

	// Pre-fill from existing config
	if existing != nil {
		email = existing.WoffuEmail
	}

	// Initial credentials form
	emailInput := huh.NewInput().
		Title("Email").
		Placeholder("you@company.com").
		Value(&email).
		Validate(func(s string) error {
			if !strings.Contains(s, "@") || !strings.Contains(s, ".") {
				return fmt.Errorf("enter a valid email")
			}
			return nil
		})

	err = newForm(
		huh.NewGroup(
			emailInput,
			huh.NewInput().
				Title("Password").
				EchoMode(huh.EchoModePassword).
				Value(&password).
				Validate(func(s string) error {
					if s == "" {
						return fmt.Errorf("password cannot be empty")
					}
					return nil
				}),
		),
	).Run()
	if err != nil {
		return
	}

	company = extractCompany(email)

	for {
		companyURL = "https://" + company + ".woffu.com"
		client := woffu.NewWoffuClient("https://app.woffu.com/api")
		companyClient := woffu.NewCompanyClient(companyURL)

		var authErr error
		var token string

		spinner.New().
			Title(fmt.Sprintf("Signing in to %s...", company+".woffu.com")).
			Action(func() {
				token, authErr = woffu.Authenticate(client, companyClient, email, password)
				if authErr == nil {
					profile, authErr = woffu.GetUserProfile(companyClient, token)
				}
			}).
			Run()

		// Success
		if authErr == nil {
			return
		}

		// Classify error using typed AuthError
		var ae *woffu.AuthError
		var kind woffu.AuthErrorKind
		if errors.As(authErr, &ae) {
			kind = ae.Kind
		} else {
			kind = woffu.ErrUnknown
		}

		fmt.Println()

		switch kind {
		case woffu.ErrBadEmail:
			fmt.Printf("  %s Email not found: %s\n\n", sErr.Render("✗"), email)
			err = newForm(
				huh.NewGroup(
					huh.NewInput().
						Title("Email").
						Description("Check for typos in your email address").
						Value(&email).
						Validate(func(s string) error {
							if !strings.Contains(s, "@") || !strings.Contains(s, ".") {
								return fmt.Errorf("enter a valid email")
							}
							return nil
						}),
				).Title("Try again"),
			).Run()
			if err != nil {
				return
			}
			company = extractCompany(email)

		case woffu.ErrBadPassword:
			fmt.Printf("  %s Wrong password for %s\n\n", sErr.Render("✗"), email)
			password = ""
			err = newForm(
				huh.NewGroup(
					huh.NewInput().
						Title("Password").
						Description(email).
						EchoMode(huh.EchoModePassword).
						Value(&password).
						Validate(func(s string) error {
							if s == "" {
								return fmt.Errorf("password cannot be empty")
							}
							return nil
						}),
				).Title("Try again"),
			).Run()
			if err != nil {
				return
			}

		case woffu.ErrBadCompany:
			fmt.Printf("  %s Company \"%s\" not found on Woffu\n\n", sErr.Render("✗"), company)
			company = ""
			err = newForm(
				huh.NewGroup(
					huh.NewInput().
						Title("Company subdomain").
						Description("The part before .woffu.com").
						Placeholder("dogfydiet").
						Value(&company).
						Validate(func(s string) error {
							if s == "" {
								return fmt.Errorf("cannot be empty")
							}
							return nil
						}),
				).Title("Try again"),
			).Run()
			if err != nil {
				return
			}

		case woffu.ErrNetwork:
			fmt.Printf("  %s Cannot connect to Woffu. Check your internet connection.\n\n", sErr.Render("✗"))
			var retry bool
			err = newForm(
				huh.NewGroup(
					huh.NewConfirm().Title("Retry?").Affirmative("Yes").Negative("Quit").Value(&retry),
				),
			).Run()
			if err != nil {
				return
			}
			if !retry {
				err = fmt.Errorf("login cancelled")
				return
			}

		default:
			fmt.Printf("  %s Login failed: %s\n\n", sErr.Render("✗"), authErr.Error())
			var retry bool
			err = newForm(
				huh.NewGroup(
					huh.NewConfirm().Title("Try again from scratch?").Affirmative("Yes").Negative("Quit").Value(&retry),
				),
			).Run()
			if err != nil {
				return
			}
			if !retry {
				err = fmt.Errorf("login cancelled")
				return
			}
			// Reset and ask everything again
			email, password = "", ""
			err = newForm(
				huh.NewGroup(
					huh.NewInput().Title("Email").Placeholder("you@company.com").Value(&email).
						Validate(func(s string) error {
							if !strings.Contains(s, "@") || !strings.Contains(s, ".") {
								return fmt.Errorf("enter a valid email")
							}
							return nil
						}),
					huh.NewInput().Title("Password").EchoMode(huh.EchoModePassword).Value(&password).
						Validate(func(s string) error {
							if s == "" {
								return fmt.Errorf("password cannot be empty")
							}
							return nil
						}),
				),
			).Run()
			if err != nil {
				return
			}
			company = extractCompany(email)
		}
	}
}

// extractCompany gets the company subdomain from an email address.
// user@dogfydiet.com → dogfydiet
func extractCompany(email string) string {
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return ""
	}
	domain := parts[1]
	// Remove TLD: dogfydiet.com → dogfydiet
	domainParts := strings.Split(domain, ".")
	if len(domainParts) >= 2 {
		return domainParts[0]
	}
	return domain
}

// claudeCodeDetected returns true if ~/.claude/ exists.
func claudeCodeDetected() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	info, err := os.Stat(filepath.Join(home, ".claude"))
	return err == nil && info.IsDir()
}

// installClaudeSkill copies the embedded skill to ~/.claude/skills/woffux/.
func installClaudeSkill() error {
	dest, err := skillPath()
	if err != nil {
		return err
	}

	data, err := skillFS.ReadFile("skill_data/SKILL.md")
	if err != nil {
		return fmt.Errorf("read embedded skill: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	return os.WriteFile(dest, data, 0644)
}
