package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/ngavilan-dogfy/woffux/internal/woffu"
)

var (
	calendarJSON  bool
	calendarPlain bool
	calendarMonth int
)

var calendarCmd = &cobra.Command{
	Use:   "calendar",
	Short: "View working days, holidays, and telework",
	Long: `Show calendar with working days, holidays, absences, and telework.

Examples:
  woffux calendar                  Current month
  woffux calendar -m 4             April
  woffux calendar --json | jq '.[] | select(.is_holiday)'`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if calendarMonth < 0 || calendarMonth > 12 {
			return fmt.Errorf("--month must be between 1 and 12")
		}

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

		days, err := woffu.GetCalendarMonth(companyClient, token, calendarMonth)
		if err != nil {
			return fmt.Errorf("get calendar: %w", err)
		}

		if calendarJSON {
			return printJSON(days)
		}

		if calendarPlain || !isTTY() {
			headers := []string{"DATE", "DAY", "STATUS", "MODE", "EVENTS"}
			var rows [][]string
			for _, d := range days {
				rows = append(rows, []string{d.Date, d.DayName, d.Status, d.Mode, strings.Join(d.EventNames, "; ")})
			}
			printTSV(headers, rows)
			return nil
		}

		// TTY — visual calendar
		now := time.Now()
		month := now.Month()
		if calendarMonth > 0 {
			month = time.Month(calendarMonth)
		}

		sDate := lipgloss.NewStyle().Width(12).Foreground(lipgloss.Color("#6b7280"))
		sDay := lipgloss.NewStyle().Width(5).Foreground(lipgloss.Color("#6b7280"))
		sWork := lipgloss.NewStyle().Foreground(lipgloss.Color("#22c55e"))
		sHoliday := lipgloss.NewStyle().Foreground(lipgloss.Color("#ef4444"))
		sWeekend := lipgloss.NewStyle().Foreground(lipgloss.Color("#4b5563"))
		sAbsence := lipgloss.NewStyle().Foreground(lipgloss.Color("#f59e0b"))
		sTele := lipgloss.NewStyle().Foreground(lipgloss.Color("#06B6D4"))
		sEvent := lipgloss.NewStyle().Foreground(lipgloss.Color("#6b7280")).Italic(true)

		fmt.Printf("\n  %s %d\n\n", month.String(), now.Year())

		for _, d := range days {
			statusStyle := sWork
			statusIcon := "  "

			switch d.Status {
			case "weekend":
				statusStyle = sWeekend
				statusIcon = "  "
			case "holiday":
				statusStyle = sHoliday
				statusIcon = "H "
			case "absence":
				statusStyle = sAbsence
				statusIcon = "A "
			case "working":
				statusIcon = "  "
			}

			modeStr := ""
			if d.Mode == "remote" {
				modeStr = sTele.Render(" remote")
			}

			eventStr := ""
			if len(d.EventNames) > 0 {
				eventStr = sEvent.Render(" " + d.EventNames[0])
			}

			fmt.Printf("  %s %s %s%s%s\n",
				sDate.Render(d.Date),
				sDay.Render(d.DayName),
				statusStyle.Render(statusIcon+d.Status),
				modeStr,
				eventStr,
			)
		}
		fmt.Println()

		return nil
	},
}

func init() {
	calendarCmd.Flags().BoolVar(&calendarJSON, "json", false, "Output as JSON")
	calendarCmd.Flags().BoolVar(&calendarPlain, "plain", false, "Output as plain TSV")
	calendarCmd.Flags().IntVarP(&calendarMonth, "month", "m", 0, "Month number (1-12, default: current)")
}
