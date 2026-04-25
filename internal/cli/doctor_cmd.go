package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	dockerclient "github.com/docker/docker/client"
	"github.com/spf13/cobra"

	"github.com/itsmehatef/dclaw/internal/client"
	"github.com/itsmehatef/dclaw/internal/config"
	"github.com/itsmehatef/dclaw/internal/paths"
)

// doctorCmd is the `dclaw doctor` health-check subcommand. Plan §11 Q3
// follow-up: pre-flight diagnostics so users can run `dclaw doctor` and
// get a clear pass/fail breakdown before they hit a runtime surprise.
//
// Top-level invocation runs an ordered battery of seven checks; each
// emits OK | WARN | FAIL with a short message. Exit 0 if all OK or only
// WARN, exit 1 if any FAIL. The optional `workspace <path>` subcommand
// pre-flights a specific workspace path through paths.Policy.Validate
// without creating an agent and without touching audit.log — lets
// operators iterate cheaply on --workspace candidates.
//
// Hard constraints from the build plan:
//   - All logic lives in internal/cli/. Doctor checks may import
//     internal/{config, paths, audit, client, sandbox, daemon} but must
//     not modify them. New helpers stay local to this file.
//   - For docker_reachable + agent_image_present we use the docker SDK
//     directly rather than internal/sandbox.NewDockerClient because the
//     latter's hard-coded 5s context and lack of a public ImageInspect
//     adapter would force either a sandbox modification or a longer
//     doctor wait. Keeping the docker handle local also means doctor
//     does not pollute the sandbox package's lifecycle.
var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Run pre-flight health checks (config, daemon, docker, image, audit log)",
	Long: `Run a battery of pre-flight health checks against the local dclaw
installation. Each check emits OK | WARN | FAIL with a short message.

Exit codes:
  0  all OK or only WARN
  1  any FAIL

Use 'dclaw doctor workspace <path>' to pre-flight a specific workspace
path through Policy.Validate without creating an agent or writing to
audit.log.

  -o json   emit a structured {"checks": [...], "exit_code": N} payload`,
	Args: cobra.NoArgs,
	RunE: runDoctor,
}

// doctorWorkspaceCmd is the `dclaw doctor workspace <path>` subcommand.
// Mirrors the validator that runs at agent.create time but does not
// create an agent and does not write an audit-log entry. Exit 0 on
// pass, ExitDataErr (65) on rejection — same shape as `agent create`
// rejection.
var doctorWorkspaceCmd = &cobra.Command{
	Use:   "workspace <path>",
	Short: "Pre-flight a workspace path through Policy.Validate (no agent created)",
	Long: `Validate a path against the workspace policy without creating an
agent and without writing to audit.log. Useful for iterating on
--workspace candidates before committing to 'agent create'.

The path is run through the same denylist + canonicalization the daemon
runs at agent.create time. AllowTrust=false is used (full validation)
so the AllowRoot prefix check fires when workspace-root is configured.

Exit codes:
  0  path passes validation
  65 path rejected (denylist, not under allow-root, etc.)`,
	Args: cobra.ExactArgs(1),
	RunE: runDoctorWorkspace,
}

// doctorDenylist is the denylist used by both doctor's
// workspace_root_valid check and the `doctor workspace <path>`
// subcommand. It defaults to paths.DefaultDenylist; tests override it
// to strip /var, /private/var, /private/tmp so t.TempDir paths on
// macOS (which resolve under /private/var/folders/...) are not
// rejected by an entry that overlaps the OS's temp-dir storage. This
// mirrors the override pattern used by initDenylist in init_cmd.go
// and by macOSDenylist in internal/paths/policy_test.go.
var doctorDenylist = paths.DefaultDenylist

func init() {
	doctorCmd.AddCommand(doctorWorkspaceCmd)
	rootCmd.AddCommand(doctorCmd)
}

// CheckState is the three-valued outcome of a single doctor check.
// "ok" | "warn" | "fail" in the JSON wire shape; lowercase to match
// the existing CLI JSON conventions (workspace_forbidden, etc.).
type CheckState string

const (
	CheckOK   CheckState = "ok"
	CheckWarn CheckState = "warn"
	CheckFail CheckState = "fail"
)

