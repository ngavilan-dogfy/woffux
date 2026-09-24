package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ngavilan-dogfy/woffux/internal/config"
	"github.com/ngavilan-dogfy/woffux/internal/timing"
	"github.com/ngavilan-dogfy/woffux/internal/woffu"
)

// The Schedule tab manages everything about *when* you sign: saved
// presets, editing a week as text with a live preview, summer hours and
// natural timing. Every change is saved to ~/.woffux.yaml and, when a
// GitHub backup exists, pushed to it.

// schedRow is one line of the preset list.
type schedRow struct {
	name    string // preset name; "" for the unsaved current week
	current bool   // the unsaved current week
	create  bool   // "+ New schedule"
}

func (d *Dashboard) schedRows() []schedRow {
	var rows []schedRow
	if d.cfg.ActiveSchedule == "" || !config.SchedulesEqual(d.cfg.Schedule, d.cfg.SavedSchedules[d.cfg.ActiveSchedule]) {
		rows = append(rows, schedRow{current: true})
	}
	for _, n := range d.cfg.SchedulePresetNames() {
		rows = append(rows, schedRow{name: n})
	}
	return append(rows, schedRow{create: true})
}

func (d *Dashboard) schedSelected() schedRow {
	rows := d.schedRows()
	d.schedCursor = min(max(d.schedCursor, 0), len(rows)-1)
	return rows[d.schedCursor]
}

func (d *Dashboard) schedOf(r schedRow) config.Schedule {
	if r.current || r.create {
		return d.cfg.Schedule
	}
	return d.cfg.SavedSchedules[r.name]
}

// ── Editor ──

type editorMode int

const (
	editNew editorMode = iota
	editPreset
	editCurrent
	editRename
)

type schedEditor struct {
	mode   editorMode
	target string // preset being edited / renamed
	name   textinput.Model
	text   textinput.Model
	focus  int // 0 name, 1 text
}

func newInput(placeholder, value string, width int) textinput.Model {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.SetValue(value)
	ti.Prompt = ""
	ti.Width = width
	ti.CharLimit = 200
	ti.Cursor.Style = lipgloss.NewStyle().Foreground(cBrand)
	ti.TextStyle = sBold
	ti.PlaceholderStyle = sFaint
	return ti
}

func (d *Dashboard) openEditor(mode editorMode, target, name, text string) tea.Cmd {
	w := min(64, d.width-14)
	e := &schedEditor{mode: mode, target: target,
		name: newInput("e.g. winter, summer, 4-days", name, w),
		text: newInput("mon-thu 8:30-13:30 14:15-17:30, fri 8-15", text, w)}
	switch mode {
	case editNew, editRename:
		e.focus = 0
		e.name.Focus()
	default:
		e.focus = 1
		e.text.Focus()
		e.text.CursorEnd()
	}
	d.editor = e
	d.overlay = overlayEditor
	return textinput.Blink
}

func (d *Dashboard) keyEditor(msg tea.KeyMsg) tea.Cmd {
	e := d.editor
	hasName := e.mode == editNew || e.mode == editRename
	hasText := e.mode != editRename
	switch msg.String() {
	case "esc":
		d.overlay, d.editor = overlayNone, nil
		return nil
	case "tab", "shift+tab", "up", "down":
		if hasName && hasText {
			e.focus = 1 - e.focus
			if e.focus == 0 {
				e.text.Blur()
				return e.name.Focus()
			}
			e.name.Blur()
			return e.text.Focus()
		}
		return nil
	case "enter":
		if hasName && hasText && e.focus == 0 {
			e.focus = 1
			e.name.Blur()
			return e.text.Focus()
		}
		return d.saveEditor()
	}
	var cmd tea.Cmd
	if e.focus == 0 {
		e.name, cmd = e.name.Update(msg)
	} else {
		e.text, cmd = e.text.Update(msg)
	}
	return cmd
}

