package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ngavilan-dogfy/woffux/internal/woffu"
)

var todayJSON bool

var todayCmd = &cobra.Command{
	Use:   "today",
	Short: "Today's detailed work info and sign slots",
	Long: `Show detailed info about today: schedule, sign slots, and working status.

Examples:
  woffux today
  woffux today --json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, password, err := loadConfigOrSetup()
		if err != nil {
			return err
		}

		client := woffu.NewWoffuClient(cfg.WoffuURL)
		companyClient := woffu.NewCompanyClient(cfg.WoffuCompanyURL)

		token, err := woffu.AuthenticateCached(client, companyClient, cfg.WoffuEmail, password)
		if err != nil {
			return fmt.Errorf("auth failed: %w\n\n  If your credentials changed, run 'woffux setup'", err)
		}

		// Get sign info
		info, err := woffu.GetSignInfo(companyClient, token, cfg.Latitude, cfg.Longitude, cfg.HomeLatitude, cfg.HomeLongitude)
		if err != nil {
			return fmt.Errorf("get sign info: %w", err)
		}

		// Get today's slots
		slots, err := woffu.GetTodaySlots(companyClient, token)
		if err != nil {
			return fmt.Errorf("get slots: %w", err)
		}

		if todayJSON {
			return printJSON(map[string]interface{}{
				"date":        info.Date,
				"working_day": info.IsWorkingDay,
				"mode":        string(info.Mode),
				"latitude":    info.Latitude,
				"longitude":   info.Longitude,
				"slots":       slots,
			})
		}

		viewToday(info, slots)
		return nil
	},
}

// slotTime extracts a short time (HH:MM) from a slot datetime string.
func slotTime(dt string) string {
	if idx := strings.Index(dt, "T"); idx != -1 {
		t := dt[idx+1:]
		if len(t) >= 5 {
			return t[:5]
		}
	}
	if len(dt) >= 5 {
		return dt[:5]
	}
	return dt
}

func init() {
	todayCmd.Flags().BoolVar(&todayJSON, "json", false, "Output as JSON")
}
