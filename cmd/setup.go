package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/huh/spinner"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/ngavilan-dogfy/woffuk-cli/internal/config"
	"github.com/ngavilan-dogfy/woffuk-cli/internal/geocode"
	gh "github.com/ngavilan-dogfy/woffuk-cli/internal/github"
)

var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205")).MarginBottom(1)
	successIcon = lipgloss.NewStyle().Foreground(lipgloss.Color("82")).SetString("✓")
	infoIcon    = lipgloss.NewStyle().Foreground(lipgloss.Color("39")).SetString("→")
	coordStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
)

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Interactive setup wizard",
	RunE:  runSetup,
}

func runSetup(cmd *cobra.Command, args []string) error {
	fmt.Println(titleStyle.Render("woffuk setup"))

	// --- Credentials ---

	var email, password, company string

	credentialsForm := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Woffu email").
				Placeholder("you@company.com").
				Value(&email).
				Validate(func(s string) error {
					if !strings.Contains(s, "@") {
						return fmt.Errorf("enter a valid email")
					}
					return nil
				}),

			huh.NewInput().
				Title("Woffu password").
				EchoMode(huh.EchoModePassword).
				Value(&password).
				Validate(func(s string) error {
					if len(s) < 1 {
						return fmt.Errorf("password cannot be empty")
					}
					return nil
				}),

			huh.NewInput().
				Title("Company name").
				Description("Your Woffu subdomain (e.g. dogfydiet)").
				Placeholder("yourcompany").
				Value(&company).
				Validate(func(s string) error {
					if len(s) < 1 {
						return fmt.Errorf("company name cannot be empty")
					}
					return nil
				}),
		).Title("Woffu credentials"),
	)

	if err := credentialsForm.Run(); err != nil {
		return err
	}

	companyURL := "https://" + company + ".woffu.com"
	fmt.Printf("  %s %s\n\n", infoIcon, companyURL)

	// --- Office location ---

	officeLat, officeLon, err := locationPicker("Office location")
	if err != nil {
		return err
	}

	time.Sleep(time.Second) // Nominatim rate limit

	// --- Home location ---

	homeLat, homeLon, err := locationPicker("Home location")
	if err != nil {
		return err
	}

	// --- Schedule ---

	var useDefaultSchedule bool

	huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Use default schedule?").
				Description("Mon-Thu: 08:30, 13:30, 14:15, 17:30 | Fri: 08:00, 15:00").
				Affirmative("Yes").
				Negative("Customize").
				Value(&useDefaultSchedule),
		).Title("Auto-sign schedule"),
	).Run()

	schedule := config.DefaultSchedule()
	zone, _ := time.Now().Zone()
	tz := zone

	if !useDefaultSchedule {
		schedule, tz, err = customScheduleForm(zone)
		if err != nil {
			return err
		}
	}

	// --- Telegram ---

	var wantTelegram bool
	var telegramToken, telegramChatID string

	huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Enable Telegram notifications?").
				Description("Get a message every time you clock in/out").
				Affirmative("Yes").
				Negative("Skip").
				Value(&wantTelegram),
		).Title("Notifications"),
	).Run()

	var telegramCfg config.TelegramConfig
	if wantTelegram {
		huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Bot Token").
					Description("Create one at @BotFather on Telegram").
					Placeholder("123456:ABC-DEF...").
					Value(&telegramToken),
				huh.NewInput().
					Title("Chat ID").
					Description("Get yours at @userinfobot on Telegram").
					Placeholder("987654321").
					Value(&telegramChatID),
			).Title("Telegram"),
		).Run()

		telegramCfg = config.TelegramConfig{
			BotToken: telegramToken,
			ChatID:   telegramChatID,
		}
		fmt.Printf("  %s Telegram notifications enabled\n\n", successIcon)
	}

	// --- Save ---

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

	err = spinner.New().
		Title("Saving config...").
		Action(func() {
			config.Save(cfg)
			config.SetPassword(email, password)
		}).
		Run()
	if err != nil {
		return err
	}

	fmt.Printf("  %s Config saved to ~/.woffuk.yaml\n", successIcon)
	fmt.Printf("  %s Password saved to OS keychain\n\n", successIcon)

	// --- GitHub ---

	var wantGitHub bool

	huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Set up GitHub Actions for auto-signing?").
				Description("Forks this repo, sets secrets, enables workflows").
				Affirmative("Yes").
				Negative("Skip").
				Value(&wantGitHub),
		).Title("GitHub Actions"),
	).Run()

	if wantGitHub {
		var forkName string
		err = spinner.New().
			Title("Setting up GitHub...").
			Action(func() {
				forkName, err = gh.ForkAndSetup(cfg, password)
			}).
			Run()
		if err != nil {
			return fmt.Errorf("github setup: %w", err)
		}

		cfg.GithubFork = forkName
		config.Save(cfg)

		fmt.Printf("  %s Fork: %s\n", successIcon, forkName)
		fmt.Printf("  %s Secrets configured\n", successIcon)
		fmt.Printf("  %s GitHub Actions enabled\n", successIcon)
	}

	// --- Done ---

	fmt.Println()
	fmt.Println(titleStyle.Render("Setup complete!"))
	printSetupSchedule(schedule)
	fmt.Println()
	fmt.Printf("  Run %s to open the dashboard.\n\n", lipgloss.NewStyle().Bold(true).Render("woffuk"))

	return nil
}

