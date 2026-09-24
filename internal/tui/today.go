package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/ngavilan-dogfy/woffux/internal/woffu"
)

// The Today screen answers, in order:
//   1. Where do I stand?          (hero: state, big number, progress)
//   2. What happens next, and who does it?
//   3. How did the day go?        (timeline: plan vs reality)
//   4. How's my week?             (per-day bars)
//   5. Can I trust the autopilot? (signers + warnings)

func (d *Dashboard) renderToday(h int) string {
	p := d.plan()
	w := d.contentWidth()

	if w >= 96 {
		leftW := w * 57 / 100
		rightW := w - leftW - 4
		left := d.renderHero(p, leftW) + "\n\n" + d.renderTimeline(p, leftW)
		right := d.renderWeek(p, rightW) + "\n\n" + d.renderAutopilot(p, rightW)
		if up := d.renderComingUp(rightW, 3); up != "" && lipgloss.Height(right)+lipgloss.Height(up)+2 < h {
			right += "\n\n" + up
		}
		return twoColumns(left, right, leftW, 4)
	}

	blocks := []string{d.renderHero(p, w), d.renderTimeline(p, w), d.renderWeek(p, w), d.renderAutopilot(p, w)}
	var out []string
	for _, b := range blocks {
		if b != "" {
			out = append(out, b)
		}
	}
	return strings.Join(out, "\n\n")
}

// ── Hero ──

type heroState struct {
	accent lipgloss.Color
	badge  string // "● WORKING"
	big    string // big digits, "" for none
	lines  []string
}

func (d *Dashboard) heroState(p dayPlan) heroState {
	now := p.now
	nowMin := now.Hour()*60 + now.Minute()
	target := ""
	if p.targetDur > 0 {
		target = fmt.Sprintf("of %s · %d%%", formatDuration(p.targetDur), int(p.progress()*100))
	}

	switch p.phase {
	case phaseWorking:
		lines := []string{sText.Render("worked today"), sSubtle.Render(target), sFaint.Render("in since " + p.lastIn)}
		if p.targetDur > 0 && p.workedDur > p.targetDur {
			lines[1] = sOK.Render(fmt.Sprintf("+%s over %s", formatDuration(p.workedDur-p.targetDur), formatDuration(p.targetDur)))
		}
		return heroState{accent: cOK, badge: sOK.Bold(true).Render("● WORKING"), big: clockDuration(p.workedDur), lines: lines}

	case phaseBreak:
		lines := []string{sText.Render("worked so far"), sSubtle.Render(target), sFaint.Render("out since " + p.lastOut)}
		return heroState{accent: cWarn, badge: sWarn.Bold(true).Render("◐ ON A BREAK"), big: clockDuration(p.workedDur), lines: lines}

	case phaseDone:
		lines := []string{sText.Render("worked today"), sSubtle.Render(target), sFaint.Render("out at " + p.lastOut)}
		return heroState{accent: cOK, badge: sOK.Bold(true).Render("✓ DAY COMPLETE"), big: clockDuration(p.workedDur), lines: lines}

	case phaseBefore:
		until := p.next.minute - nowMin
		lines := []string{sText.Render("until you clock in"), sSubtle.Render("scheduled at " + clockOf(p.next.minute))}
		if p.targetDur > 0 {
			lines = append(lines, sFaint.Render(formatDuration(p.targetDur)+" planned today"))
		}
		return heroState{accent: cBrand, badge: sBrand.Render("○ NOT STARTED"), big: clockDuration(time.Duration(until) * time.Minute), lines: lines}

	case phaseLate:
		late := nowMin - p.next.minute
		inWindow := time.Duration(late)*time.Minute <= catchUpWindow
		lines := []string{sText.Render("late for clock-in"), sSubtle.Render("was due at " + clockOf(p.next.minute))}
		if inWindow && d.anySignerActive() {
			lines = append(lines, sFaint.Render("autopilot will retry"))
			return heroState{accent: cWarn, badge: sWarn.Bold(true).Render("◷ CLOCK-IN PENDING"), big: clockDuration(time.Duration(late) * time.Minute), lines: lines}
		}
		lines = append(lines, sBad.Render("press s to sign now"))
		return heroState{accent: cBad, badge: sBad.Bold(true).Render("✗ NOT SIGNED"), big: clockDuration(time.Duration(late) * time.Minute), lines: lines}
	}

	// phaseOff: a day without work.
	switch p.kind {
	case kindHoliday:
		return heroState{accent: cHoliday, badge: fg(cHoliday).Bold(true).Render("✦ HOLIDAY"),
			lines: []string{sBold.Render(orDefault(p.reason, "Public holiday")), sSubtle.Render("Enjoy the day off — nothing to sign.")}}
	case kindWeekend:
		return heroState{accent: cGhost, badge: sSubtle.Bold(true).Render("☾ WEEKEND"),
			lines: []string{sBold.Render("No work today"), sSubtle.Render("Nothing to sign until the next working day.")}}
	case kindTimeOff:
		return heroState{accent: cTimeOff, badge: fg(cTimeOff).Bold(true).Render("☀ TIME OFF"),
			lines: []string{sBold.Render(orDefault(p.reason, "Day off")), sSubtle.Render("Autopilot skips today — nothing to sign.")}}
	case kindNoSchedule:
		return heroState{accent: cWarn, badge: sWarn.Bold(true).Render("◇ NO SCHEDULE"),
			lines: []string{sBold.Render("No sign times for " + now.Format("Monday")), sSubtle.Render("Press e to add them, or s to sign by hand.")}}
	}
	return heroState{accent: cGhost, badge: sSubtle.Render("—")}
}

