package cli

import (
	"fmt"
	"time"

	"github.com/AlecAivazis/survey/v2"
	"github.com/noqcks/forja/internal/config"
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
				fmt.Fprintln(cmd.OutOrStdout(), "No orphaned forja instances found.")
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Found %d orphaned forja instance(s):\n", len(instances))
			ids := make([]string, 0, len(instances))
			for _, instance := range instances {
				ids = append(ids, instance.ID)
				fmt.Fprintf(cmd.OutOrStdout(), "  %s  %s  %s  launched %s ago\n", instance.ID, instance.InstanceType, instance.State, time.Since(instance.LaunchTime).Round(time.Minute))
			}
			confirm := false
			if err := survey.AskOne(&survey.Confirm{Message: "Terminate?", Default: false}, &confirm); err != nil {
				return err
			}
			if !confirm {
				return nil
			}
			if err := provider.TerminateInstances(cmd.Context(), cfg.Region, ids); err != nil {
				return err
			}
			for _, id := range ids {
				fmt.Fprintf(cmd.OutOrStdout(), "  [ok] Terminated %s\n", id)
			}
			return nil
		},
	}
}
