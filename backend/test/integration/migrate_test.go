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
	embedded, err := store.EmbeddedVersion()
	require.NoError(t, err)
	assert.EqualValues(t, embedded-16, res.Applied, "migration 17 and every later one")

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

// docs/adr/0017 D2, D3, docs/adr/0009 D5, docs/adr/0028 D3: migrations 18
// and 19 keep a ticket's progress as the implementation stage, fill the
// refinement stage from decided on and the review stage when done, take a done
// ticket as done from in-progress — by its stages when they are full, by hand
// when it has children or its progress fell below 100 after it closed —
// derive the parents' new stages from the leaves up, and restore the forced
// row-level security the backfill lifts.
func TestStagesMigrationBackfill(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	name := fmt.Sprintf("cowork_it_stages_%d", time.Now().UnixNano())
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

	migrateTo(t, ownerURL, 17)
	f, err := fixture.Connect(ctx, adminURL)
	require.NoError(t, err)
	t.Cleanup(f.Close)
	person, err := f.Person(ctx, uniqueSlug("stager"), "Stager")
	require.NoError(t, err)
	tenant, err := f.Tenant(ctx, uniqueSlug("stages"), "Stages")
	require.NoError(t, err)
	project, err := f.Project(ctx, tenant, "STAGE", "Stage")
	require.NoError(t, err)
	ids := map[string]uuid.UUID{}
	// What the previous release leaves: a state, its block and its dates, the
	// progress, and done at 100.
	for _, tk := range []struct{ name, set string }{
		{"filed", "progress = 5"},
		{"analysed", "state = 'analysed', progress = 10"},
		{"decided", "state = 'decided', progress = 20"},
		{"in-progress", "state = 'in-progress', progress = 40"},
		{"blocked early", "state = 'blocked', blocked_from = 'analysed', block_kind = 'human', block_reason = 'away', progress = 15"},
		{"blocked late", "state = 'blocked', blocked_from = 'in-progress', block_kind = 'external', block_reason = 'vendor', progress = 60"},
		{"done", "state = 'done', done_at = now(), progress = 100"},
		// A parent the previous release closed takes the last derived
		// progress as its own when its last child leaves.
		{"done below full", "state = 'done', done_at = now(), progress = 40"},
		{"dropped", "state = 'dropped', progress = 30"},
		{"grand", "progress = 0"},
		{"parent", "state = 'in-progress', progress = 0"},
		{"small", "state = 'decided', effort = 'XS', progress = 20"},
		{"large", "state = 'done', done_at = now(), effort = 'L', progress = 100"},
		{"sibling", "effort = 'M', progress = 0"},
		{"done parent", "state = 'done', done_at = now(), progress = 100"},
		{"done child", "state = 'done', done_at = now(), progress = 100"},
	} {
		id, _, err := f.Ticket(ctx, tenant, project, person, tk.name)
		require.NoError(t, err)
		require.NoError(t, f.Exec(ctx, "UPDATE tickets SET "+tk.set+" WHERE id = $1", id))
		ids[tk.name] = id
	}
	for child, parent := range map[string]string{"parent": "grand", "sibling": "grand", "small": "parent", "large": "parent",
		"done child": "done parent"} {
		require.NoError(t, f.Exec(ctx, "UPDATE tickets SET parent_id = $1 WHERE id = $2", ids[parent], ids[child]))
	}
	for _, parent := range []string{"parent", "grand", "done parent"} {
		require.NoError(t, f.Exec(ctx, "UPDATE tickets SET progress_derived = ticket_derived_progress(tenant_id, id) WHERE id = $1", ids[parent]))
	}

	res, err := store.Migrate(ctx, ownerURL, runtimeRole)
	require.NoError(t, err)
	embedded, err := store.EmbeddedVersion()
	require.NoError(t, err)
	assert.EqualValues(t, embedded-17, res.Applied, "migrations 18 and 19 and every later one")

	type row struct {
		refinement, implementation, review int
		refinementDerived, reviewDerived   *int
		doneFrom                           *string
		doneByHand                         bool
	}
	read := func(name string) row {
		var r row
		require.NoError(t, f.QueryRow(ctx, `SELECT progress_refinement, progress, progress_review, progress_refinement_derived,
			progress_review_derived, done_from::text, done_by_hand FROM tickets WHERE id = $1`, ids[name]).
			Scan(&r.refinement, &r.implementation, &r.review, &r.refinementDerived, &r.reviewDerived, &r.doneFrom, &r.doneByHand))
		return r
	}
	for name, want := range map[string][3]int{
		"filed": {0, 5, 0}, "analysed": {0, 10, 0}, "decided": {100, 20, 0}, "in-progress": {100, 40, 0},
		"blocked early": {0, 15, 0}, "blocked late": {100, 60, 0}, "done": {100, 100, 100}, "done below full": {100, 40, 100},
		"dropped": {0, 30, 0},
	} {
		r := read(name)
		assert.Equal(t, want, [3]int{r.refinement, r.implementation, r.review}, name)
		assert.Nil(t, r.refinementDerived, name)
	}
	done := read("done")
	require.NotNil(t, done.doneFrom)
	assert.Equal(t, "in-progress", *done.doneFrom, "the one way the previous release had")
	assert.False(t, done.doneByHand, "a done ticket without children is done by its full stages")
	assert.True(t, read("done below full").doneByHand, "short of full, by hand")
	assert.Nil(t, read("in-progress").doneFrom)

	parent := read("parent")
	require.NotNil(t, parent.refinementDerived)
	assert.Equal(t, 100, *parent.refinementDerived, "(1×100 + 5×100) / 6")
	assert.Equal(t, 85, *parent.reviewDerived, "(1×0 + 5×100) / 6 = 83.3 → 85")
	grand := read("grand")
	assert.Equal(t, 40, *grand.refinementDerived, "(2×100 + 3×0) / 5, the parent's derived value")
	assert.Equal(t, 35, *grand.reviewDerived, "(2×85 + 3×0) / 5 = 34 → 35, derived a level after the parent")
	doneParent := read("done parent")
	assert.True(t, doneParent.doneByHand, "a parent is done by hand")
	assert.False(t, read("done child").doneByHand)

	var forced bool
	require.NoError(t, f.QueryRow(ctx, "SELECT relforcerowsecurity FROM pg_class WHERE oid = 'tickets'::regclass").Scan(&forced))
	assert.True(t, forced, "row-level security is forced on tickets again")
	for _, column := range []string{"progress_refinement", "progress_review", "progress_refinement_derived", "progress_review_derived",
		"done_from", "done_by_hand"} {
		var granted bool
		require.NoError(t, f.QueryRow(ctx, "SELECT has_column_privilege($1, 'tickets', $2, 'UPDATE')", runtimeRole, column).Scan(&granted))
		assert.True(t, granted, "the runtime role writes %s", column)
	}
	var review bool
	require.NoError(t, f.QueryRow(ctx, "SELECT 'review' = ANY (enum_range(NULL::ticket_state)::text[])").Scan(&review))
	assert.True(t, review, "the state review exists")
	require.Error(t, f.Exec(ctx, "UPDATE tickets SET state = 'in-progress' WHERE id = $1", ids["blocked late"]),
		"a blocked ticket's block is kept with blocked only")
}

