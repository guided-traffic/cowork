//go:build integration

package integration

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/store"
)

// The crossings between teams of migration 47 (docs/adr/0021 D7 as made
// concrete 2026-10-10), proved on the database before anything is built on
// them: the setting a crossing function admits itself by, the owner role's
// policies that admit it and nobody else, the guard on what a crossing writes,
// and the sight it decides for identities holding a role in one team, both or
// neither (docs/adr/0005 D3, docs/adr/0034 D4, docs/adr/0065 D5).

// crossing is newWorld with tickets related across its teams: PA of A the
// parent of CB of B, and CA of A; PC of A confidential — no assignee, AdminA
// its reporter — the parent of CB2 of B and of CA2 of A; PR of A in a project
// restricted from everybody but A's administrators, the parent of CB3 of B.
type crossing struct {
	world
	Gamma                             uuid.UUID
	PA, CB, CA, PC, CB2, CA2, PR, CB3 uuid.UUID
}

func newCrossing(t *testing.T) crossing {
	t.Helper()
	ctx := context.Background()
	f := fixtures(t)
	c := crossing{world: newWorld(t)}
	ticket := func(tenant, project, reporter uuid.UUID, title string) uuid.UUID {
		id, _, err := f.Ticket(ctx, tenant, project, reporter, title)
		require.NoError(t, err)
		return id
	}
	var err error
	c.Gamma, err = f.Project(ctx, c.A, "GAMMA", "Gamma")
	require.NoError(t, err)
	require.NoError(t, f.Exec(ctx, "UPDATE projects SET restricted = true WHERE id = $1", c.Gamma))
	c.PA = ticket(c.A, c.ProjectA, c.MemberA, "the parent of A")
	c.CA = ticket(c.A, c.ProjectA, c.MemberA, "a child of A")
	c.CB = ticket(c.B, c.ProjectB, c.MemberB, "a child of B")
	c.PC = ticket(c.A, c.ProjectA, c.AdminA, "the confidential parent of A")
	c.CA2 = ticket(c.A, c.ProjectA, c.MemberA, "a child of A under the confidential parent")
	c.CB2 = ticket(c.B, c.ProjectB, c.MemberB, "a child of B under the confidential parent")
	c.PR = ticket(c.A, c.Gamma, c.AdminA, "the parent of A in a restricted project")
	c.CB3 = ticket(c.B, c.ProjectB, c.MemberB, "a child of B under the restricted parent")
	require.NoError(t, f.Exec(ctx, "UPDATE tickets SET confidential = true WHERE id = $1", c.PC))
	for child, parent := range map[uuid.UUID]uuid.UUID{c.CB: c.PA, c.CA: c.PA, c.CB2: c.PC, c.CA2: c.PC, c.CB3: c.PR} {
		require.NoError(t, f.Exec(ctx, "UPDATE tickets SET parent_id = $1 WHERE id = $2", parent, child))
	}
	return c
}

// relationsOf reads a ticket's relations of the kinds as the context's caller,
// in the team.
func relationsOf(ctx context.Context, t *testing.T, team, ticket uuid.UUID, kinds ...string) []store.Relation {
	t.Helper()
	var rels []store.Relation
	require.NoError(t, openRuntime(t).InTenant(ctx, team, func(r *store.Reader) error {
		var err error
		rels, err = r.RelationHeads(ctx, []uuid.UUID{ticket}, kinds...)
		return err
	}))
	return rels
}

