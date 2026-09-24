package tui

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ngavilan-dogfy/woffux/internal/config"
	"github.com/ngavilan-dogfy/woffux/internal/timing"
	"github.com/ngavilan-dogfy/woffux/internal/woffu"
)

// This file holds the pure logic behind the Today screen: what kind of day
// it is, where the user stands, what happens next and who will do it. The
// render code only formats these answers, so they are easy to test.

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func stripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }

// catchUpWindow mirrors the sign command's default --catch-up-window: a
// scheduled sign that is late by less than this is still retried.
const catchUpWindow = 2 * time.Hour

// ── Day kind ──

type dayKind int

const (
	kindWorking    dayKind = iota // a normal working day with a schedule
	kindNoSchedule                // working day but no times configured
	kindWeekend
	kindHoliday
	kindTimeOff // vacation, absence, any approved leave
)

// ── Phase: where the user stands today ──

type phase int

const (
	phaseOff     phase = iota // not a working day, nothing signed
	phaseBefore               // not signed yet, first sign still ahead
	phaseLate                 // not signed yet, first sign already due
	phaseWorking              // clocked in
	phaseBreak                // clocked out, more signs scheduled
	phaseDone                 // clocked out, schedule complete
)

// span is one scheduled or worked interval, in minutes from midnight.
type span struct{ from, to int }

// plannedSign is one entry of today's schedule.
type plannedSign struct {
	minute int
	in     bool
}

// dayPlan is everything the Today screen needs, computed once per render.
type dayPlan struct {
	now      time.Time
	kind     dayKind
	phase    phase
	reason   string // holiday / absence name, when relevant
	mode     woffu.SignMode
	schedule []plannedSign
	planned  []span // scheduled work blocks
	worked   []span // actual worked blocks (open block ends at now)

	workedDur time.Duration
	targetDur time.Duration
	lastIn    string // HH:MM of the open IN, when working
	lastOut   string // HH:MM of the last OUT, when on break / done
	signCount int    // number of IN/OUT events registered today

	next    *plannedSign // next scheduled sign, nil when none left
	nextDue bool         // next sign's time already passed

	moments []time.Time // natural moment of each scheduled sign
	nextAt  time.Time   // natural moment of the next sign (zero if none)
}

// minuteOf parses "HH:MM" into minutes from midnight.
func minuteOf(hhmm string) (int, bool) {
	t, err := time.Parse("15:04", strings.TrimSpace(hhmm))
	if err != nil {
		return 0, false
	}
	return t.Hour()*60 + t.Minute(), true
}

func clockOf(minute int) string {
	if minute < 0 {
		minute = 0
	}
	return time.Date(2000, 1, 1, minute/60, minute%60, 0, 0, time.UTC).Format("15:04")
}

// slotMinute extracts minutes from midnight of a Woffu local timestamp.
func slotMinute(stamp string) (int, bool) {
	if stamp == "" {
		return 0, false
	}
	t := parseSlotTime(stamp)
	if t.IsZero() {
		return 0, false
	}
	return t.Hour()*60 + t.Minute(), true
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

func extractTime(datetime string) string {
	if idx := strings.Index(datetime, "T"); idx != -1 {
		t := datetime[idx+1:]
		if len(t) >= 5 {
			return t[:5]
		}
	}
	return datetime
}

// scheduleFor returns the configured day schedule for a weekday.
func scheduleFor(s config.Schedule, day time.Weekday) (config.DaySchedule, bool) {
	switch day {
	case time.Monday:
		return s.Monday, s.Monday.Enabled
	case time.Tuesday:
		return s.Tuesday, s.Tuesday.Enabled
	case time.Wednesday:
		return s.Wednesday, s.Wednesday.Enabled
	case time.Thursday:
		return s.Thursday, s.Thursday.Enabled
	case time.Friday:
		return s.Friday, s.Friday.Enabled
	}
	return config.DaySchedule{}, false
}

// plannedSigns converts a day schedule into ordered sign events.
func plannedSigns(ds config.DaySchedule) []plannedSign {
	var out []plannedSign
	for i, e := range ds.Times {
		m, ok := minuteOf(e.Time)
		if !ok {
			continue
		}
		out = append(out, plannedSign{minute: m, in: i%2 == 0})
	}
	return out
}

func plannedSpans(signs []plannedSign) []span {
	var out []span
	for i := 0; i+1 < len(signs); i += 2 {
		if signs[i+1].minute > signs[i].minute {
			out = append(out, span{signs[i].minute, signs[i+1].minute})
		}
	}
	return out
}

func spansDuration(spans []span) time.Duration {
	var total time.Duration
	for _, s := range spans {
		if s.to > s.from {
			total += time.Duration(s.to-s.from) * time.Minute
		}
	}
	return total
}

// uniqueNames drops duplicates (Woffu often repeats a holiday name).
func uniqueNames(names []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" || seen[strings.ToLower(n)] {
			continue
		}
		seen[strings.ToLower(n)] = true
		out = append(out, n)
	}
	return out
}

