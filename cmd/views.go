package cmd

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/ngavilan-dogfy/woffux/internal/woffu"
)

// Human (TTY) views for the query commands. JSON and TSV outputs stay in
// each command; these only replace what people read in a terminal.

func dayVerdict(info *woffu.SignInfo) string {
	if info.IsWorkingDay {
		return stIn.Render("● working day") + stFaint.Render("  ·  ") + uiMode(info.Mode)
	}
	for _, e := range info.NextEvents {
		if e.Date == info.Date && len(e.Names) > 0 {
			return lipgloss.NewStyle().Foreground(lipgloss.Color("#f472b6")).Render("✦ day off · " + strings.Join(uniqueStrings(e.Names), ", "))
		}
	}
	if wd := time.Now().Weekday(); wd == time.Saturday || wd == time.Sunday {
		return stSubtle.Render("☾ weekend")
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("#facc15")).Render("☀ day off")
}

func viewStatus(info *woffu.SignInfo) {
	t, _ := time.Parse("2006-01-02", info.Date)
	uiTitle("Today", t.Format("Monday 2 January"))
	uiLine(dayVerdict(info))
	if info.IsWorkingDay {
		uiRow("Signs from", stSubtle.Render(fmt.Sprintf("%.4f, %.4f", info.Latitude, info.Longitude)))
	}
	viewUpcoming(info, 5)
	uiHint("woffux today", "woffux")
}

func viewUpcoming(info *woffu.SignInfo, n int) {
	var rows []string
	for _, e := range info.NextEvents {
		if e.Date <= info.Date || len(rows) >= n {
			continue
		}
		name := "day off"
		if u := uniqueStrings(e.Names); len(u) > 0 {
			name = strings.Join(u, ", ")
		}
		rows = append(rows, uiIndent+stSubtle.Render(fmt.Sprintf("%-14s", uiDate(e.Date)))+" "+lipgloss.NewStyle().Foreground(lipgloss.Color("#f472b6")).Render(name))
	}
	if len(rows) == 0 {
		return
	}
	uiSection("Coming up")
	fmt.Println(strings.Join(rows, "\n"))
}

func viewToday(info *woffu.SignInfo, slots []woffu.SignSlot) {
	t, _ := time.Parse("2006-01-02", info.Date)
	uiTitle("Today", t.Format("Monday 2 January"))
	uiLine(dayVerdict(info))

	uiSection("Signs")
	if len(slots) == 0 {
		uiLine(stFaint.Render("Nothing signed yet today."))
	}
	var worked time.Duration
	now := time.Now()
	for _, s := range slots {
		line := uiIndent
		if s.In != "" {
			line += stIn.Render("IN  ") + stBold.Render(slotTime(s.In))
		}
		in := parseWoffuStamp(s.In)
		switch {
		case s.Out != "":
			line += stFaint.Render("  →  ") + stOut.Render("OUT ") + stBold.Render(slotTime(s.Out))
			if out := parseWoffuStamp(s.Out); !in.IsZero() && out.After(in) {
				worked += out.Sub(in)
				line += stFaint.Render("   " + fmtDur(out.Sub(in)))
			}
		case !in.IsZero():
			open := time.Date(now.Year(), now.Month(), now.Day(), in.Hour(), in.Minute(), 0, 0, time.Local)
			if d := now.Sub(open); d > 0 {
				worked += d
			}
			line += stIn.Render("  ● working now")
		}
		fmt.Println(line)
	}
	if worked > 0 {
		fmt.Println()
		uiRow("Worked", stBold.Render(fmtDur(worked)))
	}
	uiHint("woffux sign", "woffux")
}

