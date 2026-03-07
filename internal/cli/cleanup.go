package cli

import (
	"errors"
	"fmt"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

func newCleanupCmd(root *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "cleanup",
		Short: "Find and terminate orphaned Forja instances",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCommandConfig(false)
			if err != nil {
				return err
			}
			provider, err := providerFromConfig(cmd.Context(), cfg, root.profile)
			if err != nil {
				return err
			}
			instances, err := provider.ListOrphanedInstances(cmd.Context(), cfg.Region)
			if err != nil {
				return err
			}
			if len(instances) == 0 {
				log.Info("No orphaned forja instances found.")
				return nil
			}
			log.Infof("Found %d orphaned forja instance(s):", len(instances))
			ids := make([]string, 0, len(instances))
			for _, instance := range instances {
				ids = append(ids, instance.ID)
				details := []string{
					instance.ID,
					instance.InstanceType,
					instance.State,
					fmt.Sprintf("launched %s ago", time.Since(instance.LaunchTime).Round(time.Minute)),
				}
				if instance.BuildHash != "" {
					details = append(details, "build "+instance.BuildHash)
				}
				if instance.Name != "" {
					details = append(details, "name "+instance.Name)
				}
				log.Infof("  %s", strings.Join(details, "  "))
			}
			confirm, err := confirmAction(cmd, "Terminate orphaned instances?", false)
			if err != nil {
				if errors.Is(err, errPromptCanceled) {
					return nil
				}
				return err
			}
			if !confirm {
				return nil
			}
			if err := provider.TerminateInstances(cmd.Context(), cfg.Region, ids); err != nil {
				return err
			}
			for _, id := range ids {
				log.Infof("[ok] Terminated %s", id)
			}
			return nil
		},
	}
}
