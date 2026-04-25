package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/itsmehatef/dclaw/internal/config"
	"github.com/itsmehatef/dclaw/internal/paths"
)

// initCmd is the `dclaw init` first-run wizard. Solves plan §12 follow-up #2
// ("Easier setup for workspace-root"): before this command, a fresh user
// would `agent create`, see workspace_forbidden, read the remediation, run
// `dclaw config set workspace-root <path>`, then retry. `dclaw init` collapses
// that loop to a single step. Idempotent — re-running with workspace-root
// already configured prints the current value and exits 0.
//
// Flags:
//   - --yes: non-interactive, accept the default ($HOME/dclaw).
//   - --workspace-root <path>: explicit path, bypasses prompt and default.
//
// Validation reuses paths.Policy.Validate with AllowTrust=true so the same
// denylist (system paths, /etc, /var, the Docker socket etc.) that protects
// `agent create` also protects the configured workspace-root. We can't run
// the full allow-root prefix check here because we ARE configuring the
// allow-root.
var (
	initYes           bool
	initWorkspaceRoot string
	// initDenylist is the denylist used by the init wizard's validator. It
	// defaults to paths.DefaultDenylist; tests override it to strip
	// /var, /private/var, /private/tmp so t.TempDir paths on macOS (which
	// resolve under /private/var/folders/...) are not rejected by an entry
	// that overlaps the OS's temp-dir storage. The override mirrors the
	// pattern used by internal/paths/{policy,opensafe}_test.go.
	initDenylist = paths.DefaultDenylist
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "First-run setup: configure workspace-root",
	Long: `Configure the workspace-root for agent --workspace paths.

This is the easier first step before 'dclaw daemon start' / 'dclaw agent create'.
Writes $DCLAW_STATE_DIR/config.toml with the chosen workspace-root, creating
the directory at mode 0700 if it does not exist.

If workspace-root is already configured, prints the current value and exits 0
without modifying the file (idempotent).

Behaviors:
  - Default workspace-root: $HOME/dclaw.
  - Interactive prompt with default-on-empty-line; --yes for non-interactive
    runs; --workspace-root <path> for explicit override.
  - Reuses the same denylist used by 'agent create' (refuses /etc, /var, the
    Docker socket, etc.).`,
	Args: cobra.NoArgs,
	RunE: runInit,
}

func init() {
	initCmd.Flags().BoolVar(&initYes, "yes", false,
		"non-interactive: accept the default workspace-root without prompting")
	initCmd.Flags().StringVar(&initWorkspaceRoot, "workspace-root", "",
		"explicit workspace-root path (bypasses prompt and default)")
	rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, args []string) error {
	out := cmd.OutOrStdout()

	resolved, err := config.Resolve(stateDirFlag, "")
	if err != nil {
		return fmt.Errorf("resolve state-dir: %w", err)
	}
	stateDir := resolved.StateDir

	// Idempotency: if workspace-root is already set, print and exit 0.
	cfg, err := config.ReadConfigFile(stateDir)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	if cfg.WorkspaceRoot != "" {
		fmt.Fprintf(out, "workspace-root already configured: %s\n", cfg.WorkspaceRoot)
		return nil
	}

	// Resolve the candidate workspace-root from flags > prompt > default.
	candidate, err := chooseWorkspaceRoot(cmd)
	if err != nil {
		return err
	}

	// Validate (denylist + structural checks). Refuses /etc, /var, NUL bytes,
	// relative paths etc. We can't run the AllowRoot prefix check because
	// we ARE configuring the AllowRoot, so we set AllowTrust=true to bypass
	// that arm while keeping every other guard.
	canonical, created, err := validateAndPrepareWorkspaceRoot(candidate)
	if err != nil {
		return err
	}

	// Persist.
	cfg.WorkspaceRoot = canonical
	if err := config.WriteConfigFile(stateDir, cfg); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	fmt.Fprintf(out, "workspace-root configured: %s\n", canonical)
	if created {
		fmt.Fprintln(out, "created (mode 0700)")
	} else {
		fmt.Fprintln(out, "using existing dir")
	}
	return nil
}

