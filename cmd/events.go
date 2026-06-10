package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ngavilan-dogfy/woffux/internal/woffu"
)

var eventsJSON bool
var eventsPlain bool

var eventsCmd = &cobra.Command{
	Use:   "events",
	Short: "Show available events (vacations, hours, etc)",
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

		events, err := woffu.GetAvailableEvents(companyClient, token)
		if err != nil {
			return fmt.Errorf("get events: %w", err)
		}

		// JSON output
		if eventsJSON {
			var items []map[string]interface{}
			for _, e := range events {
				items = append(items, map[string]interface{}{
					"name":      e.Name,
					"available": e.Available,
					"unit":      e.Unit,
				})
			}
			return printJSON(items)
		}

		// Plain/piped output: TSV
		if eventsPlain || !isTTY() {
			headers := []string{"NAME", "AVAILABLE", "UNIT"}
			var rows [][]string
			for _, e := range events {
				rows = append(rows, []string{
					e.Name,
					fmt.Sprintf("%.0f", e.Available),
					e.Unit,
				})
			}
			printTSV(headers, rows)
			return nil
		}

		// TTY: styled human-friendly output
		fmt.Println("Available events:")
		fmt.Println()
		for _, e := range events {
			fmt.Printf("  %-45s %6.0f %s\n", e.Name, e.Available, e.Unit)
		}

		return nil
	},
}

func init() {
	eventsCmd.Flags().BoolVar(&eventsJSON, "json", false, "Output as JSON")
	eventsCmd.Flags().BoolVar(&eventsPlain, "plain", false, "Output as plain TSV (no colors)")
}