// saveEditor validates and persists the editor's content.
func (d *Dashboard) saveEditor() tea.Cmd {
	e := d.editor
	name := config.NormalizePresetName(e.name.Value())
	var sched config.Schedule
	if e.mode != editRename {
		s, err := config.ParseScheduleText(e.text.Value())
		if err != nil {
			return d.showToast(err.Error(), toastErr)
		}
		if s.WeekMinutes() == 0 {
			return d.showToast("That week has no working hours", toastErr)
		}
		sched = s
	}
	if (e.mode == editNew || e.mode == editRename) && name == "" {
		return d.showToast("Give it a name", toastErr)
	}
	if e.mode == editNew {
		if _, exists := d.cfg.SavedSchedules[name]; exists {
			return d.showToast(fmt.Sprintf("%q already exists — pick another name", name), toastErr)
		}
	}
	d.overlay, d.editor = overlayNone, nil

	switch e.mode {
	case editNew:
		return d.mutateConfig(fmt.Sprintf("Created %q — ⏎ to start using it", name), false, func(c *config.Config) error {
			return c.SaveSchedulePreset(name, sched)
		}, func() { d.selectPreset(name) })
	case editPreset:
		target := e.target
		return d.mutateConfig(fmt.Sprintf("Saved %q", target), target == d.cfg.ActiveSchedule, func(c *config.Config) error {
			if err := c.SaveSchedulePreset(target, sched); err != nil {
				return err
			}
			if c.ActiveSchedule == target {
				c.Schedule = sched
			}
			return nil
		}, nil)
	case editCurrent:
		return d.mutateConfig("Week updated", true, func(c *config.Config) error {
			c.Schedule = sched
			c.ActiveSchedule = ""
			return nil
		}, nil)
	case editRename:
		from := e.target
		return d.mutateConfig(fmt.Sprintf("Renamed to %q", name), false, func(c *config.Config) error {
			return c.RenamePreset(from, name)
		}, func() { d.selectPreset(name) })
	}
	return nil
}

func (d *Dashboard) selectPreset(name string) {
	for i, r := range d.schedRows() {
		if r.name == name {
			d.schedCursor = i
		}
	}
}

// configSavedMsg reports a finished config change.
type configSavedMsg struct {
	cfg   *config.Config
	toast string
	sync  bool
	after func()
}

// mutateConfig applies a change to a fresh copy of the config, saves it and,
// when the signing schedule changed and a GitHub backup exists, syncs it.
func (d *Dashboard) mutateConfig(toast string, affectsSigning bool, change func(*config.Config) error, after func()) tea.Cmd {
	return d.mutateConfigFn(func() string { return toast }, affectsSigning, change, after)
}

// mutateConfigFn is mutateConfig with a message computed after the change.
func (d *Dashboard) mutateConfigFn(toast func() string, affectsSigning bool, change func(*config.Config) error, after func()) tea.Cmd {
	cfg, err := config.Load()
	if err != nil {
		return d.showToast("Couldn't load settings: "+err.Error(), toastErr)
	}
	if err := change(cfg); err != nil {
		return d.showToast(err.Error(), toastErr)
	}
	if err := config.Save(cfg); err != nil {
		return d.showToast("Couldn't save: "+err.Error(), toastErr)
	}
	msg := configSavedMsg{cfg: cfg, toast: toast(), sync: affectsSigning, after: after}
	return func() tea.Msg { return msg }
}

func (d *Dashboard) onConfigSaved(msg configSavedMsg) tea.Cmd {
	d.applyConfig(msg.cfg)
	if msg.after != nil {
		msg.after()
	}
	if msg.sync && d.hasFork() {
		return tea.Batch(d.showToast(msg.toast+" · updating GitHub…", toastOK), d.syncGitHub())
	}
	return d.showToast(msg.toast, toastOK)
}

// ── Actions ──

func (d *Dashboard) schedUse() tea.Cmd {
	r := d.schedSelected()
	switch {
	case r.create:
		return d.schedNew()
	case r.current:
		return d.openEditor(editNew, "", "", config.ScheduleText(d.cfg.Schedule))
	case r.name == d.cfg.ActiveSchedule:
		return d.showToast(fmt.Sprintf("%q is already in use", r.name), toastInfo)
	}
	name := r.name
	var scope string
	return d.mutateConfigFn(func() string {
		msg := fmt.Sprintf("Now using %q", name)
		if scope != "" {
			msg += " " + scope
		}
		return msg
	}, true, func(c *config.Config) error {
		var err error
		scope, err = c.UsePreset(name, d.clock())
		return err
	}, nil)
}

