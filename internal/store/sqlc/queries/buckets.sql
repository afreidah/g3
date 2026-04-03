-- name: PutBucket :exec
INSERT INTO buckets (name, label_id, created_at)
VALUES ($1, $2, $3)
ON CONFLICT (name)
DO UPDATE SET
    label_id   = EXCLUDED.label_id,
    created_at = EXCLUDED.created_at;

-- name: GetBucket :one
SELECT label_id, created_at
FROM buckets
WHERE name = $1;

-- name: ListBuckets :many
SELECT name, label_id, created_at
FROM buckets
ORDER BY name;

-- name: DeleteBucket :exec
DELETE FROM buckets WHERE name = $1;
