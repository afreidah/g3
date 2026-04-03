// -------------------------------------------------------------------------------
// PostgreSQL Store - Shared Metadata Index
//
// Author: Alex Freidah
//
// PostgreSQL implementation of the backend.MetadataStore interface using
// pgx/v5 and sqlc-generated queries. Allows the g3 service to run on any
// node without persistent local storage. Schema migrations are applied
// automatically on startup via goose.
// -------------------------------------------------------------------------------

package store

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"time"

	"github.com/afreidah/g3/internal/backend"
	"github.com/afreidah/g3/internal/config"
	"github.com/afreidah/g3/internal/store/sqlc"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pressly/goose/v3"

	_ "github.com/jackc/pgx/v5/stdlib"
)

//go:embed migrations/*.sql
var migrations embed.FS

// -------------------------------------------------------------------------
// POSTGRES STORE
// -------------------------------------------------------------------------

// PostgresStore provides a shared PostgreSQL metadata index for g3 objects
// and buckets. Implements backend.MetadataStore.
type PostgresStore struct {
	pool    *pgxpool.Pool
	queries *sqlc.Queries
}

// NewPostgres connects to PostgreSQL, runs migrations, and returns a store.
func NewPostgres(ctx context.Context, cfg *config.DatabaseConfig) (*PostgresStore, error) {
	dsn := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s",
		cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.Database, cfg.SSLMode,
	)

	poolCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}
	poolCfg.MaxConns = int32(cfg.MaxConns)

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}

	// Run goose migrations
	if err := runMigrations(ctx, dsn); err != nil {
		pool.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return &PostgresStore{
		pool:    pool,
		queries: sqlc.New(pool),
	}, nil
}

// runMigrations applies embedded SQL migrations via goose.
func runMigrations(ctx context.Context, dsn string) error {
	goose.SetBaseFS(migrations)
	db, err := goose.OpenDBWithDriver("pgx", dsn)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	return goose.UpContext(ctx, db, "migrations")
}

// Close closes the connection pool.
func (s *PostgresStore) Close() error {
	s.pool.Close()
	return nil
}

// -------------------------------------------------------------------------
// OBJECT OPERATIONS
// -------------------------------------------------------------------------

// PutObject inserts or replaces an object record.
func (s *PostgresStore) PutObject(ctx context.Context, rec *backend.ObjectRecord) error {
	start := time.Now()
	defer func() { recordQuery("PutObject", start) }()

	metaJSON, err := json.Marshal(rec.Metadata)
	if err != nil {
		metaJSON = []byte("{}")
	}

	return s.queries.PutObject(ctx, sqlc.PutObjectParams{
		Bucket:      rec.Bucket,
		Key:         rec.Key,
		GmailMsgID:  rec.GmailMsgID,
		DriveFileID: rec.DriveFileID,
		Etag:        rec.ETag,
		Size:        rec.Size,
		ContentType: rec.ContentType,
		CreatedAt:   pgtype.Timestamptz{Time: rec.CreatedAt, Valid: true},
		Metadata:    metaJSON,
	})
}

// GetObject retrieves an object record by bucket and key. Returns nil if
// not found.
func (s *PostgresStore) GetObject(ctx context.Context, bucket, key string) (*backend.ObjectRecord, error) {
	start := time.Now()
	defer func() { recordQuery("GetObject", start) }()

	row, err := s.queries.GetObject(ctx, sqlc.GetObjectParams{
		Bucket: bucket,
		Key:    key,
	})
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	rec := &backend.ObjectRecord{
		Bucket:      bucket,
		Key:         key,
		GmailMsgID:  row.GmailMsgID,
		DriveFileID: row.DriveFileID,
		ETag:        row.Etag,
		Size:        row.Size,
		ContentType: row.ContentType,
		CreatedAt:   row.CreatedAt.Time,
	}
	if len(row.Metadata) > 0 {
		_ = json.Unmarshal(row.Metadata, &rec.Metadata)
	}
	return rec, nil
}

