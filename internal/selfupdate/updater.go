package selfupdate

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// The updater is a small inline Bubble Tea app, the same in every tool:
// check → what's new → confirm → download (progress) → verify → install.

var (
	stAccent = lipgloss.NewStyle().Foreground(lipgloss.Color("5")).Bold(true)
	stOK     = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	stBad    = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)
	stMuted  = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	stBold   = lipgloss.NewStyle().Bold(true)
	stKey    = lipgloss.NewStyle().Foreground(lipgloss.Color("5")).Reverse(true).Bold(true).Padding(0, 1) // reverse: the text takes the terminal's own background, readable in any theme
)

type phase int

const (
	phChecking phase = iota
	phUpToDate
	phAvailable
	phDownloading
	phVerifying
	phReady
	phFailed
	phCancelled
)

// Options controls one update run.
type Options struct {
	Yes         bool      // don't ask for confirmation
	Interactive bool      // a person at a terminal: show the inline UI
	Out         io.Writer // where plain progress goes when not interactive
	// OnInstalled runs after a new version is in place, with its path: the
	// hook for work only the new binary can do (refreshing files it embeds).
	OnInstalled func(path string)
}

// Run checks for a newer release and installs it. It returns nil when the
// tool is already up to date or the user declines.
func Run(t Tool, opts Options) error {
	if opts.Out == nil {
		opts.Out = os.Stdout
	}
	target := InstallPath(t)
	if !opts.Interactive {
		return runPlain(t, opts, target)
	}
	m := newModel(t, opts.Yes, target)
	p := tea.NewProgram(m)
	m.program = p
	if _, err := p.Run(); err != nil {
		return err
	}
	if m.phase != phReady {
		m.cleanup()
		if m.phase == phFailed {
			return errQuiet
		}
		return nil
	}
	if err := m.install(opts.Out); err != nil {
		return err
	}
	if opts.OnInstalled != nil {
		opts.OnInstalled(m.target)
	}
	return nil
}

// errQuiet signals a failure the UI already explained.
var errQuiet = errors.New("update failed")

// IsQuiet reports an error the updater already printed.
func IsQuiet(err error) bool { return errors.Is(err, errQuiet) }

type (
	checkedMsg struct {
		rel   Release
		notes []Note
		err   error
	}
	progressMsg   float64
	downloadedMsg struct{ err error }
	verifiedMsg   struct {
		err       error
		checksumd bool
	}
)

type model struct {
	t        Tool
	autoYes  bool
	target   string
	phase    phase
	rel      Release
	notes    []Note
	err      error
	tmp      string
	percent  float64
	summed   bool
	frame    int
	width    int
	program  *tea.Program
	started  time.Time
	finished bool
}

func newModel(t Tool, yes bool, target string) *model {
	return &model{t: t, autoYes: yes, target: target, width: 80}
}

type tickMsg struct{}

func tick() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m *model) Init() tea.Cmd {
	t := m.t
	return tea.Batch(tick(), func() tea.Msg {
		rel, err := Latest(t)
		if err != nil {
			return checkedMsg{err: err}
		}
		return checkedMsg{rel: rel, notes: NotesSince(t, t.Current, rel.Tag)}
	})
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case tickMsg:
		m.frame++
		if m.phase == phChecking || m.phase == phDownloading || m.phase == phVerifying {
			return m, tick()
		}
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc", "n":
			if m.phase == phDownloading || m.phase == phVerifying {
				return m, nil // never leave a half-written file behind
			}
			if m.phase == phAvailable {
				m.phase = phCancelled
			}
			return m, tea.Quit
		case "enter", "y":
			switch m.phase {
			case phAvailable:
				return m, m.startDownload()
			case phUpToDate, phFailed, phReady:
				return m, tea.Quit
			}
		}
	case checkedMsg:
		if msg.err != nil {
			m.phase, m.err = phFailed, checkErr(m.t, msg.err)
			return m, tea.Quit
		}
		m.rel, m.notes = msg.rel, msg.notes
		if IsRelease(m.t.Current) && !Newer(m.rel.Tag, m.t.Current) {
			m.phase = phUpToDate
			return m, tea.Quit
		}
		if m.rel.Asset(PlatformAsset(m.t)) == "" {
			m.phase, m.err = phFailed, fmt.Errorf("release %s has no build for %s/%s", m.rel.Tag, runtime.GOOS, runtime.GOARCH)
			return m, tea.Quit
		}
		m.phase = phAvailable
		if m.autoYes {
			return m, m.startDownload()
		}
	case progressMsg:
		m.percent = float64(msg)
	case downloadedMsg:
		if msg.err != nil {
			m.phase, m.err = phFailed, msg.err
			return m, tea.Quit
		}
		m.phase, m.percent = phVerifying, 1
		return m, tea.Batch(tick(), m.verify())
	case verifiedMsg:
		if msg.err != nil {
			m.phase, m.err = phFailed, msg.err
			return m, tea.Quit
		}
		m.summed = msg.checksumd
		m.phase = phReady
		return m, tea.Quit
	}
	return m, nil
}