// An act recorded in another team's record holds the ticket it is recorded at
// against the deletion of its row until the transaction ends — a purge waits
// for it and empties it with the rest —, and one recorded at a ticket a purge
// took away since the crossing named it records nothing and fails nothing
// (docs/adr/0024 D2 as made concrete 2026-10-10).
func TestAnActElsewhereHoldsItsTicketAgainstAPurge(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	c := newCrossing(t)
	db := openRuntime(t)
	admin, err := pgx.Connect(ctx, env.AdminURL)
	require.NoError(t, err)
	defer func() { _ = admin.Close(ctx) }()
	// childElsewhere is the far end of PA's one child in another team, CB.
	childElsewhere := func(w *store.Writer) store.FarEnd {
		rels, err := w.RelationsElsewhere(ctx, c.PA)
		require.NoError(t, err)
		for _, r := range rels {
			if r.Kind == store.RelationChild {
				return r.Far
			}
		}
		t.Fatal("no child of PA in another team")
		return store.FarEnd{}
	}
	held := store.Event{EntityType: "ticket", Action: "updated", Reason: "the held act"}
	acts := func() int64 {
		n, err := fixtures(t).QueryCount(ctx, `SELECT count(*) FROM audit_events WHERE ticket_id = $1 AND reason = 'the held act'`, c.CB)
		require.NoError(t, err)
		return n
	}

	_, err = db.Mutate(as(c.MemberA), c.A, func(w *store.Writer) error {
		if err := w.RecordElsewhere(ctx, childElsewhere(w), held); err != nil {
			return err
		}
		tx, err := admin.Begin(ctx)
		require.NoError(t, err)
		defer func() { _ = tx.Rollback(ctx) }()
		_, err = tx.Exec(ctx, "SET LOCAL lock_timeout = '300ms'")
		require.NoError(t, err)
		_, err = tx.Exec(ctx, "DELETE FROM tickets WHERE id = $1", c.CB)
		var pgErr *pgconn.PgError
		require.True(t, errors.As(err, &pgErr) && pgErr.Code == "55P03", "the deletion waits for the act's transaction: %v", err)
		w.Record(store.Event{EntityType: "seed", Action: "created"})
		return nil
	})
	require.NoError(t, err)
	assert.EqualValues(t, 1, acts(), "the act stands")

	_, err = db.Mutate(as(c.MemberA), c.A, func(w *store.Writer) error {
		far := childElsewhere(w)
		_, err := admin.Exec(ctx, "DELETE FROM tickets WHERE id = $1", c.CB)
		require.NoError(t, err)
		if err := w.RecordElsewhere(ctx, far, held); err != nil {
			return err
		}
		w.Record(store.Event{EntityType: "seed", Action: "created"})
		return nil
	})
	require.NoError(t, err, "a ticket purged since the crossing named it fails nothing")
	assert.EqualValues(t, 1, acts(), "and records nothing")
}

