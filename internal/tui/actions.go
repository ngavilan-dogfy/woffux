package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ngavilan-dogfy/woffux/internal/woffu"
)

// action is one entry of the command palette or a context menu.
type action struct {
	key      string // what runAction / runDayAction dispatch on
	section  string // group heading
	title    string
	hint     string // secondary text, right after the title
	shortcut string // direct key, shown right-aligned
	disabled bool   // shown but not runnable (with hint saying why)
	current  bool   // e.g. the schedule already in use
}

func (a action) enabled() bool { return !a.disabled && !a.current }

// ── Command palette ──

func (d *Dashboard) getActions() []action {
	var out []action
	add := func(a action) { out = append(out, a) }

	// Today
	dir := d.pendingSignAction()
	add(action{key: "sign", section: "Today", title: "Clock " + dir + " now", hint: "asks before sending", shortcut: "s"})
	add(action{key: "refresh", section: "Today", title: "Refresh", hint: "reload from Woffu", shortcut: "r"})

	// Autopilot
	if agentAvailable() {
		if d.agentOn() {
			add(action{key: "toggle-agent", section: "Autopilot", title: "Stop signing from this Mac", hint: "GitHub stays as backup", shortcut: "m"})
		} else {
			add(action{key: "toggle-agent", section: "Autopilot", title: "Sign from this Mac", hint: "on time while it's awake", shortcut: "m"})
		}
	}
	switch {
	case !d.hasFork():
		add(action{key: "toggle-github", section: "Autopilot", title: "GitHub backup signer", hint: "run woffux setup first", disabled: true})
	case d.autoActive == nil:
		add(action{key: "toggle-github", section: "Autopilot", title: "GitHub backup signer", hint: "checking status…", disabled: true})
	case d.githubOn():
		add(action{key: "toggle-github", section: "Autopilot", title: "Disable GitHub backup signer", hint: d.cfg.GithubFork, shortcut: "a"})
	default:
		add(action{key: "toggle-github", section: "Autopilot", title: "Enable GitHub backup signer", hint: d.cfg.GithubFork, shortcut: "a"})
	}
	if d.hasFork() {
		title, hint := "Sync to GitHub", "push secrets + workflows"
		if d.needsAutoSync() {
			title, hint = "Fix GitHub sync", "schedule on GitHub is outdated"
		}
		add(action{key: "sync", section: "Autopilot", title: title, hint: hint})
	}

	// Schedule
	names := d.cfg.SchedulePresetNames()
	sort.Strings(names)
	for _, name := range names {
		a := action{key: "preset:" + name, section: "Schedule", title: "Use " + name, hint: "switch schedule preset"}
		if name == d.cfg.ActiveSchedule {
			a.title, a.hint, a.current = name, "in use", true
		}
		add(a)
	}
	add(action{key: "edit-schedule", section: "Schedule", title: "Edit schedule times", hint: "opens the editor", shortcut: "e"})
	add(action{key: "save-preset", section: "Schedule", title: "Save schedule as…", hint: "keep it as a preset"})

	// Go to
	add(action{key: "tab:today", section: "Go to", title: "Today", shortcut: "1"})
	add(action{key: "tab:calendar", section: "Go to", title: "Calendar", hint: "requests, telework, vacation", shortcut: "2"})
	add(action{key: "tab:balance", section: "Go to", title: "Balance", hint: "days and hours left", shortcut: "3"})
	add(action{key: "open-woffu", section: "Go to", title: "Open Woffu in browser", shortcut: "o", disabled: strings.TrimSpace(d.cfg.WoffuCompanyURL) == ""})
	gh := action{key: "open-github", section: "Go to", title: "Open GitHub Actions", shortcut: "g"}
	if !d.hasFork() {
		gh.disabled, gh.hint = true, "run woffux setup first"
	}
	add(gh)

	// App
	add(action{key: "edit-config", section: "App", title: "Settings", hint: "credentials, locations, Telegram"})
	add(action{key: "help", section: "App", title: "Keyboard shortcuts", shortcut: "?"})
	add(action{key: "quit", section: "App", title: "Quit", shortcut: "q"})
	return out
}

// filteredActions applies the palette query: every word must match the
// title, hint or section (case-insensitive).
func (d *Dashboard) filteredActions() []action {
	all := d.getActions()
	q := strings.Fields(strings.ToLower(d.query))
	if len(q) == 0 {
		return all
	}
	var out []action
	for _, a := range all {
		hay := strings.ToLower(a.title + " " + a.hint + " " + a.section + " " + a.key)
		ok := true
		for _, w := range q {
			if !strings.Contains(hay, w) {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, a)
		}
	}
	return out
}

