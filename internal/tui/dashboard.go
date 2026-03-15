package tui

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ngavilan-dogfy/woffuk-cli/internal/config"
	gh "github.com/ngavilan-dogfy/woffuk-cli/internal/github"
	"github.com/ngavilan-dogfy/woffuk-cli/internal/notify"
	"github.com/ngavilan-dogfy/woffuk-cli/internal/woffu"
)

// Messages
type dataMsg struct {
	signInfo *woffu.SignInfo
	events   []woffu.AvailableUserEvent
	profile  *woffu.UserProfile
}
type errMsg struct{ err error }
type signDoneMsg struct{}
type autoToggleMsg struct{ enabled bool }
type syncDoneMsg struct{}
type clearFlashMsg struct{}
type tickMsg time.Time

// Action items for the menu
type action struct {
	key  string
	name string
	desc string
}

// Dashboard is the main TUI model.
type Dashboard struct {
	client        *woffu.Client
	companyClient *woffu.Client
	cfg           *config.Config
	password      string

	// Data
	loading    bool
	token      string
	signInfo   *woffu.SignInfo
	events     []woffu.AvailableUserEvent
	profile    *woffu.UserProfile
	autoActive *bool // nil = unknown
	err        error

	// UI state
	signing    bool
	menuOpen   bool
	menuCursor int
	flash      string
	flashErr   bool

	// Layout
	width  int
	height int
}

func NewDashboard(client, companyClient *woffu.Client, cfg *config.Config, password string) *Dashboard {
	return &Dashboard{
		client:        client,
		companyClient: companyClient,
		cfg:           cfg,
		password:      password,
		loading:       true,
	}
}

func (d *Dashboard) Init() tea.Cmd {
	return tea.Batch(d.fetchData(), d.fetchAutoStatus(), d.tick())
}

func (d *Dashboard) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		d.width = msg.Width
		d.height = msg.Height

	case tea.KeyMsg:
		return d.handleKey(msg)

	case dataMsg:
		d.loading = false
		d.signInfo = msg.signInfo
		d.events = msg.events
		d.profile = msg.profile

	case signDoneMsg:
		d.signing = false
		d.setFlash("Signed successfully!", false)
		return d, tea.Batch(d.fetchData(), d.clearFlashAfter(3*time.Second))

	case autoToggleMsg:
		v := msg.enabled
		d.autoActive = &v
		if v {
			d.setFlash("Auto-signing enabled", false)
		} else {
			d.setFlash("Auto-signing disabled", false)
		}
		return d, d.clearFlashAfter(3*time.Second)

	case syncDoneMsg:
		d.setFlash("Synced to GitHub", false)
		return d, d.clearFlashAfter(3*time.Second)

	case errMsg:
		d.loading = false
		d.signing = false
		d.setFlash(msg.err.Error(), true)
		return d, d.clearFlashAfter(5*time.Second)

	case clearFlashMsg:
		d.flash = ""

	case tickMsg:
		return d, d.tick()
	}

	return d, nil
}

func (d *Dashboard) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Menu navigation
	if d.menuOpen {
		actions := d.getActions()
		switch key {
		case "esc", "a":
			d.menuOpen = false
		case "up", "k":
			if d.menuCursor > 0 {
				d.menuCursor--
			}
		case "down", "j":
			if d.menuCursor < len(actions)-1 {
				d.menuCursor++
			}
		case "enter":
			d.menuOpen = false
			return d, d.executeAction(actions[d.menuCursor])
		}
		return d, nil
	}

	// Global keys
	switch key {
	case "q", "ctrl+c":
		return d, tea.Quit
	case "a", "enter":
		d.menuOpen = true
		d.menuCursor = 0
	case "s":
		return d, d.trySign()
	case "r":
		d.loading = true
		d.flash = ""
		return d, tea.Batch(d.fetchData(), d.fetchAutoStatus())
	case "o":
		openBrowserCmd(d.cfg.WoffuCompanyURL + "/v2")
	}

	return d, nil
}

