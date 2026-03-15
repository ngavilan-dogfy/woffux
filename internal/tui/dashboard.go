package tui

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ngavilan-dogfy/woffuk-cli/internal/config"
	gh "github.com/ngavilan-dogfy/woffuk-cli/internal/github"
	"github.com/ngavilan-dogfy/woffuk-cli/internal/notify"
	"github.com/ngavilan-dogfy/woffuk-cli/internal/woffu"
)

// ── Tab indices ──

const (
	tabStatus   = 0
	tabEvents   = 1
	tabCalendar = 2
	tabCount    = 3
)

var tabNames = [tabCount]string{"Status", "Events", "Calendar"}

// ── Messages ──

type dataMsg struct {
	signInfo *woffu.SignInfo
	events   []woffu.AvailableUserEvent
	profile  *woffu.UserProfile
	slots    []woffu.SignSlot
}
type errMsg struct{ err error }
type signDoneMsg struct{}
type autoToggleMsg struct{ enabled bool }
type syncDoneMsg struct{}
type clearFlashMsg struct{}
type tickMsg time.Time

// ── Action items for the overlay menu ──

type action struct {
	key  string
	name string
	desc string
}

// ── Overlay kinds ──

type overlayKind int

const (
	overlayNone     overlayKind = iota
	overlayMenu                 // action menu
	overlaySignConf             // sign confirmation
	overlayAutoConf             // auto-sign toggle confirmation
)

// ── Dashboard model ──

type Dashboard struct {
	client        *woffu.Client
	companyClient *woffu.Client
	cfg           *config.Config
	password      string

	// Data
	loading    bool
	token      string
	signInfo   *woffu.SignInfo
	events     []woffu.AvailableUserEvent
	profile    *woffu.UserProfile
	slots      []woffu.SignSlot
	autoActive *bool // nil = unknown
	err        error

	// UI state
	activeTab  int
	signing    bool
	overlay    overlayKind
	menuCursor int
	autoTarget bool // what the auto-sign toggle overlay wants to set
	flash      string
	flashErr   bool

	// Layout
	width  int
	height int
}

func NewDashboard(client, companyClient *woffu.Client, cfg *config.Config, password string) *Dashboard {
	return &Dashboard{
		client:        client,
		companyClient: companyClient,
		cfg:           cfg,
		password:      password,
		loading:       true,
		activeTab:     tabStatus,
	}
}

func (d *Dashboard) Init() tea.Cmd {
	return tea.Batch(d.fetchData(), d.fetchAutoStatus(), d.tick())
}

// ── Update ──

func (d *Dashboard) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		d.width = msg.Width
		d.height = msg.Height

	case tea.KeyMsg:
		return d.handleKey(msg)

	case dataMsg:
		d.loading = false
		d.signInfo = msg.signInfo
		d.events = msg.events
		d.profile = msg.profile
		d.slots = msg.slots

	case signDoneMsg:
		d.signing = false
		d.setFlash("Signed successfully! Data refreshing...", false)
		return d, tea.Batch(d.fetchData(), d.fetchAutoStatus(), d.clearFlashAfter(3*time.Second))

	case autoToggleMsg:
		v := msg.enabled
		d.autoActive = &v
		if v {
			d.setFlash("Auto-signing enabled", false)
		} else {
			d.setFlash("Auto-signing disabled", false)
		}
		return d, d.clearFlashAfter(3 * time.Second)

	case syncDoneMsg:
		d.setFlash("Synced to GitHub", false)
		return d, d.clearFlashAfter(3 * time.Second)

	case errMsg:
		d.loading = false
		d.signing = false
		d.setFlash(msg.err.Error(), true)
		return d, d.clearFlashAfter(5 * time.Second)

	case clearFlashMsg:
		d.flash = ""

	case tickMsg:
		return d, d.tick()
	}

	return d, nil
}

// ── Key handling ──

