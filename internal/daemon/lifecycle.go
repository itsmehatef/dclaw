package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/itsmehatef/dclaw/internal/audit"
	"github.com/itsmehatef/dclaw/internal/paths"
	"github.com/itsmehatef/dclaw/internal/protocol"
	"github.com/itsmehatef/dclaw/internal/sandbox"
	"github.com/itsmehatef/dclaw/internal/store"
)

// Lifecycle owns the real work of CRUD + start/stop for agents and channels.
// It sits between the Router (pure RPC dispatch) and the store + docker
// layers. All public methods return domain errors; the router maps those to
// RPC error envelopes.
type Lifecycle struct {
	log    *slog.Logger
	repo   *store.Repo
	docker *sandbox.DockerClient
	// policy and audit are populated by NewLifecycle. A zero-value policy
	// (AllowRoot == "") causes AgentCreate to reject every non-trust
	// workspace with ErrWorkspaceForbidden — the fail-closed default.
	// A nil audit logger disables audit writes, which is intended for
	// tests only; the daemon always wires a non-nil logger.
	policy paths.Policy
	audit  *audit.Logger
}

// NewLifecycle constructs a Lifecycle around an existing store + docker
// client, a workspace policy, and an audit logger. The policy governs
// every AgentCreate; the audit logger records one NDJSON line per decision.
// Passing a nil *audit.Logger turns off audit writes (useful in tests).
func NewLifecycle(log *slog.Logger, repo *store.Repo, docker *sandbox.DockerClient, policy paths.Policy, auditLog *audit.Logger) *Lifecycle {
	return &Lifecycle{log: log, repo: repo, docker: docker, policy: policy, audit: auditLog}
}

// ---------- agent ----------

