-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS backfill_state (
    id             INT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    status         TEXT NOT NULL,
    started_at     TIMESTAMPTZ,
    finished_at    TIMESTAMPTZ,
    last_error     TEXT,
    channels_total INT,
    channels_done  INT,
    messages_seen  BIGINT NOT NULL DEFAULT 0,
    updated_at     TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS backfill_channel (
    channel_id      BIGINT PRIMARY KEY,
    newest_at_start BIGINT,
    oldest_fetched  BIGINT,
    done            BOOLEAN NOT NULL DEFAULT FALSE,
    messages_seen   BIGINT NOT NULL DEFAULT 0,
    last_error      TEXT,
    updated_at      TIMESTAMPTZ NOT NULL
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS backfill_channel;
DROP TABLE IF EXISTS backfill_state;
-- +goose StatementEnd