package tui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ngavilan-dogfy/woffux/internal/agent"
	"github.com/ngavilan-dogfy/woffux/internal/config"
)

func (d *Dashboard) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == "ctrl+c" {
		return d, tea.Quit
	}

	switch d.overlay {
	case overlayHelp:
		d.overlay = overlayNone
		return d, nil
	case overlayConfirm:
		return d, d.keyConfirm(key)
	case overlayInput:
		return d, d.keyInput(msg)
	case overlayPalette:
		return d, d.keyPalette(msg)
	case overlayDay:
		return d, d.keyDayMenu(key)
	case overlayEditor:
		return d, d.keyEditor(msg)
	case overlayMenu:
		return d, d.keyMenu(key)
	case overlayDates:
		return d, d.keyDates(msg)
	}

	if d.activeTab == tabSchedule {
		if cmd, handled := d.keySchedule(key); handled {
			return d, cmd
		}
	}

	if d.activeTab == tabCalendar && d.cal != nil {
		if cmd, handled := d.keyCalendar(key); handled {
			return d, cmd
		}
	}

	switch key {
	case "q":
		return d, tea.Quit
	case "tab", "right", "l":
		d.switchTab((d.activeTab + 1) % tabCount)
	case "shift+tab", "left", "h":
		d.switchTab((d.activeTab - 1 + tabCount) % tabCount)
	case "1":
		d.switchTab(tabToday)
	case "2":
		d.switchTab(tabCalendar)
	case "3":
		d.switchTab(tabSchedule)
	case "4":
		d.switchTab(tabBalance)
	case "enter", ":", "ctrl+k", "/":
		d.openPalette()
	case "?":
		d.overlay = overlayHelp
	case "s":
		return d, d.askSign()
	case "r":
		if d.refreshing || d.loading {
			return d, nil
		}
		return d, d.refreshData()
	case "o":
		return d, d.openWoffu()
	case "g":
		return d, d.openGitHub()
	case "a":
		return d, d.runAction("toggle-github")
	case "m":
		return d, d.runAction("toggle-agent")
	case "e":
		d.switchTab(tabSchedule)
		d.selectActive()
		return d, d.schedEdit()
	case "U":
		if d.latest == "" {
			return d, d.showToast("You're on the latest version ("+AppVersion+")", toastInfo)
		}
		return d, d.runUpdate()
	}
	return d, nil
}

func (d *Dashboard) switchTab(tab int) {
	d.activeTab = tab
}

// ── Calendar ──

// keyCalendar handles calendar-only keys; handled=false lets global keys run.
func (d *Dashboard) keyCalendar(key string) (tea.Cmd, bool) {
	c := d.cal
	move := func(changed bool) (tea.Cmd, bool) {
		if changed {
			return d.fetchCalendarData(), true
		}
		return nil, true
	}
	switch key {
	case " ", "shift+left", "shift+right", "shift+up", "shift+down":
		if !c.loaded {
			return d.showToast("Still loading "+c.month.String()+" — one moment", toastInfo), true
		}
	}
	switch key {
	case "left", "h":
		return move(c.move(-1))
	case "right", "l":
		return move(c.move(1))
	case "up", "k":
		return move(c.move(-7))
	case "down", "j":
		return move(c.move(7))
	case "shift+left":
		c.extend(-1)
		return nil, true
	case "shift+right":
		c.extend(1)
		return nil, true
	case "shift+up":
		c.extend(-7)
		return nil, true
	case "shift+down":
		c.extend(7)
		return nil, true
	case "[", "H", "pgup":
		c.shiftMonth(-1)
		return d.fetchCalendarData(), true
	case "]", "L", "pgdown":
		c.shiftMonth(1)
		return d.fetchCalendarData(), true
	case ".", "T", "home":
		if c.jumpToday(d.clock()) {
			return d.fetchCalendarData(), true
		}
		return nil, true
	case " ":
		c.toggleSelect(c.cursorDate())
		return nil, true
	case "x", "esc":
		if len(c.selected) > 0 {
			c.clearSelection()
			return d.showToast("Selection cleared", toastInfo), true
		}
		return nil, key == "x"
	case "enter":
		d.openDayMenu()
		return nil, true
	case "c":
		return d.runDayAction("cancel-all"), true
	}
	for _, k := range requestKinds {
		if key == k.hotkey {
			return d.runDayAction("req:" + k.key), true
		}
	}
	return nil, false
}

// ── Confirm ──

func (d *Dashboard) openConfirm(spec *confirmSpec) {
	d.confirm = spec
	d.overlay = overlayConfirm
	d.confirmAt = time.Now()
}

// confirmRepeatGuard ignores confirm keys that arrive right after the dialog
// opened: that's a held or bounced key, not a decision.
const confirmRepeatGuard = 400 * time.Millisecond

