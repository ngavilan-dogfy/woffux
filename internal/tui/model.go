package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ngavilan-dogfy/woffux/internal/agent"
	"github.com/ngavilan-dogfy/woffux/internal/config"
	"github.com/ngavilan-dogfy/woffux/internal/woffu"
)

// ── Tabs ──

const (
	tabToday = iota
	tabCalendar
	tabBalance
	tabCount
)

var tabNames = [tabCount]string{"Today", "Calendar", "Balance"}

// ── Messages ──

type dataMsg struct {
	token      string
	signInfo   *woffu.SignInfo
	events     []woffu.AvailableUserEvent
	profile    *woffu.UserProfile
	slots      []woffu.SignSlot
	homeDays   []woffu.CalendarDay // current month
	homeSigns  []woffu.SignRecord  // current month
	calYear    int
	calMonth   time.Month
	calDays    []woffu.CalendarDay // month shown in the calendar tab
	userId     int
	companyId  int
	fetchedAt  time.Time
	calFetched bool
}

// errMsg is a failed data fetch; actionErrMsg is a failed user action
// (sign, request, toggle…). Keeping them apart matters: a background fetch
// failing must never release the "sign in flight" guard.
type errMsg struct{ err error }
type actionErrMsg struct{ err error }
type signDoneMsg struct{ verified bool }
type agentStatusMsg struct{ active bool }
type agentToggleMsg struct {
	enabled bool
	err     error
}
type autoToggleMsg struct {
	enabled bool
	inSync  bool
}
type autoStatusMsg struct {
	repo        string
	enabled     bool
	syncChecked bool
	inSync      bool
	syncErr     string
	lastRunAt   time.Time
	lastRunOK   bool
}
type syncDoneMsg struct{ reloaded bool }
type presetAppliedMsg struct {
	name    string
	cfg     *config.Config
	syncErr error
}
type presetSavedMsg struct{ name string }
type clearToastMsg struct{ id int }
type tickMsg time.Time
type calendarDataMsg struct {
	year  int
	month time.Month
	days  []woffu.CalendarDay
}
type execDoneMsg struct {
	err   error
	label string
}
type requestDoneMsg struct {
	count  int
	failed int
	action string
}

// ── Overlays ──

type overlayKind int

const (
	overlayNone    overlayKind = iota
	overlayPalette             // command palette (⏎ / : / ctrl+k)
	overlayDay                 // calendar day / selection actions
	overlayConfirm             // "are you sure" for anything irreversible
	overlayInput               // save-as-preset name
	overlayHelp                // keyboard reference
)

// confirmSpec describes a pending confirmation.
type confirmSpec struct {
	title   string
	lines   []string
	yes     string // label of the confirm button
	danger  bool
	onYes   func() tea.Cmd
	accent  lipgloss.Color
	subject string // big line under the title (e.g. "Clock IN")
}

// toast is the transient message shown in the footer.
type toastKind int

const (
	toastInfo toastKind = iota
	toastOK
	toastErr
)

type toast struct {
	id   int
	text string
	kind toastKind
}

// ── Model ──

type Dashboard struct {
	client        *woffu.Client
	companyClient *woffu.Client
	cfg           *config.Config
	password      string

	// Data
	loading     bool // first load in flight (no data yet)
	refreshing  bool // background refresh in flight
	token       string
	userId      int
	companyId   int
	signInfo    *woffu.SignInfo
	events      []woffu.AvailableUserEvent
	profile     *woffu.UserProfile
	slots       []woffu.SignSlot
	homeDays    []woffu.CalendarDay
	monthSigns  []woffu.SignRecord
	fetchedAt   time.Time
	loadErr     error // last fetch error when there is no data to show
	autoActive  *bool // GitHub auto-sign (nil = unknown)
	autoInSync  *bool
	autoSyncErr string
	lastRunAt   time.Time
	lastRunOK   bool
	agentActive *bool // local launchd agent (nil = unknown/unsupported)

	// UI
	activeTab int
	overlay   overlayKind
	cursor    int // cursor inside the open menu overlay
	query     string
	input     string
	confirm   *confirmSpec
	confirmAt time.Time // when the confirmation opened (key-repeat guard)
	toast     toast
	busy      string // label of the long-running action in flight
	signing   bool
	cal       *calendarGrid
	spin      spinner.Model
	ticks     int

	width, height int

	// now is overridable for tests and previews.
	now func() time.Time
}

func NewDashboard(client, companyClient *woffu.Client, cfg *config.Config, password string) *Dashboard {
	d := &Dashboard{
		client:        client,
		companyClient: companyClient,
		cfg:           cfg,
		password:      password,
		loading:       true,
		activeTab:     tabToday,
	}
	d.initSpinner()
	return d
}

func (d *Dashboard) initSpinner() {
	d.spin = spinner.New(
		spinner.WithSpinner(spinner.MiniDot),
		spinner.WithStyle(lipgloss.NewStyle().Foreground(cBrand)),
	)
}

func (d *Dashboard) clock() time.Time {
	if d.now != nil {
		return d.now()
	}
	return time.Now()
}

