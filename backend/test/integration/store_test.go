//go:build integration

package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/store"
	"github.com/guided-traffic/cowork/backend/internal/store/writeq"
	"github.com/guided-traffic/cowork/backend/test/fixture"
)

// The runtime role cannot see past row-level security, and `serve` refuses
// one that could (docs/adr/0021 D2): the owner role owns the relations and
// the administrator is a superuser.
func TestRuntimeRoleCheck(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	require.NoError(t, openRuntime(t).CheckRuntimeRole(ctx))

	err := openStore(t, env.OwnerURL).CheckRuntimeRole(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "owns relations")

	err = openStore(t, env.AdminURL).CheckRuntimeRole(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "superuser")
}

// A member of the owner role inherits its ownership and is refused as well.
func TestRuntimeRoleThatIsMemberOfTheOwnerIsRefused(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	f := fixtures(t)
	require.NoError(t, f.Exec(ctx, `DO $$ BEGIN
		IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'cowork_it_member_of_owner') THEN
			CREATE ROLE cowork_it_member_of_owner LOGIN PASSWORD 'cowork_it_member_of_owner';
		END IF; END $$`))
	require.NoError(t, f.Exec(ctx, "GRANT "+ownerRole+" TO cowork_it_member_of_owner"))
	t.Cleanup(func() { _ = f.Exec(context.Background(), "REVOKE "+ownerRole+" FROM cowork_it_member_of_owner") })

	memberURL, err := withUserAndDatabase(env.AdminURL, "cowork_it_member_of_owner", "cowork_it_member_of_owner", databaseName(t))
	require.NoError(t, err)
	err = openStore(t, memberURL).CheckRuntimeRole(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "member of the owner role")

	_, err = store.Migrate(ctx, env.OwnerURL, "cowork_it_member_of_owner")
	require.Error(t, err, "migrate refuses such a runtime role as well")
}

// The plan's proof: a deliberately unfiltered query under tenant A returns
// nothing of tenant B, as the runtime role, on every table that carries a
// tenant_id — found by walking the catalog, so a new table is covered the day
// it is created (docs/adr/0021 Consequences). The seed must give every such
// table a row of tenant B, or the test says the table is not covered.
func TestUnfilteredQueryUnderTenantSeesNothingOfAnother(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	w := newWorld(t)
	seedEveryTenantTable(t, w)
	f := fixtures(t)

	tables := tenantBoundTables(t)
	require.NotEmpty(t, tables)

	conn, err := pgx.Connect(ctx, env.RuntimeURL)
	require.NoError(t, err)
	defer func() { _ = conn.Close(ctx) }()
	tx, err := conn.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true), set_config('app.user_id', $2, true)",
		w.A.String(), w.MemberA.String())
	require.NoError(t, err)

	for _, table := range tables {
		t.Run(table, func(t *testing.T) {
			ofB, err := f.QueryCount(ctx, "SELECT count(*) FROM "+table+" WHERE tenant_id = $1", w.B)
			require.NoError(t, err)
			require.Positive(t, ofB, "the seed gives %s no row of tenant B, so the test proves nothing for it", table)

			var seen int64
			require.NoError(t, tx.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE tenant_id <> $1", w.A).Scan(&seen))
			assert.Zero(t, seen, "rows of another tenant are visible in %s", table)
		})
	}

	var tenants int64
	require.NoError(t, tx.QueryRow(ctx, "SELECT count(*) FROM tenants WHERE id <> $1", w.A).Scan(&tenants))
	assert.Zero(t, tenants, "a member of A alone sees no other tenant")
	var strangers int64
	require.NoError(t, tx.QueryRow(ctx, "SELECT count(*) FROM users WHERE id = $1", w.MemberB).Scan(&strangers))
	assert.Zero(t, strangers, "a person of B alone is not visible in A")

	// The person-scoped tables answer the person's own rows and nothing else
	// (docs/adr/0021 D6).
	require.NoError(t, f.Exec(ctx, `INSERT INTO audit_events (actor_user_id, entity_type, action)
		VALUES ($1, 'token', 'refused'), ($2, 'token', 'refused')`, w.MemberA, w.MemberB))
	for _, c := range []struct{ name, others, own string }{
		{"tokens", "SELECT count(*) FROM tokens WHERE user_id <> $1", "SELECT count(*) FROM tokens WHERE user_id = $1"},
		{"idempotency keys", "SELECT count(*) FROM idempotency_keys WHERE user_id <> $1", "SELECT count(*) FROM idempotency_keys WHERE user_id = $1"},
		{"memberships", "SELECT count(*) FROM memberships WHERE user_id <> $1 AND tenant_id <> '" + w.A.String() + "'", "SELECT count(*) FROM memberships WHERE user_id = $1"},
		{"installation-level audit rows", "SELECT count(*) FROM audit_events WHERE tenant_id IS NULL AND actor_user_id <> $1",
			"SELECT count(*) FROM audit_events WHERE tenant_id IS NULL AND actor_user_id = $1"},
	} {
		var others, own int64
		require.NoError(t, tx.QueryRow(ctx, c.others, w.MemberA).Scan(&others), c.name)
		require.NoError(t, tx.QueryRow(ctx, c.own, w.MemberA).Scan(&own), c.name)
		assert.Zero(t, others, "%s: no row of another person", c.name)
		assert.Positive(t, own, "%s: the person's own rows", c.name)
	}
}