// The setting a crossing admits itself by is set in the function's body and
// never by a SET clause, which PostgreSQL refuses to an owner role that is no
// superuser (verified on PostgreSQL 18.6); the owner's policy applies inside
// the function and never to the runtime role, even when the runtime role sets
// the setting itself; and the function leaves the caller the value it found.
func TestTheCrossingSettingIsTheFunctionsAndThePolicyTheOwners(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	c := newCrossing(t)
	f := fixtures(t)

	rows, err := f.Query(ctx, `SELECT p.proname, coalesce(p.proconfig, '{}')
		FROM pg_proc p WHERE p.pronamespace = 'public'::regnamespace AND p.prosecdef`)
	require.NoError(t, err)
	type definer struct {
		name   string
		config []string
	}
	definers, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (definer, error) {
		var d definer
		return d, r.Scan(&d.name, &d.config)
	})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(definers), 13, "the purge's function and the crossings")
	for _, d := range definers {
		pinned := false
		for _, setting := range d.config {
			assert.NotContains(t, setting, "app.crossing", "%s: the crossing is set in the body, never by a SET clause", d.name)
			pinned = pinned || (len(setting) > len("search_path=") && setting[:len("search_path=")] == "search_path=")
		}
		assert.True(t, pinned, "%s fixes no search_path", d.name)
	}

	conn, err := pgx.Connect(ctx, env.RuntimeURL)
	require.NoError(t, err)
	defer func() { _ = conn.Close(ctx) }()
	tx, err := conn.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true), set_config('app.user_id', $2, true)",
		c.B.String(), c.MemberB.String())
	require.NoError(t, err)

	for _, kind := range []string{"head", "walk", "derive", "purge", "act"} {
		_, err = tx.Exec(ctx, "SELECT set_config('app.crossing', $1, true)", kind)
		require.NoError(t, err)
		var seen, updated, deleted int64
		require.NoError(t, tx.QueryRow(ctx, "SELECT count(*) FROM tickets WHERE tenant_id <> $1", c.B).Scan(&seen))
		assert.Zero(t, seen, "the runtime role setting app.crossing to %s reads no ticket of another team", kind)
		require.NoError(t, tx.QueryRow(ctx,
			"WITH u AS (UPDATE tickets SET title = title WHERE tenant_id <> $1 RETURNING 1) SELECT count(*) FROM u", c.B).Scan(&updated))
		assert.Zero(t, updated, "nor writes one under %s", kind)
		require.NoError(t, tx.QueryRow(ctx,
			"WITH d AS (DELETE FROM ticket_links WHERE tenant_id <> $1 RETURNING 1) SELECT count(*) FROM d", c.B).Scan(&deleted))
		assert.Zero(t, deleted, "nor deletes another team's link under %s", kind)
	}

	_, err = tx.Exec(ctx, "SELECT set_config('app.crossing', 'derive', true)")
	require.NoError(t, err)
	var slug string
	var key *string
	var sight string
	require.NoError(t, tx.QueryRow(ctx, `SELECT team_slug, project_key, sight
		FROM relation_heads(ARRAY[$1]::uuid[], ARRAY['parent'])`, c.CB).Scan(&slug, &key, &sight))
	assert.Equal(t, c.SlugA, slug, "inside the function the owner's policy reads the other team")
	require.NotNil(t, key)
	assert.Equal(t, "ALPHA", *key)
	assert.Equal(t, string(store.SightHead), sight)
	var after string
	require.NoError(t, tx.QueryRow(ctx, "SELECT current_setting('app.crossing', true)").Scan(&after))
	assert.Equal(t, "derive", after, "the function leaves the caller the value it found")

	_, err = tx.Exec(ctx, "SELECT set_config('app.crossing', '', true)")
	require.NoError(t, err)
	require.NoError(t, tx.QueryRow(ctx, `SELECT count(*) FROM relation_heads(ARRAY[$1]::uuid[], ARRAY['parent'])`, c.CB).Scan(new(int64)))
	require.NoError(t, tx.QueryRow(ctx, "SELECT current_setting('app.crossing', true)").Scan(&after))
	assert.Empty(t, after)
}

// Whatever the setting, the runtime role's unfiltered queries under one team
// see nothing of another, relations between them included
// (TestUnfilteredQueryUnderTenantSeesNothingOfAnother with the crossings).
func TestTheCrossingIsTheOwnersAlone(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	c := newCrossing(t)
	seedEveryTenantTable(t, c.world)
	f := fixtures(t)
	require.NoError(t, f.Exec(ctx, `INSERT INTO ticket_links (tenant_id, type, source_id, target_id, created_by)
		VALUES ($1, 'blocks', $2, $3, $4), ($5, 'relates-to', $6, $7, $8)`,
		c.B, c.CB, c.PA, c.MemberB, c.A, c.CA, c.CB2, c.MemberA))
	tables := tenantBoundTables(t)

	conn, err := pgx.Connect(ctx, env.RuntimeURL)
	require.NoError(t, err)
	defer func() { _ = conn.Close(ctx) }()
	tx, err := conn.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true), set_config('app.user_id', $2, true)",
		c.A.String(), c.MemberA.String())
	require.NoError(t, err)
	for _, kind := range []string{"", "head", "walk", "derive", "purge", "act"} {
		_, err = tx.Exec(ctx, "SELECT set_config('app.crossing', $1, true)", kind)
		require.NoError(t, err)
		for _, table := range tables {
			var seen int64
			require.NoError(t, tx.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE tenant_id <> $1", c.A).Scan(&seen))
			assert.Zero(t, seen, "%s under app.crossing %q: rows of another team are visible", table, kind)
		}
		var tenants int64
		require.NoError(t, tx.QueryRow(ctx,
			"SELECT count(*) FROM tenants t WHERE NOT EXISTS (SELECT 1 FROM memberships m WHERE m.tenant_id = t.id AND m.user_id = $1)",
			c.MemberA).Scan(&tenants))
		assert.Zero(t, tenants, "under app.crossing %q no team the person holds no role in", kind)
	}
}

