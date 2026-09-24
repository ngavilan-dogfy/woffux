package cmd

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// The updater is a tiny full-screen-free Bubble Tea app:
// check → what's new → confirm → download (progress) → verify → done.
// Installing (which may need sudo) happens after it exits.

type updPhase int

const (
	updChecking updPhase = iota
	updUpToDate
	updAvailable
	updDownloading
	updVerifying
	updReady
	updFailed
	updCancelled
)

type updCheckedMsg struct {
	release githubRelease
	notes   []releaseNote
	err     error
}
type updProgressMsg float64
type updDownloadedMsg struct{ err error }
type updVerifiedMsg struct{ err error }

// releaseNote is what changed in one version, as short human bullets.
type releaseNote struct {
	tag     string
	bullets []string
}

type updater struct {
	phase    updPhase
	current  string
	release  githubRelease
	notes    []releaseNote
	err      error
	tmpPath  string
	assetURL string
	autoYes  bool

	spin    spinner.Model
	bar     progress.Model
	percent float64
	program *tea.Program
	started time.Time
	width   int
}

func newUpdater(autoYes bool) *updater {
	s := spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(lipgloss.NewStyle().Foreground(obBrand)))
	b := progress.New(progress.WithGradient("#6d28d9", "#a78bfa"), progress.WithWidth(40))
	return &updater{current: Version, spin: s, bar: b, autoYes: autoYes, width: 80}
}

func (u *updater) Init() tea.Cmd {
	return tea.Batch(u.spin.Tick, func() tea.Msg {
		rel, err := fetchLatestReleaseWithFallback()
		if err != nil {
			return updCheckedMsg{err: err}
		}
		return updCheckedMsg{release: rel, notes: fetchNotesSince(Version, rel.TagName)}
	})
}

func (u *updater) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		u.width = msg.Width
		u.bar.Width = min(48, max(20, msg.Width-24))
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc", "n":
			if u.phase == updDownloading || u.phase == updVerifying {
				return u, nil // don't leave a half-written file behind
			}
			if u.phase == updAvailable {
				u.phase = updCancelled
			}
			return u, tea.Quit
		case "enter", "y":
			switch u.phase {
			case updAvailable:
				return u, u.startDownload()
			case updUpToDate, updFailed, updReady:
				return u, tea.Quit
			}
		}
	case spinner.TickMsg:
		var cmd tea.Cmd
		u.spin, cmd = u.spin.Update(msg)
		return u, cmd
	case progress.FrameMsg:
		m, cmd := u.bar.Update(msg)
		u.bar = m.(progress.Model)
		return u, cmd
	case updCheckedMsg:
		if msg.err != nil {
			u.phase, u.err = updFailed, fmt.Errorf("couldn't check for updates: %w", msg.err)
			return u, nil
		}
		u.release, u.notes = msg.release, msg.notes
		if currentVersionIsLatest(u.current, u.release.TagName) {
			u.phase = updUpToDate
			return u, tea.Quit
		}
		asset, err := releaseAssetName(runtime.GOOS, runtime.GOARCH)
		if err == nil {
			u.assetURL, err = u.release.DownloadURL(asset)
		}
		if err != nil {
			u.phase, u.err = updFailed, err
			return u, nil
		}
		u.phase = updAvailable
		if u.autoYes {
			return u, u.startDownload()
		}
	case updProgressMsg:
		u.percent = float64(msg)
		return u, u.bar.SetPercent(u.percent)
	case updDownloadedMsg:
		if msg.err != nil {
			u.phase, u.err = updFailed, msg.err
			return u, nil
		}
		u.phase = updVerifying
		u.percent = 1
		return u, tea.Batch(u.bar.SetPercent(1), u.verify())
	case updVerifiedMsg:
		if msg.err != nil {
			u.phase, u.err = updFailed, msg.err
			return u, nil
		}
		u.phase = updReady
		return u, tea.Quit
	}
	return u, nil
}

