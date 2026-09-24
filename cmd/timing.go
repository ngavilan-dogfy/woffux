package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ngavilan-dogfy/woffux/internal/config"
	gh "github.com/ngavilan-dogfy/woffux/internal/github"
	"github.com/ngavilan-dogfy/woffux/internal/timing"
)

var timingCmd = &cobra.Command{
	Use:   "timing [natural|relaxed|exact]",
	Short: "Sign at natural moments instead of the exact scheduled minute",
	Long: `Natural timing moves each automatic sign a few minutes around its
scheduled time, differently every day, so signs don't look robotic:

  natural   IN 0–6 min early, OUT 0–8 min late (recommended)
  relaxed   IN 0–12 min early, OUT 0–15 min late
  exact     sign at the scheduled minute

Windows lean the safe way (in early, out late) and a work block is never
shorter than planned. The local agent and the GitHub backup compute the
same moment from a private seed, so they never double-sign.

Without an argument, shows the current setting and next week's moments.
Interactive custom windows: woffux timing custom`,
	Args:         cobra.MaximumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		if len(args) == 0 {
			fmt.Println()
			if cfg.Timing.Active() {
				fmt.Printf("  %s %s\n\n", stBold.Render("Natural timing"), stFaint.Render(cfg.Timing.Describe()))
			} else {
				fmt.Printf("  %s %s\n\n", stBold.Render("Exact timing"), stFaint.Render("— try: woffux timing natural"))
			}
			fmt.Println(indentLines(timingPreview(cfg.Timing, cfg.Schedule), 2))
			fmt.Println()
			return nil
		}
		key := strings.ToLower(args[0])
		switch key {
		case "custom":
			if cfg.Timing, err = timingWizard(cfg.Timing, cfg.Schedule); err != nil {
				return err
			}
		case "off":
			key = "exact"
			fallthrough
		default:
			found := false
			for _, p := range timing.Presets {
				if p.Key == key {
					cfg.Timing = cfg.Timing.WithPreset(p)
					found = true
				}
			}
			if !found {
				return fmt.Errorf("unknown timing %q — use natural, relaxed, exact or custom", args[0])
			}
		}
		if err := config.Save(cfg); err != nil {
			return fmt.Errorf("save config: %w", err)
		}
		fmt.Printf("\n  %s Timing saved: %s\n\n", stIn.Render("✓"), cfg.Timing.Describe())
		fmt.Println(indentLines(timingPreview(cfg.Timing, cfg.Schedule), 2))
		fmt.Println()
		if cfg.GithubFork != "" {
			password, perr := config.GetPassword(cfg.WoffuEmail)
			if perr == nil {
				if err := gh.SyncSecrets(cfg, password); err == nil {
					fmt.Printf("  %s GitHub backup updated\n\n", stIn.Render("✓"))
					return nil
				}
			}
			fmt.Printf("  %s Run %s so the GitHub backup uses it too.\n\n", stOut.Render("!"), stBold.Render("woffux sync"))
		}
		return nil
	},
}

func init() { rootCmd.AddCommand(timingCmd) }
