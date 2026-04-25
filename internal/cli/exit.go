package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"

	"github.com/spf13/cobra"

	"github.com/itsmehatef/dclaw/internal/config"
	"github.com/itsmehatef/dclaw/internal/protocol"
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
	// beta.1: structured render for workspace_forbidden (-32007) mirrors
	// the feature_not_ready precedent. ExitDataErr (65).
	var rpcErr *protocol.RPCError
	if errors.As(err, &rpcErr) && rpcErr.Code == protocol.ErrWorkspaceForbidden {
		renderWorkspaceForbidden(cmd, rpcErr)
		os.Exit(ExitDataErr)
		return nil
	}
	fmt.Fprintln(cmd.ErrOrStderr(), "error:", err)
	os.Exit(ExitInternal)
	return nil
}

// Remediation is one entry in WorkspaceForbiddenPayload.Remediations.
// Replaces the prior []map[string]string shape with compile-time field
// names. JSON wire shape is byte-identical to the previous map: the JSON
// tags ("kind", "command") emit the same keys in the same order.
type Remediation struct {
	Kind    string `json:"kind"`
	Command string `json:"command"`
}

// WorkspaceForbiddenPayload is the JSON-output shape for workspace_forbidden
// errors. Mirrors the feature_not_ready shape. "remediations" is always three
// entries in insertion-order matching the plan.
type WorkspaceForbiddenPayload struct {
	Error        string        `json:"error"`
	Message      string        `json:"message"`
	ExitCode     int           `json:"exit_code"`
	AllowRoot    string        `json:"allow_root"`
	Resolved     string        `json:"resolved"`
	Reason       string        `json:"reason"`
	Remediations []Remediation `json:"remediations"`
}

// renderWorkspaceForbidden writes the structured error to stderr (or to
// stdout as JSON if --output=json) with the exact text from §8 of the
// plan. Caller is responsible for calling os.Exit(ExitDataErr) afterwards.
func renderWorkspaceForbidden(cmd *cobra.Command, rpcErr *protocol.RPCError) {
	// Pull fields from Data when present.
	var resolved, reason string
	if m, ok := rpcErr.Data.(map[string]any); ok {
		if v, ok := m["resolved"].(string); ok {
			resolved = v
		}
		if v, ok := m["reason"].(string); ok {
			reason = v
		}
	}
	if reason == "" {
		reason = rpcErr.Message
	}

	// Configured allow-root: read from the caller's own config. If the
	// caller does not have one configured we surface the "(not configured)"
	// line — the daemon does not know the caller's config file.
	allowRoot := "(not configured — run 'dclaw config set workspace-root <path>')"
	paths, perr := config.Resolve(stateDirFlag, "")
	if perr == nil {
		if fc, ferr := config.ReadConfigFile(paths.StateDir); ferr == nil && fc.WorkspaceRoot != "" {
			allowRoot = fc.WorkspaceRoot
		}
	}

	if outputFormat == "json" {
		payload := WorkspaceForbiddenPayload{
			Error:     "workspace_forbidden",
			Message:   rpcErr.Message,
			ExitCode:  ExitDataErr,
			AllowRoot: allowRoot,
			Resolved:  resolved,
			Reason:    reason,
			Remediations: []Remediation{
				{Kind: "use_inside_root", Command: "dclaw agent create ... --workspace <path-inside-root>"},
				{Kind: "change_root", Command: "dclaw config set workspace-root <new-path>"},
				{Kind: "trust_override", Command: "dclaw agent create ... --workspace-trust \"<reason>\""},
			},
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(payload)
		return
	}

	w := cmd.ErrOrStderr()
	fmt.Fprintf(w, "error: %s\n", rpcErr.Message)
	fmt.Fprintln(w, "")
	fmt.Fprintf(w, "  Resolved path:         %s\n", resolved)
	fmt.Fprintf(w, "  Configured allow-root: %s\n", allowRoot)
	fmt.Fprintf(w, "  Reason:                %s\n", reason)
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "To fix, do one of the following:")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "  1. Use a path inside the allow-root:")
	fmt.Fprintln(w, "       dclaw agent create <name> --workspace <path-inside-root> --image=<image>")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "  2. Change the allow-root:")
	fmt.Fprintln(w, "       dclaw config set workspace-root <new-path>")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "  3. Override with explicit operator trust (persisted in state.db, shown in")
	fmt.Fprintln(w, "     'agent describe' and written to $DCLAW_STATE_DIR/audit.log):")
	fmt.Fprintln(w, "       dclaw agent create <name> --workspace <path> \\")
	fmt.Fprintln(w, "         --workspace-trust \"reason string required\" --image=<image>")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "See docs/workspace-root.md for details.")
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
