-- +goose Up
-- +goose StatementBegin

CREATE TABLE agents (
  id             TEXT PRIMARY KEY,           -- ULID
  name           TEXT NOT NULL UNIQUE,
  image          TEXT NOT NULL,
  status         TEXT NOT NULL,              -- created|running|stopped|exited|errored
  container_id   TEXT,
  workspace_path TEXT,
  labels         TEXT NOT NULL DEFAULT '{}', -- JSON object
  env            TEXT NOT NULL DEFAULT '{}', -- JSON object
  created_at     INTEGER NOT NULL,           -- unix seconds
  updated_at     INTEGER NOT NULL
);

CREATE INDEX agents_status_idx ON agents(status);
CREATE INDEX agents_updated_at_idx ON agents(updated_at);

CREATE TABLE channels (
  id             TEXT PRIMARY KEY,
  name           TEXT NOT NULL UNIQUE,
  type           TEXT NOT NULL,              -- discord|slack|cli|...
  config         TEXT NOT NULL DEFAULT '',
  created_at     INTEGER NOT NULL,
  updated_at     INTEGER NOT NULL
);

CREATE INDEX channels_type_idx ON channels(type);

CREATE TABLE channel_bindings (
  agent_id       TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
  channel_id     TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
  created_at     INTEGER NOT NULL,
  PRIMARY KEY (agent_id, channel_id)
);

CREATE TABLE events (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  agent_id       TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
  type           TEXT NOT NULL,              -- started|stopped|errored|log|...
  data           TEXT NOT NULL DEFAULT '',   -- JSON payload, free-form
  timestamp      INTEGER NOT NULL
);

CREATE INDEX events_agent_timestamp_idx ON events(agent_id, timestamp DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS events_agent_timestamp_idx;
DROP TABLE IF EXISTS events;
DROP TABLE IF EXISTS channel_bindings;
DROP INDEX IF EXISTS channels_type_idx;
DROP TABLE IF EXISTS channels;
DROP INDEX IF EXISTS agents_updated_at_idx;
DROP INDEX IF EXISTS agents_status_idx;
DROP TABLE IF EXISTS agents;
-- +goose StatementEnd
