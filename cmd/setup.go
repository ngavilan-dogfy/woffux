package cmd

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/huh/spinner"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/ngavilan-dogfy/woffuk-cli/internal/config"
	"github.com/ngavilan-dogfy/woffuk-cli/internal/geocode"
	gh "github.com/ngavilan-dogfy/woffuk-cli/internal/github"
	"github.com/ngavilan-dogfy/woffuk-cli/internal/woffu"
)

var (
	sTitle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205")).MarginBottom(1)
	sOk      = lipgloss.NewStyle().Foreground(lipgloss.Color("82")).SetString("✓")
	sInfo    = lipgloss.NewStyle().Foreground(lipgloss.Color("39")).SetString("→")
	sWarn    = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).SetString("!")
	sCoord   = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	sDim     = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	sBold    = lipgloss.NewStyle().Bold(true)
)

var errNoSelection = fmt.Errorf("no selection")

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Interactive setup wizard",
	RunE:  runSetup,
}

func runSetup(cmd *cobra.Command, args []string) error {
	fmt.Println(sTitle.Render("woffuk setup"))

	// Check gh is installed
	if err := checkGhInstalled(); err != nil {
		return err
	}

	// Load existing config for pre-filling (if any)
	existing, _ := config.Load()

	// ── Step 1: Login ──────────────────────────────────────────────

	email, password, company, companyURL, profile, err := loginFlow(existing)
	if err != nil {
		return err
	}
	_ = company

	fmt.Printf("  %s Logged in as %s\n", sOk, sBold.Render(profile.FullName))
	fmt.Printf("  %s %s — %s, %s\n", sInfo, profile.CompanyName, profile.DepartmentName, profile.JobTitle)
	fmt.Printf("  %s Office: %s\n\n", sInfo, profile.OfficeName)

	// ── Step 3: Resolve office coordinates ─────────────────────────

	var officeLat, officeLon float64

	if profile.OfficeLatitude != nil && profile.OfficeLongitude != nil {
		// Woffu has the coordinates
		officeLat = *profile.OfficeLatitude
		officeLon = *profile.OfficeLongitude
		fmt.Printf("  %s Office coordinates from Woffu: %s\n\n",
			sOk, sCoord.Render(fmt.Sprintf("%.4f, %.4f", officeLat, officeLon)))
	} else {
		// Geocode the office name
		fmt.Printf("  %s Office coordinates not in Woffu, searching...\n", sWarn)

		var results []geocode.Result
		spinner.New().
			Title(fmt.Sprintf("Geocoding \"%s\"...", profile.OfficeName)).
			Action(func() {
				results, _ = geocode.Search(profile.OfficeName, 5)
			}).
			Run()

		if len(results) > 0 {
			lat, lon, err := pickFromResults(results, "Office location")
			if err != nil {
				return err
			}
			officeLat, officeLon = lat, lon
		} else {
			// Fallback: Google Maps or manual
			lat, lon, err := locationPickerWithMap("Office location", 0, 0)
			if err != nil {
				return err
			}
			officeLat, officeLon = lat, lon
		}

		time.Sleep(time.Second) // Nominatim rate limit
	}

	// ── Step 4: Home location ──────────────────────────────────────

	homeLat, homeLon, err := locationPickerWithMap("Home location", officeLat, officeLon)
	if err != nil {
		return err
	}

	// ── Step 5: Schedule ───────────────────────────────────────────

	schedule, tz, err := scheduleWizard()
	if err != nil {
		return err
	}

	// ── Step 6: Telegram ───────────────────────────────────────────

	telegramCfg, err := telegramSetup()
	if err != nil {
		return err
	}

	// ── Step 7: Save ───────────────────────────────────────────────

	cfg := &config.Config{
		WoffuURL:        "https://app.woffu.com/api",
		WoffuCompanyURL: companyURL,
		WoffuEmail:      email,
		Latitude:        officeLat,
		Longitude:       officeLon,
		HomeLatitude:    homeLat,
		HomeLongitude:   homeLon,
		Timezone:        tz,
		Schedule:        schedule,
		Telegram:        telegramCfg,
	}

	var saveErr, keyErr error
	spinner.New().
		Title("Saving...").
		Action(func() {
			saveErr = config.Save(cfg)
			keyErr = config.SetPassword(email, password)
		}).
		Run()

	if saveErr != nil {
		return fmt.Errorf("could not save config: %w", saveErr)
	}
	if keyErr != nil {
		fmt.Printf("  %s Could not save password to keychain: %s\n", sWarn, keyErr)
		fmt.Printf("     You may need to enter it again next time.\n\n")
	} else {
		fmt.Printf("  %s Config saved\n", sOk)
		fmt.Printf("  %s Password in keychain\n\n", sOk)
	}

	// ── Step 8: GitHub ─────────────────────────────────────────────

	var wantGitHub bool
	huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Set up GitHub Actions?").
				Description("Fork, configure secrets, enable auto-signing").
				Affirmative("Yes").
				Negative("Skip").
				Value(&wantGitHub),
		),
	).Run()

	if wantGitHub {
		var forkName string
		var ghErr error
		spinner.New().
			Title("Setting up GitHub...").
			Action(func() {
				forkName, ghErr = gh.ForkAndSetup(cfg, password)
			}).
			Run()

		if ghErr != nil {
			fmt.Printf("  %s GitHub setup failed: %s\n", sWarn, ghErr)
		} else {
			cfg.GithubFork = forkName
			config.Save(cfg)
			fmt.Printf("  %s Fork: %s\n", sOk, forkName)
			fmt.Printf("  %s Secrets + workflows configured\n", sOk)
		}
	}

	// ── Done ───────────────────────────────────────────────────────

	fmt.Println()
	fmt.Println(sTitle.Render("All set!"))
	fmt.Printf("  %s %s — %s\n", sInfo, sBold.Render(profile.FullName), profile.CompanyName)
	fmt.Printf("  %s Office: %.4f, %.4f\n", sInfo, officeLat, officeLon)
	fmt.Printf("  %s Home:   %.4f, %.4f\n", sInfo, homeLat, homeLon)
	fmt.Println()
	printSetupSchedule(schedule)
	fmt.Println()
	fmt.Printf("  Run %s to open the dashboard.\n\n", sBold.Render("woffuk"))

	return nil
}

