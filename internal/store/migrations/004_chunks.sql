-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS chunks (
    id               BIGSERIAL PRIMARY KEY,
    channel_id       BIGINT NOT NULL,
    content          TEXT NOT NULL,
    started_at       TIMESTAMPTZ NOT NULL,
    ended_at         TIMESTAMPTZ NOT NULL,
    message_count    INT NOT NULL,
    first_message_id BIGINT NOT NULL,
    last_message_id  BIGINT NOT NULL,
    tsv              TSVECTOR GENERATED ALWAYS AS (to_tsvector('simple', content)) STORED
);

CREATE INDEX IF NOT EXISTS idx_chunks_tsv ON chunks USING GIN (tsv);
CREATE INDEX IF NOT EXISTS idx_chunks_channel_ended ON chunks (channel_id, ended_at DESC);

CREATE TABLE IF NOT EXISTS chunk_embeddings (
    chunk_id  BIGINT NOT NULL REFERENCES chunks(id) ON DELETE CASCADE,
    model     TEXT NOT NULL,
    embedding HALFVEC(1024) NOT NULL,
    PRIMARY KEY (chunk_id, model)
);

CREATE INDEX IF NOT EXISTS idx_chunk_embeddings_hnsw ON chunk_embeddings USING HNSW (embedding HALFVEC_COSINE_OPS);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_chunk_embeddings_hnsw;
DROP TABLE IF EXISTS chunk_embeddings;
DROP TABLE IF EXISTS chunks;
-- +goose StatementEnd