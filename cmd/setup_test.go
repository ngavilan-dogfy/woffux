package cmd

import (
	"testing"
	"time"

	"github.com/ngavilan-dogfy/woffux/internal/config"
)

func TestValidateClockTime(t *testing.T) {
	for _, value := range []string{"00:00", "08:30", "23:59"} {
		if err := validateClockTime(value); err != nil {
			t.Fatalf("%s should be valid: %v", value, err)
		}
	}
	for _, value := range []string{"24:00", "8:30", "08:99", "nope"} {
		if err := validateClockTime(value); err == nil {
			t.Fatalf("%s should be invalid", value)
		}
	}
}

func TestValidateScheduleEntries(t *testing.T) {
	valid := []config.ScheduleEntry{{Time: "08:30"}, {Time: "13:30"}, {Time: "14:15"}, {Time: "17:30"}}
	if err := validateScheduleEntries(valid); err != nil {
		t.Fatalf("valid entries failed: %v", err)
	}

	invalid := []config.ScheduleEntry{{Time: "08:30"}, {Time: "08:30"}}
	if err := validateScheduleEntries(invalid); err == nil {
		t.Fatal("expected duplicate times to fail")
	}

	invalid = []config.ScheduleEntry{{Time: "17:30"}, {Time: "08:30"}}
	if err := validateScheduleEntries(invalid); err == nil {
		t.Fatal("expected non-chronological times to fail")
	}
}

func TestCoordsConfigured(t *testing.T) {
	if coordsConfigured(0, 0) {
		t.Fatal("zero coordinates should be treated as not configured")
	}
	if !coordsConfigured(41.0, 0) || !coordsConfigured(0, 2.0) {
		t.Fatal("non-zero latitude or longitude should be configured")
	}
}

func TestApplyScheduleWizardResultSavesPresetAndMakesItActive(t *testing.T) {
	cfg := &config.Config{
		Schedule:       config.DefaultSchedule(),
		SavedSchedules: map[string]config.Schedule{"old": config.DefaultSchedule()},
		ActiveSchedule: "old",
		Timezone:       "CET",
	}
	next := config.Schedule{
		Monday: config.DaySchedule{
			Enabled: true,
			Times:   []config.ScheduleEntry{{Time: "09:00"}, {Time: "17:00"}},
		},
	}

	err := applyScheduleWizardResult(cfg, scheduleWizardResult{
		Schedule:    next,
		Timezone:    "Europe/Madrid",
		SavedPreset: " summer ",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.ActiveSchedule != "summer" {
		t.Fatalf("active schedule = %q, want summer", cfg.ActiveSchedule)
	}
	if !config.SchedulesEqual(cfg.SavedSchedules["summer"], next) {
		t.Fatal("saved preset schedule did not match selected schedule")
	}
	if cfg.Timezone != "Europe/Madrid" {
		t.Fatalf("timezone = %q, want Europe/Madrid", cfg.Timezone)
	}
	if _, ok := cfg.SavedSchedules["old"]; !ok {
		t.Fatal("existing presets should be preserved")
	}
}

func TestApplyScheduleWizardResultClearsStaleActivePreset(t *testing.T) {
	cfg := &config.Config{
		Schedule:       config.DefaultSchedule(),
		SavedSchedules: map[string]config.Schedule{"old": config.DefaultSchedule()},
		ActiveSchedule: "old",
	}
	next := config.Schedule{
		Monday: config.DaySchedule{
			Enabled: true,
			Times:   []config.ScheduleEntry{{Time: "10:00"}, {Time: "18:00"}},
		},
	}

	err := applyScheduleWizardResult(cfg, scheduleWizardResult{Schedule: next})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ActiveSchedule != "" {
		t.Fatalf("active schedule should be cleared after manual schedule, got %q", cfg.ActiveSchedule)
	}
}

func TestApplyScheduleWizardResultKeepsSelectedPresetActive(t *testing.T) {
	preset := config.DefaultSchedule()
	cfg := &config.Config{
		SavedSchedules: map[string]config.Schedule{"standard": preset},
	}

	err := applyScheduleWizardResult(cfg, scheduleWizardResult{
		Schedule:       preset,
		ActiveSchedule: "standard",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ActiveSchedule != "standard" {
		t.Fatalf("active schedule = %q, want standard", cfg.ActiveSchedule)
	}
}

func TestApplyScheduleWizardResultSummerReusesExistingPresets(t *testing.T) {
	classic, _ := config.ParseScheduleText("mon-thu 8:30-13:30 14:15-17:30, fri 8-15")
	sommer, _ := config.ParseScheduleText("mon-fri 8-15")
	cfg := &config.Config{
		Schedule:       classic,
		ActiveSchedule: "classic",
		SavedSchedules: map[string]config.Schedule{"classic": classic, "sommer": sommer},
	}
	err := applyScheduleWizardResult(cfg, scheduleWizardResult{
		Schedule: classic, Summer: &sommer, SummerFrom: "07-01", SummerTo: "08-31",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.SavedSchedules) != 2 {
		t.Fatalf("identical presets were duplicated: %v", cfg.SchedulePresetNames())
	}
	if cfg.Seasons.Default != "classic" || cfg.Seasons.Periods[0].Preset != "sommer" {
		t.Fatalf("seasons = %+v", cfg.Seasons)
	}
}

func TestNextAutomaticSignSkipsWeekend(t *testing.T) {
	s, _ := config.ParseScheduleText("mon-fri 9-17")
	cfg := &config.Config{Schedule: s}
	sat := time.Date(2026, 9, 26, 12, 0, 0, 0, time.Local)
	when, dir, ok := nextAutomaticSign(cfg, sat)
	if !ok || dir != "IN" || when.Format("Mon 15:04") != "Mon 09:00" {
		t.Fatalf("next sign = %v %s %v", when, dir, ok)
	}
}

func TestApplyScheduleWizardResultNoSummerClearsSeasons(t *testing.T) {
	s, _ := config.ParseScheduleText("mon-fri 9-17")
	cfg := &config.Config{Seasons: config.Seasons{Default: "a", Periods: []config.Season{{Preset: "b", From: "07-01", To: "08-31"}}}}
	if err := applyScheduleWizardResult(cfg, scheduleWizardResult{Schedule: s, NoSeasons: true}); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Seasons.Periods) != 0 {
		t.Fatal("answering 'no summer hours' must remove the seasonal switch")
	}
}
