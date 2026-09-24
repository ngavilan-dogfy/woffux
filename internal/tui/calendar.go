package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/ngavilan-dogfy/woffux/internal/woffu"
)

// calendarGrid is the interactive month view. Selections are keyed by date
// so they survive month changes (select across a month boundary).
type calendarGrid struct {
	year     int
	month    time.Month
	days     []woffu.CalendarDay
	loaded   bool
	cursor   int // day of month
	selected map[string]bool
	// months caches every month loaded this session, so selections that
	// span months are checked against real data and month flips are instant.
	months map[string][]woffu.CalendarDay
}

func monthKey(year int, month time.Month) string { return fmt.Sprintf("%d-%02d", year, month) }

func newCalendarGrid(year int, month time.Month, now time.Time) *calendarGrid {
	c := &calendarGrid{year: year, month: month, cursor: 1, selected: map[string]bool{}, months: map[string][]woffu.CalendarDay{}}
	if year == now.Year() && month == now.Month() {
		c.cursor = now.Day()
	}
	return c
}

func (c *calendarGrid) setDays(days []woffu.CalendarDay) {
	c.days = days
	c.loaded = true
	c.months[monthKey(c.year, c.month)] = days
}

// cacheMonth stores data for a month that may not be on screen.
func (c *calendarGrid) cacheMonth(year int, month time.Month, days []woffu.CalendarDay) {
	c.months[monthKey(year, month)] = days
}

// forgetOtherMonths drops cached months other than the visible one, after
// requests change data that the cache can't see.
func (c *calendarGrid) forgetOtherMonths() {
	keep := monthKey(c.year, c.month)
	for k := range c.months {
		if k != keep {
			delete(c.months, k)
		}
	}
}

// showMonth switches the visible month, using cached data when present
// (the caller still refetches to pick up changes).
func (c *calendarGrid) showMonth(year int, month time.Month) {
	c.year, c.month = year, month
	c.days, c.loaded = c.months[monthKey(year, month)]
}

func (c *calendarGrid) dateStr(day int) string {
	return fmt.Sprintf("%d-%02d-%02d", c.year, c.month, day)
}

func (c *calendarGrid) cursorDate() string { return c.dateStr(c.cursor) }