// A crossing that writes changes its own columns and nothing else, the owner
// role included: tickets_crossing_guard refuses the rest with 42501.
func TestACrossingWritesOnlyItsColumns(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	c := newCrossing(t)
	conn, err := pgx.Connect(ctx, env.OwnerURL)
	require.NoError(t, err)
	defer func() { _ = conn.Close(ctx) }()

	// try updates the ticket of team B as the owner inside a crossing of the
	// kind, the transaction bound to team, none for uuid.Nil.
	try := func(kind string, team uuid.UUID, set string) (int64, error) {
		tx, err := conn.Begin(ctx)
		require.NoError(t, err)
		defer func() { _ = tx.Rollback(ctx) }()
		_, err = tx.Exec(ctx, "SELECT set_config('app.crossing', $1, true)", kind)
		require.NoError(t, err)
		if team != uuid.Nil {
			_, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", team.String())
			require.NoError(t, err)
		}
		var n int64
		err = tx.QueryRow(ctx, "WITH u AS (UPDATE tickets SET "+set+" WHERE id = $1 RETURNING 1) SELECT count(*) FROM u", c.CB).Scan(&n)
		return n, err
	}
	refused := func(err error) bool {
		var pgErr *pgconn.PgError
		return errors.As(err, &pgErr) && pgErr.Code == "42501"
	}

	for _, team := range []uuid.UUID{uuid.Nil, c.A} {
		n, err := try("derive", team, "progress_derived = 50, progress_refinement_derived = 40, progress_review_derived = 30")
		require.NoError(t, err)
		assert.EqualValues(t, 1, n, "the derived write reaches a ticket of any team")
		for _, own := range []string{"progress_refinement = 95", "progress = 95", "progress_review = 95", "done_by_hand = true",
			"updated_at = now()"} {
			_, err = try("derive", team, "progress_derived = 50, "+own)
			assert.True(t, refused(err), "a derived write from outside the ticket's team leaves %s: %v", own, err)
		}
	}
	n, err := try("derive", c.B, "progress_derived = 50, progress_refinement = 95, done_by_hand = true, updated_at = now()")
	require.NoError(t, err)
	assert.EqualValues(t, 1, n, "inside the ticket's own team the derived write seeds its stages")
	_, err = try("derive", c.B, "title = 'taken over'")
	assert.True(t, refused(err), "the derived write changes no title: %v", err)
	_, err = try("derive", c.B, "version = version + 1")
	assert.True(t, refused(err), "nor a version: %v", err)
	n, err = try("purge", uuid.Nil, "parent_id = NULL")
	require.NoError(t, err)
	assert.EqualValues(t, 1, n, "the end of a relation clears a parent")
	_, err = try("purge", uuid.Nil, "parent_id = NULL, progress_derived = 50")
	assert.True(t, refused(err), "and nothing more: %v", err)
	n, err = try("head", uuid.Nil, "title = title")
	require.NoError(t, err)
	assert.Zero(t, n, "a reading crossing writes nothing")
}

