-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS tasks (
    id           BIGSERIAL PRIMARY KEY,
    channel_id   BIGINT NOT NULL,
    requester_id BIGINT NOT NULL,
    prompt       TEXT NOT NULL,
    fire_at      TIMESTAMPTZ NOT NULL,
    status       TEXT NOT NULL DEFAULT 'pending'
                 CHECK (status IN ('pending', 'running', 'done', 'failed', 'cancelled', 'interrupted')),
    result       TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at   TIMESTAMPTZ,
    finished_at  TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_tasks_status_fire_at ON tasks (status, fire_at);

DO $$
BEGIN
    IF to_regclass('reminders') IS NOT NULL THEN
        INSERT INTO tasks (channel_id, requester_id, prompt, fire_at, status, created_at)
        SELECT channel_id::bigint, user_id::bigint, 'erinnere <@' || user_id || '> an: ' || message,
               fire_at, 'pending', COALESCE(created_at, now())
        FROM reminders
        WHERE NOT fired AND deleted_at IS NULL
          AND channel_id ~ '^[0-9]+$' AND user_id ~ '^[0-9]+$';
        DROP TABLE reminders;
    END IF;
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS tasks;
-- +goose StatementEnd
