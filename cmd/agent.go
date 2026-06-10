package cmd

import (
	"fmt"

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
		fmt.Printf("  %s Local agent installed (runs at %s every hour)\n", sOk, agent.MinutesLabel())
		fmt.Printf("    Log: %s\n", agent.LogPath())
		fmt.Println("    GitHub auto-sign remains as fallback for when this Mac is asleep.")
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
		fmt.Printf("  %s Local agent removed. GitHub auto-sign (if enabled) keeps working.\n", sOk)
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
	if !agent.Supported() {
		fmt.Println("  Local agent: unsupported on this OS (macOS only)")
		return nil
	}

	installed := agent.Installed()
	loaded := agent.Loaded()

	switch {
	case installed && loaded:
		fmt.Printf("  %s Local agent: active (runs at %s)\n", sOk, agent.MinutesLabel())
	case installed && !loaded:
		fmt.Printf("  %s Local agent: installed but not loaded — run 'woffux agent on' to fix\n", sWarn)
	default:
		fmt.Printf("  %s Local agent: not installed — run 'woffux agent on'\n", sWarn)
	}

	if lines := agent.RecentLog(6); len(lines) > 0 {
		fmt.Println("\n  Recent activity:")
		for _, line := range lines {
			fmt.Printf("    %s\n", line)
		}
	}
	return nil
}
