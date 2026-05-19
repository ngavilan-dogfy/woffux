package github

import (
	"strings"
	"testing"

	"github.com/ngavilan-dogfy/woffux/internal/config"
)

func standardSchedule() config.Schedule {
	monThu := config.DaySchedule{
		Enabled: true,
		Times: []config.ScheduleEntry{
			{Time: "08:30"}, {Time: "13:30"}, {Time: "14:15"}, {Time: "17:30"},
		},
	}
	fri := config.DaySchedule{
		Enabled: true,
		Times: []config.ScheduleEntry{
			{Time: "08:00"}, {Time: "15:00"},
		},
	}
	return config.Schedule{
		Monday: monThu, Tuesday: monThu, Wednesday: monThu, Thursday: monThu,
		Friday: fri,
	}
}

func TestGenerateCrons_DSTZone(t *testing.T) {
	crons := GenerateCrons(standardSchedule(), "Europe/Madrid")

	// 6 local sign times x 2 UTC offsets = 12 entries
	if len(crons) != 12 {
		t.Fatalf("expected 12 cron entries, got %d", len(crons))
	}

	for _, c := range crons {
		parts := strings.Fields(c.Cron)
		if len(parts) != 5 {
			t.Fatalf("invalid cron shape: %s", c.Cron)
		}
		if strings.Contains(parts[1], ",") {
			t.Errorf("DST cron should use separate single-hour entries, got: %s", c.Cron)
		}
		if parts[3] != "*" {
			t.Errorf("cron should have * for month field, got: %s", parts[3])
		}
	}
}

func TestGenerateCrons_CETAlias(t *testing.T) {
	cronsAlias := GenerateCrons(standardSchedule(), "CET")
	cronsFull := GenerateCrons(standardSchedule(), "Europe/Madrid")

	if len(cronsAlias) != len(cronsFull) {
		t.Fatalf("CET alias should produce same count as Europe/Madrid: %d vs %d",
			len(cronsAlias), len(cronsFull))
	}

	for i := range cronsAlias {
		if cronsAlias[i].Cron != cronsFull[i].Cron {
			t.Errorf("entry %d differs: %q vs %q", i, cronsAlias[i].Cron, cronsFull[i].Cron)
		}
	}
}

func TestGenerateCrons_NoDSTZone(t *testing.T) {
	crons := GenerateCrons(standardSchedule(), "UTC")

	if len(crons) != 6 {
		t.Fatalf("expected 6 cron entries, got %d", len(crons))
	}

	// No DST → no comma-separated hours
	for _, c := range crons {
		parts := strings.Fields(c.Cron)
		if strings.Contains(parts[1], ",") {
			t.Errorf("non-DST cron should not have comma-separated hours: %s", c.Cron)
		}
	}
}

func TestGenerateCrons_EST(t *testing.T) {
	schedule := config.Schedule{
		Monday: config.DaySchedule{
			Enabled: true,
			Times:   []config.ScheduleEntry{{Time: "09:00"}},
		},
	}
	crons := GenerateCrons(schedule, "America/New_York")

	// EST = UTC-5, EDT = UTC-4. 09:00 -> UTC 14 (EST) or 13 (EDT).
	if len(crons) != 2 {
		t.Fatalf("expected 2 offset-specific cron entries, got %d", len(crons))
	}
	got := map[string]bool{}
	for _, c := range crons {
		got[c.Cron] = true
	}
	if !got["0 13 * * 1"] || !got["0 14 * * 1"] {
		t.Errorf("expected hours 13 and 14 for 09:00 EST/EDT, got: %v", got)
	}
}

func TestGenerateCrons_SouthernHemisphere(t *testing.T) {
	schedule := config.Schedule{
		Monday: config.DaySchedule{
			Enabled: true,
			Times:   []config.ScheduleEntry{{Time: "08:00"}},
		},
	}
	crons := GenerateCrons(schedule, "Australia/Sydney")

	// AEST=UTC+10, AEDT=UTC+11. Monday 08:00 -> Sunday 22:00/21:00 UTC.
	if len(crons) != 2 {
		t.Fatalf("expected 2 offset-specific cron entries, got %d", len(crons))
	}
	got := map[string]bool{}
	for _, c := range crons {
		got[c.Cron] = true
	}
	if !got["0 21 * * 0"] || !got["0 22 * * 0"] {
		t.Errorf("expected Sunday UTC hours 21 and 22 for Monday 08:00 Sydney, got: %v", got)
	}
}

