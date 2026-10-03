package store

import (
	"context"
	"io/fs"
	"sort"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The embedded migration set is checked without a database: every file is an
// up migration named by the pattern (there are no down files, docs/adr/0028),
// none is empty, and the versions are the sequence 1..n with no gap. A gap or
// a stray file would surface only when the migration runs against PostgreSQL,
// one job later.
func TestMigrationFilesAreWellFormed(t *testing.T) {
	entries, err := fs.ReadDir(MigrationsFS(), ".")
	require.NoError(t, err)
	require.NotEmpty(t, entries, "at least one migration is expected")

	ups := map[uint64]string{}
	for _, e := range entries {
		require.False(t, e.IsDir(), "no subdirectories under migrations/: %s", e.Name())
		match := migrationFilePattern.FindStringSubmatch(e.Name())
		require.NotNil(t, match, "%s does not match NNNNNN_<snake_name>.up.sql (down files are not used)", e.Name())

		version, err := strconv.ParseUint(match[1], 10, 64)
		require.NoError(t, err)
		require.NotZero(t, version, "%s: version 0 is reserved for an empty schema", e.Name())

		body, err := fs.ReadFile(MigrationsFS(), e.Name())
		require.NoError(t, err)
		assert.NotEmpty(t, body, "%s is empty", e.Name())

		require.NotContains(t, ups, version, "two migrations carry version %d", version)
		ups[version] = e.Name()
	}

	versions := make([]uint64, 0, len(ups))
	for v := range ups {
		versions = append(versions, v)
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i] < versions[j] })
	for i, v := range versions {
		assert.Equal(t, uint64(i+1), v, "versions must be 1..n without a gap, got %v", versions)
	}
}

func TestCountVersionsBetween(t *testing.T) {
	entries, err := fs.ReadDir(MigrationsFS(), ".")
	require.NoError(t, err)
	total := uint(len(entries))

	n, err := countVersionsBetween(0, total)
	require.NoError(t, err)
	assert.Equal(t, total, n, "a fresh database applies every migration")

	n, err = countVersionsBetween(total, total)
	require.NoError(t, err)
	assert.Zero(t, n, "a current database applies nothing")
}

func TestMigrateRejectsUnparsableURL(t *testing.T) {
	_, err := Migrate(context.Background(), "://not-a-url", "cowork_app")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse owner database url")
}

// The owner role and the runtime role must differ (docs/adr/0021 D2); the
// refusal comes before any connection is made.
func TestMigrateRefusesTheOwnerAsRuntimeRole(t *testing.T) {
	_, err := Migrate(context.Background(), "postgres://cowork_owner:secret@localhost:1/cowork", "cowork_owner")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is the owner role")
}

func TestMigrateRefusesAnUnnamedRuntimeRole(t *testing.T) {
	_, err := Migrate(context.Background(), "postgres://cowork_owner:secret@localhost:1/cowork", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not named")
}

func TestOpenRejectsUnparsableURL(t *testing.T) {
	_, err := Open(context.Background(), "://not-a-url", Options{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse database url")
}

func TestRoleFromURL(t *testing.T) {
	role, err := RoleFromURL("postgres://cowork_app:secret@db:5432/cowork?sslmode=require")
	require.NoError(t, err)
	assert.Equal(t, "cowork_app", role)

	_, err = RoleFromURL("://not-a-url")
	assert.Error(t, err)
}

func TestSchemaStatePending(t *testing.T) {
	embedded, err := EmbeddedVersion()
	require.NoError(t, err)
	require.Greater(t, embedded, uint(1))

	fresh := SchemaState{Version: 0, Embedded: embedded}
	n, err := fresh.Pending()
	require.NoError(t, err)
	assert.Equal(t, embedded, n)
	assert.False(t, fresh.Ahead())

	current := SchemaState{Version: embedded, Embedded: embedded}
	n, err = current.Pending()
	require.NoError(t, err)
	assert.Zero(t, n)

	ahead := SchemaState{Version: embedded + 1, Embedded: embedded}
	n, err = ahead.Pending()
	require.NoError(t, err)
	assert.Zero(t, n)
	assert.True(t, ahead.Ahead())
}

func TestQueryName(t *testing.T) {
	assert.Equal(t, "GetTenant", queryName("-- name: GetTenant :one\nSELECT 1"))
	assert.Equal(t, "SELECT", queryName("SELECT 1"))
	assert.Empty(t, queryName(""))
}