// AgentCreate inserts a new agent record and (if Docker reachable) creates
// the container in "created" state (not started). Returns the populated
// record.
//
// beta.1-paths-hardening: every call runs req.Workspace through
// paths.Policy.Validate before anything else. On forbidden, we write one
// audit line (outcome=forbidden) and return a wrapped ErrWorkspaceForbidden
// that the router maps to protocol.ErrWorkspaceForbidden = -32007. When
// the operator supplies a non-empty WorkspaceTrustReason we flip
// AllowTrust on a per-call copy of the policy — bypassing the AllowRoot
// Rel check but not the denylist — and record outcome=trust with the
// reason string. On pass we record outcome=pass and hand the CANONICAL
// path (not req.Workspace) to both Docker and the DB, so a TOCTOU swap
// between validate and bind-mount cannot mount a different inode.
func (l *Lifecycle) AgentCreate(ctx context.Context, req protocol.AgentCreateParams) (protocol.Agent, error) {
	if strings.TrimSpace(req.Name) == "" {
		return protocol.Agent{}, fmt.Errorf("agent name required")
	}
	if strings.TrimSpace(req.Image) == "" {
		return protocol.Agent{}, fmt.Errorf("agent image required")
	}

	if existing, err := l.repo.GetAgent(ctx, req.Name); err == nil {
		_ = existing
		return protocol.Agent{}, fmt.Errorf("agent %q: %w", req.Name, store.ErrNameTaken)
	}

	// ---- workspace policy ----
	canonical := ""
	trusted := strings.TrimSpace(req.WorkspaceTrustReason) != ""
	if req.Workspace != "" {
		policy := l.policy
		if trusted {
			policy.AllowTrust = true
		}
		var err error
		canonical, err = policy.Validate(req.Workspace)
		if err != nil {
			// Audit outcome=forbidden. Canonical is best-effort: Validate
			// may have returned empty if the failure happened before
			// canonicalization.
			_ = l.writeAudit(req.Name, req.Workspace, canonical, "forbidden", err.Error())
			return protocol.Agent{}, err
		}
		// Re-open under NOFOLLOW and re-validate. Hold the fd open until
		// docker.CreateAgent returns so an attacker can't win the race
		// between Validate and bind-mount.
		fd, reCanon, err := paths.OpenSafe(canonical, policy)
		if err != nil {
			_ = l.writeAudit(req.Name, req.Workspace, canonical, "forbidden", err.Error())
			return protocol.Agent{}, err
		}
		defer fd.Close()
		canonical = reCanon

		outcome := "pass"
		if trusted {
			outcome = "trust"
		}
		auditReason := ""
		if trusted {
			auditReason = req.WorkspaceTrustReason
		}
		_ = l.writeAudit(req.Name, req.Workspace, canonical, outcome, auditReason)
	}

	now := time.Now().Unix()
	id := ulid.Make().String()

	envMap := parseKVList(req.Env)
	labelMap := parseKVList(req.Labels)

	// Pass canonical — NOT req.Workspace — to the sandbox. If the workspace
	// was empty (no bind-mount), canonical is "" and the sandbox skips the
	// mount entirely, matching pre-beta.1 behavior.
	containerID, err := l.docker.CreateAgent(ctx, sandbox.CreateSpec{
		Name:      fmt.Sprintf("dclaw-%s", req.Name),
		Image:     req.Image,
		Env:       envMap,
		Labels:    labelMap,
		Workspace: canonical,
	})
	if err != nil {
		return protocol.Agent{}, fmt.Errorf("docker create: %w", err)
	}

	rec := store.AgentRecord{
		ID:                   id,
		Name:                 req.Name,
		Image:                req.Image,
		Status:               "created",
		ContainerID:          containerID,
		Workspace:            canonical,
		WorkspaceTrustReason: req.WorkspaceTrustReason,
		Env:                  jsonMustMarshal(envMap),
		Labels:               jsonMustMarshal(labelMap),
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	if err := l.repo.InsertAgent(ctx, rec); err != nil {
		// Best-effort cleanup of the orphaned container.
		_ = l.docker.DeleteAgent(ctx, containerID)
		return protocol.Agent{}, fmt.Errorf("store insert: %w", err)
	}

	l.log.Info("agent created", "name", req.Name, "image", req.Image, "container_id", containerID)
	return agentToWire(rec), nil
}

// writeAudit is a tiny helper so every AgentCreate branch logs through the
// same call. Returns the audit error (callers ignore — audit failure is
// non-fatal by policy).
func (l *Lifecycle) writeAudit(agentName, raw, canonical, outcome, reason string) error {
	if l.audit == nil {
		return nil
	}
	if err := l.audit.LogDecision(agentName, raw, canonical, outcome, reason, paths.PolicyVersion); err != nil {
		l.log.Warn("audit write failed", "err", err, "agent", agentName, "outcome", outcome)
		return err
	}
	return nil
}

// AgentList returns all agents, enriching each record with the live Docker
// status if the container is known.
func (l *Lifecycle) AgentList(ctx context.Context) ([]protocol.Agent, error) {
	recs, err := l.repo.ListAgents(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]protocol.Agent, 0, len(recs))
	for _, r := range recs {
		live := r
		if r.ContainerID != "" {
			if st, err := l.docker.InspectStatus(ctx, r.ContainerID); err == nil {
				live.Status = st
			}
		}
		out = append(out, agentToWire(live))
	}
	return out, nil
}

// AgentGet fetches a single agent by name with live status.
func (l *Lifecycle) AgentGet(ctx context.Context, name string) (protocol.Agent, error) {
	rec, err := l.repo.GetAgent(ctx, name)
	if err != nil {
		// Preserve the ErrNotFound wrap from store so the router's mapError
		// errors.Is switch routes this correctly.
		return protocol.Agent{}, err
	}
	if rec.ContainerID != "" {
		if st, err := l.docker.InspectStatus(ctx, rec.ContainerID); err == nil {
			rec.Status = st
		}
	}
	return agentToWire(rec), nil
}

