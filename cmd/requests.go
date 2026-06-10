package cmd

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
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

		// TTY
		if len(requests) == 0 {
			fmt.Println(lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render("  No requests found."))
			return nil
		}

		sId := lipgloss.NewStyle().Foreground(lipgloss.Color("#6b7280")).Width(10)
		sType := lipgloss.NewStyle().Width(25)
		sDate := lipgloss.NewStyle().Foreground(lipgloss.Color("#6b7280")).Width(24)

		fmt.Println()
		for _, r := range requests {
			statusStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#f59e0b")) // pending=amber
			switch r.Status {
			case "approved":
				statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#22c55e"))
			case "rejected":
				statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#ef4444"))
			case "cancelled":
				statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#6b7280"))
			}

			dateRange := r.StartDate
			if r.StartDate != r.EndDate {
				dateRange = r.StartDate + " → " + r.EndDate
			}

			fmt.Printf("  %s %s %s %s\n",
				sId.Render(fmt.Sprintf("#%d", r.RequestID)),
				sType.Render(r.EventName),
				sDate.Render(dateRange),
				statusStyle.Render(r.Status),
			)
		}
		fmt.Println()

		return nil
	},
}

func init() {
	requestsCmd.Flags().BoolVar(&requestsJSON, "json", false, "Output as JSON")
	requestsCmd.Flags().BoolVar(&requestsPlain, "plain", false, "Output as plain TSV")
	requestsCmd.Flags().IntVar(&requestsPage, "page", 1, "Page number")
	requestsCmd.Flags().IntVar(&requestsSize, "size", 50, "Page size")
}
