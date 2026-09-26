package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/ngavilan-dogfy/woffux/internal/agent"
	"github.com/ngavilan-dogfy/woffux/internal/selfupdate"
)

// Version is set at build time: -X github.com/ngavilan-dogfy/woffux/cmd.Version=v5.14.0
var Version = "dev"

func currentBuild() selfupdate.Build { return selfupdate.CurrentBuild(Version) }

// updateTool describes woffux for the self-updater.
func updateTool() selfupdate.Tool {
	return selfupdate.Tool{Repo: "ngavilan-dogfy/woffux", Binary: "woffux", Current: currentBuild().Version}
}

// cacheDir is where woffux keeps caches: Woffu sessions and the daily
// update check.
func cacheDir() string {
	if d, err := os.UserCacheDir(); err == nil {
		return filepath.Join(d, "woffux")
	}
	return filepath.Join(os.TempDir(), "woffux")
}

var (
	updateYes   bool
	updateCheck bool
)

var updateCmd = &cobra.Command{
	Use:     "update",
	Aliases: []string{"upgrade"},
	Short:   "Update woffux to the latest version",
	Long: `Update to the latest release: shows what's new, downloads it, checks it
against the release's SHA-256 checksums and that the new binary runs, then
replaces this one. Your settings are not touched.

If this Mac signs for you, its signer switches to the new version too, and
the Claude Code skill is refreshed when it's installed. The GitHub backup
always downloads the latest release by itself.

Output:
  Terminal: an inline progress view. Without a terminal: one line per step.

Examples:
  woffux update           # see what's new, confirm, update
  woffux update --yes     # no questions
  woffux update --check   # only say whether there's a new version`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		t := updateTool()
		if updateCheck {
			rel, err := selfupdate.Latest(t)
			if errors.Is(err, selfupdate.ErrNoReleases) {
				fmt.Println("no releases published yet")
				return nil
			}
			if err != nil {
				return err
			}
			if selfupdate.IsRelease(t.Current) && !selfupdate.Newer(rel.Tag, t.Current) {
				fmt.Printf("woffux %s is the latest version\n", t.Current)
				return nil
			}
			fmt.Printf("%s is available (you have %s) — run 'woffux update'\n", rel.Tag, currentBuild().Short())
			return nil
		}
		err := selfupdate.Run(t, selfupdate.Options{
			Yes:         updateYes,
			Interactive: isTTY(),
			Out:         os.Stdout,
			OnInstalled: afterUpdate,
		})
		if selfupdate.IsQuiet(err) {
			return quietError{err}
		}
		return err
	},
}

// afterUpdate does what only the new binary can: rewrite this Mac's signer
// from its own template and refresh the skill it embeds.
func afterUpdate(path string) {
	switch agentFollowUp(agent.Supported() && agent.Installed(), agent.InstalledBinary(), path) {
	case agentRestart:
		if exec.Command(path, "agent", "on").Run() == nil {
			uiOK("This Mac's signer runs the new version")
		}
	case agentElsewhere:
		uiWarn("This Mac's signer runs another woffux: %s", tildePath(agent.InstalledBinary()))
		uiLine(stFaint.Render("  Switch it to this one: woffux agent on"))
	}
	if skillInstalled() && exec.Command(path, "skill", "install", "--quiet").Run() == nil {
		uiOK("Claude Code skill refreshed")
	}
	fmt.Println()
}

type agentAction int

const (
	agentNothing   agentAction = iota
	agentRestart               // the agent runs the binary just replaced
	agentElsewhere             // it runs a different woffux, left as is
)

// agentFollowUp decides what an update means for the local signer. Only
// the binary that was replaced is touched; an agent pointing at another
// woffux (a development build, say) is left alone, with a hint.
func agentFollowUp(installed bool, agentBinary, updated string) agentAction {
	switch {
	case !installed || agentBinary == "":
		return agentNothing
	case samePath(agentBinary, updated):
		return agentRestart
	default:
		return agentElsewhere
	}
}

func samePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	if err1 != nil || err2 != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return ra == rb
}

func init() {
	updateCmd.Flags().BoolVarP(&updateYes, "yes", "y", false, "Update without asking")
	updateCmd.Flags().BoolVar(&updateCheck, "check", false, "Only check whether a newer version exists")
}
