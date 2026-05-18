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

		// Auto-sign status
		autoStatus := sMask.Render("not set up")
		if cfg.GithubFork != "" {
			enabled, err := gh.IsAutoSignEnabled(cfg.GithubFork)
			if err == nil {
				if enabled {
					autoStatus = lipgloss.NewStyle().Foreground(lipgloss.Color("82")).Bold(true).Render("active")
				} else {
					autoStatus = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true).Render("disabled")
				}
			}
		}
		fmt.Println(sLabel.Render("Auto-sign") + autoStatus)

		fmt.Println()
		fmt.Println(sLabel.Render("Schedule"))
		printScheduleVisual(cfg.Schedule, sIn, sOut)

		fmt.Println()
		fmt.Printf("  Edit: %s    Auto-sign: %s / %s\n\n",
			lipgloss.NewStyle().Bold(true).Render("woffux config edit"),
			lipgloss.NewStyle().Bold(true).Render("woffux auto on"),
			lipgloss.NewStyle().Bold(true).Render("off"))

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
		syncNeeded := false
		var afterSaveMessage string
		var syncPassword string

		switch field {
		case "email":
			oldEmail := cfg.WoffuEmail
			err = huh.NewForm(
				huh.NewGroup(
					huh.NewInput().
						Title("Email").
						Value(&cfg.WoffuEmail).
						Validate(func(s string) error {
							if extractCompany(s) == "" {
								return fmt.Errorf("enter a valid email")
							}
							return nil
						}),
				),
			).Run()
			if err == nil && cfg.WoffuEmail != oldEmail {
				cfg.WoffuCompanyURL = "https://" + extractCompany(cfg.WoffuEmail) + ".woffu.com"
				if pw, pwErr := config.GetPassword(oldEmail); pwErr == nil {
					_ = config.SetPassword(cfg.WoffuEmail, pw)
				} else {
					fmt.Printf("  %s Password for the new email was not copied. Update it with %s.\n",
						sWarn, sBold.Render("woffux config edit"))
				}
				changed = true
				syncNeeded = true
			}

		case "password":
			var pw string
			err = huh.NewForm(
				huh.NewGroup(
					huh.NewInput().Title("New password").EchoMode(huh.EchoModePassword).Value(&pw),
				),
			).Run()
			if err == nil && pw != "" {
				if err := config.SetPassword(cfg.WoffuEmail, pw); err != nil {
					return fmt.Errorf("save password: %w", err)
				}
				syncPassword = pw
				syncNeeded = true
				fmt.Printf("  %s Password updated in keychain\n", sOk)
			}

		case "office":
			lat, lon, err := locationPickerWithMap("Office location", cfg.Latitude, cfg.Longitude)
			if err == nil {
				cfg.Latitude = lat
				cfg.Longitude = lon
				changed = true
				syncNeeded = true
			}

		case "home":
			lat, lon, err := locationPickerWithMap("Home location", cfg.HomeLatitude, cfg.HomeLongitude)
			if err == nil {
				cfg.HomeLatitude = lat
				cfg.HomeLongitude = lon
				changed = true
				syncNeeded = true
			}

		case "schedule":
			scheduleResult, err := scheduleWizard()
			if err == nil {
				err = applyScheduleWizardResult(cfg, scheduleResult)
			}
			if err == nil {
				changed = true
				syncNeeded = true
				afterSaveMessage = scheduleWizardSavedPresetMessage(scheduleResult)
			}

		case "telegram":
			tgCfg, err := telegramSetup()
			if err == nil {
				cfg.Telegram = tgCfg
				changed = true
				syncNeeded = true
			}

		case "github":
			fmt.Printf("\n  Current fork: %s\n", cfg.GithubFork)
			fmt.Printf("  To re-sync secrets and workflows, run: %s\n\n",
				lipgloss.NewStyle().Bold(true).Render("woffux sync"))
			return nil
		}

		if changed {
			if err := config.Save(cfg); err != nil {
				return fmt.Errorf("save config: %w", err)
			}
			fmt.Printf("  %s Config saved locally\n", sOk)
			if afterSaveMessage != "" {
				fmt.Print(afterSaveMessage)
			}

		}

		// Explain sync and offer it. Password changes need this too even though
		// the YAML config file itself does not change.
		if syncNeeded && cfg.GithubFork != "" {
			fmt.Println()
			fmt.Println(lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render(
				"  Your local settings changed. GitHub Actions still uses the old values.\n" +
					"  Sync pushes your new settings so auto-signing uses them."))
			fmt.Println()

			var sync bool
			if err := huh.NewForm(
				huh.NewGroup(
					huh.NewConfirm().
						Title("Push changes to GitHub now?").
						Description(fmt.Sprintf("This updates secrets and workflows on %s", cfg.GithubFork)).
						Affirmative("Sync now").
						Negative("I'll do it later (woffux sync)").
						Value(&sync),
				),
			).Run(); err != nil {
				return err
			}

			if sync {
				if err := checkGhInstalled(); err != nil {
					return err
				}
				if syncPassword == "" {
					pw, err := config.GetPassword(cfg.WoffuEmail)
					if err != nil {
						return fmt.Errorf("get password for sync: %w", err)
					}
					syncPassword = pw
				}

				var syncErr error
				spinner.New().
					Title("Syncing to GitHub...").
					Action(func() { syncErr = syncGitHubConfig(cfg, syncPassword) }).
					Run()

				if syncErr != nil {
					fmt.Printf("  %s GitHub sync failed: %s\n", sWarn, syncErr)
				} else {
					fmt.Printf("  %s GitHub synced — auto-signing will use your new settings\n", sOk)
				}
			} else {
				fmt.Printf("\n  %s Remember to run %s when you're ready.\n",
					lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render("!"),
					lipgloss.NewStyle().Bold(true).Render("woffux sync"))
				fmt.Println(lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render(
					"  Until then, auto-signing uses the previous settings."))
			}
		}

		return nil
	},
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

func telegramSummary(t config.TelegramConfig) string {
	if t.BotToken != "" {
		return "enabled"
	}
	return "not configured"
}