func (d *Dashboard) applyConfig(cfg *config.Config) {
	oldFork := ""
	if d.cfg != nil {
		oldFork = strings.TrimSpace(d.cfg.GithubFork)
	}
	newFork := strings.TrimSpace(cfg.GithubFork)
	d.cfg = cfg
	if newFork == "" || newFork != oldFork {
		d.autoActive = nil
		d.autoInSync = nil
		d.autoSyncErr = ""
	}
}

func (d *Dashboard) reloadConfig() {
	newCfg, err := config.Load()
	if err != nil {
		return
	}
	d.applyConfig(newCfg)
	if pw, pwErr := config.GetPassword(newCfg.WoffuEmail); pwErr == nil {
		d.password = pw
	}
}

func (d *Dashboard) Init() tea.Cmd {
	return tea.Batch(d.fetchData(), d.fetchAutoStatus(), d.fetchAgentStatus(), d.tick(), d.spin.Tick)
}

// ── Update ──

func (d *Dashboard) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		d.width, d.height = msg.Width, msg.Height

	case tea.KeyMsg:
		return d.handleKey(msg)

	case spinner.TickMsg:
		var cmd tea.Cmd
		d.spin, cmd = d.spin.Update(msg)
		return d, cmd

	case dataMsg:
		d.loading, d.refreshing = false, false
		d.loadErr = nil
		d.token = msg.token
		d.signInfo = msg.signInfo
		d.events = msg.events
		if msg.profile != nil {
			d.profile = msg.profile
		}
		d.slots = msg.slots
		d.homeDays = msg.homeDays
		d.monthSigns = msg.homeSigns
		d.userId, d.companyId = msg.userId, msg.companyId
		d.fetchedAt = msg.fetchedAt
		if d.cal == nil {
			now := d.clock()
			d.cal = newCalendarGrid(now.Year(), now.Month(), now)
		}
		if msg.homeDays != nil {
			now := d.clock()
			d.cal.cacheMonth(now.Year(), now.Month(), msg.homeDays)
		}
		if msg.calFetched {
			if d.cal.year == msg.calYear && d.cal.month == msg.calMonth {
				d.cal.setDays(msg.calDays)
			} else {
				d.cal.cacheMonth(msg.calYear, msg.calMonth, msg.calDays)
			}
		}

	case calendarDataMsg:
		d.refreshing = false
		if d.cal != nil {
			if d.cal.year == msg.year && d.cal.month == msg.month {
				d.cal.setDays(msg.days)
			} else {
				d.cal.cacheMonth(msg.year, msg.month, msg.days)
			}
		}

	case requestDoneMsg:
		d.busy = ""
		if d.cal != nil {
			d.cal.clearSelection()
			d.cal.forgetOtherMonths()
		}
		noun := "requests"
		if msg.count == 1 {
			noun = "request"
		}
		text := fmt.Sprintf("%d %s %s", msg.count, noun, msg.action)
		kind := toastOK
		if msg.failed > 0 {
			text += fmt.Sprintf(" · %d failed", msg.failed)
			kind = toastErr
		}
		return d, tea.Batch(d.showToast(text, kind), d.refreshData())

	case signDoneMsg:
		d.signing, d.busy = false, ""
		text := "Signed and verified in Woffu"
		if !msg.verified {
			text = "Signed (Woffu did not confirm yet — check in a minute)"
		}
		return d, tea.Batch(d.showToast(text, toastOK), d.fetchData(), d.fetchAutoStatus())

	case agentStatusMsg:
		v := msg.active
		d.agentActive = &v

	case agentToggleMsg:
		d.busy = ""
		if msg.err != nil {
			return d, tea.Batch(d.showToast("This Mac: "+msg.err.Error(), toastErr), d.fetchAgentStatus())
		}
		v := msg.enabled
		d.agentActive = &v
		if msg.enabled {
			return d, d.showToast("This Mac will now sign on time while it's awake", toastOK)
		}
		return d, d.showToast("This Mac stopped signing", toastOK)

	case autoToggleMsg:
		d.busy = ""
		v := msg.enabled
		d.autoActive = &v
		d.autoSyncErr = ""
		if msg.enabled {
			inSync := msg.inSync
			d.autoInSync = &inSync
			return d, d.showToast("GitHub backup signer enabled", toastOK)
		}
		d.autoInSync = nil
		return d, d.showToast("GitHub backup signer disabled", toastOK)

	case autoStatusMsg:
		if strings.TrimSpace(msg.repo) != strings.TrimSpace(d.cfg.GithubFork) {
			return d, nil
		}
		v := msg.enabled
		d.autoActive = &v
		d.autoSyncErr = msg.syncErr
		d.lastRunAt = msg.lastRunAt
		d.lastRunOK = msg.lastRunOK
		if msg.syncChecked {
			inSync := msg.inSync
			d.autoInSync = &inSync
		} else {
			d.autoInSync = nil
		}

	case syncDoneMsg:
		d.busy = ""
		text := "Synced to GitHub · schedule refreshed"
		if !msg.reloaded {
			text = "Synced to GitHub (backup signer is off)"
		}
		return d, tea.Batch(d.showToast(text, toastOK), d.fetchAutoStatus())

	case presetAppliedMsg:
		d.busy = ""
		d.applyConfig(msg.cfg)
		if msg.syncErr != nil {
			return d, tea.Batch(d.showToast(fmt.Sprintf("Switched to %q, but GitHub sync failed: %s", msg.name, msg.syncErr), toastErr), d.refreshData())
		}
		return d, tea.Batch(d.showToast(fmt.Sprintf("Now using the %q schedule", msg.name), toastOK), d.refreshData())

	case presetSavedMsg:
		d.busy = ""
		d.reloadConfig()
		return d, d.showToast(fmt.Sprintf("Saved schedule as %q", msg.name), toastOK)

	case execDoneMsg:
		if msg.err != nil {
			return d, d.showToast(msg.label+" not changed", toastInfo)
		}
		d.reloadConfig()
		return d, tea.Batch(d.showToast(msg.label+" updated", toastOK), d.refreshData())

	case errMsg:
		d.loading, d.refreshing = false, false
		if d.signInfo == nil {
			d.loadErr = msg.err
		}
		return d, d.showToast(friendlyError(msg.err), toastErr)

	case actionErrMsg:
		d.signing, d.busy = false, ""
		return d, tea.Batch(d.showToast(friendlyError(msg.err), toastErr), d.refreshData())

	case clearToastMsg:
		if msg.id == d.toast.id {
			d.toast = toast{id: d.toast.id}
		}

	case tickMsg:
		d.ticks++
		// Refresh every 5 minutes so signs made elsewhere (phone, agent,
		// GitHub) show up on their own. Never while the user is mid-action.
		if d.ticks%5 == 0 && d.overlay == overlayNone && !d.signing && !d.loading && !d.refreshing {
			d.refreshing = true
			return d, tea.Batch(d.tick(), d.fetchData(), d.fetchAgentStatus())
		}
		return d, d.tick()
	}
	return d, nil
}