// chooseWorkspaceRoot returns the path the user picked, by precedence:
//
//  1. --workspace-root flag.
//  2. Interactive prompt with default fallback (only when stdin is a TTY
//     AND --yes was NOT passed).
//  3. Default $HOME/dclaw (used by --yes or non-TTY shells).
//
// Returns the raw user input or default — abs/clean/validation happens
// later in validateAndPrepareWorkspaceRoot so all error wording goes through
// one path.
func chooseWorkspaceRoot(cmd *cobra.Command) (string, error) {
	if cmd.Flags().Changed("workspace-root") {
		if strings.TrimSpace(initWorkspaceRoot) == "" {
			return "", fmt.Errorf("--workspace-root requires a non-empty path")
		}
		return initWorkspaceRoot, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	defaultPath := filepath.Join(home, "dclaw")

	// Non-interactive paths: --yes, or stdin not a TTY.
	if initYes || !isatty.IsTerminal(os.Stdin.Fd()) {
		return defaultPath, nil
	}

	// Interactive prompt. Empty line ⇒ default.
	fmt.Fprintf(cmd.OutOrStdout(), "Configure workspace-root [%s]: ", defaultPath)
	r := bufio.NewReader(os.Stdin)
	line, err := r.ReadString('\n')
	if err != nil && line == "" {
		// EOF on a TTY (e.g. piped /dev/null with isatty true on some shells)
		// is not really expected, but fall back to the default.
		return defaultPath, nil
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return defaultPath, nil
	}
	return line, nil
}

// validateAndPrepareWorkspaceRoot canonicalizes the candidate path, refuses
// it if denylisted, mkdir's it at mode 0700 if missing, and returns the
// final canonical path along with whether mkdir actually created the dir
// (vs. the dir already existed).
//
// Validation pipeline:
//  1. Reject empty / NUL / control / non-absolute / non-clean.
//  2. Walk up to deepest existing ancestor and run paths.Policy.Validate
//     with AllowTrust=true on it — catches denylisted parents like /etc/foo
//     before we attempt mkdir.
//  3. Run paths.Policy.Validate with AllowTrust=true on the cleaned target
//     once it exists (post-mkdir) so symlink-component escapes are caught.
//  4. Refuse if a symlink in the resolved path produced a different path
//     than the cleaned input — operators who want a symlinked workspace-root
//     should resolve it themselves.
func validateAndPrepareWorkspaceRoot(raw string) (string, bool, error) {
	if strings.TrimSpace(raw) == "" {
		return "", false, fmt.Errorf("workspace-root cannot be empty")
	}
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if c == 0x00 {
			return "", false, fmt.Errorf("workspace-root contains NUL byte at index %d", i)
		}
		if c == '\n' || c == '\r' {
			return "", false, fmt.Errorf("workspace-root contains newline at index %d", i)
		}
		if c < 0x20 && c != '\t' {
			return "", false, fmt.Errorf("workspace-root contains control byte 0x%02x at index %d", c, i)
		}
	}

	abs, err := filepath.Abs(raw)
	if err != nil {
		return "", false, fmt.Errorf("workspace-root: abs %q: %w", raw, err)
	}
	cleaned := filepath.Clean(abs)
	if !filepath.IsAbs(cleaned) {
		return "", false, fmt.Errorf("workspace-root must be absolute, got %q", raw)
	}

	policy := paths.Policy{
		Denylist:   initDenylist,
		AllowTrust: true,
	}

	// Pre-mkdir denylist check. Walk up to the deepest existing ancestor so
	// EvalSymlinks inside Validate has something to chew on. If `/etc/foo`
	// doesn't exist, the deepest existing ancestor is `/etc`, and Validate
	// rejects /etc as denylisted with a clear error — better than letting
	// mkdir fail with EACCES or worse.
	ancestor := deepestExistingAncestor(cleaned)
	if _, vErr := policy.Validate(ancestor); vErr != nil {
		return "", false, fmt.Errorf("workspace-root rejected: %w", vErr)
	}

	// mkdir if missing. Mode 0700: workspace-root is operator-private.
	created := false
	if _, statErr := os.Stat(cleaned); statErr != nil {
		if !os.IsNotExist(statErr) {
			return "", false, fmt.Errorf("stat %q: %w", cleaned, statErr)
		}
		if err := os.MkdirAll(cleaned, 0o700); err != nil {
			return "", false, fmt.Errorf("mkdir %q: %w", cleaned, err)
		}
		created = true
	}

	// Post-mkdir full validation, including EvalSymlinks. Catches the case
	// where `cleaned` resolved through a symlink pointing into denylisted
	// territory — defense-in-depth on top of the pre-mkdir ancestor check.
	canonical, vErr := policy.Validate(cleaned)
	if vErr != nil {
		return "", created, fmt.Errorf("workspace-root rejected: %w", vErr)
	}
	return canonical, created, nil
}

// deepestExistingAncestor walks up from path until it finds a path that
// exists on disk. Returns the original path if it exists; "/" in the worst
// case (which always exists). Used to give the denylist validator a path
// it can EvalSymlinks even when the target doesn't exist yet.
func deepestExistingAncestor(path string) string {
	p := filepath.Clean(path)
	for {
		if _, err := os.Stat(p); err == nil {
			return p
		}
		parent := filepath.Dir(p)
		if parent == p {
			return p
		}
		p = parent
	}
}