func checkErr(t Tool, err error) error {
	if errors.Is(err, ErrNoReleases) {
		return fmt.Errorf("no releases published yet — install from source: go install github.com/%s/cmd/%s@latest", t.Repo, t.Binary)
	}
	return fmt.Errorf("couldn't check for updates: %w", err)
}

// tempFor creates the download next to the target when possible, so the
// final rename is atomic and never crosses filesystems.
func tempFor(target, binary string) (*os.File, error) {
	if f, err := os.CreateTemp(filepath.Dir(target), "."+binary+"-update-*"); err == nil {
		return f, nil
	}
	return os.CreateTemp("", binary+"-update-*")
}

func (m *model) startDownload() tea.Cmd {
	m.phase, m.started = phDownloading, time.Now()
	f, err := tempFor(m.target, m.t.Binary)
	if err != nil {
		m.phase, m.err = phFailed, err
		return tea.Quit
	}
	m.tmp = f.Name()
	url, p := m.rel.Asset(PlatformAsset(m.t)), m.program
	return func() tea.Msg {
		defer f.Close()
		_, err := Download(url, f, func(v float64) {
			if p != nil {
				p.Send(progressMsg(v))
			}
		})
		return downloadedMsg{err}
	}
}

func (m *model) verify() tea.Cmd {
	path, rel, t := m.tmp, m.rel, m.t
	return func() tea.Msg {
		summed, err := verifyBinary(t, rel, path)
		return verifiedMsg{err: err, checksumd: summed}
	}
}

// verifyBinary checks the download against checksums.txt (when the release
// has one) and that it runs and reports the new version, so a broken or
// wrong file never replaces a working install.
func verifyBinary(t Tool, rel Release, path string) (bool, error) {
	summed := false
	if u := rel.Asset("checksums.txt"); u != "" {
		var sums strings.Builder
		if _, err := Download(u, &sums, nil); err == nil {
			found, err := VerifyChecksum(path, PlatformAsset(t), []byte(sums.String()))
			if err != nil {
				return false, err
			}
			summed = found
		}
	}
	if err := os.Chmod(path, 0o755); err != nil {
		return summed, err
	}
	out, err := exec.Command(path, "--version").Output()
	if err != nil {
		return summed, fmt.Errorf("the new binary doesn't run on this machine: %v", err)
	}
	if !strings.Contains(string(out), Normalize(rel.Tag)) {
		return summed, fmt.Errorf("the downloaded binary reports %q, expected %s", strings.TrimSpace(string(out)), rel.Tag)
	}
	return summed, nil
}

func (m *model) cleanup() {
	if m.tmp != "" {
		os.Remove(m.tmp)
	}
}

