package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ngavilan-dogfy/woffuk-cli/internal/config"
	"github.com/ngavilan-dogfy/woffuk-cli/internal/notify"
	"github.com/ngavilan-dogfy/woffuk-cli/internal/woffu"
)

var signCmd = &cobra.Command{
	Use:   "sign",
	Short: "Clock in/out on Woffu (works locally and in CI)",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, password, err := config.LoadOrEnv()
		if err != nil {
			return err
		}

		client := woffu.NewWoffuClient(cfg.WoffuURL)
		companyClient := woffu.NewCompanyClient(cfg.WoffuCompanyURL)

		fmt.Println("Authenticating...")
		token, err := woffu.Authenticate(client, companyClient, cfg.WoffuEmail, password)
		if err != nil {
			return fmt.Errorf("auth failed: %w", err)
		}

		fmt.Println("Checking calendar...")
		info, err := woffu.GetSignInfo(companyClient, token, cfg.Latitude, cfg.Longitude, cfg.HomeLatitude, cfg.HomeLongitude)
		if err != nil {
			return fmt.Errorf("get sign info: %w", err)
		}

		telegramCfg := notify.TelegramConfig{
			BotToken: cfg.Telegram.BotToken,
			ChatID:   cfg.Telegram.ChatID,
		}

		if !info.IsWorkingDay {
			fmt.Println("Not a working day — skipping.")
			_ = notify.SendSkippedNotification(telegramCfg, info.Date, "Not a working day")
			return nil
		}

		fmt.Printf("%s %s — signing with coordinates (%.4f, %.4f)\n",
			info.Mode.Emoji(), info.Mode.Label(), info.Latitude, info.Longitude)

		err = woffu.DoSign(companyClient, token, info.Latitude, info.Longitude)
		if err != nil {
			return fmt.Errorf("sign failed: %w", err)
		}

		fmt.Println("Signed successfully!")

		if err := notify.SendSignedNotification(telegramCfg, info); err != nil {
			fmt.Printf("Warning: telegram notification failed: %s\n", err)
		}

		return nil
	},
}