func (d *Dashboard) View() string {
	if d.width == 0 {
		return ""
	}

	var sections []string

	sections = append(sections, d.renderHeader())

	if d.loading {
		sections = append(sections, "\n"+sDimmed.Render("  Loading..."))
	} else {
		sections = append(sections, d.renderStatus())

		if evts := d.renderEvents(); evts != "" {
			sections = append(sections, evts)
		}
		if next := d.renderNextEvents(); next != "" {
			sections = append(sections, next)
		}
		sections = append(sections, d.renderSchedule())
		sections = append(sections, d.renderAutoSign())
	}

	if d.flash != "" {
		icon := sFlashSuccess.Render("  ✓ ")
		if d.flashErr {
			icon = sFlashError.Render("  ✗ ")
		}
		sections = append(sections, "\n"+icon+d.flash)
	}

	sections = append(sections, d.renderHelp())

	dashboard := strings.Join(sections, "\n")

	if d.menuOpen {
		return d.overlayMenu(dashboard)
	}

	return dashboard
}

// ── Render sections ──

func (d *Dashboard) renderHeader() string {
	name := "woffuk"
	if d.profile != nil {
		name = fmt.Sprintf("woffuk — %s", d.profile.FullName)
	}
	left := sTitle.Render(name)
	right := sDimmed.Render(time.Now().Format("15:04"))
	gap := d.width - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if gap < 0 {
		gap = 0
	}
	return lipgloss.NewStyle().Background(colorBarBg).Width(d.width).
		Render(left + strings.Repeat(" ", gap) + right)
}

func (d *Dashboard) renderStatus() string {
	if d.signInfo == nil {
		return ""
	}
	info := d.signInfo

	workingDay := sSuccess.Render("yes")
	if !info.IsWorkingDay {
		workingDay = sDanger.Render("no")
	}

	modeStyle := sDimmed
	if info.Mode == woffu.SignModeRemote {
		modeStyle = sSuccess
	}
	mode := modeStyle.Render(fmt.Sprintf("%s %s", info.Mode.Emoji(), info.Mode.Label()))

	rows := []string{
		sLabel.Render("Date") + sValue.Render(info.Date),
		sLabel.Render("Working day") + workingDay,
		sLabel.Render("Mode") + mode,
	}
	if info.IsWorkingDay {
		rows = append(rows, sLabel.Render("Coordinates")+sDimmed.Render(fmt.Sprintf("%.4f, %.4f", info.Latitude, info.Longitude)))
	}

	return "\n" + sBox.Render(strings.Join(rows, "\n"))
}

func (d *Dashboard) renderEvents() string {
	if len(d.events) == 0 {
		return ""
	}
	var rows []string
	for _, e := range d.events {
		name := lipgloss.NewStyle().Foreground(colorMuted).Width(40).Render(e.Name)
		val := sValue.Render(fmt.Sprintf("%6.0f %s", e.Available, e.Unit))
		rows = append(rows, "  "+name+val)
	}
	return "\n" + sSubtitle.Render("  Available events") + "\n" + strings.Join(rows, "\n")
}

func (d *Dashboard) renderNextEvents() string {
	if d.signInfo == nil || len(d.signInfo.NextEvents) == 0 {
		return ""
	}
	var rows []string
	limit := 5
	for i, e := range d.signInfo.NextEvents {
		if i >= limit {
			rows = append(rows, sDimmed.Render(fmt.Sprintf("    ... +%d more", len(d.signInfo.NextEvents)-limit)))
			break
		}
		name := ""
		if len(e.Names) > 0 {
			name = " " + e.Names[0]
		}
		rows = append(rows, "  "+sDimmed.Render("  "+e.Date)+name)
	}
	return "\n" + sSubtitle.Render("  Upcoming") + "\n" + strings.Join(rows, "\n")
}

func (d *Dashboard) renderSchedule() string {
	s := d.cfg.Schedule
	days := []struct {
		n string
		d config.DaySchedule
	}{{"Mon", s.Monday}, {"Tue", s.Tuesday}, {"Wed", s.Wednesday}, {"Thu", s.Thursday}, {"Fri", s.Friday}}

	var parts []string
	for _, dd := range days {
		if !dd.d.Enabled {
			continue
		}
		var times []string
		for i, t := range dd.d.Times {
			if i%2 == 0 {
				times = append(times, sSignIn.Render("▶")+t.Time)
			} else {
				times = append(times, sSignOut.Render("■")+t.Time)
			}
		}
		parts = append(parts, sDimmed.Render("  "+dd.n+" ")+strings.Join(times, " "))
	}
	return "\n" + sSubtitle.Render("  Schedule") + "\n" + strings.Join(parts, "\n")
}