// The sight of the other end of a relation, for identities holding a role in
// one team, both or neither (docs/adr/0005 D3, docs/adr/0034 D4,
// docs/adr/0065 D5 as amended 2026-10-10).
func TestTheSightOfARelation(t *testing.T) {
	ctx := context.Background()
	c := newCrossing(t)
	f := fixtures(t)
	parentOf := func(t *testing.T, caller context.Context, team, ticket uuid.UUID) (store.Head, bool) {
		t.Helper()
		rels := relationsOf(caller, t, team, ticket, store.RelationParent)
		if len(rels) == 0 {
			return store.Head{}, false
		}
		require.Len(t, rels, 1)
		return rels[0].Head, true
	}

	t.Run("a member of the child's team alone reads the parent's head", func(t *testing.T) {
		h, ok := parentOf(t, as(c.MemberB), c.B, c.CB)
		require.True(t, ok)
		assert.Equal(t, store.SightHead, h.Sight)
		assert.False(t, h.Readable())
		assert.Equal(t, c.SlugA+"/ALPHA-1", h.Key())
		assert.Equal(t, "the parent of A", h.Title)
		assert.Equal(t, "Team A", h.TeamName)
	})
	t.Run("a member of both teams reads it", func(t *testing.T) {
		h, ok := parentOf(t, as(c.Both), c.B, c.CB)
		require.True(t, ok)
		assert.Equal(t, store.SightSees, h.Sight)
	})
	t.Run("a member of the parent's team alone reads the child's head", func(t *testing.T) {
		rels := relationsOf(as(c.MemberA), t, c.A, c.PA, store.RelationChild)
		require.Len(t, rels, 2)
		sights := map[string]store.Sight{}
		for _, r := range rels {
			sights[r.Head.TeamSlug] = r.Head.Sight
		}
		assert.Equal(t, store.SightSees, sights[c.SlugA], "the child of the own team")
		assert.Equal(t, store.SightHead, sights[c.SlugB], "the child of the other team")
	})
	t.Run("a confidential parent is the placeholder to whoever is not admitted", func(t *testing.T) {
		for name, who := range map[string]struct {
			caller context.Context
			team   uuid.UUID
			ticket uuid.UUID
		}{
			"a member of the child's team":                    {as(c.MemberB), c.B, c.CB2},
			"a member of both who is not admitted":            {as(c.Both), c.B, c.CB2},
			"a member of the parent's own team, not admitted": {as(c.MemberA), c.A, c.CA2},
		} {
			h, ok := parentOf(t, who.caller, who.team, who.ticket)
			require.True(t, ok, name)
			assert.Equal(t, store.SightPlaceholder, h.Sight, name)
			assert.Empty(t, h.Key(), name)
			assert.Empty(t, h.Title, name)
			assert.Equal(t, "Team A", h.TeamName, name)
		}
		h, ok := parentOf(t, as(c.AdminA), c.A, c.CA2)
		require.True(t, ok)
		assert.Equal(t, store.SightSees, h.Sight, "an administrator of its team is admitted")
	})
	t.Run("a project restricted from a member shows its ticket's head", func(t *testing.T) {
		h, ok := parentOf(t, as(c.Both), c.B, c.CB3)
		require.True(t, ok)
		assert.Equal(t, store.SightHead, h.Sight)
		assert.Equal(t, c.SlugA+"/GAMMA-1", h.Key())
		require.NoError(t, f.Exec(ctx, `INSERT INTO project_access (tenant_id, project_id, user_id, role)
			VALUES ($1, $2, $3, 'member')`, c.A, c.Gamma, c.Both))
		h, _ = parentOf(t, as(c.Both), c.B, c.CB3)
		assert.Equal(t, store.SightSees, h.Sight, "on the project's list, the member reads it")
	})
	t.Run("a token restricted to a team reads every other team by heads", func(t *testing.T) {
		teamToken := store.WithCaller(context.Background(), store.Caller{UserID: c.Both, RestrictedTenantID: c.B,
			RequestID: uuid.Must(uuid.NewV7())})
		h, ok := parentOf(t, teamToken, c.B, c.CB)
		require.True(t, ok)
		assert.Equal(t, store.SightHead, h.Sight)
		projectToken := store.WithCaller(context.Background(), store.Caller{UserID: c.Both, RestrictedTenantID: c.B,
			RestrictedProjectID: c.ProjectB, RequestID: uuid.Must(uuid.NewV7())})
		h, ok = parentOf(t, projectToken, c.B, c.CB)
		require.True(t, ok)
		assert.Equal(t, store.SightHead, h.Sight)
		h, ok = parentOf(t, projectToken, c.B, c.CB2)
		require.True(t, ok)
		assert.Equal(t, store.SightPlaceholder, h.Sight, "outside its restriction a confidential ticket is the placeholder")
	})
	t.Run("a person of neither team reads nothing", func(t *testing.T) {
		stranger, err := f.Person(ctx, uniqueSlug("stranger"), "Stranger")
		require.NoError(t, err)
		assert.Empty(t, relationsOf(as(stranger), t, c.B, c.CB, store.RelationParent))
	})
	t.Run("a deleted parent is absent", func(t *testing.T) {
		require.NoError(t, f.Exec(ctx, "UPDATE tickets SET deleted_at = now(), deleted_by = $1 WHERE id = $2", c.AdminA, c.PA))
		_, ok := parentOf(t, as(c.Both), c.B, c.CB)
		assert.False(t, ok)
	})
}

