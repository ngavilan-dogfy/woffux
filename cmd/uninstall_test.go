package cmd

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// fakeOps records every action instead of doing it.
func fakeOps(calls *[]string, fail map[string]error) uninstallOps {
	rec := func(name string) error {
		*calls = append(*calls, name)
		return fail[strings.SplitN(name, " ", 2)[0]]
	}
	return uninstallOps{
		agentOff:       func() error { return rec("agentOff") },
		removeLog:      func() error { return rec("removeLog") },
		githubOff:      func(repo string) error { return rec("githubOff " + repo) },
		deleteSecrets:  func(repo string) ([]string, error) { return []string{"WOFFU_PASSWORD"}, rec("deleteSecrets " + repo) },
		deletePassword: func(email string) error { return rec("deletePassword " + email) },
		removeFile:     func(path string) error { return rec("removeFile " + path) },
		removeCache:    func() error { return rec("removeCache") },
		removeSkill:    func() (bool, error) { return true, rec("removeSkill") },
	}
}

func fullPlan() uninstallPlan {
	return uninstallPlan{Agent: true, Repo: "ana/woffux", Email: "ana@acme.com", ConfigPath: "/home/ana/.woffux.yaml",
		HasConfig: true, Skill: true, Binary: "/home/ana/.local/bin/woffux"}
}

func TestUninstallEverythingStopsSignersFirstAndTheBinaryLast(t *testing.T) {
	var calls []string
	if failed := runUninstall(fullPlan(), true, fakeOps(&calls, nil)); failed != 0 {
		t.Fatalf("failed = %d", failed)
	}
	want := []string{
		"agentOff", "removeLog",
		"githubOff ana/woffux", "deleteSecrets ana/woffux",
		"deletePassword ana@acme.com", "removeFile /home/ana/.woffux.yaml", "removeCache", "removeSkill",
		"removeFile /home/ana/.local/bin/woffux",
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls:\n  %s\nwant:\n  %s", strings.Join(calls, "\n  "), strings.Join(want, "\n  "))
	}
}

func TestUninstallStopOnlyKeepsEverythingElse(t *testing.T) {
	var calls []string
	runUninstall(fullPlan(), false, fakeOps(&calls, nil))
	if want := []string{"agentOff", "githubOff ana/woffux"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %q, want %q", calls, want)
	}
}

func TestUninstallCarriesOnWhenGitHubFails(t *testing.T) {
	var calls []string
	failed := runUninstall(fullPlan(), true, fakeOps(&calls, map[string]error{"githubOff": errors.New("gh isn't signed in")}))
	if failed != 1 {
		t.Fatalf("failed = %d, want 1", failed)
	}
	if calls[len(calls)-1] != "removeFile /home/ana/.local/bin/woffux" {
		t.Fatalf("stopped early: %q", calls)
	}
}

func TestUninstallOnlyTouchesWhatExists(t *testing.T) {
	var calls []string
	runUninstall(uninstallPlan{ConfigPath: "/home/ana/.woffux.yaml"}, true, fakeOps(&calls, nil))
	if want := []string{"removeCache"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %q, want %q", calls, want)
	}
}

func TestUninstallNeverDeletesATestBinary(t *testing.T) {
	// The test binary is cmd.test: currentExecutable must refuse it, so a
	// plan built during tests can't point at anything real to delete.
	if got := currentExecutable(); got != "" {
		t.Fatalf("currentExecutable() = %q in a test", got)
	}
}
