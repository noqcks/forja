package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

type rootOptions struct {
	profile string
}

func Execute(ctx context.Context) error {
	opts := &rootOptions{}
	cmd := &cobra.Command{
		Use:           "forja",
		Short:         "Forja is a self-hosted remote Docker build machine",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.PersistentFlags().StringVar(&opts.profile, "profile", "", "AWS profile to use")
	cmd.AddCommand(
		newInitCmd(opts),
		newBuildCmd(opts),
		newCleanupCmd(opts),
		newDestroyCmd(opts),
		newVersionCmd(),
	)
	cmd.SetContext(ctx)
	if err := cmd.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), err)
		return err
	}
	return nil
}
