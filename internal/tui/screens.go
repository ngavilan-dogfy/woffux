package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/ngavilan-dogfy/woffux/internal/woffu"
)

// ── Calendar tab ──

func (d *Dashboard) renderCalendar(h int) string {
	if d.cal == nil {
		return d.spin.View() + " " + sSubtle.Render("Loading calendar…")
	}
	w := d.contentWidth()
	now := d.clock()

	gridW := min(w, 4+7*9)
	panelW := w - gridW - 4
	side := panelW >= 30

	if !side {
		gridW = w
	}
	grid := d.cal.renderGrid(gridW, now)
	if !d.cal.loaded {
		grid += "\n\n" + d.spin.View() + " " + sFaint.Render("loading "+d.cal.month.String()+"…")
	} else {
		grid += "\n\n" + d.cal.monthSummary(gridW)
	}
	legend := []string{sFaint.Render("╌ pending"), sOK.Render("✓") + sFaint.Render(" signed"),
		sBad.Render("!") + sFaint.Render(" worked day without signs")}
	grid += "\n" + flow(legend, "   ", gridW)

	if side {
		return twoColumns(grid, d.renderDayPanel(panelW), gridW, 4)
	}
	panel := d.renderDayPanel(w)
	if lipgloss.Height(grid)+lipgloss.Height(panel)+2 <= h {
		return grid + "\n\n" + panel
	}
	return grid
}

// renderDayPanel details the cursor day (or the selection).
func (d *Dashboard) renderDayPanel(w int) string {
	c := d.cal
	var b []string

	if n := len(c.selected); n > 0 {
		dates := c.selectedDates()
		eligible := c.eligibleDates(dates)
		b = append(b, label("Selection"),
			sBrand.Render(fmt.Sprintf("%d %s", n, plural(n, "day", "days")))+sFaint.Render(" · "+truncate(describeDates(dates), w-10)))
		if skipped := n - len(eligible); skipped > 0 {
			b = append(b, sFaint.Render(fmt.Sprintf("%d can't take a new request", skipped)))
		}
		b = append(b, "")
	}

	date := c.cursorDate()
	t, _ := time.Parse("2006-01-02", date)
	info := c.dayInfoByDate(date)
	look := lookOf(info)

	b = append(b, label(relativeDayLabel(d.clock(), t)))
	b = append(b, sBold.Render(t.Format("Monday 2 January")))
	if info != nil {
		kind := fg(look.color).Render("▀▀ " + look.label)
		if look.pending {
			kind += sWarn.Render("  pending approval")
		}
		b = append(b, kind)
		if names := uniqueNames(info.EventNames); len(names) > 0 && info.Status == "holiday" {
			b = append(b, fg(cHoliday).Render(truncate(strings.Join(names, ", "), w)))
		}
	} else if c.loaded {
		b = append(b, sFaint.Render("No data for this day"))
	}

	if info != nil && len(info.Requests) > 0 {
		b = append(b, "", label("Requests"))
		for _, r := range info.Requests {
			icon, st := "○", sFaint
			switch r.Status {
			case "approved":
				icon, st = "✓", sOK
			case "pending":
				icon, st = "◷", sWarn
			case "rejected":
				icon, st = "✗", sBad
			}
			b = append(b, st.Render(icon)+" "+sText.Render(truncate(r.EventName, w-16))+" "+st.Render(r.Status))
		}
	}

	if info != nil && len(info.Signs) > 0 {
		b = append(b, "", label("Signs"))
		var parts []string
		var worked time.Duration
		for _, s := range info.Signs {
			seg := ""
			if s.In != "" {
				seg = sOK.Render("IN ") + sText.Render(extractTime(s.In))
			}
			if s.Out != "" {
				seg += sFaint.Render(" → ") + sWarn.Render("OUT ") + sText.Render(extractTime(s.Out))
				if in, out := parseSlotTime(s.In), parseSlotTime(s.Out); !in.IsZero() && out.After(in) {
					worked += out.Sub(in)
				}
			}
			parts = append(parts, seg)
		}
		b = append(b, parts...)
		if worked > 0 {
			b = append(b, sFaint.Render("worked ")+sText.Render(formatDuration(worked)))
		}
	} else if info != nil && info.Status == "working" && date < d.clock().Format("2006-01-02") && !hasTimeOff(info) {
		b = append(b, "", sBad.Render("! No signs recorded this day"), sFaint.Render("Fix it in Woffu (o) if you worked."))
	}

	// What you can do here
	b = append(b, "", label("Actions"))
	var acts []string
	for _, a := range d.dayActions() {
		if !a.enabled() || a.shortcut == "" {
			continue
		}
		acts = append(acts, keycap(a.shortcut, strings.ToLower(a.title)))
	}
	if len(acts) == 0 {
		b = append(b, sFaint.Render("Nothing to request on this day"))
	} else {
		b = append(b, flow(acts, "   ", w))
		if len(c.selected) == 0 {
			b = append(b, sFaint.Render("space selects · shift+arrows selects a range"))
		}
	}
	return strings.Join(b, "\n")
}

