package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/huh/spinner"
	"github.com/spf13/cobra"

	"github.com/ngavilan-dogfy/woffux/internal/agent"
	"github.com/ngavilan-dogfy/woffux/internal/config"
	gh "github.com/ngavilan-dogfy/woffux/internal/github"
	"github.com/ngavilan-dogfy/woffux/internal/selfupdate"
	"github.com/ngavilan-dogfy/woffux/internal/woffu"
)

// woffux doctor first gathers facts about this install (the probes touch
// the system, Woffu and GitHub, but only to read), then judges them with
// pure functions, so every verdict is tested with made-up facts.

// check is one line of the report: what was checked, how it went and,
// when something's off, how to fix it.
type check struct {
	Section string `json:"section"`
	Status  string `json:"status"` // ok, warn, fail, info
	Detail  string `json:"detail"`
	Fix     string `json:"fix,omitempty"`
}

var doctorJSON bool

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check everything, with a fix for each problem",
	Long: `Check this install from top to bottom and say how to fix anything that's
off: the binary and your PATH, your settings and the keychain, signing in
to Woffu, this Mac's signer, the GitHub backup (its workflow, secrets and
last run), the Claude Code skill, and when the next automatic sign is.

It only reads: nothing is signed, sent or changed — except that a settings
file readable by other accounts is made private, as every command does.

Output:
  Terminal: a checklist with fixes · --json: {"ok": bool, "checks": [...]}
  Exit code 1 when a check fails (warnings don't count).

Examples:
  woffux doctor
  woffux doctor --json | jq '.checks[] | select(.status != "ok")'`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		var f doctorFacts
		if doctorJSON || !isTTY() {
			f = gatherFacts()
		} else {
			spinner.New().Title("Checking your install, Woffu and GitHub…").Action(func() { f = gatherFacts() }).Run()
		}
		return reportDoctor(judge(f), doctorJSON)
	},
}

func init() {
	doctorCmd.Flags().BoolVar(&doctorJSON, "json", false, "Output as JSON")
	rootCmd.AddCommand(doctorCmd)
}

// ─── facts ───────────────────────────────────────────────────────

type binaryInfo struct {
	Path     string
	Version  string
	Writable bool // its folder can be written without sudo
}

type doctorFacts struct {
	Now time.Time

	Self      binaryInfo
	OnPath    []binaryInfo // every woffux in PATH, in PATH order
	Latest    string
	LatestErr error

	ConfigPath    string
	ConfigExists  bool
	ConfigWasOpen bool // readable by other accounts before this run
	Cfg           *config.Config
	CfgErr        error
	HasPassword   bool
	Profile       *woffu.UserProfile
	SignInErr     error

	Agent  agentFacts
	GitHub githubFacts

	ClaudeCode     bool
	SkillInstalled bool
	SkillCurrent   bool
}

type agentFacts struct {
	Supported    bool
	Installed    bool
	Loaded       bool
	Binary       binaryInfo
	BinaryExists bool
	LogAge       time.Duration // < 0: nothing logged yet
	LastLine     string
}

func (a agentFacts) active() bool { return a.Installed && a.Loaded && a.BinaryExists }

type githubFacts struct {
	Repo          string
	GhMissing     bool
	Login         string
	LoginErr      error
	StatusErr     error
	WorkflowFound bool
	Enabled       bool
	InSync        bool
	SyncErr       error
	Missing       []string
	SecretsErr    error
	LastRunAt     time.Time
	LastRunResult string // success, failure… ("" when it never ran)
	LastRunErr    error
}

func (g githubFacts) active() bool { return g.Repo != "" && g.Enabled && g.StatusErr == nil }

