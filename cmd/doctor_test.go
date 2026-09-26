package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ngavilan-dogfy/woffux/internal/config"
	"github.com/ngavilan-dogfy/woffux/internal/woffu"
)

// healthyFacts is an install where everything works: this Mac signs with
// the binary on PATH, GitHub backs it up, the skill is current.
func healthyFacts(t *testing.T) doctorFacts {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "woffux")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		WoffuURL: "https://app.woffu.com/api", WoffuCompanyURL: "https://acme.woffu.com", WoffuEmail: "ana@acme.com",
		Latitude: 40.4168, Longitude: -3.7038, HomeLatitude: 40.43, HomeLongitude: -3.70,
		GithubFork: "ana/woffux", Schedule: config.DefaultSchedule(),
	}
	self := binaryInfo{Path: bin, Version: "v5.14.0", Writable: true}
	return doctorFacts{
		Now:          time.Date(2026, 9, 28, 7, 0, 0, 0, time.Local), // a Monday morning
		Self:         self,
		OnPath:       []binaryInfo{self},
		Latest:       "v5.14.0",
		ConfigPath:   "/home/ana/.woffux.yaml",
		ConfigExists: true,
		Cfg:          cfg,
		HasPassword:  true,
		Profile:      &woffu.UserProfile{FullName: "Ana García", CompanyName: "Acme"},
		Agent: agentFacts{Supported: true, Installed: true, Loaded: true, Binary: self, BinaryExists: true,
			LogAge: 4 * time.Minute, LastLine: "SKIP 2026-09-28 06:46 no scheduled sign due now"},
		GitHub: githubFacts{Repo: "ana/woffux", Login: "ana", WorkflowFound: true, Enabled: true, InSync: true,
			LastRunAt: time.Date(2026, 9, 25, 15, 3, 0, 0, time.UTC), LastRunResult: "success"},
		ClaudeCode: true, SkillInstalled: true, SkillCurrent: true,
	}
}

func statuses(checks []check) (fails, warns []check) {
	for _, c := range checks {
		switch c.Status {
		case "fail":
			fails = append(fails, c)
		case "warn":
			warns = append(warns, c)
		}
	}
	return
}

func findCheck(t *testing.T, checks []check, status, detail string) check {
	t.Helper()
	for _, c := range checks {
		if c.Status == status && strings.Contains(c.Detail, detail) {
			return c
		}
	}
	t.Fatalf("no %s check mentioning %q in:\n%s", status, detail, dumpChecks(checks))
	return check{}
}