func (c *calendarGrid) daysInMonth() int {
	return time.Date(c.year, c.month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// firstWeekday is the column (Mon=0) of the 1st.
func (c *calendarGrid) firstWeekday() int {
	wd := time.Date(c.year, c.month, 1, 0, 0, 0, 0, time.UTC).Weekday()
	return (int(wd) + 6) % 7
}

func (c *calendarGrid) dayInfo(day int) *woffu.CalendarDay { return c.dayInfoByDate(c.dateStr(day)) }

func (c *calendarGrid) dayInfoByDate(date string) *woffu.CalendarDay {
	days := c.days
	if len(date) >= 7 && date[:7] != monthKey(c.year, c.month) {
		days = c.months[date[:7]]
	}
	for i := range days {
		if days[i].Date == date {
			return &days[i]
		}
	}
	return nil
}

// ── Navigation ──

// move shifts the cursor by delta days; returns true when the month changed
// (the caller must fetch that month).
func (c *calendarGrid) move(delta int) bool {
	t := time.Date(c.year, c.month, c.cursor, 0, 0, 0, 0, time.UTC).AddDate(0, 0, delta)
	changed := t.Year() != c.year || t.Month() != c.month
	if changed {
		c.showMonth(t.Year(), t.Month())
	}
	c.cursor = t.Day()
	return changed
}

// extend grows the selection from the cursor, within the month.
func (c *calendarGrid) extend(delta int) {
	target := min(max(c.cursor+delta, 1), c.daysInMonth())
	lo, hi := c.cursor, target
	if lo > hi {
		lo, hi = hi, lo
	}
	for d := lo; d <= hi; d++ {
		if wd := time.Date(c.year, c.month, d, 0, 0, 0, 0, time.UTC).Weekday(); wd == time.Saturday || wd == time.Sunday {
			continue // ranges skip weekends: nobody requests those
		}
		c.selected[c.dateStr(d)] = true
	}
	c.cursor = target
}

func (c *calendarGrid) shiftMonth(delta int) {
	t := time.Date(c.year, c.month, 1, 0, 0, 0, 0, time.UTC).AddDate(0, delta, 0)
	c.showMonth(t.Year(), t.Month())
	if dim := c.daysInMonth(); c.cursor > dim {
		c.cursor = dim
	}
}

// jumpToday moves to today; returns true when the month changed.
func (c *calendarGrid) jumpToday(now time.Time) bool {
	changed := c.year != now.Year() || c.month != now.Month()
	if changed {
		c.showMonth(now.Year(), now.Month())
	}
	c.cursor = now.Day()
	return changed
}

// ── Selection ──

func (c *calendarGrid) toggleSelect(date string) {
	if c.selected[date] {
		delete(c.selected, date)
	} else {
		c.selected[date] = true
	}
}

func (c *calendarGrid) clearSelection() { c.selected = map[string]bool{} }

func (c *calendarGrid) selectedDates() []string {
	dates := make([]string, 0, len(c.selected))
	for d := range c.selected {
		dates = append(dates, d)
	}
	sort.Strings(dates)
	return dates
}

// eligibleDates filters dates that can take a new request: working days
// without an active request. A date we have no data for is never eligible:
// we won't send what we can't check.
func (c *calendarGrid) eligibleDates(dates []string) []string {
	var out []string
	for _, date := range dates {
		info := c.dayInfoByDate(date)
		if info == nil || info.Status != "working" || hasActiveRequest(info) {
			continue
		}
		out = append(out, date)
	}
	return out
}

func hasActiveRequest(info *woffu.CalendarDay) bool {
	for _, r := range info.Requests {
		if r.Status == "pending" || r.Status == "approved" {
			return true
		}
	}
	return false
}

// ── Day classification for display ──

type dayLook struct {
	color   lipgloss.Color
	label   string
	pending bool // shown with a dashed bar
}

func lookOf(info *woffu.CalendarDay) dayLook {
	if info == nil {
		return dayLook{color: cGhost}
	}
	pending := info.HasPendingPresence
	for _, r := range info.Requests {
		if r.Status == "pending" {
			pending = true
		}
	}
	switch info.Status {
	case "weekend":
		return dayLook{color: cGhost, label: "Weekend"}
	case "holiday":
		return dayLook{color: cHoliday, label: "Holiday"}
	case "absence":
		return dayLook{color: cTimeOff, label: "Time off", pending: pending}
	}
	for _, r := range info.Requests {
		if r.Status == "pending" && !isTeleworkName(r.EventName) {
			return dayLook{color: cTimeOff, label: r.EventName, pending: true}
		}
		if r.Status == "approved" && !isTeleworkName(r.EventName) {
			return dayLook{color: cTimeOff, label: r.EventName}
		}
	}
	if info.Mode == "remote" {
		return dayLook{color: cRemote, label: "Remote", pending: pending}
	}
	return dayLook{color: cOffice, label: "Office", pending: pending}
}

// ── Rendering ──

// render draws the month grid, sized to width. Each day is two rows: the
// number, then a colored bar carrying the day type.
func (c *calendarGrid) renderGrid(width int, now time.Time) string {
	// The week-number column is the first thing to go on narrow terminals.
	wkW := 4
	if width < 4+7*6 {
		wkW = 0
	}
	cell := min(max((width-wkW)/7, 5), 9)
	var b strings.Builder

	// Month title
	title := sBold.Render(c.month.String()) + " " + sSubtle.Render(fmt.Sprintf("%d", c.year))
	nav := sFaint.Render("‹ [") + "  " + title + "  " + sFaint.Render("] ›")
	gridW := wkW + 7*cell
	b.WriteString(lipgloss.PlaceHorizontal(gridW, lipgloss.Center, nav))
	b.WriteString("\n\n")

	// Weekday header
	if wkW > 0 {
		b.WriteString(padRight(sGhost.Render("wk"), wkW))
	}
	for i, n := range []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"} {
		st := sFaint
		if i >= 5 {
			st = sGhost
		}
		b.WriteString(padRight(" "+st.Render(n), cell))
	}
	b.WriteString("\n")

	todayKey := now.Format("2006-01-02")
	first := c.firstWeekday()
	total := c.daysInMonth()
	day := 1 - first
	for day <= total {
		var top, bottom strings.Builder
		ref := day
		if ref < 1 {
			ref = 1
		}
		_, wk := time.Date(c.year, c.month, ref, 0, 0, 0, 0, time.UTC).ISOWeek()
		if wkW > 0 {
			top.WriteString(padRight(sGhost.Render(fmt.Sprintf("%d", wk)), wkW))
			bottom.WriteString(strings.Repeat(" ", wkW))
		}
		for col := 0; col < 7; col++ {
			if day < 1 || day > total {
				top.WriteString(strings.Repeat(" ", cell))
				bottom.WriteString(strings.Repeat(" ", cell))
				day++
				continue
			}
			t, bt := c.renderCell(day, cell, todayKey, now)
			top.WriteString(t)
			bottom.WriteString(bt)
			day++
		}
		b.WriteString(top.String() + "\n" + bottom.String() + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (c *calendarGrid) renderCell(day, cell int, todayKey string, now time.Time) (string, string) {
	date := c.dateStr(day)
	info := c.dayInfo(day)
	look := lookOf(info)
	isToday := date == todayKey
	isCursor := day == c.cursor
	isSel := c.selected[date]
	isPast := date < todayKey
	inner := cell - 1 // 1-col gutter between cells

	var bg lipgloss.Color
	switch {
	case isSel && isCursor:
		bg = cBrand
	case isSel:
		bg = cBrandDeep
	case isCursor:
		bg = cSurface
	}
	paint := func(st lipgloss.Style) lipgloss.Style {
		if bg != "" {
			return st.Background(bg)
		}
		return st
	}

	numStyle := sText
	switch {
	case info == nil:
		numStyle = sFaint
	case info.Status == "weekend":
		numStyle = sGhost
	case isPast:
		numStyle = sSubtle
	}
	if isToday {
		numStyle = sBrand
	}
	if isSel {
		numStyle = numStyle.Foreground(lipgloss.Color("#ffffff")).Bold(true)
	}

	// Sign mark: ✓ signed, ! a past working day with no signs at all.
	mark, markStyle := " ", sFaint
	if info != nil {
		switch {
		case len(info.Signs) > 0:
			mark, markStyle = "✓", sOK
		case info.Status == "working" && isPast && !hasTimeOff(info):
			mark, markStyle = "!", sBad
		}
	}
	dot, dotStyle := " ", sBrand
	if isToday {
		dot = "•"
	}
	if inner < 5 {
		dot = "" // no room: today is still marked by its color
	}
	sp := paint(lipgloss.NewStyle())
	top := sp.Render(" ") + paint(numStyle).Render(fmt.Sprintf("%2d", day)) +
		paint(dotStyle).Render(dot) + paint(markStyle).Render(mark)
	top += sp.Render(strings.Repeat(" ", max(0, inner-lipgloss.Width(top))))

	barChar := "▀"
	if look.pending {
		barChar = "╌"
	}
	var bar string
	if info != nil && info.Status == "weekend" {
		bar = sp.Render(strings.Repeat(" ", inner))
	} else {
		bar = sp.Render(" ") + paint(fg(look.color)).Render(strings.Repeat(barChar, max(1, inner-2))) + sp.Render(" ")
	}

	edge := " "
	if isCursor {
		edge = sBrand.Render("▏")
	}
	return top + edge, bar + edge
}

func hasTimeOff(info *woffu.CalendarDay) bool {
	for _, r := range info.Requests {
		if r.Status == "approved" && !isTeleworkName(r.EventName) {
			return true
		}
	}
	return false
}

// monthSummary counts the month by day type.
func (c *calendarGrid) monthSummary(width int) string {
	var office, remote, holiday, off int
	for i := range c.days {
		info := &c.days[i]
		switch lookOf(info).color {
		case cOffice:
			office++
		case cRemote:
			remote++
		case cHoliday:
			holiday++
		case cTimeOff:
			off++
		}
	}
	var parts []string
	add := func(n int, color lipgloss.Color, one, many string) {
		if n > 0 {
			parts = append(parts, fg(color).Render("▀")+" "+sSubtle.Render(fmt.Sprintf("%d %s", n, plural(n, one, many))))
		}
	}
	add(office, cOffice, "office", "office")
	add(remote, cRemote, "remote", "remote")
	add(off, cTimeOff, "day off", "days off")
	add(holiday, cHoliday, "holiday", "holidays")
	return flow(parts, "   ", width)
}