// install swaps the verified binary in, asking for sudo only when the
// install folder needs it.
func (m *model) install(out io.Writer) error {
	defer m.cleanup()
	if err := swapIn(m.tmp, m.target, out); err != nil {
		fmt.Fprintf(out, "  %s %v\n\n", stBad.Render("×"), err)
		return errQuiet
	}
	fmt.Fprintf(out, "  %s %s %s installed %s\n", stOK.Render("●"), stBold.Render(m.t.Binary), stBold.Render(m.rel.Tag), stMuted.Render(tilde(m.target)))
	if m.rel.URL != "" {
		fmt.Fprintf(out, "  %s\n", stMuted.Render("What's new: "+m.rel.URL))
	}
	fmt.Fprintln(out)
	return nil
}

func swapIn(tmp, target string, out io.Writer) error {
	if runtime.GOOS == "windows" {
		old := target + ".old"
		os.Remove(old)
		if err := os.Rename(target, old); err != nil && !os.IsNotExist(err) {
			return err
		}
		return os.Rename(tmp, target)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err == nil {
		if err := os.Rename(tmp, target); err == nil {
			return nil
		}
		if err := exec.Command("mv", "-f", tmp, target).Run(); err == nil {
			return nil
		}
	}
	fmt.Fprintf(out, "  %s %s needs admin rights — you'll be asked for your password.\n", stMuted.Render("·"), tilde(filepath.Dir(target)))
	sudo := exec.Command("sudo", "mv", "-f", tmp, target)
	sudo.Stdin, sudo.Stdout, sudo.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := sudo.Run(); err != nil {
		return fmt.Errorf("couldn't install to %s: %v (try: sudo mv %q %q)", target, err, tmp, target)
	}
	return nil
}

// InstallPath is where the new binary goes: this executable, unless it's a
// throwaway build or has another name, then the one on PATH, then
// ~/.local/bin.
func InstallPath(t Tool) string {
	if exe, err := os.Executable(); err == nil && !avoidReplacing(exe, t.Binary) {
		if r, err := filepath.EvalSymlinks(exe); err == nil && !avoidReplacing(r, t.Binary) {
			return r
		}
		return exe
	}
	if p, err := exec.LookPath(t.Binary); err == nil && !avoidReplacing(p, t.Binary) {
		return p
	}
	home, _ := os.UserHomeDir()
	name := t.Binary
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(home, ".local", "bin", name)
}

func avoidReplacing(path, binary string) bool {
	path = filepath.Clean(strings.TrimSpace(path))
	base := strings.TrimSuffix(filepath.Base(path), ".exe")
	if path == "." || base != binary {
		return true
	}
	if strings.Contains(path, string(filepath.Separator)+"go-build") {
		return true
	}
	if tmp := os.TempDir(); tmp != "" {
		if rel, err := filepath.Rel(tmp, path); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			return true
		}
	}
	return false
}

func tilde(p string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, home+string(filepath.Separator)) {
		return "~" + p[len(home):]
	}
	return p
}

// ─── view ────────────────────────────────────────────────────────

var spinFrames = []string{"◐", "◓", "◑", "◒"}

func (m *model) View() string {
	var b strings.Builder
	b.WriteString("\n  " + stAccent.Render(m.t.Binary+" update") + "\n\n")
	spin := stAccent.Render(spinFrames[m.frame%len(spinFrames)])
	switch m.phase {
	case phChecking:
		b.WriteString("  " + spin + " " + stMuted.Render("Looking for a new version…") + "\n")
	case phUpToDate:
		b.WriteString("  " + stOK.Render("●") + " You're on the latest version, " + stBold.Render(m.t.Current) + "\n")
	case phAvailable:
		b.WriteString(m.versionLine() + "\n\n")
		b.WriteString(m.notesView(14))
		b.WriteString("\n  " + stKey.Render("⏎ Update") + "   " + stMuted.Render("esc not now") + "\n")
	case phDownloading:
		b.WriteString(m.versionLine() + "\n\n")
		b.WriteString("  " + bar(m.percent, min(44, max(20, m.width-20))) + "  " + stMuted.Render(fmt.Sprintf("%3.0f%%", m.percent*100)) + "\n")
		b.WriteString("  " + stMuted.Render("Downloading…") + "\n")
	case phVerifying:
		b.WriteString(m.versionLine() + "\n\n")
		b.WriteString("  " + bar(1, min(44, max(20, m.width-20))) + "\n")
		b.WriteString("  " + spin + " " + stMuted.Render("Checking the download and that it runs…") + "\n")
	case phReady:
		b.WriteString(m.versionLine() + "\n\n")
		check := "Downloaded and verified"
		if m.summed {
			check += " (checksum ok)"
		}
		b.WriteString("  " + stOK.Render("●") + " " + check + "\n")
	case phFailed:
		b.WriteString("  " + stBad.Render("×") + " " + m.err.Error() + "\n\n")
		b.WriteString("  " + stMuted.Render("Nothing was changed. Download it by hand: https://github.com/"+m.t.Repo+"/releases/latest") + "\n")
	case phCancelled:
		b.WriteString("  " + stMuted.Render("Not updated. Run '"+m.t.Binary+" update' whenever you like.") + "\n")
	}
	return b.String() + "\n"
}