// AgentDescribe returns a verbose per-agent projection including recent events.
func (l *Lifecycle) AgentDescribe(ctx context.Context, name string) (protocol.AgentDescribeResult, error) {
	a, err := l.AgentGet(ctx, name)
	if err != nil {
		return protocol.AgentDescribeResult{}, err
	}
	events, err := l.repo.RecentEvents(ctx, a.ID, 20)
	if err != nil {
		return protocol.AgentDescribeResult{}, err
	}
	return protocol.AgentDescribeResult{
		Agent:  a,
		Events: events,
	}, nil
}

// AgentUpdate mutates image/env/labels. Image change requires the container to
// be recreated; v0.3 requires the agent to be in "stopped" or "created" state
// first.
func (l *Lifecycle) AgentUpdate(ctx context.Context, req protocol.AgentUpdateParams) (protocol.Agent, error) {
	rec, err := l.repo.GetAgent(ctx, req.Name)
	if err != nil {
		return protocol.Agent{}, err
	}
	if req.Image != "" && (rec.Status != "created" && rec.Status != "stopped" && rec.Status != "exited") {
		return protocol.Agent{}, fmt.Errorf("cannot update image while agent is %s; stop it first", rec.Status)
	}
	if req.Image != "" {
		rec.Image = req.Image
	}
	if req.Env != nil {
		rec.Env = jsonMustMarshal(parseKVList(req.Env))
	}
	if req.Labels != nil {
		rec.Labels = jsonMustMarshal(parseKVList(req.Labels))
	}
	rec.UpdatedAt = time.Now().Unix()
	if err := l.repo.UpdateAgent(ctx, rec); err != nil {
		return protocol.Agent{}, err
	}
	return agentToWire(rec), nil
}

// AgentDelete stops (if running), removes the container, deletes the DB
// record.
func (l *Lifecycle) AgentDelete(ctx context.Context, name string) error {
	rec, err := l.repo.GetAgent(ctx, name)
	if err != nil {
		return err
	}
	if rec.ContainerID != "" {
		_ = l.docker.StopAgent(ctx, rec.ContainerID, 10*time.Second)
		_ = l.docker.DeleteAgent(ctx, rec.ContainerID)
	}
	if err := l.repo.DeleteAgent(ctx, name); err != nil {
		return err
	}
	l.log.Info("agent deleted", "name", name)
	return nil
}

// AgentStart starts the container (if not already running) and flips the DB
// status to "running".
func (l *Lifecycle) AgentStart(ctx context.Context, name string) error {
	rec, err := l.repo.GetAgent(ctx, name)
	if err != nil {
		return err
	}
	if rec.ContainerID == "" {
		return fmt.Errorf("agent %q has no container", name)
	}
	if err := l.docker.StartAgent(ctx, rec.ContainerID); err != nil {
		return fmt.Errorf("docker start: %w", err)
	}

	// Liveness poll: a bad image entrypoint (one-shot script) will exit
	// immediately; surface that loudly instead of flipping the DB to running
	// and leaving chat to fail silently on a stopped container.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		status, _ := l.docker.InspectStatus(ctx, rec.ContainerID)
		if status == "exited" || status == "dead" || status == "oomkilled" {
			return fmt.Errorf("agent %q started but container exited immediately — verify the image entrypoint is long-running (see agent/README.md)", name)
		}
		if status == "running" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	rec.Status = "running"
	rec.UpdatedAt = time.Now().Unix()
	if err := l.repo.UpdateAgent(ctx, rec); err != nil {
		return err
	}
	_ = l.repo.InsertEvent(ctx, store.EventRecord{AgentID: rec.ID, Type: "started", Data: "", Timestamp: time.Now().Unix()})
	return nil
}