// dayKindSentence describes a non-working day in plain words.
func dayKindSentence(p dayPlan) string {
	switch p.kind {
	case kindHoliday:
		return "today is a holiday (" + orDefault(p.reason, "public holiday") + ")."
	case kindWeekend:
		return "today is a weekend."
	case kindTimeOff:
		return "you're off today (" + orDefault(p.reason, "time off") + ")."
	case kindNoSchedule:
		return "there's no schedule for today."
	}
	return ""
}

func (d *Dashboard) renderHero(p dayPlan, w int) string {
	hs := d.heroState(p)
	inner := w - 6 // border + padding

	mode := ""
	if p.kind == kindWorking || p.phase != phaseOff {
		if p.mode == woffu.SignModeRemote {
			mode = fg(cRemote).Render("⌂ Remote")
		} else {
			mode = fg(cOffice).Render("▣ Office")
		}
	}
	top := spread(hs.badge, mode, inner)

	var mid string
	if hs.big != "" {
		big := bigText(hs.big, hs.accent)
		side := strings.Join(padLines(hs.lines, 3), "\n")
		mid = lipgloss.JoinHorizontal(lipgloss.Top, big, "   ", side)
	} else {
		mid = strings.Join(hs.lines, "\n")
	}

	parts := []string{top, "", mid}
	if p.targetDur > 0 && p.phase != phaseOff {
		parts = append(parts, "", progressBar(p.progress(), inner, hs.accent))
	}
	if next := d.renderNext(p, inner); next != "" {
		parts = append(parts, "", next)
	}
	return card(strings.Join(parts, "\n"), hs.accent, w)
}

func padLines(lines []string, n int) []string {
	for len(lines) < n {
		lines = append(lines, "")
	}
	return lines
}

// renderNext answers "what happens next, and who will do it".
func (d *Dashboard) renderNext(p dayPlan, w int) string {
	nowMin := p.now.Hour()*60 + p.now.Minute()

	if p.next == nil {
		if p.phase == phaseOff || p.phase == phaseDone {
			if nd, ok := nextWorkingDay(p.now, d.homeDays, d.cfg.Schedule); ok {
				ds, _ := scheduleFor(d.cfg.Schedule, nd.Weekday())
				if first := plannedSigns(ds); len(first) > 0 {
					return sFaint.Render("Next  ") + sText.Render("IN "+clockOf(first[0].minute)) +
						sFaint.Render(" · "+relativeDay(p.now, nd))
				}
			}
		}
		return ""
	}

	dir, dirStyle := "IN", sOK
	if !p.next.in {
		dir, dirStyle = "OUT", sWarn
	}
	head := sFaint.Render("Next  ") + dirStyle.Bold(true).Render(dir) + " " + sBold.Render(clockOf(p.next.minute))
	var when string
	if p.nextDue {
		when = sWarn.Render("due " + formatDuration(time.Duration(nowMin-p.next.minute)*time.Minute) + " ago")
	} else {
		when = sSubtle.Render(humanUntil(p.next.minute - nowMin))
	}

	var who string
	moment := ""
	if !p.nextAt.IsZero() {
		moment = " at " + p.nextAt.Format("15:04")
	}
	switch {
	case d.agentOn():
		who = sOK.Render("●") + sSubtle.Render(" This Mac"+moment)
	case d.githubOn():
		who = sWarn.Render("●") + sSubtle.Render(" GitHub"+moment+" (can run late)")
	case d.agentActive == nil && d.autoActive == nil:
		who = sFaint.Render("checking autopilot…")
	default:
		who = sBad.Bold(true).Render("you sign it — autopilot is off")
	}
	line := head + sFaint.Render(" · ") + when + sFaint.Render(" · ") + who
	if lipgloss.Width(line) > w {
		line = head + sFaint.Render(" · ") + when
	}
	return line
}

