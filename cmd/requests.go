package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ngavilan-dogfy/woffux/internal/woffu"
)

var (
	requestsJSON  bool
	requestsPlain bool
	requestsPage  int
	requestsSize  int
)

var requestsCmd = &cobra.Command{
	Use:     "requests",
	Aliases: []string{"req"},
	Short:   "List your requests (vacations, telework, absences)",
	Long: `View your submitted requests on Woffu.

Examples:
  woffux requests                     Last 50 requests
  woffux requests --page 2            Page 2
  woffux requests --json | jq '.[] | select(.status == "approved")'
  woffux requests --plain | grep Vacaciones`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if requestsPage < 1 {
			return fmt.Errorf("--page must be 1 or greater")
		}
		if requestsSize < 1 {
			return fmt.Errorf("--size must be 1 or greater")
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

		userId, _, err := woffu.GetUserId(companyClient, token)
		if err != nil {
			return fmt.Errorf("get user: %w", err)
		}

		requests, err := woffu.GetUserRequests(companyClient, token, userId, requestsPage, requestsSize)
		if err != nil {
			return fmt.Errorf("get requests: %w", err)
		}

		if requestsJSON {
			return printJSON(requests)
		}

		if requestsPlain || !isTTY() {
			headers := []string{"ID", "TYPE", "START", "END", "STATUS", "DAYS"}
			var rows [][]string
			for _, r := range requests {
				rows = append(rows, []string{
					fmt.Sprintf("%d", r.RequestID),
					r.EventName,
					r.StartDate,
					r.EndDate,
					r.Status,
					fmt.Sprintf("%d", r.Days),
				})
			}
			printTSV(headers, rows)
			return nil
		}

		viewRequests(requests)
		return nil
	},
}

func init() {
	requestsCmd.Flags().BoolVar(&requestsJSON, "json", false, "Output as JSON")
	requestsCmd.Flags().BoolVar(&requestsPlain, "plain", false, "Output as plain TSV")
	requestsCmd.Flags().IntVar(&requestsPage, "page", 1, "Page number")
	requestsCmd.Flags().IntVar(&requestsSize, "size", 50, "Page size")
}
