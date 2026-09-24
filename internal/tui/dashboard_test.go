package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ngavilan-dogfy/woffux/internal/config"
	"github.com/ngavilan-dogfy/woffux/internal/woffu"
)

func key(k string) tea.KeyMsg {
	switch k {
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "shift+right":
		return tea.KeyMsg{Type: tea.KeyShiftRight}
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
}

func at(hhmm string) time.Time {
	t, _ := time.ParseInLocation("2006-01-02 15:04", "2026-09-23 "+hhmm, time.Local)
	return t
}

func workingInfo() *woffu.SignInfo {
	return &woffu.SignInfo{Date: "2026-09-23", Mode: woffu.SignModeOffice, IsWorkingDay: true}
}

// ── Day plan ──

func TestDayPlanPhases(t *testing.T) {
	sched := previewSchedule()
	cases := []struct {
		name  string
		now   string
		slots []woffu.SignSlot
		want  phase
	}{
		{"before first sign", "07:50", nil, phaseBefore},
		{"late for first sign", "09:10", nil, phaseLate},
		{"clocked in", "11:00", slotsAt("08:31", ""), phaseWorking},
		{"lunch break", "13:50", slotsAt("08:31", "13:31"), phaseBreak},
		{"all done", "18:00", slotsAt("08:31", "13:31", "14:16", "17:31"), phaseDone},
	}
	for _, c := range cases {
		p := buildDayPlan(at(c.now), workingInfo(), nil, sched, c.slots)
		if p.phase != c.want {
			t.Errorf("%s: phase = %v, want %v", c.name, p.phase, c.want)
		}
	}
}

func TestDayPlanNextSign(t *testing.T) {
	p := buildDayPlan(at("13:50"), workingInfo(), nil, previewSchedule(), slotsAt("08:31", "13:31"))
	if p.next == nil || !p.next.in || clockOf(p.next.minute) != "14:15" || p.nextDue {
		t.Fatalf("next = %+v due=%v, want IN 14:15 not due", p.next, p.nextDue)
	}
	if p.workedDur != 5*time.Hour {
		t.Fatalf("worked = %v, want 5h", p.workedDur)
	}
	if p.targetDur != 8*time.Hour+15*time.Minute {
		t.Fatalf("target = %v, want 8h15m", p.targetDur)
	}
}

// A holiday must never announce a sign, even though the weekday schedule
// has times (the old dashboard showed "Next IN 08:30" on holidays).
func TestDayPlanHolidayHasNoNextSign(t *testing.T) {
	cal := &woffu.CalendarDay{Date: "2026-09-23", Status: "holiday", EventNames: []string{"La Mercè", "La Mercè"}}
	info := workingInfo()
	info.IsWorkingDay = false
	p := buildDayPlan(at("08:00"), info, cal, previewSchedule(), nil)
	if p.kind != kindHoliday || p.phase != phaseOff || p.next != nil {
		t.Fatalf("holiday plan = kind %v phase %v next %+v", p.kind, p.phase, p.next)
	}
	if p.reason != "La Mercè" {
		t.Fatalf("reason = %q, want deduplicated name", p.reason)
	}
}

func TestDayPlanApprovedVacationIsTimeOff(t *testing.T) {
	cal := &woffu.CalendarDay{Date: "2026-09-23", Status: "working", Mode: "office",
		Requests: []woffu.DayRequest{{RequestID: 1, EventName: "Vacaciones", Status: "approved"}}}
	p := buildDayPlan(at("09:00"), workingInfo(), cal, previewSchedule(), nil)
	if p.kind != kindTimeOff || p.phase != phaseOff {
		t.Fatalf("kind %v phase %v, want time off", p.kind, p.phase)
	}
}

func TestDayPlanApprovedTeleworkStillWorks(t *testing.T) {
	cal := &woffu.CalendarDay{Date: "2026-09-23", Status: "working", Mode: "remote",
		Requests: []woffu.DayRequest{{RequestID: 1, EventName: "Teletrabajo", Status: "approved"}}}
	p := buildDayPlan(at("09:00"), workingInfo(), cal, previewSchedule(), nil)
	if p.kind != kindWorking {
		t.Fatalf("telework day classified as %v", p.kind)
	}
}

// Regression: an open IN "in the future" (clock skew, test data) crashed the
// old progress bar with a negative strings.Repeat count.
func TestDayPlanNeverNegative(t *testing.T) {
	p := buildDayPlan(at("07:00"), workingInfo(), nil, previewSchedule(), slotsAt("08:31", ""))
	if p.workedDur < 0 || p.progress() < 0 {
		t.Fatalf("negative worked time: %v", p.workedDur)
	}
	_ = progressBar(-1, 10, cOK)
	_ = progressBar(3, 10, cOK)
	_ = progressBar(0.5, 0, cOK)
}

func TestExpectedAgentRun(t *testing.T) {
	cases := map[string]string{"08:30": "08:31", "08:31": "08:31", "13:30": "13:31", "08:47": "09:01", "08:00": "08:01"}
	for in, want := range cases {
		m, _ := minuteOf(in)
		if got := clockOf(expectedAgentRun(m)); got != want {
			t.Errorf("expectedAgentRun(%s) = %s, want %s", in, got, want)
		}
	}
}

func TestBuildWeekSkipsHolidayTarget(t *testing.T) {
	d := previewDashboard("11:07", slotsAt("08:31", ""))
	p := d.plan()
	week := buildWeek(p.now, p, d.homeDays, d.monthSigns, d.cfg.Schedule)
	if len(week) != 5 {
		t.Fatalf("week has %d days", len(week))
	}
	if week[3].kind != kindHoliday || week[3].target != 0 {
		t.Fatalf("Thursday = %+v, want holiday with no target", week[3])
	}
	if week[0].worked != 8*time.Hour+32*time.Minute {
		t.Fatalf("Monday worked = %v", week[0].worked)
	}
	if !week[2].isToday || week[2].worked != p.workedDur {
		t.Fatalf("today not wired into the week: %+v", week[2])
	}
}

func TestNextWorkingDaySkipsHoliday(t *testing.T) {
	d := previewDashboard("18:00", nil)
	nd, ok := nextWorkingDay(at("18:00"), d.homeDays, d.cfg.Schedule)
	if !ok || nd.Format("2006-01-02") != "2026-09-25" {
		t.Fatalf("next working day = %v, want Fri 25 (Thu 24 is a holiday)", nd)
	}
}

// ── Signing always asks first ──

func TestSignKeyOpensConfirmAndEscCancels(t *testing.T) {
	d := previewDashboard("11:07", slotsAt("08:31", ""))
	d.Update(key("s"))
	if d.overlay != overlayConfirm || d.confirm == nil || !strings.Contains(d.confirm.subject, "OUT") {
		t.Fatalf("s should open an OUT confirmation, got overlay %v %+v", d.overlay, d.confirm)
	}
	d.Update(key("esc"))
	if d.overlay != overlayNone || d.signing {
		t.Fatal("esc must close without signing")
	}
}

func TestSignOnHolidayNeedsExplicitYes(t *testing.T) {
	d := previewDashboard("10:00", nil)
	d.now = fixed("2026-09-24 10:00")
	d.askSign()
	if d.confirm == nil || !d.confirm.danger {
		t.Fatal("signing on a holiday must be a dangerous confirmation")
	}
	fired := false
	d.confirm.onYes = func() tea.Cmd { fired = true; return nil }
	d.confirmAt = time.Now().Add(-time.Second)
	d.Update(key("enter"))
	if fired || d.overlay != overlayConfirm {
		t.Fatal("enter must not confirm a dangerous action")
	}
	d.Update(key("y"))
	if !fired {
		t.Fatal("y must confirm")
	}
}

// ── Palette ──

func TestPaletteFiltersAndRuns(t *testing.T) {
	d := previewDashboard("11:07", nil)
	d.Update(key("enter"))
	if d.overlay != overlayPalette {
		t.Fatal("enter on Today opens the palette")
	}
	for _, r := range "balance" {
		d.Update(key(string(r)))
	}
	items := d.filteredActions()
	if len(items) == 0 || items[d.cursor].key != "tab:balance" {
		t.Fatalf("filter 'balance' -> %+v", items)
	}
	d.Update(key("enter"))
	if d.overlay != overlayNone || d.activeTab != tabBalance {
		t.Fatal("running 'Balance' should switch tab and close")
	}
}

func TestPaletteEscClearsQueryThenCloses(t *testing.T) {
	d := previewDashboard("11:07", nil)
	d.openPalette()
	d.Update(key("x"))
	d.Update(key("esc"))
	if d.overlay != overlayPalette || d.query != "" {
		t.Fatal("first esc clears the query")
	}
	d.Update(key("esc"))
	if d.overlay != overlayNone {
		t.Fatal("second esc closes")
	}
}

func TestPalettePresetsSortedAndCurrentNotRunnable(t *testing.T) {
	d := previewDashboard("11:07", nil)
	d.cfg.SavedSchedules = map[string]config.Schedule{"zeta": {}, "alpha": {}, "classic": {}}
	var keys []string
	for _, a := range d.getActions() {
		if strings.HasPrefix(a.key, "preset:") {
			keys = append(keys, a.key)
			if a.key == "preset:classic" && a.enabled() {
				t.Fatal("the schedule in use must not be runnable")
			}
		}
	}
	if strings.Join(keys, ",") != "preset:alpha,preset:classic,preset:zeta" {
		t.Fatalf("preset order = %v", keys)
	}
}

func TestPaletteDisablesGitHubWithoutFork(t *testing.T) {
	d := previewDashboard("11:07", nil)
	d.cfg.GithubFork = ""
	for _, a := range d.getActions() {
		if (a.key == "toggle-github" || a.key == "open-github") && a.enabled() {
			t.Fatalf("%s should be disabled without a fork", a.key)
		}
		if a.key == "sync" {
			t.Fatal("sync should be hidden without a fork")
		}
	}
}

func TestMoveCursorSkipsDisabled(t *testing.T) {
	items := []action{{key: "a"}, {key: "b", disabled: true}, {key: "c", current: true}, {key: "d"}}
	if got := moveCursor(items, 0, 1); got != 3 {
		t.Fatalf("down from a = %d, want d", got)
	}
	if got := moveCursor(items, 3, 1); got != 0 {
		t.Fatalf("down from d wraps to a, got %d", got)
	}
}

// ── Calendar ──

func TestCalendarMoveCrossesMonths(t *testing.T) {
	c := newCalendarGrid(2026, time.September, at("11:00"))
	c.cursor = 30
	if !c.move(1) || c.month != time.October || c.cursor != 1 {
		t.Fatalf("right from Sep 30 -> %v %d", c.month, c.cursor)
	}
	if !c.move(-7) || c.month != time.September || c.cursor != 24 {
		t.Fatalf("up from Oct 1 -> %v %d", c.month, c.cursor)
	}
	if c.move(1) {
		t.Fatal("moving inside the month must not request a fetch")
	}
}

func TestCalendarExtendSkipsWeekends(t *testing.T) {
	c := newCalendarGrid(2026, time.September, at("11:00"))
	c.cursor = 25 // Friday
	c.extend(7)
	got := strings.Join(c.selectedDates(), ",")
	if strings.Contains(got, "-26") || strings.Contains(got, "-27") || !strings.Contains(got, "2026-09-25") || !strings.Contains(got, "2026-09-28") {
		t.Fatalf("range selection = %s", got)
	}
}

func TestEligibleDatesFiltersNonWorkingAndRequested(t *testing.T) {
	d := previewDashboard("11:07", nil)
	got := d.cal.eligibleDates([]string{"2026-09-24", "2026-09-26", "2026-09-28", "2026-09-29", "2026-10-05", "2026-10-10"})
	// Oct 5 isn't loaded, so it can't be verified and is left out.
	if strings.Join(got, ",") != "2026-09-28" {
		t.Fatalf("eligible = %v", got)
	}
}

func TestCalendarTeleworkKeyConfirmsBeforeSending(t *testing.T) {
	d := previewDashboard("11:07", nil)
	d.activeTab = tabCalendar
	d.cal.cursor = 28
	d.Update(key("t"))
	if d.overlay != overlayConfirm || d.busy != "" {
		t.Fatalf("t should open a confirmation and send nothing, overlay=%v busy=%q", d.overlay, d.busy)
	}
	if !strings.Contains(d.confirm.subject, "Telework · 1 day") {
		t.Fatalf("subject = %q", d.confirm.subject)
	}
	d.Update(key("esc"))
	if d.overlay != overlayNone {
		t.Fatal("esc closes the confirmation")
	}
}

func TestCancelOnlyOfferedWithRequests(t *testing.T) {
	d := previewDashboard("11:07", nil)
	d.cal.cursor = 28
	for _, a := range d.dayActions() {
		if a.key == "cancel-all" {
			t.Fatal("no requests on the 28th, cancel must not be offered")
		}
	}
	d.cal.cursor = 29
	found := false
	for _, a := range d.dayActions() {
		if a.key == "cancel-all" {
			found = true
		}
	}
	if !found {
		t.Fatal("pending telework on the 29th should be cancellable")
	}
}

func TestDescribeDates(t *testing.T) {
	cases := map[string][]string{
		"Mon 28 Sep 2026":       {"2026-09-28"},
		"Mon 28 – Wed 30 Sep":   {"2026-09-30", "2026-09-28", "2026-09-29"},
		"Mon 28 Sep, Fri 2 Oct": {"2026-09-28", "2026-10-02"},
	}
	for want, in := range cases {
		if got := describeDates(in); got != want {
			t.Errorf("describeDates(%v) = %q, want %q", in, got, want)
		}
	}
}

// ── State messages ──

func TestAutoStatusIgnoresStaleRepo(t *testing.T) {
	d := previewDashboard("11:07", nil)
	d.autoActive = nil
	d.Update(autoStatusMsg{repo: "someone/else", enabled: true})
	if d.autoActive != nil {
		t.Fatal("status for another repo must be ignored")
	}
	d.Update(autoStatusMsg{repo: "owner/woffux", enabled: true, syncChecked: true, inSync: false})
	if !d.needsAutoSync() {
		t.Fatal("out-of-sync workflow should be flagged")
	}
}

func TestApplyConfigClearsStaleAutoStatus(t *testing.T) {
	d := previewDashboard("11:07", nil)
	on := true
	d.autoActive = &on
	d.applyConfig(&config.Config{GithubFork: "other/fork"})
	if d.autoActive != nil {
		t.Fatal("changing fork must reset auto-sign status")
	}
}

func TestToastClearsOnlyItsOwnMessage(t *testing.T) {
	d := previewDashboard("11:07", nil)
	d.showToast("first", toastOK)
	first := d.toast.id
	d.showToast("second", toastOK)
	d.Update(clearToastMsg{id: first})
	if d.toast.text != "second" {
		t.Fatal("an old timer must not clear a newer toast")
	}
}

func TestRequestDoneReportsFailures(t *testing.T) {
	d := previewDashboard("11:07", nil)
	d.cal.selected["2026-09-28"] = true
	d.Update(requestDoneMsg{count: 2, failed: 1, action: "sent"})
	if len(d.cal.selected) != 0 || d.toast.kind != toastErr || !strings.Contains(d.toast.text, "1 failed") {
		t.Fatalf("toast = %+v, selection = %v", d.toast, d.cal.selected)
	}
}

// ── Rendering never panics and always fits ──

func TestViewFitsEverySizeAndScreen(t *testing.T) {
	overlays := []overlayKind{overlayNone, overlayPalette, overlayDay, overlayHelp, overlayInput}
	for _, w := range []int{30, 44, 60, 80, 100, 120, 200} {
		for _, h := range []int{10, 14, 24, 40, 60} {
			for tab := 0; tab < tabCount; tab++ {
				for _, ov := range overlays {
					d := previewDashboard("11:07", slotsAt("08:31", ""))
					d.width, d.height = w, h
					d.activeTab = tab
					d.overlay = ov
					out := d.View()
					lines := strings.Split(out, "\n")
					if len(lines) > h {
						t.Fatalf("%dx%d tab %d overlay %d: %d lines", w, h, tab, ov, len(lines))
					}
					for i, l := range lines {
						if lw := len([]rune(stripANSI(l))); lw > w {
							t.Fatalf("%dx%d tab %d overlay %d line %d is %d wide: %q", w, h, tab, ov, i, lw, stripANSI(l))
						}
					}
				}
			}
		}
	}
}

// At comfortable sizes nothing should rely on the clipping safety net.
func TestNoClippingAtComfortableSizes(t *testing.T) {
	overlays := []overlayKind{overlayNone, overlayPalette, overlayDay, overlayHelp, overlayInput}
	for _, w := range []int{60, 80, 100, 120, 160} {
		for tab := 0; tab < tabCount; tab++ {
			for _, ov := range overlays {
				for _, now := range []string{"07:52", "09:12", "11:07", "13:48", "18:20"} {
					d := previewDashboard(now, slotsAt("08:31", ""))
					d.width, d.height = w, 44
					d.activeTab, d.overlay = tab, ov
					before := clipCount
					out := d.View()
					if clipCount != before {
						t.Errorf("%d wide, tab %d, overlay %d at %s needed clipping:\n%s", w, tab, ov, now, stripANSI(out))
						return
					}
				}
			}
		}
	}
}

// ── Safety regressions (found in review) ──

// A date we have no data for must never be requested.
func TestUnverifiedDatesAreNeverEligible(t *testing.T) {
	d := previewDashboard("11:07", nil)
	d.cal.shiftMonth(1) // October: not loaded
	if d.cal.loaded {
		t.Fatal("October should not be loaded yet")
	}
	if got := d.cal.eligibleDates([]string{"2026-10-05"}); len(got) != 0 {
		t.Fatalf("unverified date treated as eligible: %v", got)
	}
	d.activeTab = tabCalendar
	d.Update(key("t"))
	if d.overlay == overlayConfirm {
		t.Fatal("requests must be blocked while the month loads")
	}
	d.Update(key(" "))
	if len(d.cal.selected) != 0 {
		t.Fatal("selection must be blocked while the month loads")
	}
}

// Selections in a month we navigated away from keep using that month's data.
func TestCrossMonthSelectionUsesCachedMonth(t *testing.T) {
	d := previewDashboard("11:07", nil)
	d.cal.selected["2026-09-29"] = true // pending telework already
	d.cal.selected["2026-09-28"] = true
	d.cal.shiftMonth(1)
	d.cal.setDays([]woffu.CalendarDay{{Date: "2026-10-05", Status: "working", Mode: "office"}})
	d.cal.selected["2026-10-05"] = true
	got := strings.Join(d.cal.eligibleDates(d.cal.selectedDates()), ",")
	if got != "2026-09-28,2026-10-05" {
		t.Fatalf("eligible across months = %s", got)
	}
	if ids, _ := d.activeRequests(d.cal.selectedDates()); len(ids) != 1 {
		t.Fatalf("pending request in September not found from October: %v", ids)
	}
}

// A background fetch failing must not release the sign-in-flight guard.
func TestFetchErrorKeepsSignGuard(t *testing.T) {
	d := previewDashboard("11:07", slotsAt("08:31", ""))
	d.signing = true
	d.busy = "Signing in Woffu"
	d.Update(errMsg{err: errTest("network down")})
	if !d.signing || d.busy == "" {
		t.Fatal("fetch error cleared the sign guard")
	}
	d.Update(key("s"))
	if d.overlay == overlayConfirm {
		t.Fatal("a second sign must not be offered while one is in flight")
	}
	d.Update(actionErrMsg{err: errTest("sign failed")})
	if d.signing || d.busy != "" {
		t.Fatal("an action error must release the guard")
	}
}

// Holding Enter must not walk palette → sign → confirm.
func TestConfirmIgnoresKeyRepeat(t *testing.T) {
	d := previewDashboard("11:07", slotsAt("08:31", ""))
	d.askSign()
	fired := false
	d.confirm.onYes = func() tea.Cmd { fired = true; return nil }
	d.Update(key("enter"))
	if fired {
		t.Fatal("enter right after opening must be ignored")
	}
	d.confirmAt = time.Now().Add(-time.Second)
	d.Update(key("enter"))
	if !fired {
		t.Fatal("a deliberate enter must confirm")
	}
}

func TestRequestsBlockedWhileBusy(t *testing.T) {
	d := previewDashboard("11:07", nil)
	d.activeTab = tabCalendar
	d.cal.cursor = 28
	d.busy = "Requesting telework for 5 days"
	d.Update(key("t"))
	if d.overlay == overlayConfirm {
		t.Fatal("a new request must wait for the one in flight")
	}
}

type errTest string

func (e errTest) Error() string { return string(e) }