// Without a tenant, the tenant-bound tables admit nothing — not even on a
// pooled connection that just served a tenant (docs/adr/0021 D3).
func TestTheContextDiesWithItsTransaction(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	w := newWorld(t)

	conn, err := pgx.Connect(ctx, env.RuntimeURL)
	require.NoError(t, err)
	defer func() { _ = conn.Close(ctx) }()
	tx, err := conn.Begin(ctx)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", w.A.String())
	require.NoError(t, err)
	var n int64
	require.NoError(t, tx.QueryRow(ctx, "SELECT count(*) FROM projects").Scan(&n))
	require.Positive(t, n)
	require.NoError(t, tx.Commit(ctx))

	require.NoError(t, conn.QueryRow(ctx, "SELECT count(*) FROM projects").Scan(&n),
		"an ended transaction's setting reads '' and the guarded policy matches nothing instead of raising")
	assert.Zero(t, n)
}

func TestInTenantAndInstallation(t *testing.T) {
	ctx := as(uuid.Nil)
	w := newWorld(t)
	db := openRuntime(t)

	err := db.InTenant(as(w.MemberA), w.A, func(r *store.Reader) error {
		got, err := r.GetTenant(ctx, w.A)
		require.NoError(t, err)
		assert.Equal(t, w.SlugA, got.Slug)
		_, err = r.GetTenant(ctx, w.B)
		assert.ErrorIs(t, err, pgx.ErrNoRows, "another tenant is invisible inside A")
		return nil
	})
	require.NoError(t, err)

	err = db.Installation(as(w.MemberB), func(r *store.Reader) error {
		_, err := r.GetTenantForPerson(ctx, readTenantParams(w.SlugA, w.MemberB))
		assert.ErrorIs(t, err, pgx.ErrNoRows, "the boundary check finds no membership")
		got, err := r.GetTenantForPerson(ctx, readTenantParams(w.SlugB, w.MemberB))
		require.NoError(t, err)
		assert.Equal(t, w.B, got.ID)
		mine, err := r.ListMembershipsOfUser(ctx, w.MemberB)
		require.NoError(t, err)
		assert.Len(t, mine, 1)
		return nil
	})
	require.NoError(t, err)

	err = db.Installation(as(w.Both), func(r *store.Reader) error {
		mine, err := r.ListMembershipsOfUser(ctx, w.Both)
		require.NoError(t, err)
		assert.Len(t, mine, 2, "the person in both tenants sees both memberships")
		return nil
	})
	require.NoError(t, err)
}

