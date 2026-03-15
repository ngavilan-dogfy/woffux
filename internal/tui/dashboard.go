package tui

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ngavilan-dogfy/woffux/internal/config"
	gh "github.com/ngavilan-dogfy/woffux/internal/github"
	"github.com/ngavilan-dogfy/woffux/internal/notify"
	"github.com/ngavilan-dogfy/woffux/internal/woffu"
)

// ── Tab indices ──

const (
	tabStatus   = 0
	tabCalendar = 1
	tabEvents   = 2
	tabCount    = 3
)

var tabNames = [tabCount]string{"Status", "Calendar", "Events"}

// ── Messages ──

type dataMsg struct {
	signInfo     *woffu.SignInfo
	events       []woffu.AvailableUserEvent
	profile      *woffu.UserProfile
	slots        []woffu.SignSlot
	calendarDays []woffu.CalendarDay
	userId       int
}
type errMsg struct{ err error }
type signDoneMsg struct{}
type autoToggleMsg struct{ enabled bool }
type syncDoneMsg struct{}
type clearFlashMsg struct{}
type tickMsg time.Time
type scheduleEditDoneMsg struct{}
type execDoneMsg struct{ err error }

// ── Action items for the overlay menu ──

type action struct {
	key  string
	name string
	desc string
}

// ── Overlay kinds ──

type overlayKind int

type requestDoneMsg struct{ count int }

const (
	overlayNone      overlayKind = iota
	overlayMenu                  // action menu
	overlaySignConf              // sign confirmation
	overlayAutoConf              // auto-sign toggle confirmation
	overlayCalAction             // calendar batch action picker
	overlayDayAction             // single day context menu
)

// ── Dashboard model ──

