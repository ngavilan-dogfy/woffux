package cmd

import (
	"github.com/ngavilan-dogfy/woffux/internal/timing"
	"testing"
	"time"

	"github.com/ngavilan-dogfy/woffux/internal/woffu"
)

func TestParseExpectedSignAction(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    woffu.SignAction
		wantErr bool
	}{
		{name: "empty", value: "", want: ""},
		{name: "in", value: "in", want: woffu.SignActionIn},
		{name: "out", value: "out", want: woffu.SignActionOut},
		{name: "trim and lowercase", value: " OUT ", want: woffu.SignActionOut},
		{name: "invalid", value: "toggle", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseExpectedSignAction(tt.value)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveCatchUpSignActionSignsLateInWhenOut(t *testing.T) {
	now := time.Date(2026, time.May, 19, 8, 58, 0, 0, time.Local)

	action, matched, ok, err := resolveCatchUpSignAction(
		"2:08:30:in;2:13:30:out;2:14:15:in;2:17:30:out",
		nil,
		now,
		2*time.Hour,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok || action != woffu.SignActionIn || matched != "08:30" {
		t.Fatalf("got action=%q matched=%q ok=%v, want 08:30 in", action, matched, ok)
	}
}

func TestResolveCatchUpSignActionDoesNotSignOutWithoutPriorIn(t *testing.T) {
	now := time.Date(2026, time.May, 19, 13, 35, 0, 0, time.Local)

	action, matched, ok, err := resolveCatchUpSignAction(
		"2:08:30:in;2:13:30:out;2:14:15:in;2:17:30:out",
		nil,
		now,
		2*time.Hour,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatalf("got action=%q matched=%q, expected no catch-up", action, matched)
	}
}

func TestResolveCatchUpSignActionCatchesMissedOutEvenAfterNextInTime(t *testing.T) {
	now := time.Date(2026, time.May, 19, 14, 22, 0, 0, time.Local)
	openSlot := []woffu.SignSlot{{In: "2026-05-19T08:30:00.000"}}

	action, matched, ok, err := resolveCatchUpSignAction(
		"2:08:30:in;2:13:30:out;2:14:15:in;2:17:30:out",
		openSlot,
		now,
		2*time.Hour,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok || action != woffu.SignActionOut || matched != "13:30" {
		t.Fatalf("got action=%q matched=%q ok=%v, want 13:30 out", action, matched, ok)
	}
}

func TestResolveCatchUpSignActionSkipsAlreadySatisfiedOut(t *testing.T) {
	// Everything signed on time. A duplicate DST-offset cron (or the 15m
	// watchdog) fires at 14:22 — it must NOT re-match the 13:30 out and
	// clock the user out right after the 14:15 in.
	now := time.Date(2026, time.May, 19, 14, 22, 0, 0, time.Local)
	slots := []woffu.SignSlot{
		{In: "2026-05-19T08:30:00.000", Out: "2026-05-19T13:30:00.000"},
		{In: "2026-05-19T14:15:00.000"},
	}

	action, matched, ok, err := resolveCatchUpSignAction(
		"2:08:30:in;2:13:30:out;2:14:15:in;2:17:30:out",
		slots,
		now,
		2*time.Hour,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatalf("got action=%q matched=%q, expected satisfied 13:30 out to be skipped", action, matched)
	}
}

func TestResolveCatchUpSignActionSkipsOutSatisfiedByLateCatchUp(t *testing.T) {
	// The 13:30 out was caught up late at 14:06. At 14:22 (after the 14:15
	// in) the watchdog must not match 13:30 again.
	now := time.Date(2026, time.May, 19, 14, 22, 0, 0, time.Local)
	slots := []woffu.SignSlot{
		{In: "2026-05-19T08:30:00.000", Out: "2026-05-19T14:06:00.000"},
		{In: "2026-05-19T14:15:00.000"},
	}

	_, _, ok, err := resolveCatchUpSignAction(
		"2:08:30:in;2:13:30:out;2:14:15:in;2:17:30:out",
		slots,
		now,
		2*time.Hour,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected late-satisfied 13:30 out to be skipped")
	}
}

func TestResolveCatchUpSignActionStillCatchesGenuinelyMissedIn(t *testing.T) {
	// Signed out at 13:30 but the 14:15 in never happened — the watchdog at
	// 14:40 must still catch it. The 08:30 in must not satisfy 14:15.
	now := time.Date(2026, time.May, 19, 14, 40, 0, 0, time.Local)
	slots := []woffu.SignSlot{
		{In: "2026-05-19T08:30:00.000", Out: "2026-05-19T13:30:00.000"},
	}

	action, matched, ok, err := resolveCatchUpSignAction(
		"2:08:30:in;2:13:30:out;2:14:15:in;2:17:30:out",
		slots,
		now,
		2*time.Hour,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok || action != woffu.SignActionIn || matched != "14:15" {
		t.Fatalf("got action=%q matched=%q ok=%v, want 14:15 in", action, matched, ok)
	}
}

func TestResolveCatchUpSignActionAcceptsSlightlyEarlyManualSign(t *testing.T) {
	// Manual out at 13:27 (3 min early) counts as the 13:30 out; the
	// watchdog at 13:37 must not sign out again... and since the user is
	// already out, want=in anyway — so check the IN side: manual in at
	// 14:12 counts as the 14:15 in.
	now := time.Date(2026, time.May, 19, 14, 30, 0, 0, time.Local)
	slots := []woffu.SignSlot{
		{In: "2026-05-19T08:30:00.000", Out: "2026-05-19T13:27:00.000"},
		{In: "2026-05-19T14:12:00.000"},
	}

	_, _, ok, err := resolveCatchUpSignAction(
		"2:08:30:in;2:13:30:out;2:14:15:in;2:17:30:out",
		slots,
		now,
		2*time.Hour,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected early manual signs to satisfy their scheduled events")
	}
}

func TestResolveCatchUpSignActionRespectsWindow(t *testing.T) {
	now := time.Date(2026, time.May, 19, 11, 0, 0, 0, time.Local)

	_, _, ok, err := resolveCatchUpSignAction(
		"2:08:30:in;2:13:30:out",
		nil,
		now,
		2*time.Hour,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected missed IN outside catch-up window to be skipped")
	}
}

// ── Natural timing ──

const tueSpec = "2:08:30:in;2:13:30:out;2:14:15:in;2:17:30:out"

var natural = timing.Settings{InEarly: 6, InLate: 1, OutLate: 8, Seed: "test-seed"}

func tueAt(h, m int) time.Time { return time.Date(2026, time.May, 19, h, m, 0, 0, time.Local) }

func TestPlanCatchUpWaitsForNaturalMoment(t *testing.T) {
	plan, err := planCatchUp(tueSpec, nil, tueAt(8, 16), 2*time.Hour, natural, 20*time.Minute, 0)
	if err != nil || !plan.ok {
		t.Fatalf("plan = %+v, %v", plan, err)
	}
	if plan.action != woffu.SignActionIn || plan.label != "08:30" {
		t.Fatalf("plan = %+v, want 08:30 in", plan)
	}
	if !plan.at.After(tueAt(8, 16)) || plan.at.Before(tueAt(8, 24)) || plan.at.After(tueAt(8, 31)) {
		t.Fatalf("moment %s outside 08:24–08:31", plan.at.Format("15:04:05"))
	}
	// Every signer computes the same moment.
	again, _ := planCatchUp(tueSpec, nil, tueAt(8, 1), 2*time.Hour, natural, 40*time.Minute, 0)
	if !again.at.Equal(plan.at) {
		t.Fatalf("moment changed between runs: %s vs %s", again.at, plan.at)
	}
}

func TestPlanCatchUpNothingWhenTooFarAhead(t *testing.T) {
	plan, _ := planCatchUp(tueSpec, nil, tueAt(7, 0), 2*time.Hour, natural, 20*time.Minute, 0)
	if plan.ok {
		t.Fatalf("08:30 is 90 min away, nothing to do: %+v", plan)
	}
	due, _ := anyCatchUpEventDue(tueSpec, tueAt(7, 0), 2*time.Hour, natural, 20*time.Minute)
	if due {
		t.Fatal("pre-auth check must skip without touching the network")
	}
	due, _ = anyCatchUpEventDue(tueSpec, tueAt(8, 16), 2*time.Hour, natural, 20*time.Minute)
	if !due {
		t.Fatal("pre-auth check must see the upcoming moment")
	}
}

// An IN signed early by the agent must satisfy the event for the GitHub
// fallback that runs later (no double sign).
func TestPlanCatchUpEarlySignSatisfiesEvent(t *testing.T) {
	slots := []woffu.SignSlot{{In: "2026-05-19T08:24:10", Out: "2026-05-19T13:33:00"}}
	plan, _ := planCatchUp(tueSpec, slots, tueAt(13, 40), 2*time.Hour, natural, 20*time.Minute, 3*time.Minute)
	if plan.ok && plan.label != "14:15" {
		t.Fatalf("already signed events must not be planned again: %+v", plan)
	}
}

func TestPlanCatchUpGitHubGraceGoesAfterAgent(t *testing.T) {
	agentPlan, _ := planCatchUp(tueSpec, nil, tueAt(8, 16), 2*time.Hour, natural, 20*time.Minute, 0)
	ghPlan, _ := planCatchUp(tueSpec, nil, tueAt(8, 16), 2*time.Hour, natural, 20*time.Minute, 3*time.Minute)
	if ghPlan.at.Sub(agentPlan.at) != 3*time.Minute {
		t.Fatalf("GitHub should sign 3 min after the agent: %s vs %s", ghPlan.at, agentPlan.at)
	}
}

func TestPlanCatchUpExactTimingSignsOnTheMinute(t *testing.T) {
	plan, _ := planCatchUp(tueSpec, nil, tueAt(8, 16), 2*time.Hour, timing.Settings{}, 20*time.Minute, 0)
	if !plan.ok || plan.at.Format("15:04:05") != "08:30:00" {
		t.Fatalf("exact timing should wait for 08:30:00, got %+v", plan)
	}
}

func TestSeasonalSpecFollowsDates(t *testing.T) {
	seasons := []string{"07-01..08-31=1:08:00:in;1:15:00:out", "12-24..01-06=1:09:00:in;1:14:00:out"}
	day := func(s string) time.Time { d, _ := time.Parse("2006-01-02", s); return d }
	cases := map[string]string{
		"2026-06-30": "base",
		"2026-07-01": "1:08:00:in;1:15:00:out",
		"2026-08-31": "1:08:00:in;1:15:00:out",
		"2026-09-01": "base",
		"2026-12-28": "1:09:00:in;1:14:00:out",
		"2027-01-06": "1:09:00:in;1:14:00:out",
	}
	for d, want := range cases {
		if got := seasonalSpec("base", seasons, day(d)); got != want {
			t.Errorf("%s -> %s, want %s", d, got, want)
		}
	}
}
