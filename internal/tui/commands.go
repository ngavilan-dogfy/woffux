package tui

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ngavilan-dogfy/woffux/internal/agent"
	"github.com/ngavilan-dogfy/woffux/internal/config"
	gh "github.com/ngavilan-dogfy/woffux/internal/github"
	"github.com/ngavilan-dogfy/woffux/internal/notify"
	"github.com/ngavilan-dogfy/woffux/internal/woffu"
)

// ── Fetching ──

func (d *Dashboard) refreshData() tea.Cmd {
	d.refreshing = true
	return tea.Batch(d.fetchData(), d.fetchAutoStatus(), d.fetchAgentStatus())
}

// fetchData loads everything the dashboard shows: today's status, balances,
// the current month (for the week view) and the month open in the calendar.
func (d *Dashboard) fetchData() tea.Cmd {
	client, companyClient := d.client, d.companyClient
	cfg := *d.cfg
	password := d.password
	now := d.clock()
	calYear, calMonth := now.Year(), now.Month()
	if d.cal != nil {
		calYear, calMonth = d.cal.year, d.cal.month
	}
	sameMonth := calYear == now.Year() && calMonth == now.Month()

	return func() tea.Msg {
		token, err := woffu.AuthenticateCached(client, companyClient, cfg.WoffuEmail, password)
		if err != nil {
			return errMsg{err}
		}

		var (
			msg       = dataMsg{token: token, calYear: calYear, calMonth: calMonth, fetchedAt: time.Now()}
			infoErr   error
			eventsErr error
			wg        sync.WaitGroup
		)

		wg.Add(5)
		go func() { defer wg.Done(); msg.profile, _ = woffu.GetUserProfile(companyClient, token) }()
		go func() {
			defer wg.Done()
			msg.signInfo, infoErr = woffu.GetSignInfo(companyClient, token,
				cfg.Latitude, cfg.Longitude, cfg.HomeLatitude, cfg.HomeLongitude)
		}()
		go func() { defer wg.Done(); msg.events, eventsErr = woffu.GetAvailableEvents(companyClient, token) }()
		go func() { defer wg.Done(); msg.slots, _ = woffu.GetTodaySlots(companyClient, token) }()
		go func() {
			defer wg.Done()
			msg.userId, msg.companyId, _ = woffu.GetUserIds(companyClient, token)
			days, signs := loadMonth(companyClient, token, msg.userId, now.Year(), now.Month())
			msg.homeDays, msg.homeSigns = days, signs
			if sameMonth {
				msg.calDays = days
			} else {
				msg.calDays, _ = loadMonth(companyClient, token, msg.userId, calYear, calMonth)
			}
			msg.calFetched = msg.calDays != nil
		}()
		wg.Wait()

		if infoErr != nil {
			return errMsg{infoErr}
		}
		if eventsErr != nil {
			return errMsg{eventsErr}
		}
		return msg
	}
}

// loadMonth fetches a month's calendar enriched with requests and signs.
func loadMonth(companyClient *woffu.Client, token string, userId, year int, month time.Month) ([]woffu.CalendarDay, []woffu.SignRecord) {
	var (
		days  []woffu.CalendarDay
		reqs  []woffu.UserRequest
		signs []woffu.SignRecord
		wg    sync.WaitGroup
	)
	wg.Add(1)
	go func() { defer wg.Done(); days, _ = woffu.GetCalendarMonthYM(companyClient, token, year, month) }()
	if userId > 0 {
		wg.Add(2)
		go func() { defer wg.Done(); reqs, _ = woffu.GetMonthRequests(companyClient, token, userId, year, month) }()
		go func() { defer wg.Done(); signs, _ = woffu.GetMonthSigns(companyClient, token, year, month) }()
	}
	wg.Wait()
	if days != nil {
		woffu.EnrichCalendarDays(days, reqs, signs)
	}
	return days, signs
}

// fetchCalendarData loads only the month shown in the calendar, reusing the
// cached token so month flipping stays fast.
func (d *Dashboard) fetchCalendarData() tea.Cmd {
	if d.cal == nil {
		return nil
	}
	if d.token == "" {
		return d.refreshData()
	}
	d.refreshing = true
	companyClient, token, userId := d.companyClient, d.token, d.userId
	year, month := d.cal.year, d.cal.month
	return func() tea.Msg {
		days, _ := loadMonth(companyClient, token, userId, year, month)
		if days == nil {
			return errMsg{fmt.Errorf("couldn't load %s %d", month, year)}
		}
		return calendarDataMsg{year: year, month: month, days: days}
	}
}

