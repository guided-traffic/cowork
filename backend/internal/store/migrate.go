package store

import (
	"context"
	"database/sql"
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

// RuntimeRoleSetting is the session setting through which the migration run
// names the runtime role; every migration grants that role what it needs.
const RuntimeRoleSetting = "cowork.runtime_role"

// migrationFilePattern is the file name every migration must match. There are
// no down migrations: the schema only moves forward, and a migration never
// removes what the previous release still reads (docs/adr/0028).
var migrationFilePattern = regexp.MustCompile(`^(\d{6})_([a-z0-9_]+)\.up\.sql$`)

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
	// Ahead is true when the database carries a version newer than this
	// binary knows: an image was rolled back over a newer schema. Nothing is
	// applied, and the older binary serves, because no migration removes what
	// the previous release reads (docs/adr/0028 D3, D4).
	Ahead bool
}

// RoleFromURL returns the user a PostgreSQL URL connects as.
func RoleFromURL(databaseURL string) (string, error) {
	cfg, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return "", fmt.Errorf("parse database url: %w", err)
	}
	if cfg.User == "" {
		return "", errors.New("database url names no user")
	}
	return cfg.User, nil
}

// Migrate applies every pending up migration under the owner role at
// ownerURL, granting the runtime role what each migration grants, and then
// checks that the runtime role cannot bypass row-level security
// (docs/adr/0021 D2). It is safe to call from several replicas at once:
// golang-migrate serialises them with an advisory lock. A database that is
// current is not an error, and neither is one that is ahead of this binary.
func Migrate(ctx context.Context, ownerURL, runtimeRole string) (MigrateResult, error) {
	connCfg, err := pgx.ParseConfig(ownerURL)
	if err != nil {
		return MigrateResult{}, fmt.Errorf("parse owner database url: %w", err)
	}
	if runtimeRole == "" {
		return MigrateResult{}, errors.New("the runtime role is not named")
	}
	if runtimeRole == connCfg.User {
		return MigrateResult{}, fmt.Errorf("the runtime role %q is the owner role: migrations need a separate owner role (docs/adr/0021 D2)", runtimeRole)
	}
	connCfg.RuntimeParams[RuntimeRoleSetting] = runtimeRole
	// Two handles: golang-migrate closes the one it is given.
	checks := stdlib.OpenDB(*connCfg)
	defer func() { _ = checks.Close() }()
	if err := checks.PingContext(ctx); err != nil {
		return MigrateResult{}, fmt.Errorf("ping database as owner: %w", err)
	}
	if err := checkRoleAsOwner(ctx, checks, runtimeRole, false); err != nil {
		return MigrateResult{}, err
	}

	res, err := applyMigrations(stdlib.OpenDB(*connCfg))
	if err != nil {
		return res, err
	}
	if err := checkRoleAsOwner(ctx, checks, runtimeRole, true); err != nil {
		return res, err
	}
	return res, nil
}

// applyMigrations runs golang-migrate on db and closes it.
func applyMigrations(db *sql.DB) (MigrateResult, error) {
	driver, err := pgxmigrate.WithInstance(db, &pgxmigrate.Config{MigrationsTable: MigrationsTable})
	if err != nil {
		_ = db.Close()
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
	embedded, err := EmbeddedVersion()
	if err != nil {
		return MigrateResult{}, err
	}
	if before > embedded {
		return MigrateResult{Version: before, Ahead: true}, nil
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

// EmbeddedVersion is the highest migration version compiled into the binary.
func EmbeddedVersion() (uint, error) {
	versions, err := embeddedVersions()
	if err != nil {
		return 0, err
	}
	var highest uint
	for _, v := range versions {
		highest = max(highest, v)
	}
	return highest, nil
}

func embeddedVersions() ([]uint, error) {
	entries, err := fs.ReadDir(MigrationsFS(), ".")
	if err != nil {
		return nil, fmt.Errorf("list migrations: %w", err)
	}
	var versions []uint
	for _, e := range entries {
		match := migrationFilePattern.FindStringSubmatch(e.Name())
		if match == nil {
			continue
		}
		v, err := strconv.ParseUint(match[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("migration %s: %w", e.Name(), err)
		}
		versions = append(versions, uint(v))
	}
	return versions, nil
}

// countVersionsBetween counts the embedded up migrations with a version in
// (from, to].
func countVersionsBetween(from, to uint) (uint, error) {
	versions, err := embeddedVersions()
	if err != nil {
		return 0, err
	}
	var n uint
	for _, v := range versions {
		if v > from && v <= to {
			n++
		}
	}
	return n, nil
}

// SchemaState reads the schema version as the runtime role sees it.
type SchemaState struct {
	Version  uint
	Dirty    bool
	Embedded uint
}

// Pending is the number of embedded migrations the database lacks.
func (s SchemaState) Pending() (uint, error) {
	if s.Version >= s.Embedded {
		return 0, nil
	}
	return countVersionsBetween(s.Version, s.Embedded)
}

// Ahead reports a database newer than this binary.
func (s SchemaState) Ahead() bool { return s.Version > s.Embedded }

// SchemaState reads the recorded schema version through the runtime role,
// which the first migration grants SELECT on the version table. A database
// that has never been migrated reads as version 0.
func (db *DB) SchemaState(ctx context.Context) (SchemaState, error) {
	embedded, err := EmbeddedVersion()
	if err != nil {
		return SchemaState{}, err
	}
	state := SchemaState{Embedded: embedded}
	var exists bool
	if err := db.pool.QueryRow(ctx, "SELECT to_regclass('public."+MigrationsTable+"') IS NOT NULL").Scan(&exists); err != nil {
		return SchemaState{}, fmt.Errorf("read schema version: %w", err)
	}
	if !exists {
		return state, nil
	}
	err = db.pool.QueryRow(ctx, "SELECT version, dirty FROM "+MigrationsTable).Scan(&state.Version, &state.Dirty)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return state, nil
	case err != nil:
		return SchemaState{}, fmt.Errorf("read schema version: %w", err)
	}
	return state, nil
}