func locationPicker(title string) (float64, float64, error) {
	for {
		var query string

		huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Search for a place").
					Description("Street, building, city...").
					Placeholder("Passeig Zona Franca 28 Barcelona").
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

		err := spinner.New().
			Title("Searching...").
			Action(func() {
				results, searchErr = geocode.Search(query, 5)
			}).
			Run()
		if err != nil {
			return 0, 0, err
		}

		if searchErr != nil {
			fmt.Printf("  Error: %s. Try again.\n\n", searchErr)
			continue
		}

		if len(results) == 0 {
			fmt.Println("  No results. Try adding more details (city, country).")
			fmt.Println()
			continue
		}

		// Build options for the select
		options := make([]huh.Option[int], 0, len(results)+1)
		for i, r := range results {
			options = append(options, huh.NewOption(
				fmt.Sprintf("%s  %s", r.DisplayName, coordStyle.Render(fmt.Sprintf("(%.4f, %.4f)", r.Lat, r.Lon))),
				i,
			))
		}
		options = append(options, huh.NewOption("None of these — search again", -1))

		var choice int

		huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[int]().
					Title("Pick a location").
					Options(options...).
					Value(&choice),
			),
		).Run()

		if choice == -1 {
			continue
		}

		r := results[choice]
		fmt.Printf("  %s %s\n", successIcon, r.DisplayName)
		fmt.Printf("  %s %.6f, %.6f\n\n", infoIcon, r.Lat, r.Lon)
		return r.Lat, r.Lon, nil
	}
}

func customScheduleForm(defaultTz string) (config.Schedule, string, error) {
	var monTimes, tueTimes, wedTimes, thuTimes, friTimes, tz string

	tz = defaultTz

	huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Timezone").
				Value(&tz),
			huh.NewInput().
				Title("Monday").
				Description("HH:MM separated by commas, or 'off'").
				Placeholder("08:30, 13:30, 14:15, 17:30").
				Value(&monTimes),
			huh.NewInput().
				Title("Tuesday").
				Placeholder("08:30, 13:30, 14:15, 17:30").
				Value(&tueTimes),
			huh.NewInput().
				Title("Wednesday").
				Placeholder("08:30, 13:30, 14:15, 17:30").
				Value(&wedTimes),
			huh.NewInput().
				Title("Thursday").
				Placeholder("08:30, 13:30, 14:15, 17:30").
				Value(&thuTimes),
			huh.NewInput().
				Title("Friday").
				Placeholder("08:00, 15:00").
				Value(&friTimes),
		).Title("Custom schedule"),
	).Run()

	return config.Schedule{
		Monday:    parseDayInput(monTimes),
		Tuesday:   parseDayInput(tueTimes),
		Wednesday: parseDayInput(wedTimes),
		Thursday:  parseDayInput(thuTimes),
		Friday:    parseDayInput(friTimes),
	}, tz, nil
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

func printSetupSchedule(s config.Schedule) {
	days := []struct {
		name string
		day  config.DaySchedule
	}{
		{"Mon", s.Monday}, {"Tue", s.Tuesday}, {"Wed", s.Wednesday},
		{"Thu", s.Thursday}, {"Fri", s.Friday},
	}
	for _, d := range days {
		if !d.day.Enabled {
			fmt.Printf("  %s: off\n", d.name)
			continue
		}
		var times []string
		for _, t := range d.day.Times {
			times = append(times, t.Time)
		}
		fmt.Printf("  %s: %s\n", d.name, strings.Join(times, ", "))
	}
}