func (d *Dashboard) fetchAutoStatus() tea.Cmd {
	repo := strings.TrimSpace(d.cfg.GithubFork)
	if repo == "" {
		return nil
	}
	cfgCopy := *d.cfg
	return func() tea.Msg {
		enabled, err := gh.IsAutoSignEnabled(repo)
		if err != nil {
			return nil // not critical: the panel keeps saying "checking"
		}
		msg := autoStatusMsg{repo: repo, enabled: enabled}
		inSync, syncErr := gh.CheckWorkflowSync(repo, &cfgCopy)
		if syncErr != nil {
			msg.syncErr = syncErr.Error()
		} else {
			msg.syncChecked = true
			msg.inSync = inSync
		}
		if createdAt, conclusion, ok, err := gh.LastScheduledRun(repo); err == nil && ok {
			if t, parseErr := time.Parse(time.RFC3339, createdAt); parseErr == nil {
				msg.lastRunAt = t
				msg.lastRunOK = conclusion == "success"
			}
		}
		return msg
	}
}

// ── Signing ──

// askSign opens the sign confirmation (signing is irreversible in Woffu, so
// it never fires from a single keypress).
func (d *Dashboard) askSign() tea.Cmd {
	if cmd, busy := d.guardBusy(); busy {
		return cmd
	}
	if d.signInfo == nil {
		return d.showToast("Still loading today's data — try again in a second", toastInfo)
	}
	p := d.plan()
	dir := d.pendingSignAction()
	accent := cOK
	subject := "Clock IN"
	if dir == "OUT" {
		accent = cWarn
		subject = "Clock OUT"
	}
	lines := []string{
		sText.Render(d.clock().Format("15:04")) + sFaint.Render(" · ") + sText.Render(modeLabel(d.signInfo.Mode)) +
			sFaint.Render(" location"),
	}
	switch {
	case dir == "OUT":
		lines = append(lines, sFaint.Render("Ends a stretch that started at "+p.lastIn+" · "+formatDuration(p.workedDur)+" worked today"))
	case p.signCount > 0:
		lines = append(lines, sFaint.Render("Back from your break · out since "+p.lastOut))
	case p.next != nil:
		lines = append(lines, sFaint.Render("Starts your day · scheduled at "+clockOf(p.next.minute)))
	}
	danger := false
	if p.kind != kindWorking && dir == "IN" {
		danger = true
		lines = append(lines, "", sWarn.Render("Heads up: ")+sText.Render(dayKindSentence(p)),
			sFaint.Render("Woffu will record hours on a day you're not expected to work."))
	}
	d.openConfirm(&confirmSpec{
		title:   "Sign now",
		subject: subject,
		lines:   lines,
		yes:     "Sign",
		danger:  danger,
		accent:  accent,
		onYes:   d.doSign,
	})
	return nil
}

func (d *Dashboard) doSign() tea.Cmd {
	if d.signInfo == nil || d.signing || d.busy != "" {
		return nil
	}
	d.signing = true
	d.startBusy("Signing in Woffu")
	companyClient, token := d.companyClient, d.token
	info := *d.signInfo
	tgCfg := notify.TelegramConfig{BotToken: d.cfg.Telegram.BotToken, ChatID: d.cfg.Telegram.ChatID}

	return func() tea.Msg {
		before, beforeErr := woffu.GetTodaySlots(companyClient, token)
		if err := woffu.DoSign(companyClient, token, info.Latitude, info.Longitude); err != nil {
			_ = notify.SendFailedNotification(tgCfg, info.Date, fmt.Sprintf("Sign request failed: %s", err))
			return actionErrMsg{err}
		}
		verified := false
		if beforeErr == nil {
			if err := woffu.VerifySignRegistered(companyClient, token, woffu.IsSignedIn(before)); err != nil {
				_ = notify.SendFailedNotification(tgCfg, info.Date, err.Error())
				return actionErrMsg{fmt.Errorf("sign NOT verified: %w", err)}
			}
			verified = true
		}
		_ = notify.SendSignedNotification(tgCfg, &info)
		return signDoneMsg{verified: verified}
	}
}