func (d *Dashboard) schedEdit() tea.Cmd {
	r := d.schedSelected()
	switch {
	case r.create:
		return d.schedNew()
	case r.current:
		return d.openEditor(editCurrent, "", "", config.ScheduleText(d.cfg.Schedule))
	}
	return d.openEditor(editPreset, r.name, r.name, config.ScheduleText(d.cfg.SavedSchedules[r.name]))
}

// schedTemplates are the starting points offered for a new schedule.
var schedTemplates = []struct{ name, text string }{
	{"Split day, short Friday", "mon-thu 08:30-13:30 14:15-17:30, fri 08:00-15:00"},
	{"Split day all week", "mon-fri 09:00-14:00 15:00-18:00"},
	{"Continuous (intensiva)", "mon-fri 08:00-15:00"},
	{"Early shift", "mon-fri 07:00-15:00"},
	{"Four days", "mon-thu 08:00-18:00"},
}

func (d *Dashboard) schedNew() tea.Cmd {
	var items []action
	items = append(items, action{key: "tpl:current", section: "Start from", title: "Your current week", hint: config.ScheduleText(d.cfg.Schedule)})
	for i, t := range schedTemplates {
		items = append(items, action{key: fmt.Sprintf("tpl:%d", i), section: "Start from", title: t.name, hint: t.text})
	}
	items = append(items, action{key: "tpl:blank", section: "Start from", title: "Blank", hint: "write it from scratch"})
	d.openMenu("New schedule", items, func(key string) tea.Cmd {
		text := ""
		switch {
		case key == "tpl:current":
			text = config.ScheduleText(d.cfg.Schedule)
		case key == "tpl:blank":
		default:
			var i int
			fmt.Sscanf(strings.TrimPrefix(key, "tpl:"), "%d", &i)
			text = schedTemplates[i].text
		}
		return d.openEditor(editNew, "", "", text)
	})
	return nil
}

func (d *Dashboard) schedCopy() tea.Cmd {
	r := d.schedSelected()
	if r.create {
		return nil
	}
	base := r.name
	if r.current {
		base = "my week"
	}
	name := base + " copy"
	for i := 2; d.cfg.SavedSchedules[name].Monday.Times != nil || d.cfg.SavedSchedules[name].Friday.Times != nil; i++ {
		name = fmt.Sprintf("%s copy %d", base, i)
	}
	return d.openEditor(editNew, "", name, config.ScheduleText(d.schedOf(r)))
}

func (d *Dashboard) schedRename() tea.Cmd {
	r := d.schedSelected()
	if r.name == "" {
		return d.showToast("Save this week as a preset first (⏎)", toastInfo)
	}
	return d.openEditor(editRename, r.name, r.name, "")
}

func (d *Dashboard) schedDelete() tea.Cmd {
	r := d.schedSelected()
	if r.name == "" {
		return nil
	}
	if d.cfg.PresetInSeasons(r.name) {
		return d.showToast(fmt.Sprintf("%q is used by your seasonal switch — change that first (S)", r.name), toastErr)
	}
	name := r.name
	lines := []string{sFaint.Render(config.ScheduleText(d.cfg.SavedSchedules[name]))}
	if name == d.cfg.ActiveSchedule {
		lines = append(lines, "", sText.Render("It's in use: your week stays the same, it just won't have a name."))
	}
	d.openConfirm(&confirmSpec{
		title: "Delete preset", subject: "Delete “" + name + "”", lines: lines, yes: "Delete", accent: cBad,
		onYes: func() tea.Cmd {
			return d.mutateConfig(fmt.Sprintf("Deleted %q", name), false, func(c *config.Config) error {
				delete(c.SavedSchedules, name)
				if c.ActiveSchedule == name {
					c.ActiveSchedule = ""
				}
				return nil
			}, nil)
		},
	})
	return nil
}

