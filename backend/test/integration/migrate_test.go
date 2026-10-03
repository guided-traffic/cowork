//go:build integration

package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	pgxmigrate "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/store"
	"github.com/guided-traffic/cowork/backend/test/fixture"
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

// migrateTo brings a database to one version of the embedded set, the way
// store.Migrate runs it, so a test can arrange the data an earlier release
// left before the migration under test runs.
func migrateTo(t *testing.T, ownerURL string, version uint) {
	t.Helper()
	cfg, err := pgx.ParseConfig(ownerURL)
	require.NoError(t, err)
	cfg.RuntimeParams[store.RuntimeRoleSetting] = runtimeRole
	driver, err := pgxmigrate.WithInstance(stdlib.OpenDB(*cfg), &pgxmigrate.Config{MigrationsTable: store.MigrationsTable})
	require.NoError(t, err)
	source, err := iofs.New(store.MigrationsFS(), ".")
	require.NoError(t, err)
	m, err := migrate.NewWithInstance("iofs", source, "pgx5", driver)
	require.NoError(t, err)
	require.NoError(t, m.Migrate(version))
	srcErr, dbErr := m.Close()
	require.NoError(t, srcErr)
	require.NoError(t, dbErr)
}

// docs/adr/0014 D2, docs/adr/0028 D3: migration 17 gives every open ticket of
// a project a key in number order, evenly spaced, leaves the done and dropped
// ones unranked, and restores the forced row-level security it lifts for its
// backfill.
func TestRankMigrationKeepsNumberOrder(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	name := fmt.Sprintf("cowork_it_rank_%d", time.Now().UnixNano())
	require.NoError(t, createDatabase(ctx, env.AdminURL, name))
	t.Cleanup(func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), time.Minute)
		defer dropCancel()
		assert.NoError(t, dropDatabase(dropCtx, env.AdminURL, name))
	})
	adminURL, err := withUserAndDatabase(env.AdminURL, "", "", name)
	require.NoError(t, err)
	ownerURL, err := withUserAndDatabase(env.AdminURL, ownerRole, ownerRole, name)
	require.NoError(t, err)

	migrateTo(t, ownerURL, 16)
	f, err := fixture.Connect(ctx, adminURL)
	require.NoError(t, err)
	t.Cleanup(f.Close)
	person, err := f.Person(ctx, uniqueSlug("ranker"), "Ranker")
	require.NoError(t, err)
	tenant, err := f.Tenant(ctx, uniqueSlug("rank"), "Rank")
	require.NoError(t, err)
	small, err := f.Project(ctx, tenant, "SMALL", "Small")
	require.NoError(t, err)
	large, err := f.Project(ctx, tenant, "LARGE", "Large")
	require.NoError(t, err)
	for range 6 {
		_, _, err := f.Ticket(ctx, tenant, small, person, "small")
		require.NoError(t, err)
	}
	for range 70 {
		_, _, err := f.Ticket(ctx, tenant, large, person, "large")
		require.NoError(t, err)
	}
	require.NoError(t, f.Exec(ctx, `UPDATE tickets SET state = 'done', done_at = now() WHERE project_id = $1 AND number IN (2, 5)`, small))
	require.NoError(t, f.Exec(ctx, `UPDATE tickets SET state = 'dropped' WHERE project_id = $1 AND number = 3`, small))

	res, err := store.Migrate(ctx, ownerURL, runtimeRole)
	require.NoError(t, err)
	assert.EqualValues(t, 1, res.Applied)

	for _, p := range []struct {
		id       uuid.UUID
		open     []int
		terminal []int
		width    int
	}{
		{small, []int{1, 4, 6}, []int{2, 3, 5}, 2},
		{large, seq(1, 70), nil, 3},
	} {
		keys := map[int]*string{}
		rows, err := f.Query(ctx, "SELECT number, rank FROM tickets WHERE project_id = $1", p.id)
		require.NoError(t, err)
		for rows.Next() {
			var n int
			var k *string
			require.NoError(t, rows.Scan(&n, &k))
			keys[n] = k
		}
		require.NoError(t, rows.Err())
		rows.Close()
		previous := ""
		for _, n := range p.open {
			k := keys[n]
			require.NotNil(t, k, "open ticket %d is ranked", n)
			assert.True(t, domain.ValidRank(*k), *k)
			assert.LessOrEqual(t, len(*k), p.width)
			assert.Less(t, previous, *k, "in number order")
			if previous != "" {
				between, err := domain.RankBetween(previous, *k)
				require.NoError(t, err)
				assert.LessOrEqual(t, len(between), p.width+1, "with room between two keys")
			}
			previous = *k
		}
		for _, n := range p.terminal {
			assert.Nil(t, keys[n], "a terminal ticket has no rank")
		}
		var byKey []int
		rows, err = f.Query(ctx, "SELECT number FROM tickets WHERE project_id = $1 AND rank IS NOT NULL ORDER BY rank", p.id)
		require.NoError(t, err)
		for rows.Next() {
			var n int
			require.NoError(t, rows.Scan(&n))
			byKey = append(byKey, n)
		}
		require.NoError(t, rows.Err())
		rows.Close()
		assert.Equal(t, p.open, byKey, "the column orders the keys byte by byte, digits before capitals before small letters")
	}

	var forced bool
	require.NoError(t, f.QueryRow(ctx, "SELECT relforcerowsecurity FROM pg_class WHERE oid = 'tickets'::regclass").Scan(&forced))
	assert.True(t, forced, "row-level security is forced on tickets again")
	var granted bool
	require.NoError(t, f.QueryRow(ctx, "SELECT has_column_privilege($1, 'tickets', 'rank', 'UPDATE')", runtimeRole).Scan(&granted))
	assert.True(t, granted, "the runtime role writes the rank")
}

func seq(from, to int) []int {
	out := make([]int, 0, to-from+1)
	for i := from; i <= to; i++ {
		out = append(out, i)
	}
	return out
}
