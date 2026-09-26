package cmd

import (
	"bytes"
	"os"
	"testing"
)

// The repository's copy of the skill is what Claude Code loads while
// working on woffux; it must be the one the binary installs.
func TestRepositorySkillMatchesEmbedded(t *testing.T) {
	repo, err := os.ReadFile("../.claude/skills/woffux/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(repo, embeddedSkill()) {
		t.Fatal(".claude/skills/woffux/SKILL.md differs from cmd/skill_data/SKILL.md: copy one over the other")
	}
}

func TestSkillInstallStatusRemove(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if skillInstalled() {
		t.Fatal("installed in an empty home")
	}
	if _, err := installSkill(); err != nil {
		t.Fatal(err)
	}
	if !skillInstalled() || !skillCurrent() {
		t.Fatal("expected a current skill after install")
	}
	dest, _ := skillPath()
	if err := os.WriteFile(dest, []byte("older"), 0o644); err != nil {
		t.Fatal(err)
	}
	if skillCurrent() {
		t.Fatal("an older skill reported as current")
	}
	if removed, err := removeSkill(); err != nil || !removed {
		t.Fatalf("remove: %v %v", removed, err)
	}
	if removed, _ := removeSkill(); removed {
		t.Fatal("removed twice")
	}
}