// audit_events is append-only by grant (docs/adr/0026 D3): the runtime role
// may insert and read, and nothing else.
func TestTheAuditRecordIsAppendOnly(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	w := newWorld(t)
	seedEveryTenantTable(t, w)

	conn, err := pgx.Connect(ctx, env.RuntimeURL)
	require.NoError(t, err)
	defer func() { _ = conn.Close(ctx) }()
	inA := func(t *testing.T) pgx.Tx {
		tx, err := conn.Begin(ctx)
		require.NoError(t, err)
		t.Cleanup(func() { _ = tx.Rollback(ctx) })
		_, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", w.A.String())
		require.NoError(t, err)
		return tx
	}
	for _, stmt := range []string{
		"UPDATE audit_events SET note = 'rewritten'",
		"DELETE FROM audit_events",
		"TRUNCATE audit_events",
		"ALTER TABLE audit_events DISABLE ROW LEVEL SECURITY",
		"ALTER TABLE audit_events NO FORCE ROW LEVEL SECURITY",
	} {
		t.Run(stmt, func(t *testing.T) {
			_, err := inA(t).Exec(ctx, stmt)
			assertInsufficientPrivilege(t, err)
		})
	}
	t.Run("a grant to itself grants nothing", func(t *testing.T) {
		tx := inA(t)
		_, err := tx.Exec(ctx, "GRANT UPDATE ON audit_events TO "+runtimeRole)
		require.NoError(t, err, "PostgreSQL warns instead of refusing a grant without grant option")
		_, err = tx.Exec(ctx, "UPDATE audit_events SET note = 'rewritten'")
		assertInsufficientPrivilege(t, err)
	})
}

// assertInsufficientPrivilege expects SQLSTATE 42501.
func assertInsufficientPrivilege(t *testing.T, err error) {
	t.Helper()
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	assert.Equal(t, "42501", pgErr.Code, pgErr.Message)
}

func TestMutateWritesTheActWithItsCallersFacts(t *testing.T) {
	w := newWorld(t)
	db := openRuntime(t)
	f := fixtures(t)
	_, tokenID, err := f.Token(context.Background(), fixture.TokenSpec{UserID: w.AdminA, Agent: true})
	require.NoError(t, err)
	requestID := uuid.Must(uuid.NewV7())
	ctx := store.WithCaller(context.Background(), store.Caller{
		UserID: w.AdminA, TokenID: tokenID, Agent: "claude-code/opus/s1",
		Capabilities: []string{"decide", "close"}, RequestID: requestID,
	})

	_, err = db.Mutate(ctx, w.A, func(wr *store.Writer) error {
		got, err := wr.GetTenant(ctx, w.A)
		if err != nil {
			return err
		}
		if _, err := wr.UpdateTenantSettings(ctx, writeq.UpdateTenantSettingsParams{
			TenantID: w.A, Version: got.Version, Name: "Renamed", MembersCreateProjects: true,
		}); err != nil {
			return err
		}
		wr.Record(store.Event{EntityType: "tenant", EntityID: w.A, Action: "updated",
			Before: map[string]string{"name": got.Name}, After: map[string]string{"name": "Renamed"}})
		return nil
	})
	require.NoError(t, err)

	var (
		actor, token, request uuid.UUID
		agent                 string
		caps                  []string
		after                 []byte
	)
	require.NoError(t, fixtures(t).QueryRow(context.Background(),
		`SELECT actor_user_id, token_id, request_id, agent, agent_capabilities, after
		 FROM audit_events WHERE tenant_id = $1 AND action = 'updated' AND entity_id = $1`, w.A).
		Scan(&actor, &token, &request, &agent, &caps, &after))
	assert.Equal(t, w.AdminA, actor)
	assert.Equal(t, tokenID, token)
	assert.Equal(t, requestID, request)
	assert.Equal(t, "claude-code/opus/s1", agent)
	assert.Equal(t, []string{"decide", "close"}, caps)
	assert.JSONEq(t, `{"name":"Renamed"}`, string(after))
}

