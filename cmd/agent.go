package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ngavilan-dogfy/woffux/internal/agent"
)

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Local auto-sign agent (launchd) — fires on time even when GitHub crons are delayed",
	Long: `Manage the local auto-sign agent.

GitHub Actions cron schedules are best-effort and routinely fire hours late
or not at all. The local agent runs 'woffux sign --scheduled' every 15
minutes via launchd: signs happen on time whenever this Mac is awake, and
the GitHub workflow stays as a fallback for when it isn't.

The agent reads the active schedule from the local config on every run, so
changing schedule or preset needs no re-sync.

Commands:
  woffux agent on        Install and start the agent
  woffux agent off       Stop and remove the agent
  woffux agent status    Show agent state and recent activity`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return agentStatus()
	},
}

var agentOnCmd = &cobra.Command{
	Use:   "on",
	Short: "Install and start the local auto-sign agent",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := agent.Install(); err != nil {
			return err
		}
		fmt.Println()
		uiOK("This Mac will sign for you while it's awake")
		uiLine(stFaint.Render("  Log: " + agent.LogPath()))
		fmt.Println()
		return nil
	},
}

var agentOffCmd = &cobra.Command{
	Use:   "off",
	Short: "Stop and remove the local auto-sign agent",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := agent.Uninstall(); err != nil {
			return err
		}
		fmt.Println()
		uiOK("This Mac stopped signing (the GitHub backup, if on, keeps working)")
		fmt.Println()
		return nil
	},
}

var agentStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show local agent state and recent activity",
	RunE: func(cmd *cobra.Command, args []string) error {
		return agentStatus()
	},
}

func init() {
	agentCmd.AddCommand(agentOnCmd)
	agentCmd.AddCommand(agentOffCmd)
	agentCmd.AddCommand(agentStatusCmd)
}

func agentStatus() error {
	uiTitle("This Mac signer", "local agent")
	if !agent.Supported() {
		uiLine(stFaint.Render("Only available on macOS. Use the GitHub backup: woffux auto on"))
		fmt.Println()
		return nil
	}
	installed, loaded := agent.Installed(), agent.Loaded()
	switch {
	case installed && loaded:
		uiRow("State", stIn.Render("● on")+stFaint.Render("  checks at "+agent.MinutesLabel()+" and waits for each sign's moment"))
	case installed:
		uiRow("State", stOut.Render("! installed but not running")+stFaint.Render("  fix: woffux agent on"))
	default:
		uiRow("State", stFaint.Render("○ off")+stFaint.Render("  turn on: woffux agent on"))
	}
	uiRow("Log", stSubtle.Render(agent.LogPath()))

	if lines := agent.RecentLog(12); len(lines) > 0 {
		uiSection("Recent activity")
		// Collapse repeats ("skipped … not a working day ×7").
		for i := 0; i < len(lines); {
			j := i
			for j+1 < len(lines) && lines[j+1] == lines[i] {
				j++
			}
			out := agentLogLine(lines[i])
			if j > i {
				out += stFaint.Render(fmt.Sprintf("  ×%d", j-i+1))
			}
			uiLine(out)
			i = j + 1
		}
	}
	fmt.Println()
	return nil
}

// agentLogLine colours one agent log line by what happened.
func agentLogLine(line string) string {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return ""
	}
	rest := strings.TrimSpace(strings.TrimPrefix(line, fields[0]))
	switch fields[0] {
	case "OK":
		return stIn.Render("✓ signed  ") + stText.Render(rest)
	case "WAIT":
		return stBrand.Render("◷ waiting ") + stSubtle.Render(rest)
	case "SKIP":
		return stFaint.Render("· skipped " + rest)
	}
	if strings.HasPrefix(line, "Error") || strings.Contains(strings.ToLower(line), "fail") {
		return stBad.Render("✗ ") + stText.Render(line)
	}
	return stSubtle.Render(line)
}
