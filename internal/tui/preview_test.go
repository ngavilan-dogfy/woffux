package tui

// Render-preview harness: writes every screen and state to files so layout
// work can be reviewed without a real Woffu account.
//
//	WOFFUX_TUI_PREVIEW=/tmp/previews go test ./internal/tui -run TestPreview
//
// Each file holds the raw ANSI frame (view it with `cat`).

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/ngavilan-dogfy/woffux/internal/config"
	"github.com/ngavilan-dogfy/woffux/internal/woffu"
)

func previewSchedule() config.Schedule {
	day := config.DaySchedule{Enabled: true, Times: []config.ScheduleEntry{
		{Time: "08:30"}, {Time: "13:30"}, {Time: "14:15"}, {Time: "17:30"},
	}}
	fri := config.DaySchedule{Enabled: true, Times: []config.ScheduleEntry{{Time: "08:00"}, {Time: "15:00"}}}
	return config.Schedule{Monday: day, Tuesday: day, Wednesday: day, Thursday: day, Friday: fri}
}

// previewDashboard builds a dashboard for Wed 23 Sep 2026 at the given time.
func previewDashboard(hhmm string, slots []woffu.SignSlot) *Dashboard {
	now, _ := time.ParseInLocation("2006-01-02 15:04", "2026-09-23 "+hhmm, time.Local)
	on, off := true, false
	inSync := true
	cfg := &config.Config{
		GithubFork:      "owner/woffux",
		WoffuCompanyURL: "https://example.woffu.com",
		ActiveSchedule:  "classic",
		Schedule:        previewSchedule(),
		SavedSchedules:  map[string]config.Schedule{"classic": previewSchedule(), "summer": previewSchedule()},
	}

	var days []woffu.CalendarDay
	for i := 1; i <= 30; i++ {
		date := time.Date(2026, 9, i, 0, 0, 0, 0, time.Local)
		cd := woffu.CalendarDay{Date: date.Format("2006-01-02"), DayName: date.Format("Mon"), Status: "working", Mode: "office"}
		switch {
		case date.Weekday() == time.Saturday || date.Weekday() == time.Sunday:
			cd.Status, cd.Mode, cd.IsWeekend = "weekend", "", true
		case i == 11 || i == 24:
			cd.Status, cd.Mode, cd.IsHoliday = "holiday", "", true
			cd.EventNames = []string{"Mare de Déu de la Mercè", "Mare de Déu de la Mercè"}
			if i == 11 {
				cd.EventNames = []string{"Diada Nacional de Catalunya"}
			}
		case i >= 14 && i <= 18:
			cd.Status, cd.Mode, cd.HasAbsence = "absence", "", true
			cd.Requests = []woffu.DayRequest{{RequestID: 900 + i, EventName: "Vacaciones", Status: "approved"}}
		case i == 2 || i == 9 || i == 22:
			cd.Mode = "remote"
		case i == 29 || i == 30:
			cd.Mode, cd.HasPendingPresence = "remote", true
			cd.Requests = []woffu.DayRequest{{RequestID: 950 + i, EventName: "Teletrabajo", Status: "pending"}}
		}
		if cd.Status == "working" && i < 23 && i != 10 {
			cd.Signs = []woffu.SignSlot{{In: cd.Date + "T08:31:10", Out: cd.Date + "T13:31:40"}, {In: cd.Date + "T14:16:02", Out: cd.Date + "T17:31:55"}}
		}
		days = append(days, cd)
	}
	var signs []woffu.SignRecord
	for _, dd := range []string{"2026-09-21", "2026-09-22"} {
		signs = append(signs,
			woffu.SignRecord{Date: dd, Time: "08:31", Type: "in"}, woffu.SignRecord{Date: dd, Time: "13:31", Type: "out"},
			woffu.SignRecord{Date: dd, Time: "14:16", Type: "in"}, woffu.SignRecord{Date: dd, Time: "17:48", Type: "out"})
	}

	d := NewDashboard(nil, nil, cfg, "")
	d.now = func() time.Time { return now }
	d.loading = false
	d.width, d.height = 120, 38
	d.profile = &woffu.UserProfile{FullName: "NAHUEL GAVILAN BERNAL"}
	d.signInfo = &woffu.SignInfo{Date: "2026-09-23", Mode: woffu.SignModeOffice, IsWorkingDay: true,
		NextEvents: []woffu.SignEvent{
			{Date: "2026-09-24", Names: []string{"Mare de Déu de la Mercè"}},
			{Date: "2026-10-12", Names: []string{"Fiesta Nacional de España"}},
			{Date: "2026-11-01", Names: []string{"Todos los Santos"}},
		}}
	d.slots = slots
	d.homeDays = days
	d.monthSigns = signs
	d.events = []woffu.AvailableUserEvent{
		{Name: "Vacaciones", Available: 6, Unit: "days"},
		{Name: "Horas Sindicales", Available: 363, Unit: "hours"},
		{Name: "Horas sindicales (delegado/a prevención)", Available: 330, Unit: "hours"},
		{Name: "Ausencia por Razones de Fuerza Mayor", Available: 32, Unit: "hours"},
		{Name: "Bolsa de horas", Available: 26, Unit: "hours"},
		{Name: "Asistencia médica / Médico familiares (hasta 1º)", Available: 3, Unit: "days"},
		{Name: "Asuntos Propios", Available: 0, Unit: "days"},
	}
	d.agentActive = &on
	d.autoActive = &off
	d.autoInSync = &inSync
	d.fetchedAt = now
	d.cal = newCalendarGrid(2026, time.September, now)
	d.cal.setDays(days)
	return d
}