// DeleteObject removes an object record from the index.
func (s *PostgresStore) DeleteObject(ctx context.Context, bucket, key string) error {
	start := time.Now()
	defer func() { recordQuery("DeleteObject", start) }()

	return s.queries.DeleteObject(ctx, sqlc.DeleteObjectParams{
		Bucket: bucket,
		Key:    key,
	})
}

// ListObjects returns object records matching a bucket and optional key
// prefix, ordered by key.
func (s *PostgresStore) ListObjects(ctx context.Context, bucket, prefix, startAfter string, maxKeys int) ([]*backend.ObjectRecord, error) {
	start := time.Now()
	defer func() { recordQuery("ListObjects", start) }()

	var rows []sqlc.ListObjectsByPrefixRow
	var err error

	if prefix != "" {
		rows, err = s.queries.ListObjectsByPrefix(ctx, sqlc.ListObjectsByPrefixParams{
			Bucket:  bucket,
			Key:     prefix + "%",
			Key_2:   startAfter,
			Limit:   int32(maxKeys),
		})
	} else {
		allRows, allErr := s.queries.ListObjectsAll(ctx, sqlc.ListObjectsAllParams{
			Bucket: bucket,
			Key:    startAfter,
			Limit:  int32(maxKeys),
		})
		if allErr != nil {
			return nil, allErr
		}
		// Convert to prefix row type
		for i := range allRows {
			rows = append(rows, sqlc.ListObjectsByPrefixRow(allRows[i]))
		}
		err = nil
	}

	if err != nil {
		return nil, err
	}

	records := make([]*backend.ObjectRecord, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		rec := &backend.ObjectRecord{
			Bucket:      bucket,
			Key:         row.Key,
			GmailMsgID:  row.GmailMsgID,
			DriveFileID: row.DriveFileID,
			ETag:        row.Etag,
			Size:        row.Size,
			ContentType: row.ContentType,
			CreatedAt:   row.CreatedAt.Time,
		}
		if len(row.Metadata) > 0 {
			_ = json.Unmarshal(row.Metadata, &rec.Metadata)
		}
		records = append(records, rec)
	}
	return records, nil
}

// -------------------------------------------------------------------------
// BUCKET OPERATIONS
// -------------------------------------------------------------------------

// PutBucket inserts or replaces a bucket record.
func (s *PostgresStore) PutBucket(ctx context.Context, rec *backend.BucketRecord) error {
	start := time.Now()
	defer func() { recordQuery("PutBucket", start) }()

	return s.queries.PutBucket(ctx, sqlc.PutBucketParams{
		Name:      rec.Name,
		LabelID:   rec.LabelID,
		CreatedAt: pgtype.Timestamptz{Time: rec.CreatedAt, Valid: true},
	})
}

// GetBucket retrieves a bucket record by name. Returns nil if not found.
func (s *PostgresStore) GetBucket(ctx context.Context, name string) (*backend.BucketRecord, error) {
	start := time.Now()
	defer func() { recordQuery("GetBucket", start) }()

	row, err := s.queries.GetBucket(ctx, name)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	return &backend.BucketRecord{
		Name:      name,
		LabelID:   row.LabelID,
		CreatedAt: row.CreatedAt.Time,
	}, nil
}

// ListBuckets returns all bucket records ordered by name.
func (s *PostgresStore) ListBuckets(ctx context.Context) ([]*backend.BucketRecord, error) {
	start := time.Now()
	defer func() { recordQuery("ListBuckets", start) }()

	rows, err := s.queries.ListBuckets(ctx)
	if err != nil {
		return nil, err
	}

	records := make([]*backend.BucketRecord, 0, len(rows))
	for _, row := range rows {
		records = append(records, &backend.BucketRecord{
			Name:      row.Name,
			LabelID:   row.LabelID,
			CreatedAt: row.CreatedAt.Time,
		})
	}
	return records, nil
}