// schedSeasons opens the summer-hours menu.
func (d *Dashboard) schedSeasons() tea.Cmd {
	var items []action
	cur := ""
	if p := d.cfg.Seasons.Periods; len(p) > 0 {
		cur = p[0].Preset
		items = append(items, action{key: "season:dates", section: "Summer hours", title: "Change dates", hint: dayMonthTUI(p[0].From) + " → " + dayMonthTUI(p[0].To)})
		items = append(items, action{key: "season:off", section: "Summer hours", title: "Turn off", hint: "keep one schedule all year"})
	}
	for _, n := range d.cfg.SchedulePresetNames() {
		a := action{key: "season:use:" + n, section: "Use for summer", title: n, hint: config.ScheduleText(d.cfg.SavedSchedules[n])}
		if n == cur {
			a.current, a.hint = true, "in use for summer"
		}
		items = append(items, a)
	}
	if len(d.cfg.SavedSchedules) == 0 {
		items = append(items, action{key: "none", section: "Use for summer", title: "Create a summer preset first (n)", disabled: true})
	}
	d.openMenu("Summer hours", items, func(key string) tea.Cmd {
		switch {
		case key == "season:off":
			return d.mutateConfig("Summer hours off", true, func(c *config.Config) error {
				if def := c.Seasons.Default; def != "" {
					c.UsePreset(def, d.clock())
				}
				c.Seasons = config.Seasons{}
				return nil
			}, nil)
		case key == "season:dates":
			p := d.cfg.Seasons.Periods[0]
			return d.openDates(p.From, p.To, p.Preset)
		case strings.HasPrefix(key, "season:use:"):
			name := strings.TrimPrefix(key, "season:use:")
			from, to := "07-01", "08-31"
			if p := d.cfg.Seasons.Periods; len(p) > 0 {
				from, to = p[0].From, p[0].To
			}
			return d.openDates(from, to, name)
		}
		return nil
	})
	return nil
}

// openDates asks for the summer period (reusing the editor's two inputs).
func (d *Dashboard) openDates(from, to, preset string) tea.Cmd {
	d.datesPreset = preset
	cmd := d.openEditor(editRename, "", dayMonthTUI(from), "")
	d.editor.text = newInput("31/8", dayMonthTUI(to), 12)
	d.editor.name.Placeholder = "1/7"
	d.editor.name.Width = 12
	d.overlay = overlayDates
	return cmd
}

func (d *Dashboard) keyDates(msg tea.KeyMsg) tea.Cmd {
	e := d.editor
	switch msg.String() {
	case "esc":
		d.overlay, d.editor = overlayNone, nil
		return nil
	case "tab", "shift+tab", "up", "down":
		e.focus = 1 - e.focus
		if e.focus == 0 {
			e.text.Blur()
			return e.name.Focus()
		}
		e.name.Blur()
		return e.text.Focus()
	case "enter":
		if e.focus == 0 {
			e.focus = 1
			e.name.Blur()
			return e.text.Focus()
		}
		from, err1 := config.NormalizeSeasonDate(e.name.Value())
		to, err2 := config.NormalizeSeasonDate(e.text.Value())
		if err1 != nil || err2 != nil {
			return d.showToast("Dates look like 1/7 (day/month)", toastErr)
		}
		preset := d.datesPreset
		d.overlay, d.editor = overlayNone, nil
		return d.mutateConfig(fmt.Sprintf("Summer: %q from %s to %s", preset, dayMonthTUI(from), dayMonthTUI(to)), true, func(c *config.Config) error {
			def := c.Seasons.Default
			if def == "" || def == preset {
				def = c.ActiveSchedule
			}
			if def == "" || def == preset {
				def = "regular"
				if err := c.SaveSchedulePreset(def, c.Schedule); err != nil {
					return err
				}
			}
			c.Seasons = config.Seasons{Default: def, Periods: []config.Season{{Preset: preset, From: from, To: to}}}
			c.ApplySeasons(d.clock())
			return nil
		}, nil)
	}
	var cmd tea.Cmd
	if e.focus == 0 {
		e.name, cmd = e.name.Update(msg)
	} else {
		e.text, cmd = e.text.Update(msg)
	}
	return cmd
}

