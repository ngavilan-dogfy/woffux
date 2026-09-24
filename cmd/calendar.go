package cmd

import (
	"fmt"
	"strings"
	"time"

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

		token, err := woffu.AuthenticateCached(client, companyClient, cfg.WoffuEmail, password)
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

		month := time.Now().Month()
		if calendarMonth > 0 {
			month = time.Month(calendarMonth)
		}
		if uid, _, err := woffu.GetUserIds(companyClient, token); err == nil && uid > 0 {
			reqs, _ := woffu.GetMonthRequests(companyClient, token, uid, calendarYearFor(month), month)
			signs, _ := woffu.GetMonthSigns(companyClient, token, calendarYearFor(month), month)
			woffu.EnrichCalendarDays(days, reqs, signs)
		}
		viewCalendar(days, calendarYearFor(month), month)
		return nil
	},
}

func init() {
	calendarCmd.Flags().BoolVar(&calendarJSON, "json", false, "Output as JSON")
	calendarCmd.Flags().BoolVar(&calendarPlain, "plain", false, "Output as plain TSV")
	calendarCmd.Flags().IntVarP(&calendarMonth, "month", "m", 0, "Month number (1-12, default: current)")
}