func (d *Dashboard) runAction(key string) tea.Cmd {
	switch key {
	case "toggle-agent", "toggle-github", "sync":
		if cmd, busy := d.guardBusy(); busy {
			return cmd
		}
	default:
		if strings.HasPrefix(key, "preset:") {
			if cmd, busy := d.guardBusy(); busy {
				return cmd
			}
		}
	}
	switch key {
	case "sign":
		return d.askSign()
	case "refresh":
		return d.refreshData()
	case "toggle-agent":
		if !agentAvailable() {
			return d.showToast("The local agent is only available on macOS", toastErr)
		}
		return d.toggleAgent(!d.agentOn())
	case "toggle-github":
		if !d.hasFork() {
			return d.showToast("GitHub isn't set up — run woffux setup", toastErr)
		}
		if d.autoActive == nil {
			return d.showToast("Still checking GitHub — try again in a moment", toastInfo)
		}
		return d.toggleAuto(!*d.autoActive)
	case "sync":
		return d.syncGitHub()
	case "edit-schedule":
		return d.execWoffux("Schedule", "schedule", "edit")
	case "edit-config":
		return d.execWoffux("Settings", "config", "edit")
	case "save-preset":
		d.input = ""
		d.overlay = overlayInput
		return nil
	case "tab:today":
		d.switchTab(tabToday)
	case "tab:calendar":
		d.switchTab(tabCalendar)
	case "tab:balance":
		d.switchTab(tabBalance)
	case "open-woffu":
		return d.openWoffu()
	case "open-github":
		return d.openGitHub()
	case "help":
		d.overlay = overlayHelp
	case "quit":
		return tea.Quit
	default:
		if name, ok := strings.CutPrefix(key, "preset:"); ok {
			if name == d.cfg.ActiveSchedule {
				return nil
			}
			return d.applyPreset(name)
		}
	}
	return nil
}

// ── Calendar context menu ──

// targetDates is what calendar actions apply to: the selection when there
// is one, otherwise the day under the cursor.
func (d *Dashboard) targetDates() []string {
	if d.cal == nil {
		return nil
	}
	if len(d.cal.selected) > 0 {
		return d.cal.selectedDates()
	}
	return []string{d.cal.cursorDate()}
}

// activeRequests collects pending/approved requests on the target days.
func (d *Dashboard) activeRequests(dates []string) (ids []int, byStatus map[string]int) {
	byStatus = map[string]int{}
	seen := map[int]bool{}
	for _, date := range dates {
		info := d.cal.dayInfoByDate(date)
		if info == nil {
			continue
		}
		for _, r := range info.Requests {
			if (r.Status == "pending" || r.Status == "approved") && !seen[r.RequestID] {
				seen[r.RequestID] = true
				ids = append(ids, r.RequestID)
				byStatus[r.Status]++
			}
		}
	}
	return ids, byStatus
}

func (d *Dashboard) dayActions() []action {
	if d.cal == nil {
		return nil
	}
	dates := d.targetDates()
	eligible := d.cal.eligibleDates(dates)
	ids, byStatus := d.activeRequests(dates)

	var out []action
	if len(eligible) == 0 {
		out = append(out, action{key: "none", section: "Request", title: "Nothing to request",
			hint: d.whyNotRequestable(dates), disabled: true})
	}
	for _, k := range requestKinds {
		if len(eligible) == 0 {
			break
		}
		a := action{key: "req:" + k.key, section: "Request", title: k.name, shortcut: k.hotkey}
		switch {
		case len(dates) > 1 || len(eligible) != len(dates):
			a.hint = fmt.Sprintf("%d %s", len(eligible), plural(len(eligible), "day", "days"))
			if skipped := len(dates) - len(eligible); skipped > 0 {
				a.hint += fmt.Sprintf(" · %d skipped", skipped)
			}
		}
		out = append(out, a)
	}

	if len(ids) > 0 {
		var parts []string
		if n := byStatus["pending"]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d pending", n))
		}
		if n := byStatus["approved"]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d approved", n))
		}
		out = append(out, action{key: "cancel-all", section: "Existing requests",
			title: fmt.Sprintf("Cancel %d %s", len(ids), plural(len(ids), "request", "requests")),
			hint:  strings.Join(parts, ", "), shortcut: "c"})
	}

	if len(d.cal.selected) > 0 {
		out = append(out, action{key: "clear", section: "Selection", title: "Clear selection", shortcut: "x"})
	}
	return out
}

// whyNotRequestable explains in a few words why no request fits.
func (d *Dashboard) whyNotRequestable(dates []string) string {
	if len(dates) != 1 {
		return "no working days selected"
	}
	info := d.cal.dayInfoByDate(dates[0])
	switch {
	case info == nil:
		return "weekend"
	case hasActiveRequest(info):
		return "already requested"
	case info.Status == "weekend":
		return "weekend"
	case info.Status == "holiday":
		return "public holiday"
	}
	return "not a working day"
}

