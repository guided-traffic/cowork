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
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/guided-traffic/cowork/backend/internal/metrics"
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
	// Metrics receives what the store can tell (docs/adr/0060 D4): the pool,
	// the schema state and the consistency check's counts, read at a scrape,
	// the failed statements by kind, the jobs' runs and the committed acts.
	// nil records nothing.
	Metrics *metrics.Metrics
}

// DB is the runtime role's connection pool behind the transaction wrappers.
type DB struct {
	pool    *pgxpool.Pool
	logger  *slog.Logger
	metrics *metrics.Metrics
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
	cfg.ConnConfig.Tracer = &slowQueryTracer{logger: logger, threshold: threshold, metrics: opts.Metrics}
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
	db := &DB{pool: pool, logger: logger, metrics: opts.Metrics}
	opts.Metrics.ObservePool(db.poolStats)
	opts.Metrics.ObserveSchema(db.schemaForMetrics)
	opts.Metrics.ObserveConsistency(db.consistencyForMetrics)
	return db, nil
}

// poolStats is what the pool says of itself, for a scrape.
func (db *DB) poolStats() metrics.PoolStats {
	s := db.pool.Stat()
	return metrics.PoolStats{
		Idle: s.IdleConns(), InUse: s.AcquiredConns(), Constructing: s.ConstructingConns(), Max: s.MaxConns(),
		Acquires: s.AcquireCount(), AcquireTime: s.AcquireDuration(),
		Waits: s.EmptyAcquireCount(), WaitTime: s.EmptyAcquireWaitTime(),
		Canceled: s.CanceledAcquireCount(),
	}
}

// schemaForMetrics reads the recorded schema version and dirty flag for a
// scrape (docs/adr/0060 D6); a failed read is logged, its error can name the
// host and the user, so it never reaches the scrape.
func (db *DB) schemaForMetrics(ctx context.Context) (uint, bool, error) {
	state, err := db.SchemaState(ctx)
	if err != nil {
		db.logger.Warn("the schema version could not be read for the metrics", "error", err)
		return 0, false, err
	}
	return state.Version, state.Dirty, nil
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
// name sqlc writes into its first line, never with its arguments, and counts
// a statement that failed by its kind (docs/adr/0060 D4).
type slowQueryTracer struct {
	logger    *slog.Logger
	threshold time.Duration
	metrics   *metrics.Metrics
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
	if data.Err != nil && !errors.Is(data.Err, pgx.ErrNoRows) {
		t.metrics.QueryError(queryErrorKind(data.Err))
	}
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

// kindConnection is the kind of a statement that failed on its connection:
// one that broke, could not be made, or was lost before the statement went out.
const kindConnection = "connection"

// sqlStates names the SQLSTATEs a failed statement is counted by; another
// state of class 23 is an integrity_violation, of class 08 a connection
// failure, any other other_sqlstate (docs/adr/0060 D4, D5: a closed set).
var sqlStates = map[string]string{
	"23505": "unique_violation",
	"23503": "foreign_key_violation",
	"23514": "check_violation",
	"23502": "not_null_violation",
	"40001": "serialization_failure",
	"40P01": "deadlock_detected",
	"42501": "insufficient_privilege",
	"55P03": "lock_not_available",
	"57014": "query_canceled",
}

// queryErrorKind names what made a statement fail, for the metrics: the
// SQLSTATE's meaning where PostgreSQL answered — insufficient_privilege is a
// policy, a grant or a guard that refused —, the request's end where the
// context ended it, a connection that broke, or other.
func queryErrorKind(err error) string {
	var pgErr *pgconn.PgError
	switch {
	case errors.As(err, &pgErr):
		if kind, ok := sqlStates[pgErr.Code]; ok {
			return kind
		}
		switch {
		case strings.HasPrefix(pgErr.Code, "23"):
			return "integrity_violation"
		case strings.HasPrefix(pgErr.Code, "08"):
			return kindConnection
		}
		return "other_sqlstate"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case pgconn.SafeToRetry(err), pgconn.Timeout(err):
		return kindConnection
	}
	var connect *pgconn.ConnectError
	if errors.As(err, &connect) {
		return kindConnection
	}
	return "other"
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
