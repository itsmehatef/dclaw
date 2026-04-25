// Package audit writes an append-only NDJSON log of every workspace-validator
// decision. One line per AgentCreate call: pass, forbidden, or trust. File
// lives at $DCLAW_STATE_DIR/audit.log, opened with O_APPEND|O_CREATE|O_SYNC
// and mode 0600.
//
// Shape is fixed by docs/phase-3-beta1-paths-hardening-plan.md §7:
//
//	{"ts":"<RFC3339 UTC ms>", "agent_name":"...", "raw_input":"...",
//	 "canonical":"...", "outcome":"pass|forbidden|trust",
//	 "reason":"...", "policy_version":1}
//
// The daemon keeps a single Logger for its lifetime. LogDecision is safe
// for concurrent use: a per-Logger mutex serializes writes (and rotation),
// and audit records are well under PIPE_BUF.
//
// Rotation: when the active audit.log would exceed Logger.MaxSize after
// the next record, the file is closed, audit.log.{N-1} is renamed to
// audit.log.{N} for N from MaxFiles-1 down to 1, audit.log is renamed
// to audit.log.1, and a fresh audit.log is opened. The slot at
// audit.log.{MaxFiles} (if present) is removed before the rename chain
// so the rename target never pre-exists. Rotation runs in-process under
// the same mutex that serializes writes; callers see one slightly slower
// LogDecision call when a rotation triggers and otherwise no change.
//
// Defaults: MaxSize=10MB, MaxFiles=5. Set MaxSize<=0 on the Logger after
// New to disable rotation entirely (legacy unbounded-growth behavior).
package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Default rotation parameters. Production daemons get these via New;
// tests override Logger.MaxSize / Logger.MaxFiles after construction.
const (
	DefaultMaxSize  int64 = 10 * 1024 * 1024 // 10 MB
	DefaultMaxFiles int   = 5
)

// Logger holds the open audit.log file. Construct one per daemon via New
// and close it on shutdown.
//
// MaxSize and MaxFiles control size-based rotation. Both are exported so
// tests can override them after New (e.g. MaxSize=200, MaxFiles=3 to
// force rotation in a few iterations). Set MaxSize<=0 to disable
// rotation; MaxFiles<=1 means "only audit.log, no .1/.2/... siblings".
type Logger struct {
	mu   sync.Mutex
	file *os.File
	// path retained for diagnostics; not used in the hot path.
	path string

	// MaxSize is the rotation threshold in bytes. When the current
	// audit.log size plus the next record would exceed MaxSize, the
	// file is rotated before the write. <=0 disables rotation.
	MaxSize int64
	// MaxFiles is the maximum number of audit log files to retain
	// (the active audit.log plus MaxFiles-1 historical siblings
	// audit.log.1 .. audit.log.{MaxFiles-1}). The oldest file is
	// dropped silently on rotation when the chain is full.
	MaxFiles int
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
//
// The returned Logger has MaxSize=DefaultMaxSize and MaxFiles=DefaultMaxFiles.
// Tests may override either field directly after New; production callers
// take the defaults.
func New(stateDir string) (*Logger, error) {
	if stateDir == "" {
		return nil, fmt.Errorf("audit: stateDir required")
	}
	path := filepath.Join(stateDir, "audit.log")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY|os.O_SYNC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("audit open %q: %w", path, err)
	}
	return &Logger{
		file:     f,
		path:     path,
		MaxSize:  DefaultMaxSize,
		MaxFiles: DefaultMaxFiles,
	}, nil
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
//
// Rotation: if the current file size plus len(buf) would exceed
// l.MaxSize, the file is rotated under the same mutex before the write.
// A rotation error is returned to the caller; the new record is NOT
// written in that case (the existing file is left intact for the
// operator to investigate).
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

	// Size-based rotation check: if MaxSize is set and the next record
	// would push the file past the threshold, rotate first. Stat the
	// open *os.File so we see the current on-disk size (O_APPEND +
	// O_SYNC means writes have already hit disk, so this matches what
	// a fresh stat of the path would return).
	if l.MaxSize > 0 {
		fi, statErr := l.file.Stat()
		if statErr != nil {
			return fmt.Errorf("audit stat: %w", statErr)
		}
		if fi.Size()+int64(len(buf)) > l.MaxSize {
			if err := l.rotateLocked(); err != nil {
				return fmt.Errorf("audit rotate: %w", err)
			}
		}
	}

	if _, err := l.file.Write(buf); err != nil {
		return fmt.Errorf("audit write: %w", err)
	}
	return nil
}

// rotateLocked performs size-based rotation. Caller must hold l.mu.
//
// Procedure:
//  1. Close the current *os.File.
//  2. Remove audit.log.{MaxFiles} if it exists, so the rename chain
//     never targets an existing file (Windows-portable, and on POSIX
//     just spares us a stray clobber).
//  3. Rename audit.log.{N-1} -> audit.log.{N} for N from MaxFiles-1
//     down to 1. Missing intermediates are skipped silently — a fresh
//     workspace may not have all slots populated yet.
//  4. Rename audit.log -> audit.log.1.
//  5. Open a fresh audit.log with the same flags/mode and assign to
//     l.file.
//
// If MaxFiles<=1 the historical chain is skipped entirely: audit.log
// is removed and a fresh empty file is opened.
func (l *Logger) rotateLocked() error {
	// Close the active file. If close fails we still attempt the
	// rename chain — losing the file handle is recoverable, but
	// leaving the daemon stuck at the size threshold is not.
	if l.file != nil {
		_ = l.file.Close()
		l.file = nil
	}

	maxFiles := l.MaxFiles
	if maxFiles < 1 {
		maxFiles = 1
	}

	if maxFiles >= 2 {
		// Drop the oldest slot first so the chain has somewhere to
		// shift into. os.Remove on a missing file returns *PathError
		// with ENOENT; we ignore that case explicitly.
		oldest := fmt.Sprintf("%s.%d", l.path, maxFiles-1)
		if err := os.Remove(oldest); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove oldest %q: %w", oldest, err)
		}

		// Shift audit.log.{N-1} -> audit.log.{N} for N from
		// maxFiles-1 down to 2. (N=1 is the rename of audit.log
		// itself, handled below.)
		for n := maxFiles - 1; n >= 2; n-- {
			src := fmt.Sprintf("%s.%d", l.path, n-1)
			dst := fmt.Sprintf("%s.%d", l.path, n)
			if _, err := os.Stat(src); err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return fmt.Errorf("stat %q: %w", src, err)
			}
			if err := os.Rename(src, dst); err != nil {
				return fmt.Errorf("rename %q -> %q: %w", src, dst, err)
			}
		}

		// audit.log -> audit.log.1.
		dst := l.path + ".1"
		if err := os.Rename(l.path, dst); err != nil {
			return fmt.Errorf("rename %q -> %q: %w", l.path, dst, err)
		}
	} else {
		// MaxFiles<=1: no historical retention. Just unlink the
		// active file before reopening.
		if err := os.Remove(l.path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove %q: %w", l.path, err)
		}
	}

	// Open a fresh audit.log with the same flags/mode as New.
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY|os.O_SYNC, 0o600)
	if err != nil {
		return fmt.Errorf("reopen %q: %w", l.path, err)
	}
	l.file = f
	return nil
}