func pickFromResults(results []geocode.Result, title string) (float64, float64, error) {
	if len(results) == 1 {
		r := results[0]
		var confirm bool
		huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title(fmt.Sprintf("Is this your %s?", strings.ToLower(title))).
					Description(fmt.Sprintf("%s  %s", r.DisplayName, sCoord.Render(fmt.Sprintf("(%.4f, %.4f)", r.Lat, r.Lon)))).
					Affirmative("Yes").
					Negative("Search manually").
					Value(&confirm),
			),
		).Run()

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
	huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[int]().
				Title(title).
				Options(options...).
				Value(&choice),
		),
	).Run()

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
		err := huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[string]().
					Title(title).
					Options(
						huh.NewOption("Paste a Google Maps URL", "gmaps"),
						huh.NewOption("Enter coordinates manually", "manual"),
					).
					Value(&method),
			),
		).Run()
		if err != nil {
			return 0, 0, err
		}

		switch method {
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
	huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Open Google Maps?").
				Affirmative("Open").
				Negative("I have the URL").
				Value(&openGmaps),
		),
	).Run()

	if openGmaps {
		openURL("https://www.google.com/maps")
		fmt.Printf("  %s Opened. Find your location and copy the URL.\n\n", sInfo)
	}

	for {
		var url string
		err := huh.NewForm(
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

	err := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Latitude").
				Placeholder("41.353186").
				Value(&latStr).
				Validate(func(s string) error {
					if _, err := strconv.ParseFloat(s, 64); err != nil {
						return fmt.Errorf("enter a valid number")
					}
					return nil
				}),
			huh.NewInput().
				Title("Longitude").
				Placeholder("2.144802").
				Value(&lonStr).
				Validate(func(s string) error {
					if _, err := strconv.ParseFloat(s, 64); err != nil {
						return fmt.Errorf("enter a valid number")
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

func scheduleWizard() (config.Schedule, string, error) {
	sIn := lipgloss.NewStyle().Foreground(lipgloss.Color("82"))
	sOut := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))

	zone, _ := time.Now().Zone()

	// Load existing presets
	existing, _ := config.Load()

	var mode string
	presetOptions := []huh.Option[string]{
		huh.NewOption(
			fmt.Sprintf("Standard split (8.5h)   %s 08:30  %s 13:30  %s 14:15  %s 17:30",
				sIn.Render("IN"), sOut.Render("OUT"), sIn.Render("IN"), sOut.Render("OUT")),
			"standard"),
		huh.NewOption(
			fmt.Sprintf("Intensive (6h)          %s 08:00  %s 14:00",
				sIn.Render("IN"), sOut.Render("OUT")),
			"intensive"),
		huh.NewOption(
			fmt.Sprintf("Morning shift (7h)      %s 07:00  %s 14:00",
				sIn.Render("IN"), sOut.Render("OUT")),
			"morning"),
		huh.NewOption(
			fmt.Sprintf("Flexible (8h)           %s 09:00  %s 14:00  %s 15:00  %s 18:00",
				sIn.Render("IN"), sOut.Render("OUT"), sIn.Render("IN"), sOut.Render("OUT")),
			"flexible"),
	}

	// Add saved presets
	if existing != nil {
		for name := range existing.SavedSchedules {
			presetOptions = append(presetOptions,
				huh.NewOption(fmt.Sprintf("Saved: %s", sBold.Render(name)), "saved:"+name))
		}
	}

	presetOptions = append(presetOptions,
		huh.NewOption("Custom — pick days and define blocks", "custom"))

	err := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Auto-sign schedule").
				Options(presetOptions...).
				Value(&mode),
		),
	).Run()
	if err != nil {
		return config.Schedule{}, "", err
	}

	var schedule config.Schedule

	switch {
	case mode == "standard":
		schedule = makeSchedule(
			dayWith("08:30", "13:30", "14:15", "17:30"),
			dayWith("08:00", "15:00"))
	case mode == "intensive":
		schedule = makeScheduleAll(dayWith("08:00", "14:00"))
	case mode == "morning":
		schedule = makeScheduleAll(dayWith("07:00", "14:00"))
	case mode == "flexible":
		schedule = makeSchedule(
			dayWith("09:00", "14:00", "15:00", "18:00"),
			dayWith("08:00", "15:00"))
	case strings.HasPrefix(mode, "saved:"):
		name := strings.TrimPrefix(mode, "saved:")
		if existing != nil {
			if s, ok := existing.SavedSchedules[name]; ok {
				schedule = s
			}
		}
	case mode == "custom":
		schedule, err = customScheduleWizard(sIn, sOut)
		if err != nil {
			return config.Schedule{}, "", err
		}
	}

	// Show summary
	fmt.Println()
	printScheduleVisual(schedule, sIn, sOut)
	fmt.Println()

	// Offer to save as preset
	var saveName string
	huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Save as preset? (name or Enter to skip)").
				Placeholder("summer").
				Value(&saveName),
		),
	).Run()

	if saveName != "" && existing != nil {
		existing.SaveSchedulePreset(saveName, schedule)
		existing.ActiveSchedule = saveName
		config.Save(existing)
		fmt.Printf("  %s Saved as \"%s\"\n\n", sOk, saveName)
	}

	return schedule, zone, nil
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

		err := huh.NewForm(
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
	huh.NewForm(
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
	).Run()

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
				if len(s) != 5 || s[2] != ':' {
					return fmt.Errorf("format: HH:MM")
				}
				return nil
			}))
	}

	err := huh.NewForm(huh.NewGroup(fields...)).Run()
	if err != nil {
		return config.DaySchedule{}, err
	}

	var entries []config.ScheduleEntry
	for _, t := range times {
		if t != "" {
			entries = append(entries, config.ScheduleEntry{Time: t})
		}
	}

	return config.DaySchedule{Enabled: true, Times: entries}, nil
}

