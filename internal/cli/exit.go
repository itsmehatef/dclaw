package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"

	"github.com/spf13/cobra"
)

// Exit codes (see Section 4 of phase-3-daemon-plan.md).
const (
	ExitOK              = 0
	ExitGeneric         = 1
	ExitUsage           = 2
	ExitInputErr        = 64
	ExitDataErr         = 65
	ExitDaemonDown      = 69 // reused from v0.2.0-cli
	ExitInternal        = 70
	ExitTempFail        = 75
	ExitNoPerm          = 77
)

// DaemonUnreachable converts a low-level dial error into a CLI-facing error
// with a standardized message and the daemon-down exit code.
func DaemonUnreachable(err error) error {
	msg := "dclaw daemon is not running; run 'dclaw daemon start'"
	if err != nil {
		msg = fmt.Sprintf("%s (%v)", msg, err)
	}
	if outputFormat == "json" {
		payload := map[string]any{
			"error":     "daemon_unreachable",
			"message":   msg,
			"exit_code": ExitDaemonDown,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(payload)
	} else {
		fmt.Fprintln(os.Stderr, msg)
	}
	os.Exit(ExitDaemonDown)
	return nil
}

// HandleRPCError formats an RPC error for the CLI. Returns a cobra-friendly
// error after calling os.Exit internally for fatal cases.
func HandleRPCError(cmd *cobra.Command, err error) error {
	if err == nil {
		return nil
	}
	// A bare "connection refused" means the daemon died mid-operation.
	var nerr *net.OpError
	if errors.As(err, &nerr) {
		return DaemonUnreachable(err)
	}
	fmt.Fprintln(cmd.ErrOrStderr(), "error:", err)
	os.Exit(ExitInternal)
	return nil
}

// Legacy helpers kept for any v0.2.0-cli stubs still in place during the
// transition. After alpha.1 completes these should have no live callers.
const NextMilestone = "v0.3.0-daemon"
const ExitCodeNeedsDaemon = ExitDaemonDown

type NotReadyPayload struct {
	Error     string `json:"error"`
	Message   string `json:"message"`
	ExitCode  int    `json:"exit_code"`
	Milestone string `json:"milestone"`
	Command   string `json:"command"`
}