func (u *updater) startDownload() tea.Cmd {
	u.phase = updDownloading
	u.started = time.Now()
	tmp, err := os.CreateTemp("", "woffux-update-*")
	if err != nil {
		u.phase, u.err = updFailed, err
		return nil
	}
	u.tmpPath = tmp.Name()
	p := u.program
	url := u.assetURL
	return func() tea.Msg {
		defer tmp.Close()
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return updDownloadedMsg{err}
		}
		req.Header.Set("Accept", "application/octet-stream")
		client := &http.Client{Timeout: 5 * time.Minute}
		resp, err := client.Do(req)
		if err != nil {
			return updDownloadedMsg{fmt.Errorf("download failed: %w", err)}
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return updDownloadedMsg{fmt.Errorf("download returned HTTP %d (the release may still be building — try again in a minute)", resp.StatusCode)}
		}
		total := resp.ContentLength
		var done int64
		buf := make([]byte, 64*1024)
		lastSent := time.Time{}
		for {
			n, rerr := resp.Body.Read(buf)
			if n > 0 {
				if _, werr := tmp.Write(buf[:n]); werr != nil {
					return updDownloadedMsg{werr}
				}
				done += int64(n)
				if total > 0 && p != nil && time.Since(lastSent) > 60*time.Millisecond {
					p.Send(updProgressMsg(float64(done) / float64(total)))
					lastSent = time.Now()
				}
			}
			if rerr == io.EOF {
				break
			}
			if rerr != nil {
				return updDownloadedMsg{fmt.Errorf("download interrupted: %w", rerr)}
			}
		}
		if done == 0 {
			return updDownloadedMsg{fmt.Errorf("downloaded file is empty")}
		}
		return updDownloadedMsg{}
	}
}

// verify runs the downloaded binary and checks it reports the new version,
// so a broken or wrong download never replaces a working install.
func (u *updater) verify() tea.Cmd {
	path, tag := u.tmpPath, u.release.TagName
	return func() tea.Msg {
		if err := os.Chmod(path, 0o755); err != nil {
			return updVerifiedMsg{err}
		}
		out, err := exec.Command(path, "--version").Output()
		if err != nil {
			return updVerifiedMsg{fmt.Errorf("the new binary doesn't run on this machine: %w", err)}
		}
		if !strings.Contains(string(out), normalizeVersion(tag)) {
			return updVerifiedMsg{fmt.Errorf("the downloaded binary reports %q, expected %s", strings.TrimSpace(string(out)), tag)}
		}
		return updVerifiedMsg{}
	}
}

// ── View ──

func (u *updater) View() string {
	var b strings.Builder
	b.WriteString("\n  " + stBrand.Render("◆ woffux update") + "\n\n")
	switch u.phase {
	case updChecking:
		b.WriteString("  " + u.spin.View() + " " + stSubtle.Render("Looking for a new version…") + "\n")
	case updUpToDate:
		b.WriteString("  " + stIn.Render("✓ ") + stText.Render("You're on the latest version, ") + stBold.Render(u.current) + "\n")
	case updAvailable:
		b.WriteString(u.versionLine() + "\n\n")
		b.WriteString(u.notesView(14))
		b.WriteString("\n  " + lipgloss.NewStyle().Background(obBrand).Foreground(lipgloss.Color("#1c1917")).Bold(true).Padding(0, 1).Render("⏎ Update") +
			"   " + stFaint.Render("esc not now") + "\n")
	case updDownloading:
		b.WriteString(u.versionLine() + "\n\n")
		b.WriteString("  " + u.bar.View() + "  " + stSubtle.Render(fmt.Sprintf("%3.0f%%", u.percent*100)) + "\n")
		b.WriteString("  " + stFaint.Render("Downloading…") + "\n")
	case updVerifying:
		b.WriteString(u.versionLine() + "\n\n")
		b.WriteString("  " + u.bar.ViewAs(1) + "\n")
		b.WriteString("  " + u.spin.View() + " " + stSubtle.Render("Checking the new binary works…") + "\n")
	case updReady:
		b.WriteString(u.versionLine() + "\n\n")
		b.WriteString("  " + u.bar.ViewAs(1) + "\n")
		b.WriteString("  " + stIn.Render("✓ ") + stText.Render("Downloaded and verified") + "\n")
	case updFailed:
		b.WriteString("  " + stBad.Render("✗ ") + stText.Render(u.err.Error()) + "\n\n")
		b.WriteString("  " + stFaint.Render("Nothing was changed. Manual download: https://github.com/ngavilan-dogfy/woffux/releases/latest") + "\n")
	case updCancelled:
		b.WriteString("  " + stFaint.Render("Not updated. Run woffux update whenever you like.") + "\n")
	}
	return b.String() + "\n"
}

func (u *updater) versionLine() string {
	return "  " + stSubtle.Render(u.current) + stFaint.Render("  →  ") + stBrand.Render(u.release.TagName)
}

