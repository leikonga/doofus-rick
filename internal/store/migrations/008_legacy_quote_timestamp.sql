-- +goose Up
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = current_schema() AND table_name = 'quotes' AND column_name = 'timestamp'
    ) THEN
        UPDATE quotes SET created_at = "timestamp"
        WHERE (created_at IS NULL OR created_at = '0001-01-01 00:00:00') AND "timestamp" IS NOT NULL;
    END IF;
END $$;
-- +goose StatementEnd