// classifyDay decides what kind of day a calendar entry is. info and cal may
// be nil while data is loading.
func classifyDay(date time.Time, info *woffu.SignInfo, cal *woffu.CalendarDay, sched config.Schedule) (dayKind, string) {
	if cal != nil {
		switch cal.Status {
		case "holiday":
			return kindHoliday, strings.Join(uniqueNames(cal.EventNames), ", ")
		case "weekend":
			return kindWeekend, ""
		case "absence":
			return kindTimeOff, strings.Join(uniqueNames(cal.EventNames), ", ")
		}
		// Approved leave shows up as a request even when the calendar still
		// says "working" (e.g. vacation just approved).
		for _, r := range cal.Requests {
			if r.Status == "approved" && !isTeleworkName(r.EventName) {
				return kindTimeOff, r.EventName
			}
		}
	} else {
		if date.Weekday() == time.Saturday || date.Weekday() == time.Sunday {
			return kindWeekend, ""
		}
		if info != nil && !info.IsWorkingDay {
			return kindTimeOff, ""
		}
	}
	if info != nil && !info.IsWorkingDay && cal != nil && cal.Status == "working" {
		// Woffu says "not a working day" for a reason the calendar doesn't
		// show (e.g. a company event). Trust the signer's view.
		return kindTimeOff, strings.Join(uniqueNames(cal.EventNames), ", ")
	}
	if _, ok := scheduleFor(sched, date.Weekday()); !ok {
		if date.Weekday() == time.Saturday || date.Weekday() == time.Sunday {
			return kindWeekend, ""
		}
		return kindNoSchedule, ""
	}
	return kindWorking, ""
}

func isTeleworkName(name string) bool {
	return strings.Contains(strings.ToLower(name), "teletrabajo") ||
		strings.Contains(strings.ToLower(name), "telework") ||
		strings.Contains(strings.ToLower(name), "remote")
}

// buildDayPlan answers every Today-screen question from raw data.
func buildDayPlan(now time.Time, info *woffu.SignInfo, cal *woffu.CalendarDay, sched config.Schedule, slots []woffu.SignSlot) dayPlan {
	p := dayPlan{now: now, mode: woffu.SignModeOffice}
	if info != nil {
		p.mode = info.Mode
	}
	p.kind, p.reason = classifyDay(now, info, cal, sched)

	if p.kind == kindWorking {
		ds, _ := scheduleFor(sched, now.Weekday())
		p.schedule = plannedSigns(ds)
		p.planned = plannedSpans(p.schedule)
		p.targetDur = spansDuration(p.planned)
	}

	nowMin := now.Hour()*60 + now.Minute()
	open := false
	for _, s := range slots {
		in, okIn := slotMinute(s.In)
		if okIn {
			p.signCount++
		}
		out, okOut := slotMinute(s.Out)
		if okOut {
			p.signCount++
		}
		switch {
		case okIn && okOut:
			if out > in {
				p.worked = append(p.worked, span{in, out})
			}
			p.lastOut = clockOf(out)
			open = false
		case okIn:
			// Guard against clock skew: an IN "in the future" counts as now.
			end := nowMin
			if end < in {
				end = in
			}
			p.worked = append(p.worked, span{in, end})
			p.lastIn = clockOf(in)
			open = true
		}
	}
	p.workedDur = spansDuration(p.worked)

	if p.signCount < len(p.schedule) {
		n := p.schedule[p.signCount]
		p.next = &n
		p.nextDue = n.minute <= nowMin
	}

	switch {
	case open:
		p.phase = phaseWorking
	case p.signCount > 0 && p.next != nil:
		p.phase = phaseBreak
	case p.signCount > 0:
		p.phase = phaseDone
	case p.kind != kindWorking:
		p.phase = phaseOff
	case p.next != nil && p.nextDue:
		p.phase = phaseLate
	case p.next != nil:
		p.phase = phaseBefore
	default:
		p.phase = phaseOff
	}
	return p
}

// progress is worked / target clamped to [0, 1].
func (p dayPlan) progress() float64 {
	if p.targetDur <= 0 {
		return 0
	}
	f := float64(p.workedDur) / float64(p.targetDur)
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}