func (d *Dashboard) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// ── Overlay: sign confirmation ──
	if d.overlay == overlaySignConf {
		switch key {
		case "y", "Y", "enter":
			d.overlay = overlayNone
			return d, d.trySign()
		case "n", "N", "esc", "q":
			d.overlay = overlayNone
		}
		return d, nil
	}

	// ── Overlay: auto-sign toggle confirmation ──
	if d.overlay == overlayAutoConf {
		switch key {
		case "y", "Y", "enter":
			d.overlay = overlayNone
			return d, d.toggleAuto(d.autoTarget)
		case "n", "N", "esc", "q":
			d.overlay = overlayNone
		}
		return d, nil
	}

	// ── Overlay: action menu ──
	if d.overlay == overlayMenu {
		actions := d.getActions()
		switch key {
		case "esc", "q":
			d.overlay = overlayNone
		case "up", "k":
			if d.menuCursor > 0 {
				d.menuCursor--
			}
		case "down", "j":
			if d.menuCursor < len(actions)-1 {
				d.menuCursor++
			}
		case "enter":
			d.overlay = overlayNone
			return d, d.executeAction(actions[d.menuCursor])
		}
		return d, nil
	}

	// ── Global keys ──
	switch key {
	case "q", "ctrl+c":
		return d, tea.Quit

	// Tab navigation
	case "tab":
		d.activeTab = (d.activeTab + 1) % tabCount
	case "shift+tab":
		d.activeTab = (d.activeTab - 1 + tabCount) % tabCount
	case "1":
		d.activeTab = tabStatus
	case "2":
		d.activeTab = tabEvents
	case "3":
		d.activeTab = tabCalendar

	// Actions
	case "enter":
		d.overlay = overlayMenu
		d.menuCursor = 0
	case "s":
		d.overlay = overlaySignConf
	case "a":
		if d.autoActive != nil {
			d.autoTarget = !*d.autoActive
			d.overlay = overlayAutoConf
		} else if d.cfg.GithubFork == "" {
			d.setFlash("Auto-sign not set up. Run woffuk setup.", true)
			return d, d.clearFlashAfter(3 * time.Second)
		}
	case "r":
		d.loading = true
		d.flash = ""
		return d, tea.Batch(d.fetchData(), d.fetchAutoStatus())
	case "o":
		openBrowserCmd(d.cfg.WoffuCompanyURL + "/v2")
		d.setFlash("Opened Woffu in browser", false)
		return d, d.clearFlashAfter(2 * time.Second)
	case "g":
		if d.cfg.GithubFork != "" {
			openBrowserCmd("https://github.com/" + d.cfg.GithubFork + "/actions")
			d.setFlash("Opened GitHub Actions in browser", false)
			return d, d.clearFlashAfter(2 * time.Second)
		} else {
			d.setFlash("GitHub not configured — run woffuk setup", true)
			return d, d.clearFlashAfter(3 * time.Second)
		}
	}

	return d, nil
}

// ── View ──

func (d *Dashboard) View() string {
	if d.width == 0 {
		return ""
	}

	var sections []string

	// Header bar
	sections = append(sections, d.renderHeader())

	// Tab bar
	sections = append(sections, d.renderTabBar())

	// Tab content
	if d.loading {
		sections = append(sections, "\n"+sDimmed.Render("  Loading..."))
	} else {
		switch d.activeTab {
		case tabStatus:
			sections = append(sections, d.renderStatusTab())
		case tabEvents:
			sections = append(sections, d.renderEventsTab())
		case tabCalendar:
			sections = append(sections, d.renderCalendarTab())
		}
	}

	// Flash message
	if d.flash != "" {
		icon := sFlashSuccess.Render("  ✓ ")
		if d.flashErr {
			icon = sFlashError.Render("  ✗ ")
		}
		sections = append(sections, "\n"+icon+d.flash)
	}

	// Footer help
	sections = append(sections, d.renderHelp())

	dashboard := strings.Join(sections, "\n")

	// Overlays — render fullscreen centered (no ANSI string splicing)
	switch d.overlay {
	case overlayMenu:
		return d.renderOverlayMenu()
	case overlaySignConf:
		return d.renderOverlayConfirm("Sign now?", "Clock in/out on Woffu right now.", "y/enter", "n/esc")
	case overlayAutoConf:
		verb := "Enable"
		desc := "Resume GitHub Actions auto-signing."
		if !d.autoTarget {
			verb = "Disable"
			desc = "Stop GitHub Actions from signing automatically."
		}
		return d.renderOverlayConfirm(verb+" auto-sign?", desc, "y/enter", "n/esc")
	}

	return dashboard
}

