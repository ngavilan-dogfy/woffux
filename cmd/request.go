package cmd

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/huh/spinner"
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
			if dates, err = pickRequestDates(companyClient, token, userId, selectedType); err != nil {
				return err
			}
			if len(dates) == 0 {
				fmt.Println()
				uiWarn("No days selected — nothing sent.")
				fmt.Println()
				return nil
			}
			if ok, err := confirmRequest(companyClient, token, selectedType, dates); err != nil || !ok {
				if err == nil {
					fmt.Println()
					uiWarn("Cancelled — nothing was sent.")
					fmt.Println()
				}
				return err
			}
		}

		// Submit each date
		fmt.Println()
		successCount := 0
		for i, date := range dates {
			var submitErr error
			spinner.New().
				Title(fmt.Sprintf("Sending %d of %d · %s…", i+1, len(dates), uiDate(date))).
				Action(func() {
					submitErr = woffu.CreateRequest(companyClient, token, userId, companyId, selectedType.ID, date, date, selectedType.IsVacation)
				}).
				Run()
			if submitErr != nil {
				uiErr("%s — %s", uiDate(date), submitErr)
			} else {
				uiOK("%s — %s requested", uiDate(date), uiTrim(selectedType.Name))
				successCount++
			}
		}
		fmt.Println()
		if successCount == len(dates) {
			uiLine(stBold.Render(fmt.Sprintf("%d %s sent", successCount, pluralS(successCount, "request", "requests"))) + stFaint.Render(" — your manager will review them in Woffu."))
		} else {
			uiLine(stOut.Render(fmt.Sprintf("%d of %d sent", successCount, len(dates))))
		}
		uiHint("woffux requests")
		return nil
	},
}

var requestCancelCmd = &cobra.Command{
	Use:   "cancel [request-id]",
	Short: "Cancel requests (pick from a list, or pass an ID)",
	Long: `Cancel/withdraw requests.

Without an ID you pick from your pending and upcoming approved requests.
Find IDs with:  woffux requests`,
	Args:         cobra.MaximumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, password, err := loadConfigOrSetup()
		if err != nil {
			return err
		}
		client := woffu.NewWoffuClient(cfg.WoffuURL)
		companyClient := woffu.NewCompanyClient(cfg.WoffuCompanyURL)
		token, err := woffu.AuthenticateCached(client, companyClient, cfg.WoffuEmail, password)
		if err != nil {
			return fmt.Errorf("auth failed: %w", err)
		}

		var ids []int
		labels := map[int]string{}
		if len(args) == 1 {
			var id int
			if _, err := fmt.Sscanf(strings.TrimPrefix(args[0], "#"), "%d", &id); err != nil {
				return fmt.Errorf("invalid request ID: %s", args[0])
			}
			ids = []int{id}
			labels[id] = fmt.Sprintf("request #%d", id)
		} else {
			userId, _, err := woffu.GetUserIds(companyClient, token)
			if err != nil {
				return fmt.Errorf("get user: %w", err)
			}
			var reqs []woffu.UserRequest
			spinner.New().Title("Loading your requests…").Action(func() {
				reqs, err = woffu.GetUserRequests(companyClient, token, userId, 1, 100)
			}).Run()
			if err != nil {
				return fmt.Errorf("get requests: %w", err)
			}
			today := time.Now().Format("2006-01-02")
			var opts []huh.Option[int]
			for _, r := range reqs {
				if r.Status != "pending" && !(r.Status == "approved" && r.EndDate >= today) {
					continue
				}
				label := fmt.Sprintf("%-16s %-18s %s", uiDate(r.StartDate), uiTrim(r.EventName), uiRequestStatus(r.Status))
				labels[r.RequestID] = uiTrim(r.EventName) + " · " + uiDate(r.StartDate)
				opts = append(opts, huh.NewOption(label, r.RequestID))
			}
			if len(opts) == 0 {
				fmt.Println()
				uiLine(stFaint.Render("Nothing to cancel: no pending or upcoming approved requests."))
				fmt.Println()
				return nil
			}
			if err := newForm(huh.NewGroup(huh.NewMultiSelect[int]().
				Title("Which requests should be cancelled?").
				Description("space to select · enter to continue").
				Options(opts...).Height(min(len(opts)+2, 14)).Value(&ids))).Run(); err != nil {
				return err
			}
			if len(ids) == 0 {
				return nil
			}
		}

		var names []string
		for _, id := range ids {
			names = append(names, labels[id])
		}
		confirm := false
		if err := newForm(huh.NewGroup(huh.NewConfirm().
			Title(fmt.Sprintf("Cancel %d %s?", len(ids), pluralS(len(ids), "request", "requests"))).
			Description(strings.Join(names, "\n") + "\n\nApproved ones are withdrawn and your manager is notified.").
			Affirmative("Cancel them").Negative("Keep them").Value(&confirm))).Run(); err != nil || !confirm {
			return err
		}

		fmt.Println()
		for _, id := range ids {
			var cancelErr error
			spinner.New().Title(fmt.Sprintf("Cancelling %s…", labels[id])).
				Action(func() { cancelErr = woffu.CancelRequest(companyClient, token, id) }).Run()
			if cancelErr != nil {
				uiErr("%s — %s", labels[id], cancelErr)
			} else {
				uiOK("%s cancelled", labels[id])
			}
		}
		fmt.Println()
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
	commonNames := []string{"teletrabajo", "vacaciones", "asuntos propios", "bolsa de horas"}
	rank := func(name string) int {
		for i, cn := range commonNames {
			if strings.Contains(strings.ToLower(name), cn) {
				return i
			}
		}
		return len(commonNames)
	}
	idx := make([]int, len(types))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return rank(types[idx[a]].Name) < rank(types[idx[b]].Name) })

	var opts []huh.Option[int]
	for _, i := range idx {
		t := types[i]
		name := uiTrim(t.Name)
		if r := []rune(name); len(r) > 44 {
			name = string(r[:43]) + "…"
		}
		label := padTo(name, 46)
		if t.Available != "" {
			label += stFaint.Render(t.Available + " left")
		}
		opts = append(opts, huh.NewOption(label, i))
	}
	var selected int
	if err := newForm(huh.NewGroup(huh.NewSelect[int]().
		Title("What do you want to request?").
		Description("type / to search").
		Options(opts...).Height(min(len(opts)+2, 14)).Value(&selected))).Run(); err != nil {
		return nil, err
	}
	return &types[selected], nil
}