// gatherFacts runs the probes; the slow ones (Woffu, GitHub, the latest
// release) in parallel.
func gatherFacts() doctorFacts {
	f := doctorFacts{Now: time.Now()}

	f.Self = binaryInfo{Version: currentBuild().Short()}
	if exe, err := os.Executable(); err == nil {
		if r, err := filepath.EvalSymlinks(exe); err == nil {
			exe = r
		}
		f.Self.Path, f.Self.Writable = exe, dirWritable(filepath.Dir(exe))
	}
	f.OnPath = woffuxOnPath()

	f.ConfigPath, _ = config.Path()
	if info, err := os.Stat(f.ConfigPath); err == nil {
		f.ConfigExists = true
		f.ConfigWasOpen = info.Mode().Perm()&0o077 != 0
		f.Cfg, f.CfgErr = config.Load() // also makes the file private
	}
	var password string
	if f.Cfg != nil && f.Cfg.WoffuEmail != "" {
		pw, err := config.GetPassword(f.Cfg.WoffuEmail)
		f.HasPassword, password = err == nil && pw != "", pw
	}

	f.Agent = agentProbe()
	f.ClaudeCode = claudeCodeDetected()
	f.SkillInstalled, f.SkillCurrent = skillInstalled(), skillCurrent()

	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		rel, err := selfupdate.Latest(updateTool())
		f.Latest, f.LatestErr = rel.Tag, err
	}()
	go func() {
		defer wg.Done()
		if f.HasPassword && f.Cfg.WoffuCompanyURL != "" {
			client, companyClient := woffu.NewWoffuClient(f.Cfg.WoffuURL), woffu.NewCompanyClient(f.Cfg.WoffuCompanyURL)
			token, err := woffu.AuthenticateCached(client, companyClient, f.Cfg.WoffuEmail, password)
			if err == nil {
				f.Profile, err = woffu.GetUserProfile(companyClient, token)
			}
			f.SignInErr = err
		}
	}()
	go func() {
		defer wg.Done()
		if f.Cfg != nil && f.Cfg.GithubFork != "" {
			f.GitHub = githubProbe(f.Cfg)
		}
	}()
	wg.Wait()
	return f
}

func agentProbe() agentFacts {
	a := agentFacts{Supported: agent.Supported(), LogAge: -1}
	if !a.Supported {
		return a
	}
	a.Installed, a.Loaded = agent.Installed(), agent.Loaded()
	if bin := agent.InstalledBinary(); bin != "" {
		a.Binary = binaryInfo{Path: bin}
		if info, err := os.Stat(bin); err == nil && !info.IsDir() {
			a.BinaryExists = true
			a.Binary.Version = versionOf(bin)
		}
	}
	if info, err := os.Stat(agent.LogPath()); err == nil {
		a.LogAge = time.Since(info.ModTime())
	}
	if lines := agent.RecentLog(1); len(lines) > 0 {
		a.LastLine = lines[0]
	}
	return a
}

func githubProbe(cfg *config.Config) githubFacts {
	g := githubFacts{Repo: cfg.GithubFork}
	if _, err := exec.LookPath("gh"); err != nil {
		g.GhMissing = true
		return g
	}
	if g.Login, g.LoginErr = gh.Login(g.Repo); g.LoginErr != nil {
		return g
	}
	workflows, err := gh.GetAutoSignStatus(g.Repo)
	if g.StatusErr = err; err != nil {
		return g
	}
	for _, w := range workflows {
		if w.Name == "Auto Sign" {
			g.WorkflowFound, g.Enabled = true, w.State == "active"
		}
	}
	g.InSync, g.SyncErr = gh.CheckWorkflowSync(g.Repo, cfg)
	if names, err := gh.SecretNames(g.Repo); err != nil {
		g.SecretsErr = err
	} else {
		g.Missing = gh.MissingSecrets(names)
	}
	at, result, ok, err := gh.LastScheduledRun(g.Repo)
	g.LastRunErr = err
	if ok {
		g.LastRunAt, _ = time.Parse(time.RFC3339, at)
		g.LastRunResult = result
	}
	return g
}

// woffuxOnPath lists every woffux executable in PATH, in PATH order, once
// each.
func woffuxOnPath() []binaryInfo {
	var out []binaryInfo
	seen := map[string]bool{}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			continue
		}
		p := filepath.Join(dir, "woffux")
		info, err := os.Stat(p)
		if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
			continue
		}
		real := p
		if r, err := filepath.EvalSymlinks(p); err == nil {
			real = r
		}
		if seen[real] {
			continue
		}
		seen[real] = true
		out = append(out, binaryInfo{Path: p, Version: versionOf(p), Writable: dirWritable(dir)})
	}
	return out
}

// versionOf asks a woffux binary its version ("" when it doesn't answer).
func versionOf(bin string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(out)), "woffux version "))
}

func dirWritable(dir string) bool {
	f, err := os.CreateTemp(dir, ".woffux-doctor-*")
	if err != nil {
		return false
	}
	f.Close()
	os.Remove(f.Name())
	return true
}

// ─── judgement ───────────────────────────────────────────────────

const (
	secInstall  = "Install"
	secSettings = "Settings"
	secMac      = "This Mac signs"
	secGitHub   = "GitHub backup"
	secClaude   = "Claude Code"
	secAuto     = "Autopilot"
)

func judge(f doctorFacts) []check {
	var out []check
	out = append(out, judgeInstall(f)...)
	out = append(out, judgeSettings(f)...)
	out = append(out, judgeAgent(f)...)
	out = append(out, judgeGitHub(f)...)
	out = append(out, judgeSkill(f)...)
	out = append(out, judgeAutopilot(f)...)
	return out
}

