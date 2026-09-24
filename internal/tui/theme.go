package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// ── Palette ──
//
// One accent (violet, the woffux brand) plus a small set of semantic colors.
// Every color means one thing everywhere: green is "working / done", amber is
// "needs attention soon", red is "something is wrong", teal is "remote",
// blue is "office", rose is "holiday", gold is "time off".

var (
	cBrand     = lipgloss.Color("#a78bfa") // violet — brand, focus, selection
	cBrandDeep = lipgloss.Color("#6d28d9")
	cText      = lipgloss.Color("#e7e5e4") // primary text
	cSubtle    = lipgloss.Color("#a8a29e") // secondary text
	cFaint     = lipgloss.Color("#78716c") // hints, labels
	cGhost     = lipgloss.Color("#57534e") // rules, empty tracks
	cLine      = lipgloss.Color("#57534e") // borders
	cSurface   = lipgloss.Color("#44403c") // raised surface (cursor rows)

	cOK      = lipgloss.Color("#4ade80") // working / success
	cWarn    = lipgloss.Color("#fbbf24") // attention
	cBad     = lipgloss.Color("#f87171") // error / missed
	cRemote  = lipgloss.Color("#2dd4bf") // telework
	cOffice  = lipgloss.Color("#60a5fa") // office
	cHoliday = lipgloss.Color("#f472b6") // public holiday
	cTimeOff = lipgloss.Color("#facc15") // vacation / absence
)

// ── Text styles ──

var (
	sText   = lipgloss.NewStyle().Foreground(cText)
	sBold   = lipgloss.NewStyle().Foreground(cText).Bold(true)
	sSubtle = lipgloss.NewStyle().Foreground(cSubtle)
	sFaint  = lipgloss.NewStyle().Foreground(cFaint)
	sGhost  = lipgloss.NewStyle().Foreground(cGhost)
	sBrand  = lipgloss.NewStyle().Foreground(cBrand).Bold(true)
	sOK     = lipgloss.NewStyle().Foreground(cOK)
	sWarn   = lipgloss.NewStyle().Foreground(cWarn)
	sBad    = lipgloss.NewStyle().Foreground(cBad)
	sKey    = lipgloss.NewStyle().Foreground(cBrand).Bold(true)

	// sLabel is the small-caps-ish section label used above every block.
	sLabel = lipgloss.NewStyle().Foreground(cFaint).Bold(true)
)

func fg(c lipgloss.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }

// label renders a spaced uppercase section label: "T O D A Y" reads as a
// quiet heading without competing with the content.
func label(s string) string {
	return sLabel.Render(strings.ToUpper(s))
}

// keycap renders a shortcut and what it does, for footers and hints.
func keycap(key, desc string) string {
	return sKey.Render(key) + " " + sFaint.Render(desc)
}

// card wraps content in a rounded border with the given accent.
func card(content string, accent lipgloss.Color, width int) string {
	st := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accent).
		Padding(0, 2)
	if width > 0 {
		st = st.Width(width - 2) // border takes 2 columns
	}
	return st.Render(content)
}

// rule draws a faint horizontal line.
func rule(width int) string {
	if width <= 0 {
		return ""
	}
	return sGhost.Render(strings.Repeat("─", width))
}

// ── Big digits ──
//
// A 3-row half-block font for the hero number (hours worked, countdowns).
// Large, calm numbers answer "how am I doing" from across the room.

var bigGlyphs = map[rune][3]string{
	'0': {"█▀█", "█ █", "▀▀▀"},
	'1': {"▀█ ", " █ ", "▀▀▀"},
	'2': {"▀▀█", "█▀▀", "▀▀▀"},
	'3': {"▀▀█", " ▀█", "▀▀▀"},
	'4': {"█ █", "▀▀█", "  ▀"},
	'5': {"█▀▀", "▀▀█", "▀▀▀"},
	'6': {"█▀▀", "█▀█", "▀▀▀"},
	'7': {"▀▀█", "  █", "  ▀"},
	'8': {"█▀█", "█▀█", "▀▀▀"},
	'9': {"█▀█", "▀▀█", "▀▀▀"},
	':': {" ", "▀", "▀"},
	' ': {" ", " ", " "},
}

// bigText renders s with the block font. Unknown runes fall back to spaces.
func bigText(s string, color lipgloss.Color) string {
	var rows [3]strings.Builder
	first := true
	for _, r := range s {
		g, ok := bigGlyphs[r]
		if !ok {
			g = bigGlyphs[' ']
		}
		for i := 0; i < 3; i++ {
			if !first {
				rows[i].WriteString(" ")
			}
			rows[i].WriteString(g[i])
		}
		first = false
	}
	st := fg(color).Bold(true)
	return st.Render(rows[0].String()) + "\n" + st.Render(rows[1].String()) + "\n" + st.Render(rows[2].String())
}

// ── Layout helpers ──

// truncate cuts a plain or styled string to width, adding an ellipsis.
func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	return ansi.Truncate(s, width, "…")
}

// padRight pads a (possibly styled) string with spaces to exactly width.
func padRight(s string, width int) string {
	w := lipgloss.Width(s)
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}

// spread places left and right on one line of the given width.
func spread(left, right string, width int) string {
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

// fitHeight pads or clips a block to exactly h lines.
func fitHeight(block string, h int) string {
	if h <= 0 {
		return ""
	}
	lines := strings.Split(block, "\n")
	if len(lines) > h {
		lines = lines[:h]
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

// indent prefixes every line of block with n spaces.
func indent(block string, n int) string {
	pad := strings.Repeat(" ", n)
	lines := strings.Split(block, "\n")
	for i, l := range lines {
		if l != "" {
			lines[i] = pad + l
		}
	}
	return strings.Join(lines, "\n")
}

// flow lays out items left to right, wrapping to new lines at width.
func flow(items []string, sep string, width int) string {
	var lines []string
	line := ""
	for _, it := range items {
		if line != "" && lipgloss.Width(line)+lipgloss.Width(sep)+lipgloss.Width(it) > width {
			lines = append(lines, line)
			line = ""
		}
		if line != "" {
			line += sep
		}
		line += it
	}
	if line != "" {
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}