func (d *Dashboard) schedTiming() tea.Cmd {
	cur := d.cfg.Timing.PresetKey()
	if !d.cfg.Timing.Active() {
		cur = "exact"
	}
	var items []action
	for _, p := range timing.Presets {
		a := action{key: p.Key, section: "Natural timing", title: p.Name, hint: p.Description}
		if p.Key == cur {
			a.current = true
		}
		items = append(items, a)
	}
	d.openMenu("How precise should signs be?", items, func(key string) tea.Cmd { return d.applyTiming(key) })
	return nil
}

// ── Generic menu overlay ──

func (d *Dashboard) openMenu(title string, items []action, onSelect func(string) tea.Cmd) {
	d.menu = &menuSpec{title: title, items: items, onSelect: onSelect}
	d.cursor = firstEnabled(items)
	d.overlay = overlayMenu
}

type menuSpec struct {
	title    string
	items    []action
	onSelect func(string) tea.Cmd
}

func (d *Dashboard) keyMenu(key string) tea.Cmd {
	m := d.menu
	switch key {
	case "esc", "q":
		d.overlay, d.menu = overlayNone, nil
	case "up", "k", "shift+tab":
		d.cursor = moveCursor(m.items, d.cursor, -1)
	case "down", "j", "tab":
		d.cursor = moveCursor(m.items, d.cursor, 1)
	case "enter":
		if d.cursor < len(m.items) && m.items[d.cursor].enabled() {
			d.overlay, d.menu = overlayNone, nil
			return m.onSelect(m.items[d.cursor].key)
		}
	}
	return nil
}

func (d *Dashboard) renderMenu(h int) string {
	w := min(72, d.width-6)
	return overlayBox(sBold.Render(d.menu.title)+"\n\n"+menuList(d.menu.items, d.cursor, w-4, max(4, h-8), true), cBrand, w)
}

// ── Keys for the tab ──

func (d *Dashboard) keySchedule(key string) (tea.Cmd, bool) {
	rows := d.schedRows()
	switch key {
	case "up", "k":
		d.schedCursor = max(0, d.schedCursor-1)
	case "down", "j":
		d.schedCursor = min(len(rows)-1, d.schedCursor+1)
	case "enter":
		return d.schedUse(), true
	case "e":
		return d.schedEdit(), true
	case "n":
		return d.schedNew(), true
	case "c":
		return d.schedCopy(), true
	case "R":
		return d.schedRename(), true
	case "x", "delete", "backspace":
		return d.schedDelete(), true
	case "S":
		return d.schedSeasons(), true
	case "t":
		return d.schedTiming(), true
	default:
		return nil, false
	}
	return nil, true
}

// ── Rendering ──

func (d *Dashboard) renderSchedule(h int) string {
	w := d.contentWidth()
	listW := 34
	if w < 90 {
		return d.renderScheduleList(w) + "\n\n" + d.renderScheduleDetail(w)
	}
	return twoColumns(d.renderScheduleList(listW), d.renderScheduleDetail(w-listW-4), listW, 4)
}

func (d *Dashboard) renderScheduleList(w int) string {
	lines := []string{label("Schedules")}
	for i, r := range d.schedRows() {
		var left, right string
		switch {
		case r.create:
			left = sBrand.Render("+ New schedule")
		case r.current:
			left = sWarn.Render("● ") + sText.Render("Current week")
			right = sFaint.Render("unsaved")
		default:
			mark := sFaint.Render("○ ")
			if r.name == d.cfg.ActiveSchedule {
				mark = sOK.Render("● ")
			}
			left = mark + sText.Render(truncate(r.name, w-16))
			right = sFaint.Render(config.FormatMinutes(d.cfg.SavedSchedules[r.name].WeekMinutes()))
			for _, p := range d.cfg.Seasons.Periods {
				if p.Preset == r.name {
					right = fg(cTimeOff).Render("☀ ") + right
				}
			}
		}
		row := spread(left, right, w-2)
		if i == d.schedCursor {
			row = sBrand.Render("▌") + " " + highlightRow(stripANSI(left), stripANSI(right), w-2)
		} else {
			row = "  " + row
		}
		lines = append(lines, row)
	}
	return strings.Join(lines, "\n")
}

