package config

import (
	"fmt"
	"os"
	"path/filepath"

	toml "github.com/pelletier/go-toml/v2"
)

// FileConfig is the in-memory representation of $STATE_DIR/config.toml.
//
// beta.1-paths-hardening shipped a single key (workspace-root) parsed by a
// homegrown ~40-line regex reader because Plan §11 Q2 deferred adopting a
// real TOML library "until the schema grew past one key." With beta.2.2's
// `dclaw init` writing config, beta.2.3's audit-rotation knobs landing on
// the roadmap, and beta.2.4's `dclaw doctor` reading config, beta.2.5
// graduates the parser to github.com/pelletier/go-toml/v2 and adds
// structured sub-tables for [audit] and [daemon] so future tunables fit
// without another rewrite.
//
// Backward compatibility: existing config.toml files that contain only
// `workspace-root = "..."` (everything written by beta.1+ `dclaw init`
// and `dclaw config set`) parse cleanly under the new reader — the new
// fields are zero-valued. See TestReadConfigFileBackwardsCompat.
type FileConfig struct {
	// WorkspaceRoot is the allow-root for agent --workspace paths. Empty
	// means "not configured"; in that case the validator rejects every
	// non-trust path with a pointer to `dclaw config set workspace-root`.
	WorkspaceRoot string `toml:"workspace-root,omitempty"`

	// Audit collects tunables for the audit-log rotation introduced in
	// beta.2.3. All fields default to zero so omitting the [audit] table
	// continues to give the beta.2.3 defaults (10 MB / 5 files) — see
	// cmd/dclawd's startup wiring.
	Audit FileConfigAudit `toml:"audit,omitempty"`

	// Daemon collects daemon-process tunables. Both fields are declared
	// for future use (no consumers wire them in beta.2.5); persisting
	// them in config.toml today means operators can pre-stage values
	// that activate when the wiring lands.
	Daemon FileConfigDaemon `toml:"daemon,omitempty"`
}

// FileConfigAudit is the [audit] sub-table. Mirrors audit.Logger.MaxSize /
// MaxFiles so cmd/dclawd can apply them at construction time. Both keys
// use kebab-case in TOML to match the project's existing convention
// (`workspace-root`).
type FileConfigAudit struct {
	// MaxSizeBytes is the rotation threshold in bytes for audit.log.
	// Zero (or unset) means "use the default" (audit.DefaultMaxSize,
	// 10 MB in beta.2.3). Negative values disable rotation per
	// audit.Logger semantics, so a deliberate -1 here also works.
	MaxSizeBytes int64 `toml:"max-size-bytes,omitempty"`
	// MaxFiles is the total number of audit log files to retain (active
	// audit.log plus historical .1 .. .{N-1}). Zero (or unset) means
	// "use the default" (audit.DefaultMaxFiles, 5 in beta.2.3).
	MaxFiles int `toml:"max-files,omitempty"`
}

// FileConfigDaemon is the [daemon] sub-table. Both fields are declared for
// future use; consumers do not read them in beta.2.5. Declaring the shape
// now lets operators pre-stage values without a future schema migration.
type FileConfigDaemon struct {
	// Socket overrides the daemon's Unix socket path. No consumer reads
	// it yet; --socket flag and DCLAW_STATE_DIR-derived defaults still
	// win in beta.2.5.
	Socket string `toml:"socket,omitempty"`
	// LogLevel overrides the daemon's log level (debug|info|warn|error).
	// No consumer reads it yet; --log-level flag still wins in beta.2.5.
	LogLevel string `toml:"log-level,omitempty"`
}

// configFileName is the filename inside $STATE_DIR.
const configFileName = "config.toml"

// ReadConfigFile reads $stateDir/config.toml into a FileConfig. A missing
// file returns a zero-value FileConfig and nil error — the expected
// pre-first-`config set` state. Parse errors return the zero value and
// a non-nil error.
//
// The signature is preserved verbatim from beta.1's homegrown reader so
// every caller (cmd/dclawd, internal/cli/config_cmd, init_cmd, doctor_cmd)
// keeps working without churn.
func ReadConfigFile(stateDir string) (FileConfig, error) {
	if stateDir == "" {
		return FileConfig{}, fmt.Errorf("config: stateDir required")
	}
	path := filepath.Join(stateDir, configFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return FileConfig{}, nil
		}
		return FileConfig{}, fmt.Errorf("open %q: %w", path, err)
	}
	var cfg FileConfig
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return FileConfig{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return cfg, nil
}

// WriteConfigFile writes cfg to $stateDir/config.toml, creating the file
// with mode 0600 if it does not exist. Best-effort atomic: writes to a
// .tmp file then renames. Overwrites any existing content — the writer is
// the canonical source for the file's shape.
//
// Atomicity caveat: POSIX guarantees rename(2) atomicity only when source
// and destination live on the same filesystem. Cross-filesystem cases —
// NFS mounts where a stale-file-handle rename can split, and certain
// container bind-mounts where the .tmp lands on a different layer than
// the target — may not produce an atomic replace. The state-dir is
// almost always a single local filesystem (per docs/workspace-root.md),
// so this caveat is documented for operators with non-standard layouts
// rather than worked around in code.
//
// Marshaling is delegated to go-toml/v2; values that contain double
// quotes or newlines are escaped per TOML grammar instead of rejected,
// which is a strict-superset relaxation of beta.1's homegrown writer.
func WriteConfigFile(stateDir string, cfg FileConfig) error {
	if stateDir == "" {
		return fmt.Errorf("config: stateDir required")
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return fmt.Errorf("mkdir state-dir: %w", err)
	}
	path := filepath.Join(stateDir, configFileName)
	tmp := path + ".tmp"

	body, err := toml.Marshal(&cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return fmt.Errorf("write %q: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename %q -> %q: %w", tmp, path, err)
	}
	return nil
}
