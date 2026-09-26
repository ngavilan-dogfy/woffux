package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAgentFollowUp(t *testing.T) {
	dir := t.TempDir()
	updated := filepath.Join(dir, "woffux")
	other := filepath.Join(dir, "dev", "woffux")
	link := filepath.Join(dir, "woffux-link")
	for _, p := range []string{updated, other} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(updated, link); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name        string
		installed   bool
		agentBinary string
		want        agentAction
	}{
		{"no agent", false, "", agentNothing},
		{"agent runs the updated binary", true, updated, agentRestart},
		{"agent runs it through a symlink", true, link, agentRestart},
		{"agent runs another woffux", true, other, agentElsewhere},
		{"plist without a binary", true, "", agentNothing},
	}
	for _, c := range cases {
		if got := agentFollowUp(c.installed, c.agentBinary, updated); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
