package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ngavilan-dogfy/woffux/internal/woffu"
)

var statusJSON bool
var statusPlain bool

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show today's signing status",
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

		info, err := woffu.GetSignInfo(companyClient, token, cfg.Latitude, cfg.Longitude, cfg.HomeLatitude, cfg.HomeLongitude)
		if err != nil {
			return fmt.Errorf("get sign info: %w", err)
		}

		// JSON output
		if statusJSON {
			return printJSON(statusToJSON(info))
		}

		// Plain/piped output: TSV
		if statusPlain || !isTTY() {
			headers := []string{"DATE", "WORKING_DAY", "MODE", "LATITUDE", "LONGITUDE"}
			rows := [][]string{
				{
					info.Date,
					boolToYesNo(info.IsWorkingDay),
					string(info.Mode),
					fmt.Sprintf("%.4f", info.Latitude),
					fmt.Sprintf("%.4f", info.Longitude),
				},
			}
			printTSV(headers, rows)
			return nil
		}

		// TTY: styled human-friendly output
		fmt.Printf("Date:        %s\n", info.Date)
		fmt.Printf("Working day: %s\n", boolToYesNo(info.IsWorkingDay))
		fmt.Printf("Mode:        %s %s\n", info.Mode.Emoji(), info.Mode.Label())
		if info.IsWorkingDay {
			fmt.Printf("Coordinates: %.4f, %.4f\n", info.Latitude, info.Longitude)
		}

		if len(info.NextEvents) > 0 {
			fmt.Println("\nNext events:")
			for _, e := range info.NextEvents {
				names := ""
				if len(e.Names) > 0 {
					names = " — " + e.Names[0]
				}
				fmt.Printf("  %s%s\n", e.Date, names)
			}
		}

		return nil
	},
}

func init() {
	statusCmd.Flags().BoolVar(&statusJSON, "json", false, "Output as JSON")
	statusCmd.Flags().BoolVar(&statusPlain, "plain", false, "Output as plain TSV (no colors)")
}

func boolToYesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// statusToJSON builds a structured map for JSON output.
func statusToJSON(info *woffu.SignInfo) map[string]interface{} {
	result := map[string]interface{}{
		"date":        info.Date,
		"working_day": info.IsWorkingDay,
		"mode":        string(info.Mode),
		"latitude":    info.Latitude,
		"longitude":   info.Longitude,
	}

	if len(info.NextEvents) > 0 {
		var events []map[string]interface{}
		for _, e := range info.NextEvents {
			events = append(events, map[string]interface{}{
				"date":  e.Date,
				"names": e.Names,
			})
		}
		result["next_events"] = events
	}

	return result
}