// docs/adr/0010 D3 as amended 2026-10-04, docs/adr/0028 D3: migration 29 keeps
// the horizon every ticket showed — one that derived release or icebox under
// rule set v1 and had none set holds it as its own, set by nobody, with a
// reason and a new version; one set stays — makes every derived value later
// under v2, and restores the forced row-level security it lifts.
func TestHorizonMigrationKeepsWhatTicketsShow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	name := fmt.Sprintf("cowork_it_horizon_%d", time.Now().UnixNano())
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

	migrateTo(t, ownerURL, 28)
	f, err := fixture.Connect(ctx, adminURL)
	require.NoError(t, err)
	t.Cleanup(f.Close)
	person, err := f.Person(ctx, uniqueSlug("planner"), "Planner")
	require.NoError(t, err)
	tenant, err := f.Tenant(ctx, uniqueSlug("horizon"), "Horizon")
	require.NoError(t, err)
	project, err := f.Project(ctx, tenant, "PLAN", "Plan")
	require.NoError(t, err)
	for range 4 {
		_, _, err := f.Ticket(ctx, tenant, project, person, "planned")
		require.NoError(t, err)
	}
	require.NoError(t, f.Exec(ctx, `UPDATE tickets SET urgency_rule = 'v1:default' WHERE project_id = $1`, project))
	require.NoError(t, f.Exec(ctx, `UPDATE tickets SET urgency_derived = 'icebox', urgency_rule = 'v1:icebox-decision'
		WHERE project_id = $1 AND number = 1`, project))
	require.NoError(t, f.Exec(ctx, `UPDATE tickets SET urgency_derived = 'release', urgency_rule = 'v1:release-block',
		urgency_override = 'now', urgency_override_by = $2, urgency_override_at = now() WHERE project_id = $1 AND number = 2`, project, person))
	require.NoError(t, f.Exec(ctx, `UPDATE tickets SET state = 'dropped', urgency_derived = 'icebox', urgency_rule = 'v1:icebox-block'
		WHERE project_id = $1 AND number = 4`, project))

	_, err = store.Migrate(ctx, ownerURL, runtimeRole)
	require.NoError(t, err)

	type row struct {
		derived, rule string
		set, reason   *string
		by            *uuid.UUID
		version       int
	}
	rows, err := f.Query(ctx, `SELECT number, urgency_derived, urgency_rule, urgency_override, urgency_override_reason,
		urgency_override_by, version FROM tickets WHERE project_id = $1`, project)
	require.NoError(t, err)
	got := map[int]row{}
	for rows.Next() {
		var n int
		var r row
		require.NoError(t, rows.Scan(&n, &r.derived, &r.rule, &r.set, &r.reason, &r.by, &r.version))
		got[n] = r
	}
	require.NoError(t, rows.Err())
	rows.Close()

	for n, r := range got {
		assert.Equal(t, "later", r.derived, "ticket %d derives later", n)
		assert.Equal(t, "v2:default", r.rule, "ticket %d", n)
	}
	for _, n := range []int{1, 4} {
		require.NotNil(t, got[n].set, "ticket %d keeps its horizon", n)
		assert.Equal(t, "icebox", *got[n].set)
		assert.Nil(t, got[n].by, "set by nobody")
		require.NotNil(t, got[n].reason)
		assert.Contains(t, *got[n].reason, "kept from the derivation")
		assert.Equal(t, 2, got[n].version, "what it shows is its own now")
	}
	require.NotNil(t, got[2].set)
	assert.Equal(t, "now", *got[2].set, "a horizon set stays")
	assert.Equal(t, person, *got[2].by)
	assert.Equal(t, 1, got[2].version)
	assert.Nil(t, got[3].set, "later stays later, nothing set")
	assert.Equal(t, 1, got[3].version)

	var forced bool
	require.NoError(t, f.QueryRow(ctx, "SELECT relforcerowsecurity FROM pg_class WHERE oid = 'tickets'::regclass").Scan(&forced))
	assert.True(t, forced, "row-level security is forced on tickets again")
}