func TestGenerateCrons_PreviousUTCDay(t *testing.T) {
	schedule := config.Schedule{
		Monday: config.DaySchedule{
			Enabled: true,
			Times:   []config.ScheduleEntry{{Time: "00:30"}},
		},
	}
	crons := GenerateCrons(schedule, "Europe/Madrid")

	got := map[string]bool{}
	for _, c := range crons {
		got[c.Cron] = true
	}
	if !got["30 22 * * 0"] || !got["30 23 * * 0"] {
		t.Errorf("expected Monday 00:30 Madrid to run on Sunday UTC, got: %v", got)
	}
}

func TestGenerateCrons_DeterministicOrder(t *testing.T) {
	schedule := standardSchedule()

	// Run multiple times and ensure same output
	first := GenerateCrons(schedule, "CET")
	for i := 0; i < 20; i++ {
		got := GenerateCrons(schedule, "CET")
		if len(got) != len(first) {
			t.Fatalf("iteration %d: length changed %d vs %d", i, len(got), len(first))
		}
		for j := range first {
			if got[j].Cron != first[j].Cron {
				t.Errorf("iteration %d, entry %d: %q vs %q", i, j, got[j].Cron, first[j].Cron)
			}
		}
	}
}

func TestGenerateCrons_DisabledDays(t *testing.T) {
	schedule := config.Schedule{
		Monday:  config.DaySchedule{Enabled: true, Times: []config.ScheduleEntry{{Time: "08:00"}}},
		Tuesday: config.DaySchedule{Enabled: false},
	}
	crons := GenerateCrons(schedule, "UTC")

	if len(crons) != 1 {
		t.Fatalf("expected 1 entry (only Monday), got %d", len(crons))
	}

	// Should only have day 1 (Monday)
	if !strings.HasSuffix(crons[0].Cron, " 1") {
		t.Errorf("expected day 1 only, got: %s", crons[0].Cron)
	}
}

func TestSignTimes(t *testing.T) {
	times := signTimes(standardSchedule())

	expected := map[string]bool{
		"08:30": true, "13:30": true, "14:15": true, "17:30": true,
		"08:00": true, "15:00": true,
	}

	if len(times) != len(expected) {
		t.Fatalf("expected %d unique times, got %d: %v", len(expected), len(times), times)
	}

	for _, tt := range times {
		if !expected[tt] {
			t.Errorf("unexpected sign time: %s", tt)
		}
	}
}

func TestLocalToUTC(t *testing.T) {
	tests := []struct {
		hour, offset, wantHour, wantDayDelta int
	}{
		{8, 1, 7, 0},    // CET
		{8, 2, 6, 0},    // CEST
		{8, -5, 13, 0},  // EST
		{0, 1, 23, -1},  // midnight CET -> previous UTC day
		{23, -1, 0, 1},  // 23:00 UTC-1 -> next UTC day
		{8, 10, 22, -1}, // AEST -> previous UTC day
	}

	for _, tt := range tests {
		gotHour, gotDayDelta := localToUTC(tt.hour, tt.offset)
		if gotHour != tt.wantHour || gotDayDelta != tt.wantDayDelta {
			t.Errorf("localToUTC(%d, %d) = (%d, %d), want (%d, %d)",
				tt.hour, tt.offset, gotHour, gotDayDelta, tt.wantHour, tt.wantDayDelta)
		}
	}
}

func TestHasDST(t *testing.T) {
	if !hasDST("Europe/Madrid") {
		t.Error("Europe/Madrid should have DST")
	}
	if !hasDST("CET") {
		t.Error("CET alias should have DST")
	}
	if hasDST("UTC") {
		t.Error("UTC should not have DST")
	}
}