// A canonical key is read only where the caller reads the ticket; every other
// key answers as one that does not exist (docs/adr/0008 D2).
func TestAReadableTicketIsOneTheCallerReads(t *testing.T) {
	ctx := context.Background()
	c := newCrossing(t)
	f := fixtures(t)
	db := openRuntime(t)
	readable := func(caller context.Context, team uuid.UUID, slug, project string, number int32) bool {
		var ok bool
		require.NoError(t, db.InTenant(caller, team, func(r *store.Reader) error {
			var err error
			_, ok, err = r.ReadableTicket(caller, slug, project, number)
			return err
		}))
		return ok
	}
	assert.True(t, readable(as(c.Both), c.B, c.SlugA, "ALPHA", 1), "a member of both reads A's ticket")
	assert.False(t, readable(as(c.MemberB), c.B, c.SlugA, "ALPHA", 1), "a member of B alone does not")
	assert.False(t, readable(as(c.Both), c.B, c.SlugA, "ALPHA", 3), "nor a confidential one they are not admitted to")
	assert.True(t, readable(as(c.AdminA), c.A, c.SlugA, "ALPHA", 3), "its team's administrator reads it")
	assert.False(t, readable(as(c.Both), c.B, c.SlugA, "GAMMA", 1), "nor one of a project restricted from them")
	assert.False(t, readable(as(c.Both), c.B, c.SlugA, "ALPHA", 99), "nor one that does not exist")
	assert.False(t, readable(as(c.Both), c.B, "no-such-team", "ALPHA", 1), "nor one of no team")
	teamToken := store.WithCaller(context.Background(), store.Caller{UserID: c.Both, RestrictedTenantID: c.B,
		RequestID: uuid.Must(uuid.NewV7())})
	assert.False(t, readable(teamToken, c.B, c.SlugA, "ALPHA", 1), "nor through a token restricted to another team")
	require.NoError(t, f.Exec(ctx, "UPDATE tickets SET deleted_at = now(), deleted_by = $1 WHERE id = $2", c.AdminA, c.PA))
	assert.False(t, readable(as(c.Both), c.B, c.SlugA, "ALPHA", 1), "nor a deleted one")
}

// The cycle walks cross teams under the graph locks (docs/adr/0008 D2,
// docs/adr/0012 D4).
func TestTheWalksCrossTeams(t *testing.T) {
	ctx := context.Background()
	c := newCrossing(t)
	f := fixtures(t)
	db := openRuntime(t)
	require.NoError(t, f.Exec(ctx, `INSERT INTO ticket_links (tenant_id, type, source_id, target_id, created_by)
		VALUES ($1, 'blocks', $2, $3, $4)`, c.A, c.PA, c.CB, c.MemberA))
	var parentCycle, blocksCycle, noCycle bool
	_, err := db.Mutate(as(c.Both), c.A, func(w *store.Writer) error {
		_, err := w.ParentChainReaches(ctx, c.CB, c.PA)
		require.Error(t, err, "a walk takes its graph's lock first")
		require.NoError(t, w.LockGraph(ctx, store.GraphParents))
		// PA as a child of CB, whose parent PA is: a cycle through B.
		if parentCycle, err = w.ParentChainReaches(ctx, c.CB, c.PA); err != nil {
			return err
		}
		if noCycle, err = w.ParentChainReaches(ctx, c.CB2, c.PA); err != nil {
			return err
		}
		w.Record(store.Event{EntityType: "seed", Action: "created"})
		return nil
	})
	require.NoError(t, err)
	assert.True(t, parentCycle)
	assert.False(t, noCycle)

	_, err = db.Mutate(as(c.Both), c.B, func(w *store.Writer) error {
		require.NoError(t, w.LockGraph(ctx, store.GraphBlocks))
		require.Error(t, w.LockGraph(ctx, store.GraphParents), "the parents' lock comes before the blocks', never after it")
		// CB blocks PA, while PA blocks CB: a cycle through A, from B.
		var err error
		if blocksCycle, err = w.BlocksReach(ctx, c.PA, c.CB); err != nil {
			return err
		}
		_, err = w.BlocksReach(ctx, c.CB, c.PA)
		require.Error(t, err, "the near end of a new link is a ticket of the transaction's team")
		w.Record(store.Event{EntityType: "seed", Action: "created"})
		return nil
	})
	require.Error(t, err, "the refused walk failed the transaction")
	assert.True(t, blocksCycle)
}

