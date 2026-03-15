package cmd

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

//go:embed skill_data/SKILL.md
var skillFS embed.FS

var skillCmd = &cobra.Command{
	Use:   "skill",
	Short: "Manage Claude Code skill integration",
	Long:  "Install or remove the woffux skill for Claude Code, so you can use /woffux in any session.",
}

var skillInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install the /woffux skill for Claude Code",
	RunE: func(cmd *cobra.Command, args []string) error {
		dest, err := skillPath()
		if err != nil {
			return err
		}

		// Read embedded skill file
		data, err := skillFS.ReadFile("skill_data/SKILL.md")
		if err != nil {
			return fmt.Errorf("read embedded skill: %w", err)
		}

		// Create directory
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			return fmt.Errorf("create skill directory: %w", err)
		}

		// Write skill file
		if err := os.WriteFile(dest, data, 0644); err != nil {
			return fmt.Errorf("write skill: %w", err)
		}

		fmt.Printf("\n  %s Claude Code skill installed\n", sOk)
		fmt.Printf("  %s\n\n", sDim.Render(dest))
		fmt.Printf("  Use %s in any Claude Code session.\n", sBold.Render("/woffux"))
		fmt.Printf("  %s\n\n", sDim.Render("Ask things like: \"have I signed today?\", \"how many vacation days?\", \"sign me out\""))

		return nil
	},
}

var skillRemoveCmd = &cobra.Command{
	Use:   "remove",
	Short: "Remove the /woffux skill from Claude Code",
	RunE: func(cmd *cobra.Command, args []string) error {
		dest, err := skillPath()
		if err != nil {
			return err
		}

		dir := filepath.Dir(dest)
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			fmt.Printf("\n  %s Skill not installed.\n\n", sDim.Render("i"))
			return nil
		}

		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("remove skill: %w", err)
		}

		fmt.Printf("\n  %s Skill removed.\n\n", sOk)
		return nil
	},
}

var skillStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check if the Claude Code skill is installed",
	Run: func(cmd *cobra.Command, args []string) {
		dest, err := skillPath()
		if err != nil {
			fmt.Printf("\n  %s Cannot determine skill path: %s\n\n",
				lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("!"), err)
			return
		}

		if _, err := os.Stat(dest); os.IsNotExist(err) {
			fmt.Printf("\n  %s Not installed. Run %s\n\n",
				sDim.Render("○"),
				sBold.Render("woffux skill install"))
		} else {
			fmt.Printf("\n  %s Installed at %s\n", sOk, sDim.Render(dest))
			fmt.Printf("  Use %s in Claude Code.\n\n", sBold.Render("/woffux"))
		}
	},
}

func init() {
	skillCmd.AddCommand(skillInstallCmd)
	skillCmd.AddCommand(skillRemoveCmd)
	skillCmd.AddCommand(skillStatusCmd)
	rootCmd.AddCommand(skillCmd)
}

func skillPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot find home directory: %w", err)
	}
	return filepath.Join(home, ".claude", "skills", "woffux", "SKILL.md"), nil
}
