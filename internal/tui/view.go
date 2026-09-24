package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// ── Frame ──
//
// Every screen is: header (brand · tabs · clock), body, footer (toast or
// key hints). Overlays replace the body only, so you never lose context.

const gutter = 2

func (d *Dashboard) View() string {
	if d.width == 0 || d.height == 0 {
		return ""
	}
	if d.width < 44 || d.height < 14 {
		return lipgloss.Place(d.width, d.height, lipgloss.Center, lipgloss.Center,
			sSubtle.Render("woffux needs a bit more room")+"\n"+sFaint.Render("(at least 44×14)"))
	}

	header := d.renderHeader()
	footer := d.renderFooter()
	bodyH := d.height - lipgloss.Height(header) - lipgloss.Height(footer)

	var body string
	switch {
	case d.overlay != overlayNone:
		body = lipgloss.Place(d.width, bodyH, lipgloss.Center, lipgloss.Center, d.renderOverlay(bodyH))
	case d.loading:
		body = lipgloss.Place(d.width, bodyH, lipgloss.Center, lipgloss.Center, d.renderLoading())
	case d.loadErr != nil && d.signInfo == nil:
		body = lipgloss.Place(d.width, bodyH, lipgloss.Center, lipgloss.Center, d.renderLoadError())
	default:
		body = d.renderBody(bodyH)
	}
	return header + "\n" + clipWidth(fitHeight(body, bodyH), d.width) + "\n" + footer
}

// clipWidth is a safety net: no line may ever exceed the terminal width,
// or the terminal wraps it and the whole layout shifts.
func clipWidth(block string, w int) string {
	lines := strings.Split(block, "\n")
	for i, l := range lines {
		if lipgloss.Width(l) > w {
			clipCount++
			lines[i] = ansi.Truncate(l, w, "")
		}
	}
	return strings.Join(lines, "\n")
}

func (d *Dashboard) contentWidth() int { return d.width - 2*gutter }

func (d *Dashboard) renderBody(h int) string {
	var b string
	switch d.activeTab {
	case tabToday:
		b = d.renderToday(h)
	case tabCalendar:
		b = d.renderCalendar(h)
	case tabSchedule:
		b = d.renderSchedule(h)
	case tabBalance:
		b = d.renderBalance(h)
	}
	return indent("\n"+b, gutter)
}

// ── Header ──

func (d *Dashboard) renderHeader() string {
	w := d.width - 2*gutter
	now := d.clock()
	right := sSubtle.Render(now.Format("Mon 2 Jan")) + "  " + sBold.Render(now.Format("15:04"))
	if d.refreshing || d.busy != "" {
		right = d.spin.View() + " " + right
	} else if !d.fetchedAt.IsZero() && now.Sub(d.fetchedAt) > 10*time.Minute {
		right = sWarn.Render("● stale") + "  " + right
	}
	if d.latest != "" {
		right = sKey.Render("U") + sOK.Render(" ⬆ "+d.latest) + "   " + right
	}

	// Degrade gracefully: drop the date, then shorten inactive tabs, then
	// drop the brand, until the header fits.
	left := d.headerLeft(true, true)
	if lipgloss.Width(left)+lipgloss.Width(right)+2 > w {
		right = sBold.Render(now.Format("15:04"))
	}
	if lipgloss.Width(left)+lipgloss.Width(right)+2 > w {
		left = d.headerLeft(true, false)
	}
	if lipgloss.Width(left)+lipgloss.Width(right)+2 > w {
		left = d.headerLeft(false, false)
	}
	line := strings.Repeat(" ", gutter) + spread(left, right, w)
	return "\n" + line + "\n" + strings.Repeat(" ", gutter) + rule(w)
}

func (d *Dashboard) headerLeft(withBrand, fullTabs bool) string {
	var tabs []string
	for i, name := range tabNames {
		num := fmt.Sprintf("%d", i+1)
		if i != d.activeTab && !fullTabs {
			tabs = append(tabs, sFaint.Render(num))
			continue
		}
		if i == d.activeTab {
			tabs = append(tabs, lipgloss.NewStyle().Background(cSurface).Padding(0, 1).Render(
				lipgloss.NewStyle().Background(cSurface).Foreground(cFaint).Render(num+" ")+
					lipgloss.NewStyle().Background(cSurface).Foreground(cBrand).Bold(true).Render(name)))
		} else {
			tabs = append(tabs, lipgloss.NewStyle().Padding(0, 1).Render(sGhost.Render(num+" ")+sFaint.Render(name)))
		}
	}
	if !withBrand {
		return strings.Join(tabs, " ")
	}
	return sBrand.Render("◆ woffux") + "   " + strings.Join(tabs, " ")
}