func TestMutateCommitsNothingWithoutAnActOrOnError(t *testing.T) {
	w := newWorld(t)
	db := openRuntime(t)
	ctx := as(w.AdminA)
	rename := func(wr *store.Writer, name string) error {
		got, err := wr.GetTenant(ctx, w.A)
		if err != nil {
			return err
		}
		_, err = wr.UpdateTenantSettings(ctx, writeq.UpdateTenantSettingsParams{
			TenantID: w.A, Version: got.Version, Name: name, MembersCreateProjects: true})
		return err
	}

	_, err := db.Mutate(ctx, w.A, func(wr *store.Writer) error { return rename(wr, "No act") })
	assert.ErrorIs(t, err, store.ErrNoAct)

	boom := errors.New("boom")
	_, err = db.Mutate(ctx, w.A, func(wr *store.Writer) error {
		if err := rename(wr, "Failed"); err != nil {
			return err
		}
		wr.Record(store.Event{EntityType: "tenant", EntityID: w.A, Action: "updated"})
		return boom
	})
	assert.ErrorIs(t, err, boom)

	_, err = db.Mutate(ctx, w.A, func(*store.Writer) error { return store.ErrNoChange })
	assert.ErrorIs(t, err, store.ErrNoChange)

	f := fixtures(t)
	n, err := f.QueryCount(context.Background(), "SELECT count(*) FROM audit_events WHERE tenant_id = $1", w.A)
	require.NoError(t, err)
	assert.Zero(t, n, "no act was committed")
	n, err = f.QueryCount(context.Background(), "SELECT count(*) FROM tenants WHERE id = $1 AND name = 'Tenant A'", w.A)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n, "the tenant keeps its name")
}

// docs/adr/0045 D4: the same key with the same request replays the stored
// response without repeating the act; with a different request it is a
// mismatch; concurrent duplicates commit one act.
func TestMutateIdempotency(t *testing.T) {
	w := newWorld(t)
	db := openRuntime(t)
	f := fixtures(t)
	_, tokenID, err := f.Token(context.Background(), fixture.TokenSpec{UserID: w.MemberA})
	require.NoError(t, err)
	base := store.WithCaller(context.Background(), store.Caller{UserID: w.MemberA, TokenID: tokenID})
	key := uuid.Must(uuid.NewV7())
	keyed := func(fingerprint string) context.Context {
		return store.WithIdempotency(base, store.Idempotency{Key: key, Fingerprint: sha256.Sum256([]byte(fingerprint))})
	}
	runs := 0
	act := func(wr *store.Writer) error {
		runs++
		wr.Record(store.Event{EntityType: "probe", Action: "created"})
		wr.Respond(store.Result{Status: 201, Headers: map[string]string{"ETag": `"1"`}, Body: []byte(`{"n":1}`)})
		return nil
	}

	replay, err := db.Mutate(keyed("request one"), w.A, act)
	require.NoError(t, err)
	assert.Nil(t, replay, "the first request executes")

	replay, err = db.Mutate(keyed("request one"), w.A, act)
	require.NoError(t, err)
	require.NotNil(t, replay, "the repetition replays")
	assert.Equal(t, 201, replay.Status)
	assert.Equal(t, `"1"`, replay.Headers["ETag"])
	assert.JSONEq(t, `{"n":1}`, string(replay.Body))
	assert.Equal(t, 1, runs, "the act ran once")

	_, err = db.Mutate(keyed("request two"), w.A, act)
	assert.ErrorIs(t, err, store.ErrIdempotencyMismatch)

	n, err := f.QueryCount(context.Background(), "SELECT count(*) FROM audit_events WHERE idempotency_key = $1", key)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n, "one act carries the key (docs/adr/0045 D7)")

	// Concurrent duplicates with a fresh key: exactly one act commits.
	concurrent := uuid.Must(uuid.NewV7())
	var wg sync.WaitGroup
	errs := make([]error, 6)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx := store.WithIdempotency(base, store.Idempotency{Key: concurrent, Fingerprint: sha256.Sum256([]byte("same"))})
			_, errs[i] = db.Mutate(ctx, w.A, func(wr *store.Writer) error {
				wr.Record(store.Event{EntityType: "probe", Action: "created"})
				wr.Respond(store.Result{Status: 201, Body: []byte(`{}`)})
				return nil
			})
		}()
	}
	wg.Wait()
	for _, err := range errs {
		assert.NoError(t, err)
	}
	n, err = f.QueryCount(context.Background(), "SELECT count(*) FROM audit_events WHERE idempotency_key = $1", concurrent)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n, "concurrent duplicates commit one act")
}

