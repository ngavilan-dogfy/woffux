package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/ngavilan-dogfy/woffux/internal/woffu"
)

const cellWidth = 6

// calendarGrid renders a visual monthly calendar with colored days.
type calendarGrid struct {
	year     int
	month    time.Month
	days     []woffu.CalendarDay
	cursor   int            // day of month (1-31), 0 = none
	selected map[string]bool // keyed by "YYYY-MM-DD" — persists across months
	width    int
}

func newCalendarGrid(year int, month time.Month, days []woffu.CalendarDay) *calendarGrid {
	today := time.Now().Day()
	currentMonth := time.Now().Month()
	currentYear := time.Now().Year()
	cursor := 0
	if month == currentMonth && year == currentYear {
		cursor = today
	} else {
		cursor = 1
	}

	return &calendarGrid{
		year:     year,
		month:    month,
		days:     days,
		cursor:   cursor,
		selected: make(map[string]bool),
	}
}

func (c *calendarGrid) dateStr(day int) string {
	return fmt.Sprintf("%d-%02d-%02d", c.year, c.month, day)
}

func (c *calendarGrid) daysInMonth() int {
	return time.Date(c.year, c.month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

func (c *calendarGrid) firstWeekday() int {
	wd := time.Date(c.year, c.month, 1, 0, 0, 0, 0, time.UTC).Weekday()
	if wd == time.Sunday {
		return 6
	}
	return int(wd) - 1
}

func (c *calendarGrid) dayInfo(day int) *woffu.CalendarDay {
	return c.dayInfoByDate(c.dateStr(day))
}

func (c *calendarGrid) dayInfoByDate(date string) *woffu.CalendarDay {
	for i := range c.days {
		if c.days[i].Date == date {
			return &c.days[i]
		}
	}
	return nil
}

// allEligibleDates returns selected dates eligible for request creation.
// Current month dates are filtered (must be working, no active request).
// Other month dates are included optimistically (API validates).
func (c *calendarGrid) allEligibleDates() []string {
	var dates []string
	for _, date := range c.selectedDates() {
		info := c.dayInfoByDate(date)
		if info != nil {
			// We have data — apply filters
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
			if hasActiveReq {
				continue
			}
		}
		// No info (other month) or passed filters — include
		dates = append(dates, date)
	}
	return dates
}

func (c *calendarGrid) toggleSelect(day int) {
	ds := c.dateStr(day)
	if c.selected[ds] {
		delete(c.selected, ds)
	} else {
		c.selected[ds] = true
	}
}

func (c *calendarGrid) clearSelection() {
	c.selected = make(map[string]bool)
}

func (c *calendarGrid) selectedDates() []string {
	dates := make([]string, 0, len(c.selected))
	for d := range c.selected {
		dates = append(dates, d)
	}
	sort.Strings(dates)
	return dates
}

// selectedDayInfos returns CalendarDay for selected days in the current month.
func (c *calendarGrid) selectedDayInfos() []*woffu.CalendarDay {
	var infos []*woffu.CalendarDay
	for d := 1; d <= c.daysInMonth(); d++ {
		if c.selected[c.dateStr(d)] {
			if info := c.dayInfo(d); info != nil {
				infos = append(infos, info)
			}
		}
	}
	return infos
}

// currentMonthSelectedCount returns how many selected days are in the displayed month.
func (c *calendarGrid) currentMonthSelectedCount() int {
	count := 0
	for d := 1; d <= c.daysInMonth(); d++ {
		if c.selected[c.dateStr(d)] {
			count++
		}
	}
	return count
}

func (c *calendarGrid) monthStats() (working, telework, holidays, weekends, absences int) {
	for _, d := range c.days {
		switch d.Status {
		case "working":
			working++
			if d.Mode == "remote" {
				telework++
			}
		case "holiday":
			holidays++
		case "weekend":
			weekends++
		case "absence":
			absences++
		}
	}
	return
}

// ── Navigation ──

// moveLeft moves cursor left; returns true if month changed (needs data fetch).
func (c *calendarGrid) moveLeft() bool {
	if c.cursor > 1 {
		c.cursor--
		return false
	}
	// At first day — go to previous month
	c.prevMonth()
	c.cursor = c.daysInMonth()
	return true
}

// moveRight moves cursor right; returns true if month changed (needs data fetch).
func (c *calendarGrid) moveRight() bool {
	if c.cursor < c.daysInMonth() {
		c.cursor++
		return false
	}
	// At last day — go to next month
	c.nextMonth()
	c.cursor = 1
	return true
}

// moveUp moves cursor up by one week; returns true if month changed.
func (c *calendarGrid) moveUp() bool {
	if c.cursor > 7 {
		c.cursor -= 7
		return false
	}
	// Would go before day 1 — go to previous month, keeping relative position
	targetDay := c.cursor + c.daysInPrevMonth() - 7
	c.prevMonth()
	dim := c.daysInMonth()
	if targetDay > dim {
		targetDay = dim
	}
	if targetDay < 1 {
		targetDay = 1
	}
	c.cursor = targetDay
	return true
}

// moveDown moves cursor down by one week; returns true if month changed.
func (c *calendarGrid) moveDown() bool {
	if c.cursor+7 <= c.daysInMonth() {
		c.cursor += 7
		return false
	}
	// Would go past last day — go to next month
	overflow := c.cursor + 7 - c.daysInMonth()
	c.nextMonth()
	dim := c.daysInMonth()
	if overflow > dim {
		overflow = dim
	}
	c.cursor = overflow
	return true
}

func (c *calendarGrid) daysInPrevMonth() int {
	if c.month == time.January {
		return time.Date(c.year-1, time.December+1, 0, 0, 0, 0, 0, time.UTC).Day()
	}
	return time.Date(c.year, c.month, 0, 0, 0, 0, 0, time.UTC).Day()
}

// ── Range selection: move + select origin and destination ──

func (c *calendarGrid) moveLeftSelect() {
	if c.cursor > 1 {
		c.selected[c.dateStr(c.cursor)] = true
		c.cursor--
		c.selected[c.dateStr(c.cursor)] = true
	}
}

func (c *calendarGrid) moveRightSelect() {
	if c.cursor < c.daysInMonth() {
		c.selected[c.dateStr(c.cursor)] = true
		c.cursor++
		c.selected[c.dateStr(c.cursor)] = true
	}
}

func (c *calendarGrid) moveUpSelect() {
	if c.cursor > 7 {
		start := c.cursor
		c.cursor -= 7
		for d := c.cursor; d <= start; d++ {
			c.selected[c.dateStr(d)] = true
		}
	}
}

func (c *calendarGrid) moveDownSelect() {
	if c.cursor+7 <= c.daysInMonth() {
		start := c.cursor
		c.cursor += 7
		for d := start; d <= c.cursor; d++ {
			c.selected[c.dateStr(d)] = true
		}
	}
}

// ── Month navigation — selections persist ──

func (c *calendarGrid) prevMonth() {
	if c.month == time.January {
		c.month = time.December
		c.year--
	} else {
		c.month--
	}
	c.cursor = 1
}

func (c *calendarGrid) nextMonth() {
	if c.month == time.December {
		c.month = time.January
		c.year++
	} else {
		c.month++
	}
	c.cursor = 1
}

// ── Rendering ──

func (c *calendarGrid) render() string {
	var b strings.Builder

	// Month header — prominent with navigation arrows
	monthName := strings.ToUpper(c.month.String())
	leftArrow := lipgloss.NewStyle().Foreground(colorDim).Bold(true).Render("\u25C0") // ◀
	rightArrow := lipgloss.NewStyle().Foreground(colorDim).Bold(true).Render("\u25B6") // ▶
	monthLabel := lipgloss.NewStyle().Bold(true).Foreground(colorPrimary).Render(monthName)
	yearLabel := lipgloss.NewStyle().Bold(true).Foreground(colorAccent).Render(fmt.Sprintf("%d", c.year))
	header := fmt.Sprintf("%s   %s %s   %s", leftArrow, monthLabel, yearLabel, rightArrow)

	// Center the header over the grid (4 + 7*cellWidth)
	gridWidth := 4 + 7*cellWidth
	headerWidth := lipgloss.Width(header)
	headerPad := (gridWidth - headerWidth) / 2
	if headerPad < 0 {
		headerPad = 0
	}
	b.WriteString(strings.Repeat(" ", headerPad) + header)
	b.WriteString("\n")
	// Subtle underline for the header
	b.WriteString("    " + lipgloss.NewStyle().Foreground(colorSeparator).Render(strings.Repeat("\u2500", 7*cellWidth))) // ─
	b.WriteString("\n")

	// Day names header with week number spacer
	b.WriteString(lipgloss.NewStyle().Foreground(colorDim).Width(4).Render(""))
	dayNames := []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}
	for _, d := range dayNames {
		b.WriteString(lipgloss.NewStyle().Foreground(colorDim).Width(cellWidth).Align(lipgloss.Center).Render(d))
	}
	b.WriteString("\n")

	// Calendar grid
	firstDay := c.firstWeekday()
	totalDays := c.daysInMonth()
	today := time.Now()
	isCurrentMonth := c.month == today.Month() && c.year == today.Year()

	day := 1
	for week := 0; week < 6; week++ {
		if day > totalDays {
			break
		}

		// Week number
		wkDay := day
		if week == 0 {
			wkDay = 1
		}
		_, wk := time.Date(c.year, c.month, wkDay, 0, 0, 0, 0, time.UTC).ISOWeek()
		b.WriteString(lipgloss.NewStyle().Foreground(colorDim).Width(4).Render(fmt.Sprintf("%d", wk)))

		for col := 0; col < 7; col++ {
			if week == 0 && col < firstDay {
				b.WriteString(strings.Repeat(" ", cellWidth))
				continue
			}
			if day > totalDays {
				break
			}

			cell := c.renderDay(day, col, isCurrentMonth && day == today.Day())
			b.WriteString(cell)
			day++
		}
		b.WriteString("\n")
	}

	// Month stats
	working, telework, holidays, weekends, absences := c.monthStats()
	var stats []string
	stats = append(stats, fmt.Sprintf("%d working", working))
	if telework > 0 {
		stats = append(stats, fmt.Sprintf("%d telework", telework))
	}
	if holidays > 0 {
		stats = append(stats, fmt.Sprintf("%d holidays", holidays))
	}
	if absences > 0 {
		stats = append(stats, fmt.Sprintf("%d absences", absences))
	}
	stats = append(stats, fmt.Sprintf("%d weekends", weekends))
	b.WriteString("\n    " + sDimmed.Render(strings.Join(stats, " · ")))

	// Compact legend
	b.WriteString("\n    ")
	dot := func(c lipgloss.Color, label string) string {
		return lipgloss.NewStyle().Foreground(c).Render("●") + sDimmed.Render(label)
	}
	b.WriteString(dot(colorSuccess, " office "))
	b.WriteString(dot(colorSecondary, " remote "))
	b.WriteString(dot(colorDanger, " holiday "))
	b.WriteString(dot(colorWarning, " absence "))
	b.WriteString(dot(colorDim, " weekend"))
	b.WriteString("   ")
	b.WriteString(lipgloss.NewStyle().Underline(true).Foreground(colorText).Render("N") + sDimmed.Render("=approved "))
	b.WriteString(lipgloss.NewStyle().Italic(true).Foreground(colorText).Render("N") + sDimmed.Render("=pending"))

	// Selected count
	total := len(c.selected)
	if total > 0 {
		thisMonth := c.currentMonthSelectedCount()
		label := fmt.Sprintf("%d selected", total)
		if thisMonth < total {
			label = fmt.Sprintf("%d selected (%d this month)", total, thisMonth)
		}
		b.WriteString("\n\n")
		b.WriteString("    " + lipgloss.NewStyle().Bold(true).Foreground(colorPrimary).Render(label))
		b.WriteString("  " + sDimmed.Render("enter=action  x=clear"))
	}

	// Day detail panel
	b.WriteString(c.renderDayDetail())

	return b.String()
}

// renderDayDetail renders the context panel for the cursor day.
func (c *calendarGrid) renderDayDetail() string {
	info := c.dayInfo(c.cursor)
	if info == nil {
		return ""
	}

	var b strings.Builder
	b.WriteString("\n\n")

	// Date + weekday + status
	t, _ := time.Parse("2006-01-02", info.Date)
	dateLabel := t.Format("Mon 2 Jan")
	b.WriteString("    " + sValue.Render(dateLabel) + "  ")

	switch info.Status {
	case "working":
		if info.Mode == "remote" {
			b.WriteString(lipgloss.NewStyle().Foreground(colorSecondary).Bold(true).Render("Remote"))
		} else {
			b.WriteString(lipgloss.NewStyle().Foreground(colorSuccess).Bold(true).Render("Office"))
		}
	case "holiday":
		b.WriteString(lipgloss.NewStyle().Foreground(colorDanger).Bold(true).Render("Holiday"))
	case "weekend":
		b.WriteString(sDimmed.Render("Weekend"))
	case "absence":
		b.WriteString(lipgloss.NewStyle().Foreground(colorWarning).Bold(true).Render("Absence"))
	}

	if len(info.EventNames) > 0 {
		b.WriteString("  " + sDimmed.Render(strings.Join(info.EventNames, ", ")))
	}

	// Requests
	if len(info.Requests) > 0 {
		b.WriteString("\n")
		for _, r := range info.Requests {
			statusStyle := sDimmed
			statusIcon := "○"
			switch r.Status {
			case "approved":
				statusStyle = lipgloss.NewStyle().Foreground(colorSuccess)
				statusIcon = "✓"
			case "pending":
				statusStyle = lipgloss.NewStyle().Foreground(colorWarning)
				statusIcon = "◷"
			case "rejected":
				statusStyle = lipgloss.NewStyle().Foreground(colorDanger)
				statusIcon = "✗"
			}
			b.WriteString(fmt.Sprintf("      %s %s  %s",
				statusStyle.Render(statusIcon),
				lipgloss.NewStyle().Foreground(colorText).Render(r.EventName),
				statusStyle.Render(r.Status)))
			b.WriteString(fmt.Sprintf("  %s", sDimmed.Render(fmt.Sprintf("#%d", r.RequestID))))
			b.WriteString("\n")
		}
	}

	// Sign slots
	if len(info.Signs) > 0 {
		b.WriteString("      ")
		for _, s := range info.Signs {
			if s.In != "" {
				b.WriteString(lipgloss.NewStyle().Foreground(colorSuccess).Render("IN") + " " + sValue.Render(extractTimeFromDT(s.In)) + "  ")
			}
			if s.Out != "" {
				b.WriteString(lipgloss.NewStyle().Foreground(colorDanger).Render("OUT") + " " + sValue.Render(extractTimeFromDT(s.Out)) + "  ")
			}
		}
		b.WriteString("\n")
	}

	return b.String()
}

func extractTimeFromDT(dt string) string {
	if idx := strings.Index(dt, "T"); idx != -1 {
		t := dt[idx+1:]
		if len(t) >= 5 {
			return t[:5]
		}
	}
	return dt
}

func (c *calendarGrid) renderDay(day, col int, isToday bool) string {
	info := c.dayInfo(day)

	label := fmt.Sprintf("%2d", day)

	style := lipgloss.NewStyle().Width(cellWidth).Align(lipgloss.Center)

	// Color based on day status
	if info != nil {
		switch info.Status {
		case "weekend":
			style = style.Foreground(colorDim)
		case "holiday":
			style = style.Foreground(colorDanger)
		case "absence":
			style = style.Foreground(colorWarning)
		case "working":
			if info.Mode == "remote" {
				style = style.Foreground(colorSecondary)
			} else {
				style = style.Foreground(colorSuccess)
			}
		}

		// Request/presence status: underline=approved, italic=pending
		hasApproved := false
		hasPending := info.HasPendingPresence
		for _, r := range info.Requests {
			switch r.Status {
			case "approved":
				hasApproved = true
			case "pending":
				hasPending = true
			}
		}
		// Approved telework from PresenceEvents (mode=remote, not pending)
		if info.Mode == "remote" && !info.HasPendingPresence {
			hasApproved = true
		}
		if hasApproved {
			style = style.Underline(true)
		} else if hasPending {
			style = style.Italic(true)
		}
	} else if col >= 5 {
		style = style.Foreground(colorDim)
	}

	// Today highlight (bold only — underline reserved for approved requests)
	if isToday {
		style = style.Bold(true)
	}

	// Selected
	ds := c.dateStr(day)
	if c.selected[ds] {
		style = style.Background(lipgloss.Color("#7c3aed")).Foreground(lipgloss.Color("#ffffff"))
	}

	// Cursor
	if day == c.cursor {
		if !c.selected[ds] {
			style = style.Background(lipgloss.Color("#374151"))
		}
		style = style.Bold(true)
	}

	return style.Render(label)
}
