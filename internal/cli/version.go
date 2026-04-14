package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/realhatefk/dclaw/internal/version"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the dclaw version",
	Long: `Print the dclaw version string, git commit SHA, build date, and Go version.

The version information is stamped at build time via -ldflags. A binary built
without ldflags will report "dev" for all fields.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Fprintf(cmd.OutOrStdout(),
			"dclaw version %s (commit %s, built %s, %s)\n",
			version.Version, version.Commit, version.BuildDate, version.GoVersion(),
		)
		return nil
	},
}