func (m *model) versionLine() string {
	cur := m.t.Current
	if cur == "" {
		cur = "dev"
	}
	return "  " + stMuted.Render(cur) + stMuted.Render("  →  ") + stAccent.Render(m.rel.Tag)
}

// notesView lists what's new, newest version first, capped at max lines.
func (m *model) notesView(maxLines int) string {
	if len(m.notes) == 0 {
		return "  " + stMuted.Render("What's new: github.com/"+m.t.Repo+"/releases") + "\n"
	}
	var lines []string
	for _, n := range m.notes {
		lines = append(lines, "  "+stBold.Render(n.Tag))
		for _, bl := range n.Bullets {
			lines = append(lines, "    "+stAccent.Render("•")+" "+truncRunes(bl, max(30, m.width-8)))
		}
	}
	if len(lines) > maxLines {
		extra := len(lines) - maxLines
		lines = append(lines[:maxLines], "  "+stMuted.Render(fmt.Sprintf("… and %d more", extra)))
	}
	return strings.Join(lines, "\n") + "\n"
}

// bar draws a progress bar with eighth blocks.
func bar(v float64, width int) string {
	v = max(0, min(1, v))
	eighths := int(v * float64(width*8))
	full, part := eighths/8, eighths%8
	s := strings.Repeat("█", full)
	if part > 0 && full < width {
		s += string([]rune(" ▏▎▍▌▋▊▉")[part])
		full++
	}
	return stAccent.Render(s) + stMuted.Render(strings.Repeat("░", max(0, width-full)))
}

func truncRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// ─── without a terminal ──────────────────────────────────────────

func runPlain(t Tool, opts Options, target string) error {
	out := opts.Out
	rel, err := Latest(t)
	if err != nil {
		return checkErr(t, err)
	}
	if IsRelease(t.Current) && !Newer(rel.Tag, t.Current) {
		fmt.Fprintf(out, "%s is up to date (%s)\n", t.Binary, t.Current)
		return nil
	}
	url := rel.Asset(PlatformAsset(t))
	if url == "" {
		return fmt.Errorf("release %s has no build for %s/%s", rel.Tag, runtime.GOOS, runtime.GOARCH)
	}
	f, err := tempFor(target, t.Binary)
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	fmt.Fprintf(out, "downloading %s %s…\n", t.Binary, rel.Tag)
	_, err = Download(url, f, nil)
	f.Close()
	if err != nil {
		return err
	}
	summed, err := verifyBinary(t, rel, tmp)
	if err != nil {
		return err
	}
	if err := swapIn(tmp, target, out); err != nil {
		return err
	}
	check := ""
	if summed {
		check = ", checksum verified"
	}
	fmt.Fprintf(out, "updated %s %s → %s (%s%s)\n", t.Binary, t.Current, rel.Tag, target, check)
	if opts.OnInstalled != nil {
		opts.OnInstalled(target)
	}
	return nil
}
