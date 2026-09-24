package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ngavilan-dogfy/woffux/internal/woffu"
)

var whoamiJSON bool

var whoamiCmd = &cobra.Command{
	Use:   "whoami",
	Short: "Show current authenticated user",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, password, err := loadConfigOrSetup()
		if err != nil {
			return err
		}

		client := woffu.NewWoffuClient(cfg.WoffuURL)
		companyClient := woffu.NewCompanyClient(cfg.WoffuCompanyURL)

		token, err := woffu.AuthenticateCached(client, companyClient, cfg.WoffuEmail, password)
		if err != nil {
			return fmt.Errorf("auth failed: %w\n\n  If your credentials changed, run 'woffux setup'", err)
		}

		profile, err := woffu.GetUserProfile(companyClient, token)
		if err != nil {
			return fmt.Errorf("get profile: %w", err)
		}

		if whoamiJSON {
			return printJSON(map[string]interface{}{
				"name":       profile.FullName,
				"email":      profile.Email,
				"company":    profile.CompanyName,
				"department": profile.DepartmentName,
				"job_title":  profile.JobTitle,
				"office":     profile.OfficeName,
			})
		}

		viewWhoami(profile)
		return nil
	},
}

func init() {
	whoamiCmd.Flags().BoolVar(&whoamiJSON, "json", false, "Output as JSON")
}
