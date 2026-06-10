package cmd

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
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

		sLabel := lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Width(14)
		sVal := lipgloss.NewStyle().Bold(true)

		fmt.Println()
		fmt.Println("  " + sLabel.Render("Name") + sVal.Render(profile.FullName))
		fmt.Println("  " + sLabel.Render("Email") + sVal.Render(profile.Email))
		fmt.Println("  " + sLabel.Render("Company") + sVal.Render(profile.CompanyName))
		fmt.Println("  " + sLabel.Render("Department") + sVal.Render(profile.DepartmentName))
		fmt.Println("  " + sLabel.Render("Job title") + sVal.Render(profile.JobTitle))
		fmt.Println("  " + sLabel.Render("Office") + sVal.Render(profile.OfficeName))
		fmt.Println()

		return nil
	},
}

func init() {
	whoamiCmd.Flags().BoolVar(&whoamiJSON, "json", false, "Output as JSON")
}