// ── Render: header ──

func (d *Dashboard) renderHeader() string {
	name := "woffuk"
	if d.profile != nil {
		name = fmt.Sprintf("woffuk — %s", d.profile.FullName)
	}
	left := sTitle.Render(name)
	right := sDimmed.Render(time.Now().Format("15:04"))
	gap := d.width - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if gap < 0 {
		gap = 0
	}
	return lipgloss.NewStyle().Background(colorBarBg).Width(d.width).
		Render(left + strings.Repeat(" ", gap) + right)
}

// ── Render: tab bar ──

func (d *Dashboard) renderTabBar() string {
	var tabs []string
	for i, name := range tabNames {
		tabs = append(tabs, " "+tabStyle(name, i == d.activeTab)+" ")
	}
	bar := strings.Join(tabs, sDimmed.Render("|"))
	return "  " + bar
}

// ── Render: Status tab ──

func (d *Dashboard) renderStatusTab() string {
	var parts []string

	// Sign info box
	if d.signInfo != nil {
		info := d.signInfo

		workingDay := sSuccess.Render("yes")
		if !info.IsWorkingDay {
			workingDay = sDanger.Render("no")
		}

		modeStyle := sDimmed
		if info.Mode == woffu.SignModeRemote {
			modeStyle = sSuccess
		}
		mode := modeStyle.Render(fmt.Sprintf("%s %s", info.Mode.Emoji(), info.Mode.Label()))

		rows := []string{
			sLabel.Render("Date") + sValue.Render(info.Date),
			sLabel.Render("Working day") + workingDay,
			sLabel.Render("Mode") + mode,
		}
		if info.IsWorkingDay {
			rows = append(rows, sLabel.Render("Coordinates")+sDimmed.Render(fmt.Sprintf("%.4f, %.4f", info.Latitude, info.Longitude)))
		}

		// Sign status from slots
		rows = append(rows, sLabel.Render("Signed")+d.signStatusText())

		parts = append(parts, "\n"+sBox.Render(strings.Join(rows, "\n")))
	}

	// Today's slots
	if len(d.slots) > 0 {
		parts = append(parts, d.renderSlots())
	}

	// Schedule
	parts = append(parts, d.renderSchedule())

	// Auto-sign status
	parts = append(parts, d.renderAutoSign())

	return strings.Join(parts, "\n")
}

func (d *Dashboard) signStatusText() string {
	if len(d.slots) == 0 {
		return sDanger.Render("not signed yet")
	}

	// Check last slot to determine current state
	lastSlot := d.slots[len(d.slots)-1]
	if lastSlot.Out != "" {
		return sSuccess.Render(fmt.Sprintf("clocked out (%d signs)", len(d.slots)*2))
	}
	if lastSlot.In != "" {
		return sSuccess.Render("clocked in")
	}

	return sDimmed.Render(fmt.Sprintf("%d slots", len(d.slots)))
}

func (d *Dashboard) renderSlots() string {
	var rows []string
	for i, s := range d.slots {
		in := sDimmed.Render("  —  ")
		out := sDimmed.Render("  —  ")
		if s.In != "" {
			// Extract time from datetime
			inTime := extractTime(s.In)
			in = sSignIn.Render("IN") + "  " + sValue.Render(inTime)
		}
		if s.Out != "" {
			outTime := extractTime(s.Out)
			out = sSignOut.Render("OUT") + " " + sValue.Render(outTime)
		}
		rows = append(rows, fmt.Sprintf("    %d. %s   %s", i+1, in, out))
	}
	return "\n" + sSubtitle.Render("  Today's signs") + "\n" + strings.Join(rows, "\n")
}