// docs/adr/0043 D4 as amended 2026-10-05, docs/adr/0028 D3: migration 37 lets
// the capability sets of the tokens and of the chat name set-horizon beside
// override-urgency, which the release before reads; it rewrites no row — the
// rewrite and the old name's removal are migration 38's —, and a name outside
// the catalogue stays refused.
func TestTheCapabilityMigrationTakesBothNamesAndRewritesNothing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	name := fmt.Sprintf("cowork_it_capability_%d", time.Now().UnixNano())
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

	migrateTo(t, ownerURL, 36)
	f, err := fixture.Connect(ctx, adminURL)
	require.NoError(t, err)
	t.Cleanup(f.Close)
	person, err := f.Person(ctx, uniqueSlug("capable"), "Capable")
	require.NoError(t, err)
	_, before, err := f.Token(ctx, fixture.TokenSpec{UserID: person, Agent: true, Capabilities: []string{"rank", "override-urgency"}})
	require.NoError(t, err)
	require.NoError(t, f.Exec(ctx, `INSERT INTO chat_capabilities (user_id, capabilities) VALUES ($1, ARRAY['override-urgency'])`, person))
	_, _, err = f.Token(ctx, fixture.TokenSpec{UserID: person, Agent: true, Capabilities: []string{"set-horizon"}})
	require.Error(t, err, "before migration 37 the new name is refused")

	migrateTo(t, ownerURL, 37)

	kept, err := f.QueryCount(ctx, `SELECT count(*) FROM tokens WHERE id = $1 AND capabilities = ARRAY['rank', 'override-urgency']::text[]`, before)
	require.NoError(t, err)
	assert.EqualValues(t, 1, kept, "a token's stored set is not rewritten")
	kept, err = f.QueryCount(ctx, `SELECT count(*) FROM chat_capabilities WHERE user_id = $1 AND capabilities = ARRAY['override-urgency']::text[]`, person)
	require.NoError(t, err)
	assert.EqualValues(t, 1, kept, "the chat's stored set is not rewritten")

	_, _, err = f.Token(ctx, fixture.TokenSpec{UserID: person, Agent: true, Capabilities: []string{"set-horizon", "override-urgency"}})
	assert.NoError(t, err, "a token takes either name")
	assert.NoError(t, f.Exec(ctx, `UPDATE chat_capabilities SET capabilities = ARRAY['set-horizon'] WHERE user_id = $1`, person),
		"the chat takes the new name")
	_, _, err = f.Token(ctx, fixture.TokenSpec{UserID: person, Agent: true, Capabilities: []string{"horizon"}})
	assert.Error(t, err, "a name outside the catalogue stays refused")
	assert.Error(t, f.Exec(ctx, `UPDATE chat_capabilities SET capabilities = ARRAY['set-urgency'] WHERE user_id = $1`, person))
}

