-- +goose Up

CREATE TABLE IF NOT EXISTS objects (
    bucket        TEXT NOT NULL,
    key           TEXT NOT NULL,
    gmail_msg_id  TEXT NOT NULL,
    drive_file_id TEXT NOT NULL DEFAULT '',
    etag          TEXT NOT NULL,
    size          BIGINT NOT NULL,
    content_type  TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    metadata      JSONB NOT NULL DEFAULT '{}',
    PRIMARY KEY (bucket, key)
);

CREATE INDEX IF NOT EXISTS idx_objects_bucket_prefix
    ON objects (bucket, key text_pattern_ops);

CREATE TABLE IF NOT EXISTS buckets (
    name       TEXT PRIMARY KEY,
    label_id   TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- +goose Down
-- Intentionally empty: dropping tables would orphan objects in Gmail/Drive
SELECT 1;