func extractTime(datetime string) string {
	// "2026-03-15T08:32:00.000" → "08:32"
	if idx := strings.Index(datetime, "T"); idx != -1 {
		time := datetime[idx+1:]
		if len(time) >= 5 {
			return time[:5]
		}
	}
	return datetime
}

// ── Render: Events tab ──

func (d *Dashboard) renderEventsTab() string {
	if len(d.events) == 0 {
		return "\n" + sDimmed.Render("  No events available.")
	}

	var rows []string
	for _, e := range d.events {
		name := lipgloss.NewStyle().Foreground(colorMuted).Width(40).Render(e.Name)
		val := sValue.Render(fmt.Sprintf("%6.0f %s", e.Available, e.Unit))
		rows = append(rows, "  "+name+val)
	}
	return "\n" + sSubtitle.Render("  Available events") + "\n" + strings.Join(rows, "\n")
}

// ── Render: Calendar tab ──

func (d *Dashboard) renderCalendarTab() string {
	if d.signInfo == nil || len(d.signInfo.NextEvents) == 0 {
		return "\n" + sDimmed.Render("  No upcoming holidays or events.")
	}

	var rows []string
	for _, e := range d.signInfo.NextEvents {
		name := ""
		if len(e.Names) > 0 {
			name = " " + e.Names[0]
		}
		rows = append(rows, "  "+sDimmed.Render("  "+e.Date)+name)
	}
	return "\n" + sSubtitle.Render("  Upcoming holidays & events") + "\n" + strings.Join(rows, "\n")
}

// ── Render: schedule section ──

func (d *Dashboard) renderSchedule() string {
	s := d.cfg.Schedule
	days := []struct {
		n string
		d config.DaySchedule
	}{{"Mon", s.Monday}, {"Tue", s.Tuesday}, {"Wed", s.Wednesday}, {"Thu", s.Thursday}, {"Fri", s.Friday}}

	var parts []string
	for _, dd := range days {
		if !dd.d.Enabled {
			continue
		}
		line := "  " + sDimmed.Render(dd.n) + "  "
		for i, t := range dd.d.Times {
			if i%2 == 0 {
				line += sSignIn.Render("IN") + " " + t.Time + "  "
			} else {
				line += sSignOut.Render("OUT") + " " + t.Time + "  "
			}
		}
		parts = append(parts, line)
	}
	return "\n" + sSubtitle.Render("  Schedule") + "\n" + strings.Join(parts, "\n")
}

// ── Render: auto-sign status ──

func (d *Dashboard) renderAutoSign() string {
	status := sDimmed.Render("checking...")
	if d.autoActive != nil {
		if *d.autoActive {
			status = sSuccess.Render("active")
		} else {
			status = sDanger.Render("disabled")
		}
	} else if d.cfg.GithubFork == "" {
		status = sDimmed.Render("not set up")
	}
	return "\n" + sLabel.Render("  Auto-sign") + status
}

// ── Render: footer help ──

func (d *Dashboard) renderHelp() string {
	var hints []string

	switch d.activeTab {
	case tabStatus:
		hints = []string{
			hint("s", "sign"),
			hint("a", "auto-sign"),
			hint("r", "refresh"),
			hint("o", "woffu"),
			hint("g", "github"),
		}
	case tabEvents:
		hints = []string{
			hint("r", "refresh"),
			hint("o", "woffu"),
		}
	case tabCalendar:
		hints = []string{
			hint("r", "refresh"),
			hint("o", "woffu"),
		}
	}

	// Tab navigation + quit always shown
	hints = append(hints,
		hint("tab", "next"),
		hint("1-3", "tabs"),
		hint("enter", "menu"),
		hint("q", "quit"),
	)

	return "\n" + sDimmed.Render("  ") + strings.Join(hints, "  ")
}

// ── Action menu ──