func (d *Dashboard) renderScheduleDetail(w int) string {
	r := d.schedSelected()
	s := d.schedOf(r)
	var title string
	switch {
	case r.create:
		title = sBrand.Render("New schedule")
	case r.current:
		title = sBold.Render("Current week") + sFaint.Render("  not saved as a preset")
	default:
		title = sBold.Render(r.name)
		if r.name == d.cfg.ActiveSchedule {
			title += sOK.Render("  ● in use")
		}
	}
	var b []string
	if r.create {
		b = append(b, title, "", sSubtle.Render("Start from a template or your current week,"), sSubtle.Render("then adjust it as text with a live preview."), "", keycap("⏎", "create"))
	} else {
		b = append(b, title, "", weekGraph(s, w), "", sFaint.Render(truncate(config.ScheduleText(s), w)))
	}

	// Summer hours
	b = append(b, "", label("Summer hours"))
	if p := d.cfg.Seasons.Periods; len(p) > 0 {
		b = append(b, fg(cTimeOff).Render("☀ ")+sText.Render(p[0].Preset)+sFaint.Render(fmt.Sprintf("  %s → %s · otherwise %s", dayMonthTUI(p[0].From), dayMonthTUI(p[0].To), orDefault(d.cfg.Seasons.Default, "current"))))
		if when, preset, ok := d.cfg.NextSeasonChange(d.clock()); ok {
			b = append(b, sFaint.Render("next switch: "+preset+" on "+when.Format("Mon 2 Jan 2006")))
		}
	} else {
		b = append(b, sFaint.Render("Off — press ")+sKey.Render("S")+sFaint.Render(" to switch automatically in summer"))
	}

	// Natural timing
	b = append(b, "", label("Natural timing"))
	if d.cfg.Timing.Active() {
		b = append(b, sText.Render("natural")+sFaint.Render(" · "+d.cfg.Timing.Short()))
	} else {
		b = append(b, sText.Render("exact minute"))
	}
	if prev := d.timingPreviewTUI(3); prev != "" {
		b = append(b, prev)
	}
	return strings.Join(b, "\n")
}

// weekGraph draws the week on a 06–21 axis, one row per weekday.
func weekGraph(s config.Schedule, w int) string {
	const from, to = 6 * 60, 21 * 60
	barW := max(20, min(48, w-14))
	col := func(m int) int { return (m - from) * barW / (to - from) }
	ticks := []rune(strings.Repeat(" ", barW+2))
	for h := 6; h <= 21; h += 3 {
		c := min(col(h*60), barW)
		copy(ticks[c:], []rune(fmt.Sprintf("%02d", h)))
	}
	lines := []string{"     " + sGhost.Render(string(ticks))}
	for _, wd := range []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday} {
		day := s.Day(wd)
		cells := make([]bool, barW)
		for i := 0; i+1 < len(day.Times) && day.Enabled; i += 2 {
			a, _ := minuteOf(day.Times[i].Time)
			b, _ := minuteOf(day.Times[i+1].Time)
			for c := max(0, col(a)); c < min(barW, col(b)); c++ {
				cells[c] = true
			}
		}
		var bar strings.Builder
		for _, on := range cells {
			if on {
				bar.WriteString(fg(cBrand).Render("█"))
			} else {
				bar.WriteString(sGhost.Render("·"))
			}
		}
		hours := sFaint.Render("off")
		if day.Enabled {
			hours = sSubtle.Render(config.FormatMinutes(day.DayMinutes()))
		}
		lines = append(lines, sText.Render(wd.String()[:3])+"  "+bar.String()+"  "+hours)
	}
	lines = append(lines, sFaint.Render("week ")+sBold.Render(config.FormatMinutes(s.WeekMinutes())))
	return strings.Join(lines, "\n")
}

