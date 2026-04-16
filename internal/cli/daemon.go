package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/itsmehatef/dclaw/internal/client"
	"github.com/itsmehatef/dclaw/internal/daemon"
)

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Manage the dclaw daemon (control plane)",
	Long:  `Start, stop, and inspect the dclawd daemon.`,
}

var daemonStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the dclaw daemon",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		cfg, err := daemon.LoadConfig(daemonSocket, filepath.Join(home, ".dclaw"), "info")
		if err != nil {
			return err
		}

		// Already running?
		if pid, _ := cfg.ReadPIDFile(); pid > 0 {
			if err := syscall.Kill(pid, 0); err == nil {
				fmt.Fprintf(cmd.OutOrStdout(), "daemon already running (pid %d)\n", pid)
				return nil
			}
		}

		// Locate the dclawd binary: prefer the one next to the dclaw binary.
		dclawdPath, err := locateDclawd()
		if err != nil {
			return err
		}

		daemonProc := exec.Command(dclawdPath,
			"--socket", cfg.SocketPath,
			"--state-dir", cfg.StateDir,
		)
		// Detach: new process group, discard stdio.
		daemonProc.Stdin = nil
		daemonProc.Stdout, _ = os.OpenFile(cfg.LogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		daemonProc.Stderr = daemonProc.Stdout
		daemonProc.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err := daemonProc.Start(); err != nil {
			return fmt.Errorf("fork dclawd: %w", err)
		}
		// Don't wait; detach.
		_ = daemonProc.Process.Release()

		// Poll until the socket is reachable (up to 5s).
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(cfg.SocketPath); err == nil {
				fmt.Fprintf(cmd.OutOrStdout(), "dclaw daemon started (pid %d, socket %s)\n",
					daemonProc.Process.Pid, cfg.SocketPath)
				return nil
			}
			time.Sleep(100 * time.Millisecond)
		}
		return errors.New("daemon did not become ready within 5s (check ~/.dclaw/logs/daemon.log)")
	},
}

var daemonStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the dclaw daemon",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		home, _ := os.UserHomeDir()
		cfg, err := daemon.LoadConfig(daemonSocket, filepath.Join(home, ".dclaw"), "info")
		if err != nil {
			return err
		}
		pid, err := cfg.ReadPIDFile()
		if err != nil || pid == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "daemon not running")
			return nil
		}
		if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
			return fmt.Errorf("kill pid %d: %w", pid, err)
		}
		// Wait up to 10s for exit.
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if err := syscall.Kill(pid, 0); err != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "dclaw daemon stopped (pid %d)\n", pid)
				return nil
			}
			time.Sleep(100 * time.Millisecond)
		}
		_ = syscall.Kill(pid, syscall.SIGKILL)
		return fmt.Errorf("daemon did not exit in 10s; sent SIGKILL")
	},
}

var daemonStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the dclaw daemon status",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
		defer cancel()
		return withClient(ctx, func(c *client.RPCClient) error {
			s, err := c.DaemonStatus(ctx)
			if err != nil {
				return HandleRPCError(cmd, err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), s)
			return nil
		})
	},
}

func init() {
	daemonCmd.AddCommand(daemonStartCmd, daemonStopCmd, daemonStatusCmd)
}

// locateDclawd returns the path to the dclawd binary. Search order:
//   1. $DCLAWD_BIN
//   2. a sibling named "dclawd" next to the current dclaw binary
//   3. PATH lookup
func locateDclawd() (string, error) {
	if env := os.Getenv("DCLAWD_BIN"); env != "" {
		return env, nil
	}
	if self, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(self), "dclawd")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return exec.LookPath("dclawd")
}