func judgeInstall(f doctorFacts) []check {
	c := func(status, detail, fix string) check { return check{secInstall, status, detail, fix} }
	out := []check{c("ok", fmt.Sprintf("woffux %s · %s", f.Self.Version, tildePath(f.Self.Path)), "")}

	switch {
	case !selfupdate.IsRelease(f.Self.Version):
		out = append(out, c("info", "A development build: updates are for release builds", ""))
	case f.LatestErr != nil:
		out = append(out, c("info", "Couldn't check for a newer version: "+f.LatestErr.Error(), ""))
	case selfupdate.Newer(f.Latest, f.Self.Version):
		out = append(out, c("warn", f.Latest+" is out", "woffux update"))
	default:
		out = append(out, c("ok", "Up to date", ""))
	}

	switch {
	case len(f.OnPath) == 0:
		out = append(out, c("warn", "Typing 'woffux' finds nothing: it isn't in your PATH",
			"add "+tildePath(filepath.Dir(f.Self.Path))+" to your PATH, or reinstall: curl -fsSL https://raw.githubusercontent.com/ngavilan-dogfy/woffux/main/install.sh | sh"))
	case !samePath(f.OnPath[0].Path, f.Self.Path):
		out = append(out, c("warn", "Typing 'woffux' runs another copy: "+describeBinary(f.OnPath[0]),
			"remove it, or put "+tildePath(filepath.Dir(f.Self.Path))+" first in your PATH"))
	}
	for i, b := range f.OnPath {
		if samePath(b.Path, f.Self.Path) || i == 0 || samePath(b.Path, f.Agent.Binary.Path) {
			continue
		}
		fix := "rm " + tildePath(b.Path)
		if !b.Writable {
			fix = "sudo " + fix
		}
		out = append(out, c("warn", "Another copy is still at "+describeBinary(b)+"; nothing here uses it", fix))
	}
	return out
}

func judgeSettings(f doctorFacts) []check {
	c := func(status, detail, fix string) check { return check{secSettings, status, detail, fix} }
	path := tildePath(f.ConfigPath)
	switch {
	case !f.ConfigExists:
		return []check{c("fail", "woffux isn't set up yet", "woffux setup")}
	case f.CfgErr != nil:
		return []check{c("fail", path+" can't be read: "+f.CfgErr.Error(), "woffux setup")}
	}
	var out []check
	if f.ConfigWasOpen {
		out = append(out, c("ok", path+" was readable by other accounts; now it's private", ""))
	} else {
		out = append(out, c("ok", path+", readable only by you", ""))
	}
	cfg := f.Cfg
	if cfg.WoffuEmail == "" || cfg.WoffuCompanyURL == "" {
		return append(out, c("fail", "Your settings are incomplete", "woffux setup"))
	}
	if !f.HasPassword {
		return append(out, c("fail", "No Woffu password for "+cfg.WoffuEmail+" in the keychain", "woffux setup"))
	}
	out = append(out, c("ok", "Password for "+cfg.WoffuEmail+" in the keychain", ""))

	switch kind := authKind(f.SignInErr); {
	case f.SignInErr == nil && f.Profile != nil:
		who := f.Profile.FullName
		if f.Profile.CompanyName != "" {
			who += " · " + f.Profile.CompanyName
		}
		out = append(out, c("ok", "Woffu signs you in as "+who, ""))
	case f.SignInErr == nil:
	case kind == woffu.ErrBadPassword || kind == woffu.ErrNoPasswordLogin:
		out = append(out, c("fail", "Woffu rejects the saved password: automatic signs will fail",
			"woffux config edit → Password (then woffux sync if GitHub signs too)"))
	case kind == woffu.ErrNetwork:
		out = append(out, c("warn", "Couldn't reach Woffu to check the sign-in", ""))
	default:
		out = append(out, c("fail", "Signing in to Woffu failed: "+f.SignInErr.Error(), "woffux setup"))
	}

	switch {
	case !coordsConfigured(cfg.Latitude, cfg.Longitude):
		out = append(out, c("fail", "No office location: Woffu would get signs from nowhere", "woffux config edit → Office"))
	case !coordsConfigured(cfg.HomeLatitude, cfg.HomeLongitude):
		out = append(out, c("fail", "No home location for telework days", "woffux config edit → Home"))
	default:
		out = append(out, c("ok", "Office and home locations set", ""))
	}
	if cfg.Schedule.WeekMinutes() == 0 {
		out = append(out, c("warn", "Your week has no working hours: nothing will be signed", "woffux schedule edit"))
	} else {
		out = append(out, c("ok", "Week: "+weekOneLine(cfg.Schedule), ""))
	}
	return out
}

