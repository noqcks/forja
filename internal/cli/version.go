package cli

import (
	"fmt"

	"github.com/noqcks/forja/internal/version"
	"github.com/spf13/cobra"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintf(cmd.OutOrStdout(), "forja %s (%s) built %s\n", version.Version, version.Commit, version.Date)
		},
	}
}
