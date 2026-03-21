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
type calendarDataMsg struct{ calendarDays []woffu.CalendarDay }
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
	overlayNone       overlayKind = iota
	overlayMenu                   // action menu
	overlaySignConf               // sign confirmation
	overlayAutoConf               // auto-sign toggle confirmation
	overlayCalAction              // calendar batch action picker
	overlayDayAction              // single day context menu
	overlaySavePreset             // save-as-preset text input
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
	activeTab   int
	signing     bool
	overlay     overlayKind
	menuCursor  int
	autoTarget  bool   // what the auto-sign toggle overlay wants to set
	presetInput string // text buffer for save-preset overlay
	flash       string
	flashErr    bool
	cal         *calendarGrid // interactive calendar

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

	case calendarDataMsg:
		d.loading = false
		d.calendarDays = msg.calendarDays
		if d.cal != nil {
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

	// ── Overlay: save preset (text input) ──
	if d.overlay == overlaySavePreset {
		switch key {
		case "esc":
			d.overlay = overlayNone
		case "enter":
			if d.presetInput != "" {
				name := d.presetInput
				d.overlay = overlayNone
				return d, d.savePreset(name)
			}
		case "backspace":
			if len(d.presetInput) > 0 {
				d.presetInput = d.presetInput[:len(d.presetInput)-1]
			}
		default:
			// Only accept printable single characters
			if len(key) == 1 && key[0] >= 32 && key[0] <= 126 {
				d.presetInput += key
			}
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
			for next := d.menuCursor - 1; next >= 0; next-- {
				if actions[next].key != "---" {
					d.menuCursor = next
					break
				}
			}
		case "down", "j":
			for next := d.menuCursor + 1; next < len(actions); next++ {
				if actions[next].key != "---" {
					d.menuCursor = next
					break
				}
			}
		case "enter":
			if actions[d.menuCursor].key != "---" {
				d.overlay = overlayNone
				return d, d.executeAction(actions[d.menuCursor])
			}
		}
		return d, nil
	}

	// ── Calendar tab keys ──
	if d.activeTab == tabCalendar && d.cal != nil && d.overlay == overlayNone {
		switch key {
		// Navigation (arrows cross month boundaries)
		case "left", "h":
			if d.cal.moveLeft() {
				d.loading = true
				return d, d.fetchCalendarData()
			}
			return d, nil
		case "right", "l":
			if d.cal.moveRight() {
				d.loading = true
				return d, d.fetchCalendarData()
			}
			return d, nil
		case "up", "k":
			if d.cal.moveUp() {
				d.loading = true
				return d, d.fetchCalendarData()
			}
			return d, nil
		case "down", "j":
			if d.cal.moveDown() {
				d.loading = true
				return d, d.fetchCalendarData()
			}
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

		// Month navigation (lightweight fetch — no re-auth)
		case "[", "H":
			d.cal.prevMonth()
			d.loading = true
			return d, d.fetchCalendarData()
		case "]", "L":
			d.cal.nextMonth()
			d.loading = true
			return d, d.fetchCalendarData()

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
		d.activeTab = tabCalendar
	case "3":
		d.activeTab = tabEvents

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
	case overlaySavePreset:
		return d.renderSavePresetOverlay()
	case overlaySignConf:
		signAction := "IN"
		if woffu.IsSignedIn(d.slots) {
			signAction = "OUT"
		}
		return d.renderOverlayConfirm(
			fmt.Sprintf("Sign %s?", signAction),
			fmt.Sprintf("Clock %s on Woffu right now.", strings.ToLower(signAction)),
			"y/enter", "n/esc",
		)
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

	// Greeting
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

	// Compact info box: date + mode + clocked status on 2 lines
	if d.signInfo != nil {
		info := d.signInfo

		shortDate := info.Date
		if t, err := time.Parse("2006-01-02", info.Date); err == nil {
			shortDate = t.Format("Mon, 2 January 2006")
		}

		modeStyle := sDimmed
		if info.Mode == woffu.SignModeRemote {
			modeStyle = sSuccess
		}
		modeText := modeStyle.Render(fmt.Sprintf("%s %s", info.Mode.Emoji(), info.Mode.Label()))

		clockStatus := d.signStatusText()

		row1 := sValue.Render(shortDate)
		row2 := modeText + "    " + clockStatus

		parts = append(parts, "\n"+sInfoBox.Render(row1+"\n"+row2))
	}

	// Progress bar -- always visible when we have a target, even with 0 slots
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
			label += "  " + sLiveIndicator.Render("● live")
		}

		progressLine := "  " + lipgloss.NewStyle().Foreground(barColor).Render(bar) +
			"  " + sValue.Render(formatDuration(worked)) +
			sDimmed.Render(" / "+formatDuration(target)) +
			sDimmed.Render(fmt.Sprintf("  (%d%%)", pct))

		parts = append(parts, "\n"+sSubtitle.Render(label)+"\n"+progressLine)
	}

	// Timeline (today's signs as a vertical timeline)
	parts = append(parts, d.renderSlots())

	// Next scheduled sign with type + countdown
	parts = append(parts, d.nextScheduledSign())

	// Auto-sign status (collapsed schedule section)
	parts = append(parts, d.renderAutoSign())

	return strings.Join(parts, "\n")
}