// timingPreviewTUI lists the next working days' sign moments.
func (d *Dashboard) timingPreviewTUI(n int) string {
	now := d.clock()
	var rows []string
	for i := 1; len(rows) < n && i < 14; i++ {
		date := now.AddDate(0, 0, i)
		probe := *d.cfg
		probe.ApplySeasons(date)
		day := probe.Schedule.Day(date.Weekday())
		if !day.Enabled {
			continue
		}
		// Skip holidays and days off we know about from Woffu.
		if cal := d.calendarDayFor(date); cal != nil && cal.Status != "working" {
			continue
		}
		var ev []timing.Event
		for j, e := range day.Times {
			m, _ := minuteOf(e.Time)
			ev = append(ev, timing.Event{Minute: m, In: j%2 == 0})
		}
		var parts []string
		for j, t := range d.cfg.Timing.Targets(date, ev) {
			st := sOK
			if !ev[j].In {
				st = sWarn
			}
			parts = append(parts, st.Render(t.Format("15:04")))
		}
		rows = append(rows, sFaint.Render(date.Format("Mon 02")+"  ")+strings.Join(parts, "  "))
	}
	return strings.Join(rows, "\n")
}

// renderEditor draws the schedule editor with a live preview.
func (d *Dashboard) renderEditor() string {
	e := d.editor
	w := min(76, d.width-6)
	field := func(title string, ti textinput.Model, focused bool) string {
		border := cLine
		if focused {
			border = cBrand
		}
		return sFaint.Render(title) + "\n" + lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(border).
			Padding(0, 1).Width(w-8).Render(ti.View())
	}
	var parts []string
	switch e.mode {
	case editNew:
		parts = append(parts, sBold.Render("New schedule"), "", field("Name", e.name, e.focus == 0), field("Week", e.text, e.focus == 1))
	case editPreset:
		parts = append(parts, sBold.Render("Edit “"+e.target+"”"), "", field("Week", e.text, true))
	case editCurrent:
		parts = append(parts, sBold.Render("Edit your week"), "", field("Week", e.text, true))
	case editRename:
		parts = append(parts, sBold.Render("Rename “"+e.target+"”"), "", field("New name", e.name, true))
	}
	if e.mode != editRename {
		s, err := config.ParseScheduleText(e.text.Value())
		switch {
		case strings.TrimSpace(e.text.Value()) == "":
			parts = append(parts, "", sFaint.Render("Days, then blocks:  mon-thu 8:30-13:30 14:15-17:30, fri 8-15"),
				sFaint.Render("Spanish letters work too: L M X J V · days you leave out are off"))
		case err != nil:
			parts = append(parts, "", sBad.Render("✗ "+err.Error()))
		default:
			parts = append(parts, "", weekGraph(s, w-6))
		}
	}
	hint := keycap("⏎", "save") + "   " + keycap("esc", "cancel")
	if e.mode == editNew {
		hint = keycap("tab", "switch field") + "   " + hint
	}
	parts = append(parts, "", hint)
	return overlayBox(strings.Join(parts, "\n"), cBrand, w)
}

func (d *Dashboard) renderDates() string {
	e := d.editor
	w := min(56, d.width-6)
	box := func(ti textinput.Model, focused bool) string {
		border := cLine
		if focused {
			border = cBrand
		}
		return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(border).Padding(0, 1).Width(14).Render(ti.View())
	}
	body := sBold.Render("Summer hours: “"+d.datesPreset+"”") + "\n\n" +
		sSubtle.Render("Every year between these dates (day/month):") + "\n\n" +
		lipgloss.JoinHorizontal(lipgloss.Center, sFaint.Render("from  "), box(e.name, e.focus == 0), sFaint.Render("   to  "), box(e.text, e.focus == 1)) +
		"\n\n" + keycap("tab", "switch") + "   " + keycap("⏎", "save") + "   " + keycap("esc", "cancel")
	return overlayBox(body, cTimeOff, w)
}

func dayMonthTUI(mmdd string) string {
	t, err := time.Parse("01-02", mmdd)
	if err != nil {
		return mmdd
	}
	return fmt.Sprintf("%d/%d", t.Day(), int(t.Month()))
}

// calendarDayFor finds a date in the loaded calendar months.
func (d *Dashboard) calendarDayFor(date time.Time) *woffu.CalendarDay {
	key := date.Format("2006-01-02")
	for i := range d.homeDays {
		if d.homeDays[i].Date == key {
			return &d.homeDays[i]
		}
	}
	if d.cal != nil {
		return d.cal.dayInfoByDate(key)
	}
	return nil
}