func slotsAt(pairs ...string) []woffu.SignSlot {
	var out []woffu.SignSlot
	for i := 0; i < len(pairs); i += 2 {
		s := woffu.SignSlot{In: "2026-09-23T" + pairs[i] + ":00"}
		if i+1 < len(pairs) && pairs[i+1] != "" {
			s.Out = "2026-09-23T" + pairs[i+1] + ":00"
		}
		out = append(out, s)
	}
	return out
}

func TestPreview(t *testing.T) {
	dir := os.Getenv("WOFFUX_TUI_PREVIEW")
	if dir == "" {
		t.Skip("set WOFFUX_TUI_PREVIEW=<dir> to write previews")
	}
	lipgloss.SetColorProfile(termenv.TrueColor)
	_ = os.MkdirAll(dir, 0o755)

	cases := map[string]func() *Dashboard{
		"01-before":   func() *Dashboard { return previewDashboard("07:52", nil) },
		"02-working":  func() *Dashboard { return previewDashboard("11:07", slotsAt("08:31", "")) },
		"03-break":    func() *Dashboard { return previewDashboard("13:48", slotsAt("08:31", "13:31")) },
		"04-late":     func() *Dashboard { return previewDashboard("09:12", nil) },
		"05-done":     func() *Dashboard { return previewDashboard("18:20", slotsAt("08:31", "13:31", "14:16", "17:48")) },
		"06-holiday":  func() *Dashboard { d := previewDashboard("10:00", nil); d.now = fixed("2026-09-24 10:00"); return d },
		"07-calendar": func() *Dashboard { d := previewDashboard("11:07", nil); d.activeTab = tabCalendar; return d },
		"08-cal-sel": func() *Dashboard {
			d := previewDashboard("11:07", nil)
			d.activeTab = tabCalendar
			d.cal.cursor = 28
			d.cal.extend(1)
			d.cal.extend(1)
			return d
		},
		"09-balance": func() *Dashboard { d := previewDashboard("11:07", nil); d.activeTab = tabBalance; return d },
		"10-palette": func() *Dashboard { d := previewDashboard("11:07", slotsAt("08:31", "")); d.openPalette(); return d },
		"11-confirm": func() *Dashboard {
			d := previewDashboard("11:07", slotsAt("08:31", ""))
			d.askSign()
			return d
		},
		"12-help": func() *Dashboard { d := previewDashboard("11:07", nil); d.overlay = overlayHelp; return d },
		"13-narrow": func() *Dashboard {
			d := previewDashboard("11:07", slotsAt("08:31", ""))
			d.width, d.height = 80, 44
			return d
		},
		"14-daymenu": func() *Dashboard {
			d := previewDashboard("11:07", nil)
			d.activeTab = tabCalendar
			d.cal.cursor = 25
			d.openDayMenu()
			return d
		},
		"15-nosigner": func() *Dashboard {
			d := previewDashboard("09:12", nil)
			off := false
			d.agentActive = &off
			return d
		},
		"16-cal-narrow": func() *Dashboard {
			d := previewDashboard("11:07", nil)
			d.activeTab = tabCalendar
			d.width, d.height = 80, 44
			return d
		},
		"17-loading": func() *Dashboard { d := previewDashboard("11:07", nil); d.loading = true; return d },
		"18-request": func() *Dashboard {
			d := previewDashboard("11:07", nil)
			d.activeTab = tabCalendar
			d.cal.cursor = 28
			d.cal.extend(2)
			d.runDayAction("req:telework")
			return d
		},
	}
	for name, build := range cases {
		d := build()
		if err := os.WriteFile(filepath.Join(dir, name+".ans"), []byte(d.View()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	fmt.Println("previews written to", dir)
}

func fixed(s string) func() time.Time {
	t, _ := time.ParseInLocation("2006-01-02 15:04", s, time.Local)
	return func() time.Time { return t }
}