func (d *Dashboard) signStatusText() string {
	if len(d.slots) == 0 {
		return sDimmed.Render("○") + " " + sDanger.Render("not signed")
	}

	lastSlot := d.slots[len(d.slots)-1]
	if lastSlot.Out != "" {
		return sDimmed.Render("○") + " " + sDimmed.Render("clocked out")
	}
	if lastSlot.In != "" {
		return sLiveIndicator.Render("●") + " " + sSuccess.Render("clocked in")
	}

	return sDimmed.Render("○") + " " + sDimmed.Render(fmt.Sprintf("%d slots", len(d.slots)))
}

func (d *Dashboard) renderSlots() string {
	if len(d.slots) == 0 {
		return ""
	}

	header := d.renderSectionLine("Timeline")
	var rows []string

	for _, s := range d.slots {
		if s.In != "" {
			inTime := extractTime(s.In)
			marker := sSignIn.Render("\u25B6") // ▶
			label := sSignIn.Render("IN ")
			timeStr := sValue.Render(inTime)

			// If this slot has no OUT, show a track line to "now"
			if s.Out == "" {
				track := sTimelineTrack.Render("  " + strings.Repeat("\u2501", 15) + " ") // ━
				nowLabel := sNowMarker.Render("now")
				rows = append(rows, fmt.Sprintf("  %s %s  %s%s%s", marker, timeStr, label, track, nowLabel))
			} else {
				rows = append(rows, fmt.Sprintf("  %s %s  %s", marker, timeStr, label))
			}
		}
		if s.Out != "" {
			outTime := extractTime(s.Out)
			marker := sSignOut.Render("\u25A0") // ■
			label := sSignOut.Render("OUT")
			timeStr := sValue.Render(outTime)
			rows = append(rows, fmt.Sprintf("  %s %s  %s", marker, timeStr, label))
		}
	}

	return "\n" + header + "\n" + strings.Join(rows, "\n")
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

// renderSectionLine renders a section divider: "── Title ──────────────"
func (d *Dashboard) renderSectionLine(title string) string {
	prefix := "\u2500\u2500 " // ──
	suffix := " "
	titleRendered := sSectionHeader.Render(prefix) + sSubtitle.Render(title) + sSectionHeader.Render(suffix)
	// Fill remaining width with ─
	titleWidth := lipgloss.Width(titleRendered)
	maxWidth := 40
	if d.width > 4 && d.width-4 < maxWidth {
		maxWidth = d.width - 4
	}
	remaining := maxWidth - titleWidth
	if remaining < 0 {
		remaining = 0
	}
	return "  " + titleRendered + sSectionHeader.Render(strings.Repeat("\u2500", remaining))
}

// renderSchedule is no longer shown in the status tab (redundant with timeline + next sign).
// Kept for potential reuse in other views.
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
			status = sSuccess.Render("● active")
		} else {
			status = sDanger.Render("○ disabled")
		}
	} else if d.cfg.GithubFork == "" {
		status = sDimmed.Render("not set up")
	}

	header := d.renderSectionLine("Schedule")
	line := "  " + sDimmed.Render("Auto-sign") + "  " + status
	if d.cfg.ActiveSchedule != "" {
		line += "    " + sDimmed.Render("preset:") + " " + sValue.Render(d.cfg.ActiveSchedule)
	}
	return "\n" + header + "\n" + line
}