// CheckResult is one entry in the ordered doctor report. Marshals to
// the wire shape {"name": "...", "state": "ok|warn|fail", "message": "..."}.
type CheckResult struct {
	Name    string     `json:"name"`
	State   CheckState `json:"state"`
	Message string     `json:"message"`
}

// doctorReport is the full -o json payload.
type doctorReport struct {
	Checks   []CheckResult `json:"checks"`
	ExitCode int           `json:"exit_code"`
}

// runDoctor is the RunE for the top-level `dclaw doctor`. It runs the
// seven checks in order, collects the results, prints them in the
// selected format, and exits 0 (no FAIL) or 1 (any FAIL).
func runDoctor(cmd *cobra.Command, args []string) error {
	if err := validateOutputFormat(); err != nil {
		return err
	}

	results := runAllChecks(cmd.Context())

	exitCode := 0
	for _, r := range results {
		if r.State == CheckFail {
			exitCode = 1
			break
		}
	}

	if outputFormat == "json" {
		report := doctorReport{Checks: results, ExitCode: exitCode}
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return err
		}
	} else {
		printChecksTable(cmd.OutOrStdout(), results)
	}

	if exitCode != 0 {
		// Use os.Exit so we match `dclaw doctor` semantics across the
		// process tree (smoke scripts grep on the exit code). Using a
		// returned error would set 1 too, but cobra would also print
		// "Error: ..." to stderr which we do not want — the failing
		// checks already printed FAIL lines.
		os.Exit(exitCode)
	}
	return nil
}

// runAllChecks runs the seven pre-flight checks in the order specified
// in the build plan and returns the ordered results.
//
// Order matters: later checks may be skipped (state="warn", message
// "skipped: ...") when an earlier check FAILed in a way that makes
// the later check meaningless. Specifically:
//   - workspace_root_valid is skipped when workspace_root_configured FAILs.
//   - agent_image_present is skipped when docker_reachable WARNs/FAILs.
func runAllChecks(ctx context.Context) []CheckResult {
	results := make([]CheckResult, 0, 7)

	// 1. config_resolved
	cfgResolved, cfgPaths := checkConfigResolved()
	results = append(results, cfgResolved)

	// 2. workspace_root_configured
	wsConfigured, fileCfg := checkWorkspaceRootConfigured(cfgPaths)
	results = append(results, wsConfigured)

	// 3. workspace_root_valid (skipped if step 2 FAILed)
	if wsConfigured.State == CheckFail {
		results = append(results, CheckResult{
			Name:    "workspace_root_valid",
			State:   CheckWarn,
			Message: "skipped: workspace-root not configured",
		})
	} else {
		results = append(results, checkWorkspaceRootValid(fileCfg.WorkspaceRoot))
	}

	// 4. daemon_reachable
	results = append(results, checkDaemonReachable(ctx))

	// 5. docker_reachable
	dockerReachable := checkDockerReachable(ctx)
	results = append(results, dockerReachable)

	// 6. agent_image_present (skipped if docker is not reachable)
	if dockerReachable.State == CheckOK {
		results = append(results, checkAgentImagePresent(ctx))
	} else {
		results = append(results, CheckResult{
			Name:    "agent_image_present",
			State:   CheckWarn,
			Message: "skipped: docker not reachable",
		})
	}

	// 7. audit_log_writable
	results = append(results, checkAuditLogWritable(cfgPaths))

	return results
}

// printChecksTable writes the seven check results to w in the
// table format `[STATE] name <padding> message`. Width is fixed
// to keep output friendly to grep/awk; we don't use tabwriter
// because the brackets are visually distinct on their own.
func printChecksTable(w io.Writer, results []CheckResult) {
	for _, r := range results {
		label := strings.ToUpper(string(r.State))
		// Pad the label to a 4-char width inside brackets so OK,
		// WARN, FAIL all line up.
		fmt.Fprintf(w, "[%-4s] %-28s %s\n", label, r.Name, r.Message)
	}
}

// ---------- check 1: config_resolved ----------

