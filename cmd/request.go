package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/huh/spinner"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/ngavilan-dogfy/woffux/internal/woffu"
)

var (
	requestType  string
	requestDates string
)

var requestCmd = &cobra.Command{
	Use:   "request",
	Short: "Create or cancel requests (telework, vacation, absence)",
	Long: `Submit or cancel requests on Woffu.

Examples:
  woffux request                                  Interactive — pick type and dates
  woffux request --type "Teletrabajo🏡" --dates 2026-03-20
  woffux request --type Vacaciones --dates 2026-04-07,2026-04-08,2026-04-09
  woffux request cancel 17117405                  Cancel a request by ID`,
	RunE: func(cmd *cobra.Command, args []string) error {
		var parsedDates []string
		if requestDates != "" {
			var err error
			parsedDates, err = parseDateList(requestDates)
			if err != nil {
				return err
			}
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

		// Get request types
		types, err := woffu.GetRequestTypes(companyClient, token)
		if err != nil {
			return fmt.Errorf("get request types: %w", err)
		}

		userId, companyId, err := woffu.GetUserIds(companyClient, token)
		if err != nil {
			return fmt.Errorf("get user: %w", err)
		}

		// Resolve type
		var selectedType *woffu.RequestType
		if requestType != "" {
			// Match by name (partial, case-insensitive)
			for i, t := range types {
				if strings.Contains(strings.ToLower(t.Name), strings.ToLower(requestType)) {
					selectedType = &types[i]
					break
				}
			}
			if selectedType == nil {
				return fmt.Errorf("request type \"%s\" not found. Run 'woffux request' interactively to see available types", requestType)
			}
		} else {
			// Interactive: pick type
			selectedType, err = pickRequestType(types)
			if err != nil {
				return err
			}
		}

		// Resolve dates
		var dates []string
		if requestDates != "" {
			dates = parsedDates
		} else {
			// Interactive: enter dates
			var datesInput string
			err = huh.NewForm(
				huh.NewGroup(
					huh.NewInput().
						Title("Dates").
						Description("YYYY-MM-DD, comma-separated for multiple").
						Placeholder("2026-03-20,2026-03-21").
						Value(&datesInput).
						Validate(func(s string) error {
							_, err := parseDateList(s)
							return err
						}),
				).Title(selectedType.Name),
			).Run()
			if err != nil {
				return err
			}
			dates, err = parseDateList(datesInput)
			if err != nil {
				return err
			}
		}

		// Submit each date
		sOkStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#22c55e")).Bold(true)
		sErrStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#ef4444")).Bold(true)

		successCount := 0
		for i, date := range dates {
			var submitErr error
			spinner.New().
				Title(fmt.Sprintf("Submitting %d/%d (%s)...", i+1, len(dates), date)).
				Action(func() {
					submitErr = woffu.CreateRequest(companyClient, token, userId, companyId, selectedType.ID, date, date, selectedType.IsVacation)
				}).
				Run()

			if submitErr != nil {
				fmt.Printf("  %s %s — %s\n", sErrStyle.Render("✗"), date, submitErr)
			} else {
				fmt.Printf("  %s %s — %s submitted\n", sOkStyle.Render("✓"), date, selectedType.Name)
				successCount++
			}
		}

		fmt.Printf("\n  %d/%d requests submitted.\n\n", successCount, len(dates))
		return nil
	},
}

var requestCancelCmd = &cobra.Command{
	Use:   "cancel <request-id>",
	Short: "Cancel a request",
	Long: `Cancel/withdraw a request by its ID.

Find the ID with:  woffux requests
                   woffux requests --json | jq '.[].request_id'`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, password, err := loadConfigOrSetup()
		if err != nil {
			return err
		}

		var requestId int
		_, err = fmt.Sscanf(args[0], "%d", &requestId)
		if err != nil {
			return fmt.Errorf("invalid request ID: %s", args[0])
		}

		client := woffu.NewWoffuClient(cfg.WoffuURL)
		companyClient := woffu.NewCompanyClient(cfg.WoffuCompanyURL)

		token, err := woffu.AuthenticateCached(client, companyClient, cfg.WoffuEmail, password)
		if err != nil {
			return fmt.Errorf("auth failed: %w", err)
		}

		var confirm bool
		if err := huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title(fmt.Sprintf("Cancel request #%d?", requestId)).
					Description("This will withdraw the request from Woffu").
					Affirmative("Cancel it").
					Negative("Keep it").
					Value(&confirm),
			),
		).Run(); err != nil {
			return err
		}

		if !confirm {
			return nil
		}

		var cancelErr error
		spinner.New().
			Title("Cancelling...").
			Action(func() {
				cancelErr = woffu.CancelRequest(companyClient, token, requestId)
			}).
			Run()

		if cancelErr != nil {
			fmt.Printf("\n  %s Could not cancel: %s\n\n",
				lipgloss.NewStyle().Foreground(lipgloss.Color("#ef4444")).Render("✗"), cancelErr)
			return nil
		}

		fmt.Printf("\n  %s Request #%d cancelled.\n\n",
			lipgloss.NewStyle().Foreground(lipgloss.Color("#22c55e")).Render("✓"), requestId)
		return nil
	},
}

func init() {
	requestCmd.Flags().StringVarP(&requestType, "type", "t", "", "Request type name (e.g. Teletrabajo, Vacaciones)")
	requestCmd.Flags().StringVarP(&requestDates, "dates", "d", "", "Dates (YYYY-MM-DD, comma-separated)")
	requestCmd.AddCommand(requestCancelCmd)
}

func parseDateList(value string) ([]string, error) {
	parts := strings.Split(value, ",")
	dates := make([]string, 0, len(parts))
	for _, part := range parts {
		date := strings.TrimSpace(part)
		if date == "" {
			return nil, fmt.Errorf("dates must be YYYY-MM-DD values separated by commas")
		}
		if _, err := time.Parse("2006-01-02", date); err != nil {
			return nil, fmt.Errorf("invalid date %q (use YYYY-MM-DD)", date)
		}
		dates = append(dates, date)
	}
	if len(dates) == 0 {
		return nil, fmt.Errorf("enter at least one date")
	}
	return dates, nil
}

func pickRequestType(types []woffu.RequestType) (*woffu.RequestType, error) {
	// Group: common types first, then the rest
	var commonOptions []huh.Option[int]
	var otherOptions []huh.Option[int]

	commonNames := []string{"teletrabajo", "vacaciones", "asuntos propios", "bolsa de horas", "enfermedad"}

	for i, t := range types {
		label := t.Name
		if t.Available != "" {
			label += fmt.Sprintf("  (%s available)", t.Available)
		}

		isCommon := false
		for _, cn := range commonNames {
			if strings.Contains(strings.ToLower(t.Name), cn) {
				isCommon = true
				break
			}
		}

		opt := huh.NewOption(label, i)
		if isCommon {
			commonOptions = append(commonOptions, opt)
		} else {
			otherOptions = append(otherOptions, opt)
		}
	}

	allOptions := append(commonOptions, otherOptions...)

	var selected int
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[int]().
				Title("Request type").
				Options(allOptions...).
				Value(&selected),
		),
	).Run()
	if err != nil {
		return nil, err
	}

	return &types[selected], nil
}