func viewEvents(events []woffu.AvailableUserEvent) {
	uiTitle("What you have left")
	sort.SliceStable(events, func(i, j int) bool {
		di, dj := strings.HasPrefix(strings.ToLower(events[i].Unit), "day"), strings.HasPrefix(strings.ToLower(events[j].Unit), "day")
		if di != dj {
			return di
		}
		return events[i].Available > events[j].Available
	})
	nameW := 0
	for _, e := range events {
		nameW = max(nameW, lipgloss.Width(e.Name))
	}
	nameW = min(nameW, 44)
	for _, e := range events {
		name := e.Name
		if lipgloss.Width(name) > nameW {
			name = string([]rune(name)[:nameW-1]) + "…"
		}
		amt := uiAmount(e.Available, e.Unit)
		st, bar := stBold, obIn
		if e.Available <= 0 {
			st, bar = stFaint, obGhost
		}
		scale := 22.0
		if !strings.HasPrefix(strings.ToLower(e.Unit), "day") {
			scale = 40
			if e.Available > 0 {
				bar = obBrand
			}
		}
		uiAfterTitle = false
		fmt.Printf("%s%s  %s  %s\n", uiIndent, stText.Render(padTo(name, nameW)), st.Render(fmt.Sprintf("%8s", amt)), uiBar(e.Available/scale, 16, bar))
	}
	uiHint("woffux request", "woffux requests")
}

func viewWhoami(p *woffu.UserProfile) {
	uiTitle(titleCase(p.FullName))
	uiRow("Email", stText.Render(p.Email))
	uiRow("Company", stText.Render(p.CompanyName))
	if p.DepartmentName != "" || p.JobTitle != "" {
		uiRow("Role", stText.Render(strings.Trim(p.JobTitle+" · "+p.DepartmentName, " ·")))
	}
	uiRow("Office", stText.Render(p.OfficeName))
	fmt.Println()
}

// viewHolidays shows this year's company holidays in date order. Woffu
// stores fixed-date holidays once with the year they were created
// (e.g. 2024-01-01 for New Year), so those are shown on this year's date.
func viewHolidays(holidays []woffu.Holiday) {
	now := time.Now()
	type item struct {
		date time.Time
		name string
	}
	seen := map[string]bool{}
	var items []item
	for _, h := range holidays {
		t, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(h.Date)[:min(10, len(strings.TrimSpace(h.Date)))], time.Local)
		if err != nil {
			continue
		}
		if t.Year() < now.Year() {
			t = time.Date(now.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.Local)
		}
		if t.Year() != now.Year() {
			continue
		}
		key := t.Format("01-02")
		if seen[key] {
			continue
		}
		seen[key] = true
		items = append(items, item{t, h.Name})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].date.Before(items[j].date) })

	uiTitle(fmt.Sprintf("Holidays %d", now.Year()), fmt.Sprintf("%d days", len(items)))
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	nextMarked := false
	rose := lipgloss.NewStyle().Foreground(lipgloss.Color("#f472b6"))
	for _, it := range items {
		date := fmt.Sprintf("%-12s", it.date.Format("Mon 2 Jan"))
		switch {
		case it.date.Before(today):
			uiLine(stGhost.Render(date + " " + it.name))
		case !nextMarked:
			nextMarked = true
			days := int(it.date.Sub(today).Hours() / 24)
			when := fmt.Sprintf("in %d days", days)
			if days == 0 {
				when = "today"
			} else if days == 1 {
				when = "tomorrow"
			}
			label := "  ← next, " + when
			if days == 0 {
				label = "  ← today"
			}
			uiLine(stBold.Render(date) + " " + rose.Bold(true).Render(it.name) + stFaint.Render(label))
		default:
			uiLine(stSubtle.Render(date) + " " + rose.Render(it.name))
		}
	}
	fmt.Println()
}

// ── small helpers ──

func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		k := strings.ToLower(strings.TrimSpace(s))
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, strings.TrimSpace(s))
	}
	return out
}

