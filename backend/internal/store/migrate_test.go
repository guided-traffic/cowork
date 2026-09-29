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

// The embedded migration set is checked without a database: every file is
// named by the pattern, every up has its down, and the versions are the
// sequence 1..n with no gap. A gap or an orphan would surface only when the
// migration runs against PostgreSQL, one job later.
func TestMigrationFilesAreWellFormed(t *testing.T) {
	entries, err := fs.ReadDir(MigrationsFS(), ".")
	require.NoError(t, err)
	require.NotEmpty(t, entries, "at least one migration is expected")

	ups := map[uint64]string{}
	downs := map[uint64]string{}
	for _, e := range entries {
		require.False(t, e.IsDir(), "no subdirectories under migrations/: %s", e.Name())
		match := migrationFilePattern.FindStringSubmatch(e.Name())
		require.NotNil(t, match, "%s does not match NNNNNN_<snake_name>.(up|down).sql", e.Name())

		version, err := strconv.ParseUint(match[1], 10, 64)
		require.NoError(t, err)
		require.NotZero(t, version, "%s: version 0 is reserved for an empty schema", e.Name())

		body, err := fs.ReadFile(MigrationsFS(), e.Name())
		require.NoError(t, err)
		assert.NotEmpty(t, body, "%s is empty", e.Name())

		switch match[3] {
		case "up":
			require.NotContains(t, ups, version, "two up migrations carry version %d", version)
			ups[version] = e.Name()
		case "down":
			require.NotContains(t, downs, version, "two down migrations carry version %d", version)
			downs[version] = e.Name()
		}
	}

	for v, name := range ups {
		assert.Contains(t, downs, v, "%s has no down migration", name)
	}
	for v, name := range downs {
		assert.Contains(t, ups, v, "%s has no up migration", name)
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
	total := uint(len(entries) / 2)

	n, err := countVersionsBetween(0, total)
	require.NoError(t, err)
	assert.Equal(t, total, n, "a fresh database applies every migration")

	n, err = countVersionsBetween(total, total)
	require.NoError(t, err)
	assert.Zero(t, n, "a current database applies nothing")
}

func TestMigrateRejectsUnparsableURL(t *testing.T) {
	_, err := Migrate(context.Background(), "://not-a-url")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse database url")
}

func TestConnectRejectsUnparsableURL(t *testing.T) {
	_, err := Connect(context.Background(), "://not-a-url")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse database url")
}
