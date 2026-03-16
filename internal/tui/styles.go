package tui

import "github.com/charmbracelet/lipgloss"

// Colors
var (
	colorPrimary   = lipgloss.Color("#c084fc") // Purple
	colorSecondary = lipgloss.Color("#06B6D4") // Cyan
	colorSuccess   = lipgloss.Color("#22c55e") // Green
	colorDanger    = lipgloss.Color("#ef4444") // Red
	colorWarning   = lipgloss.Color("#f59e0b") // Amber
	colorMuted     = lipgloss.Color("#6b7280") // Gray
	colorDim       = lipgloss.Color("#4b5563") // Dark gray
	colorText      = lipgloss.Color("#f9fafb") // White
	colorBg        = lipgloss.Color("#111827") // Dark bg
	colorBarBg     = lipgloss.Color("#0f172a") // Darker bg
	colorTabActive = lipgloss.Color("#c084fc") // Active tab underline
	colorTabInact  = lipgloss.Color("#4b5563") // Inactive tab
)

// Styles
var (
	sTitle = lipgloss.NewStyle().
		Bold(true).
		Foreground(colorPrimary).
		PaddingLeft(1)

	sSubtitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorSecondary)

	sLabel = lipgloss.NewStyle().
		Foreground(colorMuted).
		Width(18)

	sValue = lipgloss.NewStyle().
		Foreground(colorText).
		Bold(true)

	sSuccess = lipgloss.NewStyle().
			Foreground(colorSuccess).
			Bold(true)

	sDanger = lipgloss.NewStyle().
		Foreground(colorDanger).
		Bold(true)

	sDimmed = lipgloss.NewStyle().
		Foreground(colorDim)

	sKey = lipgloss.NewStyle().
		Foreground(colorSecondary).
		Bold(true)

	sHint = lipgloss.NewStyle().
		Foreground(colorMuted)

	sBox = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorDim).
		Padding(0, 1)

	sSection = lipgloss.NewStyle().
			Foreground(colorSecondary).
			Bold(true).
			PaddingBottom(1)

	sFlashSuccess = lipgloss.NewStyle().Foreground(colorSuccess)
	sFlashError   = lipgloss.NewStyle().Foreground(colorDanger)

	sSignIn  = lipgloss.NewStyle().Foreground(colorSuccess)
	sSignOut = lipgloss.NewStyle().Foreground(colorDanger)

	// New styles for redesigned status tab
	sSectionHeader = lipgloss.NewStyle().
			Foreground(colorDim)

	sInfoBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorDim).
			Padding(0, 2)

	sLiveIndicator = lipgloss.NewStyle().
			Foreground(colorSuccess).
			Bold(true)

	sCountdown = lipgloss.NewStyle().
			Foreground(colorMuted).
			Italic(true)

	sTimelineTrack = lipgloss.NewStyle().
			Foreground(colorDim)

	sNowMarker = lipgloss.NewStyle().
			Foreground(colorWarning).
			Bold(true)
)

// hint renders a keyboard shortcut hint: key in cyan + description in gray.
func hint(key, desc string) string {
	return sKey.Render(key) + " " + sHint.Render(desc)
}

// tabStyle renders a tab label with active/inactive styling.
// Active tabs get a colored underline and bold text; inactive tabs are dimmed.
func tabStyle(name string, active bool) string {
	if active {
		return lipgloss.NewStyle().
			Bold(true).
			Foreground(colorPrimary).
			UnderlineSpaces(true).
			Underline(true).
			Render(name)
	}
	return lipgloss.NewStyle().
		Foreground(colorTabInact).
		Render(name)
}