func (d *Dashboard) keyConfirm(key string) tea.Cmd {
	spec := d.confirm
	if spec == nil {
		d.overlay = overlayNone
		return nil
	}
	if (key == "enter" || key == "y" || key == "Y") && time.Since(d.confirmAt) < confirmRepeatGuard {
		return nil
	}
	switch key {
	case "y", "Y":
		d.overlay, d.confirm = overlayNone, nil
		return spec.onYes()
	case "enter":
		if spec != nil && spec.danger {
			return nil // dangerous confirmations need an explicit "y"
		}
		d.overlay, d.confirm = overlayNone, nil
		return spec.onYes()
	case "esc", "n", "N", "q":
		d.overlay, d.confirm = overlayNone, nil
		return d.showToast("Cancelled — nothing was sent", toastInfo)
	}
	return nil
}

// ── Text input (save preset) ──

func (d *Dashboard) keyInput(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		d.overlay = overlayNone
		return nil
	case "enter":
		name := config.NormalizePresetName(d.input)
		if name == "" {
			return d.showToast("Give the schedule a name first", toastErr)
		}
		d.overlay = overlayNone
		return d.savePreset(name)
	case "backspace":
		r := []rune(d.input)
		if len(r) > 0 {
			d.input = string(r[:len(r)-1])
		}
		return nil
	case "ctrl+u":
		d.input = ""
		return nil
	}
	if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
		d.input += string(msg.Runes)
		if msg.Type == tea.KeySpace {
			d.input += " "
		}
	}
	return nil
}

// ── Palette ──

func (d *Dashboard) openPalette() {
	d.overlay = overlayPalette
	d.query = ""
	d.cursor = firstEnabled(d.filteredActions())
}

func (d *Dashboard) keyPalette(msg tea.KeyMsg) tea.Cmd {
	items := d.filteredActions()
	switch msg.String() {
	case "esc":
		if d.query != "" {
			d.query = ""
			d.cursor = firstEnabled(d.filteredActions())
			return nil
		}
		d.overlay = overlayNone
		return nil
	case "up", "ctrl+p", "shift+tab":
		d.cursor = moveCursor(items, d.cursor, -1)
		return nil
	case "down", "ctrl+n", "tab":
		d.cursor = moveCursor(items, d.cursor, 1)
		return nil
	case "enter":
		if d.cursor >= 0 && d.cursor < len(items) && items[d.cursor].enabled() {
			d.overlay = overlayNone
			return d.runAction(items[d.cursor].key)
		}
		return nil
	case "backspace":
		r := []rune(d.query)
		if len(r) > 0 {
			d.query = string(r[:len(r)-1])
			d.cursor = firstEnabled(d.filteredActions())
		}
		return nil
	case "ctrl+u":
		d.query = ""
		d.cursor = firstEnabled(d.filteredActions())
		return nil
	}
	if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
		if msg.Type == tea.KeySpace {
			d.query += " "
		} else {
			d.query += string(msg.Runes)
		}
		d.cursor = firstEnabled(d.filteredActions())
	}
	return nil
}

// ── Day menu ──

func (d *Dashboard) openDayMenu() {
	d.overlay = overlayDay
	d.cursor = firstEnabled(d.dayActions())
}

func (d *Dashboard) keyDayMenu(key string) tea.Cmd {
	items := d.dayActions()
	switch key {
	case "esc", "q":
		d.overlay = overlayNone
		return nil
	case "up", "k", "shift+tab":
		d.cursor = moveCursor(items, d.cursor, -1)
		return nil
	case "down", "j", "tab":
		d.cursor = moveCursor(items, d.cursor, 1)
		return nil
	case "enter":
		if d.cursor >= 0 && d.cursor < len(items) && items[d.cursor].enabled() {
			d.overlay = overlayNone
			return d.runDayAction(items[d.cursor].key)
		}
		return nil
	}
	// Item hotkeys work inside the menu too.
	for _, it := range items {
		if it.shortcut != "" && it.shortcut == key && it.enabled() {
			d.overlay = overlayNone
			return d.runDayAction(it.key)
		}
	}
	return nil
}

// ── Shared cursor helpers ──

func firstEnabled(items []action) int {
	for i, it := range items {
		if it.enabled() {
			return i
		}
	}
	return 0
}

func moveCursor(items []action, cursor, delta int) int {
	n := len(items)
	if n == 0 {
		return 0
	}
	for step := 1; step <= n; step++ {
		next := ((cursor+delta*step)%n + n) % n
		if items[next].enabled() {
			return next
		}
	}
	return cursor
}

// agentAvailable reports whether the local agent can be managed here.
func agentAvailable() bool { return agent.Supported() }

// hasFork reports whether a GitHub fork is configured.
func (d *Dashboard) hasFork() bool { return strings.TrimSpace(d.cfg.GithubFork) != "" }

// selectActive points the schedule list at the schedule in use.
func (d *Dashboard) selectActive() {
	for i, r := range d.schedRows() {
		if (r.current) || (r.name != "" && r.name == d.cfg.ActiveSchedule) {
			d.schedCursor = i
			return
		}
	}
}
