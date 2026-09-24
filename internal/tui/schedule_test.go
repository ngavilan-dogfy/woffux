package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ngavilan-dogfy/woffux/internal/config"
)

// scheduleDash returns a dashboard whose config lives in a temp HOME, so
// schedule actions really save and reload.
func scheduleDash(t *testing.T) *Dashboard {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	d := previewDashboard("11:07", nil)
	d.cfg.GithubFork = "" // no GitHub sync in tests
	if err := config.Save(d.cfg); err != nil {
		t.Fatal(err)
	}
	d.activeTab = tabSchedule
	return d
}

// run executes a command chain until it settles, feeding messages back.
// Timers (toast expiry, ticks) are skipped: anything slower than 200ms.
func run(d *Dashboard, cmd tea.Cmd) {
	for i := 0; cmd != nil && i < 10; i++ {
		ch := make(chan tea.Msg, 1)
		go func(c tea.Cmd) { ch <- c() }(cmd)
		var msg tea.Msg
		select {
		case msg = <-ch:
		case <-time.After(200 * time.Millisecond):
			return
		}
		if msg == nil {
			return
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				run(d, c)
			}
			return
		}
		_, cmd = d.Update(msg)
	}
}

func typeText(d *Dashboard, s string) {
	for _, r := range s {
		d.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func TestScheduleTabCreatePreset(t *testing.T) {
	d := scheduleDash(t)
	d.Update(key("n"))
	if d.overlay != overlayMenu {
		t.Fatal("n opens the template picker")
	}
	d.cursor = 3 // "Continuous (intensiva)"
	_, cmd := d.Update(key("enter"))
	run(d, cmd)
	if d.overlay != overlayEditor || d.editor.text.Value() != "mon-fri 08:00-15:00" {
		t.Fatalf("editor should open prefilled, got %v %q", d.overlay, d.editor.text.Value())
	}
	typeText(d, "summer")
	_, cmd = d.Update(key("enter")) // name → week
	run(d, cmd)
	_, cmd = d.Update(key("enter")) // save
	run(d, cmd)
	if _, ok := d.cfg.SavedSchedules["summer"]; !ok {
		t.Fatalf("preset not saved: %v", d.cfg.SchedulePresetNames())
	}
	saved, _ := config.Load()
	if _, ok := saved.SavedSchedules["summer"]; !ok {
		t.Fatal("preset not written to disk")
	}
}

func TestScheduleEditorRejectsBadWeek(t *testing.T) {
	d := scheduleDash(t)
	d.selectActive()
	run(d, d.schedEdit())
	d.editor.text.SetValue("mon 17-9")
	_, cmd := d.Update(key("enter"))
	run(d, cmd)
	if d.overlay != overlayEditor || !strings.Contains(d.toast.text, "forward") {
		t.Fatalf("invalid week must keep the editor open with the error, toast=%q", d.toast.text)
	}
}

func TestScheduleUseAndRename(t *testing.T) {
	d := scheduleDash(t)
	d.selectPreset("summer")
	_, cmd := d.Update(key("enter"))
	run(d, cmd)
	if d.cfg.ActiveSchedule != "summer" {
		t.Fatalf("active = %q", d.cfg.ActiveSchedule)
	}
	run(d, d.schedRename())
	d.editor.name.SetValue("verano")
	_, cmd = d.Update(key("enter"))
	run(d, cmd)
	if d.cfg.ActiveSchedule != "verano" {
		t.Fatalf("rename should follow the active preset, got %q", d.cfg.ActiveSchedule)
	}
}

func TestScheduleSummerAndProtectedDelete(t *testing.T) {
	d := scheduleDash(t)
	run(d, d.openDates("07-01", "08-31", "summer"))
	_, cmd := d.Update(key("enter")) // from → to
	run(d, cmd)
	_, cmd = d.Update(key("enter")) // save
	run(d, cmd)
	if len(d.cfg.Seasons.Periods) != 1 || d.cfg.Seasons.Periods[0].Preset != "summer" {
		t.Fatalf("seasons = %+v", d.cfg.Seasons)
	}
	d.selectPreset("summer")
	d.Update(key("x"))
	if d.overlay == overlayConfirm || !strings.Contains(d.toast.text, "seasonal") {
		t.Fatal("a preset used by seasons must not be deletable")
	}
}
