package cmd

import (
	"bytes"
	"embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

// The Claude Code skill ships inside the binary, so it always matches the
// commands of the woffux that installs it. .claude/skills/woffux/SKILL.md
// in the repository is the same file, for people working on woffux itself.

//go:embed skill_data/SKILL.md
var skillFS embed.FS

var skillQuiet bool

var skillCmd = &cobra.Command{
	Use:   "skill",
	Short: "Teach Claude Code to use woffux (the /woffux skill)",
	Long: `Install, check or remove the /woffux skill for Claude Code. With it, Claude
can tell you whether you've signed today, show your balance, request days
off and sign for you — always asking before anything that writes to Woffu.

The skill lives in ~/.claude/skills/woffux/ and is refreshed by 'woffux
update'.`,
}

var skillInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install or refresh the /woffux skill",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		dest, err := installSkill()
		if err != nil {
			return err
		}
		if skillQuiet {
			return nil
		}
		fmt.Println()
		uiOK("Claude Code skill installed")
		uiLine(stFaint.Render("  " + tildePath(dest)))
		uiLine(stText.Render("Use ") + stBold.Render("/woffux") + stText.Render(" in any Claude Code session — for example:"))
		uiLine(stFaint.Render(`  "have I signed today?" · "how many vacation days do I have?" · "sign me out"`))
		fmt.Println()
		return nil
	},
}

var skillRemoveCmd = &cobra.Command{
	Use:   "remove",
	Short: "Remove the /woffux skill",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		removed, err := removeSkill()
		if err != nil {
			return err
		}
		fmt.Println()
		if removed {
			uiOK("Claude Code skill removed")
		} else {
			uiLine(stFaint.Render("The skill isn't installed."))
		}
		fmt.Println()
		return nil
	},
}

var skillStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Is the /woffux skill installed and current?",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		dest, err := skillPath()
		if err != nil {
			return err
		}
		fmt.Println()
		switch {
		case !skillInstalled():
			uiLine(stFaint.Render("○ Not installed — ") + stBold.Render("woffux skill install"))
		case !skillCurrent():
			uiWarn("Installed, but from another version of woffux — refresh it: woffux skill install")
		default:
			uiOK("Installed and current  %s", stFaint.Render(tildePath(dest)))
		}
		fmt.Println()
		return nil
	},
}

func init() {
	skillInstallCmd.Flags().BoolVarP(&skillQuiet, "quiet", "q", false, "Print nothing on success")
	skillCmd.AddCommand(skillInstallCmd)
	skillCmd.AddCommand(skillRemoveCmd)
	skillCmd.AddCommand(skillStatusCmd)
	rootCmd.AddCommand(skillCmd)
}

// skillPath is ~/.claude/skills/woffux/SKILL.md.
func skillPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot find home directory: %w", err)
	}
	return filepath.Join(home, ".claude", "skills", "woffux", "SKILL.md"), nil
}

func embeddedSkill() []byte {
	data, _ := skillFS.ReadFile("skill_data/SKILL.md")
	return data
}

// installSkill writes the embedded skill, replacing an older one.
func installSkill() (string, error) {
	dest, err := skillPath()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", fmt.Errorf("create %s: %w", filepath.Dir(dest), err)
	}
	if err := os.WriteFile(dest, embeddedSkill(), 0o644); err != nil {
		return "", fmt.Errorf("write the skill: %w", err)
	}
	return dest, nil
}

// removeSkill deletes the skill's folder; false when it wasn't there.
func removeSkill() (bool, error) {
	dest, err := skillPath()
	if err != nil {
		return false, err
	}
	dir := filepath.Dir(dest)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return false, nil
	}
	if err := os.RemoveAll(dir); err != nil {
		return false, fmt.Errorf("remove the skill: %w", err)
	}
	return true, nil
}

func skillInstalled() bool {
	dest, err := skillPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(dest)
	return err == nil
}

// skillCurrent reports whether the installed skill is this version's.
func skillCurrent() bool {
	dest, err := skillPath()
	if err != nil {
		return false
	}
	data, err := os.ReadFile(dest)
	return err == nil && bytes.Equal(data, embeddedSkill())
}

// claudeCodeDetected reports whether Claude Code is used on this machine.
func claudeCodeDetected() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	info, err := os.Stat(filepath.Join(home, ".claude"))
	return err == nil && info.IsDir()
}