// pickRequestDates offers the next working days as a checklist. Weekends,
// holidays, days off and days that already have a request are left out, so
// every option is one Woffu will accept.
func pickRequestDates(companyClient *woffu.Client, token string, userId int, t *woffu.RequestType) ([]string, error) {
	now := time.Now()
	var days []woffu.CalendarDay
	spinner.New().Title("Loading your calendar…").Action(func() {
		for m := 0; m < 3; m++ {
			first := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local).AddDate(0, m, 0)
			month, _ := woffu.GetCalendarMonthYM(companyClient, token, first.Year(), first.Month())
			reqs, _ := woffu.GetMonthRequests(companyClient, token, userId, first.Year(), first.Month())
			woffu.EnrichCalendarDays(month, reqs, nil)
			days = append(days, month...)
		}
	}).Run()

	today := now.Format("2006-01-02")
	var opts []huh.Option[string]
	for _, d := range days {
		if d.Date < today || d.Status != "working" || len(opts) >= 60 {
			continue
		}
		busy := false
		for _, r := range d.Requests {
			if r.Status == "pending" || r.Status == "approved" {
				busy = true
			}
		}
		if busy {
			continue
		}
		mode := stFaint.Render("office")
		if d.Mode == "remote" {
			mode = stFaint.Render("remote")
		}
		dt, _ := time.Parse("2006-01-02", d.Date)
		label := fmt.Sprintf("%-16s %s", dt.Format("Mon 2 Jan"), mode)
		if d.Date == today {
			label = fmt.Sprintf("%-16s %s", "today", mode)
		}
		opts = append(opts, huh.NewOption(label, d.Date))
	}
	if len(opts) == 0 {
		return nil, fmt.Errorf("no free working days in the next months")
	}
	var picked []string
	err := newForm(huh.NewGroup(huh.NewMultiSelect[string]().
		Title("Which days? · " + uiTrim(t.Name)).
		Description("x or space selects · ctrl+a all · / search · enter continues\nOnly working days without a request are listed.").
		Options(opts...).Height(16).Value(&picked))).Run()
	sort.Strings(picked)
	return picked, err
}

// confirmRequest shows exactly what will be sent and the balance after.
func confirmRequest(companyClient *woffu.Client, token string, t *woffu.RequestType, dates []string) (bool, error) {
	var parts []string
	for _, d := range dates {
		parts = append(parts, uiDate(d))
	}
	desc := strings.Join(parts, ", ")
	if events, err := woffu.GetAvailableEvents(companyClient, token); err == nil {
		for _, e := range events {
			if strings.Contains(strings.ToLower(uiTrim(e.Name)), strings.ToLower(uiTrim(t.Name))) {
				desc += "\n\nBalance: " + uiAmount(e.Available, e.Unit)
				if strings.HasPrefix(strings.ToLower(e.Unit), "day") {
					desc += " → " + uiAmount(e.Available-float64(len(dates)), e.Unit) + " after this"
				}
			}
		}
	}
	desc += "\n\nYour manager gets one request per day in Woffu."
	ok := true
	err := newForm(huh.NewGroup(huh.NewConfirm().
		Title(fmt.Sprintf("Request %s for %d %s?", uiTrim(t.Name), len(dates), pluralS(len(dates), "day", "days"))).
		Description(desc).
		Affirmative("Send").Negative("Cancel").Value(&ok))).Run()
	return ok, err
}

func pluralS(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
