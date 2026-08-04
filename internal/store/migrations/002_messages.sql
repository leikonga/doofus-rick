-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS messages (
    id          BIGINT PRIMARY KEY,
    channel_id  BIGINT NOT NULL,
    author_id   BIGINT NOT NULL,
    author_name TEXT   NOT NULL,
    content     TEXT   NOT NULL,
    reply_to_id BIGINT,
    is_bot      BOOLEAN NOT NULL,
    attachments JSONB,
    created_at  TIMESTAMPTZ NOT NULL,
    edited_at   TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_messages_channel_created ON messages (channel_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_messages_author_created ON messages (author_id, created_at DESC);

CREATE TABLE IF NOT EXISTS forgotten_authors (
    user_id    BIGINT PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS forgotten_authors;
DROP TABLE IF EXISTS messages;
-- +goose StatementEnd