// docs/adr/0045 D4: expired keys are removed by a job with a system actor,
// under a transaction-level advisory lock (docs/adr/0027 D5).
func TestIdempotencyExpiryJob(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	w := newWorld(t)
	db := openRuntime(t)
	f := fixtures(t)
	_, tokenID, err := f.Token(ctx, fixture.TokenSpec{UserID: w.MemberA})
	require.NoError(t, err)
	require.NoError(t, f.Exec(ctx, `INSERT INTO idempotency_keys
		(token_id, user_id, tenant_id, key, fingerprint, response_status, response_body, expires_at)
		VALUES ($1, $2, $3, $4, $5, 201, '\x', now() - interval '1 second')`,
		tokenID, w.MemberA, w.A, uuid.Must(uuid.NewV7()), make([]byte, 32)))

	// Another replica holds the job's lock: this one skips.
	lockConn, err := pgx.Connect(ctx, env.AdminURL)
	require.NoError(t, err)
	defer func() { _ = lockConn.Close(ctx) }()
	lockTx, err := lockConn.Begin(ctx)
	require.NoError(t, err)
	_, err = lockTx.Exec(ctx, "SELECT pg_advisory_xact_lock($1, $2)", int32(0x636f776b), int32(1))
	require.NoError(t, err)
	ran, err := db.RunJob(ctx, "idempotency-expiry", 1, func(*store.Writer) error { return nil })
	require.NoError(t, err)
	assert.False(t, ran, "a held lock skips the run")
	require.NoError(t, lockTx.Rollback(ctx))

	removed, err := db.ExpireIdempotencyKeys(ctx)
	require.NoError(t, err)
	assert.Positive(t, removed)
	n, err := f.QueryCount(ctx, "SELECT count(*) FROM idempotency_keys WHERE expires_at <= now()")
	require.NoError(t, err)
	assert.Zero(t, n)
	n, err = f.QueryCount(ctx, `SELECT count(*) FROM audit_events
		WHERE actor_system = 'system:idempotency-expiry' AND actor_user_id IS NULL AND action = 'expired'`)
	require.NoError(t, err)
	assert.Positive(t, n, "the job's act names the system actor")

	ran, err = db.RunJob(ctx, "idempotency-expiry", 1, func(*store.Writer) error { return nil })
	require.NoError(t, err)
	assert.True(t, ran, "the transaction-level lock was released with the job's transaction")
}