func (d *Dashboard) renderAutoSign() string {
	status := sDimmed.Render("checking...")
	if d.autoActive != nil {
		if *d.autoActive {
			status = sSuccess.Render("active")
		} else {
			status = sDanger.Render("disabled")
		}
	} else if d.cfg.GithubFork == "" {
		status = sDimmed.Render("not set up")
	}
	return "\n" + sLabel.Render("  Auto-sign") + status
}

func (d *Dashboard) renderHelp() string {
	hints := []string{
		hint("a", "actions"),
		hint("s", "sign"),
		hint("r", "refresh"),
		hint("o", "woffu"),
		hint("q", "quit"),
	}
	return "\n" + sDimmed.Render("  ") + strings.Join(hints, "  ")
}

// ── Action menu ──

func (d *Dashboard) getActions() []action {
	actions := []action{
		{key: "sign", name: "Sign now", desc: "Clock in/out right now"},
	}

	if d.autoActive != nil {
		if *d.autoActive {
			actions = append(actions, action{key: "auto-off", name: "Disable auto-sign", desc: "Stop GitHub Actions from signing"})
		} else {
			actions = append(actions, action{key: "auto-on", name: "Enable auto-sign", desc: "Resume GitHub Actions signing"})
		}
	}

	// Add saved schedule switching
	if d.cfg.SavedSchedules != nil && len(d.cfg.SavedSchedules) > 0 {
		for name := range d.cfg.SavedSchedules {
			label := fmt.Sprintf("Switch to \"%s\" schedule", name)
			if name == d.cfg.ActiveSchedule {
				label += " (current)"
			}
			actions = append(actions, action{key: "preset:" + name, name: label, desc: "Apply saved schedule preset"})
		}
	}

	actions = append(actions,
		action{key: "sync", name: "Sync to GitHub", desc: "Push secrets + workflows to fork"},
		action{key: "open", name: "Open Woffu", desc: "Open Woffu in browser"},
		action{key: "open-gh", name: "Open GitHub fork", desc: "View fork and workflow runs"},
	)

	return actions
}

func (d *Dashboard) executeAction(a action) tea.Cmd {
	switch a.key {
	case "sign":
		return d.trySign()
	case "auto-on":
		return d.toggleAuto(true)
	case "auto-off":
		return d.toggleAuto(false)
	case "sync":
		return d.syncGitHub()
	case "open":
		openBrowserCmd(d.cfg.WoffuCompanyURL + "/v2")
	case "open-gh":
		if d.cfg.GithubFork != "" {
			openBrowserCmd("https://github.com/" + d.cfg.GithubFork + "/actions")
		}
	default:
		if strings.HasPrefix(a.key, "preset:") {
			name := strings.TrimPrefix(a.key, "preset:")
			return d.applyPreset(name)
		}
	}
	return nil
}

func (d *Dashboard) overlayMenu(bg string) string {
	actions := d.getActions()

	var rows []string
	for i, a := range actions {
		cursor := "  "
		style := sDimmed
		if i == d.menuCursor {
			cursor = sKey.Render("▸ ")
			style = sValue
		}
		name := style.Render(a.name)
		desc := sDimmed.Render("  " + a.desc)
		rows = append(rows, cursor+name+desc)
	}

	menuContent := sSection.Render("  Actions") + "\n\n" + strings.Join(rows, "\n") + "\n\n" +
		sDimmed.Render("  ↑↓ navigate  enter select  esc close")

	menuBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorPrimary).
		Padding(1, 2).
		Width(50).
		Render(menuContent)

	// Center the menu overlay
	return placeCenter(bg, menuBox, d.width, d.height)
}

func placeCenter(bg, overlay string, w, h int) string {
	bgLines := strings.Split(bg, "\n")
	ovLines := strings.Split(overlay, "\n")

	ovW := lipgloss.Width(overlay)
	startRow := (h - len(ovLines)) / 2
	startCol := (w - ovW) / 2
	if startRow < 0 {
		startRow = 0
	}
	if startCol < 0 {
		startCol = 0
	}

	// Pad bg to fill height
	for len(bgLines) < h {
		bgLines = append(bgLines, "")
	}

	for i, ovLine := range ovLines {
		row := startRow + i
		if row >= len(bgLines) {
			break
		}
		// Pad the bg line
		bgLine := bgLines[row]
		bgRunes := []rune(bgLine)
		for len(bgRunes) < w {
			bgRunes = append(bgRunes, ' ')
		}
		// Replace section with overlay line
		prefix := string(bgRunes[:startCol])
		suffix := ""
		end := startCol + lipgloss.Width(ovLine)
		if end < len(bgRunes) {
			suffix = string(bgRunes[end:])
		}
		bgLines[row] = prefix + ovLine + suffix
	}

	return strings.Join(bgLines[:h], "\n")
}