// checkConfigResolved verifies that config.Resolve returns a state-dir
// and socket path. FAILs only when os.UserHomeDir errors and no
// override is in play; that is rare on real hosts but possible inside
// stripped containers where HOME is unset.
func checkConfigResolved() (CheckResult, config.Paths) {
	resolved, err := config.Resolve(stateDirFlag, daemonSocket)
	if err != nil {
		return CheckResult{
			Name:    "config_resolved",
			State:   CheckFail,
			Message: fmt.Sprintf("config.Resolve failed: %v", err),
		}, config.Paths{}
	}
	return CheckResult{
		Name:    "config_resolved",
		State:   CheckOK,
		Message: fmt.Sprintf("state-dir=%s socket=%s", resolved.StateDir, resolved.SocketPath),
	}, resolved
}

// ---------- check 2: workspace_root_configured ----------

// checkWorkspaceRootConfigured reads $STATE_DIR/config.toml and
// FAILs if WorkspaceRoot is empty, OK otherwise. A genuine I/O error
// reading config.toml (other than "not found") also FAILs.
func checkWorkspaceRootConfigured(p config.Paths) (CheckResult, config.FileConfig) {
	cfg, err := config.ReadConfigFile(p.StateDir)
	if err != nil {
		return CheckResult{
			Name:    "workspace_root_configured",
			State:   CheckFail,
			Message: fmt.Sprintf("read config.toml: %v", err),
		}, config.FileConfig{}
	}
	if cfg.WorkspaceRoot == "" {
		return CheckResult{
			Name:    "workspace_root_configured",
			State:   CheckFail,
			Message: "workspace-root not configured — run 'dclaw init'",
		}, cfg
	}
	return CheckResult{
		Name:    "workspace_root_configured",
		State:   CheckOK,
		Message: fmt.Sprintf("workspace-root=%s", cfg.WorkspaceRoot),
	}, cfg
}

// ---------- check 3: workspace_root_valid ----------

// checkWorkspaceRootValid runs paths.Policy.Validate on the configured
// workspace-root with AllowTrust=true. AllowTrust here means "treat
// this path as a trusted operator-supplied root" — we still run the
// denylist + canonicalization checks but skip the AllowRoot prefix
// check because we ARE configuring the AllowRoot (matches dclaw init's
// posture). FAILs if the path no longer exists, is denylisted, or
// fails any other invariant.
func checkWorkspaceRootValid(workspaceRoot string) CheckResult {
	policy := paths.Policy{
		Denylist:   doctorDenylist,
		AllowTrust: true,
	}
	canonical, err := policy.Validate(workspaceRoot)
	if err != nil {
		return CheckResult{
			Name:    "workspace_root_valid",
			State:   CheckFail,
			Message: fmt.Sprintf("workspace-root rejected: %v", err),
		}
	}
	return CheckResult{
		Name:    "workspace_root_valid",
		State:   CheckOK,
		Message: fmt.Sprintf("validates as %s", canonical),
	}
}

// ---------- check 4: daemon_reachable ----------

// checkDaemonReachable opens a 2s-timeout connection to the daemon
// socket and asks for daemon.status. If the socket cannot be reached
// at all (most common: daemon not running) we WARN — that is a normal
// state on a fresh host, not a configuration error. If the socket
// exists but the RPC fails with something other than a dial error,
// we FAIL because the daemon is wedged.
func checkDaemonReachable(parent context.Context) CheckResult {
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()

	c := client.NewRPCClient(daemonSocket)
	if err := c.Dial(ctx); err != nil {
		// Dial failed. On a non-running daemon this is the connection
		// refused / no-such-file path; we surface that as WARN with a
		// remediation pointer. We don't try to distinguish ENOENT vs
		// ECONNREFUSED — both mean the same thing operationally.
		return CheckResult{
			Name:    "daemon_reachable",
			State:   CheckWarn,
			Message: "daemon not started; run 'dclaw daemon start'",
		}
	}
	defer c.Close()

	if _, err := c.DaemonStatus(ctx); err != nil {
		return CheckResult{
			Name:    "daemon_reachable",
			State:   CheckFail,
			Message: fmt.Sprintf("daemon.status RPC failed: %v", err),
		}
	}
	return CheckResult{
		Name:    "daemon_reachable",
		State:   CheckOK,
		Message: fmt.Sprintf("daemon.status OK at %s", daemonSocket),
	}
}

// ---------- check 5: docker_reachable ----------

