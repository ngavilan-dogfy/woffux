package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var versionJSON bool

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version, commit and where this binary lives",
	Long: `Show the version of this woffux, the commit it was built from and where
it's installed. Handy for bug reports.

Output:
  Terminal: a short summary · piped: the version · --json: every field

Examples:
  woffux version
  woffux version --json`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		b := currentBuild()
		if versionJSON {
			return printJSON(b)
		}
		if !isTTY() {
			fmt.Println(b.Version)
			return nil
		}
		fmt.Println()
		uiLine(stBrand.Render("◆ woffux") + " " + stBold.Render(b.Short()))
		for _, r := range [][2]string{{"Built", b.Date}, {"Go", b.Go}, {"Platform", b.Platform}, {"Binary", tildePath(b.Path)}} {
			if r[1] != "" {
				uiRow(r[0], stText.Render(r[1]))
			}
		}
		fmt.Println()
		return nil
	},
}

func init() {
	versionCmd.Flags().BoolVar(&versionJSON, "json", false, "Output as JSON")
	rootCmd.AddCommand(versionCmd)
	rootCmd.Version = currentBuild().Short()
}
