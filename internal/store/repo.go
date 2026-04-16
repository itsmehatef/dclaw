package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/itsmehatef/dclaw/internal/protocol"
)

// AgentRecord is the on-disk shape of an agent row. Env and Labels are raw
// JSON text; the Lifecycle layer marshals/unmarshals.
type AgentRecord struct {
	ID           string
	Name         string
	Image        string
	Status       string
	ContainerID  string
	Workspace    string
	Labels       string
	Env          string
	CreatedAt    int64
	UpdatedAt    int64
}

// ChannelRecord is the on-disk shape of a channel row.
type ChannelRecord struct {
	ID        string
	Name      string
	Type      string
	Config    string
	CreatedAt int64
	UpdatedAt int64
}

// EventRecord is the on-disk shape of an event row.
type EventRecord struct {
	ID        int64
	AgentID   string
	Type      string
	Data      string
	Timestamp int64
}

// InsertAgent inserts a new row. Returns an error if the name is not unique.
func (r *Repo) InsertAgent(ctx context.Context, rec AgentRecord) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO agents (id, name, image, status, container_id, workspace_path, labels, env, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, rec.ID, rec.Name, rec.Image, rec.Status, rec.ContainerID, rec.Workspace, rec.Labels, rec.Env, rec.CreatedAt, rec.UpdatedAt)
	return err
}

// GetAgent returns the agent with the given name, or an error if none exists.
func (r *Repo) GetAgent(ctx context.Context, name string) (AgentRecord, error) {
	var rec AgentRecord
	err := r.db.QueryRowContext(ctx, `
		SELECT id, name, image, status, COALESCE(container_id, ''), COALESCE(workspace_path, ''), labels, env, created_at, updated_at
		FROM agents WHERE name = ?
	`, name).Scan(&rec.ID, &rec.Name, &rec.Image, &rec.Status, &rec.ContainerID, &rec.Workspace, &rec.Labels, &rec.Env, &rec.CreatedAt, &rec.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return AgentRecord{}, fmt.Errorf("agent %q not found", name)
	}
	return rec, err
}

// ListAgents returns all agents ordered by created_at desc.
func (r *Repo) ListAgents(ctx context.Context) ([]AgentRecord, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, name, image, status, COALESCE(container_id, ''), COALESCE(workspace_path, ''), labels, env, created_at, updated_at
		FROM agents ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AgentRecord
	for rows.Next() {
		var rec AgentRecord
		if err := rows.Scan(&rec.ID, &rec.Name, &rec.Image, &rec.Status, &rec.ContainerID, &rec.Workspace, &rec.Labels, &rec.Env, &rec.CreatedAt, &rec.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// UpdateAgent replaces an existing row by name. Returns an error if no row
// was matched.
func (r *Repo) UpdateAgent(ctx context.Context, rec AgentRecord) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE agents SET image=?, status=?, container_id=?, workspace_path=?, labels=?, env=?, updated_at=?
		WHERE name = ?
	`, rec.Image, rec.Status, rec.ContainerID, rec.Workspace, rec.Labels, rec.Env, rec.UpdatedAt, rec.Name)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("agent %q not found", rec.Name)
	}
	return nil
}

// DeleteAgent removes a row by name.
func (r *Repo) DeleteAgent(ctx context.Context, name string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM agents WHERE name = ?`, name)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("agent %q not found", name)
	}
	return nil
}

// InsertChannel stores a channel record.
func (r *Repo) InsertChannel(ctx context.Context, rec ChannelRecord) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO channels (id, name, type, config, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, rec.ID, rec.Name, rec.Type, rec.Config, rec.CreatedAt, rec.UpdatedAt)
	return err
}

// GetChannel returns the channel with the given name.
func (r *Repo) GetChannel(ctx context.Context, name string) (protocol.Channel, error) {
	var c protocol.Channel
	err := r.db.QueryRowContext(ctx, `SELECT name, type, config FROM channels WHERE name = ?`, name).
		Scan(&c.Name, &c.Type, &c.Config)
	if errors.Is(err, sql.ErrNoRows) {
		return c, fmt.Errorf("channel %q not found", name)
	}
	return c, err
}

// ListChannels returns all channels.
func (r *Repo) ListChannels(ctx context.Context) ([]protocol.Channel, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT name, type, config FROM channels ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []protocol.Channel
	for rows.Next() {
		var c protocol.Channel
		if err := rows.Scan(&c.Name, &c.Type, &c.Config); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// DeleteChannel removes a channel by name.
func (r *Repo) DeleteChannel(ctx context.Context, name string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM channels WHERE name = ?`, name)
	return err
}

// AttachChannel creates a binding row between agent and channel.
func (r *Repo) AttachChannel(ctx context.Context, agentName, channelName string) error {
	aID, cID, err := r.lookupBindingIDs(ctx, agentName, channelName)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO channel_bindings (agent_id, channel_id, created_at) VALUES (?, ?, ?)
	`, aID, cID, time.Now().Unix())
	return err
}

// DetachChannel deletes a binding row.
func (r *Repo) DetachChannel(ctx context.Context, agentName, channelName string) error {
	aID, cID, err := r.lookupBindingIDs(ctx, agentName, channelName)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `DELETE FROM channel_bindings WHERE agent_id = ? AND channel_id = ?`, aID, cID)
	return err
}

func (r *Repo) lookupBindingIDs(ctx context.Context, agentName, channelName string) (string, string, error) {
	var aID, cID string
	if err := r.db.QueryRowContext(ctx, `SELECT id FROM agents WHERE name = ?`, agentName).Scan(&aID); err != nil {
		return "", "", fmt.Errorf("agent %q not found", agentName)
	}
	if err := r.db.QueryRowContext(ctx, `SELECT id FROM channels WHERE name = ?`, channelName).Scan(&cID); err != nil {
		return "", "", fmt.Errorf("channel %q not found", channelName)
	}
	return aID, cID, nil
}

// InsertEvent appends an event row.
func (r *Repo) InsertEvent(ctx context.Context, rec EventRecord) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO events (agent_id, type, data, timestamp) VALUES (?, ?, ?, ?)
	`, rec.AgentID, rec.Type, rec.Data, rec.Timestamp)
	return err
}

// RecentEvents returns up to `limit` most recent events for the given agent.
func (r *Repo) RecentEvents(ctx context.Context, agentID string, limit int) ([]protocol.Event, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT type, data, timestamp FROM events
		WHERE agent_id = ? ORDER BY timestamp DESC LIMIT ?
	`, agentID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []protocol.Event
	for rows.Next() {
		var e protocol.Event
		if err := rows.Scan(&e.Type, &e.Data, &e.Timestamp); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// NewID returns a new ULID string suitable for id columns.
func NewID() string { return ulid.Make().String() }