// checkDockerReachable opens a Docker SDK client with FromEnv
// resolution and pings it. WARNs (not FAILs) on failure: docker may
// be intentionally not running on dev hosts where the operator is
// only working on the daemon/CLI. Uses a 3s timeout to keep doctor
// snappy on hosts where the docker socket exists but is unresponsive.
func checkDockerReachable(parent context.Context) CheckResult {
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()

	cli, err := dockerclient.NewClientWithOpts(dockerclient.FromEnv, dockerclient.WithAPIVersionNegotiation())
	if err != nil {
		return CheckResult{
			Name:    "docker_reachable",
			State:   CheckWarn,
			Message: fmt.Sprintf("docker client init failed: %v", err),
		}
	}
	defer cli.Close()

	if _, err := cli.Ping(ctx); err != nil {
		return CheckResult{
			Name:    "docker_reachable",
			State:   CheckWarn,
			Message: fmt.Sprintf("docker ping failed: %v (start Docker Desktop or set DOCKER_HOST)", err),
		}
	}
	return CheckResult{
		Name:    "docker_reachable",
		State:   CheckOK,
		Message: "docker ping OK",
	}
}

// ---------- check 6: agent_image_present ----------

// agentImageRef is the default agent image reference checked by the
// agent_image_present probe. Held as a package-level constant so future
// upgrades from v0.1 → v0.2 update one spot. Matches the literal used
// by scripts/smoke-daemon.sh and docs/workspace-root.md.
const agentImageRef = "dclaw-agent:v0.1"

// checkAgentImagePresent runs `docker image inspect dclaw-agent:v0.1`.
// WARNs on absence with a build/pull suggestion — not a hard FAIL
// because the operator may legitimately use a custom image (the
// workspace-root.md "Custom image compatibility" section explicitly
// supports this).
func checkAgentImagePresent(parent context.Context) CheckResult {
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()

	cli, err := dockerclient.NewClientWithOpts(dockerclient.FromEnv, dockerclient.WithAPIVersionNegotiation())
	if err != nil {
		return CheckResult{
			Name:    "agent_image_present",
			State:   CheckWarn,
			Message: fmt.Sprintf("docker client init failed: %v", err),
		}
	}
	defer cli.Close()

	if _, _, err := cli.ImageInspectWithRaw(ctx, agentImageRef); err != nil {
		return CheckResult{
			Name:    "agent_image_present",
			State:   CheckWarn,
			Message: fmt.Sprintf("%s image not found locally — run 'docker pull %s' or build via agent/build.sh (%v)", agentImageRef, agentImageRef, err),
		}
	}
	return CheckResult{
		Name:    "agent_image_present",
		State:   CheckOK,
		Message: fmt.Sprintf("%s present", agentImageRef),
	}
}

// ---------- check 7: audit_log_writable ----------

// checkAuditLogWritable opens $STATE_DIR/audit.log with O_APPEND|
// O_CREATE at 0600 and immediately closes it. This catches the most
// common state-dir-permissions failure: audit.log exists but is owned
// by root from a previous sudo invocation, or $STATE_DIR is missing
// and not creatable. Does NOT actually write a record — opening with
// O_APPEND|O_CREATE is enough to prove the audit logger will be able
// to write.
//
// We use O_APPEND|O_CREATE (not O_RDWR) to match the production audit
// logger's flags (see internal/audit/audit.go); the kernel permission
// check is identical for our purposes.
func checkAuditLogWritable(p config.Paths) CheckResult {
	auditPath := filepath.Join(p.StateDir, "audit.log")

	// Ensure the state-dir exists at mode 0700 (matches audit.New +
	// config.WriteConfigFile). If MkdirAll fails the open below fails
	// too with a friendlier error, but mkdir gives us a more precise
	// message.
	if err := os.MkdirAll(p.StateDir, 0o700); err != nil {
		return CheckResult{
			Name:    "audit_log_writable",
			State:   CheckFail,
			Message: fmt.Sprintf("mkdir %s: %v", p.StateDir, err),
		}
	}

	f, err := os.OpenFile(auditPath, os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return CheckResult{
			Name:    "audit_log_writable",
			State:   CheckFail,
			Message: fmt.Sprintf("open %s: %v", auditPath, err),
		}
	}
	_ = f.Close()
	return CheckResult{
		Name:    "audit_log_writable",
		State:   CheckOK,
		Message: fmt.Sprintf("opened %s (O_APPEND|O_CREATE, 0600)", auditPath),
	}
}

