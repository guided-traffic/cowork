// Package store owns the PostgreSQL connection pool and the schema migrations.
//
// The schema lives in migrations/ as pairs of NNNNNN_<name>.up.sql and
// .down.sql files, embedded into the binary and applied by Migrate. The
// server applies them on start (config.EnvMigrateOnStart) and `cowork migrate`
// applies them on demand.
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
