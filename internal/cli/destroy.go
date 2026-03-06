package cli

import (
	"fmt"

	"github.com/AlecAivazis/survey/v2"
	"github.com/noqcks/forja/internal/cloud"
	"github.com/noqcks/forja/internal/config"
	"github.com/spf13/cobra"
)

func newDestroyCmd(root *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "destroy",
		Short: "Tear down all AWS resources created by forja init",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "This will delete ALL forja AWS resources in %s:\n", cfg.Region)
			fmt.Fprintf(cmd.OutOrStdout(), "  - S3 bucket: %s\n", cfg.CacheBucket)
			fmt.Fprintf(cmd.OutOrStdout(), "  - IAM role: %s\n", cfg.Resources.IAMRoleName)
			fmt.Fprintf(cmd.OutOrStdout(), "  - Security group: %s\n", cfg.Resources.SecurityGroupName)
			fmt.Fprintf(cmd.OutOrStdout(), "  - Launch templates: %s, %s\n", cfg.Resources.LaunchTemplates["amd64"], cfg.Resources.LaunchTemplates["arm64"])
			confirm := ""
			if err := survey.AskOne(&survey.Input{Message: `Type "destroy" to confirm:`}, &confirm, survey.WithValidator(survey.Required)); err != nil {
				return err
			}
			if confirm != "destroy" {
				return fmt.Errorf("destroy cancelled")
			}
			provider, err := providerFromConfig(cmd.Context(), cfg, root.profile)
			if err != nil {
				return err
			}
			result, err := provider.DestroyInfrastructure(cmd.Context(), cloud.DestroyRequest{
				Region:              cfg.Region,
				CacheBucket:         cfg.CacheBucket,
				LaunchTemplates:     cfg.Resources.LaunchTemplates,
				SecurityGroupID:     cfg.Resources.SecurityGroupID,
				IAMRoleName:         cfg.Resources.IAMRoleName,
				InstanceProfileName: cfg.Resources.InstanceProfileName,
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "  [ok] Terminated %d running instances\n", result.TerminatedInstances)
			fmt.Fprintln(cmd.OutOrStdout(), "  [ok] Deleted S3 bucket")
			fmt.Fprintln(cmd.OutOrStdout(), "  [ok] Deleted launch templates")
			fmt.Fprintln(cmd.OutOrStdout(), "  [ok] Deleted security group")
			fmt.Fprintln(cmd.OutOrStdout(), "  [ok] Deleted IAM role + instance profile")
			path, _ := config.ConfigPath()
			fmt.Fprintf(cmd.OutOrStdout(), "\nAll forja resources removed. Config file at %s retained.\n", path)
			return nil
		},
	}
}