func judgeAgent(f doctorFacts) []check {
	c := func(status, detail, fix string) check { return check{secMac, status, detail, fix} }
	a := f.Agent
	switch {
	case !a.Supported:
		return []check{c("info", "The local signer needs macOS; the GitHub backup signs from anywhere", "")}
	case !a.Installed && f.GitHub.active():
		return []check{c("info", "Off: the GitHub backup signs for you, but GitHub's timers often run late",
			"woffux agent on — signs on time whenever this Mac is awake")}
	case !a.Installed:
		return []check{c("warn", "Off", "woffux agent on — signs on time whenever this Mac is awake")}
	case a.Binary.Path == "":
		return []check{c("fail", "Its launchd file doesn't say which woffux to run", "woffux agent on")}
	case !a.BinaryExists:
		return []check{c("fail", "It runs "+tildePath(a.Binary.Path)+", which doesn't exist anymore: nothing signs", "woffux agent on")}
	case !a.Loaded:
		return []check{c("fail", "Installed, but launchd isn't running it: nothing signs", "woffux agent on")}
	}

	var out []check
	if samePath(a.Binary.Path, f.Self.Path) {
		out = append(out, c("ok", "On · runs "+describeBinary(a.Binary)+" at "+agent.MinutesLabel(), ""))
	} else {
		out = append(out, c("warn", "It runs another woffux: "+describeBinary(a.Binary),
			"woffux agent on — switches it to "+tildePath(f.Self.Path)))
	}
	switch {
	case strings.HasPrefix(a.LastLine, "Error"):
		out = append(out, c("fail", "The last run failed: "+strings.TrimSpace(strings.TrimPrefix(a.LastLine, "Error:")), "woffux agent status"))
	case a.LogAge < 0:
		out = append(out, c("info", "No runs logged yet", ""))
	case a.LogAge > 40*time.Minute:
		out = append(out, c("warn", "Last ran "+fmtAge(a.LogAge)+" ago; it runs every 15 minutes while the Mac is awake",
			"normal if this Mac just woke up; otherwise: woffux agent on"))
	default:
		out = append(out, c("ok", "Last ran "+fmtAge(a.LogAge)+" ago", ""))
	}
	return out
}

func judgeGitHub(f doctorFacts) []check {
	c := func(status, detail, fix string) check { return check{secGitHub, status, detail, fix} }
	g := f.GitHub
	switch {
	case f.Cfg == nil:
		return nil
	case g.Repo == "" && f.Agent.active():
		return []check{c("info", "Not set up: optional, it signs when this Mac is asleep", "woffux setup → Who signs")}
	case g.Repo == "":
		return []check{c("info", "Not set up", "woffux setup → Who signs")}
	case g.GhMissing:
		return []check{c("fail", "The GitHub CLI (gh) isn't installed: woffux needs it to look after "+g.Repo, "brew install gh && gh auth login")}
	case g.LoginErr != nil:
		return []check{c("fail", "gh isn't signed in to GitHub", "gh auth login")}
	case g.StatusErr != nil:
		return []check{c("fail", "Couldn't read "+g.Repo+"'s workflows: "+g.StatusErr.Error(), "woffux setup → Who signs")}
	case !g.WorkflowFound:
		return []check{c("fail", "There's no Auto Sign workflow on "+g.Repo, "woffux sync")}
	}

	out := []check{}
	if g.Enabled {
		out = append(out, c("ok", "On · "+g.Repo+" as "+g.Login, ""))
	} else {
		out = append(out, c("info", "Paused on "+g.Repo, "woffux auto on"))
	}
	switch {
	case g.SyncErr != nil:
		out = append(out, c("warn", "Couldn't compare its workflow with your settings: "+g.SyncErr.Error(), ""))
	case !g.InSync:
		out = append(out, c("fail", "Its workflow doesn't match your settings: GitHub would sign at other times", "woffux sync"))
	default:
		out = append(out, c("ok", "Workflow matches your settings", ""))
	}
	switch {
	case g.SecretsErr != nil:
		out = append(out, c("warn", "Couldn't list its secrets: "+g.SecretsErr.Error(), ""))
	case len(g.Missing) > 0:
		out = append(out, c("fail", "Missing secrets: "+strings.Join(g.Missing, ", "), "woffux sync"))
	default:
		out = append(out, c("ok", "Credentials and locations stored as encrypted secrets", ""))
	}
	switch {
	case g.LastRunErr != nil:
		out = append(out, c("warn", "Couldn't read its last run: "+g.LastRunErr.Error(), ""))
	case g.LastRunResult == "":
		out = append(out, c("info", "No scheduled runs yet", ""))
	case g.LastRunResult == "success":
		out = append(out, c("ok", "Last scheduled run "+fmtWhen(g.LastRunAt, f.Now)+" succeeded", ""))
	default:
		out = append(out, c("fail", "Last scheduled run "+fmtWhen(g.LastRunAt, f.Now)+": "+g.LastRunResult, "woffux open github"))
	}
	return out
}

