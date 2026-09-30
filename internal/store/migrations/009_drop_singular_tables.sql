-- +goose Up
-- +goose StatementBegin
DROP TABLE IF EXISTS backfill_state;
DROP TABLE IF EXISTS backfill_channel;
DROP TABLE IF EXISTS user_affinity;
DROP TABLE IF EXISTS ambient_log;
-- +goose StatementEnd
