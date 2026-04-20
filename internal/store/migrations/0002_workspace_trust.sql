-- +goose Up
-- +goose StatementBegin
ALTER TABLE agents ADD COLUMN workspace_trust_reason TEXT;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE agents DROP COLUMN workspace_trust_reason;
-- +goose StatementEnd