// The resolver's lookup sees exactly the row whose secret the caller holds
// (docs/adr/0021 D6, docs/adr/0035 D1).
func TestTokenLookupSeesThePresentedRowOnly(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	w := newWorld(t)
	db := openRuntime(t)
	f := fixtures(t)
	plaintext, id, err := f.Token(ctx, fixture.TokenSpec{UserID: w.MemberA, Name: "mine"})
	require.NoError(t, err)
	_, _, err = f.Token(ctx, fixture.TokenSpec{UserID: w.MemberB, Name: "theirs"})
	require.NoError(t, err)

	rec, err := db.LookupToken(ctx, fixture.TokenHash(plaintext))
	require.NoError(t, err)
	assert.Equal(t, id, rec.Token.ID)
	assert.Equal(t, w.MemberA, rec.Person.ID)

	_, err = db.LookupToken(ctx, sha256.Sum256([]byte("cwk_not-a-token")))
	assert.ErrorIs(t, err, store.ErrNotFound)

	conn, err := pgx.Connect(ctx, env.RuntimeURL)
	require.NoError(t, err)
	defer func() { _ = conn.Close(ctx) }()
	hash := fixture.TokenHash(plaintext)
	tx, err := conn.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, "SELECT set_config('app.token_hash', $1, true)", hex.EncodeToString(hash[:]))
	require.NoError(t, err)
	var visible int64
	require.NoError(t, tx.QueryRow(ctx, "SELECT count(*) FROM tokens").Scan(&visible))
	assert.EqualValues(t, 1, visible, "an unfiltered read of tokens shows the presented row alone")
}

// docs/adr/0035 D9 as amended: a refused use is recorded at most once per
// token, reason and hour.
// docs/adr/0035 D6: a revocation is final even for the runtime role, which
// may write revoked_at — a compromised serving process cannot bring a leaked
// token back.
func TestRevocationIsFinal(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	f := fixtures(t)
	_, id, err := f.Token(ctx, fixture.TokenSpec{UserID: w.MemberA, Revoked: true})
	require.NoError(t, err)

	conn, err := pgx.Connect(ctx, env.RuntimeURL)
	require.NoError(t, err)
	defer func() { _ = conn.Close(ctx) }()
	tx, err := conn.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, "SELECT set_config('app.user_id', $1, true)", w.MemberA.String())
	require.NoError(t, err)
	_, err = tx.Exec(ctx, "UPDATE tokens SET revoked_at = NULL, revoked_by = NULL WHERE id = $1", id)
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	assert.Equal(t, "23514", pgErr.Code, "a revoked token stays revoked")
}

func TestTokenRefusalsAreBounded(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	w := newWorld(t)
	db := openRuntime(t)
	f := fixtures(t)
	plaintext, id, err := f.Token(ctx, fixture.TokenSpec{UserID: w.MemberA, Revoked: true})
	require.NoError(t, err)
	rec, err := db.LookupToken(ctx, fixture.TokenHash(plaintext))
	require.NoError(t, err)

	for range 5 {
		require.NoError(t, db.RecordTokenRefusal(ctx, rec, "revoked", uuid.Must(uuid.NewV7())))
	}
	require.NoError(t, db.RecordTokenRefusal(ctx, rec, "expired", uuid.Must(uuid.NewV7())))
	n, err := f.QueryCount(ctx, `SELECT count(*) FROM audit_events WHERE token_id = $1 AND action = 'refused'`, id)
	require.NoError(t, err)
	assert.EqualValues(t, 2, n, "one row per reason within the hour")
}

func TestTouchTokenLastUsedOncePerDay(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	w := newWorld(t)
	db := openRuntime(t)
	f := fixtures(t)
	_, id, err := f.Token(ctx, fixture.TokenSpec{UserID: w.MemberA})
	require.NoError(t, err)
	day := time.Date(2026, 10, 2, 23, 30, 0, 0, time.FixedZone("CEST", 2*3600))
	require.NoError(t, db.TouchTokenLastUsed(ctx, w.MemberA, id, day))
	n, err := f.QueryCount(ctx, "SELECT count(*) FROM tokens WHERE id = $1 AND last_used_on = '2026-10-02'", id)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n, "the date is the UTC date")
	n, err = f.QueryCount(ctx, "SELECT count(*) FROM audit_events WHERE token_id = $1", id)
	require.NoError(t, err)
	assert.Zero(t, n, "bookkeeping writes no act")
}
