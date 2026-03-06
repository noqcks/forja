package cli

import (
	"errors"
	"time"

	"github.com/noqcks/forja/internal/config"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

func newCleanupCmd(root *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "cleanup",
		Short: "Find and terminate orphaned Forja instances",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
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
				log.Infof("  %s  %s  %s  launched %s ago", instance.ID, instance.InstanceType, instance.State, time.Since(instance.LaunchTime).Round(time.Minute))
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
