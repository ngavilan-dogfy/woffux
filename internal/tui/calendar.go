package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/ngavilan-dogfy/woffux/internal/woffu"
)

const cellWidth = 6 // wider cells to fit badge

// calendarGrid renders a visual monthly calendar with colored days and badges.
type calendarGrid struct {
	year     int
	month    time.Month
	days     []woffu.CalendarDay
	cursor   int // day of month (1-31), 0 = none
	selected map[int]bool
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
		selected: make(map[int]bool),
	}
}

func (c *calendarGrid) daysInMonth() int {
	return time.Date(c.year, c.month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

func (c *calendarGrid) firstWeekday() int {
	// Monday = 0, Sunday = 6
	wd := time.Date(c.year, c.month, 1, 0, 0, 0, 0, time.UTC).Weekday()
	if wd == time.Sunday {
		return 6
	}
	return int(wd) - 1
}

func (c *calendarGrid) dayInfo(day int) *woffu.CalendarDay {
	date := fmt.Sprintf("%d-%02d-%02d", c.year, c.month, day)
	for i := range c.days {
		if c.days[i].Date == date {
			return &c.days[i]
		}
	}
	return nil
}

func (c *calendarGrid) toggleSelect(day int) {
	if c.selected[day] {
		delete(c.selected, day)
	} else {
		c.selected[day] = true
	}
}

func (c *calendarGrid) clearSelection() {
	c.selected = make(map[int]bool)
}

func (c *calendarGrid) selectedDates() []string {
	var dates []string
	for d := 1; d <= c.daysInMonth(); d++ {
		if c.selected[d] {
			dates = append(dates, fmt.Sprintf("%d-%02d-%02d", c.year, c.month, d))
		}
	}
	return dates
}

// selectedDayInfos returns the CalendarDay for each selected day.
func (c *calendarGrid) selectedDayInfos() []*woffu.CalendarDay {
	var infos []*woffu.CalendarDay
	for d := 1; d <= c.daysInMonth(); d++ {
		if c.selected[d] {
			if info := c.dayInfo(d); info != nil {
				infos = append(infos, info)
			}
		}
	}
	return infos
}

func (c *calendarGrid) moveLeft() {
	if c.cursor > 1 {
		c.cursor--
	}
}

func (c *calendarGrid) moveRight() {
	if c.cursor < c.daysInMonth() {
		c.cursor++
	}
}

func (c *calendarGrid) moveUp() {
	if c.cursor > 7 {
		c.cursor -= 7
	}
}

func (c *calendarGrid) moveDown() {
	if c.cursor+7 <= c.daysInMonth() {
		c.cursor += 7
	}
}

// Range selection: move + select both origin and destination

func (c *calendarGrid) moveLeftSelect() {
	if c.cursor > 1 {
		c.selected[c.cursor] = true
		c.cursor--
		c.selected[c.cursor] = true
	}
}

func (c *calendarGrid) moveRightSelect() {
	if c.cursor < c.daysInMonth() {
		c.selected[c.cursor] = true
		c.cursor++
		c.selected[c.cursor] = true
	}
}

func (c *calendarGrid) moveUpSelect() {
	if c.cursor > 7 {
		start := c.cursor
		c.cursor -= 7
		for d := c.cursor; d <= start; d++ {
			c.selected[d] = true
		}
	}
}

func (c *calendarGrid) moveDownSelect() {
	if c.cursor+7 <= c.daysInMonth() {
		start := c.cursor
		c.cursor += 7
		for d := start; d <= c.cursor; d++ {
			c.selected[d] = true
		}
	}
}

func (c *calendarGrid) prevMonth() {
	if c.month == time.January {
		c.month = time.December
		c.year--
	} else {
		c.month--
	}
	c.cursor = 1
	c.selected = make(map[int]bool)
}

func (c *calendarGrid) nextMonth() {
	if c.month == time.December {
		c.month = time.January
		c.year++
	} else {
		c.month++
	}
	c.cursor = 1
	c.selected = make(map[int]bool)
}

func (c *calendarGrid) render() string {
	var b strings.Builder

	// Month header with navigation
	monthName := c.month.String()
	header := fmt.Sprintf("◀  %s %d  ▶", monthName, c.year)
	b.WriteString("  " + lipgloss.NewStyle().Bold(true).Foreground(colorPrimary).Render(header))
	b.WriteString("\n\n")

	// Day names
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

	// Badge legend
	b.WriteString("\n")
	b.WriteString("  ")
	b.WriteString(lipgloss.NewStyle().Foreground(colorSecondary).Bold(true).Render("T") + " telework  ")
	b.WriteString(lipgloss.NewStyle().Foreground(colorWarning).Bold(true).Render("V") + " vacation  ")
	b.WriteString(lipgloss.NewStyle().Foreground(colorDanger).Bold(true).Render("H") + " holiday  ")
	b.WriteString(lipgloss.NewStyle().Foreground(colorWarning).Bold(true).Render("A") + " absence  ")
	b.WriteString(lipgloss.NewStyle().Foreground(colorSuccess).Bold(true).Render("●") + " signed")

	// Selected count
	if len(c.selected) > 0 {
		b.WriteString("\n\n")
		b.WriteString("  " + lipgloss.NewStyle().Bold(true).Foreground(colorPrimary).Render(
			fmt.Sprintf("%d days selected", len(c.selected))))
		b.WriteString("  " + sDimmed.Render("enter=action  x=clear"))
	}

	// Day detail panel
	b.WriteString(c.renderDayDetail())

	return b.String()
}

// renderDayDetail renders the rich context panel for the cursor day.
func (c *calendarGrid) renderDayDetail() string {
	info := c.dayInfo(c.cursor)
	if info == nil {
		return ""
	}

	var b strings.Builder
	b.WriteString("\n\n")

	// Date + status
	dateStyle := sValue
	b.WriteString("  " + dateStyle.Render(info.Date) + "  ")

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

	// Event names (holidays, etc.)
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
			b.WriteString(fmt.Sprintf("    %s %s  %s",
				statusStyle.Render(statusIcon),
				lipgloss.NewStyle().Foreground(colorText).Render(r.EventName),
				statusStyle.Render(r.Status)))
			b.WriteString(fmt.Sprintf("  %s", sDimmed.Render(fmt.Sprintf("#%d", r.RequestID))))
			b.WriteString("\n")
		}
	}

	// Sign slots
	if len(info.Signs) > 0 {
		b.WriteString("    ")
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

// extractTimeFromDT extracts HH:MM from a datetime string.
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
	badge := ""
	if info != nil {
		badge = info.Badge()
	}

	// Build cell content: "DD" or "DDb" where b is badge
	label := fmt.Sprintf("%2d", day)
	if badge != "" {
		label = fmt.Sprintf("%2d%s", day, badge)
	}

	style := lipgloss.NewStyle().Width(cellWidth).Align(lipgloss.Center)

	// Color based on status
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
	} else if col >= 5 {
		style = style.Foreground(colorDim)
	}

	// Today highlight
	if isToday {
		style = style.Underline(true).Bold(true)
	}

	// Selected
	if c.selected[day] {
		style = style.Background(lipgloss.Color("#7c3aed")).Foreground(lipgloss.Color("#ffffff"))
	}

	// Cursor
	if day == c.cursor {
		if !c.selected[day] {
			style = style.Background(lipgloss.Color("#374151"))
		}
		style = style.Bold(true)
	}

	return style.Render(label)
}
