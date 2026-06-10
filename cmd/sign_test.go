package cmd

import (
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
