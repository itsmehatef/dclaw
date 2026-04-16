package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

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
}

// NewLifecycle constructs a Lifecycle around an existing store + docker
// client.
func NewLifecycle(log *slog.Logger, repo *store.Repo, docker *sandbox.DockerClient) *Lifecycle {
	return &Lifecycle{log: log, repo: repo, docker: docker}
}

// ---------- agent ----------

// AgentCreate inserts a new agent record and (if Docker reachable) creates
// the container in "created" state (not started). Returns the populated
// record.
func (l *Lifecycle) AgentCreate(ctx context.Context, req protocol.AgentCreateParams) (protocol.Agent, error) {
	if strings.TrimSpace(req.Name) == "" {
		return protocol.Agent{}, fmt.Errorf("agent name required")
	}
	if strings.TrimSpace(req.Image) == "" {
		return protocol.Agent{}, fmt.Errorf("agent image required")
	}

	if _, err := l.repo.GetAgent(ctx, req.Name); err == nil {
		return protocol.Agent{}, fmt.Errorf("agent %q already exists", req.Name)
	}

	now := time.Now().Unix()
	id := ulid.Make().String()

	envMap := parseKVList(req.Env)
	labelMap := parseKVList(req.Labels)

	containerID, err := l.docker.CreateAgent(ctx, sandbox.CreateSpec{
		Name:      fmt.Sprintf("dclaw-%s", req.Name),
		Image:     req.Image,
		Env:       envMap,
		Labels:    labelMap,
		Workspace: req.Workspace,
	})
	if err != nil {
		return protocol.Agent{}, fmt.Errorf("docker create: %w", err)
	}

	rec := store.AgentRecord{
		ID:          id,
		Name:        req.Name,
		Image:       req.Image,
		Status:      "created",
		ContainerID: containerID,
		Workspace:   req.Workspace,
		Env:         jsonMustMarshal(envMap),
		Labels:      jsonMustMarshal(labelMap),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := l.repo.InsertAgent(ctx, rec); err != nil {
		// Best-effort cleanup of the orphaned container.
		_ = l.docker.DeleteAgent(ctx, containerID)
		return protocol.Agent{}, fmt.Errorf("store insert: %w", err)
	}

	l.log.Info("agent created", "name", req.Name, "image", req.Image, "container_id", containerID)
	return agentToWire(rec), nil
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
		return protocol.Agent{}, fmt.Errorf("agent %q not found", name)
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
		return protocol.Agent{}, fmt.Errorf("agent %q not found", req.Name)
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
		return fmt.Errorf("agent %q not found", name)
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
		return fmt.Errorf("agent %q not found", name)
	}
	if rec.ContainerID == "" {
		return fmt.Errorf("agent %q has no container", name)
	}
	if err := l.docker.StartAgent(ctx, rec.ContainerID); err != nil {
		return fmt.Errorf("docker start: %w", err)
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
		return fmt.Errorf("agent %q not found", name)
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
		return nil, fmt.Errorf("agent %q not found", name)
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
		return protocol.AgentExecResult{}, fmt.Errorf("agent %q not found", req.Name)
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
		ID:          rec.ID,
		Name:        rec.Name,
		Image:       rec.Image,
		Status:      rec.Status,
		ContainerID: rec.ContainerID,
		Workspace:   rec.Workspace,
		Env:         env,
		Labels:      labels,
		CreatedAt:   rec.CreatedAt,
		UpdatedAt:   rec.UpdatedAt,
	}
}
