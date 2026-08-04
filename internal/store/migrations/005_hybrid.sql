-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS user_affinity (
    user_id     BIGINT PRIMARY KEY,
    score       INT NOT NULL DEFAULT -20,
    last_reason TEXT,
    updated_at  TIMESTAMPTZ NOT NULL
);

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
DROP TABLE IF EXISTS user_affinity;
-- +goose StatementEnd