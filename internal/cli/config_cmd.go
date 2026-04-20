package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/itsmehatef/dclaw/internal/config"
)

// configCmd is the `dclaw config` subcommand tree. beta.1-paths-hardening
// ships exactly two leaves: `get workspace-root` and `set workspace-root`.
// Future keys plug in as additional leaf subcommands; the homegrown TOML
// parser in internal/config/file.go handles one key at a time.
var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Read and write dclaw config values",
	Long:  `Manage $DCLAW_STATE_DIR/config.toml. Currently exposes 'workspace-root' for the --workspace allow-root.`,
}

var configGetCmd = &cobra.Command{
	Use:   "get <key>",
	Short: "Read a config value",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		key := args[0]
		// PR-C is based on PR-A (no --state-dir flag); resolver picks up
		// DCLAW_STATE_DIR env or the default ~/.dclaw. Once PR-B lands in
		// the series, the flag will flow through config.Resolve automatically.
		paths, err := config.Resolve("", "")
		if err != nil {
			return err
		}
		cfg, err := config.ReadConfigFile(paths.StateDir)
		if err != nil {
			return fmt.Errorf("read config: %w", err)
		}
		switch key {
		case "workspace-root":
			if cfg.WorkspaceRoot == "" {
				// Match §9 of the plan — empty means "not configured".
				fmt.Fprintln(cmd.OutOrStdout(), "(not configured)")
				return nil
			}
			fmt.Fprintln(cmd.OutOrStdout(), cfg.WorkspaceRoot)
			return nil
		default:
			return fmt.Errorf("unknown config key %q (supported: workspace-root)", key)
		}
	},
}

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Write a config value",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		key, value := args[0], args[1]
		// PR-C is based on PR-A (no --state-dir flag); resolver picks up
		// DCLAW_STATE_DIR env or the default ~/.dclaw. Once PR-B lands in
		// the series, the flag will flow through config.Resolve automatically.
		paths, err := config.Resolve("", "")
		if err != nil {
			return err
		}
		cfg, err := config.ReadConfigFile(paths.StateDir)
		if err != nil {
			return fmt.Errorf("read config: %w", err)
		}
		switch key {
		case "workspace-root":
			if strings.TrimSpace(value) == "" {
				return fmt.Errorf("workspace-root cannot be empty")
			}
			cfg.WorkspaceRoot = value
		default:
			return fmt.Errorf("unknown config key %q (supported: workspace-root)", key)
		}
		if err := config.WriteConfigFile(paths.StateDir, cfg); err != nil {
			return fmt.Errorf("write config: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s = %s\n", key, value)
		return nil
	},
}

func init() {
	configCmd.AddCommand(configGetCmd, configSetCmd)
	rootCmd.AddCommand(configCmd)
}