func relativeDayLabel(now, t time.Time) string {
	r := relativeDay(now, t)
	switch r {
	case "today", "tomorrow":
		return r
	}
	if t.Before(now) {
		return "past day"
	}
	return "upcoming"
}

// ── Balance tab ──

func (d *Dashboard) renderBalance(h int) string {
	w := d.contentWidth()
	if len(d.events) == 0 {
		return sFaint.Render("Woffu didn't return any balances.")
	}

	events := append([]woffu.AvailableUserEvent{}, d.events...)
	// Days first, then hours; biggest first inside each group.
	sort.SliceStable(events, func(i, j int) bool {
		ui, uj := unitRank(events[i].Unit), unitRank(events[j].Unit)
		if ui != uj {
			return ui < uj
		}
		return events[i].Available > events[j].Available
	})

	colW := w
	twoCol := w >= 100
	if twoCol {
		colW = (w - 4) / 2
	}

	// Days on the left, hours on the right: like with like.
	var days, hours []string
	for _, e := range events {
		if unitRank(e.Unit) == 0 {
			days = append(days, balanceRow(e, colW))
		} else {
			hours = append(hours, balanceRow(e, colW))
		}
	}
	var body string
	switch {
	case twoCol && len(days) > 0 && len(hours) > 0:
		body = twoColumns(label("Days")+"\n\n"+strings.Join(days, "\n\n"), label("Hours")+"\n\n"+strings.Join(hours, "\n\n"), colW, 4)
	default:
		body = strings.Join(append(days, hours...), "\n\n")
	}
	out := body

	if pend := d.pendingRequestsSummary(w); pend != "" {
		out += "\n\n" + pend
	}
	if up := d.renderComingUp(w, 5); up != "" && lipgloss.Height(out)+lipgloss.Height(up)+2 < h {
		out += "\n\n" + up
	}
	return out
}

func unitRank(u string) int {
	if strings.HasPrefix(strings.ToLower(u), "day") {
		return 0
	}
	return 1
}

func balanceRow(e woffu.AvailableUserEvent, w int) string {
	amount := formatAmount(e.Available, e.Unit)
	color := balanceColor(e)
	amt := fg(color).Bold(true).Render(amount)
	if e.Available <= 0 {
		amt = sFaint.Render(amount)
	}
	name := sText.Render(truncate(e.Name, w-lipgloss.Width(amount)-2))
	if e.Available <= 0 {
		name = sFaint.Render(truncate(e.Name, w-lipgloss.Width(amount)-2))
	}
	top := spread(name, amt, w)

	// A gentle gauge: days are compared to a working month, hours to a
	// working week, so the bar says "a lot" or "almost none" at a glance.
	scale := 22.0
	if unitRank(e.Unit) == 1 {
		scale = 40
	}
	return top + "\n" + progressBar(e.Available/scale, w, color)
}

func balanceColor(e woffu.AvailableUserEvent) lipgloss.Color {
	n := strings.ToLower(e.Name)
	switch {
	case strings.Contains(n, "vacacion"), strings.Contains(n, "asuntos"):
		return cTimeOff
	case strings.Contains(n, "bolsa"):
		return cBrand
	case strings.Contains(n, "médic"), strings.Contains(n, "medic"):
		return cRemote
	}
	return cOffice
}

