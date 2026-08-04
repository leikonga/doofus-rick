-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS ambient_log (
    id         BIGSERIAL PRIMARY KEY,
    channel_id BIGINT NOT NULL,
    fired_at   TIMESTAMPTZ NOT NULL,
    score      INT NOT NULL,
    hook       TEXT
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS ambient_log;
-- +goose StatementEnd