func makeSchedule(monThu, fri config.DaySchedule) config.Schedule {
	return config.Schedule{
		Monday: monThu, Tuesday: monThu, Wednesday: monThu, Thursday: monThu,
		Friday: fri,
	}
}

func makeScheduleAll(day config.DaySchedule) config.Schedule {
	return config.Schedule{
		Monday: day, Tuesday: day, Wednesday: day, Thursday: day, Friday: day,
	}
}

func dayWith(times ...string) config.DaySchedule {
	entries := make([]config.ScheduleEntry, len(times))
	for i, t := range times {
		entries[i] = config.ScheduleEntry{Time: t}
	}
	return config.DaySchedule{Enabled: true, Times: entries}
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
		fmt.Printf("  woffuk needs %s to set up auto-signing via GitHub Actions.\n\n", sBold.Render("gh"))

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
		return fmt.Errorf("gh CLI required — install it and run 'woffuk setup' again")
	}

	// Check if gh is authenticated
	authOut, authErr := exec.Command("gh", "auth", "status").CombinedOutput()
	if authErr != nil {
		fmt.Println()
		fmt.Printf("  %s GitHub CLI is installed but not authenticated.\n\n", sErr.Render("✗"))
		fmt.Printf("  Run: %s\n\n", sBold.Render("gh auth login"))

		var doLogin bool
		huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title("Run 'gh auth login' now?").
					Affirmative("Yes").
					Negative("I'll do it later").
					Value(&doLogin),
			),
		).Run()

		if doLogin {
			loginCmd := exec.Command("gh", "auth", "login")
			loginCmd.Stdin = os.Stdin
			loginCmd.Stdout = os.Stdout
			loginCmd.Stderr = os.Stderr
			if err := loginCmd.Run(); err != nil {
				return fmt.Errorf("gh auth login failed — try manually and run 'woffuk setup' again")
			}
			fmt.Println()
		} else {
			return fmt.Errorf("run 'gh auth login' first, then 'woffuk setup'")
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

	err := huh.NewForm(
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
	huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Open @BotFather in browser?").
				Affirmative("Open").
				Negative("I already have a token").
				Value(&openBotFather),
		),
	).Run()

	if openBotFather {
		openURL("https://t.me/BotFather")
		fmt.Printf("  %s Opened in browser. Create the bot and come back.\n\n", sInfo)
	}

	var token string
	err = huh.NewForm(
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
	huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Open @userinfobot in browser?").
				Affirmative("Open").
				Negative("I already have my ID").
				Value(&openUserInfo),
		),
	).Run()

	if openUserInfo {
		openURL("https://t.me/userinfobot")
		fmt.Printf("  %s Opened in browser. Get your ID and come back.\n\n", sInfo)
	}

	var chatID string
	err = huh.NewForm(
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
		fmt.Printf("     Check your token and chat ID. You can reconfigure later in ~/.woffuk.yaml\n\n")
	} else {
		fmt.Printf("  %s Test message sent! Check your Telegram.\n\n", sOk)
	}

	return testCfg, nil
}