// pendingRequestsSummary lists requests still waiting for approval this month.
func (d *Dashboard) pendingRequestsSummary(w int) string {
	type item struct {
		date string
		name string
	}
	var items []item
	for _, day := range d.homeDays {
		for _, r := range day.Requests {
			if r.Status == "pending" {
				items = append(items, item{day.Date, r.EventName})
			}
		}
	}
	if len(items) == 0 {
		return ""
	}
	var rows []string
	for i, it := range items {
		if i == 5 {
			rows = append(rows, sFaint.Render(fmt.Sprintf("+%d more — see Calendar", len(items)-5)))
			break
		}
		t, _ := time.Parse("2006-01-02", it.date)
		rows = append(rows, sWarn.Render("◷ ")+padRight(sSubtle.Render(t.Format("Mon 2 Jan")), 12)+sText.Render(truncate(it.name, w-14)))
	}
	return label("Waiting for approval") + "\n" + strings.Join(rows, "\n")
}

// ── Overlays ──

func (d *Dashboard) renderOverlay(h int) string {
	switch d.overlay {
	case overlayPalette:
		return d.renderPalette(h)
	case overlayDay:
		return d.renderDayMenu(h)
	case overlayConfirm:
		return d.renderConfirm()
	case overlayInput:
		return d.renderInput()
	case overlayHelp:
		return d.renderHelp()
	}
	return ""
}

func overlayBox(content string, accent lipgloss.Color, w int) string {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accent).
		Padding(1, 2).
		Width(w).
		Render(content)
}

// menuList renders actions with section headings, a cursor bar and a
// scrolling window that keeps the cursor visible.
func menuList(items []action, cursor, w, maxRows int, headings bool) string {
	type line struct {
		text string
		item int // -1 for headings
	}
	var lines []line
	lastSection := ""
	for i, a := range items {
		if headings && a.section != lastSection {
			if lastSection != "" {
				lines = append(lines, line{"", -1})
			}
			lines = append(lines, line{label(a.section), -1})
			lastSection = a.section
		}
		selected := i == cursor
		title := a.title
		st := sText
		switch {
		case a.disabled:
			st = sGhost
		case a.current:
			st = sSubtle
			title = "✓ " + title
		}
		left := st.Render(title)
		if a.hint != "" {
			left += sFaint.Render("  " + a.hint)
		}
		right := ""
		if a.shortcut != "" {
			right = sKey.Render(a.shortcut)
		}
		left = truncate(left, w-4-lipgloss.Width(right))
		row := spread(left, right, w-2)
		if selected {
			row = sBrand.Render("▌") + " " + highlightRow(left, right, w-2)
		} else {
			row = "  " + row
		}
		lines = append(lines, line{row, i})
	}

	// Scroll window around the cursor.
	cursorLine := 0
	for i, l := range lines {
		if l.item == cursor {
			cursorLine = i
		}
	}
	start := 0
	if len(lines) > maxRows {
		start = min(max(0, cursorLine-maxRows/2), len(lines)-maxRows)
	}
	end := min(len(lines), start+maxRows)
	var out []string
	if start > 0 {
		out = append(out, sGhost.Render("  ↑ more"))
		start++
	}
	cut := end < len(lines)
	if cut {
		end--
	}
	for _, l := range lines[start:end] {
		out = append(out, l.text)
	}
	if cut {
		out = append(out, sGhost.Render("  ↓ more"))
	}
	return strings.Join(out, "\n")
}

func highlightRow(left, right string, w int) string {
	bg := lipgloss.NewStyle().Background(cSurface)
	plainLeft := stripANSI(left)
	l := bg.Foreground(cText).Bold(true).Render(plainLeft)
	r := ""
	if right != "" {
		r = bg.Foreground(cBrand).Bold(true).Render(stripANSI(right))
	}
	gap := max(1, w-lipgloss.Width(l)-lipgloss.Width(r))
	return l + bg.Render(strings.Repeat(" ", gap)) + r
}

func (d *Dashboard) renderPalette(h int) string {
	w := min(68, d.width-6)
	items := d.filteredActions()

	prompt := sBrand.Render("› ")
	if d.query == "" {
		prompt += sFaint.Render("What do you want to do?") + sBrand.Render("▏")
	} else {
		prompt += sBold.Render(d.query) + sBrand.Render("▏")
	}

	var list string
	if len(items) == 0 {
		list = sFaint.Render("  Nothing matches “" + d.query + "”")
	} else {
		list = menuList(items, d.cursor, w-4, max(4, h-9), d.query == "")
	}
	return overlayBox(prompt+"\n"+rule(w-4)+"\n"+list, cBrand, w)
}

