package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/ngavilan-dogfy/woffuk-cli/internal/config"
	"github.com/ngavilan-dogfy/woffuk-cli/internal/geocode"
	gh "github.com/ngavilan-dogfy/woffuk-cli/internal/github"
)

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Interactive setup wizard",
	RunE: func(cmd *cobra.Command, args []string) error {
		reader := bufio.NewReader(os.Stdin)

		fmt.Println("=== woffuk setup ===")
		fmt.Println()

		// Email
		email := prompt(reader, "Woffu email")

		// Password (masked)
		fmt.Print("Woffu password: ")
		passBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
		if err != nil {
			return fmt.Errorf("read password: %w", err)
		}
		password := string(passBytes)
		fmt.Println()

		// Company URL
		company := prompt(reader, "Company name (e.g. dogfydiet)")
		companyURL := "https://" + company + ".woffu.com"
		fmt.Printf("  Company URL: %s\n", companyURL)

		// Office address
		fmt.Println()
		officeAddr := prompt(reader, "Office address")
		fmt.Println("  Geocoding...")
		officeResult, err := geocode.Geocode(officeAddr)
		if err != nil {
			return fmt.Errorf("geocode office: %w", err)
		}
		fmt.Printf("  Found: %s\n", officeResult.DisplayName)
		fmt.Printf("  Coordinates: %.4f, %.4f\n", officeResult.Lat, officeResult.Lon)

		// Nominatim rate limit: 1 req/sec
		time.Sleep(time.Second)

		// Home address
		fmt.Println()
		homeAddr := prompt(reader, "Home address")
		fmt.Println("  Geocoding...")
		homeResult, err := geocode.Geocode(homeAddr)
		if err != nil {
			return fmt.Errorf("geocode home: %w", err)
		}
		fmt.Printf("  Found: %s\n", homeResult.DisplayName)
		fmt.Printf("  Coordinates: %.4f, %.4f\n", homeResult.Lat, homeResult.Lon)

		// Schedule configuration
		fmt.Println()
		fmt.Println("=== Auto-sign schedule ===")
		fmt.Println()

		// Detect timezone
		zone, _ := time.Now().Zone()
		tz := prompt(reader, fmt.Sprintf("Timezone [%s]", zone))
		if tz == "" {
			tz = zone
		}

		fmt.Println()
		fmt.Println("Default schedule:")
		fmt.Println("  Mon-Thu: 08:30, 13:30, 14:15, 17:30")
		fmt.Println("  Fri:     08:00, 15:00")
		fmt.Println()

		useDefault := prompt(reader, "Use default schedule? [Y/n]")
		schedule := config.DefaultSchedule()

		if strings.ToLower(useDefault) == "n" {
			fmt.Println()
			fmt.Println("Enter times as HH:MM separated by commas, or 'off' to disable a day.")
			fmt.Println()
			schedule.Monday = promptDay(reader, "Monday", schedule.Monday)
			schedule.Tuesday = promptDay(reader, "Tuesday", schedule.Tuesday)
			schedule.Wednesday = promptDay(reader, "Wednesday", schedule.Wednesday)
			schedule.Thursday = promptDay(reader, "Thursday", schedule.Thursday)
			schedule.Friday = promptDay(reader, "Friday", schedule.Friday)
		}

		// Telegram notifications (optional)
		fmt.Println()
		fmt.Println("=== Telegram notifications (optional) ===")
		fmt.Println()
		telegramToken := prompt(reader, "Telegram Bot Token (or Enter to skip)")
		var telegramCfg config.TelegramConfig
		if telegramToken != "" {
			telegramChatID := prompt(reader, "Telegram Chat ID")
			telegramCfg = config.TelegramConfig{
				BotToken: telegramToken,
				ChatID:   telegramChatID,
			}
			fmt.Println("  Telegram notifications enabled")
		} else {
			fmt.Println("  Skipped — you can configure later in ~/.woffuk.yaml")
		}

		// Save config
		cfg := &config.Config{
			WoffuURL:        "https://app.woffu.com/api",
			WoffuCompanyURL: companyURL,
			WoffuEmail:      email,
			Latitude:        officeResult.Lat,
			Longitude:       officeResult.Lon,
			HomeLatitude:    homeResult.Lat,
			HomeLongitude:   homeResult.Lon,
			Timezone:        tz,
			Schedule:        schedule,
			Telegram:        telegramCfg,
		}

		if err := config.Save(cfg); err != nil {
			return fmt.Errorf("save config: %w", err)
		}
		fmt.Println("\nConfig saved to ~/.woffuk.yaml")

		// Save password to keychain
		if err := config.SetPassword(email, password); err != nil {
			return fmt.Errorf("save password to keychain: %w", err)
		}
		fmt.Println("Password saved to OS keychain")

		// GitHub setup
		fmt.Println()
		forkAnswer := prompt(reader, "Fork repo and configure GitHub Actions? [Y/n]")
		if forkAnswer == "" || strings.ToLower(forkAnswer) == "y" {
			fmt.Println("  Forking repo...")
			forkName, err := gh.ForkAndSetup(cfg, password)
			if err != nil {
				return fmt.Errorf("github setup: %w", err)
			}
			cfg.GithubFork = forkName
			if err := config.Save(cfg); err != nil {
				return fmt.Errorf("save config with fork: %w", err)
			}
			fmt.Printf("  Fork: %s\n", forkName)
			fmt.Println("  Secrets configured")
			fmt.Println("  Workflows generated and pushed")
			fmt.Println("  GitHub Actions enabled")
		}

		fmt.Println()
		fmt.Println("Setup complete! Auto-signing is active.")
		printSetupSchedule(schedule)
		fmt.Println()
		fmt.Println("Commands:")
		fmt.Println("  woffuk              Open dashboard")
		fmt.Println("  woffuk status       Today's status")
		fmt.Println("  woffuk events       Available events")
		fmt.Println("  woffuk sign         Sign manually")
		fmt.Println("  woffuk schedule     View schedule")
		fmt.Println("  woffuk schedule edit  Edit schedule")

		return nil
	},
}

func prompt(reader *bufio.Reader, label string) string {
	fmt.Printf("%s: ", label)
	input, _ := reader.ReadString('\n')
	return strings.TrimSpace(input)
}

func promptDay(reader *bufio.Reader, name string, current config.DaySchedule) config.DaySchedule {
	var currentTimes []string
	for _, t := range current.Times {
		currentTimes = append(currentTimes, t.Time)
	}
	defaultStr := strings.Join(currentTimes, ", ")

	fmt.Printf("  %s [%s]: ", name, defaultStr)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)

	if input == "" {
		return current
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
		{"Mon", s.Monday},
		{"Tue", s.Tuesday},
		{"Wed", s.Wednesday},
		{"Thu", s.Thursday},
		{"Fri", s.Friday},
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
