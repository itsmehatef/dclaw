-- +goose Up
-- +goose StatementBegin

CREATE TABLE chat_messages (
  id          TEXT PRIMARY KEY,
  agent_id    TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
  role        TEXT NOT NULL CHECK(role IN ('user', 'agent', 'system', 'error')),
  content     TEXT NOT NULL,
  parent_id   TEXT NOT NULL DEFAULT '',
  message_id  TEXT NOT NULL UNIQUE,
  sequence    INTEGER NOT NULL DEFAULT 0,
  timestamp   INTEGER NOT NULL
);

CREATE INDEX chat_messages_agent_ts_idx ON chat_messages(agent_id, timestamp DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS chat_messages_agent_ts_idx;
DROP TABLE IF EXISTS chat_messages;

-- +goose StatementEnd
