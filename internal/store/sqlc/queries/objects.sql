-- name: PutObject :exec
INSERT INTO objects (bucket, key, gmail_msg_id, drive_file_id, etag, size, content_type, created_at, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (bucket, key)
DO UPDATE SET
    gmail_msg_id  = EXCLUDED.gmail_msg_id,
    drive_file_id = EXCLUDED.drive_file_id,
    etag          = EXCLUDED.etag,
    size          = EXCLUDED.size,
    content_type  = EXCLUDED.content_type,
    created_at    = EXCLUDED.created_at,
    metadata      = EXCLUDED.metadata;

-- name: GetObject :one
SELECT gmail_msg_id, drive_file_id, etag, size, content_type, created_at, metadata
FROM objects
WHERE bucket = $1 AND key = $2;

-- name: DeleteObject :exec
DELETE FROM objects WHERE bucket = $1 AND key = $2;

-- name: ListObjectsByPrefix :many
SELECT key, gmail_msg_id, drive_file_id, etag, size, content_type, created_at, metadata
FROM objects
WHERE bucket = $1
  AND key LIKE $2
  AND key > $3
ORDER BY key
LIMIT $4;

-- name: ListObjectsAll :many
SELECT key, gmail_msg_id, drive_file_id, etag, size, content_type, created_at, metadata
FROM objects
WHERE bucket = $1
  AND key > $2
ORDER BY key
LIMIT $3;

-- name: DeleteObjectsByBucket :exec
DELETE FROM objects WHERE bucket = $1;
