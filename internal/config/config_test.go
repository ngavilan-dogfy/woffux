package config

import "testing"

func TestSaveSchedulePresetNormalizesName(t *testing.T) {
	cfg := &Config{}
	schedule := DefaultSchedule()

	if err := cfg.SaveSchedulePreset("  summer  ", schedule); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := cfg.SavedSchedules["summer"]; !ok {
		t.Fatalf("preset was not saved under normalized name: %#v", cfg.SavedSchedules)
	}
	if err := cfg.SaveSchedulePreset("   ", schedule); err == nil {
		t.Fatal("expected blank preset name to fail")
	}
}

func TestSchedulePresetsAreCopied(t *testing.T) {
	cfg := &Config{}
	schedule := DefaultSchedule()
	if err := cfg.SaveSchedulePreset("standard", schedule); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	schedule.Monday.Times[0].Time = "10:00"
	if cfg.SavedSchedules["standard"].Monday.Times[0].Time != "08:30" {
		t.Fatal("saved preset should not share mutable slices with the source schedule")
	}

	if !cfg.LoadSchedulePreset("standard") {
		t.Fatal("expected preset to load")
	}
	cfg.Schedule.Monday.Times[0].Time = "11:00"
	if cfg.SavedSchedules["standard"].Monday.Times[0].Time != "08:30" {
		t.Fatal("loaded schedule should not share mutable slices with the saved preset")
	}
}

func TestNormalizeClearsStaleActiveSchedule(t *testing.T) {
	preset := DefaultSchedule()
	cfg := &Config{
		Schedule:       makeScheduleForTest("09:00", "17:00"),
		SavedSchedules: map[string]Schedule{"standard": preset},
		ActiveSchedule: "standard",
	}

	cfg.Normalize()

	if cfg.ActiveSchedule != "" {
		t.Fatalf("active schedule should be cleared, got %q", cfg.ActiveSchedule)
	}
}

func TestNormalizeKeepsMatchingActiveSchedule(t *testing.T) {
	preset := DefaultSchedule()
	cfg := &Config{
		Schedule:       preset,
		SavedSchedules: map[string]Schedule{"standard": preset},
		ActiveSchedule: " standard ",
	}

	cfg.Normalize()

	if cfg.ActiveSchedule != "standard" {
		t.Fatalf("active schedule = %q, want standard", cfg.ActiveSchedule)
	}
}

func TestSchedulePresetNamesSorted(t *testing.T) {
	cfg := &Config{
		SavedSchedules: map[string]Schedule{
			"zeta":  {},
			"alpha": {},
		},
	}

	got := cfg.SchedulePresetNames()
	if len(got) != 2 || got[0] != "alpha" || got[1] != "zeta" {
		t.Fatalf("unexpected names: %#v", got)
	}
}

func makeScheduleForTest(times ...string) Schedule {
	entries := make([]ScheduleEntry, len(times))
	for i, value := range times {
		entries[i] = ScheduleEntry{Time: value}
	}
	return Schedule{
		Monday: DaySchedule{Enabled: true, Times: entries},
	}
}
