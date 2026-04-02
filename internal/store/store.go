// -------------------------------------------------------------------------------
// Store - SQLite Metadata Index
//
// Author: Alex Freidah
//
// Embedded SQLite database that maps S3 objects to Gmail message IDs and
// caches object metadata locally. Implements the backend.MetadataStore
// interface. Eliminates API calls for HeadObject and ListObjects, and
// reduces GetObject/DeleteObject from two API calls to one.
// -------------------------------------------------------------------------------

package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/afreidah/g3/internal/backend"

	_ "github.com/mattn/go-sqlite3"
)

// -------------------------------------------------------------------------
// STORE
// -------------------------------------------------------------------------

// Store provides a local SQLite metadata index for g3 objects and buckets.
// Implements backend.MetadataStore.
type Store struct {
	db *sql.DB
}

// New opens or creates a SQLite database at the given path and runs schema
// migrations.
func New(path string) (*Store, error) {
	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return &Store{db: db}, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// migrate creates the schema if it doesn't exist.
func migrate(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS objects (
			bucket        TEXT NOT NULL,
			key           TEXT NOT NULL,
			gmail_msg_id  TEXT NOT NULL,
			drive_file_id TEXT DEFAULT '',
			etag          TEXT NOT NULL,
			size          INTEGER NOT NULL,
			content_type  TEXT NOT NULL,
			created_at    TIMESTAMP NOT NULL,
			metadata      TEXT DEFAULT '{}',
			PRIMARY KEY (bucket, key)
		);

		CREATE TABLE IF NOT EXISTS buckets (
			name       TEXT PRIMARY KEY,
			label_id   TEXT NOT NULL,
			created_at TIMESTAMP NOT NULL
		);

		CREATE INDEX IF NOT EXISTS idx_objects_bucket_prefix
			ON objects (bucket, key);
	`)
	return err
}

// -------------------------------------------------------------------------
// OBJECT OPERATIONS
// -------------------------------------------------------------------------

// PutObject inserts or replaces an object record in the index.
func (s *Store) PutObject(ctx context.Context, rec *backend.ObjectRecord) error {
	metaJSON, err := json.Marshal(rec.Metadata)
	if err != nil {
		metaJSON = []byte("{}")
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO objects (bucket, key, gmail_msg_id, drive_file_id, etag, size, content_type, created_at, metadata)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, rec.Bucket, rec.Key, rec.GmailMsgID, rec.DriveFileID, rec.ETag, rec.Size, rec.ContentType, rec.CreatedAt, string(metaJSON))
	return err
}

// GetObject retrieves an object record by bucket and key. Returns nil if
// not found.
func (s *Store) GetObject(ctx context.Context, bucket, key string) (*backend.ObjectRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT gmail_msg_id, drive_file_id, etag, size, content_type, created_at, metadata
		FROM objects WHERE bucket = ? AND key = ?
	`, bucket, key)

	rec := &backend.ObjectRecord{Bucket: bucket, Key: key}
	var metaJSON string
	err := row.Scan(&rec.GmailMsgID, &rec.DriveFileID, &rec.ETag, &rec.Size, &rec.ContentType, &rec.CreatedAt, &metaJSON)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if metaJSON != "" {
		_ = json.Unmarshal([]byte(metaJSON), &rec.Metadata)
	}
	return rec, nil
}

// DeleteObject removes an object record from the index.
func (s *Store) DeleteObject(ctx context.Context, bucket, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM objects WHERE bucket = ? AND key = ?`, bucket, key)
	return err
}

// ListObjects returns object records matching a bucket and optional key
// prefix, ordered by key.
func (s *Store) ListObjects(ctx context.Context, bucket, prefix, startAfter string, maxKeys int) ([]*backend.ObjectRecord, error) {
	query := `SELECT key, gmail_msg_id, drive_file_id, etag, size, content_type, created_at, metadata
		FROM objects WHERE bucket = ?`
	args := []any{bucket}

	if prefix != "" {
		query += ` AND key LIKE ?`
		args = append(args, prefix+"%")
	}
	if startAfter != "" {
		query += ` AND key > ?`
		args = append(args, startAfter)
	}

	query += ` ORDER BY key LIMIT ?`
	args = append(args, maxKeys)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var records []*backend.ObjectRecord
	for rows.Next() {
		rec := &backend.ObjectRecord{Bucket: bucket}
		var metaJSON string
		if err := rows.Scan(&rec.Key, &rec.GmailMsgID, &rec.DriveFileID, &rec.ETag, &rec.Size, &rec.ContentType, &rec.CreatedAt, &metaJSON); err != nil {
			slog.WarnContext(ctx, "Failed to scan object row", "error", err)
			continue
		}
		if metaJSON != "" {
			_ = json.Unmarshal([]byte(metaJSON), &rec.Metadata)
		}
		records = append(records, rec)
	}
	return records, rows.Err()
}

// -------------------------------------------------------------------------
// BUCKET OPERATIONS
// -------------------------------------------------------------------------

// PutBucket inserts or replaces a bucket record.
func (s *Store) PutBucket(ctx context.Context, rec *backend.BucketRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO buckets (name, label_id, created_at)
		VALUES (?, ?, ?)
	`, rec.Name, rec.LabelID, rec.CreatedAt)
	return err
}

// GetBucket retrieves a bucket record by name. Returns nil if not found.
func (s *Store) GetBucket(ctx context.Context, name string) (*backend.BucketRecord, error) {
	row := s.db.QueryRowContext(ctx, `SELECT label_id, created_at FROM buckets WHERE name = ?`, name)

	rec := &backend.BucketRecord{Name: name}
	err := row.Scan(&rec.LabelID, &rec.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return rec, nil
}

// ListBuckets returns all bucket records ordered by name.
func (s *Store) ListBuckets(ctx context.Context) ([]*backend.BucketRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name, label_id, created_at FROM buckets ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var records []*backend.BucketRecord
	for rows.Next() {
		rec := &backend.BucketRecord{}
		if err := rows.Scan(&rec.Name, &rec.LabelID, &rec.CreatedAt); err != nil {
			continue
		}
		records = append(records, rec)
	}
	return records, rows.Err()
}
