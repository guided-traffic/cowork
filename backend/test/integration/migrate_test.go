//go:build integration

// Package integration holds the tests that need a running PostgreSQL 18.
// `make test-integration` provides COWORK_TEST_DATABASE_URL; the variable is
// required, not optional, so a misconfigured job fails instead of passing on
// zero tests.
package integration

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/store"
)

const envTestDatabaseURL = "COWORK_TEST_DATABASE_URL"

func databaseURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv(envTestDatabaseURL)
	if url == "" {
		t.Fatalf("%s is not set: `make postgres-up` starts a local PostgreSQL 18 and `make test-integration` sets the variable", envTestDatabaseURL)
	}
	return url
}

func TestMigrateBringsFreshDatabaseToCurrentVersion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	url := databaseURL(t)

	first, err := store.Migrate(ctx, url)
	require.NoError(t, err)
	assert.False(t, first.Dirty)
	assert.NotZero(t, first.Version)

	second, err := store.Migrate(ctx, url)
	require.NoError(t, err)
	assert.Equal(t, first.Version, second.Version, "a second run finds the schema current")
	assert.Zero(t, second.Applied, "a second run applies nothing")
	assert.False(t, second.Dirty)

	pool, err := store.Connect(ctx, url)
	require.NoError(t, err)
	defer pool.Close()

	var serverVersion int
	require.NoError(t, pool.QueryRow(ctx, "SELECT current_setting('server_version_num')::int").Scan(&serverVersion))
	assert.GreaterOrEqual(t, serverVersion, 180000, "the schema relies on PostgreSQL 18 built-ins such as uuidv7()")

	var tenantsExists bool
	require.NoError(t, pool.QueryRow(ctx, "SELECT to_regclass('public.tenants') IS NOT NULL").Scan(&tenantsExists))
	assert.True(t, tenantsExists, "migration 000001 creates tenants")

	var recorded uint
	require.NoError(t, pool.QueryRow(ctx, "SELECT version FROM "+store.MigrationsTable).Scan(&recorded))
	assert.Equal(t, first.Version, recorded)
}

func TestTenantIdsAreUUIDv7(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	url := databaseURL(t)

	_, err := store.Migrate(ctx, url)
	require.NoError(t, err)

	pool, err := store.Connect(ctx, url)
	require.NoError(t, err)
	defer pool.Close()

	var version int
	err = pool.QueryRow(ctx,
		`WITH t AS (INSERT INTO tenants (slug, name) VALUES ('it-' || substr(md5(random()::text), 1, 8), 'integration') RETURNING id)
		 SELECT uuid_extract_version(id) FROM t`).Scan(&version)
	require.NoError(t, err)
	assert.Equal(t, 7, version)
}
