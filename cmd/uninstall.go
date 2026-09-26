package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/ngavilan-dogfy/woffux/internal/agent"
	"github.com/ngavilan-dogfy/woffux/internal/config"
	gh "github.com/ngavilan-dogfy/woffux/internal/github"
)

var (
	uninstallYes      bool
	uninstallStopOnly bool
	errUninstallAsks  = errors.New("woffux uninstall asks before removing anything: run it in a terminal, or pass --yes")
)

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Stop signing and remove woffux",
	Long: `Stop every automatic sign and, unless you only want to pause, remove
everything woffux put on this computer and on GitHub:

  · this Mac's signer (the launchd agent) and its log
  · the GitHub backup: Auto Sign turned off, and your credentials and
    locations deleted from the repository's secrets
  · ~/.woffux.yaml, the password in the keychain and cached sessions
  · the Claude Code skill, and this woffux binary

It asks first. Your Woffu account and history are not touched, and your
GitHub repository is never deleted: the command says where to do it if
you want to.

Examples:
  woffux uninstall                 # choose: stop signing, or remove everything
  woffux uninstall --stop-only     # only stop signing (undo: woffux agent on / auto on)
  woffux uninstall --yes           # remove everything without asking`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, _ := config.Load()
		plan := planUninstall(cfg, currentExecutable())

		everything := !uninstallStopOnly
		if !uninstallYes {
			if !isTTY() {
				return errUninstallAsks
			}
			choice, err := askUninstall(plan, uninstallStopOnly)
			if err != nil || choice == "" {
				fmt.Println()
				uiLine(stFaint.Render("Nothing was changed."))
				fmt.Println()
				return nil
			}
			everything = choice == "all"
		}
		fmt.Println()
		failed := runUninstall(plan, everything, realUninstallOps())
		fmt.Println()
		if failed > 0 {
			return quietError{fmt.Errorf("%d steps failed", failed)}
		}
		return nil
	},
}

func init() {
	uninstallCmd.Flags().BoolVarP(&uninstallYes, "yes", "y", false, "Don't ask")
	uninstallCmd.Flags().BoolVar(&uninstallStopOnly, "stop-only", false, "Only stop automatic signing; keep settings and the binary")
	rootCmd.AddCommand(uninstallCmd)
}

// uninstallPlan is what this install has, so only what exists is offered.
type uninstallPlan struct {
	Agent      bool   // this Mac's signer is installed
	Repo       string // the GitHub backup's repository
	Email      string // whose password is in the keychain
	ConfigPath string
	HasConfig  bool
	Skill      bool
	Binary     string // this executable ("" when unknown)
}

func planUninstall(cfg *config.Config, binary string) uninstallPlan {
	p := uninstallPlan{Agent: agent.Supported() && agent.Installed(), Skill: skillInstalled(), Binary: binary}
	p.ConfigPath, _ = config.Path()
	if _, err := os.Stat(p.ConfigPath); err == nil {
		p.HasConfig = true
	}
	if cfg != nil {
		p.Repo, p.Email = cfg.GithubFork, cfg.WoffuEmail
	}
	return p
}

func currentExecutable() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	if filepath.Base(exe) != "woffux" {
		return "" // a development or test build: not ours to delete
	}
	return exe
}

func askUninstall(p uninstallPlan, stopOnly bool) (string, error) {
	uiTitle("Uninstall woffux")
	uiLine(stText.Render("Removing everything means:"))
	for _, line := range p.describe() {
		uiLine(stFaint.Render("  · ") + stText.Render(line))
	}
	fmt.Println()
	uiLine(stFaint.Render("Your Woffu account and its history are not touched."))
	fmt.Println()
	choice := "all"
	if stopOnly {
		choice = "stop"
	}
	err := newForm(huh.NewGroup(huh.NewSelect[string]().
		Title("What should woffux do?").
		Options(
			huh.NewOption("Remove everything", "all"),
			huh.NewOption("Only stop signing "+stFaint.Render("— keep settings; undo with woffux agent on / auto on"), "stop"),
			huh.NewOption("Cancel", ""),
		).
		Value(&choice))).Run()
	return choice, err
}

// describe lists what removing everything does on this install.
func (p uninstallPlan) describe() []string {
	var out []string
	if p.Agent {
		out = append(out, "This Mac stops signing: its launchd agent and log are removed")
	}
	if p.Repo != "" {
		out = append(out, "GitHub stops signing on "+p.Repo+", and your credentials and locations are deleted from its secrets")
	}
	if p.HasConfig {
		out = append(out, "Your settings ("+tildePath(p.ConfigPath)+"), the password in the keychain and cached sessions")
	}
	if p.Skill {
		out = append(out, "The /woffux skill for Claude Code")
	}
	if p.Binary != "" {
		out = append(out, "woffux itself: "+tildePath(p.Binary))
	}
	return out
}