// ── Commands ──

func (d *Dashboard) fetchData() tea.Cmd {
	return func() tea.Msg {
		token, err := woffu.Authenticate(d.client, d.companyClient, d.cfg.WoffuEmail, d.password)
		if err != nil {
			return errMsg{err}
		}
		d.token = token

		profile, _ := woffu.GetUserProfile(d.companyClient, token)

		info, err := woffu.GetSignInfo(d.companyClient, token,
			d.cfg.Latitude, d.cfg.Longitude, d.cfg.HomeLatitude, d.cfg.HomeLongitude)
		if err != nil {
			return errMsg{err}
		}

		events, err := woffu.GetAvailableEvents(d.companyClient, token)
		if err != nil {
			return errMsg{err}
		}

		return dataMsg{signInfo: info, events: events, profile: profile}
	}
}

func (d *Dashboard) fetchAutoStatus() tea.Cmd {
	if d.cfg.GithubFork == "" {
		return nil
	}
	return func() tea.Msg {
		enabled, err := gh.IsAutoSignEnabled(d.cfg.GithubFork)
		if err != nil {
			return nil // Silently fail - not critical
		}
		return autoToggleMsg{enabled: enabled}
	}
}

func (d *Dashboard) trySign() tea.Cmd {
	if d.signing {
		return nil
	}
	if d.signInfo != nil && !d.signInfo.IsWorkingDay {
		d.setFlash("Not a working day", true)
		return d.clearFlashAfter(3 * time.Second)
	}
	d.signing = true
	d.setFlash("Signing...", false)
	return d.doSign()
}

func (d *Dashboard) doSign() tea.Cmd {
	return func() tea.Msg {
		if d.signInfo == nil {
			return errMsg{fmt.Errorf("no sign info available")}
		}
		err := woffu.DoSign(d.companyClient, d.token, d.signInfo.Latitude, d.signInfo.Longitude)
		if err != nil {
			return errMsg{err}
		}
		tgCfg := notify.TelegramConfig{BotToken: d.cfg.Telegram.BotToken, ChatID: d.cfg.Telegram.ChatID}
		_ = notify.SendSignedNotification(tgCfg, d.signInfo)
		return signDoneMsg{}
	}
}

func (d *Dashboard) toggleAuto(enable bool) tea.Cmd {
	return func() tea.Msg {
		var err error
		if enable {
			err = gh.EnableAutoSign(d.cfg.GithubFork)
		} else {
			err = gh.DisableAutoSign(d.cfg.GithubFork)
		}
		if err != nil {
			return errMsg{err}
		}
		return autoToggleMsg{enabled: enable}
	}
}

func (d *Dashboard) syncGitHub() tea.Cmd {
	return func() tea.Msg {
		pw, _ := config.GetPassword(d.cfg.WoffuEmail)
		if err := gh.SyncSecrets(d.cfg, pw); err != nil {
			return errMsg{fmt.Errorf("sync secrets: %w", err)}
		}
		if err := gh.SyncWorkflows(d.cfg); err != nil {
			return errMsg{fmt.Errorf("sync workflows: %w", err)}
		}
		return syncDoneMsg{}
	}
}

func (d *Dashboard) applyPreset(name string) tea.Cmd {
	return func() tea.Msg {
		if !d.cfg.LoadSchedulePreset(name) {
			return errMsg{fmt.Errorf("preset \"%s\" not found", name)}
		}
		if err := config.Save(d.cfg); err != nil {
			return errMsg{fmt.Errorf("save config: %w", err)}
		}
		// Sync workflows if github is configured
		if d.cfg.GithubFork != "" {
			gh.SyncWorkflows(d.cfg)
		}
		return syncDoneMsg{}
	}
}

func (d *Dashboard) tick() tea.Cmd {
	return tea.Tick(time.Minute, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (d *Dashboard) clearFlashAfter(dur time.Duration) tea.Cmd {
	return tea.Tick(dur, func(t time.Time) tea.Msg { return clearFlashMsg{} })
}

func (d *Dashboard) setFlash(text string, isErr bool) {
	d.flash = text
	d.flashErr = isErr
}

func openBrowserCmd(url string) {
	exec.Command("open", url).Start()
}
