package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/ngavilan-dogfy/woffux/internal/woffu"
)

var (
	historyJSON  bool
	historyPlain bool
	historyFrom  string
	historyTo    string
	historyDays  int
)

var historyCmd = &cobra.Command{
	Use:   "history",
	Short: "View sign history",
	Long: `View past clock in/out records.

Examples:
  woffux history                  Last 7 days
  woffux history -d 30            Last 30 days
 woffux history --from 2026-03-01
  woffux history --json | jq '.[].date'`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if historyDays < 0 {
			return fmt.Errorf("--days must be 0 or greater")
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

		// Resolve date range
		now := time.Now()
		to := now
		from := now.AddDate(0, 0, -historyDays)

		if historyFrom != "" {
			from, err = time.Parse("2006-01-02", historyFrom)
			if err != nil {
				return fmt.Errorf("invalid --from date (use YYYY-MM-DD): %w", err)
			}
		}
		if historyTo != "" {
			to, err = time.Parse("2006-01-02", historyTo)
			if err != nil {
				return fmt.Errorf("invalid --to date (use YYYY-MM-DD): %w", err)
			}
		}
		if from.After(to) {
			return fmt.Errorf("--from must be on or before --to")
		}

		signs, err := woffu.GetSignHistory(companyClient, token, from, to)
		if err != nil {
			return fmt.Errorf("get history: %w", err)
		}

		if historyJSON {
			return printJSON(signs)
		}

		if historyPlain || !isTTY() {
			headers := []string{"DATE", "TIME", "TYPE"}
			var rows [][]string
			for _, s := range signs {
				rows = append(rows, []string{s.Date, s.Time, s.Type})
			}
			printTSV(headers, rows)
			return nil
		}

		viewHistory(signs, from, to)
		return nil
	},
}

func init() {
	historyCmd.Flags().BoolVar(&historyJSON, "json", false, "Output as JSON")
	historyCmd.Flags().BoolVar(&historyPlain, "plain", false, "Output as plain TSV")
	historyCmd.Flags().StringVar(&historyFrom, "from", "", "Start date (YYYY-MM-DD)")
	historyCmd.Flags().StringVar(&historyTo, "to", "", "End date (YYYY-MM-DD)")
	historyCmd.Flags().IntVarP(&historyDays, "days", "d", 7, "Number of days to look back")
}