// ── Render: footer help ──

func (d *Dashboard) renderHelp() string {
	var left, right []string

	switch d.activeTab {
	case tabStatus:
		left = []string{hint("s", "sign"), hint("r", "refresh")}
	case tabEvents:
		left = []string{hint("r", "refresh")}
	case tabCalendar:
		left = []string{hint("←→↑↓", "move"), hint("space", "select"), hint("H/L", "month")}
	}

	right = []string{hint("enter", "menu"), hint("tab", "switch"), hint("q", "quit")}

	leftStr := strings.Join(left, "  ")
	rightStr := strings.Join(right, "  ")
	gap := d.width - lipgloss.Width(leftStr) - lipgloss.Width(rightStr) - 6
	if gap < 2 {
		gap = 2
	}

	return "\n  " + leftStr + strings.Repeat(" ", gap) + rightStr
}

// ── Action menu ──

func (d *Dashboard) getActions() []action {
	actions := []action{
		{key: "sign", name: "Sign now", desc: "Clock in/out"},
	}

	if d.autoActive != nil {
		if *d.autoActive {
			actions = append(actions, action{key: "auto-off", name: "Disable auto-sign", desc: ""})
		} else {
			actions = append(actions, action{key: "auto-on", name: "Enable auto-sign", desc: ""})
		}
	}

	// Schedule section
	actions = append(actions, action{key: "---", name: "Schedule", desc: ""})

	if d.cfg.SavedSchedules != nil && len(d.cfg.SavedSchedules) > 0 {
		for name := range d.cfg.SavedSchedules {
			label := name
			if name == d.cfg.ActiveSchedule {
				label += "  (active)"
			}
			actions = append(actions, action{key: "preset:" + name, name: label, desc: ""})
		}
	}

	actions = append(actions,
		action{key: "save-preset", name: "Save as preset...", desc: ""},
		action{key: "edit-schedule", name: "Edit schedule...", desc: ""},
	)

	// Tools section
	actions = append(actions, action{key: "---", name: "Tools", desc: ""})
	actions = append(actions,
		action{key: "sync", name: "Sync to GitHub", desc: ""},
		action{key: "open", name: "Open Woffu", desc: ""},
		action{key: "open-gh", name: "Open GitHub Actions", desc: ""},
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
	case "save-preset":
		d.presetInput = ""
		d.overlay = overlaySavePreset
		return nil
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
		if a.key == "---" {
			// Section separator
			rows = append(rows, "\n  "+sSectionHeader.Render("── ")+sSubtitle.Render(a.name)+sSectionHeader.Render(" ──"))
			continue
		}
		cursor := "  "
		style := sDimmed
		if i == d.menuCursor {
			cursor = sKey.Render("▸ ")
			style = sValue
		}
		rows = append(rows, cursor+style.Render(a.name))
	}

	menuContent := strings.Join(rows, "\n") + "\n\n" +
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

// fetchCalendarData fetches only calendar + requests + signs for the displayed month.
// Uses cached token (no re-auth) and runs API calls in parallel.
func (d *Dashboard) fetchCalendarData() tea.Cmd {
	return func() tea.Msg {
		calYear := d.cal.year
		calMonth := d.cal.month

		var calDays []woffu.CalendarDay
		var reqs []woffu.UserRequest
		var signs []woffu.SignRecord
		var calErr error

		var wg sync.WaitGroup

		wg.Add(1)
		go func() {
			defer wg.Done()
			calDays, calErr = woffu.GetCalendarMonthYM(d.companyClient, d.token, calYear, calMonth)
		}()

		if d.userId > 0 {
			wg.Add(2)
			go func() {
				defer wg.Done()
				reqs, _ = woffu.GetMonthRequests(d.companyClient, d.token, d.userId, calYear, calMonth)
			}()
			go func() {
				defer wg.Done()
				signs, _ = woffu.GetMonthSigns(d.companyClient, d.token, calYear, calMonth)
			}()
		}

		wg.Wait()

		if calErr != nil {
			return errMsg{calErr}
		}

		woffu.EnrichCalendarDays(calDays, reqs, signs)
		return calendarDataMsg{calendarDays: calDays}
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
	if len(d.cal.selected) == 0 {
		return nil
	}

	// Analyze current month selection (we have full data)
	infos := d.cal.selectedDayInfos()
	withPendingReq := 0
	withApprovedReq := 0

	for _, info := range infos {
		for _, r := range info.Requests {
			switch r.Status {
			case "pending":
				withPendingReq++
			case "approved":
				withApprovedReq++
			}
		}
	}

	// Eligible dates for creation (current month filtered + other months optimistic)
	eligibleCount := len(d.cal.allEligibleDates())

	var actions []action

	// Offer creation actions if there are eligible days
	if eligibleCount > 0 {
		actions = append(actions,
			action{key: "telework", name: fmt.Sprintf("Request Telework (%d days)", eligibleCount), desc: "Teletrabajo 🏡"},
			action{key: "vacation", name: fmt.Sprintf("Request Vacation (%d days)", eligibleCount), desc: "Vacaciones"},
			action{key: "personal", name: fmt.Sprintf("Request Personal Day (%d days)", eligibleCount), desc: "Asuntos Propios"},
			action{key: "hours", name: fmt.Sprintf("Request Hours Pool (%d days)", eligibleCount), desc: "Bolsa de horas"},
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

	// Creation actions — current month filtered + other months included optimistically
	eligibleDates := d.cal.allEligibleDates()

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

	infos := d.cal.selectedDayInfos()
	title := fmt.Sprintf("  Action for %d days this month", len(infos))

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

func (d *Dashboard) savePreset(name string) tea.Cmd {
	return func() tea.Msg {
		d.cfg.SaveSchedulePreset(name, d.cfg.Schedule)
		d.cfg.ActiveSchedule = name
		if err := config.Save(d.cfg); err != nil {
			return errMsg{fmt.Errorf("save config: %w", err)}
		}
		return syncDoneMsg{}
	}
}

func (d *Dashboard) renderSavePresetOverlay() string {
	title := sKey.Render("Save schedule as preset")
	input := d.presetInput
	if input == "" {
		input = sDimmed.Render("type a name...")
	} else {
		input = sValue.Render(input) + sKey.Render("_")
	}
	help := sDimmed.Render("enter = save  esc = cancel")

	box := fmt.Sprintf("\n  %s\n\n  > %s\n\n  %s\n", title, input, help)

	return lipgloss.Place(d.width, d.height,
		lipgloss.Center, lipgloss.Center,
		lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("63")).
			Padding(1, 3).
			Render(box),
	)
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

	now := time.Now()
	currentTime := now.Format("15:04")

	for i, t := range sched.Times {
		if t.Time > currentTime {
			// Determine sign type: even index = IN, odd index = OUT
			signType := "IN "
			marker := sSignIn.Render("\u25B6") // ▶
			typeLabel := sSignIn.Render(signType)
			if i%2 != 0 {
				signType = "OUT"
				marker = sSignOut.Render("\u25A0") // ■
				typeLabel = sSignOut.Render(signType)
			}

			// Calculate countdown
			nextTime, err := time.Parse("15:04", t.Time)
			countdown := ""
			if err == nil {
				nextFull := time.Date(now.Year(), now.Month(), now.Day(),
					nextTime.Hour(), nextTime.Minute(), 0, 0, now.Location())
				diff := nextFull.Sub(now)
				if diff > 0 {
					h := int(diff.Hours())
					m := int(diff.Minutes()) % 60
					if h > 0 {
						countdown = fmt.Sprintf("in %dh %dm", h, m)
					} else {
						countdown = fmt.Sprintf("in %dm", m)
					}
				}
			}

			header := d.renderSectionLine("Next")
			line := fmt.Sprintf("  %s %s  %s", marker, sValue.Render(t.Time), typeLabel)
			if countdown != "" {
				line += "    " + sCountdown.Render(countdown)
			}
			return "\n" + header + "\n" + line
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