// progressBar draws a smooth bar with eighth-block precision.
func progressBar(f float64, w int, color lipgloss.Color) string {
	if w <= 0 {
		return ""
	}
	if f < 0 {
		f = 0
	}
	if f > 1 {
		f = 1
	}
	eighths := int(f * float64(w) * 8)
	full := eighths / 8
	rem := eighths % 8
	partials := []string{"", "▏", "▎", "▍", "▌", "▋", "▊", "▉"}
	bar := strings.Repeat("█", full)
	used := full
	if rem > 0 && full < w {
		bar += partials[rem]
		used++
	}
	return fg(color).Render(bar) + sGhost.Render(strings.Repeat("─", max(0, w-used)))
}

// ── Timeline ──

// renderTimeline draws the working day on a horizontal axis: planned blocks,
// worked blocks and "now", so a missed or late sign jumps out.
func (d *Dashboard) renderTimeline(p dayPlan, w int) string {
	if len(p.planned) == 0 && len(p.worked) == 0 {
		return ""
	}
	nowMin := p.now.Hour()*60 + p.now.Minute()
	lo, hi := 24*60, 0
	for _, s := range append(append([]span{}, p.planned...), p.worked...) {
		lo, hi = min(lo, s.from), max(hi, s.to)
	}
	if p.phase != phaseOff {
		lo, hi = min(lo, nowMin), max(hi, nowMin)
	}
	lo = (lo / 60) * 60
	hi = ((hi + 59) / 60) * 60
	if hi <= lo {
		hi = lo + 60
	}

	barW := w
	perCell := float64(hi-lo) / float64(barW)
	cellMin := func(i int) int { return lo + int((float64(i)+0.5)*perCell) }
	in := func(spans []span, m int) bool {
		for _, s := range spans {
			if m >= s.from && m < s.to {
				return true
			}
		}
		return false
	}

	// Hour ticks
	ticks := []rune(strings.Repeat(" ", barW))
	hours := (hi - lo) / 60
	step := 1
	for hours/step > barW/5 {
		step++
	}
	for hm := lo; hm <= hi; hm += 60 * step {
		pos := int(float64(hm-lo) / perCell)
		lbl := []rune(fmt.Sprintf("%02d", hm/60))
		if pos+len(lbl) > barW {
			pos = barW - len(lbl)
		}
		for i, r := range lbl {
			ticks[pos+i] = r
		}
	}

	// Bar
	var bar strings.Builder
	for i := 0; i < barW; i++ {
		m := cellMin(i)
		switch {
		case in(p.worked, m):
			bar.WriteString(sOK.Render("█"))
		case in(p.planned, m) && m < nowMin && p.phase != phaseOff && !near(p.worked, m, signSlack):
			bar.WriteString(fg(cBad).Render("▒"))
		case in(p.planned, m):
			bar.WriteString(fg(cFaint).Render("░"))
		default:
			bar.WriteString(fg(cLine).Render("─"))
		}
	}

	lines := []string{sGhost.Render(string(ticks)), bar.String()}

	if nowMin >= lo && nowMin <= hi && p.kind == kindWorking {
		pos := min(int(float64(nowMin-lo)/perCell), barW-1)
		tag := "▲ now " + p.now.Format("15:04")
		if pos+len([]rune(tag)) > barW {
			tag = "now " + p.now.Format("15:04") + " ▲"
			pos = max(0, pos-len([]rune(tag))+1)
		}
		lines = append(lines, strings.Repeat(" ", pos)+sBrand.Render(tag))
	}

	legend := sOK.Render("█") + sFaint.Render(" worked  ") + sFaint.Render("░ planned")
	if strings.Contains(bar.String(), fg(cBad).Render("▒")) {
		legend += "  " + fg(cBad).Render("▒") + sFaint.Render(" not signed")
	}
	if signs := d.signsLine(); signs != "" {
		lines = append(lines, "", signs)
	}
	title := spread(label("Timeline"), legend, w)
	return title + "\n" + strings.Join(lines, "\n")
}

