package cmd

import (
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"runtime"
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

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Interactive setup wizard",
	RunE:  runSetup,
}

func runSetup(cmd *cobra.Command, args []string) error {
	fmt.Println(sTitle.Render("woffuk setup"))

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
			// Fallback: manual pick
			lat, lon, err := locationPickerWithMap("Office location", 41.39, 2.17)
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

	spinner.New().
		Title("Saving...").
		Action(func() {
			config.Save(cfg)
			config.SetPassword(email, password)
		}).
		Run()

	fmt.Printf("  %s Config saved\n", sOk)
	fmt.Printf("  %s Password in keychain\n\n", sOk)

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
		return locationPicker(title)
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
		return locationPicker(title)
	}

	r := results[choice]
	fmt.Printf("  %s %s\n\n", sOk, sCoord.Render(fmt.Sprintf("%.4f, %.4f", r.Lat, r.Lon)))
	return r.Lat, r.Lon, nil
}

func locationPickerWithMap(title string, defaultLat, defaultLon float64) (float64, float64, error) {
	var useMap bool
	huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(title).
				Description("Pick on a map in your browser, or search by text").
				Affirmative("Open map").
				Negative("Search by text").
				Value(&useMap),
		),
	).Run()

	if useMap {
		fmt.Printf("  %s Opening map in browser...\n", sInfo)
		result, err := geocode.PickFromMap(title, defaultLat, defaultLon)
		if err != nil {
			return 0, 0, err
		}
		fmt.Printf("  %s %s\n\n", sOk, sCoord.Render(fmt.Sprintf("%.6f, %.6f", result.Lat, result.Lon)))
		return result.Lat, result.Lon, nil
	}

	return locationPicker(title)
}

func locationPicker(title string) (float64, float64, error) {
	for {
		var query string
		huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Search").
					Description("Street, building, city...").
					Placeholder("Carrer Vistula 12 Segur de Calafell").
					Value(&query).
					Validate(func(s string) error {
						if len(s) < 3 {
							return fmt.Errorf("enter at least 3 characters")
						}
						return nil
					}),
			).Title(title),
		).Run()

		var results []geocode.Result
		var searchErr error

		spinner.New().
			Title("Searching...").
			Action(func() {
				results, searchErr = geocode.Search(query, 5)
			}).
			Run()

		if searchErr != nil {
			fmt.Printf("  %s %s\n\n", sWarn, searchErr)
			continue
		}

		if len(results) == 0 {
			fmt.Printf("  %s No results. Try with more details.\n\n", sWarn)
			continue
		}

		lat, lon, err := pickFromResults(results, title)
		if err != nil {
			return 0, 0, err
		}
		return lat, lon, nil
	}
}