func parseWoffuStamp(s string) time.Time {
	for _, layout := range []string{"2006-01-02T15:04:05.000", "2006-01-02T15:04:05", "2006-01-02T15:04"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func fmtDur(d time.Duration) string {
	h, m := int(d.Hours()), int(d.Minutes())%60
	if h == 0 {
		return fmt.Sprintf("%dm", m)
	}
	return fmt.Sprintf("%dh %02dm", h, m)
}

func padTo(s string, w int) string {
	if d := w - lipgloss.Width(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

func titleCase(s string) string {
	words := strings.Fields(strings.ToLower(s))
	for i, w := range words {
		r := []rune(w)
		r[0] = []rune(strings.ToUpper(string(r[0])))[0]
		words[i] = string(r)
	}
	return strings.Join(words, " ")
}

// ── Requests ──

func viewRequests(requests []woffu.UserRequest) {
	today := time.Now().Format("2006-01-02")
	var pending, upcoming, past, declined []woffu.UserRequest
	for _, r := range requests {
		switch {
		case r.Status == "pending":
			pending = append(pending, r)
		case r.Status == "rejected" || r.Status == "cancelled":
			declined = append(declined, r)
		case r.EndDate >= today:
			upcoming = append(upcoming, r)
		default:
			past = append(past, r)
		}
	}
	byStart := func(rs []woffu.UserRequest, asc bool) {
		sort.SliceStable(rs, func(i, j int) bool {
			if asc {
				return rs[i].StartDate < rs[j].StartDate
			}
			return rs[i].StartDate > rs[j].StartDate
		})
	}
	byStart(pending, true)
	byStart(upcoming, true)
	byStart(past, false)
	byStart(declined, false)

	uiTitle("Your requests", fmt.Sprintf("%d pending", len(pending)), fmt.Sprintf("%d approved ahead", len(upcoming)))
	row := func(r woffu.UserRequest) {
		when := uiDate(r.StartDate)
		if r.EndDate != "" && r.EndDate != r.StartDate {
			when += " → " + uiDate(r.EndDate)
		}
		uiAfterTitle = false
		fmt.Printf("%s%s  %s  %s  %s\n", uiIndent, stText.Render(padTo(when, 24)), padTo(stBold.Render(uiTrim(r.EventName)), 22), padTo(uiRequestStatus(r.Status), 13), stGhost.Render(fmt.Sprintf("#%d", r.RequestID)))
	}
	block := func(title string, rs []woffu.UserRequest, limit int) {
		if len(rs) == 0 {
			return
		}
		uiSection(title)
		for i, r := range rs {
			if limit > 0 && i == limit {
				uiLine(stFaint.Render(fmt.Sprintf("… %d more (woffux requests --json)", len(rs)-limit)))
				return
			}
			row(r)
		}
	}
	if len(requests) == 0 {
		uiLine(stFaint.Render("No requests yet."))
	}
	block("Waiting for approval", pending, 0)
	block("Approved, coming up", upcoming, 0)
	block("Approved, past", past, 5)
	block("Rejected or cancelled", declined, 5)
	uiHint("woffux request", "woffux request cancel <#id>")
}

// ── Calendar ──

func viewCalendar(days []woffu.CalendarDay, year int, month time.Month) {
	byDate := map[string]woffu.CalendarDay{}
	for _, d := range days {
		byDate[d.Date] = d
	}
	var office, remote, off, holiday int
	colorOf := func(d woffu.CalendarDay) lipgloss.Color {
		switch d.Status {
		case "weekend":
			return obGhost
		case "holiday":
			holiday++
			return lipgloss.Color("#f472b6")
		case "absence":
			off++
			return lipgloss.Color("#facc15")
		}
		if d.Mode == "remote" {
			remote++
			return lipgloss.Color("#2dd4bf")
		}
		office++
		return lipgloss.Color("#60a5fa")
	}

	first := time.Date(year, month, 1, 0, 0, 0, 0, time.Local)
	uiTitle(first.Format("January 2006"))
	uiLine(stFaint.Render(" Mon  Tue  Wed  Thu  Fri  ") + stGhost.Render("Sat  Sun"))
	col := (int(first.Weekday()) + 6) % 7
	todayKey := time.Now().Format("2006-01-02")
	line := uiIndent + strings.Repeat("     ", col)
	dim := first.AddDate(0, 1, -1).Day()
	for day := 1; day <= dim; day++ {
		key := first.AddDate(0, 0, day-1).Format("2006-01-02")
		d, ok := byDate[key]
		st := stFaint
		if ok {
			st = lipgloss.NewStyle().Foreground(colorOf(d))
		}
		num := fmt.Sprintf("%3d", day)
		mark := " "
		if ok && len(d.Requests) > 0 {
			for _, r := range d.Requests {
				if r.Status == "pending" {
					mark = "◷"
				}
			}
		}
		if key == todayKey {
			st = st.Bold(true).Underline(true)
		}
		line += st.Render(num) + stOut.Render(mark) + " "
		col++
		if col == 7 {
			fmt.Println(line)
			line, col = uiIndent, 0
		}
	}
	if col > 0 {
		fmt.Println(line)
	}

	fmt.Println()
	var legend []string
	add := func(n int, c lipgloss.Color, label string) {
		if n > 0 {
			legend = append(legend, lipgloss.NewStyle().Foreground(c).Render("■")+" "+stSubtle.Render(fmt.Sprintf("%d %s", n, label)))
		}
	}
	add(office, "#60a5fa", "office")
	add(remote, "#2dd4bf", "remote")
	add(off, "#facc15", "off")
	add(holiday, "#f472b6", "holiday")
	uiLine(strings.Join(legend, "   ") + "   " + stOut.Render("◷") + stFaint.Render(" pending"))

	// Notable days, in words.
	var notes []string
	for _, d := range days {
		var what string
		switch {
		case d.Status == "holiday":
			what = lipgloss.NewStyle().Foreground(lipgloss.Color("#f472b6")).Render(strings.Join(uniqueStrings(d.EventNames), ", "))
		case len(d.Requests) > 0:
			var parts []string
			for _, r := range d.Requests {
				if r.Status == "cancelled" || r.Status == "rejected" {
					continue
				}
				parts = append(parts, uiTrim(r.EventName)+" "+uiRequestStatus(r.Status))
			}
			what = strings.Join(parts, ", ")
		}
		if what != "" {
			notes = append(notes, uiIndent+stSubtle.Render(padTo(uiDate(d.Date), 14))+" "+what)
		}
	}
	if len(notes) > 0 {
		uiSection("Notable days")
		fmt.Println(strings.Join(notes, "\n"))
	}
	uiHint("woffux calendar -m <month>", "woffux request")
}

// ── History ──

func viewHistory(signs []woffu.SignRecord, from, to time.Time) {
	uiTitle("Sign history", from.Format("2 Jan")+" – "+to.Format("2 Jan"))
	if len(signs) == 0 {
		uiLine(stFaint.Render("No signs in this period."))
		fmt.Println()
		return
	}
	var dates []string
	byDate := map[string][]woffu.SignRecord{}
	for _, s := range signs {
		if _, ok := byDate[s.Date]; !ok {
			dates = append(dates, s.Date)
		}
		byDate[s.Date] = append(byDate[s.Date], s)
	}
	sort.Strings(dates)
	var total time.Duration
	for _, date := range dates {
		var parts []string
		var worked time.Duration
		var inAt time.Time
		hasIn := false
		for _, s := range byDate[date] {
			t, _ := time.Parse("15:04", s.Time)
			if s.Type == "in" {
				parts = append(parts, stIn.Render("IN ")+stBold.Render(s.Time))
				inAt, hasIn = t, true
			} else {
				parts = append(parts, stOut.Render("OUT ")+stBold.Render(s.Time))
				if hasIn && t.After(inAt) {
					worked += t.Sub(inAt)
				}
				hasIn = false
			}
		}
		total += worked
		hours := ""
		if worked > 0 {
			hours = stSubtle.Render(fmtDur(worked))
		}
		uiAfterTitle = false
		fmt.Printf("%s%s  %s  %s\n", uiIndent, stText.Render(padTo(uiDate(date), 14)), padTo(strings.Join(parts, stFaint.Render(" · ")), 52), hours)
	}
	fmt.Println()
	uiRow("Total", stBold.Render(fmtDur(total))+stFaint.Render(fmt.Sprintf(" over %d %s", len(dates), map[bool]string{true: "day", false: "days"}[len(dates) == 1])))
	fmt.Println()
}

// calendarYearFor picks the year the calendar command fetched for month
// (the current year; Woffu's month lookup is relative to it).
func calendarYearFor(month time.Month) int { return time.Now().Year() }