// docs/adr/0043 D4 and docs/adr/0010 D1 as amended 2026-10-06, docs/adr/0028
// D3: migration 38 rewrites what migration 37 let the release before store.
// Every override-urgency in the capability sets of the tokens — a revoked
// one's too — and of the chat becomes set-horizon, each name once, in the
// order it was first named; a saved filter's urgency becomes horizon, a
// horizon already there winning; nothing else of a row moves. The checks
// still take the old name, which the release before writes after an image
// rollback, and refuse a name outside the catalogue; the forced row-level
// security the rewrite lifts is restored.
func TestTheContractMigrationRewritesTheNamesBefore(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	name := fmt.Sprintf("cowork_it_contract_%d", time.Now().UnixNano())
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

	migrateTo(t, ownerURL, 37)
	f, err := fixture.Connect(ctx, adminURL)
	require.NoError(t, err)
	t.Cleanup(f.Close)
	person, err := f.Person(ctx, uniqueSlug("contract"), "Contract")
	require.NoError(t, err)
	tenant, err := f.Tenant(ctx, uniqueSlug("contract"), "Contract")
	require.NoError(t, err)

	tokens := map[string]struct {
		before, after []string
		revoked       bool
	}{
		"the release before 0.5":     {before: []string{"rank", "override-urgency"}, after: []string{"rank", "set-horizon"}},
		"0.5, both names":            {before: []string{"rank", "set-horizon", "override-urgency"}, after: []string{"rank", "set-horizon"}},
		"the old name first":         {before: []string{"override-urgency", "rank", "set-horizon"}, after: []string{"set-horizon", "rank"}},
		"revoked":                    {before: []string{"override-urgency"}, after: []string{"set-horizon"}, revoked: true},
		"without the capability":     {before: []string{"close"}, after: []string{"close"}},
		"the new name only, as kept": {before: []string{"set-horizon", "upload"}, after: []string{"set-horizon", "upload"}},
	}
	tokenIDs := map[string]uuid.UUID{}
	for label, c := range tokens {
		_, id, err := f.Token(ctx, fixture.TokenSpec{UserID: person, Agent: true, Capabilities: c.before, Revoked: c.revoked})
		require.NoError(t, err, label)
		tokenIDs[label] = id
	}

	chats := map[string]struct{ before, after []string }{
		"the old name":  {before: []string{"override-urgency"}, after: []string{"set-horizon"}},
		"both names":    {before: []string{"rank", "set-horizon", "interest", "override-urgency"}, after: []string{"rank", "set-horizon", "interest"}},
		"untouched set": {before: []string{"rank"}, after: []string{"rank"}},
	}
	chatOwners := map[string]uuid.UUID{}
	chosenAt := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	for label, c := range chats {
		owner, err := f.Person(ctx, uniqueSlug("chat"), label)
		require.NoError(t, err)
		require.NoError(t, f.Exec(ctx, `INSERT INTO chat_capabilities (user_id, capabilities, updated_at) VALUES ($1, $2, $3)`,
			owner, c.before, chosenAt))
		chatOwners[label] = owner
	}

	filters := map[string]struct{ before, after string }{
		"saved with urgency": {before: `{"urgency": ["now", "!icebox"], "state": ["filed"]}`,
			after: `{"horizon": ["now", "!icebox"], "state": ["filed"]}`},
		"both, horizon wins": {before: `{"horizon": ["next"], "urgency": ["now"]}`, after: `{"horizon": ["next"]}`},
		"saved with horizon": {before: `{"horizon": ["later"]}`, after: `{"horizon": ["later"]}`},
	}
	filterIDs := map[string]uuid.UUID{}
	for label, c := range filters {
		var id uuid.UUID
		require.NoError(t, f.QueryRow(ctx, `INSERT INTO saved_filters (tenant_id, owner_id, name, parameters, version)
			VALUES ($1, $2, $3, $4::jsonb, 3) RETURNING id`, tenant, person, label, c.before).Scan(&id))
		filterIDs[label] = id
	}

	_, err = store.Migrate(ctx, ownerURL, runtimeRole)
	require.NoError(t, err)

	for label, c := range tokens {
		var got []string
		require.NoError(t, f.QueryRow(ctx, `SELECT capabilities FROM tokens WHERE id = $1`, tokenIDs[label]).Scan(&got), label)
		assert.Equal(t, c.after, got, "a token: %s", label)
	}
	revoked, err := f.QueryCount(ctx, `SELECT count(*) FROM tokens WHERE id = $1 AND revoked_at IS NOT NULL`, tokenIDs["revoked"])
	require.NoError(t, err)
	assert.EqualValues(t, 1, revoked, "a revoked token stays revoked")

	for label, c := range chats {
		var got []string
		var at time.Time
		require.NoError(t, f.QueryRow(ctx, `SELECT capabilities, updated_at FROM chat_capabilities WHERE user_id = $1`,
			chatOwners[label]).Scan(&got, &at), label)
		assert.Equal(t, c.after, got, "a chat: %s", label)
		assert.True(t, chosenAt.Equal(at), "the time the person chose stays: %s", label)
	}

	for label, c := range filters {
		var got string
		var version int
		require.NoError(t, f.QueryRow(ctx, `SELECT parameters::text, version FROM saved_filters WHERE id = $1`,
			filterIDs[label]).Scan(&got, &version), label)
		assert.JSONEq(t, c.after, got, "a filter: %s", label)
		assert.Equal(t, 3, version, "a filter keeps its version: %s", label)
	}

	_, _, err = f.Token(ctx, fixture.TokenSpec{UserID: person, Agent: true, Capabilities: []string{"rank", "set-horizon", "override-urgency"}})
	assert.NoError(t, err, "a token as the release before writes it after a rollback is taken")
	assert.NoError(t, f.Exec(ctx, `UPDATE chat_capabilities SET capabilities = ARRAY['rank', 'set-horizon', 'override-urgency']
		WHERE user_id = $1`, chatOwners["untouched set"]), "a chat set as the release before writes it is taken")
	_, _, err = f.Token(ctx, fixture.TokenSpec{UserID: person, Agent: true, Capabilities: []string{"horizon"}})
	assert.Error(t, err, "a name outside the catalogue stays refused")
	assert.Error(t, f.Exec(ctx, `UPDATE chat_capabilities SET capabilities = ARRAY['set-urgency'] WHERE user_id = $1`,
		chatOwners["untouched set"]))

	for _, table := range []string{"tokens", "chat_capabilities", "saved_filters"} {
		var forced bool
		require.NoError(t, f.QueryRow(ctx, "SELECT relforcerowsecurity FROM pg_class WHERE oid = $1::regclass", table).Scan(&forced))
		assert.True(t, forced, "row-level security is forced on %s again", table)
	}
}