func scheduleWizard() (config.Schedule, string, error) {
	sIn := lipgloss.NewStyle().Foreground(lipgloss.Color("82"))   // green
	sOut := lipgloss.NewStyle().Foreground(lipgloss.Color("196")) // red

	zone, _ := time.Now().Zone()

	var preset string
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Auto-sign schedule").
				Options(
					huh.NewOption(
						fmt.Sprintf("Standard split (8.5h)   %s 08:30  %s 13:30  %s 14:15  %s 17:30",
							sIn.Render("IN"), sOut.Render("OUT"), sIn.Render("IN"), sOut.Render("OUT")),
						"standard",
					),
					huh.NewOption(
						fmt.Sprintf("Intensive (6h)          %s 08:00  %s 14:00",
							sIn.Render("IN"), sOut.Render("OUT")),
						"intensive",
					),
					huh.NewOption(
						fmt.Sprintf("Morning shift (7h)      %s 07:00  %s 14:00",
							sIn.Render("IN"), sOut.Render("OUT")),
						"morning",
					),
					huh.NewOption(
						fmt.Sprintf("Flexible (8h)           %s 09:00  %s 14:00  %s 15:00  %s 18:00",
							sIn.Render("IN"), sOut.Render("OUT"), sIn.Render("IN"), sOut.Render("OUT")),
						"flexible",
					),
					huh.NewOption("Custom — define your own blocks", "custom"),
				).
				Value(&preset),
		),
	).Run()
	if err != nil {
		return config.Schedule{}, "", err
	}

	var schedule config.Schedule

	switch preset {
	case "standard":
		schedule = config.Schedule{
			Monday:    dayWith("08:30", "13:30", "14:15", "17:30"),
			Tuesday:   dayWith("08:30", "13:30", "14:15", "17:30"),
			Wednesday: dayWith("08:30", "13:30", "14:15", "17:30"),
			Thursday:  dayWith("08:30", "13:30", "14:15", "17:30"),
			Friday:    dayWith("08:00", "15:00"),
		}
	case "intensive":
		schedule = config.Schedule{
			Monday:    dayWith("08:00", "14:00"),
			Tuesday:   dayWith("08:00", "14:00"),
			Wednesday: dayWith("08:00", "14:00"),
			Thursday:  dayWith("08:00", "14:00"),
			Friday:    dayWith("08:00", "14:00"),
		}
	case "morning":
		schedule = config.Schedule{
			Monday:    dayWith("07:00", "14:00"),
			Tuesday:   dayWith("07:00", "14:00"),
			Wednesday: dayWith("07:00", "14:00"),
			Thursday:  dayWith("07:00", "14:00"),
			Friday:    dayWith("07:00", "14:00"),
		}
	case "flexible":
		schedule = config.Schedule{
			Monday:    dayWith("09:00", "14:00", "15:00", "18:00"),
			Tuesday:   dayWith("09:00", "14:00", "15:00", "18:00"),
			Wednesday: dayWith("09:00", "14:00", "15:00", "18:00"),
			Thursday:  dayWith("09:00", "14:00", "15:00", "18:00"),
			Friday:    dayWith("08:00", "15:00"),
		}
	case "custom":
		schedule, err = customScheduleBlocks(sIn, sOut)
		if err != nil {
			return config.Schedule{}, "", err
		}
	}

	// Show summary
	fmt.Println()
	printScheduleVisual(schedule, sIn, sOut)
	fmt.Println()

	return schedule, zone, nil
}

func customScheduleBlocks(sIn, sOut lipgloss.Style) (config.Schedule, error) {
	fmt.Println()
	fmt.Printf("  Define time blocks as %s/%s pairs.\n", sIn.Render("IN"), sOut.Render("OUT"))
	fmt.Printf("  Each pair = one clock in + one clock out.\n\n")

	// Mon-Thu block
	fmt.Println(sBold.Render("  Monday — Thursday"))
	monThu, err := editBlocks(sIn, sOut, "08:30", "13:30", "14:15", "17:30")
	if err != nil {
		return config.Schedule{}, err
	}

	// Friday
	fmt.Println(sBold.Render("  Friday"))
	fri, err := editBlocks(sIn, sOut, "08:00", "15:00")
	if err != nil {
		return config.Schedule{}, err
	}

	return config.Schedule{
		Monday:    monThu,
		Tuesday:   monThu,
		Wednesday: monThu,
		Thursday:  monThu,
		Friday:    fri,
	}, nil
}

func editBlocks(sIn, sOut lipgloss.Style, defaults ...string) (config.DaySchedule, error) {
	// defaults come in pairs: in, out, in, out...
	numBlocks := len(defaults) / 2
	if numBlocks == 0 {
		numBlocks = 1
	}

	times := make([]string, len(defaults))
	copy(times, defaults)

	// Ensure we have at least 2 values
	for len(times) < 2 {
		times = append(times, "")
	}

	fields := make([]huh.Field, 0, len(times))
	for i := range times {
		label := sIn.Render("▶ IN ")
		if i%2 == 1 {
			label = sOut.Render("■ OUT")
		}
		blockNum := (i / 2) + 1
		title := fmt.Sprintf("%s  Block %d", label, blockNum)

		idx := i // capture
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

	group := huh.NewGroup(fields...)
	err := huh.NewForm(group).Run()
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

func parseDayInput(input string) config.DaySchedule {
	input = strings.TrimSpace(input)
	if input == "" {
		return config.DaySchedule{Enabled: true, Times: []config.ScheduleEntry{
			{Time: "08:30"}, {Time: "13:30"}, {Time: "14:15"}, {Time: "17:30"},
		}}
	}
	if strings.ToLower(input) == "off" {
		return config.DaySchedule{Enabled: false}
	}

	parts := strings.Split(input, ",")
	var times []config.ScheduleEntry
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t != "" {
			times = append(times, config.ScheduleEntry{Time: t})
		}
	}
	return config.DaySchedule{Enabled: true, Times: times}
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