func (d *Dashboard) getActions() []action {
	actions := []action{
		{key: "sign", name: "Sign now", desc: "Clock in/out right now"},
	}

	if d.autoActive != nil {
		if *d.autoActive {
			actions = append(actions, action{key: "auto-off", name: "Disable auto-sign", desc: "Stop GitHub Actions from signing"})
		} else {
			actions = append(actions, action{key: "auto-on", name: "Enable auto-sign", desc: "Resume GitHub Actions signing"})
		}
	}

	// Add saved schedule switching
	if d.cfg.SavedSchedules != nil && len(d.cfg.SavedSchedules) > 0 {
		for name := range d.cfg.SavedSchedules {
			label := fmt.Sprintf("Switch to \"%s\" schedule", name)
			if name == d.cfg.ActiveSchedule {
				label += " (current)"
			}
			actions = append(actions, action{key: "preset:" + name, name: label, desc: "Apply saved schedule preset"})
		}
	}

	actions = append(actions,
		action{key: "sync", name: "Sync to GitHub", desc: "Push secrets + workflows to fork"},
		action{key: "open", name: "Open Woffu", desc: "Open Woffu in browser"},
		action{key: "open-gh", name: "Open GitHub fork", desc: "View fork and workflow runs"},
	)

	return actions
}

func (d *Dashboard) executeAction(a action) tea.Cmd {
	switch a.key {
	case "sign":
		return d.trySign()
	case "auto-on":
		return d.toggleAuto(true)
	case "auto-off":
		return d.toggleAuto(false)
	case "sync":
		return d.syncGitHub()
	case "open":
		openBrowserCmd(d.cfg.WoffuCompanyURL + "/v2")
		d.setFlash("Opened Woffu in browser", false)
		return d.clearFlashAfter(2 * time.Second)
	case "open-gh":
		if d.cfg.GithubFork != "" {
			openBrowserCmd("https://github.com/" + d.cfg.GithubFork + "/actions")
			d.setFlash("Opened GitHub Actions", false)
			return d.clearFlashAfter(2 * time.Second)
		}
		d.setFlash("GitHub not configured", true)
		return d.clearFlashAfter(3 * time.Second)
	default:
		if strings.HasPrefix(a.key, "preset:") {
			name := strings.TrimPrefix(a.key, "preset:")
			return d.applyPreset(name)
		}
	}
	return nil
}

// ── Overlay rendering ──
// Overlays replace the full screen (no ANSI-corrupting string splicing).

func (d *Dashboard) renderOverlayMenu() string {
	actions := d.getActions()

	var rows []string
	for i, a := range actions {
		cursor := "  "
		style := sDimmed
		if i == d.menuCursor {
			cursor = sKey.Render("▸ ")
			style = sValue
		}
		name := style.Render(a.name)
		desc := sDimmed.Render("  " + a.desc)
		rows = append(rows, cursor+name+desc)
	}

	menuContent := sSection.Render("  Actions") + "\n\n" + strings.Join(rows, "\n") + "\n\n" +
		sDimmed.Render("  ↑↓ navigate  enter select  esc close")

	menuBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorPrimary).
		Padding(1, 2).
		Render(menuContent)

	return lipgloss.Place(d.width, d.height, lipgloss.Center, lipgloss.Center, menuBox)
}

func (d *Dashboard) renderOverlayConfirm(title, desc, yesHint, noHint string) string {
	content := sSection.Render("  "+title) + "\n\n" +
		sDimmed.Render("  "+desc) + "\n\n" +
		"  " + hint(yesHint, "confirm") + "    " + hint(noHint, "cancel")

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorWarning).
		Padding(1, 2).
		Render(content)

	return lipgloss.Place(d.width, d.height, lipgloss.Center, lipgloss.Center, box)
}

// ── Commands ──

