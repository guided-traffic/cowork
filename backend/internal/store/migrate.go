package store

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"strconv"

	"github.com/golang-migrate/migrate/v4"
	pgxmigrate "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// MigrationsTable is the table golang-migrate keeps the schema version in.
const MigrationsTable = "schema_migrations"

// migrationFilePattern is the file name every migration must match.
var migrationFilePattern = regexp.MustCompile(`^(\d{6})_([a-z0-9_]+)\.(up|down)\.sql$`)

// MigrationsFS returns the embedded migration files, rooted at the directory
// that holds them.
func MigrationsFS() fs.FS {
	sub, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		panic(err) // the directory is compiled in; a failure here is a build defect
	}
	return sub
}

// MigrateResult reports what Migrate found and did.
type MigrateResult struct {
	// Version is the schema version after the call.
	Version uint
	// Applied is the number of migrations this call applied.
	Applied uint
	// Dirty is true when a previous run failed halfway; Migrate does not
	// return a dirty schema without an error.
	Dirty bool
}

// Migrate applies every pending up migration to the database at databaseURL
// and returns the resulting version. It is safe to call from several replicas
// at once: golang-migrate serialises the callers with a PostgreSQL advisory
// lock. A database that is already current is not an error.
func Migrate(ctx context.Context, databaseURL string) (MigrateResult, error) {
	connCfg, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return MigrateResult{}, fmt.Errorf("parse database url: %w", err)
	}
	db := stdlib.OpenDB(*connCfg)
	defer func() { _ = db.Close() }()
	if err := db.PingContext(ctx); err != nil {
		return MigrateResult{}, fmt.Errorf("ping database: %w", err)
	}

	driver, err := pgxmigrate.WithInstance(db, &pgxmigrate.Config{MigrationsTable: MigrationsTable})
	if err != nil {
		return MigrateResult{}, fmt.Errorf("open migration driver: %w", err)
	}
	source, err := iofs.New(migrationFiles, "migrations")
	if err != nil {
		return MigrateResult{}, fmt.Errorf("open migration source: %w", err)
	}
	m, err := migrate.NewWithInstance("iofs", source, "pgx5", driver)
	if err != nil {
		return MigrateResult{}, fmt.Errorf("prepare migrations: %w", err)
	}
	defer func() { _, _ = m.Close() }()

	before, dirty, err := currentVersion(m)
	if err != nil {
		return MigrateResult{}, err
	}
	if dirty {
		return MigrateResult{Version: before, Dirty: true},
			fmt.Errorf("schema version %d is dirty: a previous migration failed halfway and needs a manual repair", before)
	}

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		after, dirtyAfter, _ := currentVersion(m)
		return MigrateResult{Version: after, Dirty: dirtyAfter}, fmt.Errorf("apply migrations: %w", err)
	}

	after, dirtyAfter, err := currentVersion(m)
	if err != nil {
		return MigrateResult{}, err
	}
	applied, err := countVersionsBetween(before, after)
	if err != nil {
		return MigrateResult{}, err
	}
	return MigrateResult{Version: after, Applied: applied, Dirty: dirtyAfter}, nil
}

// currentVersion maps golang-migrate's "no version yet" to version 0.
func currentVersion(m *migrate.Migrate) (uint, bool, error) {
	v, dirty, err := m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("read schema version: %w", err)
	}
	return v, dirty, nil
}

// countVersionsBetween counts the embedded up migrations with a version in
// (from, to].
func countVersionsBetween(from, to uint) (uint, error) {
	entries, err := fs.ReadDir(MigrationsFS(), ".")
	if err != nil {
		return 0, fmt.Errorf("list migrations: %w", err)
	}
	var n uint
	for _, e := range entries {
		match := migrationFilePattern.FindStringSubmatch(e.Name())
		if match == nil || match[3] != "up" {
			continue
		}
		v, err := strconv.ParseUint(match[1], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("migration %s: %w", e.Name(), err)
		}
		if uint(v) > from && uint(v) <= to {
			n++
		}
	}
	return n, nil
}
