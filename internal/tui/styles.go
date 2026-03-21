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
	colorBarBg     = lipgloss.Color("#1e1b4b") // Deep indigo bar bg
	colorHeaderBg  = lipgloss.Color("#2e1065") // Rich purple header bg
	colorFooterBg  = lipgloss.Color("#1e1b4b") // Deep indigo footer bg
	colorTabActive = lipgloss.Color("#c084fc") // Active tab underline
	colorTabInact  = lipgloss.Color("#4b5563") // Inactive tab
	colorOverlayBg = lipgloss.Color("#1c1917") // Warm dark bg for overlays
	colorAccent    = lipgloss.Color("#a78bfa") // Lighter purple accent
	colorSeparator = lipgloss.Color("#374151") // Subtle separator
)

// Styles
var (
	sTitle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#e9d5ff")).
		PaddingLeft(1)

	sBrand = lipgloss.NewStyle().
		Bold(true).
		Foreground(colorPrimary)

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

	// Section headers
	sSectionHeader = lipgloss.NewStyle().
			Foreground(colorDim)

	// Info box with nicer border and padding
	sInfoBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorSeparator).
			Padding(1, 3).
			MarginLeft(2)

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

	// Overlay title bar
	sOverlayTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#e9d5ff")).
			Background(colorHeaderBg).
			Padding(0, 2)

	// Menu item (normal)
	sMenuItem = lipgloss.NewStyle().
			Foreground(colorMuted).
			PaddingLeft(2).
			PaddingRight(2)

	// Menu item (active/hovered)
	sMenuItemActive = lipgloss.NewStyle().
			Foreground(colorText).
			Bold(true).
			Background(lipgloss.Color("#374151")).
			PaddingLeft(1).
			PaddingRight(2)

	// Menu separator
	sMenuSeparator = lipgloss.NewStyle().
			Foreground(colorSeparator)
)

// hint renders a keyboard shortcut hint: key in cyan + description in gray.
func hint(key, desc string) string {
	return sKey.Render(key) + " " + sHint.Render(desc)
}

// tabStyle renders a tab label with active/inactive styling.
// Active tabs use block characters for a real tab look; inactive tabs are dimmed.
func tabStyle(name string, active bool) string {
	if active {
		return lipgloss.NewStyle().
			Bold(true).
			Foreground(colorPrimary).
			Render(" " + name + " ")
	}
	return lipgloss.NewStyle().
		Foreground(colorTabInact).
		Render(" " + name + " ")
}