type Dashboard struct {
	client        *woffu.Client
	companyClient *woffu.Client
	cfg           *config.Config
	password      string

	// Data
	loading      bool
	token        string
	userId       int
	signInfo     *woffu.SignInfo
	events       []woffu.AvailableUserEvent
	profile      *woffu.UserProfile
	slots        []woffu.SignSlot
	calendarDays []woffu.CalendarDay
	autoActive   *bool // nil = unknown
	err          error

	// UI state
	activeTab  int
	signing    bool
	overlay    overlayKind
	menuCursor int
	autoTarget bool // what the auto-sign toggle overlay wants to set
	flash      string
	flashErr   bool
	cal        *calendarGrid // interactive calendar

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
		d.calendarDays = msg.calendarDays
		d.userId = msg.userId
		if d.cal == nil && len(msg.calendarDays) > 0 {
			now := time.Now()
			d.cal = newCalendarGrid(now.Year(), now.Month(), msg.calendarDays)
		} else if d.cal != nil {
			d.cal.days = msg.calendarDays
		}

	case requestDoneMsg:
		d.setFlash(fmt.Sprintf("%d requests submitted!", msg.count), false)
		return d, tea.Batch(d.fetchData(), d.clearFlashAfter(3*time.Second))

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

	case execDoneMsg:
		if msg.err != nil {
			d.setFlash("Schedule edit cancelled", true)
			return d, d.clearFlashAfter(3 * time.Second)
		}
		// Reload config after schedule edit
		newCfg, err := config.Load()
		if err == nil {
			d.cfg = newCfg
		}
		d.setFlash("Schedule updated!", false)
		return d, tea.Batch(d.fetchData(), d.clearFlashAfter(3*time.Second))

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

	// ── Overlay: calendar action (multi-select) ──
	if d.overlay == overlayCalAction {
		calActions := d.getCalActions()
		switch key {
		case "esc", "q":
			d.overlay = overlayNone
		case "up", "k":
			if d.menuCursor > 0 {
				d.menuCursor--
			}
		case "down", "j":
			if d.menuCursor < len(calActions)-1 {
				d.menuCursor++
			}
		case "enter":
			d.overlay = overlayNone
			return d, d.executeCalAction(calActions[d.menuCursor])
		}
		return d, nil
	}

	// ── Overlay: single day action ──
	if d.overlay == overlayDayAction {
		dayActions := d.getDayActions()
		switch key {
		case "esc", "q":
			d.overlay = overlayNone
		case "up", "k":
			if d.menuCursor > 0 {
				d.menuCursor--
			}
		case "down", "j":
			if d.menuCursor < len(dayActions)-1 {
				d.menuCursor++
			}
		case "enter":
			d.overlay = overlayNone
			return d, d.executeDayAction(dayActions[d.menuCursor])
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

	// ── Calendar tab keys ──
	if d.activeTab == tabCalendar && d.cal != nil && d.overlay == overlayNone {
		switch key {
		// Navigation
		case "left", "h":
			d.cal.moveLeft()
			return d, nil
		case "right", "l":
			d.cal.moveRight()
			return d, nil
		case "up", "k":
			d.cal.moveUp()
			return d, nil
		case "down", "j":
			d.cal.moveDown()
			return d, nil

		// Range selection (shift+arrows)
		case "shift+left":
			d.cal.moveLeftSelect()
			return d, nil
		case "shift+right":
			d.cal.moveRightSelect()
			return d, nil
		case "shift+up":
			d.cal.moveUpSelect()
			return d, nil
		case "shift+down":
			d.cal.moveDownSelect()
			return d, nil

		// Single toggle / clear
		case " ":
			d.cal.toggleSelect(d.cal.cursor)
			return d, nil
		case "x":
			d.cal.clearSelection()
			return d, nil

		// Month navigation
		case "[", "H":
			d.cal.prevMonth()
			d.loading = true
			return d, d.fetchData()
		case "]", "L":
			d.cal.nextMonth()
			d.loading = true
			return d, d.fetchData()

		// Actions
		case "enter":
			if len(d.cal.selected) > 0 {
				d.overlay = overlayCalAction
				d.menuCursor = 0
				return d, nil
			}
			// No selection → single day context menu
			d.overlay = overlayDayAction
			d.menuCursor = 0
			return d, nil
		}
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
			d.setFlash("Auto-sign not set up. Run woffux setup.", true)
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
			d.setFlash("GitHub not configured — run woffux setup", true)
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
	case overlayCalAction:
		return d.renderCalActionOverlay()
	case overlayDayAction:
		return d.renderDayActionOverlay()
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
	name := "woffux"
	if d.profile != nil {
		name = fmt.Sprintf("woffux — %s", d.profile.FullName)
	}
	left := sTitle.Render(name)
	_, isoWeek := time.Now().ISOWeek()
	right := sDimmed.Render(fmt.Sprintf("W%d  %s", isoWeek, time.Now().Format("15:04")))
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

	// Greeting + week
	now := time.Now()
	greetText := greeting(now)
	if d.profile != nil {
		firstName := strings.Split(d.profile.FullName, " ")[0]
		if len(firstName) > 1 {
			firstName = strings.ToUpper(firstName[:1]) + strings.ToLower(firstName[1:])
		}
		greetText += ", " + firstName
	}
	parts = append(parts, "\n  "+sSubtitle.Render(greetText))

	// Sign info box
	if d.signInfo != nil {
		info := d.signInfo

		// Full date with weekday
		fullDate := info.Date
		if t, err := time.Parse("2006-01-02", info.Date); err == nil {
			fullDate = t.Format("Monday, 2 January 2006")
		}

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
			sLabel.Render("Date") + sValue.Render(fullDate),
			sLabel.Render("Working day") + workingDay,
			sLabel.Render("Mode") + mode,
			sLabel.Render("Signed") + d.signStatusText(),
		}

		parts = append(parts, "\n"+sBox.Render(strings.Join(rows, "\n")))
	}

	// Hours worked + progress bar
	if len(d.slots) > 0 {
		worked, clockedIn := d.hoursWorkedToday()
		target := d.targetHoursToday()

		if target > 0 {
			pct := int(float64(worked) / float64(target) * 100)
			if pct > 100 {
				pct = 100
			}

			barColor := colorSecondary
			if pct >= 100 {
				barColor = colorSuccess
			}

			bar := renderProgressBar(worked, target, 30)
			label := "  Today's progress"
			if clockedIn {
				label += "  " + lipgloss.NewStyle().Foreground(colorSuccess).Render("● live")
			}

			progressLine := "  " + lipgloss.NewStyle().Foreground(barColor).Render(bar) +
				"  " + sValue.Render(formatDuration(worked)) +
				sDimmed.Render(" / "+formatDuration(target)) +
				sDimmed.Render(fmt.Sprintf("  (%d%%)", pct))

			parts = append(parts, "\n"+sSubtitle.Render(label)+"\n"+progressLine)
		}
	}

	// Today's slots
	if len(d.slots) > 0 {
		parts = append(parts, d.renderSlots())
	}

	// Next scheduled sign
	if next := d.nextScheduledSign(); next != "" {
		parts = append(parts, "\n"+sLabel.Render("  Next sign")+sValue.Render(next))
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
	if d.cal == nil {
		return "\n" + sDimmed.Render("  Loading calendar...")
	}
	return "\n" + d.cal.render()
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
			hint("←→↑↓", "navigate"),
			hint("space", "select"),
			hint("⇧+arrows", "range"),
			hint("H/L", "month"),
			hint("x", "clear"),
			hint("enter", "action"),
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
		action{key: "edit-schedule", name: "Edit schedule", desc: "Change auto-sign times"},
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
	case "edit-schedule":
		return d.editSchedule()
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

		// Calendar for current month (or the month the grid is showing)
		calYear := time.Now().Year()
		calMonth := time.Now().Month()
		if d.cal != nil {
			calYear = d.cal.year
			calMonth = d.cal.month
		}
		calDays, _ := woffu.GetCalendarMonthYM(d.companyClient, token, calYear, calMonth)

		// Enrich calendar with requests and signs
		userId, _, _ := woffu.GetUserIds(d.companyClient, token)
		if userId > 0 {
			reqs, _ := woffu.GetMonthRequests(d.companyClient, token, userId, calYear, calMonth)
			signs, _ := woffu.GetMonthSigns(d.companyClient, token, calYear, calMonth)
			woffu.EnrichCalendarDays(calDays, reqs, signs)
		}

		return dataMsg{signInfo: info, events: events, profile: profile, slots: slots, calendarDays: calDays, userId: userId}
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

// editSchedule suspends the TUI and runs `woffux schedule edit` interactively.
func (d *Dashboard) editSchedule() tea.Cmd {
	bin, err := os.Executable()
	if err != nil {
		bin = "woffux"
	}
	c := exec.Command(bin, "schedule", "edit")
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return execDoneMsg{err: err}
	})
}

// ── Calendar batch actions ──

func (d *Dashboard) getCalActions() []action {
	infos := d.cal.selectedDayInfos()
	n := len(infos)
	if n == 0 {
		return nil
	}

	// Analyze selection
	workingNoReq := 0
	withPendingReq := 0
	withApprovedReq := 0

	for _, info := range infos {
		hasActiveReq := false
		for _, r := range info.Requests {
			switch r.Status {
			case "pending":
				withPendingReq++
				hasActiveReq = true
			case "approved":
				withApprovedReq++
				hasActiveReq = true
			}
		}
		if !hasActiveReq && info.Status == "working" {
			workingNoReq++
		}
	}

	var actions []action

	// Offer creation actions if there are working days without requests
	if workingNoReq > 0 {
		actions = append(actions,
			action{key: "telework", name: fmt.Sprintf("Request Telework (%d days)", workingNoReq), desc: "Teletrabajo 🏡"},
			action{key: "vacation", name: fmt.Sprintf("Request Vacation (%d days)", workingNoReq), desc: "Vacaciones"},
			action{key: "personal", name: fmt.Sprintf("Request Personal Day (%d days)", workingNoReq), desc: "Asuntos Propios"},
			action{key: "hours", name: fmt.Sprintf("Request Hours Pool (%d days)", workingNoReq), desc: "Bolsa de horas"},
		)
	}

	// Offer cancel if there are pending/approved requests
	if withPendingReq > 0 {
		actions = append(actions, action{
			key:  "cancel-pending",
			name: fmt.Sprintf("Cancel pending requests (%d)", withPendingReq),
			desc: "Remove pending requests",
		})
	}
	if withApprovedReq > 0 {
		actions = append(actions, action{
			key:  "cancel-approved",
			name: fmt.Sprintf("Cancel approved requests (%d)", withApprovedReq),
			desc: "Cancel approved requests",
		})
	}

	// Fallback if nothing matched (e.g. all holidays/weekends)
	if len(actions) == 0 {
		actions = append(actions, action{
			key: "noop", name: "No actions available", desc: "Selected days are holidays/weekends",
		})
	}

	return actions
}

func (d *Dashboard) executeCalAction(a action) tea.Cmd {
	if a.key == "noop" {
		return nil
	}

	// Handle cancel actions
	if a.key == "cancel-pending" || a.key == "cancel-approved" {
		targetStatus := "pending"
		if a.key == "cancel-approved" {
			targetStatus = "approved"
		}
		return d.cancelSelectedRequests(targetStatus)
	}

	// Creation actions — only operate on working days without active requests
	infos := d.cal.selectedDayInfos()
	var eligibleDates []string
	for _, info := range infos {
		if info.Status != "working" {
			continue
		}
		hasActiveReq := false
		for _, r := range info.Requests {
			if r.Status == "pending" || r.Status == "approved" {
				hasActiveReq = true
				break
			}
		}
		if !hasActiveReq {
			eligibleDates = append(eligibleDates, info.Date)
		}
	}

	if len(eligibleDates) == 0 {
		d.setFlash("No eligible days for this action", true)
		return d.clearFlashAfter(3 * time.Second)
	}

	// Map action key to search term for request type
	typeSearch := map[string]string{
		"telework": "teletrabajo",
		"vacation": "vacaciones",
		"personal": "asuntos propios",
		"hours":    "bolsa de horas",
	}

	search := typeSearch[a.key]
	if search == "" {
		return nil
	}

	d.setFlash(fmt.Sprintf("Submitting %d requests...", len(eligibleDates)), false)

	return func() tea.Msg {
		types, err := woffu.GetRequestTypes(d.companyClient, d.token)
		if err != nil {
			return errMsg{err}
		}

		var matchedType *woffu.RequestType
		for i, t := range types {
			if strings.Contains(strings.ToLower(t.Name), search) {
				matchedType = &types[i]
				break
			}
		}
		if matchedType == nil {
			return errMsg{fmt.Errorf("request type \"%s\" not found", search)}
		}

		userId, companyId, err := woffu.GetUserIds(d.companyClient, d.token)
		if err != nil {
			return errMsg{err}
		}

		count := 0
		for _, date := range eligibleDates {
			err := woffu.CreateRequest(d.companyClient, d.token, userId, companyId, matchedType.ID, date, date, matchedType.IsVacation)
			if err == nil {
				count++
			}
		}

		return requestDoneMsg{count: count}
	}
}

// cancelSelectedRequests cancels requests matching the target status on selected days.
func (d *Dashboard) cancelSelectedRequests(targetStatus string) tea.Cmd {
	infos := d.cal.selectedDayInfos()

	var requestIDs []int
	for _, info := range infos {
		for _, r := range info.Requests {
			if r.Status == targetStatus {
				requestIDs = append(requestIDs, r.RequestID)
			}
		}
	}

	if len(requestIDs) == 0 {
		d.setFlash("No requests to cancel", true)
		return d.clearFlashAfter(3 * time.Second)
	}

	d.setFlash(fmt.Sprintf("Cancelling %d requests...", len(requestIDs)), false)

	return func() tea.Msg {
		count := 0
		for _, id := range requestIDs {
			if err := woffu.CancelRequest(d.companyClient, d.token, id); err == nil {
				count++
			}
		}
		return requestDoneMsg{count: count}
	}
}

func (d *Dashboard) renderCalActionOverlay() string {
	actions := d.getCalActions()

	var rows []string
	for i, a := range actions {
		cursor := "  "
		style := sDimmed
		if i == d.menuCursor {
			cursor = sKey.Render("▸ ")
			style = sValue
		}
		rows = append(rows, cursor+style.Render(a.name))
	}

	dates := d.cal.selectedDates()
	title := fmt.Sprintf("  Action for %d selected days", len(dates))

	menuContent := sSection.Render(title) + "\n\n" + strings.Join(rows, "\n") + "\n\n" +
		sDimmed.Render("  ↑↓ navigate  enter submit  esc cancel")

	menuBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorPrimary).
		Padding(1, 2).
		Render(menuContent)

	return lipgloss.Place(d.width, d.height, lipgloss.Center, lipgloss.Center, menuBox)
}

func (d *Dashboard) renderDayActionOverlay() string {
	actions := d.getDayActions()

	var rows []string
	for i, a := range actions {
		cursor := "  "
		style := sDimmed
		if i == d.menuCursor {
			cursor = sKey.Render("▸ ")
			style = sValue
		}
		name := style.Render(a.name)
		desc := ""
		if a.desc != "" {
			desc = "  " + sDimmed.Render(a.desc)
		}
		rows = append(rows, cursor+name+desc)
	}

	info := d.cal.dayInfo(d.cal.cursor)
	title := fmt.Sprintf("  %s", info.Date)

	menuContent := sSection.Render(title) + "\n\n" + strings.Join(rows, "\n") + "\n\n" +
		sDimmed.Render("  ↑↓ navigate  enter select  esc close")

	menuBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorSecondary).
		Padding(1, 2).
		Render(menuContent)

	return lipgloss.Place(d.width, d.height, lipgloss.Center, lipgloss.Center, menuBox)
}

