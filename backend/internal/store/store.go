// Package store owns the PostgreSQL connection pool and the schema migrations.
//
// The schema lives in migrations/ as NNNNNN_<name>.up.sql files, embedded into
// the binary and applied by Migrate. There are no down files: the schema only
// moves forward, and a migration never removes what the previous release
// still reads (docs/adr/0028). The server applies the migrations on start
// (config.EnvMigrateOnStart) and `cowork migrate` applies them on demand.
package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Connect opens a connection pool on databaseURL and verifies it with a ping.
func Connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("open database pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}