func TestGenerateWorkflowYAML_ContainsCatchUpSchedule(t *testing.T) {
	yaml := GenerateWorkflowYAML(standardSchedule(), "CET")

	if !strings.Contains(yaml, "catch-up every 15m") {
		t.Error("workflow should contain periodic catch-up cron entries")
	}
	if !strings.Contains(yaml, "--catch-up '") {
		t.Error("workflow should call sign catch-up mode")
	}
	if !strings.Contains(yaml, "--catch-up-window '2h'") {
		t.Error("workflow should use a bounded catch-up window")
	}
	if strings.Contains(yaml, ",7 * *") {
		t.Error("DST workflow should not generate comma-separated UTC hours")
	}
}

func TestGenerateWorkflowYAML_EmptyScheduleHasNoCronSchedule(t *testing.T) {
	yaml := GenerateWorkflowYAML(config.Schedule{}, "UTC")

	if strings.Contains(yaml, "  schedule:") {
		t.Error("empty schedule should not include an automatic cron trigger")
	}
	if !strings.Contains(yaml, "  workflow_dispatch:") {
		t.Error("empty schedule should keep a manual dispatch trigger")
	}
}

func TestGenerateWorkflowYAML_NoDSTNoGuard(t *testing.T) {
	yaml := GenerateWorkflowYAML(standardSchedule(), "UTC")

	if strings.Contains(yaml, "Timezone guard") {
		t.Error("non-DST workflow should NOT contain timezone guard")
	}
}

func TestGenerateWorkflowYAML_ValidBashSyntax(t *testing.T) {
	yaml := GenerateWorkflowYAML(standardSchedule(), "CET")

	// The %% bug: ensure no double %% in the output (only single %)
	if strings.Contains(yaml, "RANDOM %%") {
		t.Error("YAML contains %% which is invalid bash — should be single %")
	}
	if !strings.Contains(yaml, "RANDOM % ") {
		t.Error("YAML should contain RANDOM % (valid bash modulo)")
	}
}

func TestGenerateWorkflowYAML_ContainsCatchUpSpec(t *testing.T) {
	yaml := GenerateWorkflowYAML(standardSchedule(), "CET")

	if !strings.Contains(yaml, "1:08:30:in") {
		t.Error("workflow should encode Monday IN catch-up event")
	}
	if !strings.Contains(yaml, "1:13:30:out") {
		t.Error("workflow should encode Monday OUT catch-up event")
	}
	if !strings.Contains(yaml, "--catch-up-timezone 'Europe/Madrid'") {
		t.Error("workflow should pass resolved IANA timezone")
	}
	if strings.Contains(yaml, `case "$CRON" in`) {
		t.Error("workflow should no longer rely on exact cron case mapping")
	}
}

func TestGenerateCrons_ActionField(t *testing.T) {
	crons := GenerateCrons(standardSchedule(), "UTC")

	want := map[string]string{
		"Mon-Tue-Wed-Thu 08:30 (UTC+0)": "in",
		"Mon-Tue-Wed-Thu 13:30 (UTC+0)": "out",
		"Mon-Tue-Wed-Thu 14:15 (UTC+0)": "in",
		"Mon-Tue-Wed-Thu 17:30 (UTC+0)": "out",
		"Fri 08:00 (UTC+0)":             "in",
		"Fri 15:00 (UTC+0)":             "out",
	}

	got := make(map[string]string, len(crons))
	for _, c := range crons {
		got[c.Comment] = c.Action
	}
	for comment, action := range want {
		if got[comment] != action {
			t.Errorf("%s action = %q, want %q", comment, got[comment], action)
		}
	}
}

func TestGenerateCatchUpCrons(t *testing.T) {
	crons := GenerateCatchUpCrons(standardSchedule(), "CET", catchUpWindowMinutes)
	if len(crons) == 0 {
		t.Fatal("expected catch-up crons")
	}

	var sawCET, sawCEST bool
	for _, cron := range crons {
		if cron.Action != "catch-up" {
			t.Fatalf("unexpected action: %#v", cron)
		}
		if strings.Contains(cron.Comment, "UTC+1") {
			sawCET = true
		}
		if strings.Contains(cron.Comment, "UTC+2") {
			sawCEST = true
		}
		if !strings.HasPrefix(cron.Cron, "7,22,37,52 ") {
			t.Fatalf("catch-up cron should run offset from exact sign minutes: %#v", cron)
		}
	}
	if !sawCET || !sawCEST {
		t.Fatalf("expected both DST offsets, got %#v", crons)
	}
}
