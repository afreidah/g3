// -------------------------------------------------------------------------------
// g3 - CLI Shared Helpers
//
// Author: Alex Freidah
//
// Helpers shared by the g3 subcommand packages under internal/cli/*. Living in
// internal/ (not cmd/) keeps the subcommand logic importable and unit-testable;
// cmd/g3 retains only the os.Exit wrappers and dispatch.
// -------------------------------------------------------------------------------

// Package cli holds helpers shared by the g3 subcommand packages under
// internal/cli/*. Each subcommand lives in its own package (versioncmd,
// validatecmd, synccmd, authcmd, servecmd) so it is independently testable.
package cli

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/afreidah/g3/internal/backend"
	"github.com/afreidah/g3/internal/config"
	"github.com/afreidah/g3/internal/store"
)

// DefaultConfigPath is the fallback path for the -config flag.
const DefaultConfigPath = "config.yaml"

// InitMetadataStore initializes the configured metadata store and returns it
// along with a cleanup function to close it.
func InitMetadataStore(ctx context.Context, cfg *config.DatabaseConfig) (backend.MetadataStore, func(), error) {
	switch cfg.Driver {
	case "postgres":
		pgStore, err := store.NewPostgres(ctx, cfg)
		if err != nil {
			return nil, nil, fmt.Errorf("postgres: %w", err)
		}
		slog.InfoContext(ctx, "Metadata store initialized", "driver", "postgres", "host", cfg.Host)
		return pgStore, func() { _ = pgStore.Close() }, nil
	default:
		sqliteStore, err := store.NewSQLite(cfg.Path)
		if err != nil {
			return nil, nil, fmt.Errorf("sqlite: %w", err)
		}
		slog.InfoContext(ctx, "Metadata store initialized", "driver", "sqlite", "path", cfg.Path)
		return sqliteStore, func() { _ = sqliteStore.Close() }, nil
	}
}
