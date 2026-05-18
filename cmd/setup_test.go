package cmd

import (
	"testing"

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
