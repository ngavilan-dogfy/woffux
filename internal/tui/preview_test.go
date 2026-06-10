package tui

// Render-preview harness: prints the dashboard to test output so layout work
// can be reviewed without a real terminal.
// Run with: WOFFUX_TUI_PREVIEW=1 go test ./internal/tui -run TestPreview -v

import (
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/ngavilan-dogfy/woffux/internal/config"
	"github.com/ngavilan-dogfy/woffux/internal/woffu"
)

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func previewDashboard() *Dashboard {
	active := true
	agentOn := true
	inSync := true
	today := time.Now().Format("2006-01-02")

	cfg := &config.Config{
		GithubFork:      "owner/woffux",
		WoffuCompanyURL: "https://example.woffu.com",
		ActiveSchedule:  "classic",
		Schedule:        config.DefaultSchedule(),
		SavedSchedules: map[string]config.Schedule{
			"classic":    config.DefaultSchedule(),
			"intensivah": config.DefaultSchedule(),
		},
	}

	return &Dashboard{
		cfg:    cfg,
		width:  90,
		height: 40,
		profile: &woffu.UserProfile{
			FullName: "Nahuel Gavilán",
		},
		signInfo: &woffu.SignInfo{
			Date:         today,
			Mode:         woffu.SignModeRemote,
			IsWorkingDay: true,
			Latitude:     41.19,
			Longitude:    1.59,
		},
		slots: []woffu.SignSlot{
			{In: today + "T08:32:00.000", Out: today + "T13:30:00.000"},
			{In: today + "T14:15:00.000"},
		},
		monthSigns: []woffu.SignRecord{
			{Date: time.Now().AddDate(0, 0, -2).Format("2006-01-02"), Time: "08:30", Type: "in"},
			{Date: time.Now().AddDate(0, 0, -2).Format("2006-01-02"), Time: "17:31", Type: "out"},
			{Date: time.Now().AddDate(0, 0, -1).Format("2006-01-02"), Time: "08:28", Type: "in"},
			{Date: time.Now().AddDate(0, 0, -1).Format("2006-01-02"), Time: "17:35", Type: "out"},
		},
		events: []woffu.AvailableUserEvent{
			{Name: "Vacaciones", Available: 18, Unit: "days"},
			{Name: "Bolsa de horas", Available: 12.5, Unit: "hours"},
			{Name: "Asuntos propios", Available: 2, Unit: "days"},
		},
		autoActive:  &active,
		autoInSync:  &inSync,
		agentActive: &agentOn,
		lastRunAt:   time.Now().Add(-130 * time.Minute),
		lastRunOK:   true,
	}
}

func printView(t *testing.T, d *Dashboard) {
	t.Helper()
	view := ansiRe.ReplaceAllString(d.View(), "")
	t.Logf("\n%s", view)
}

func TestPreviewStatusTab(t *testing.T) {
	if os.Getenv("WOFFUX_TUI_PREVIEW") != "1" {
		t.Skip("preview disabled")
	}
	d := previewDashboard()
	d.activeTab = tabStatus
	printView(t, d)
}

func TestPreviewEventsTab(t *testing.T) {
	if os.Getenv("WOFFUX_TUI_PREVIEW") != "1" {
		t.Skip("preview disabled")
	}
	d := previewDashboard()
	d.activeTab = tabEvents
	printView(t, d)
}

func TestPreviewMenuOverlay(t *testing.T) {
	if os.Getenv("WOFFUX_TUI_PREVIEW") != "1" {
		t.Skip("preview disabled")
	}
	d := previewDashboard()
	d.overlay = overlayMenu
	printView(t, d)
}

func TestPreviewConfirmSignOverlay(t *testing.T) {
	if os.Getenv("WOFFUX_TUI_PREVIEW") != "1" {
		t.Skip("preview disabled")
	}
	d := previewDashboard()
	d.overlay = overlayConfirmSign
	printView(t, d)
}

func TestPreviewHelpOverlay(t *testing.T) {
	if os.Getenv("WOFFUX_TUI_PREVIEW") != "1" {
		t.Skip("preview disabled")
	}
	d := previewDashboard()
	d.overlay = overlayHelp
	printView(t, d)
}
