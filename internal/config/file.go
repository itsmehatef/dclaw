package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// FileConfig is the in-memory representation of $STATE_DIR/config.toml.
// beta.1-paths-hardening ships exactly one key: workspace-root. The file
// grows in follow-ups; for now a homegrown ~40-line parser is cheaper
// than pulling github.com/pelletier/go-toml/v2 for one string.
type FileConfig struct {
	// WorkspaceRoot is the allow-root for agent --workspace paths. Empty
	// means "not configured"; in that case the validator rejects every
	// non-trust path with a pointer to `dclaw config set workspace-root`.
	WorkspaceRoot string
}

// configFileName is the filename inside $STATE_DIR.
const configFileName = "config.toml"

// workspaceRootLineRE matches the one-and-only supported line shape:
//
//	workspace-root = "..."
//
// leading whitespace allowed, trailing whitespace allowed, optional
// inline `# comment` trailing the value (TOML spec), and the value
// itself may contain any non-quote characters. Double quotes are
// mandatory; single quotes are not supported (we would need to decide
// quoting semantics, out of scope for beta.1).
var workspaceRootLineRE = regexp.MustCompile(`^\s*workspace-root\s*=\s*"([^"]*)"\s*(#.*)?$`)

// ReadConfigFile reads $stateDir/config.toml into a FileConfig. A missing
// file returns a zero-value FileConfig and nil error — the expected
// pre-first-`config set` state. Parse errors return the zero value and
// a non-nil error.
func ReadConfigFile(stateDir string) (FileConfig, error) {
	if stateDir == "" {
		return FileConfig{}, fmt.Errorf("config: stateDir required")
	}
	path := filepath.Join(stateDir, configFileName)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return FileConfig{}, nil
		}
		return FileConfig{}, fmt.Errorf("open %q: %w", path, err)
	}
	defer f.Close()

	var cfg FileConfig
	scan := bufio.NewScanner(f)
	lineno := 0
	for scan.Scan() {
		lineno++
		line := scan.Text()
		trimmed := strings.TrimSpace(line)
		// Skip blanks and comments.
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		m := workspaceRootLineRE.FindStringSubmatch(line)
		if m == nil {
			return FileConfig{}, fmt.Errorf("parse %s line %d: unrecognized shape (only 'workspace-root = \"...\"' supported): %q", path, lineno, line)
		}
		cfg.WorkspaceRoot = m[1]
	}
	if err := scan.Err(); err != nil {
		return FileConfig{}, fmt.Errorf("read %s: %w", path, err)
	}
	return cfg, nil
}

// WriteConfigFile writes cfg to $stateDir/config.toml, creating the file
// with mode 0600 if it does not exist. Best-effort atomic: writes to a
// .tmp file then renames. Overwrites any existing content — we currently
// support exactly one key, so "write" means "replace".
//
// Atomicity caveat: POSIX guarantees rename(2) atomicity only when source
// and destination live on the same filesystem. Cross-filesystem cases —
// NFS mounts where a stale-file-handle rename can split, and certain
// container bind-mounts where the .tmp lands on a different layer than
// the target — may not produce an atomic replace. The state-dir is
// almost always a single local filesystem (per docs/workspace-root.md),
// so this caveat is documented for operators with non-standard layouts
// rather than worked around in code.
func WriteConfigFile(stateDir string, cfg FileConfig) error {
	if stateDir == "" {
		return fmt.Errorf("config: stateDir required")
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return fmt.Errorf("mkdir state-dir: %w", err)
	}
	path := filepath.Join(stateDir, configFileName)
	tmp := path + ".tmp"

	// Reject values that would break the grammar on round-trip. The only
	// forbidden byte is double-quote; everything else survives because
	// we quote on write.
	if strings.ContainsRune(cfg.WorkspaceRoot, '"') {
		return fmt.Errorf("workspace-root cannot contain double-quote: %q", cfg.WorkspaceRoot)
	}
	if strings.ContainsAny(cfg.WorkspaceRoot, "\r\n") {
		return fmt.Errorf("workspace-root cannot contain newline: %q", cfg.WorkspaceRoot)
	}

	body := ""
	if cfg.WorkspaceRoot != "" {
		body = fmt.Sprintf("workspace-root = %q\n", cfg.WorkspaceRoot)
	}
	if err := os.WriteFile(tmp, []byte(body), 0o600); err != nil {
		return fmt.Errorf("write %q: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename %q → %q: %w", tmp, path, err)
	}
	return nil
}