// ── Single day context actions ──

func (d *Dashboard) getDayActions() []action {
	if d.cal == nil {
		return nil
	}
	info := d.cal.dayInfo(d.cal.cursor)
	if info == nil {
		return []action{{key: "noop", name: "No data for this day", desc: ""}}
	}

	var actions []action

	// Check existing requests on this day
	hasPending := false
	hasApproved := false
	for _, r := range info.Requests {
		switch r.Status {
		case "pending":
			hasPending = true
			actions = append(actions, action{
				key:  fmt.Sprintf("cancel-req:%d", r.RequestID),
				name: fmt.Sprintf("Cancel \"%s\" (pending)", r.EventName),
				desc: fmt.Sprintf("Request #%d", r.RequestID),
			})
		case "approved":
			hasApproved = true
			actions = append(actions, action{
				key:  fmt.Sprintf("cancel-req:%d", r.RequestID),
				name: fmt.Sprintf("Cancel \"%s\" (approved)", r.EventName),
				desc: fmt.Sprintf("Request #%d", r.RequestID),
			})
		}
	}

	// Offer creation if working day with no active requests
	if info.Status == "working" && !hasPending && !hasApproved {
		actions = append(actions,
			action{key: "day-telework", name: "Request teletrabajo", desc: "🏡"},
			action{key: "day-vacation", name: "Request vacation", desc: "🏖️"},
			action{key: "day-personal", name: "Request personal day", desc: ""},
		)
	}

	if len(info.Signs) > 0 {
		actions = append(actions, action{key: "view-signs", name: "View sign history", desc: fmt.Sprintf("%d slots", len(info.Signs))})
	}

	if len(actions) == 0 {
		switch info.Status {
		case "holiday":
			actions = append(actions, action{key: "noop", name: "Holiday — no actions", desc: strings.Join(info.EventNames, ", ")})
		case "weekend":
			actions = append(actions, action{key: "noop", name: "Weekend — no actions", desc: ""})
		default:
			actions = append(actions, action{key: "noop", name: "No actions available", desc: ""})
		}
	}

	return actions
}

