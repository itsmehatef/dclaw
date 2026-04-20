// Package client defines the interface the CLI uses to talk to the dclaw
// daemon. In v0.2.0-cli the only implementation is NoopClient; in v0.3+ a
// real Unix-socket JSON-RPC client will implement it.
package client

import (
	"context"
	"errors"
)

// ErrDaemonNotImplemented is returned by NoopClient for every method.
var ErrDaemonNotImplemented = errors.New("dclaw daemon not yet implemented — see v0.3.0-daemon")

// Agent is a projection of the daemon's agent record suitable for display.
// Fields are deliberately minimal for v0.2.0-cli; they will grow in v0.3+.
type Agent struct {
	Name                 string            `json:"name"`
	Image                string            `json:"image"`
	Channel              string            `json:"channel,omitempty"`
	Workspace            string            `json:"workspace,omitempty"`
	WorkspaceTrustReason string            `json:"workspace_trust_reason,omitempty"`
	Env                  map[string]string `json:"env,omitempty"`
	Labels               map[string]string `json:"labels,omitempty"`
	Status               string            `json:"status,omitempty"` // running, stopped, ...
}

// Channel is a projection of the daemon's channel record.
type Channel struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Config string `json:"config,omitempty"`
}

// Client is the interface the CLI uses to talk to the daemon. Methods
// intentionally look like the CLI's own subcommands — this keeps the
// mapping from command -> daemon call obvious.
//
// In v0.2.0-cli every method on every implementation returns
// ErrDaemonNotImplemented. The interface exists so phase 3 can drop in a
// real implementation without any CLI changes beyond constructing a
// different concrete type.
type Client interface {
	// Version / health
	DaemonVersion(ctx context.Context) (string, error)

	// Agents
	AgentCreate(ctx context.Context, a Agent) error
	AgentList(ctx context.Context) ([]Agent, error)
	AgentGet(ctx context.Context, name string) (Agent, error)
	AgentUpdate(ctx context.Context, a Agent) error
	AgentDelete(ctx context.Context, name string) error
	AgentStart(ctx context.Context, name string) error
	AgentStop(ctx context.Context, name string) error
	AgentRestart(ctx context.Context, name string) error
	AgentLogs(ctx context.Context, name string, tail int, follow bool) (<-chan string, error)
	AgentExec(ctx context.Context, name string, argv []string) (int, error)

	// Channels
	ChannelCreate(ctx context.Context, c Channel) error
	ChannelList(ctx context.Context) ([]Channel, error)
	ChannelGet(ctx context.Context, name string) (Channel, error)
	ChannelDelete(ctx context.Context, name string) error
	ChannelAttach(ctx context.Context, agentName, channelName string) error
	ChannelDetach(ctx context.Context, agentName, channelName string) error

	// Daemon lifecycle
	DaemonStart(ctx context.Context) error
	DaemonStop(ctx context.Context) error
	DaemonStatus(ctx context.Context) (string, error)
}

// NoopClient is the v0.2.0-cli implementation: every method returns
// ErrDaemonNotImplemented. The CLI does not actually call it yet; the
// RequireDaemon() helper short-circuits first. It exists so downstream
// code can begin wiring against the Client interface today.
type NoopClient struct{}

func (NoopClient) DaemonVersion(context.Context) (string, error) {
	return "", ErrDaemonNotImplemented
}
func (NoopClient) AgentCreate(context.Context, Agent) error { return ErrDaemonNotImplemented }
func (NoopClient) AgentList(context.Context) ([]Agent, error) {
	return nil, ErrDaemonNotImplemented
}
func (NoopClient) AgentGet(context.Context, string) (Agent, error) {
	return Agent{}, ErrDaemonNotImplemented
}
func (NoopClient) AgentUpdate(context.Context, Agent) error  { return ErrDaemonNotImplemented }
func (NoopClient) AgentDelete(context.Context, string) error { return ErrDaemonNotImplemented }
func (NoopClient) AgentStart(context.Context, string) error  { return ErrDaemonNotImplemented }
func (NoopClient) AgentStop(context.Context, string) error   { return ErrDaemonNotImplemented }
func (NoopClient) AgentRestart(context.Context, string) error {
	return ErrDaemonNotImplemented
}
func (NoopClient) AgentLogs(context.Context, string, int, bool) (<-chan string, error) {
	return nil, ErrDaemonNotImplemented
}
func (NoopClient) AgentExec(context.Context, string, []string) (int, error) {
	return 0, ErrDaemonNotImplemented
}
func (NoopClient) ChannelCreate(context.Context, Channel) error { return ErrDaemonNotImplemented }
func (NoopClient) ChannelList(context.Context) ([]Channel, error) {
	return nil, ErrDaemonNotImplemented
}
func (NoopClient) ChannelGet(context.Context, string) (Channel, error) {
	return Channel{}, ErrDaemonNotImplemented
}
func (NoopClient) ChannelDelete(context.Context, string) error { return ErrDaemonNotImplemented }
func (NoopClient) ChannelAttach(context.Context, string, string) error {
	return ErrDaemonNotImplemented
}
func (NoopClient) ChannelDetach(context.Context, string, string) error {
	return ErrDaemonNotImplemented
}
func (NoopClient) DaemonStart(context.Context) error  { return ErrDaemonNotImplemented }
func (NoopClient) DaemonStop(context.Context) error   { return ErrDaemonNotImplemented }
func (NoopClient) DaemonStatus(context.Context) (string, error) {
	return "", ErrDaemonNotImplemented
}

// Ensure NoopClient implements Client at compile time.
var _ Client = NoopClient{}