// attachMoments fills in the natural moment of every scheduled sign (see
// internal/timing), so the screen shows when a sign will really happen.
func (p *dayPlan) attachMoments(tm timing.Settings) {
	events := make([]timing.Event, len(p.schedule))
	for i, e := range p.schedule {
		events[i] = timing.Event{Minute: e.minute, In: e.in}
	}
	p.moments = tm.Targets(p.now, events)
	if p.next != nil && p.signCount < len(p.moments) {
		p.nextAt = p.moments[p.signCount]
	}
}

// ── Week ──

type weekDay struct {
	date    time.Time
	kind    dayKind
	reason  string
	worked  time.Duration
	target  time.Duration
	isToday bool
	future  bool
}

// buildWeek computes Monday–Friday of the current ISO week.
func buildWeek(now time.Time, today dayPlan, days []woffu.CalendarDay, signs []woffu.SignRecord, sched config.Schedule) []weekDay {
	byDate := map[string]*woffu.CalendarDay{}
	for i := range days {
		byDate[days[i].Date] = &days[i]
	}
	wd := int(now.Weekday())
	if wd == 0 {
		wd = 7
	}
	monday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -(wd - 1))

	var out []weekDay
	for i := 0; i < 5; i++ {
		date := monday.AddDate(0, 0, i)
		key := date.Format("2006-01-02")
		w := weekDay{date: date}
		w.isToday = key == now.Format("2006-01-02")
		w.future = date.After(now) && !w.isToday
		w.kind, w.reason = classifyDay(date, nil, byDate[key], sched)
		if w.kind == kindWorking {
			ds, _ := scheduleFor(sched, date.Weekday())
			w.target = spansDuration(plannedSpans(plannedSigns(ds)))
		}
		if w.isToday {
			w.kind, w.reason = today.kind, today.reason
			w.worked = today.workedDur
			w.target = today.targetDur
		} else {
			w.worked = workedFromRecords(key, signs)
		}
		out = append(out, w)
	}
	return out
}

func workedFromRecords(date string, signs []woffu.SignRecord) time.Duration {
	var total time.Duration
	var inAt time.Time
	hasIn := false
	for _, rec := range signs {
		if rec.Date != date {
			continue
		}
		t, err := time.Parse("15:04", rec.Time)
		if err != nil {
			continue
		}
		if rec.Type == "in" {
			inAt, hasIn = t, true
		} else if hasIn {
			if diff := t.Sub(inAt); diff > 0 {
				total += diff
			}
			hasIn = false
		}
	}
	return total
}

// nextWorkingDay finds the first working day after today in the loaded
// calendar (falls back to the schedule when the calendar ends).
func nextWorkingDay(now time.Time, days []woffu.CalendarDay, sched config.Schedule) (time.Time, bool) {
	byDate := map[string]*woffu.CalendarDay{}
	for i := range days {
		byDate[days[i].Date] = &days[i]
	}
	base := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	for i := 1; i <= 21; i++ {
		d := base.AddDate(0, 0, i)
		if k, _ := classifyDay(d, nil, byDate[d.Format("2006-01-02")], sched); k == kindWorking {
			return d, true
		}
	}
	return time.Time{}, false
}

// ── Formatting ──

func formatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h == 0 {
		return strconv.Itoa(m) + "m"
	}
	return strconv.Itoa(h) + "h " + pad2(m) + "m"
}

// formatDurationShort renders a duration as "45m" or "2h10m".
func formatDurationShort(dur time.Duration) string {
	if dur < 0 {
		dur = 0
	}
	h := int(dur.Hours())
	m := int(dur.Minutes()) % 60
	if h == 0 {
		return strconv.Itoa(m) + "m"
	}
	return strconv.Itoa(h) + "h" + pad2(m) + "m"
}

// clockDuration renders "3:07" for the big hero digits.
func clockDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	return strconv.Itoa(int(d.Hours())) + ":" + pad2(int(d.Minutes())%60)
}

// humanUntil renders a friendly relative time: "in 35m", "in 2h 05m".
func humanUntil(minutes int) string {
	if minutes <= 0 {
		return "now"
	}
	return "in " + formatDuration(time.Duration(minutes)*time.Minute)
}

// relativeDay says "today", "tomorrow", "Monday" or "Mon 5 Oct".
func relativeDay(now, d time.Time) string {
	a := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	b := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
	diff := int(b.Sub(a).Hours() / 24)
	switch {
	case diff == 0:
		return "today"
	case diff == 1:
		return "tomorrow"
	case diff > 1 && diff < 7:
		return d.Format("Monday")
	}
	return d.Format("Mon 2 Jan")
}

func pad2(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}
