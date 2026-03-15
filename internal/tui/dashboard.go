package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ngavilan-dogfy/woffuk-cli/internal/config"
	"github.com/ngavilan-dogfy/woffuk-cli/internal/notify"
	"github.com/ngavilan-dogfy/woffuk-cli/internal/woffu"
)

type state int

const (
	stateLoading state = iota
	stateReady
	stateError
	stateSigning
	stateSigned
)

type dataLoadedMsg struct {
	signInfo *woffu.SignInfo
	events   []woffu.AvailableUserEvent
}

type errMsg struct{ err error }
type signDoneMsg struct{}

type Dashboard struct {
	client        *woffu.Client
	companyClient *woffu.Client
	cfg           *config.Config
	password      string

	state    state
	token    string
	signInfo *woffu.SignInfo
	events   []woffu.AvailableUserEvent
	err      error
}

func NewDashboard(client, companyClient *woffu.Client, cfg *config.Config, password string) *Dashboard {
	return &Dashboard{
		client:        client,
		companyClient: companyClient,
		cfg:           cfg,
		password:      password,
		state:         stateLoading,
	}
}

func (d *Dashboard) Init() tea.Cmd {
	return d.fetchData()
}

func (d *Dashboard) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return d, tea.Quit
		case "s":
			if d.state == stateReady && d.signInfo != nil && d.signInfo.IsWorkingDay {
				d.state = stateSigning
				return d, d.doSign()
			}
		case "r":
			d.state = stateLoading
			return d, d.fetchData()
		}

	case dataLoadedMsg:
		d.signInfo = msg.signInfo
		d.events = msg.events
		d.state = stateReady

	case signDoneMsg:
		d.state = stateSigned

	case errMsg:
		d.err = msg.err
		d.state = stateError
	}

	return d, nil
}

func (d *Dashboard) View() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("woffuk"))
	b.WriteString("\n\n")

	switch d.state {
	case stateLoading:
		b.WriteString(dimStyle.Render("  Loading..."))

	case stateError:
		b.WriteString(redStyle.Render(fmt.Sprintf("  Error: %s", d.err)))

	case stateSigning:
		b.WriteString(dimStyle.Render("  Signing..."))

	case stateSigned:
		b.WriteString(greenStyle.Render("  Signed successfully!"))
		b.WriteString("\n")
		b.WriteString(helpStyle.Render("  [r] refresh  [q] quit"))

	case stateReady:
		b.WriteString(d.renderStatus())
		b.WriteString("\n")
		b.WriteString(d.renderEvents())
		b.WriteString("\n")
		b.WriteString(d.renderNextEvents())
		b.WriteString("\n")
		b.WriteString(d.renderHelp())
	}

	return b.String()
}

func (d *Dashboard) renderStatus() string {
	info := d.signInfo

	workingDay := redStyle.Render("no")
	if info.IsWorkingDay {
		workingDay = greenStyle.Render("yes")
	}

	modeStr := dimStyle.Render(fmt.Sprintf("%s %s", info.Mode.Emoji(), info.Mode.Label()))

	rows := []string{
		labelStyle.Render("Date") + valueStyle.Render(info.Date),
		labelStyle.Render("Working day") + workingDay,
		labelStyle.Render("Mode") + modeStr,
	}

	if info.IsWorkingDay {
		rows = append(rows, labelStyle.Render("Coordinates")+
			dimStyle.Render(fmt.Sprintf("%.4f, %.4f", info.Latitude, info.Longitude)))
	}

	content := strings.Join(rows, "\n")
	return boxStyle.Render(content)
}

func (d *Dashboard) renderEvents() string {
	if len(d.events) == 0 {
		return ""
	}

	var rows []string
	for _, e := range d.events {
		name := eventNameStyle.Render(fmt.Sprintf("%-42s", e.Name))
		value := eventValueStyle.Render(fmt.Sprintf("%6.0f %s", e.Available, e.Unit))
		rows = append(rows, name+value)
	}

	content := dimStyle.Render("Available events") + "\n" + strings.Join(rows, "\n")
	return boxStyle.Render(content)
}

func (d *Dashboard) renderNextEvents() string {
	if d.signInfo == nil || len(d.signInfo.NextEvents) == 0 {
		return ""
	}

	var rows []string
	limit := 8
	for i, e := range d.signInfo.NextEvents {
		if i >= limit {
			rows = append(rows, dimStyle.Render(fmt.Sprintf("  ... and %d more", len(d.signInfo.NextEvents)-limit)))
			break
		}
		name := ""
		if len(e.Names) > 0 {
			name = " — " + e.Names[0]
		}
		rows = append(rows, fmt.Sprintf("  %s%s", dimStyle.Render(e.Date), name))
	}

	content := dimStyle.Render("Next holidays / events") + "\n" + strings.Join(rows, "\n")
	return boxStyle.Render(content)
}

func (d *Dashboard) renderHelp() string {
	parts := []string{"[r] refresh", "[q] quit"}
	if d.signInfo != nil && d.signInfo.IsWorkingDay {
		parts = append([]string{"[s] sign"}, parts...)
	}
	return helpStyle.Render("  " + lipgloss.JoinHorizontal(lipgloss.Top, strings.Join(parts, "  ")))
}

func (d *Dashboard) fetchData() tea.Cmd {
	return func() tea.Msg {
		token, err := woffu.Authenticate(d.client, d.companyClient, d.cfg.WoffuEmail, d.password)
		if err != nil {
			return errMsg{err}
		}
		d.token = token

		info, err := woffu.GetSignInfo(d.companyClient, token,
			d.cfg.Latitude, d.cfg.Longitude, d.cfg.HomeLatitude, d.cfg.HomeLongitude)
		if err != nil {
			return errMsg{err}
		}

		events, err := woffu.GetAvailableEvents(d.companyClient, token)
		if err != nil {
			return errMsg{err}
		}

		return dataLoadedMsg{signInfo: info, events: events}
	}
}

func (d *Dashboard) doSign() tea.Cmd {
	return func() tea.Msg {
		err := woffu.DoSign(d.companyClient, d.token, d.signInfo.Latitude, d.signInfo.Longitude)
		if err != nil {
			return errMsg{err}
		}

		telegramCfg := notify.TelegramConfig{
			BotToken: d.cfg.Telegram.BotToken,
			ChatID:   d.cfg.Telegram.ChatID,
		}
		_ = notify.SendSignedNotification(telegramCfg, d.signInfo)

		return signDoneMsg{}
	}
}
