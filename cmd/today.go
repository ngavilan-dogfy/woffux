package cmd

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
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

		token, err := woffu.Authenticate(client, companyClient, cfg.WoffuEmail, password)
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

		// TTY
		sLabel := lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Width(16)
		sVal := lipgloss.NewStyle().Bold(true)
		sIn := lipgloss.NewStyle().Foreground(lipgloss.Color("#22c55e"))
		sOut := lipgloss.NewStyle().Foreground(lipgloss.Color("#ef4444"))
		sDim := lipgloss.NewStyle().Foreground(lipgloss.Color("#6b7280"))

		workingDay := sIn.Render("yes")
		if !info.IsWorkingDay {
			workingDay = sOut.Render("no")
		}

		fmt.Println()
		fmt.Println("  " + sLabel.Render("Date") + sVal.Render(info.Date))
		fmt.Println("  " + sLabel.Render("Working day") + workingDay)
		fmt.Println("  " + sLabel.Render("Mode") + fmt.Sprintf("%s %s", info.Mode.Emoji(), info.Mode.Label()))
		if info.IsWorkingDay {
			fmt.Println("  " + sLabel.Render("Coordinates") + sDim.Render(fmt.Sprintf("%.4f, %.4f", info.Latitude, info.Longitude)))
		}

		// Slots
		fmt.Println()
		if len(slots) == 0 {
			fmt.Println("  " + sDim.Render("No sign slots today"))
		} else {
			fmt.Println("  " + sVal.Render("Sign slots"))
			for i, s := range slots {
				in := sDim.Render("—")
				out := sDim.Render("—")
				if s.In != "" {
					in = sIn.Render("IN  " + slotTime(s.In))
				}
				if s.Out != "" {
					out = sOut.Render("OUT " + slotTime(s.Out))
				}
				fmt.Printf("    Block %d:  %s  %s\n", i+1, in, out)
			}
		}
		fmt.Println()

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