// ── Autopilot ──

func (d *Dashboard) toggleAgent(enable bool) tea.Cmd {
	if enable {
		d.startBusy("Installing the local agent")
		return func() tea.Msg { return agentToggleMsg{enabled: true, err: agent.Install()} }
	}
	d.startBusy("Removing the local agent")
	return func() tea.Msg { return agentToggleMsg{enabled: false, err: agent.Uninstall()} }
}

func (d *Dashboard) toggleAuto(enable bool) tea.Cmd {
	cfgCopy := *d.cfg
	cfg := &cfgCopy
	repo := cfg.GithubFork
	if enable {
		d.startBusy("Enabling GitHub signer")
	} else {
		d.startBusy("Disabling GitHub signer")
	}
	return func() tea.Msg {
		var err error
		if enable {
			err = gh.EnableAndRefreshAutoSign(cfg)
		} else {
			err = gh.DisableAutoSign(repo)
		}
		if err != nil {
			return actionErrMsg{err}
		}
		return autoToggleMsg{enabled: enable, inSync: enable}
	}
}

func (d *Dashboard) syncGitHub() tea.Cmd {
	cfgCopy := *d.cfg
	cfg := &cfgCopy
	password := d.password
	d.startBusy("Syncing to GitHub")
	return func() tea.Msg {
		if password == "" {
			pw, err := config.GetPassword(cfg.WoffuEmail)
			if err != nil {
				return actionErrMsg{fmt.Errorf("get password: %w", err)}
			}
			password = pw
		}
		if err := gh.SyncSecrets(cfg, password); err != nil {
			return actionErrMsg{fmt.Errorf("sync secrets: %w", err)}
		}
		if err := gh.SyncWorkflows(cfg); err != nil {
			return actionErrMsg{fmt.Errorf("sync workflows: %w", err)}
		}
		reloaded := false
		if cfg.GithubFork != "" {
			var err error
			reloaded, err = gh.ReloadAutoSignIfEnabled(cfg.GithubFork)
			if err != nil {
				return actionErrMsg{fmt.Errorf("reload auto-sign: %w", err)}
			}
		}
		return syncDoneMsg{reloaded: reloaded}
	}
}

// ── Schedules ──

func (d *Dashboard) applyPreset(name string) tea.Cmd {
	d.startBusy(fmt.Sprintf("Switching to %q", name))
	return func() tea.Msg {
		freshCfg, err := config.Load()
		if err != nil {
			return actionErrMsg{fmt.Errorf("load config: %w", err)}
		}
		if !freshCfg.LoadSchedulePreset(name) {
			return actionErrMsg{fmt.Errorf("preset %q not found", name)}
		}
		if err := config.Save(freshCfg); err != nil {
			return actionErrMsg{fmt.Errorf("save config: %w", err)}
		}
		var syncErr error
		if freshCfg.GithubFork != "" {
			if _, err := gh.SyncWorkflowsAndRefresh(freshCfg); err != nil {
				syncErr = fmt.Errorf("sync workflows: %w", err)
			}
		}
		return presetAppliedMsg{name: name, cfg: freshCfg, syncErr: syncErr}
	}
}

func (d *Dashboard) savePreset(name string) tea.Cmd {
	name = config.NormalizePresetName(name)
	schedule := d.cfg.Schedule
	return func() tea.Msg {
		freshCfg, err := config.Load()
		if err != nil {
			return actionErrMsg{fmt.Errorf("load config: %w", err)}
		}
		if err := freshCfg.SaveSchedulePreset(name, schedule); err != nil {
			return actionErrMsg{err}
		}
		freshCfg.ActiveSchedule = name
		if err := config.Save(freshCfg); err != nil {
			return actionErrMsg{fmt.Errorf("save config: %w", err)}
		}
		return presetSavedMsg{name: name}
	}
}

// execWoffux suspends the TUI and runs an interactive woffux subcommand.
func (d *Dashboard) execWoffux(label string, args ...string) tea.Cmd {
	bin, err := os.Executable()
	if err != nil {
		bin = "woffux"
	}
	c := exec.Command(bin, args...)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return execDoneMsg{err: err, label: label}
	})
}