// AgentStop sends SIGTERM, waits 10s, then SIGKILL if still alive.
func (l *Lifecycle) AgentStop(ctx context.Context, name string) error {
	rec, err := l.repo.GetAgent(ctx, name)
	if err != nil {
		return err
	}
	if rec.ContainerID == "" {
		return fmt.Errorf("agent %q has no container", name)
	}
	if err := l.docker.StopAgent(ctx, rec.ContainerID, 10*time.Second); err != nil {
		return fmt.Errorf("docker stop: %w", err)
	}
	rec.Status = "stopped"
	rec.UpdatedAt = time.Now().Unix()
	if err := l.repo.UpdateAgent(ctx, rec); err != nil {
		return err
	}
	_ = l.repo.InsertEvent(ctx, store.EventRecord{AgentID: rec.ID, Type: "stopped", Data: "", Timestamp: time.Now().Unix()})
	return nil
}

// AgentRestart = stop + start.
func (l *Lifecycle) AgentRestart(ctx context.Context, name string) error {
	if err := l.AgentStop(ctx, name); err != nil {
		// If the agent was already stopped, fall through.
		if !strings.Contains(err.Error(), "not running") {
			return err
		}
	}
	return l.AgentStart(ctx, name)
}

// AgentLogsBulk returns the last N log lines (stdout + stderr interleaved).
func (l *Lifecycle) AgentLogsBulk(ctx context.Context, name string, tail int) ([]string, error) {
	rec, err := l.repo.GetAgent(ctx, name)
	if err != nil {
		return nil, err
	}
	if rec.ContainerID == "" {
		return nil, fmt.Errorf("agent %q has no container", name)
	}
	if tail <= 0 {
		tail = 100
	}
	return l.docker.LogsTail(ctx, rec.ContainerID, tail)
}

// AgentExec runs a command inside the agent container synchronously.
func (l *Lifecycle) AgentExec(ctx context.Context, req protocol.AgentExecParams) (protocol.AgentExecResult, error) {
	rec, err := l.repo.GetAgent(ctx, req.Name)
	if err != nil {
		return protocol.AgentExecResult{}, err
	}
	if rec.ContainerID == "" {
		return protocol.AgentExecResult{}, fmt.Errorf("agent %q has no container", req.Name)
	}
	stdout, stderr, code, err := l.docker.ExecIn(ctx, rec.ContainerID, req.Argv)
	if err != nil {
		return protocol.AgentExecResult{}, err
	}
	return protocol.AgentExecResult{
		ExitCode: code,
		Stdout:   stdout,
		Stderr:   stderr,
	}, nil
}

// ---------- channel ----------

func (l *Lifecycle) ChannelCreate(ctx context.Context, req protocol.ChannelCreateParams) (protocol.Channel, error) {
	if req.Name == "" || req.Type == "" {
		return protocol.Channel{}, fmt.Errorf("channel name and type required")
	}
	id := ulid.Make().String()
	now := time.Now().Unix()
	rec := store.ChannelRecord{
		ID:        id,
		Name:      req.Name,
		Type:      req.Type,
		Config:    req.Config,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := l.repo.InsertChannel(ctx, rec); err != nil {
		return protocol.Channel{}, err
	}
	return protocol.Channel{Name: req.Name, Type: req.Type, Config: req.Config}, nil
}

// ---------- helpers ----------

func parseKVList(items []string) map[string]string {
	out := make(map[string]string, len(items))
	for _, kv := range items {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			out[kv] = ""
			continue
		}
		out[kv[:eq]] = kv[eq+1:]
	}
	return out
}

func jsonMustMarshal(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func agentToWire(rec store.AgentRecord) protocol.Agent {
	var env, labels map[string]string
	if rec.Env != "" {
		_ = json.Unmarshal([]byte(rec.Env), &env)
	}
	if rec.Labels != "" {
		_ = json.Unmarshal([]byte(rec.Labels), &labels)
	}
	return protocol.Agent{
		ID:                   rec.ID,
		Name:                 rec.Name,
		Image:                rec.Image,
		Status:               rec.Status,
		ContainerID:          rec.ContainerID,
		Workspace:            rec.Workspace,
		WorkspaceTrustReason: rec.WorkspaceTrustReason,
		Env:                  env,
		Labels:               labels,
		CreatedAt:            rec.CreatedAt,
		UpdatedAt:            rec.UpdatedAt,
	}
}
