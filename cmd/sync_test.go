package cmd

import (
	"testing"

	"github.com/ngavilan-dogfy/woffux/internal/config"
)

func TestScheduleStats(t *testing.T) {
	days, signs := scheduleStats(config.Schedule{})
	if days != 0 || signs != 0 {
		t.Fatalf("empty schedule stats = (%d, %d), want (0, 0)", days, signs)
	}

	days, signs = scheduleStats(config.Schedule{
		Monday: config.DaySchedule{
			Enabled: true,
			Times:   []config.ScheduleEntry{{Time: "08:30"}, {Time: "17:30"}},
		},
		Friday: config.DaySchedule{
			Enabled: true,
			Times:   []config.ScheduleEntry{{Time: "08:00"}, {Time: "13:00"}, {Time: "14:00"}, {Time: "15:00"}},
		},
	})
	if days != 2 || signs != 6 {
		t.Fatalf("schedule stats = (%d, %d), want (2, 6)", days, signs)
	}
}

func TestScheduleSummaryUsesAllEnabledDays(t *testing.T) {
	summary := scheduleSummary(config.Schedule{
		Tuesday: config.DaySchedule{
			Enabled: true,
			Times:   []config.ScheduleEntry{{Time: "09:00"}, {Time: "17:00"}},
		},
	})
	if summary != "1 day, 2 signs" {
		t.Fatalf("summary = %q", summary)
	}
}