func judgeSkill(f doctorFacts) []check {
	c := func(status, detail, fix string) check { return check{secClaude, status, detail, fix} }
	switch {
	case !f.ClaudeCode:
		return nil
	case !f.SkillInstalled:
		return []check{c("info", "The /woffux skill isn't installed", "woffux skill install")}
	case !f.SkillCurrent:
		return []check{c("warn", "The /woffux skill is from another version of woffux", "woffux skill install")}
	}
	return []check{c("ok", "The /woffux skill is installed and current", "")}
}

func judgeAutopilot(f doctorFacts) []check {
	c := func(status, detail, fix string) check { return check{secAuto, status, detail, fix} }
	if f.Cfg == nil || f.CfgErr != nil {
		return nil
	}
	mac, github := f.Agent.active(), f.GitHub.active()
	if !mac && !github {
		return []check{c("warn", "Nobody signs automatically: woffux only shows your status", "woffux agent on · woffux auto on")}
	}
	when, dir, ok := nextAutomaticSign(f.Cfg, f.Now)
	if !ok {
		return []check{c("info", "No automatic sign in the next two weeks", "")}
	}
	by := signerPlan{mac: mac, github: github}.String()
	return []check{c("ok", "Next sign: "+dir+" "+when.Format("Mon 2 Jan · 15:04")+" · "+by, "")}
}

func describeBinary(b binaryInfo) string {
	if b.Version == "" {
		return tildePath(b.Path)
	}
	return tildePath(b.Path) + " (" + b.Version + ")"
}

func fmtAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "less than a minute"
	case d < time.Hour:
		return fmt.Sprintf("%d min", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d h", int(d.Hours()))
	}
	return fmt.Sprintf("%d days", int(d.Hours()/24))
}

func fmtWhen(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	t = t.Local()
	if y, m, d := now.Date(); t.Year() == y && t.Month() == m && t.Day() == d {
		return "today at " + t.Format("15:04")
	}
	return t.Format("Mon 2 Jan 15:04")
}

// ─── report ──────────────────────────────────────────────────────

func reportDoctor(checks []check, jsonOut bool) error {
	failed, warned := 0, 0
	for _, c := range checks {
		switch c.Status {
		case "fail":
			failed++
		case "warn":
			warned++
		}
	}
	if jsonOut {
		if err := printJSON(map[string]any{"ok": failed == 0, "checks": checks}); err != nil {
			return err
		}
	} else if !isTTY() {
		printTSV([]string{"section", "status", "detail", "fix"}, checkRows(checks))
	} else {
		uiTitle("woffux doctor")
		section := ""
		for _, c := range checks {
			if c.Section != section {
				section = c.Section
				uiSection(section)
			}
			printCheck(c)
		}
		fmt.Println()
		switch {
		case failed > 0:
			uiLine(stBad.Render(fmt.Sprintf("%d %s to fix", failed, pluralS(failed, "problem", "problems"))) + stFaint.Render(" — each one says how"))
		case warned > 0:
			uiLine(stOut.Render(fmt.Sprintf("%d %s worth a look", warned, pluralS(warned, "thing", "things"))) + stFaint.Render(", nothing broken"))
		default:
			uiLine(stIn.Render("All good."))
		}
		fmt.Println()
	}
	if failed > 0 {
		return quietError{errors.New("some checks failed")}
	}
	return nil
}

func printCheck(c check) {
	mark := map[string]string{
		"ok":   stIn.Render("✓"),
		"warn": stOut.Render("!"),
		"fail": stBad.Render("✗"),
		"info": stFaint.Render("·"),
	}[c.Status]
	uiLine(mark + " " + stText.Render(c.Detail))
	if c.Fix != "" {
		uiLine("  " + stFaint.Render("→ "+c.Fix))
	}
}

func checkRows(checks []check) [][]string {
	rows := make([][]string, 0, len(checks))
	for _, c := range checks {
		rows = append(rows, []string{c.Section, c.Status, c.Detail, c.Fix})
	}
	return rows
}