func (d *Dashboard) runDayAction(key string) tea.Cmd {
	if d.cal == nil {
		return nil
	}
	if key != "clear" {
		if cmd, busy := d.guardBusy(); busy {
			return cmd
		}
		if !d.cal.loaded {
			return d.showToast("Still loading "+d.cal.month.String()+" — one moment", toastInfo)
		}
	}
	dates := d.targetDates()

	switch {
	case key == "clear":
		d.cal.clearSelection()
		return nil

	case key == "cancel-all":
		ids, byStatus := d.activeRequests(dates)
		if len(ids) == 0 {
			return d.showToast("No pending or approved requests on "+describeDates(dates), toastInfo)
		}
		lines := []string{sText.Render(describeDates(dates))}
		if byStatus["approved"] > 0 {
			lines = append(lines, "", sWarn.Render(fmt.Sprintf("%d already approved", byStatus["approved"]))+
				sFaint.Render(" — your manager will see the cancellation."))
		}
		d.openConfirm(&confirmSpec{
			title:   "Cancel requests",
			subject: fmt.Sprintf("Cancel %d %s", len(ids), plural(len(ids), "request", "requests")),
			lines:   lines,
			yes:     "Cancel them",
			danger:  byStatus["approved"] > 0,
			accent:  cBad,
			onYes:   func() tea.Cmd { return d.cancelRequests(ids) },
		})
		return nil

	case strings.HasPrefix(key, "req:"):
		kind, ok := findRequestKind(strings.TrimPrefix(key, "req:"))
		if !ok {
			return nil
		}
		eligible := d.cal.eligibleDates(dates)
		if len(eligible) == 0 {
			return d.showToast(describeDates(dates)+" can't be requested (weekend, holiday or already requested)", toastInfo)
		}
		lines := []string{sText.Render(describeDates(eligible))}
		if skipped := len(dates) - len(eligible); skipped > 0 {
			lines = append(lines, sFaint.Render(fmt.Sprintf("%d %s skipped: weekends, holidays or already requested", skipped, plural(skipped, "day", "days"))))
		}
		if bal := d.balanceFor(kind); bal != "" {
			lines = append(lines, "", sFaint.Render("Balance: ")+sText.Render(bal))
		}
		lines = append(lines, "", sFaint.Render("Your manager gets a request in Woffu."))
		d.openConfirm(&confirmSpec{
			title:   "New request",
			subject: fmt.Sprintf("%s · %d %s", kind.name, len(eligible), plural(len(eligible), "day", "days")),
			lines:   lines,
			yes:     "Send request",
			accent:  kindAccent(kind.key),
			onYes:   func() tea.Cmd { return d.submitRequests(kind, eligible) },
		})
		return nil
	}
	return nil
}

// balanceFor finds the matching balance line for a request kind.
func (d *Dashboard) balanceFor(kind requestKind) string {
	for _, e := range d.events {
		if strings.Contains(strings.ToLower(e.Name), kind.search) {
			return formatAmount(e.Available, e.Unit) + " left"
		}
	}
	return ""
}

func kindAccent(key string) lipgloss.Color {
	switch key {
	case "telework":
		return cRemote
	case "vacation", "personal":
		return cTimeOff
	}
	return cBrand
}

// describeDates renders a compact, human list of dates: "Mon 28 Sep",
// "Mon 28 – Wed 30 Sep", or "5 days: Mon 28 Sep, …".
func describeDates(dates []string) string {
	var ts []time.Time
	for _, s := range dates {
		if t, err := time.Parse("2006-01-02", s); err == nil {
			ts = append(ts, t)
		}
	}
	sort.Slice(ts, func(i, j int) bool { return ts[i].Before(ts[j]) })
	switch len(ts) {
	case 0:
		return "no days"
	case 1:
		return ts[0].Format("Mon 2 Jan 2006")
	}
	contiguous := true
	for i := 1; i < len(ts); i++ {
		if ts[i].Sub(ts[i-1]) != 24*time.Hour {
			contiguous = false
			break
		}
	}
	first, last := ts[0], ts[len(ts)-1]
	if contiguous {
		if first.Month() == last.Month() {
			return first.Format("Mon 2") + " – " + last.Format("Mon 2 Jan")
		}
		return first.Format("Mon 2 Jan") + " – " + last.Format("Mon 2 Jan")
	}
	var parts []string
	for i, t := range ts {
		if i == 4 {
			parts = append(parts, fmt.Sprintf("+%d more", len(ts)-4))
			break
		}
		parts = append(parts, t.Format("Mon 2 Jan"))
	}
	return strings.Join(parts, ", ")
}

// formatAmount renders "6 days", "26 h", "12.5 h".
func formatAmount(v float64, unit string) string {
	num := fmt.Sprintf("%.1f", v)
	num = strings.TrimSuffix(num, ".0")
	switch strings.ToLower(unit) {
	case "hours", "hour", "h":
		return num + " h"
	case "days", "day", "d":
		if num == "1" {
			return "1 day"
		}
		return num + " days"
	}
	return strings.TrimSpace(num + " " + unit)
}

func modeLabel(m woffu.SignMode) string {
	if m == woffu.SignModeRemote {
		return "Remote"
	}
	return "Office"
}
