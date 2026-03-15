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

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "View or edit individual settings",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		sLabel := lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Width(20)
		sVal := lipgloss.NewStyle().Bold(true)
		sMask := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
		sIn := lipgloss.NewStyle().Foreground(lipgloss.Color("82"))
		sOut := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))

		fmt.Println()
		fmt.Println(sLabel.Render("Email") + sVal.Render(cfg.WoffuEmail))
		fmt.Println(sLabel.Render("Password") + sMask.Render("••••••••"))
		fmt.Println(sLabel.Render("Company") + sVal.Render(cfg.WoffuCompanyURL))
		fmt.Println(sLabel.Render("Office") + sVal.Render(fmt.Sprintf("%.6f, %.6f", cfg.Latitude, cfg.Longitude)))
		fmt.Println(sLabel.Render("Home") + sVal.Render(fmt.Sprintf("%.6f, %.6f", cfg.HomeLatitude, cfg.HomeLongitude)))
		fmt.Println(sLabel.Render("Timezone") + sVal.Render(cfg.Timezone))
		fmt.Println(sLabel.Render("GitHub fork") + sVal.Render(cfg.GithubFork))

		tg := "not configured"
		if cfg.Telegram.BotToken != "" {
			tg = "enabled"
		}
		fmt.Println(sLabel.Render("Telegram") + sVal.Render(tg))

		fmt.Println()
		fmt.Println(sLabel.Render("Schedule"))
		printScheduleVisual(cfg.Schedule, sIn, sOut)

		fmt.Println()
		fmt.Printf("  Edit with: %s\n\n", lipgloss.NewStyle().Bold(true).Render("woffuk config edit"))

		return nil
	},
}

var configEditCmd = &cobra.Command{
	Use:   "edit",
	Short: "Edit a specific setting",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		var field string
		err = huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[string]().
					Title("What do you want to change?").
					Options(
						huh.NewOption(fmt.Sprintf("Email           %s", cfg.WoffuEmail), "email"),
						huh.NewOption("Password        ••••••••", "password"),
						huh.NewOption(fmt.Sprintf("Office coords   %.4f, %.4f", cfg.Latitude, cfg.Longitude), "office"),
						huh.NewOption(fmt.Sprintf("Home coords     %.4f, %.4f", cfg.HomeLatitude, cfg.HomeLongitude), "home"),
						huh.NewOption(fmt.Sprintf("Schedule        %s", scheduleSummary(cfg.Schedule)), "schedule"),
						huh.NewOption(fmt.Sprintf("Telegram        %s", telegramSummary(cfg.Telegram)), "telegram"),
						huh.NewOption(fmt.Sprintf("GitHub fork     %s", cfg.GithubFork), "github"),
					).
					Value(&field),
			),
		).Run()
		if err != nil {
			return err
		}

		changed := false

		switch field {
		case "email":
			err = huh.NewForm(
				huh.NewGroup(
					huh.NewInput().Title("Email").Value(&cfg.WoffuEmail),
				),
			).Run()
			if err == nil {
				cfg.WoffuCompanyURL = "https://" + extractCompany(cfg.WoffuEmail) + ".woffu.com"
				changed = true
			}

		case "password":
			var pw string
			err = huh.NewForm(
				huh.NewGroup(
					huh.NewInput().Title("New password").EchoMode(huh.EchoModePassword).Value(&pw),
				),
			).Run()
			if err == nil && pw != "" {
				config.SetPassword(cfg.WoffuEmail, pw)
				fmt.Printf("  %s Password updated in keychain\n", sOk)
			}

		case "office":
			lat, lon, err := locationPickerWithMap("Office location", 0, 0)
			if err == nil {
				cfg.Latitude = lat
				cfg.Longitude = lon
				changed = true
			}

		case "home":
			lat, lon, err := locationPickerWithMap("Home location", 0, 0)
			if err == nil {
				cfg.HomeLatitude = lat
				cfg.HomeLongitude = lon
				changed = true
			}

		case "schedule":
			schedule, tz, err := scheduleWizard()
			if err == nil {
				cfg.Schedule = schedule
				cfg.Timezone = tz
				changed = true
			}

		case "telegram":
			tgCfg, err := telegramSetup()
			if err == nil {
				cfg.Telegram = tgCfg
				changed = true
			}

		case "github":
			fmt.Printf("\n  Current fork: %s\n", cfg.GithubFork)
			fmt.Printf("  To re-sync secrets and workflows, run: %s\n\n",
				lipgloss.NewStyle().Bold(true).Render("woffuk sync"))
			return nil
		}

		if changed {
			if err := config.Save(cfg); err != nil {
				return fmt.Errorf("save config: %w", err)
			}
			fmt.Printf("  %s Config saved\n", sOk)

			// Offer to sync with GitHub
			if cfg.GithubFork != "" {
				var sync bool
				huh.NewForm(
					huh.NewGroup(
						huh.NewConfirm().
							Title("Sync changes to GitHub?").
							Description("Updates secrets and workflows in your fork").
							Affirmative("Yes").
							Negative("Skip").
							Value(&sync),
					),
				).Run()

				if sync {
					pw, _ := config.GetPassword(cfg.WoffuEmail)
					var syncErr error
					spinner.New().
						Title("Syncing...").
						Action(func() {
							gh.SyncSecrets(cfg, pw)
							syncErr = gh.SyncWorkflows(cfg)
						}).
						Run()
					if syncErr != nil {
						fmt.Printf("  %s Sync failed: %s\n", sWarn, syncErr)
					} else {
						fmt.Printf("  %s GitHub synced\n", sOk)
					}
				}
			}
		}

		return nil
	},
}

func init() {
	configCmd.AddCommand(configEditCmd)
}

func scheduleSummary(s config.Schedule) string {
	count := 0
	for _, d := range []config.DaySchedule{s.Monday, s.Tuesday, s.Wednesday, s.Thursday, s.Friday} {
		if d.Enabled {
			count++
		}
	}
	if count == 0 {
		return "all days off"
	}
	times := 0
	if s.Monday.Enabled {
		times = len(s.Monday.Times)
	}
	return fmt.Sprintf("%d days, %d signs/day", count, times)
}

func telegramSummary(t config.TelegramConfig) string {
	if t.BotToken != "" {
		return "enabled"
	}
	return "not configured"
}

