package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/ngavilan-dogfy/woffux/internal/config"
	"github.com/ngavilan-dogfy/woffux/internal/woffu"
)

func TestGetActionsSortsPresetNames(t *testing.T) {
	d := &Dashboard{
		cfg: &config.Config{
			SavedSchedules: map[string]config.Schedule{
				"zeta":  {},
				"alpha": {},
			},
		},
	}

	actions := d.getActions()
	var presetKeys []string
	for _, action := range actions {
		if len(action.key) > len("preset:") && action.key[:len("preset:")] == "preset:" {
			presetKeys = append(presetKeys, action.key)
		}
	}

	if len(presetKeys) != 2 || presetKeys[0] != "preset:alpha" || presetKeys[1] != "preset:zeta" {
		t.Fatalf("unexpected preset action order: %#v", presetKeys)
	}
}

func TestGetActionsMarksCurrentPresetReadOnly(t *testing.T) {
	d := &Dashboard{
		cfg: &config.Config{
			ActiveSchedule:  "classic",
			GithubFork:      "owner/woffux",
			WoffuCompanyURL: "https://example.woffu.com",
			SavedSchedules: map[string]config.Schedule{
				"classic":   {},
				"lunchtime": {},
			},
		},
	}
	active := true
	d.autoActive = &active

	actions := d.getActions()
	var current action
	for _, a := range actions {
		if a.key == "preset:classic" {
			current = a
			break
		}
	}

	if !current.current {
		t.Fatalf("classic preset was not marked current: %#v", current)
	}
	if isSelectableAction(current) {
		t.Fatalf("current preset should be read-only: %#v", current)
	}
	if current.name != "classic" || current.desc != "Current schedule" {
		t.Fatalf("unexpected current preset label: %#v", current)
	}
}

func TestGetActionsDisablesGitHubActionsWhenForkMissing(t *testing.T) {
	d := &Dashboard{
		cfg: &config.Config{
			WoffuCompanyURL: "https://example.woffu.com",
		},
	}

	actions := d.getActions()
	for _, key := range []string{"auto-unavailable", "sync-unavailable", "open-gh-unavailable"} {
		t.Run(key, func(t *testing.T) {
			var found *action
			for i := range actions {
				if actions[i].key == key {
					found = &actions[i]
					break
				}
			}
			if found == nil {
				t.Fatalf("missing action %s in %#v", key, actions)
			}
			if isSelectableAction(*found) {
				t.Fatalf("%s should be disabled: %#v", key, *found)
			}
		})
	}
}

func TestMoveActionCursorSkipsReadOnlyRows(t *testing.T) {
	actions := []action{
		{key: "---", name: "Section"},
		{key: "disabled", name: "Disabled", disabled: true},
		{key: "preset:classic", name: "classic", current: true},
		{key: "sign", name: "Sign"},
		{key: "sync", name: "Sync"},
	}

	if got := firstSelectableAction(actions); got != 3 {
		t.Fatalf("first selectable = %d, want 3", got)
	}
	if got := moveActionCursor(actions, 3, 1); got != 4 {
		t.Fatalf("move down = %d, want 4", got)
	}
	if got := moveActionCursor(actions, 3, -1); got != 4 {
		t.Fatalf("move up should wrap to last selectable, got %d", got)
	}
	if got := moveActionCursor(actions, 4, 1); got != 3 {
		t.Fatalf("move down should wrap to first selectable, got %d", got)
	}
}

func TestExecuteSavePresetOpensNameInput(t *testing.T) {
	d := &Dashboard{cfg: &config.Config{}, overlay: overlayMenu, presetInput: "old"}

	if cmd := d.executeAction(action{key: "save-preset", name: "Save as preset"}); cmd != nil {
		t.Fatal("save preset should only change overlay state")
	}
	if d.overlay != overlaySavePreset {
		t.Fatalf("overlay = %v, want save preset overlay", d.overlay)
	}
	if d.presetInput != "" {
		t.Fatalf("preset input = %q, want empty", d.presetInput)
	}
}

