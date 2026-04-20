// Package audit writes an append-only NDJSON log of every workspace-validator
// decision. One line per AgentCreate call: pass, forbidden, or trust. File
// lives at $DCLAW_STATE_DIR/audit.log, opened with O_APPEND|O_CREATE|O_SYNC
// and mode 0600. No rotation in beta.1.
//
// Shape is fixed by docs/phase-3-beta1-paths-hardening-plan.md §7:
//
//	{"ts":"<RFC3339 UTC ms>", "agent_name":"...", "raw_input":"...",
//	 "canonical":"...", "outcome":"pass|forbidden|trust",
//	 "reason":"...", "policy_version":1}
//
// The daemon keeps a single Logger for its lifetime. LogDecision is safe
// for concurrent use: O_APPEND guarantees POSIX-atomic writes smaller than
// PIPE_BUF, and audit records are well under that bound.
package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Logger holds the open audit.log file. Construct one per daemon via New
// and close it on shutdown.
type Logger struct {
	mu   sync.Mutex
	file *os.File
	// path retained for diagnostics; not used in the hot path.
	path string
}

// Record is the on-wire (really on-disk) NDJSON shape. Fields match §7 of
// the plan exactly. Exported so external tooling (e.g. test readback) can
// reuse the type without re-declaring it.
type Record struct {
	TS            string `json:"ts"`
	AgentName     string `json:"agent_name"`
	RawInput      string `json:"raw_input"`
	Canonical     string `json:"canonical"`
	Outcome       string `json:"outcome"`
	Reason        string `json:"reason"`
	PolicyVersion int    `json:"policy_version"`
}

// New opens or creates $stateDir/audit.log with O_APPEND|O_CREATE|O_SYNC
// and mode 0600. The O_SYNC ensures each record hits disk before the
// syscall returns — we pay a ~1ms penalty per agent-create in exchange
// for surviving kernel panic between write and fsync.
//
// Returns (logger, nil) on success; caller must Close the logger at
// shutdown. It is an error to call New if stateDir does not exist;
// callers should have already created it via os.MkdirAll (the daemon
// does so in LoadConfig).
func New(stateDir string) (*Logger, error) {
	if stateDir == "" {
		return nil, fmt.Errorf("audit: stateDir required")
	}
	path := filepath.Join(stateDir, "audit.log")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY|os.O_SYNC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("audit open %q: %w", path, err)
	}
	return &Logger{file: f, path: path}, nil
}

// Close shuts the audit log. Safe to call multiple times; subsequent calls
// are no-ops.
func (l *Logger) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	return err
}

// Path returns the on-disk location of audit.log. Used by the daemon
// legacy-scan log message and in tests.
func (l *Logger) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

// LogDecision writes one NDJSON record for a validator decision.
//
// outcome must be one of "pass" | "forbidden" | "trust". reason is
// empty except when outcome=="trust", in which case it's the
// operator-supplied --workspace-trust reason string. policyVersion
// tracks denylist semantics changes; beta.1 ships 1.
//
// Returns an error if the write fails; callers log but do not propagate
// (an audit-write failure does not block the agent.create RPC).
func (l *Logger) LogDecision(agentName, rawInput, canonical, outcome, reason string, policyVersion int) error {
	if l == nil {
		return nil
	}
	rec := Record{
		TS:            time.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
		AgentName:     agentName,
		RawInput:      rawInput,
		Canonical:     canonical,
		Outcome:       outcome,
		Reason:        reason,
		PolicyVersion: policyVersion,
	}
	buf, err := json.Marshal(&rec)
	if err != nil {
		return fmt.Errorf("audit marshal: %w", err)
	}
	buf = append(buf, '\n')

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return fmt.Errorf("audit: logger closed")
	}
	if _, err := l.file.Write(buf); err != nil {
		return fmt.Errorf("audit write: %w", err)
	}
	return nil
}