// friendlyError turns transport errors into something a human can act on.
func friendlyError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	low := strings.ToLower(msg)
	switch {
	case strings.Contains(low, "no such host"), strings.Contains(low, "network is unreachable"),
		strings.Contains(low, "connection refused"), strings.Contains(low, "i/o timeout"):
		return "Can't reach Woffu — check your connection (r to retry)"
	case strings.Contains(low, "401"), strings.Contains(low, "unauthorized"), strings.Contains(low, "invalid_grant"):
		return "Woffu rejected the login — update your password with `woffux config edit`"
	}
	return msg
}

func (d *Dashboard) tick() tea.Cmd {
	return tea.Tick(time.Minute, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// showToast sets the footer message and schedules its removal.
func (d *Dashboard) showToast(text string, kind toastKind) tea.Cmd {
	d.toast = toast{id: d.toast.id + 1, text: text, kind: kind}
	id := d.toast.id
	dur := 4 * time.Second
	if kind == toastErr {
		dur = 8 * time.Second
	}
	return tea.Tick(dur, func(time.Time) tea.Msg { return clearToastMsg{id: id} })
}

// guardBusy refuses to start an action while another one is in flight
// (double signs flip IN/OUT; double requests duplicate them).
func (d *Dashboard) guardBusy() (tea.Cmd, bool) {
	if d.busy != "" || d.signing {
		return d.showToast("Hold on — still "+strings.ToLower(orDefault(d.busy, "working"))+"…", toastInfo), true
	}
	return nil, false
}

// startBusy marks a long action in flight; the footer shows a spinner.
func (d *Dashboard) startBusy(label string) {
	d.busy = label
	d.toast = toast{id: d.toast.id + 1}
}

func (d *Dashboard) fetchAgentStatus() tea.Cmd {
	if !agent.Supported() {
		return nil
	}
	return func() tea.Msg {
		return agentStatusMsg{active: agent.Installed() && agent.Loaded()}
	}
}

// ── Derived state ──

func (d *Dashboard) todayCalendarDay() *woffu.CalendarDay {
	key := d.clock().Format("2006-01-02")
	for i := range d.homeDays {
		if d.homeDays[i].Date == key {
			return &d.homeDays[i]
		}
	}
	return nil
}

func (d *Dashboard) plan() dayPlan {
	return buildDayPlan(d.clock(), d.signInfo, d.todayCalendarDay(), d.cfg.Schedule, d.slots)
}

func (d *Dashboard) needsAutoSync() bool {
	return d.autoActive != nil && *d.autoActive && d.autoInSync != nil && !*d.autoInSync
}

func (d *Dashboard) agentOn() bool  { return d.agentActive != nil && *d.agentActive }
func (d *Dashboard) githubOn() bool { return d.autoActive != nil && *d.autoActive }

func (d *Dashboard) anySignerActive() bool { return d.agentOn() || d.githubOn() }

// pendingSignAction tells which direction the next manual sign will take.
func (d *Dashboard) pendingSignAction() string {
	if woffu.IsSignedIn(d.slots) {
		return "OUT"
	}
	return "IN"
}
