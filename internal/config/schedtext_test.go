package config

import (
	"strings"
	"testing"
	"time"
)

func TestParseScheduleTextFormats(t *testing.T) {
	cases := map[string]string{
		"mon-thu 8:30-13:30 14:15-17:30, fri 8-15": "mon-thu 08:30-13:30 14:15-17:30, fri 08:00-15:00",
		"L-J 8:30-13:30 14:15-17:30; V 8.00-15":    "mon-thu 08:30-13:30 14:15-17:30, fri 08:00-15:00",
		"weekdays 9-18":                            "mon-fri 09:00-18:00",
		"laborables 0900-1400 15h-18h":             "mon-fri 09:00-14:00 15:00-18:00",
		"lunes 9-14\nmartes-viernes 9-17":          "mon 09:00-14:00, tue-fri 09:00-17:00",
		"L+X+V 8-15":                               "mon 08:00-15:00, wed 08:00-15:00, fri 08:00-15:00",
		"mon-fri 9-17, fri off":                    "",
	}
	for in, want := range cases {
		s, err := ParseScheduleText(in)
		if want == "" {
			if err == nil {
				t.Errorf("%q should fail (friday twice)", in)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: %v", in, err)
			continue
		}
		if got := ScheduleText(s); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}

func TestParseScheduleTextErrorsAreHelpful(t *testing.T) {
	for in, frag := range map[string]string{
		"":               "at least one day",
		"mon":            "add the hours",
		"funday 9-17":    "unknown day",
		"mon 17-9":       "go forward",
		"mon 9-25":       "not a valid time",
		"mon 9:00 17:00": "not a block",
		"fri-mon 9-17":   "backwards",
	} {
		_, err := ParseScheduleText(in)
		if err == nil || !strings.Contains(err.Error(), frag) {
			t.Errorf("%q: error %v should mention %q", in, err, frag)
		}
	}
}

func TestWeekMinutes(t *testing.T) {
	s, _ := ParseScheduleText("mon-thu 8:30-13:30 14:15-17:30, fri 8-15")
	if got := FormatMinutes(s.WeekMinutes()); got != "40h" {
		t.Fatalf("week = %s, want 40h", got)
	}
}

func TestSeasonsSwitchPresets(t *testing.T) {
	classic, _ := ParseScheduleText("mon-thu 8:30-13:30 14:15-17:30, fri 8-15")
	summer, _ := ParseScheduleText("mon-fri 8-15")
	cfg := &Config{
		Schedule:       classic,
		ActiveSchedule: "classic",
		SavedSchedules: map[string]Schedule{"classic": classic, "summer": summer},
		Seasons:        Seasons{Default: "classic", Periods: []Season{{Preset: "summer", From: "07-01", To: "08-31"}}},
	}
	day := func(s string) time.Time { t, _ := time.Parse("2006-01-02", s); return t }

	if cfg.ApplySeasons(day("2026-06-30")) {
		t.Fatal("June should stay classic")
	}
	if !cfg.ApplySeasons(day("2026-07-01")) || cfg.ActiveSchedule != "summer" || !SchedulesEqual(cfg.Schedule, summer) {
		t.Fatal("July 1st should switch to summer")
	}
	if !cfg.ApplySeasons(day("2026-09-01")) || cfg.ActiveSchedule != "classic" {
		t.Fatal("September should return to classic")
	}
	next, preset, ok := cfg.NextSeasonChange(day("2026-09-24"))
	if !ok || preset != "summer" || next.Format("01-02") != "07-01" {
		t.Fatalf("next change = %v %s", next, preset)
	}
}

func TestSeasonWrapsNewYear(t *testing.T) {
	s := Seasons{Default: "a", Periods: []Season{{Preset: "xmas", From: "12-20", To: "01-06"}}}
	for d, want := range map[string]string{"2026-12-25": "xmas", "2027-01-03": "xmas", "2027-01-07": "a"} {
		tt, _ := time.Parse("2006-01-02", d)
		if got := s.PresetFor(tt); got != want {
			t.Errorf("%s -> %s, want %s", d, got, want)
		}
	}
}

func TestNormalizeSeasonDate(t *testing.T) {
	for in, want := range map[string]string{"07-01": "07-01", "1/7": "07-01", "31/08": "08-31"} {
		if got, err := NormalizeSeasonDate(in); err != nil || got != want {
			t.Errorf("%q -> %q %v", in, got, err)
		}
	}
	if _, err := NormalizeSeasonDate("32/1"); err == nil {
		t.Error("32/1 must fail")
	}
}