func (d *Dashboard) executeDayAction(a action) tea.Cmd {
	if a.key == "noop" || a.key == "view-signs" {
		return nil
	}

	// Handle cancel
	if strings.HasPrefix(a.key, "cancel-req:") {
		idStr := strings.TrimPrefix(a.key, "cancel-req:")
		var reqID int
		fmt.Sscanf(idStr, "%d", &reqID)
		if reqID == 0 {
			return nil
		}
		d.setFlash("Cancelling request...", false)
		return func() tea.Msg {
			if err := woffu.CancelRequest(d.companyClient, d.token, reqID); err != nil {
				return errMsg{err}
			}
			return requestDoneMsg{count: 1}
		}
	}

	// Handle day-specific request creation
	typeSearch := map[string]string{
		"day-telework": "teletrabajo",
		"day-vacation": "vacaciones",
		"day-personal": "asuntos propios",
	}

	search := typeSearch[a.key]
	if search == "" {
		return nil
	}

	info := d.cal.dayInfo(d.cal.cursor)
	if info == nil {
		return nil
	}
	date := info.Date

	d.setFlash("Submitting request...", false)
	return func() tea.Msg {
		types, err := woffu.GetRequestTypes(d.companyClient, d.token)
		if err != nil {
			return errMsg{err}
		}

		var matchedType *woffu.RequestType
		for i, t := range types {
			if strings.Contains(strings.ToLower(t.Name), search) {
				matchedType = &types[i]
				break
			}
		}
		if matchedType == nil {
			return errMsg{fmt.Errorf("request type \"%s\" not found", search)}
		}

		userId, companyId, err := woffu.GetUserIds(d.companyClient, d.token)
		if err != nil {
			return errMsg{err}
		}

		if err := woffu.CreateRequest(d.companyClient, d.token, userId, companyId, matchedType.ID, date, date, matchedType.IsVacation); err != nil {
			return errMsg{err}
		}

		return requestDoneMsg{count: 1}
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

// ── Status helpers ──

func greeting(t time.Time) string {
	hour := t.Hour()
	switch {
	case hour < 12:
		return "Good morning"
	case hour < 18:
		return "Good afternoon"
	default:
		return "Good evening"
	}
}

func parseSlotTime(dt string) time.Time {
	if dt == "" {
		return time.Time{}
	}
	for _, layout := range []string{
		"2006-01-02T15:04:05.000",
		"2006-01-02T15:04:05",
		"2006-01-02T15:04",
	} {
		if t, err := time.Parse(layout, dt); err == nil {
			return t
		}
	}
	return time.Time{}
}

func (d *Dashboard) hoursWorkedToday() (time.Duration, bool) {
	var total time.Duration
	clockedIn := false
	now := time.Now()
	for _, s := range d.slots {
		inTime := parseSlotTime(s.In)
		if inTime.IsZero() {
			continue
		}
		outTime := parseSlotTime(s.Out)
		if outTime.IsZero() {
			todayIn := time.Date(now.Year(), now.Month(), now.Day(),
				inTime.Hour(), inTime.Minute(), inTime.Second(), 0, now.Location())
			total += now.Sub(todayIn)
			clockedIn = true
		} else {
			total += outTime.Sub(inTime)
		}
	}
	return total, clockedIn
}

func (d *Dashboard) todaySchedule() (config.DaySchedule, bool) {
	switch time.Now().Weekday() {
	case time.Monday:
		return d.cfg.Schedule.Monday, d.cfg.Schedule.Monday.Enabled
	case time.Tuesday:
		return d.cfg.Schedule.Tuesday, d.cfg.Schedule.Tuesday.Enabled
	case time.Wednesday:
		return d.cfg.Schedule.Wednesday, d.cfg.Schedule.Wednesday.Enabled
	case time.Thursday:
		return d.cfg.Schedule.Thursday, d.cfg.Schedule.Thursday.Enabled
	case time.Friday:
		return d.cfg.Schedule.Friday, d.cfg.Schedule.Friday.Enabled
	default:
		return config.DaySchedule{}, false
	}
}

func (d *Dashboard) targetHoursToday() time.Duration {
	sched, enabled := d.todaySchedule()
	if !enabled || len(sched.Times) < 2 {
		return 0
	}
	var total time.Duration
	for i := 0; i+1 < len(sched.Times); i += 2 {
		inTime, err1 := time.Parse("15:04", sched.Times[i].Time)
		outTime, err2 := time.Parse("15:04", sched.Times[i+1].Time)
		if err1 != nil || err2 != nil {
			continue
		}
		total += outTime.Sub(inTime)
	}
	return total
}

func (d *Dashboard) nextScheduledSign() string {
	sched, enabled := d.todaySchedule()
	if !enabled {
		return ""
	}
	currentTime := time.Now().Format("15:04")
	for _, t := range sched.Times {
		if t.Time > currentTime {
			return t.Time
		}
	}
	return ""
}

func renderProgressBar(current, target time.Duration, width int) string {
	if target <= 0 {
		return strings.Repeat("░", width)
	}
	pct := float64(current) / float64(target)
	if pct > 1 {
		pct = 1
	}
	filled := int(pct * float64(width))
	if filled > width {
		filled = width
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

func formatDuration(d time.Duration) string {
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	return fmt.Sprintf("%dh %02dm", h, m)
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