func (d *Dashboard) renderDayMenu(h int) string {
	w := min(60, d.width-6)
	var title string
	if n := len(d.cal.selected); n > 0 {
		title = sBold.Render(fmt.Sprintf("%d %s selected", n, plural(n, "day", "days"))) + "\n" +
			sFaint.Render(truncate(describeDates(d.cal.selectedDates()), w-6))
	} else {
		t, _ := time.Parse("2006-01-02", d.cal.cursorDate())
		info := d.cal.dayInfo(d.cal.cursor)
		look := lookOf(info)
		title = sBold.Render(t.Format("Monday 2 January")) + "\n" + fg(look.color).Render("▀▀ "+orDefault(look.label, "—"))
	}
	items := d.dayActions()
	var list string
	if len(items) == 0 {
		list = sFaint.Render("Nothing to do on this day.")
	} else {
		list = menuList(items, d.cursor, w-4, max(4, h-10), true)
	}
	return overlayBox(title+"\n\n"+list, cBrand, w)
}

func (d *Dashboard) renderConfirm() string {
	c := d.confirm
	if c == nil {
		return ""
	}
	w := min(60, d.width-6)
	accent := c.accent
	if accent == "" {
		accent = cBrand
	}
	parts := []string{label(c.title), "", fg(accent).Bold(true).Render(c.subject)}
	if len(c.lines) > 0 {
		parts = append(parts, "")
		for _, l := range c.lines {
			parts = append(parts, lipgloss.NewStyle().Width(w-6).Render(l))
		}
	}
	yesKey := "⏎"
	if c.danger {
		yesKey = "y"
	}
	btn := lipgloss.NewStyle().Background(accent).Foreground(lipgloss.Color("#1c1917")).Bold(true).Padding(0, 1).Render(yesKey + "  " + c.yes)
	parts = append(parts, "", btn+"   "+keycap("esc", "cancel"))
	return overlayBox(strings.Join(parts, "\n"), accent, w)
}

func (d *Dashboard) renderInput() string {
	w := min(52, d.width-6)
	field := sBold.Render(d.input) + sBrand.Render("▏")
	if d.input == "" {
		field = sBrand.Render("▏") + sFaint.Render("e.g. summer, intensive")
	}
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cLine).Padding(0, 1).Width(w - 8).Render(field)
	body := label("Save schedule as") + "\n\n" +
		sSubtle.Render("Keep the current sign times as a preset you can switch back to.") + "\n\n" +
		box + "\n\n" + keycap("⏎", "save") + "   " + keycap("esc", "cancel")
	return overlayBox(body, cBrand, w)
}

func (d *Dashboard) renderHelp() string {
	group := func(title string, rows [][2]string) string {
		out := []string{label(title)}
		for _, r := range rows {
			out = append(out, padRight(sKey.Render(r[0]), 13)+sSubtle.Render(r[1]))
		}
		return strings.Join(out, "\n")
	}
	everywhere := group("Everywhere", [][2]string{
		{"⏎  :  ctrl+k", "all actions"},
		{"1 2 3  tab", "switch screen"},
		{"s", "clock in / out"},
		{"r", "refresh"},
		{"o / g", "open Woffu / GitHub"},
		{"?", "this help"},
		{"q", "quit"},
	})
	autopilot := group("Autopilot", [][2]string{
		{"m", "sign from this Mac"},
		{"a", "GitHub backup signer"},
		{"e", "edit schedule"},
	})
	calendar := group("Calendar", [][2]string{
		{"←↑↓→ hjkl", "move"},
		{"[ ]", "previous / next month"},
		{".", "jump to today"},
		{"space", "select day"},
		{"shift+arrows", "select a range"},
		{"t v p b", "telework · vacation · personal · hours"},
		{"c", "cancel requests"},
		{"esc", "clear selection"},
		{"⏎", "all day actions"},
	})
	left := everywhere + "\n\n" + autopilot
	w := min(92, d.width-6)
	var body string
	if w >= 90 {
		body = twoColumns(left, calendar, 32, 4)
	} else {
		body = left + "\n\n" + calendar
	}
	body += "\n\n" + sFaint.Render("Every action that talks to Woffu asks first. Press any key to close.")
	return overlayBox(body, cBrand, w)
}