// signsLine lists today's registered signs: "IN 08:31 → OUT 13:31 · IN 14:16".
func (d *Dashboard) signsLine() string {
	var parts []string
	for _, s := range d.slots {
		seg := ""
		if s.In != "" {
			seg = sOK.Render("IN ") + sText.Render(extractTime(s.In))
		}
		if s.Out != "" {
			if seg != "" {
				seg += sFaint.Render(" → ")
			}
			seg += sWarn.Render("OUT ") + sText.Render(extractTime(s.Out))
		}
		if seg != "" {
			parts = append(parts, seg)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return sFaint.Render("Signed  ") + strings.Join(parts, sFaint.Render("  ·  "))
}

// signSlack is how far a sign may drift from its planned time before the
// timeline calls the gap "not signed" (agent runs + random delay).
const signSlack = 20

// near reports whether minute m is within slack minutes of any span.
func near(spans []span, m, slack int) bool {
	for _, s := range spans {
		if m >= s.from-slack && m < s.to+slack {
			return true
		}
	}
	return false
}

// ── Week ──

func (d *Dashboard) renderWeek(p dayPlan, w int) string {
	week := buildWeek(p.now, p, d.homeDays, d.monthSigns, d.cfg.Schedule)
	var maxTarget time.Duration
	var sumWorked, sumTarget time.Duration
	for _, wd := range week {
		maxTarget = max(maxTarget, wd.target, wd.worked)
		sumWorked += wd.worked
		if wd.kind == kindWorking {
			sumTarget += wd.target
		}
	}
	if maxTarget == 0 {
		maxTarget = 8 * time.Hour
	}

	nameW := 7
	valW := 8
	barW := max(6, w-nameW-valW-2)

	var rows []string
	for _, wd := range week {
		name := wd.date.Format("Mon 02")
		nameSt := sSubtle
		if wd.isToday {
			nameSt = sBrand
		} else if wd.future {
			nameSt = sFaint
		}
		var mid, val string
		switch {
		case wd.kind == kindHoliday && wd.worked == 0:
			mid = fg(cHoliday).Render(truncate("✦ "+orDefault(wd.reason, "Holiday"), barW))
		case wd.kind == kindTimeOff && wd.worked == 0:
			mid = fg(cTimeOff).Render(truncate("☀ "+orDefault(wd.reason, "Time off"), barW))
		case wd.kind == kindNoSchedule && wd.worked == 0:
			mid = sFaint.Render("no schedule")
		default:
			fill := float64(wd.worked) / float64(maxTarget)
			tgt := float64(wd.target) / float64(maxTarget)
			mid = weekBar(fill, tgt, barW, wd)
			switch {
			case wd.worked > 0:
				val = sText.Render(formatDurationShort(wd.worked))
			case wd.future || wd.isToday:
				val = sFaint.Render(formatDurationShort(wd.target))
			default:
				val = sBad.Render("none")
			}
		}
		rows = append(rows, padRight(nameSt.Render(name), nameW+1)+padRight(mid, barW+1)+lipgloss.PlaceHorizontal(valW, lipgloss.Right, val))
	}

	total := sBold.Render(formatDuration(sumWorked))
	if sumTarget > 0 {
		total += sFaint.Render(" of " + formatDuration(sumTarget))
	}
	_, wk := p.now.ISOWeek()
	title := spread(label(fmt.Sprintf("Week %d", wk)), total, w)
	return title + "\n" + strings.Join(rows, "\n")
}

// weekBar: solid for worked, faint for the rest of the day's target.
func weekBar(fill, target float64, w int, wd weekDay) string {
	f := min(int(fill*float64(w)+0.5), w)
	t := min(int(target*float64(w)+0.5), w)
	color := cOK
	if wd.isToday {
		color = cBrand
	}
	var b strings.Builder
	b.WriteString(fg(color).Render(strings.Repeat("█", f)))
	if t > f {
		b.WriteString(sGhost.Render(strings.Repeat("░", t-f)))
	}
	return b.String()
}

// ── Autopilot ──

func (d *Dashboard) renderAutopilot(p dayPlan, w int) string {
	var rows []string
	row := func(dot, name, detail string) {
		rows = append(rows, dot+" "+padRight(sText.Render(name), 10)+truncate(detail, w-13))
	}

	// This Mac
	if agentAvailable() {
		switch {
		case d.agentActive == nil:
			row(sFaint.Render("○"), "This Mac", sFaint.Render("checking…"))
		case *d.agentActive:
			row(sOK.Render("●"), "This Mac", sSubtle.Render("signs on time while awake"))
		default:
			row(sFaint.Render("○"), "This Mac", sFaint.Render("off · ")+sKey.Render("m")+sFaint.Render(" to enable"))
		}
	}

	// GitHub
	switch {
	case !d.hasFork():
		row(sFaint.Render("○"), "GitHub", sFaint.Render("not set up · woffux setup"))
	case d.autoActive == nil:
		row(sFaint.Render("○"), "GitHub", sFaint.Render("checking…"))
	case !*d.autoActive:
		row(sFaint.Render("○"), "GitHub", sFaint.Render("off · ")+sKey.Render("a")+sFaint.Render(" to enable"))
	case d.needsAutoSync():
		row(sWarn.Render("●"), "GitHub", sWarn.Render("outdated schedule · ⏎ Fix GitHub sync"))
	default:
		detail := "backup signer"
		st := sSubtle
		if !d.lastRunAt.IsZero() {
			ago := p.now.Sub(d.lastRunAt)
			detail += " · ran " + formatDurationShort(ago.Round(time.Minute)) + " ago"
			if !d.lastRunOK {
				detail += " (failed)"
				st = sWarn
			}
		}
		row(sOK.Render("●"), "GitHub", st.Render(detail))
	}

	// Timing
	if d.cfg.Timing.Active() {
		row(sBrand.Render("◇"), "Timing", sText.Render("natural")+sFaint.Render(" · "+d.cfg.Timing.Short()))
	} else {
		row(sFaint.Render("◇"), "Timing", sFaint.Render("exact minute · ")+sKey.Render("⏎")+sFaint.Render(" Natural timing"))
	}

	// Schedule
	sched := orDefault(d.cfg.ActiveSchedule, "custom")
	row(sBrand.Render("◆"), "Schedule", sText.Render(sched)+sFaint.Render(" · ")+sKey.Render("e")+sFaint.Render(" edit"))
	if len(p.schedule) > 0 {
		var parts []string
		for i := 0; i+1 < len(p.schedule); i += 2 {
			parts = append(parts, clockOf(p.schedule[i].minute)+"–"+clockOf(p.schedule[i+1].minute))
		}
		if len(p.schedule)%2 == 1 {
			parts = append(parts, clockOf(p.schedule[len(p.schedule)-1].minute)+"–")
		}
		rows = append(rows, "  "+padRight("", 10)+sFaint.Render(truncate(p.now.Format("Mon")+" "+strings.Join(parts, "  "), w-13)))
	}

	// Verdict: one sentence the user can trust.
	var verdict string
	switch {
	case d.agentActive == nil && d.autoActive == nil && d.hasFork():
		verdict = sFaint.Render("Checking who signs for you…")
	case !d.anySignerActive():
		verdict = sBad.Bold(true).Render("Nobody signs for you") + sSubtle.Render(" — enable This Mac (m)")
	case d.needsAutoSync() && !d.agentOn():
		verdict = sWarn.Render("GitHub has an old schedule — sync it")
	case p.kind != kindWorking:
		verdict = sOK.Render("Autopilot on") + sSubtle.Render(" · resting today")
	case d.agentOn():
		verdict = sOK.Render("Autopilot on") + sSubtle.Render(" · you don't need to do anything")
	default:
		verdict = sOK.Render("Autopilot on") + sSubtle.Render(" · GitHub may sign a few minutes late")
	}

	return label("Autopilot") + "\n" + truncate(verdict, w) + "\n" + strings.Join(rows, "\n")
}

// ── Coming up ──

func (d *Dashboard) renderComingUp(w, n int) string {
	if d.signInfo == nil {
		return ""
	}
	today := d.clock().Format("2006-01-02")
	var rows []string
	for _, e := range d.signInfo.NextEvents {
		if e.Date <= today || len(rows) >= n {
			continue
		}
		t, err := time.Parse("2006-01-02", e.Date)
		if err != nil {
			continue
		}
		names := uniqueNames(e.Names)
		name := "Day off"
		if len(names) > 0 {
			name = strings.Join(names, ", ")
		}
		when := relativeDay(d.clock(), t)
		if when == t.Format("Monday") {
			when = t.Format("Mon 2 Jan")
		}
		rows = append(rows, padRight(sSubtle.Render(when), 12)+fg(cHoliday).Render(truncate(name, w-12)))
	}
	if len(rows) == 0 {
		return ""
	}
	return label("Coming up") + "\n" + strings.Join(rows, "\n")
}

func orDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}
