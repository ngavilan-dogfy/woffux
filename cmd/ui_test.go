package cmd

import (
	"github.com/ngavilan-dogfy/woffux/internal/config"
	"strings"
	"testing"
	"time"
)

func TestUITrimDropsEmoji(t *testing.T) {
	if got := uiTrim("Teletrabajo🏡"); got != "Teletrabajo" {
		t.Fatalf("uiTrim = %q", got)
	}
	if got := uiTrim("Médico familiares (hasta 1º)"); got != "Médico familiares (hasta 1º)" {
		t.Fatalf("accents must survive: %q", got)
	}
}

func TestUIAmount(t *testing.T) {
	for in, want := range map[float64]string{6: "6 days", 1: "1 day", 12.5: "12.5 days"} {
		if got := uiAmount(in, "days"); got != want {
			t.Errorf("uiAmount(%v) = %q", in, got)
		}
	}
	if got := uiAmount(26, "hours"); got != "26 h" {
		t.Errorf("hours = %q", got)
	}
}

func TestUIDateRelative(t *testing.T) {
	today := time.Now().Format("2006-01-02")
	if uiDate(today) != "today" || uiDate(time.Now().AddDate(0, 0, 1).Format("2006-01-02")) != "tomorrow" {
		t.Fatal("relative days")
	}
	if !strings.Contains(uiDate("2020-03-02"), "2020") {
		t.Fatal("other years show the year")
	}
}

func TestSetWeekday(t *testing.T) {
	var s = setWeekday(setWeekday(emptySchedule(), time.Monday, dayOn()), time.Friday, dayOn())
	if !s.Monday.Enabled || !s.Friday.Enabled || s.Tuesday.Enabled {
		t.Fatalf("setWeekday = %+v", s)
	}
}

func emptySchedule() (s scheduleT) { return }

func dayOn() dayT { return dayT{Enabled: true} }

type scheduleT = config.Schedule
type dayT = config.DaySchedule