func TestRenderOverlayMenuShowsClearPresetState(t *testing.T) {
	d := &Dashboard{
		cfg: &config.Config{
			ActiveSchedule:  "classic",
			GithubFork:      "owner/woffux",
			WoffuCompanyURL: "https://example.woffu.com",
			SavedSchedules: map[string]config.Schedule{
				"classic":   {},
				"lunchtime": {},
			},
		},
		width:  100,
		height: 32,
	}
	active := true
	d.autoActive = &active

	rendered := d.renderOverlayMenu()
	for _, want := range []string{
		"Actions",
		"Disable auto-sign",
		"✓ classic",
		"Current schedule",
		"lunchtime",
		"Save as preset",
		"Tools",
		"Open GitHub Actions",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered menu missing %q:\n%s", want, rendered)
		}
	}
	if strings.Contains(rendered, "preset-active") {
		t.Fatalf("rendered stale preset-active marker:\n%s", rendered)
	}
}

func TestApplyConfigClearsStaleAutoStatus(t *testing.T) {
	active := true
	d := &Dashboard{
		cfg:        &config.Config{GithubFork: "old/woffux"},
		autoActive: &active,
	}

	d.applyConfig(&config.Config{GithubFork: "new/woffux"})
	if d.autoActive != nil {
		t.Fatal("expected auto status to reset when fork changes")
	}

	active = true
	d.autoActive = &active
	d.applyConfig(&config.Config{})
	if d.autoActive != nil {
		t.Fatal("expected auto status to reset when fork is removed")
	}
}

func TestAutoStatusMsgIgnoresStaleRepo(t *testing.T) {
	d := &Dashboard{cfg: &config.Config{GithubFork: "current/woffux"}}

	d.Update(autoStatusMsg{repo: "old/woffux", enabled: true})
	if d.autoActive != nil {
		t.Fatal("stale auto status should be ignored")
	}

	d.Update(autoStatusMsg{repo: "current/woffux", enabled: true})
	if d.autoActive == nil || !*d.autoActive {
		t.Fatalf("current auto status was not applied: %#v", d.autoActive)
	}
}

func TestDataMsgStoresCompanyID(t *testing.T) {
	d := &Dashboard{cfg: &config.Config{}}

	d.Update(dataMsg{userId: 12, companyId: 34})

	if d.userId != 12 || d.companyId != 34 {
		t.Fatalf("cached ids = (%d, %d), want (12, 34)", d.userId, d.companyId)
	}
}

func TestRequestDoneClearsCalendarSelection(t *testing.T) {
	d := &Dashboard{
		cfg: &config.Config{},
		cal: &calendarGrid{
			selected: map[string]bool{"2026-05-19": true},
		},
	}

	d.Update(requestDoneMsg{count: 1, action: "submitted"})

	if len(d.cal.selected) != 0 {
		t.Fatalf("selection was not cleared: %#v", d.cal.selected)
	}
}

func TestSignSlotSummary(t *testing.T) {
	got := signSlotSummary([]woffu.SignSlot{
		{In: "2026-05-19T08:30:00.000", Out: "2026-05-19T13:30:00.000"},
		{In: "2026-05-19T14:15:00.000"},
	})

	for _, want := range []string{"Sign history:", "IN 08:30", "OUT 13:30", "IN 14:15"} {
		if !strings.Contains(got, want) {
			t.Fatalf("summary missing %q: %s", want, got)
		}
	}
}

func TestExecuteViewSignsShowsFlash(t *testing.T) {
	d := &Dashboard{
		cfg: &config.Config{},
		cal: newCalendarGrid(2026, time.May, []woffu.CalendarDay{
			{
				Date: "2026-05-19",
				Signs: []woffu.SignSlot{
					{In: "2026-05-19T08:30:00.000", Out: "2026-05-19T13:30:00.000"},
				},
			},
		}),
	}
	d.cal.cursor = 19

	if cmd := d.executeDayAction(action{key: "view-signs", name: "View sign history"}); cmd == nil {
		t.Fatal("expected clear-flash command")
	}
	if !strings.Contains(d.flash, "IN 08:30") || !strings.Contains(d.flash, "OUT 13:30") {
		t.Fatalf("unexpected flash: %q", d.flash)
	}
}

func TestNoopDayActionIsDisabled(t *testing.T) {
	d := &Dashboard{
		cfg: &config.Config{},
		cal: newCalendarGrid(2026, time.May, []woffu.CalendarDay{
			{Date: "2026-05-19", Status: "holiday", EventNames: []string{"Local holiday"}},
		}),
	}
	d.cal.cursor = 19

	actions := d.getDayActions()
	if len(actions) != 1 || actions[0].key != "noop" || !actions[0].disabled {
		t.Fatalf("unexpected day actions: %#v", actions)
	}
}