// ── Requests ──

// requestKind is one of the request types offered from the calendar.
type requestKind struct {
	key    string // action key
	name   string // what the user sees
	search string // substring matched against Woffu request type names
	hotkey string
}

var requestKinds = []requestKind{
	{key: "telework", name: "Telework", search: "teletrabajo", hotkey: "t"},
	{key: "vacation", name: "Vacation", search: "vacaciones", hotkey: "v"},
	{key: "personal", name: "Personal day", search: "asuntos propios", hotkey: "p"},
	{key: "hours", name: "Hours pool", search: "bolsa de horas", hotkey: "b"},
}

func findRequestKind(key string) (requestKind, bool) {
	for _, k := range requestKinds {
		if k.key == key {
			return k, true
		}
	}
	return requestKind{}, false
}

// submitRequests creates one request per date for the given kind.
func (d *Dashboard) submitRequests(kind requestKind, dates []string) tea.Cmd {
	if len(dates) == 0 {
		return nil
	}
	d.startBusy(fmt.Sprintf("Requesting %s for %d %s", strings.ToLower(kind.name), len(dates), plural(len(dates), "day", "days")))
	companyClient, token := d.companyClient, d.token
	cachedUser, cachedCompany := d.userId, d.companyId
	return func() tea.Msg {
		types, err := woffu.GetRequestTypes(companyClient, token)
		if err != nil {
			return actionErrMsg{err}
		}
		var matched *woffu.RequestType
		for i, t := range types {
			if strings.Contains(strings.ToLower(t.Name), kind.search) {
				matched = &types[i]
				break
			}
		}
		if matched == nil {
			return actionErrMsg{fmt.Errorf("your company has no %q request type", kind.search)}
		}
		userId, companyId := cachedUser, cachedCompany
		if userId == 0 || companyId == 0 {
			if userId, companyId, err = woffu.GetUserIds(companyClient, token); err != nil {
				return actionErrMsg{err}
			}
		}
		ok, failed := 0, 0
		var lastErr error
		for _, date := range dates {
			if err := woffu.CreateRequest(companyClient, token, userId, companyId, matched.ID, date, date, matched.IsVacation); err != nil {
				failed++
				lastErr = err
				continue
			}
			ok++
		}
		if ok == 0 && lastErr != nil {
			return actionErrMsg{fmt.Errorf("request failed: %w", lastErr)}
		}
		return requestDoneMsg{count: ok, failed: failed, action: "sent"}
	}
}

// cancelRequests cancels the given request IDs.
func (d *Dashboard) cancelRequests(ids []int) tea.Cmd {
	if len(ids) == 0 {
		return nil
	}
	d.startBusy(fmt.Sprintf("Cancelling %d %s", len(ids), plural(len(ids), "request", "requests")))
	companyClient, token := d.companyClient, d.token
	return func() tea.Msg {
		ok, failed := 0, 0
		var lastErr error
		for _, id := range ids {
			if err := woffu.CancelRequest(companyClient, token, id); err != nil {
				failed++
				lastErr = err
				continue
			}
			ok++
		}
		if ok == 0 && lastErr != nil {
			return actionErrMsg{fmt.Errorf("cancel failed: %w", lastErr)}
		}
		return requestDoneMsg{count: ok, failed: failed, action: "cancelled"}
	}
}

// ── Browser ──

func (d *Dashboard) openWoffu() tea.Cmd {
	if strings.TrimSpace(d.cfg.WoffuCompanyURL) == "" {
		return d.showToast("Woffu URL isn't configured — run woffux setup", toastErr)
	}
	openBrowser(strings.TrimRight(d.cfg.WoffuCompanyURL, "/") + "/v2")
	return d.showToast("Opened Woffu in your browser", toastOK)
}

func (d *Dashboard) openGitHub() tea.Cmd {
	if d.cfg.GithubFork == "" {
		return d.showToast("GitHub isn't set up — run woffux setup", toastErr)
	}
	openBrowser("https://github.com/" + d.cfg.GithubFork + "/actions")
	return d.showToast("Opened GitHub Actions in your browser", toastOK)
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	}
	if cmd != nil {
		_ = cmd.Start()
	}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
