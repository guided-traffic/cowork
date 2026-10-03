// Package store owns the PostgreSQL connection pool, the schema migrations and
// the only ways to reach the data (docs/adr/0027).
//
// The schema lives in migrations/ as NNNNNN_<name>.up.sql files, embedded into
// the binary and applied by Migrate under the owner role; there are no down
// files (docs/adr/0028). The server connects as the runtime role, which owns
// nothing (docs/adr/0021 D2), through a DB whose pool is unexported: a query
// runs inside InTenant or Installation, a write inside Mutate, and nothing
// else hands out a connection.
package store

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DefaultSlowQuery is the duration above which a query is logged at warn
// (docs/adr/0027 D8).
const DefaultSlowQuery = 500 * time.Millisecond

// Options configures Open.
type Options struct {
	// Logger receives slow-query warnings. nil discards them.
	Logger *slog.Logger
	// SlowQuery is the threshold of the slow-query log; zero means
	// DefaultSlowQuery.
	SlowQuery time.Duration
}

// DB is the runtime role's connection pool behind the transaction wrappers.
type DB struct {
	pool   *pgxpool.Pool
	logger *slog.Logger
}

// Open opens a connection pool on databaseURL, verifies it with a ping and
// returns it behind the wrappers. Timestamps are read in UTC: the API speaks
// UTC (docs/adr/0055 D3), and pgx would otherwise decode into time.Local.
func Open(ctx context.Context, databaseURL string, opts Options) (*DB, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	threshold := opts.SlowQuery
	if threshold <= 0 {
		threshold = DefaultSlowQuery
	}
	cfg.ConnConfig.Tracer = &slowQueryTracer{logger: logger, threshold: threshold}
	cfg.AfterConnect = func(_ context.Context, conn *pgx.Conn) error {
		conn.TypeMap().RegisterType(&pgtype.Type{
			Name:  "timestamptz",
			OID:   pgtype.TimestamptzOID,
			Codec: &pgtype.TimestamptzCodec{ScanLocation: time.UTC},
		})
		return nil
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("open database pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return &DB{pool: pool, logger: logger}, nil
}

// Ping checks that the database answers; /readyz calls it.
func (db *DB) Ping(ctx context.Context) error {
	return db.pool.Ping(ctx)
}

// Close closes the pool.
func (db *DB) Close() {
	db.pool.Close()
}

// slowQueryTracer logs a query that took longer than the threshold, by the
// name sqlc writes into its first line, never with its arguments.
type slowQueryTracer struct {
	logger    *slog.Logger
	threshold time.Duration
}

type traceStartKey struct{}

type traceStart struct {
	at  time.Time
	sql string
}

func (t *slowQueryTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, traceStartKey{}, traceStart{at: time.Now(), sql: data.SQL})
}

func (t *slowQueryTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	start, ok := ctx.Value(traceStartKey{}).(traceStart)
	if !ok {
		return
	}
	if took := time.Since(start.at); took > t.threshold {
		t.logger.Warn("slow query", "query", queryName(start.sql), "duration", took, "failed", data.Err != nil)
	}
}

// queryName returns the sqlc name of a query ("-- name: GetTicket :one") or
// the first word of an unnamed one.
func queryName(sql string) string {
	line := strings.TrimSpace(strings.SplitN(sql, "\n", 2)[0])
	if rest, ok := strings.CutPrefix(line, "-- name: "); ok {
		return strings.Fields(rest)[0]
	}
	if fields := strings.Fields(line); len(fields) > 0 {
		return fields[0]
	}
	return ""
}

// ErrNotFound is returned when a row the caller asked for does not exist or
// is not visible to it; the API answers both alike (docs/adr/0047 D5).
var ErrNotFound = errors.New("not found")

// notFound maps pgx's no-rows error onto ErrNotFound.
func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
