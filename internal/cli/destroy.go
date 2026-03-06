package cli

import (
	"errors"
	"fmt"

	"github.com/noqcks/forja/internal/cloud"
	"github.com/noqcks/forja/internal/config"
	log "github.com/sirupsen/logrus"
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
			log.Warnf("This will delete ALL forja AWS resources in %s:", cfg.Region)
			log.Warnf("  - S3 bucket: %s", cfg.CacheBucket)
			log.Warnf("  - IAM role: %s", cfg.Resources.IAMRoleName)
			log.Warnf("  - Security group: %s", cfg.Resources.SecurityGroupName)
			log.Warnf("  - Launch templates: %s, %s", cfg.Resources.LaunchTemplates["amd64"], cfg.Resources.LaunchTemplates["arm64"])
			confirm, err := confirmTypedValue(cmd, `Type "destroy" to confirm:`, "destroy")
			if err != nil {
				if errors.Is(err, errPromptCanceled) {
					return fmt.Errorf("destroy cancelled")
				}
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
			log.Infof("[ok] Terminated %d running instances", result.TerminatedInstances)
			log.Info("[ok] Deleted S3 bucket")
			log.Info("[ok] Deleted launch templates")
			log.Info("[ok] Deleted security group")
			log.Info("[ok] Deleted IAM role + instance profile")
			path, _ := config.ConfigPath()
			log.Infof("All forja resources removed. Config file at %s retained.", path)
			return nil
		},
	}
}