// ── Footer ──

func (d *Dashboard) renderFooter() string {
	w := d.width - 2*gutter
	var left string
	switch {
	case d.busy != "":
		left = d.spin.View() + " " + sText.Render(d.busy+"…")
	case d.toast.text != "":
		switch d.toast.kind {
		case toastOK:
			left = sOK.Render("✓ ") + sText.Render(d.toast.text)
		case toastErr:
			left = sBad.Render("✗ ") + sText.Render(d.toast.text)
		default:
			left = sBrand.Render("› ") + sSubtle.Render(d.toast.text)
		}
	}
	right := keycap("?", "help") + "  " + keycap("q", "quit")
	if d.overlay != overlayNone {
		right = keycap("esc", "close")
	}
	if left == "" {
		hints := d.footerHints()
		for len(hints) > 0 && lipgloss.Width(strings.Join(hints, "  "))+lipgloss.Width(right)+2 > w {
			hints = hints[:len(hints)-1]
		}
		left = strings.Join(hints, "  ")
	}
	if lipgloss.Width(left)+lipgloss.Width(right)+2 > w {
		left = truncate(left, w-lipgloss.Width(right)-2)
	}
	return strings.Repeat(" ", gutter) + rule(w) + "\n" + strings.Repeat(" ", gutter) + spread(left, right, w)
}

func (d *Dashboard) footerHints() []string {
	var hints []string
	switch d.overlay {
	case overlayPalette:
		return []string{keycap("↑↓", "choose"), keycap("⏎", "run"), sFaint.Render("type to filter")}
	case overlayDay:
		return []string{keycap("↑↓", "choose"), keycap("⏎", "run"), sFaint.Render("or press the letter")}
	case overlayConfirm, overlayInput, overlayHelp, overlayEditor, overlayDates:
		return nil
	case overlayMenu:
		return []string{keycap("↑↓", "choose"), keycap("⏎", "select")}
	}
	switch d.activeTab {
	case tabToday:
		hints = []string{keycap("s", "sign "+strings.ToLower(d.pendingSignAction())), keycap("⏎", "actions"), keycap("r", "refresh"), keycap("tab", "calendar")}
	case tabCalendar:
		if d.cal != nil && len(d.cal.selected) > 0 {
			n := len(d.cal.selected)
			hints = []string{sBrand.Render(fmt.Sprintf("%d %s selected", n, plural(n, "day", "days"))), keycap("t", "telework"), keycap("v", "vacation"), keycap("c", "cancel"), keycap("esc", "clear")}
		} else {
			hints = []string{keycap("←↑↓→", "move"), keycap("space", "select"), keycap("[ ]", "month"), keycap("t", "telework"), keycap("v", "vacation"), keycap("⏎", "more")}
		}
	case tabSchedule:
		hints = []string{keycap("↑↓", "choose"), keycap("⏎", "use"), keycap("e", "edit"), keycap("n", "new"), keycap("c", "copy"), keycap("R", "rename"), keycap("x", "delete"), keycap("S", "summer"), keycap("t", "timing")}
	case tabBalance:
		hints = []string{keycap("⏎", "actions"), keycap("r", "refresh"), keycap("o", "open Woffu")}
	}
	return hints
}

// ── Loading / error ──

func (d *Dashboard) renderLoading() string {
	return lipgloss.JoinVertical(lipgloss.Center,
		sBrand.Render("◆ woffux"),
		"",
		d.spin.View()+" "+sSubtle.Render("Talking to Woffu…"),
	)
}

func (d *Dashboard) renderLoadError() string {
	body := sBad.Render("✗ Couldn't load your data") + "\n\n" +
		sText.Render(truncate(friendlyError(d.loadErr), 60)) + "\n\n" +
		keycap("r", "try again") + "   " + keycap("o", "open Woffu") + "   " + keycap("q", "quit")
	return card(body, cBad, 0)
}

// ── Sections ──

// twoColumns lays out left and right blocks, or stacks them when narrow.
func twoColumns(left, right string, leftW, gap int) string {
	lines := func(s string) []string { return strings.Split(s, "\n") }
	l, r := lines(left), lines(right)
	n := max(len(l), len(r))
	var b strings.Builder
	for i := 0; i < n; i++ {
		var a, c string
		if i < len(l) {
			a = l[i]
		}
		if i < len(r) {
			c = r[i]
		}
		b.WriteString(padRight(a, leftW) + strings.Repeat(" ", gap) + c)
		if i < n-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// clipCount counts safety-net truncations (tests assert it stays at zero).
var clipCount int