func sendTestTelegram(cfg config.TelegramConfig) error {
	// Reuse the notify package
	body := fmt.Sprintf(`{"chat_id":"%s","text":"✅ woffuk connected! You'll receive notifications here."}`, cfg.ChatID)
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

	err = huh.NewForm(
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
		).Title("Login to Woffu"),
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
			err = huh.NewForm(
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
			err = huh.NewForm(
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
			err = huh.NewForm(
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
			huh.NewForm(
				huh.NewGroup(
					huh.NewConfirm().Title("Retry?").Affirmative("Yes").Negative("Quit").Value(&retry),
				),
			).Run()
			if !retry {
				err = fmt.Errorf("login cancelled")
				return
			}

		default:
			fmt.Printf("  %s Login failed: %s\n\n", sErr.Render("✗"), authErr.Error())
			var retry bool
			huh.NewForm(
				huh.NewGroup(
					huh.NewConfirm().Title("Try again from scratch?").Affirmative("Yes").Negative("Quit").Value(&retry),
				),
			).Run()
			if !retry {
				err = fmt.Errorf("login cancelled")
				return
			}
			// Reset and ask everything again
			email, password = "", ""
			err = huh.NewForm(
				huh.NewGroup(
					huh.NewInput().Title("Email").Placeholder("you@company.com").Value(&email).
						Validate(func(s string) error {
							if !strings.Contains(s, "@") {
								return fmt.Errorf("enter a valid email")
							}
							return nil
						}),
					huh.NewInput().Title("Password").EchoMode(huh.EchoModePassword).Value(&password),
				).Title("Login to Woffu"),
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

func printSetupSchedule(s config.Schedule) {
	sIn := lipgloss.NewStyle().Foreground(lipgloss.Color("82"))
	sOut := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	printScheduleVisual(s, sIn, sOut)
}