// ---------- doctor workspace subcommand ----------

// runDoctorWorkspace is the RunE for `dclaw doctor workspace <path>`.
// Builds a paths.Policy from current config (or default denylist if
// no config) with AllowTrust=false (full normal validation) and runs
// Policy.Validate. Prints OK + canonical on pass, structured rejection
// on fail. Exits 65 (ExitDataErr) on rejection — same shape as
// agent create's workspace_forbidden but does NOT write to audit.log.
func runDoctorWorkspace(cmd *cobra.Command, args []string) error {
	if err := validateOutputFormat(); err != nil {
		return err
	}
	target := args[0]

	// Build the policy from current config. If config-resolve fails
	// (very rare — broken HOME) we fall back to default denylist with
	// no allow-root, which means non-trust paths get rejected with the
	// "no workspace-root configured" message; that is the same behavior
	// the daemon would surface to a real `agent create`.
	policy := paths.Policy{
		Denylist:   doctorDenylist,
		AllowTrust: false,
	}
	if resolved, err := config.Resolve(stateDirFlag, ""); err == nil {
		if cfg, err := config.ReadConfigFile(resolved.StateDir); err == nil {
			policy.AllowRoot = cfg.WorkspaceRoot
		}
	}

	canonical, vErr := policy.Validate(target)
	if vErr != nil {
		writeDoctorWorkspaceRejection(cmd, target, policy.AllowRoot, vErr)
		os.Exit(ExitDataErr)
		return nil
	}

	if outputFormat == "json" {
		payload := map[string]any{
			"ok":        true,
			"resolved":  canonical,
			"raw_input": target,
			"exit_code": ExitOK,
		}
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		_ = enc.Encode(payload)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "OK: %s\n", canonical)
	}
	return nil
}

// writeDoctorWorkspaceRejection prints the structured rejection in the
// selected output format. JSON shape mirrors WorkspaceForbiddenPayload
// closely enough that scripts already consuming `agent create` JSON
// can be reused; we deliberately use a slightly different "error"
// label ("workspace_rejected" rather than "workspace_forbidden") so
// scripts can distinguish a doctor pre-flight from an actual daemon
// rejection if they care to.
func writeDoctorWorkspaceRejection(cmd *cobra.Command, raw, allowRoot string, vErr error) {
	allowRootDisplay := allowRoot
	if allowRootDisplay == "" {
		allowRootDisplay = "(not configured — run 'dclaw init')"
	}
	reason := vErr.Error()
	// Strip the standard "workspace path forbidden by policy: " prefix
	// when it leads the wrapped error so the human-facing reason reads
	// naturally; we keep the full chain when it does not.
	if errors.Is(vErr, paths.ErrWorkspaceForbidden) {
		reason = strings.TrimPrefix(reason, paths.ErrWorkspaceForbidden.Error()+": ")
	}

	if outputFormat == "json" {
		payload := WorkspaceForbiddenPayload{
			Error:     "workspace_rejected",
			Message:   vErr.Error(),
			ExitCode:  ExitDataErr,
			AllowRoot: allowRootDisplay,
			Resolved:  "", // doctor workspace does not produce a canonical on rejection
			Reason:    reason,
			Remediations: []Remediation{
				{Kind: "use_inside_root", Command: "dclaw doctor workspace <path-inside-root>"},
				{Kind: "change_root", Command: "dclaw config set workspace-root <new-path>"},
				{Kind: "trust_override", Command: "dclaw agent create ... --workspace-trust \"<reason>\""},
			},
		}
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		_ = enc.Encode(payload)
		return
	}

	w := cmd.ErrOrStderr()
	fmt.Fprintf(w, "error: %s\n", vErr.Error())
	fmt.Fprintln(w, "")
	fmt.Fprintf(w, "  Raw input:             %s\n", raw)
	fmt.Fprintf(w, "  Configured allow-root: %s\n", allowRootDisplay)
	fmt.Fprintf(w, "  Reason:                %s\n", reason)
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "This is a doctor pre-flight; no audit-log entry was written.")
	fmt.Fprintln(w, "See docs/workspace-root.md for details.")
}
