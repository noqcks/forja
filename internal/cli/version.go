package cli

import (
	"github.com/noqcks/forja/internal/version"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			log.Infof("forja %s (%s) built %s", version.Version, version.Commit, version.Date)
		},
	}
}