// The derived progress counts a child of another team, written through the
// crossing, the parent's version unchanged (docs/adr/0017 D3).
func TestRefreshDerivedCountsAChildOfAnotherTeam(t *testing.T) {
	ctx := context.Background()
	c := newCrossing(t)
	f := fixtures(t)
	db := openRuntime(t)
	require.NoError(t, f.Exec(ctx, "UPDATE tickets SET progress = 50 WHERE id = $1", c.CA))
	require.NoError(t, f.Exec(ctx, "UPDATE tickets SET progress = 100 WHERE id = $1", c.CB))
	var before int32
	require.NoError(t, f.QueryRow(ctx, "SELECT version FROM tickets WHERE id = $1", c.PA).Scan(&before))
	_, err := db.Mutate(as(c.MemberB), c.B, func(w *store.Writer) error {
		if err := w.RefreshDerived(ctx, &c.PA); err != nil {
			return err
		}
		w.Record(store.Event{EntityType: "seed", Action: "created"})
		return nil
	})
	require.NoError(t, err)
	var derived *int16
	var version int32
	require.NoError(t, f.QueryRow(ctx, "SELECT progress_derived, version FROM tickets WHERE id = $1", c.PA).Scan(&derived, &version))
	require.NotNil(t, derived)
	assert.EqualValues(t, 75, *derived, "the mean of 50 and 100, both effort S")
	assert.Equal(t, before, version)
}

// Ending a team's relations — what the deletion of a team does first — leaves
// the other team's children as roots, the team's own tickets parentless, the
// links between them gone and the other team's parents derived again, each
// change recorded in the other team's record as system:team-deletion
// (docs/adr/0024 D6 as made concrete 2026-10-10). No route calls it.
func TestEndingATeamsRelations(t *testing.T) {
	ctx := context.Background()
	c := newCrossing(t)
	f := fixtures(t)
	db := openRuntime(t)
	parentB, _, err := f.Ticket(ctx, c.B, c.ProjectB, c.MemberB, "a parent of B over a child of A")
	require.NoError(t, err)
	require.NoError(t, f.Exec(ctx, "UPDATE tickets SET parent_id = $1, progress = 60 WHERE id = $2", parentB, c.CA))
	require.NoError(t, f.Exec(ctx, "UPDATE tickets SET progress_derived = 60, progress_refinement_derived = 0, progress_review_derived = 0 WHERE id = $1", parentB))
	linkB, _, err := f.Ticket(ctx, c.B, c.ProjectB, c.MemberB, "a ticket of B linked both ways")
	require.NoError(t, err)
	require.NoError(t, f.Exec(ctx, `INSERT INTO ticket_links (tenant_id, type, source_id, target_id, created_by)
		VALUES ($1, 'blocks', $2, $3, $4), ($5, 'found-in', $6, $7, $8)`,
		c.B, linkB, c.PA, c.MemberB, c.A, c.PA, linkB, c.MemberA))

	ran, err := db.EndTeamRelations(ctx, c.A)
	require.NoError(t, err)
	require.True(t, ran)

	for _, child := range []uuid.UUID{c.CB, c.CB2, c.CB3, c.CA} {
		var parent *uuid.UUID
		require.NoError(t, f.QueryRow(ctx, "SELECT parent_id FROM tickets WHERE id = $1", child).Scan(&parent))
		assert.Nil(t, parent, "%s is a root", child)
	}
	links, err := f.QueryCount(ctx, "SELECT count(*) FROM ticket_links WHERE source_id = $1 OR target_id = $1", linkB)
	require.NoError(t, err)
	assert.Zero(t, links)
	var derived *int16
	require.NoError(t, f.QueryRow(ctx, "SELECT progress_derived FROM tickets WHERE id = $1", parentB).Scan(&derived))
	assert.Nil(t, derived, "the parent of B counts the child of A no more")
	for ticket, action := range map[uuid.UUID]string{c.CB: "updated", c.CB2: "updated", c.CB3: "updated", linkB: "unlinked"} {
		n, err := f.QueryCount(ctx, `SELECT count(*) FROM audit_events
			WHERE tenant_id = $1 AND ticket_id = $2 AND action = $3 AND actor_system = 'system:team-deletion'`, c.B, ticket, action)
		require.NoError(t, err)
		assert.Positive(t, n, "the act %s on %s is in B's record", action, ticket)
	}
	outside, err := f.QueryCount(ctx, `SELECT count(*) FROM audit_events WHERE tenant_id IS DISTINCT FROM $1
		AND actor_system = 'system:team-deletion' AND ticket_id IN ($2, $3)`, c.B, c.CB, linkB)
	require.NoError(t, err)
	assert.Zero(t, outside, "nothing of it is recorded outside B")
}

