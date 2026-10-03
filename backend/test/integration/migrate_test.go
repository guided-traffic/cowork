//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/store"
)

func TestMigrateBringsFreshDatabaseToCurrentVersion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	embedded, err := store.EmbeddedVersion()
	require.NoError(t, err)
	assert.Equal(t, embedded, env.Migrated.Version, "TestMain's run reached the newest version")
	assert.Equal(t, embedded, env.Migrated.Applied, "a fresh database applies every migration")
	assert.False(t, env.Migrated.Dirty)

	second, err := store.Migrate(ctx, env.OwnerURL, runtimeRole)
	require.NoError(t, err)
	assert.Equal(t, embedded, second.Version, "a second run finds the schema current")
	assert.Zero(t, second.Applied, "a second run applies nothing")
	assert.False(t, second.Dirty)
	assert.False(t, second.Ahead)

	db := openRuntime(t)
	state, err := db.SchemaState(ctx)
	require.NoError(t, err)
	assert.Equal(t, embedded, state.Version, "the runtime role reads the recorded version")
	pending, err := state.Pending()
	require.NoError(t, err)
	assert.Zero(t, pending)

	f := fixtures(t)
	serverVersion, err := f.QueryCount(ctx, "SELECT current_setting('server_version_num')::int")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, serverVersion, int64(180000), "the schema relies on PostgreSQL 18 built-ins such as uuidv7()")
}

// An image rolled back over a newer schema keeps serving: neither `migrate`
// nor the start-up check refuses a database ahead of the binary
// (docs/adr/0028 D4, docs/adr/0057 D3).
func TestSchemaAheadOfTheBinaryIsServed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	f := fixtures(t)
	require.NoError(t, f.Exec(ctx, "UPDATE "+store.MigrationsTable+" SET version = version + 1"))
	t.Cleanup(func() {
		require.NoError(t, f.Exec(context.Background(), "UPDATE "+store.MigrationsTable+" SET version = version - 1"))
	})

	res, err := store.Migrate(ctx, env.OwnerURL, runtimeRole)
	require.NoError(t, err)
	assert.True(t, res.Ahead)
	assert.Zero(t, res.Applied)

	state, err := openRuntime(t).SchemaState(ctx)
	require.NoError(t, err)
	assert.True(t, state.Ahead())
	pending, err := state.Pending()
	require.NoError(t, err)
	assert.Zero(t, pending)
}

func TestTenantIdsAreUUIDv7(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	f := fixtures(t)
	id, err := f.Tenant(ctx, uniqueSlug("uuid"), "uuid")
	require.NoError(t, err)
	assert.EqualValues(t, 7, id.Version())
}