// docs/adr/0028 D3, docs/adr/0018 D5 as amended 2026-10-06: migration 39
// widens the policies of saved_filters for an administrator's unshare and
// deletion of another person's shared filter and changes no row — what an
// administrator of the tenant could not do on version 38 they do on 39, and a
// filter that is not shared stays its owner's alone.
func TestTheModerationMigrationWidensTheFilterPoliciesAndChangesNoRow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	name := fmt.Sprintf("cowork_it_moderation_%d", time.Now().UnixNano())
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
	runtimeURL, err := withUserAndDatabase(env.AdminURL, runtimeRole, runtimeRole, name)
	require.NoError(t, err)

	migrateTo(t, ownerURL, 38)
	f, err := fixture.Connect(ctx, adminURL)
	require.NoError(t, err)
	t.Cleanup(f.Close)
	tenant, err := f.Tenant(ctx, uniqueSlug("moderation"), "Moderation")
	require.NoError(t, err)
	owner, err := f.Person(ctx, uniqueSlug("owner"), "Owner")
	require.NoError(t, err)
	admin, err := f.Person(ctx, uniqueSlug("admin"), "Admin")
	require.NoError(t, err)
	require.NoError(t, f.Member(ctx, tenant, owner, domain.RoleMember))
	require.NoError(t, f.Member(ctx, tenant, admin, domain.RoleAdmin))
	savedAt := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	filter := func(name string, shared bool) uuid.UUID {
		var id uuid.UUID
		require.NoError(t, f.QueryRow(ctx, `INSERT INTO saved_filters (tenant_id, owner_id, name, parameters, shared, version, updated_at)
			VALUES ($1, $2, $3, '{"state": ["filed"]}', $4, 3, $5) RETURNING id`, tenant, owner, name, shared, savedAt).Scan(&id))
		return id
	}
	shared, private := filter("shared", true), filter("private", false)

	// asAdmin runs one statement as the runtime role for the administrator,
	// the filter named as the unshare names it, and rolls it back.
	asAdmin := func(sql string, args ...any) int64 {
		conn, err := pgx.Connect(ctx, runtimeURL)
		require.NoError(t, err)
		defer func() { _ = conn.Close(ctx) }()
		tx, err := conn.Begin(ctx)
		require.NoError(t, err)
		defer func() { _ = tx.Rollback(ctx) }()
		_, err = tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true), set_config('app.user_id', $2, true),
			set_config('app.saved_filter_id', $3, true)`, tenant.String(), admin.String(), shared.String())
		require.NoError(t, err)
		tag, err := tx.Exec(ctx, sql, args...)
		require.NoError(t, err)
		return tag.RowsAffected()
	}
	unshare := `UPDATE saved_filters SET shared = false, version = version + 1, updated_at = now()
		WHERE tenant_id = $1 AND id = $2 AND shared AND version = 3`
	remove := `DELETE FROM saved_filters WHERE tenant_id = $1 AND id = $2`
	assert.Zero(t, asAdmin(unshare, tenant, shared), "version 38 holds every write to the owner")
	assert.Zero(t, asAdmin(remove, tenant, shared))

	_, err = store.Migrate(ctx, ownerURL, runtimeRole)
	require.NoError(t, err)

	for id, want := range map[uuid.UUID]bool{shared: true, private: false} {
		var (
			isShared bool
			version  int
			at       time.Time
			params   string
		)
		require.NoError(t, f.QueryRow(ctx, `SELECT shared, version, updated_at, parameters::text FROM saved_filters WHERE id = $1`, id).
			Scan(&isShared, &version, &at, &params))
		assert.Equal(t, want, isShared, "the migration changes no row")
		assert.Equal(t, 3, version)
		assert.True(t, savedAt.Equal(at))
		assert.JSONEq(t, `{"state": ["filed"]}`, params)
	}
	assert.EqualValues(t, 1, asAdmin(unshare, tenant, shared), "an administrator unshares another person's shared filter")
	assert.EqualValues(t, 1, asAdmin(remove, tenant, shared), "and deletes it")
	assert.Zero(t, asAdmin(remove, tenant, private), "and never touches one that is not shared")
	assert.Zero(t, asAdmin(`SELECT 1 FROM saved_filters WHERE id = $1`, private), "or reads it")

	var forced bool
	require.NoError(t, f.QueryRow(ctx, "SELECT relforcerowsecurity FROM pg_class WHERE oid = 'saved_filters'::regclass").Scan(&forced))
	assert.True(t, forced)
}