// cowork serve refuses crossings that are not the owner role's alone: a policy
// of a crossing that names another role, a crossing function another role
// owns (docs/adr/0021 D7 as made concrete 2026-10-10). A database migrated
// from scratch passes.
func TestServeRefusesACrossingOfAnotherOwner(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	name := fmt.Sprintf("cowork_it_cross_%d", time.Now().UnixNano())
	require.NoError(t, createDatabase(ctx, env.AdminURL, name))
	t.Cleanup(func() { _ = dropDatabase(context.Background(), env.AdminURL, name) })
	ownerURL, err := withUserAndDatabase(env.AdminURL, ownerRole, ownerRole, name)
	require.NoError(t, err)
	runtimeURL, err := withUserAndDatabase(env.AdminURL, runtimeRole, runtimeRole, name)
	require.NoError(t, err)
	adminURL, err := withUserAndDatabase(env.AdminURL, "", "", name)
	require.NoError(t, err)
	_, err = store.Migrate(ctx, ownerURL, runtimeRole)
	require.NoError(t, err)
	db := openStore(t, runtimeURL)
	require.NoError(t, db.CheckCrossing(ctx), "a database migrated from scratch")

	admin, err := pgx.Connect(ctx, adminURL)
	require.NoError(t, err)
	defer func() { _ = admin.Close(ctx) }()
	_, err = admin.Exec(ctx, `DO $$ BEGIN
		IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'cowork_it_other') THEN CREATE ROLE cowork_it_other NOLOGIN; END IF;
	END $$`)
	require.NoError(t, err)
	_, err = admin.Exec(ctx, "ALTER POLICY tickets_crossing_read ON tickets TO cowork_it_other")
	require.NoError(t, err)
	err = db.CheckCrossing(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tickets_crossing_read names another role")

	_, err = admin.Exec(ctx, fmt.Sprintf("ALTER POLICY tickets_crossing_read ON tickets TO %s", ownerRole))
	require.NoError(t, err)
	require.NoError(t, db.CheckCrossing(ctx))
	_, err = admin.Exec(ctx, "ALTER FUNCTION relation_heads(uuid[], text[]) OWNER TO cowork_it_other")
	require.NoError(t, err)
	err = db.CheckCrossing(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "relation_heads is not the owner's")
}

// The probe the mechanism rests on, kept: PostgreSQL refuses a custom setting
// in a function's SET clause to the owner role, which is no superuser — so the
// crossings set app.crossing in their bodies (docs/adr/0021 D7 as made
// concrete 2026-10-10; verified on PostgreSQL 18.6).
func TestPostgreSQLRefusesTheCrossingInASetClause(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	conn, err := pgx.Connect(ctx, env.OwnerURL)
	require.NoError(t, err)
	defer func() { _ = conn.Close(ctx) }()
	tx, err := conn.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `CREATE FUNCTION crossing_probe() RETURNS text LANGUAGE sql SECURITY DEFINER
		SET app.crossing = 'head' AS $$ SELECT current_setting('app.crossing', true) $$`)
	var pgErr *pgconn.PgError
	require.True(t, errors.As(err, &pgErr), "the owner role puts no custom setting into a SET clause: %v", err)
	assert.Equal(t, "42501", pgErr.Code)
	assert.Contains(t, pgErr.Message, `permission denied to set parameter "app.crossing"`)
}