func dumpChecks(checks []check) string {
	var b strings.Builder
	for _, c := range checks {
		b.WriteString("  " + c.Status + "  " + c.Section + ": " + c.Detail)
		if c.Fix != "" {
			b.WriteString("  → " + c.Fix)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func TestDoctorHealthyInstall(t *testing.T) {
	checks := judge(healthyFacts(t))
	if fails, warns := statuses(checks); len(fails)+len(warns) > 0 {
		t.Fatalf("a healthy install reported problems:\n%s", dumpChecks(checks))
	}
	next := findCheck(t, checks, "ok", "Next sign: IN Mon 28 Sep")
	if !strings.Contains(next.Detail, "This Mac, with GitHub as backup") {
		t.Errorf("next sign doesn't say who signs: %q", next.Detail)
	}
	findCheck(t, checks, "ok", "Woffu signs you in as Ana García · Acme")
}

func TestDoctorFindsStrandedAgent(t *testing.T) {
	f := healthyFacts(t)
	f.Agent.Binary = binaryInfo{Path: "/usr/local/bin/woffux"}
	f.Agent.BinaryExists = false
	c := findCheck(t, judge(f), "fail", "doesn't exist anymore")
	if c.Fix != "woffux agent on" {
		t.Errorf("fix = %q", c.Fix)
	}
}

func TestDoctorAgentOnAnotherCopy(t *testing.T) {
	f := healthyFacts(t)
	f.Agent.Binary = binaryInfo{Path: "/opt/woffux/woffux", Version: "v5.9.0"}
	c := findCheck(t, judge(f), "warn", "It runs another woffux: /opt/woffux/woffux (v5.9.0)")
	if !strings.HasPrefix(c.Fix, "woffux agent on") {
		t.Errorf("fix = %q", c.Fix)
	}
}

func TestDoctorPathProblems(t *testing.T) {
	f := healthyFacts(t)
	old := binaryInfo{Path: "/usr/local/bin/woffux", Version: "v5.9.0", Writable: false}
	f.OnPath = []binaryInfo{f.Self, old}
	c := findCheck(t, judge(f), "warn", "Another copy is still at /usr/local/bin/woffux (v5.9.0)")
	if c.Fix != "sudo rm /usr/local/bin/woffux" {
		t.Errorf("fix = %q", c.Fix)
	}

	// The old copy comes first: typing woffux runs it.
	f.OnPath = []binaryInfo{old, f.Self}
	findCheck(t, judge(f), "warn", "Typing 'woffux' runs another copy: /usr/local/bin/woffux")

	// A copy only this Mac's signer uses is reported there, not here.
	f.OnPath = []binaryInfo{f.Self, old}
	f.Agent.Binary = old
	for _, c := range judgeInstall(f) {
		if strings.Contains(c.Detail, "/usr/local/bin/woffux") {
			t.Errorf("the signer's copy reported as a leftover: %q", c.Detail)
		}
	}
}

func TestDoctorGitHubDrift(t *testing.T) {
	f := healthyFacts(t)
	f.GitHub.InSync = false
	f.GitHub.Missing = []string{"WOFFUX_TIMING"}
	f.GitHub.LastRunResult = "failure"
	checks := judge(f)
	for _, detail := range []string{"doesn't match your settings", "Missing secrets: WOFFUX_TIMING"} {
		if c := findCheck(t, checks, "fail", detail); c.Fix != "woffux sync" {
			t.Errorf("%s: fix = %q", detail, c.Fix)
		}
	}
	findCheck(t, checks, "fail", "Last scheduled run Fri 25 Sep")
}

func TestDoctorRejectedPassword(t *testing.T) {
	f := healthyFacts(t)
	f.Profile = nil
	f.SignInErr = &woffu.AuthError{Kind: woffu.ErrBadPassword, Detail: "wrong password"}
	c := findCheck(t, judge(f), "fail", "Woffu rejects the saved password")
	if !strings.Contains(c.Fix, "config edit") {
		t.Errorf("fix = %q", c.Fix)
	}

	f.SignInErr = &woffu.AuthError{Kind: woffu.ErrNetwork, Detail: "cannot connect to Woffu"}
	findCheck(t, judge(f), "warn", "Couldn't reach Woffu")
}

func TestDoctorNobodySigns(t *testing.T) {
	f := healthyFacts(t)
	f.Agent = agentFacts{Supported: true, LogAge: -1}
	f.GitHub = githubFacts{}
	f.Cfg.GithubFork = ""
	checks := judge(f)
	findCheck(t, checks, "warn", "Nobody signs automatically")
	findCheck(t, checks, "warn", "Off")
}

func TestDoctorNotSetUp(t *testing.T) {
	f := healthyFacts(t)
	f.ConfigExists, f.Cfg, f.HasPassword, f.Profile = false, nil, false, nil
	f.GitHub = githubFacts{}
	checks := judge(f)
	if c := findCheck(t, checks, "fail", "isn't set up yet"); c.Fix != "woffux setup" {
		t.Errorf("fix = %q", c.Fix)
	}
	for _, c := range checks {
		if c.Section == secGitHub || c.Section == secAuto {
			t.Errorf("without settings there's nothing to say about %s: %q", c.Section, c.Detail)
		}
	}
}

func TestDoctorLastRunFailed(t *testing.T) {
	f := healthyFacts(t)
	f.Agent.LastLine = "Error: auth failed: wrong password"
	findCheck(t, judge(f), "fail", "The last run failed: auth failed: wrong password")

	f.Agent.LastLine = "SKIP …"
	f.Agent.LogAge = 3 * time.Hour
	findCheck(t, judge(f), "warn", "Last ran 3 h ago")
}

func TestDoctorFailsTheCommandOnlyOnFailures(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	warnOnly := []check{{Section: secInstall, Status: "warn", Detail: "v9.9.9 is out", Fix: "woffux update"}}
	if err := reportDoctor(warnOnly, true); err != nil {
		t.Errorf("warnings failed the command: %v", err)
	}
	failing := append(warnOnly, check{Section: secSettings, Status: "fail", Detail: "x"})
	var quiet quietError
	if err := reportDoctor(failing, true); !errors.As(err, &quiet) {
		t.Errorf("err = %v, want a quietError", err)
	}
}
