package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ngavilan-dogfy/woffux/internal/config"
	"github.com/ngavilan-dogfy/woffux/internal/notify"
	"github.com/ngavilan-dogfy/woffux/internal/woffu"
)

var signForce bool
var signExpected string

var signCmd = &cobra.Command{
	Use:   "sign",
	Short: "Clock in/out on Woffu (works locally and in CI)",
	Long: `Clock in/out on Woffu. Checks calendar first and only signs on working days.

Examples:
  woffux sign                    Sign for today (toggle in/out)
  woffux sign --force            Sign even if not a working day
  woffux sign --expected in      Only sign IN (skip if already signed in)
  woffux sign --expected out     Only sign OUT (skip if already signed out)

The --expected flag prevents the auto-sign from toggling you in the wrong
direction when you've already signed manually.

Batch (from stdin):
  echo "sign" | woffux sign

In CI, reads credentials from environment variables.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		expectedAction, err := parseExpectedSignAction(signExpected)
		if err != nil {
			return err
		}

		cfg, password, err := config.LoadOrEnv()
		if err != nil {
			return err
		}

		client := woffu.NewWoffuClient(cfg.WoffuURL)
		companyClient := woffu.NewCompanyClient(cfg.WoffuCompanyURL)

		if isTTY() {
			fmt.Println("Authenticating...")
		}
		token, err := woffu.Authenticate(client, companyClient, cfg.WoffuEmail, password)
		if err != nil {
			return fmt.Errorf("auth failed: %w", err)
		}

		if isTTY() {
			fmt.Println("Checking calendar...")
		}
		info, err := woffu.GetSignInfo(companyClient, token, cfg.Latitude, cfg.Longitude, cfg.HomeLatitude, cfg.HomeLongitude)
		if err != nil {
			return fmt.Errorf("get sign info: %w", err)
		}

		telegramCfg := notify.TelegramConfig{
			BotToken: cfg.Telegram.BotToken,
			ChatID:   cfg.Telegram.ChatID,
		}

		if !info.IsWorkingDay && !signForce {
			if isTTY() {
				fmt.Println("Not a working day — skipping.")
			}
			_ = notify.SendSkippedNotification(telegramCfg, info.Date, "Not a working day")
			return nil
		}

		// Guard: check if sign should be skipped based on expected action
		if expectedAction != "" {
			slots, slotsErr := woffu.GetTodaySlots(companyClient, token)
			if slotsErr != nil {
				return fmt.Errorf("get slots: %w", slotsErr)
			}
			if woffu.ShouldSkipSign(slots, expectedAction) {
				reason := fmt.Sprintf("Already signed %s", expectedAction)
				if isTTY() {
					fmt.Printf("%s — skipping.\n", reason)
				} else {
					fmt.Printf("SKIP %s %s\n", info.Date, reason)
				}
				_ = notify.SendSkippedNotification(telegramCfg, info.Date, reason)
				return nil
			}
		}

		if isTTY() {
			fmt.Printf("%s %s — signing with coordinates (%.4f, %.4f)\n",
				info.Mode.Emoji(), info.Mode.Label(), info.Latitude, info.Longitude)
		}

		err = woffu.DoSign(companyClient, token, info.Latitude, info.Longitude)
		if err != nil {
			return fmt.Errorf("sign failed: %w", err)
		}

		if isTTY() {
			fmt.Println("Signed successfully!")
		} else {
			fmt.Printf("OK %s %s %s\n", info.Date, info.Mode, info.Mode.Label())
		}

		if err := notify.SendSignedNotification(telegramCfg, info); err != nil && isTTY() {
			fmt.Printf("Warning: telegram notification failed: %s\n", err)
		}

		return nil
	},
}

func init() {
	signCmd.Flags().BoolVar(&signForce, "force", false, "Sign even if not a working day")
	signCmd.Flags().StringVar(&signExpected, "expected", "", "Expected sign action: 'in' or 'out'. Skips if already in that state.")
}

func parseExpectedSignAction(value string) (woffu.SignAction, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "":
		return "", nil
	case string(woffu.SignActionIn):
		return woffu.SignActionIn, nil
	case string(woffu.SignActionOut):
		return woffu.SignActionOut, nil
	default:
		return "", fmt.Errorf("invalid --expected value %q (use \"in\" or \"out\")", value)
	}
}

// readStdinLines reads non-empty lines from stdin (for batch piping).
func readStdinLines() []string {
	var lines []string
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}
