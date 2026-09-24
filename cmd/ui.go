package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/ngavilan-dogfy/woffux/internal/woffu"
)

// One small vocabulary for every command's human output, so the CLI reads
// like the dashboard: a title, labelled rows, quiet section labels, status
// lines with a leading mark, and a hint of what to do next.

const uiIndent = "  "

// uiAfterTitle is true right after a title (which already ends with a
// blank line), so the next section doesn't add a second one.
var uiAfterTitle bool

// uiLine prints one indented line.
func uiLine(s string) {
	uiAfterTitle = false
	fmt.Println(uiIndent + s)
}

// uiTitle prints "◆ Title · subtitle" with a blank line before and after.
func uiTitle(title string, sub ...string) {
	line := stBrand.Render("◆ " + title)
	if len(sub) > 0 && sub[0] != "" {
		line += stFaint.Render("  ·  " + strings.Join(sub, "  ·  "))
	}
	fmt.Println()
	fmt.Println(uiIndent + line)
	fmt.Println()
	uiAfterTitle = true
}

// uiSection prints a quiet uppercase label introducing a block.
func uiSection(name string) {
	if !uiAfterTitle {
		fmt.Println()
	}
	uiAfterTitle = false
	fmt.Println(uiIndent + lipgloss.NewStyle().Foreground(obFaint).Bold(true).Render(strings.ToUpper(name)))
}

// uiRow prints an aligned label/value row.
func uiRow(label, value string) {
	uiAfterTitle = false
	fmt.Println(uiIndent + stFaint.Render(fmt.Sprintf("%-12s", label)) + " " + value)
}

func uiOK(format string, a ...any) {
	uiAfterTitle = false
	fmt.Println(uiIndent + stIn.Render("✓ ") + stText.Render(fmt.Sprintf(format, a...)))
}

func uiWarn(format string, a ...any) {
	uiAfterTitle = false
	fmt.Println(uiIndent + stOut.Render("! ") + stText.Render(fmt.Sprintf(format, a...)))
}

func uiErr(format string, a ...any) {
	uiAfterTitle = false
	fmt.Println(uiIndent + stBad.Render("✗ ") + stText.Render(fmt.Sprintf(format, a...)))
}

// uiHint ends an output with what to do next: "→ woffux x · woffux y".
func uiHint(cmds ...string) {
	if len(cmds) == 0 {
		return
	}
	var parts []string
	for _, c := range cmds {
		parts = append(parts, stSubtle.Render(c))
	}
	fmt.Println()
	fmt.Println(uiIndent + stFaint.Render("→ ") + strings.Join(parts, stFaint.Render("  ·  ")))
	fmt.Println()
}

// uiDate renders a YYYY-MM-DD date for humans: "today", "tomorrow",
// "Mon 5 Oct", adding the year when it isn't this year.
func uiDate(date string) string {
	t, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(date), time.Local)
	if err != nil {
		return date
	}
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	switch int(t.Sub(today).Hours() / 24) {
	case 0:
		return "today"
	case 1:
		return "tomorrow"
	case -1:
		return "yesterday"
	}
	if t.Year() != now.Year() {
		return t.Format("Mon 2 Jan 2006")
	}
	return t.Format("Mon 2 Jan")
}

// uiMode renders the sign mode like the dashboard does.
func uiMode(m woffu.SignMode) string {
	if m == woffu.SignModeRemote {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#2dd4bf")).Render("⌂ Remote")
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("#60a5fa")).Render("▣ Office")
}

// uiRequestStatus renders a request status with its mark and colour.
func uiRequestStatus(status string) string {
	switch strings.ToLower(status) {
	case "approved":
		return stIn.Render("✓ approved")
	case "pending":
		return stOut.Render("◷ pending")
	case "rejected":
		return stBad.Render("✗ rejected")
	case "cancelled", "canceled":
		return stFaint.Render("– cancelled")
	}
	return stSubtle.Render(status)
}

// uiAmount renders a balance: "6 days", "26 h".
func uiAmount(v float64, unit string) string {
	num := strings.TrimSuffix(fmt.Sprintf("%.1f", v), ".0")
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

// uiTrim strips decorative emoji Woffu puts in type names ("Teletrabajo🏡").
func uiTrim(name string) string {
	var b strings.Builder
	for _, r := range name {
		if r < 0x2600 { // keep letters, accents, punctuation
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

// uiBar draws a small gauge for balances.
func uiBar(f float64, w int, color lipgloss.Color) string {
	f = min(max(f, 0), 1)
	n := int(f*float64(w) + 0.5)
	return lipgloss.NewStyle().Foreground(color).Render(strings.Repeat("━", n)) +
		stGhost.Render(strings.Repeat("━", w-n))
}

// stripANSIcmd removes styling (for text that goes into form descriptions).
func stripANSIcmd(s string) string { return ansi.Strip(s) }
