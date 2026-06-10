package cmd

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/ngavilan-dogfy/woffux/internal/woffu"
)

var (
	holidaysJSON  bool
	holidaysPlain bool
)

var holidaysCmd = &cobra.Command{
	Use:   "holidays",
	Short: "List company holidays",
	Long: `Show all holidays in your company calendar.

Examples:
  woffux holidays
  woffux holidays --json | jq '.[].name'`,
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

		_, calendarId, err := woffu.GetUserId(companyClient, token)
		if err != nil {
			return fmt.Errorf("get user: %w", err)
		}

		holidays, err := woffu.GetHolidays(companyClient, token, calendarId)
		if err != nil {
			return fmt.Errorf("get holidays: %w", err)
		}

		if holidaysJSON {
			return printJSON(holidays)
		}

		if holidaysPlain || !isTTY() {
			headers := []string{"DATE", "NAME"}
			var rows [][]string
			for _, h := range holidays {
				rows = append(rows, []string{h.Date, h.Name})
			}
			printTSV(headers, rows)
			return nil
		}

		// TTY
		sDate := lipgloss.NewStyle().Foreground(lipgloss.Color("#6b7280")).Width(14)
		sName := lipgloss.NewStyle().Foreground(lipgloss.Color("#ef4444"))

		fmt.Printf("\n  Company holidays (%d)\n\n", len(holidays))
		for _, h := range holidays {
			fmt.Printf("  %s %s\n", sDate.Render(h.Date), sName.Render(h.Name))
		}
		fmt.Println()

		return nil
	},
}

func init() {
	holidaysCmd.Flags().BoolVar(&holidaysJSON, "json", false, "Output as JSON")
	holidaysCmd.Flags().BoolVar(&holidaysPlain, "plain", false, "Output as plain TSV")
}