// notesView lists what's new, newest version first, capped at max lines.
func (u *updater) notesView(maxLines int) string {
	if len(u.notes) == 0 {
		return "  " + stFaint.Render("What's new: github.com/ngavilan-dogfy/woffux/releases") + "\n"
	}
	var lines []string
	for _, n := range u.notes {
		lines = append(lines, "  "+stBold.Render(n.tag))
		for _, bl := range n.bullets {
			lines = append(lines, "    "+stBrand.Render("•")+" "+stText.Render(truncateRunes(bl, max(30, u.width-8))))
		}
	}
	if len(lines) > maxLines {
		extra := len(lines) - maxLines
		lines = append(lines[:maxLines], "  "+stFaint.Render(fmt.Sprintf("… and %d more", extra)))
	}
	return strings.Join(lines, "\n") + "\n"
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// ── Release notes ──

var (
	noteBullet  = regexp.MustCompile(`^\s*[*-]\s+(.*)$`)
	noteTrailer = regexp.MustCompile(`\s+by @\S+ in \S+$`)
	notePrefix  = regexp.MustCompile(`^(feat|fix|perf|docs|chore|refactor|test)(\([^)]*\))?!?:\s*`)
)

// parseReleaseNotes turns GitHub's generated notes into short bullets:
// "* feat(cli): redo every command by @x in https://…" → "redo every command".
func parseReleaseNotes(body string) []string {
	var out []string
	for _, line := range strings.Split(body, "\n") {
		m := noteBullet.FindStringSubmatch(line)
		if m == nil || strings.Contains(line, "Full Changelog") || strings.Contains(line, "made their first contribution") {
			continue
		}
		text := noteTrailer.ReplaceAllString(strings.TrimSpace(m[1]), "")
		text = notePrefix.ReplaceAllString(text, "")
		text = regexp.MustCompile(`\s*\(#\d+\)$`).ReplaceAllString(text, "")
		if text == "" {
			continue
		}
		r := []rune(text)
		r[0] = []rune(strings.ToUpper(string(r[0])))[0]
		out = append(out, string(r))
	}
	return out
}

// fetchNotesSince collects notes for every release newer than current, up
// to latest. Best effort: without API access it returns nothing.
func fetchNotesSince(current, latest string) []releaseNote {
	req, err := http.NewRequest("GET", strings.TrimSuffix(releasesAPI, "/latest")+"?per_page=20", nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if tok := githubToken(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := updateHTTPClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		if resp != nil {
			resp.Body.Close()
		}
		return notesFromAtom(current, latest)
	}
	defer resp.Body.Close()
	var rels []struct {
		TagName string `json:"tag_name"`
		Body    string `json:"body"`
	}
	if json.NewDecoder(resp.Body).Decode(&rels) != nil {
		return nil
	}
	var out []releaseNote
	for _, r := range rels {
		if !versionNewer(r.TagName, current) || versionNewer(r.TagName, latest) {
			continue
		}
		if b := parseReleaseNotes(r.Body); len(b) > 0 {
			out = append(out, releaseNote{tag: r.TagName, bullets: b})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return versionNewer(out[i].tag, out[j].tag) })
	return out
}

// versionNewer reports a > b for vMAJOR.MINOR.PATCH tags ("dev" is oldest).
func versionNewer(a, b string) bool {
	pa, pb := versionParts(a), versionParts(b)
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return false
}

func versionParts(v string) [3]int {
	var p [3]int
	fmt.Sscanf(normalizeVersion(v), "%d.%d.%d", &p[0], &p[1], &p[2])
	return p
}

// releasesAtom is GitHub's public release feed: same notes, no API quota.
var releasesAtom = "https://github.com/ngavilan-dogfy/woffux/releases.atom"

var (
	atomEntry = regexp.MustCompile(`(?s)<entry>.*?<title>([^<]+)</title>.*?<content type="html">(.*?)</content>`)
	htmlLi    = regexp.MustCompile(`(?s)<li>(.*?)</li>`)
	htmlTag   = regexp.MustCompile(`<[^>]+>`)
)

func notesFromAtom(current, latest string) []releaseNote {
	resp, err := updateHTTPClient.Get(releasesAtom)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil || resp.StatusCode != 200 {
		return nil
	}
	return parseAtomNotes(string(body), current, latest)
}

func parseAtomNotes(feed, current, latest string) []releaseNote {
	var out []releaseNote
	for _, m := range atomEntry.FindAllStringSubmatch(feed, -1) {
		tag := strings.TrimSpace(m[1])
		if !versionNewer(tag, current) || versionNewer(tag, latest) {
			continue
		}
		content := html.UnescapeString(m[2])
		var md strings.Builder
		for _, li := range htmlLi.FindAllStringSubmatch(content, -1) {
			md.WriteString("* " + strings.TrimSpace(html.UnescapeString(htmlTag.ReplaceAllString(li[1], ""))) + "\n")
		}
		if b := parseReleaseNotes(md.String()); len(b) > 0 {
			out = append(out, releaseNote{tag: tag, bullets: b})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return versionNewer(out[i].tag, out[j].tag) })
	return out
}