func (d *Dashboard) fetchData() tea.Cmd {
	return func() tea.Msg {
		token, err := woffu.Authenticate(d.client, d.companyClient, d.cfg.WoffuEmail, d.password)
		if err != nil {
			return errMsg{err}
		}
		d.token = token

		profile, _ := woffu.GetUserProfile(d.companyClient, token)

		info, err := woffu.GetSignInfo(d.companyClient, token,
			d.cfg.Latitude, d.cfg.Longitude, d.cfg.HomeLatitude, d.cfg.HomeLongitude)
		if err != nil {
			return errMsg{err}
		}

		events, err := woffu.GetAvailableEvents(d.companyClient, token)
		if err != nil {
			return errMsg{err}
		}

		slots, _ := woffu.GetTodaySlots(d.companyClient, token)

		return dataMsg{signInfo: info, events: events, profile: profile, slots: slots}
	}
}

func (d *Dashboard) fetchAutoStatus() tea.Cmd {
	if d.cfg.GithubFork == "" {
		return nil
	}
	return func() tea.Msg {
		enabled, err := gh.IsAutoSignEnabled(d.cfg.GithubFork)
		if err != nil {
			return nil // Silently fail - not critical
		}
		return autoToggleMsg{enabled: enabled}
	}
}

func (d *Dashboard) trySign() tea.Cmd {
	if d.signing {
		return nil
	}
	if d.signInfo != nil && !d.signInfo.IsWorkingDay {
		d.setFlash("Not a working day", true)
		return d.clearFlashAfter(3 * time.Second)
	}
	d.signing = true
	d.setFlash("Signing...", false)
	return d.doSign()
}

func (d *Dashboard) doSign() tea.Cmd {
	return func() tea.Msg {
		if d.signInfo == nil {
			return errMsg{fmt.Errorf("no sign info available")}
		}
		err := woffu.DoSign(d.companyClient, d.token, d.signInfo.Latitude, d.signInfo.Longitude)
		if err != nil {
			return errMsg{err}
		}
		tgCfg := notify.TelegramConfig{BotToken: d.cfg.Telegram.BotToken, ChatID: d.cfg.Telegram.ChatID}
		_ = notify.SendSignedNotification(tgCfg, d.signInfo)
		return signDoneMsg{}
	}
}

func (d *Dashboard) toggleAuto(enable bool) tea.Cmd {
	return func() tea.Msg {
		var err error
		if enable {
			err = gh.EnableAutoSign(d.cfg.GithubFork)
		} else {
			err = gh.DisableAutoSign(d.cfg.GithubFork)
		}
		if err != nil {
			return errMsg{err}
		}
		return autoToggleMsg{enabled: enable}
	}
}

func (d *Dashboard) syncGitHub() tea.Cmd {
	return func() tea.Msg {
		pw, _ := config.GetPassword(d.cfg.WoffuEmail)
		if err := gh.SyncSecrets(d.cfg, pw); err != nil {
			return errMsg{fmt.Errorf("sync secrets: %w", err)}
		}
		if err := gh.SyncWorkflows(d.cfg); err != nil {
			return errMsg{fmt.Errorf("sync workflows: %w", err)}
		}
		return syncDoneMsg{}
	}
}

func (d *Dashboard) applyPreset(name string) tea.Cmd {
	return func() tea.Msg {
		if !d.cfg.LoadSchedulePreset(name) {
			return errMsg{fmt.Errorf("preset \"%s\" not found", name)}
		}
		if err := config.Save(d.cfg); err != nil {
			return errMsg{fmt.Errorf("save config: %w", err)}
		}
		// Sync workflows if github is configured
		if d.cfg.GithubFork != "" {
			gh.SyncWorkflows(d.cfg)
		}
		return syncDoneMsg{}
	}
}

func (d *Dashboard) tick() tea.Cmd {
	return tea.Tick(time.Minute, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (d *Dashboard) clearFlashAfter(dur time.Duration) tea.Cmd {
	return tea.Tick(dur, func(t time.Time) tea.Msg { return clearFlashMsg{} })
}

func (d *Dashboard) setFlash(text string, isErr bool) {
	d.flash = text
	d.flashErr = isErr
}

func openBrowserCmd(url string) {
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
		cmd.Start()
	}
}