// uninstallOps are the actions, swapped for fakes in tests: nothing in a
// test may stop a real signer or delete real data.
type uninstallOps struct {
	agentOff       func() error
	removeLog      func() error
	githubOff      func(repo string) error
	deleteSecrets  func(repo string) ([]string, error)
	deletePassword func(email string) error
	removeFile     func(path string) error
	removeCache    func() error
	removeSkill    func() (bool, error)
}

func realUninstallOps() uninstallOps {
	return uninstallOps{
		agentOff:  agent.Uninstall,
		removeLog: func() error { return removeIfExists(agent.LogPath()) },
		githubOff: func(repo string) error {
			if _, err := exec.LookPath("gh"); err != nil {
				return errors.New("the GitHub CLI (gh) isn't installed")
			}
			return gh.DisableAutoSign(repo)
		},
		deleteSecrets:  gh.DeleteSecrets,
		deletePassword: config.DeletePassword,
		removeFile:     removeIfExists,
		removeCache:    func() error { return os.RemoveAll(cacheDir()) },
		removeSkill:    removeSkill,
	}
}

func removeIfExists(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// runUninstall stops the signers first, then (everything) deletes the
// credentials on GitHub, the local data and the binary last. It returns
// how many steps failed; each one says how to finish it by hand.
func runUninstall(p uninstallPlan, everything bool, ops uninstallOps) (failed int) {
	fail := func(format string, a ...any) {
		uiErr(format, a...)
		failed++
	}

	if p.Agent {
		if err := ops.agentOff(); err != nil {
			fail("This Mac's signer: %s", err)
		} else {
			uiOK("This Mac stopped signing")
			if everything {
				_ = ops.removeLog()
			}
		}
	}

	if p.Repo != "" {
		manual := "https://github.com/" + p.Repo + "/actions"
		if err := ops.githubOff(p.Repo); err != nil {
			fail("GitHub signing on %s: %s", p.Repo, err)
			uiLine(stFaint.Render("  Turn off the Auto Sign workflow at " + manual))
		} else {
			uiOK("GitHub stopped signing on %s", p.Repo)
		}
		if everything {
			removed, err := ops.deleteSecrets(p.Repo)
			switch {
			case err != nil:
				fail("GitHub secrets on %s: %s", p.Repo, err)
				uiLine(stFaint.Render("  Delete the WOFFU_* secrets at https://github.com/" + p.Repo + "/settings/secrets/actions"))
			case len(removed) > 0:
				uiOK("Deleted %d secrets from %s (credentials, locations, timing)", len(removed), p.Repo)
			}
			if p.Repo != gh.UpstreamRepo {
				uiLine(stFaint.Render("  The repository " + p.Repo + " stays; delete it at https://github.com/" + p.Repo + "/settings if you don't need it"))
			}
		}
	}

	if !everything {
		uiLine(stFaint.Render("Settings kept. Sign automatically again with: woffux agent on · woffux auto on"))
		return failed
	}

	if p.Email != "" {
		if err := ops.deletePassword(p.Email); err != nil {
			fail("The password in the keychain: %s", err)
		} else {
			uiOK("Password removed from the keychain")
		}
	}
	if p.HasConfig {
		if err := ops.removeFile(p.ConfigPath); err != nil {
			fail("%s: %s", tildePath(p.ConfigPath), err)
		} else {
			uiOK("Settings removed")
		}
	}
	if err := ops.removeCache(); err != nil {
		fail("Cached sessions: %s", err)
	}
	if p.Skill {
		if _, err := ops.removeSkill(); err != nil {
			fail("The Claude Code skill: %s", err)
		} else {
			uiOK("Claude Code skill removed")
		}
	}
	if p.Binary != "" {
		if err := ops.removeFile(p.Binary); err != nil {
			fail("Couldn't remove %s: %s", tildePath(p.Binary), err)
			uiLine(stFaint.Render("  Remove it with: sudo rm " + p.Binary))
		} else {
			uiOK("woffux removed from %s", tildePath(filepath.Dir(p.Binary)))
		}
	}
	if failed == 0 {
		fmt.Println()
		uiLine(stText.Render("woffux is gone. Thanks for using it."))
	}
	return failed
}
