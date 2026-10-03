//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/store"
)

// The policies of migrations 15 and 16, asked directly: a statement as the
// runtime role with the settings a request would carry, and what row-level
// security — the second line behind the handlers (docs/adr/0021) — says to it.

// settings are the transaction-local settings of a context.
type settings map[string]string

func ctxOf(user, tenant uuid.UUID) settings {
	s := settings{}
	if user != uuid.Nil {
		s["app.user_id"] = user.String()
	}
	if tenant != uuid.Nil {
		s["app.tenant_id"] = tenant.String()
	}
	return s
}

// run executes one statement in a transaction of its own as the runtime role
// with the settings, rolls it back, and returns the rows it affected or the
// error.
func run(t *testing.T, s settings, sql string, args ...any) (int64, error) {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, env.RuntimeURL)
	require.NoError(t, err)
	defer func() { _ = conn.Close(ctx) }()
	tx, err := conn.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	for k, v := range s {
		_, err := tx.Exec(ctx, "SELECT set_config($1, $2, true)", k, v)
		require.NoError(t, err)
	}
	tag, err := tx.Exec(ctx, sql, args...)
	return tag.RowsAffected(), err
}

func count(t *testing.T, s settings, sql string, args ...any) int64 {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, env.RuntimeURL)
	require.NoError(t, err)
	defer func() { _ = conn.Close(ctx) }()
	tx, err := conn.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	for k, v := range s {
		_, err := tx.Exec(ctx, "SELECT set_config($1, $2, true)", k, v)
		require.NoError(t, err)
	}
	var n int64
	require.NoError(t, tx.QueryRow(ctx, sql, args...).Scan(&n))
	return n
}

// denied says the statement was refused by a policy (row-level security) or by
// a grant, as the text of the error tells.
func denied(t *testing.T, s settings, sql string, args ...any) {
	t.Helper()
	_, err := run(t, s, sql, args...)
	var pgErr *pgconn.PgError
	require.True(t, errors.As(err, &pgErr), "refused: %s (got %v)", sql, err)
	assert.Equal(t, "42501", pgErr.Code, sql)
}

func affects(t *testing.T, want int64, s settings, sql string, args ...any) {
	t.Helper()
	n, err := run(t, s, sql, args...)
	require.NoError(t, err, sql)
	assert.Equal(t, want, n, sql)
}

func TestPoliciesOfThePersonsAndTheirAccounts(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	f := fixtures(t)
	names := withAccounts(t, w)
	boss, err := f.Person(ctx, uniqueSlug("boss"), "Boss")
	require.NoError(t, err)
	require.NoError(t, f.GlobalAdmin(ctx, boss))
	require.NoError(t, f.Account(ctx, boss, testPassword, uuid.Nil, false))
	adminB, err := f.Person(ctx, uniqueSlug("admin-b"), "Admin B")
	require.NoError(t, err)
	require.NoError(t, f.Member(ctx, w.B, adminB, "admin"))
	_, tokenID, err := f.Token(ctx, fixtureSpec(w.MemberA))
	require.NoError(t, err)

	adminA := ctxOf(w.AdminA, w.A)
	memberA := ctxOf(w.MemberA, w.A)
	asAdminB := ctxOf(adminB, w.B)
	global := ctxOf(boss, uuid.Nil)
	login := settings{"app.job": "login"}
	bootstrapJob := settings{"app.job": "bootstrap"}
	nobody := settings{}

	t.Run("users: only an administrator inserts, never a global administrator, and only the start-up synchronisation makes one", func(t *testing.T) {
		insert := `INSERT INTO users (id, username, display_name, global_admin) VALUES (uuidv7(), $1, 'x', $2)`
		affects(t, 1, adminA, insert, uniqueSlug("pol"), false)
		denied(t, adminA, insert, uniqueSlug("pol"), true)
		denied(t, memberA, insert, uniqueSlug("pol"), false)
		denied(t, nobody, insert, uniqueSlug("pol"), false)
		denied(t, login, insert, uniqueSlug("pol"), false)
		denied(t, global, insert, uniqueSlug("pol"), false)
		affects(t, 1, bootstrapJob, insert, uniqueSlug("pol"), true)
		denied(t, adminA, `UPDATE users SET username = 'renamed' WHERE id = $1`, w.MemberA)
	})

	t.Run("users: an administrator deactivates the accounts their tenant manages and no others", func(t *testing.T) {
		deactivate := `UPDATE users SET deactivated_at = now() WHERE id = $1`
		affects(t, 1, adminA, deactivate, w.MemberA)
		affects(t, 0, adminA, deactivate, w.MemberB)
		affects(t, 0, adminA, deactivate, boss)
		affects(t, 0, asAdminB, deactivate, w.MemberA)
		affects(t, 0, memberA, deactivate, w.ViewerA)
		affects(t, 0, nobody, deactivate, w.MemberA)
		affects(t, 1, bootstrapJob, deactivate, boss)
		denied(t, adminA, `UPDATE users SET global_admin = true WHERE id = $1`, w.MemberA)
	})

	t.Run("local accounts: a person reads their own, the tenant's administrators the ones it manages, the login all", func(t *testing.T) {
		read := `SELECT count(*) FROM local_accounts WHERE user_id = $1`
		assert.EqualValues(t, 1, count(t, ctxOf(w.MemberA, uuid.Nil), read, w.MemberA))
		assert.EqualValues(t, 0, count(t, ctxOf(w.MemberA, uuid.Nil), read, w.ViewerA))
		assert.EqualValues(t, 1, count(t, adminA, read, w.MemberA), "managed by A")
		assert.EqualValues(t, 0, count(t, adminA, read, w.MemberB), "managed by B")
		assert.EqualValues(t, 0, count(t, asAdminB, read, w.MemberA))
		assert.EqualValues(t, 0, count(t, memberA, read, w.ViewerA), "a member is no administrator")
		assert.EqualValues(t, 1, count(t, login, read, w.MemberB))
		assert.EqualValues(t, 0, count(t, nobody, read, w.MemberA))
		assert.EqualValues(t, 0, count(t, global, read, w.MemberA), "a global administrator has no reach into a tenant's accounts")
	})

	t.Run("local accounts: written by the administrators of the managing tenant, by the person for a tenant account, by the synchronisation", func(t *testing.T) {
		set := `UPDATE local_accounts SET password_hash = '$argon2id$v=19$m=19456,t=2,p=1$AAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAA', updated_at = now() WHERE user_id = $1`
		affects(t, 1, adminA, set, w.MemberA)
		affects(t, 0, adminA, set, w.MemberB)
		affects(t, 0, asAdminB, set, w.MemberA)
		affects(t, 0, memberA, set, w.ViewerA)
		affects(t, 1, memberA, set, w.MemberA)
		affects(t, 1, bootstrapJob, set, boss)
		denied(t, ctxOf(boss, uuid.Nil), set, boss) // the configuration's account is not the person's to change
		denied(t, adminA, `UPDATE local_accounts SET managing_tenant_id = $2 WHERE user_id = $1`, w.MemberA, w.B)
		denied(t, adminA, `UPDATE local_accounts SET origin = 'config', managing_tenant_id = NULL WHERE user_id = $1`, w.MemberA)
		insert := `INSERT INTO local_accounts (user_id, password_hash, origin, managing_tenant_id) VALUES ($1, '$argon2id$x', $2, $3)`
		stranger, err := f.Person(ctx, uniqueSlug("stranger"), "Stranger")
		require.NoError(t, err)
		denied(t, adminA, insert, stranger, "tenant", w.B)
		denied(t, adminA, insert, stranger, "config", nil)
		denied(t, memberA, insert, stranger, "tenant", w.A)
		affects(t, 1, adminA, insert, stranger, "tenant", w.A)
		affects(t, 1, bootstrapJob, insert, stranger, "config", nil)
		denied(t, adminA, `UPDATE local_accounts SET user_id = $2 WHERE user_id = $1`, w.MemberA, stranger)
	})

	t.Run("memberships: a marked grant into the administrator's own tenant, the creator's into a new one", func(t *testing.T) {
		grant := `INSERT INTO memberships (tenant_id, user_id, role, source) VALUES ($1, $2, $3, $4)`
		affects(t, 1, adminA, grant, w.A, boss, "viewer", "grant")
		denied(t, adminA, grant, w.B, boss, "viewer", "grant")
		denied(t, adminA, grant, w.A, boss, "viewer", "mapping")
		denied(t, memberA, grant, w.A, boss, "viewer", "grant")
		denied(t, nobody, grant, w.A, boss, "viewer", "grant")
		affects(t, 1, global, grant, w.B, boss, "admin", "grant")
		denied(t, global, grant, w.B, w.MemberA, "admin", "grant")
		denied(t, global, grant, w.B, boss, "member", "grant")
		affects(t, 1, bootstrapJob, grant, w.B, boss, "admin", "grant")
		denied(t, adminA, `UPDATE memberships SET role = 'admin' WHERE user_id = $1`, w.MemberA)
		denied(t, adminA, `DELETE FROM memberships WHERE user_id = $1`, w.MemberA)
	})

	t.Run("tenants: a global administrator or the synchronisation creates one", func(t *testing.T) {
		create := `INSERT INTO tenants (id, slug, name) VALUES (uuidv7(), $1, 'x')`
		affects(t, 1, global, create, uniqueSlug("pol"))
		affects(t, 1, bootstrapJob, create, uniqueSlug("pol"))
		denied(t, adminA, create, uniqueSlug("pol"))
		denied(t, memberA, create, uniqueSlug("pol"))
		denied(t, nobody, create, uniqueSlug("pol"))
		denied(t, login, create, uniqueSlug("pol"))
		assert.EqualValues(t, 0, count(t, nobody, `SELECT count(*) FROM tenants`), "no context reads no tenant")
		assert.Positive(t, count(t, login, `SELECT count(*) FROM tenants`), "the login asks whether one exists")
		denied(t, global, `DELETE FROM tenants WHERE id = $1`, w.A)
	})

	t.Run("tokens: a person makes their own; the administrators of a managed account revoke its tokens", func(t *testing.T) {
		mint := `INSERT INTO tokens (user_id, name, token_hash, scope, expires_at) VALUES ($1, 't', decode(repeat('ab', 32), 'hex'), 'read', now() + interval '1 day')`
		affects(t, 1, ctxOf(w.MemberA, uuid.Nil), mint, w.MemberA)
		denied(t, ctxOf(w.MemberA, uuid.Nil), mint, w.ViewerA)
		denied(t, adminA, mint, w.MemberA) // adminA is not MemberA
		denied(t, nobody, mint, w.MemberA)
		revoke := `UPDATE tokens SET revoked_at = now() WHERE id = $1`
		affects(t, 1, adminA, revoke, tokenID)
		affects(t, 0, asAdminB, revoke, tokenID)
		affects(t, 0, memberA.merge(settings{"app.user_id": w.ViewerA.String()}), revoke, tokenID)
		affects(t, 1, bootstrapJob, revoke, tokenID)
		denied(t, adminA, `UPDATE tokens SET scope = 'admin' WHERE id = $1`, tokenID)
	})

	t.Run("login attempts and locks: the login's, cleared by the administrators of the account", func(t *testing.T) {
		insert := `INSERT INTO login_attempts (username, address, failed, created_at) VALUES ($1, decode(repeat('00', 32), 'hex'), true, now())`
		affects(t, 1, login, insert, names["memberA"])
		denied(t, adminA, insert, names["memberA"])
		denied(t, nobody, insert, names["memberA"])
		lock := `INSERT INTO login_locks (username, locked_at, sticky) VALUES ($1, now(), false)`
		affects(t, 1, login, lock, names["memberA"])
		denied(t, memberA, lock, names["memberA"])
		assert.EqualValues(t, 0, count(t, nobody, `SELECT count(*) FROM login_attempts`))
		assert.EqualValues(t, 0, count(t, memberA, `SELECT count(*) FROM login_locks`))

		// Rows for the administrator's own tenant's accounts, deleted by them.
		f.Exec(ctx, `INSERT INTO login_attempts (username, address, failed, created_at) VALUES ($1, decode(repeat('00', 32), 'hex'), true, now())`, names["memberA"])
		f.Exec(ctx, `INSERT INTO login_attempts (username, address, failed, created_at) VALUES ($1, decode(repeat('00', 32), 'hex'), true, now())`, names["memberB"])
		f.Exec(ctx, `INSERT INTO login_locks (username, locked_at, sticky) VALUES ($1, now(), true) ON CONFLICT DO NOTHING`, names["memberA"])
		assert.EqualValues(t, 1, count(t, adminA, `SELECT count(*) FROM login_locks WHERE username = $1`, names["memberA"]))
		assert.EqualValues(t, 0, count(t, asAdminB, `SELECT count(*) FROM login_locks WHERE username = $1`, names["memberA"]))
		deleteFailed := `DELETE FROM login_attempts WHERE username = $1`
		affects(t, 1, adminA, deleteFailed, names["memberA"])
		affects(t, 0, adminA, deleteFailed, names["memberB"])
		affects(t, 0, asAdminB, deleteFailed, names["memberA"])
		affects(t, 1, adminA, `DELETE FROM login_locks WHERE username = $1`, names["memberA"])
		affects(t, 0, memberA, `DELETE FROM login_locks WHERE username = $1`, names["memberA"])
	})
}

func (s settings) merge(other settings) settings {
	out := settings{}
	for k, v := range s {
		out[k] = v
	}
	for k, v := range other {
		out[k] = v
	}
	return out
}

// docs/adr/0031 D1, D7: a session is its person's — read and ended by them, read
// by a global administrator, found by whoever holds its cookie, ended by the
// administrators of a managed account and by the expiry job — and no one else
// reaches it.
func TestPoliciesOfTheSessions(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	f := fixtures(t)
	withAccounts(t, w)
	boss, err := f.Person(ctx, uniqueSlug("boss"), "Boss")
	require.NoError(t, err)
	require.NoError(t, f.GlobalAdmin(ctx, boss))
	adminB, err := f.Person(ctx, uniqueSlug("admin-b"), "Admin B")
	require.NoError(t, err)
	require.NoError(t, f.Member(ctx, w.B, adminB, "admin"))
	value, hash, err := auth.GenerateSession()
	require.NoError(t, err)
	require.NoError(t, f.Exec(ctx, `INSERT INTO sessions (user_id, token_hash, created_at, last_seen_at, expires_at)
		VALUES ($1, $2, now(), now(), now() + interval '1 hour')`, w.MemberA, hash[:]))
	_ = value

	read := `SELECT count(*) FROM sessions WHERE user_id = $1`
	assert.EqualValues(t, 1, count(t, ctxOf(w.MemberA, uuid.Nil), read, w.MemberA), "its person")
	assert.EqualValues(t, 0, count(t, ctxOf(w.ViewerA, uuid.Nil), read, w.MemberA), "not another person")
	assert.EqualValues(t, 0, count(t, settings{}, read, w.MemberA), "not an anonymous context")
	assert.EqualValues(t, 1, count(t, settings{"app.session_hash": hexOf(hash[:])}, read, w.MemberA), "whoever holds the cookie")
	assert.EqualValues(t, 0, count(t, settings{"app.session_hash": hexOf(make([]byte, 32))}, read, w.MemberA), "not with another cookie")
	assert.EqualValues(t, 1, count(t, ctxOf(boss, uuid.Nil), read, w.MemberA), "a global administrator reads (D7)")
	assert.EqualValues(t, 1, count(t, ctxOf(w.AdminA, w.A), read, w.MemberA), "the administrators of the managing tenant")
	assert.EqualValues(t, 0, count(t, ctxOf(adminB, w.B), read, w.MemberA), "not another tenant's")

	insert := `INSERT INTO sessions (user_id, token_hash, created_at, last_seen_at, expires_at) VALUES ($1, decode(repeat('cd', 32), 'hex'), now(), now(), now() + interval '1 hour')`
	affects(t, 1, ctxOf(w.MemberA, uuid.Nil), insert, w.MemberA)
	denied(t, ctxOf(w.MemberA, uuid.Nil), insert, w.ViewerA)
	denied(t, ctxOf(boss, uuid.Nil), insert, w.MemberA)
	denied(t, settings{}, insert, w.MemberA)

	del := `DELETE FROM sessions WHERE user_id = $1`
	affects(t, 1, ctxOf(w.MemberA, uuid.Nil), del, w.MemberA)
	affects(t, 0, ctxOf(w.ViewerA, uuid.Nil), del, w.MemberA)
	affects(t, 0, ctxOf(boss, uuid.Nil), del, w.MemberA)
	affects(t, 1, ctxOf(w.AdminA, w.A), del, w.MemberA)
	affects(t, 0, ctxOf(adminB, w.B), del, w.MemberA)
	affects(t, 1, settings{"app.session_hash": hexOf(hash[:])}, `DELETE FROM sessions WHERE token_hash = $1`, hash[:])
	affects(t, 1, settings{"app.job": "session-expiry"}, del, w.MemberA)
	affects(t, 1, settings{"app.job": "bootstrap"}, del, w.MemberA)
	denied(t, ctxOf(w.MemberA, uuid.Nil), `UPDATE sessions SET user_id = $1 WHERE user_id = $2`, w.ViewerA, w.MemberA)
	affects(t, 1, ctxOf(w.MemberA, uuid.Nil), `UPDATE sessions SET last_seen_at = now() WHERE user_id = $1`, w.MemberA)
	affects(t, 0, ctxOf(w.ViewerA, uuid.Nil), `UPDATE sessions SET last_seen_at = now() WHERE user_id = $1`, w.MemberA)
}

func hexOf(b []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 0, len(b)*2)
	for _, c := range b {
		out = append(out, digits[c>>4], digits[c&0xf])
	}
	return string(out)
}

// The store's own reads of a session: the resolver finds the row of the hash it
// presents and no other (docs/adr/0031 D6).
func TestSessionLookupFindsThePresentedRowOnly(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	f := fixtures(t)
	db := openRuntime(t)
	_, mine, err := auth.GenerateSession()
	require.NoError(t, err)
	_, theirs, err := auth.GenerateSession()
	require.NoError(t, err)
	for person, hash := range map[uuid.UUID][32]byte{w.MemberA: mine, w.ViewerA: theirs} {
		require.NoError(t, f.Exec(ctx, `INSERT INTO sessions (user_id, token_hash, created_at, last_seen_at, expires_at)
			VALUES ($1, $2, now(), now(), now() + interval '1 hour')`, person, hash[:]))
	}
	rec, err := db.LookupSession(ctx, mine)
	require.NoError(t, err)
	assert.Equal(t, w.MemberA, rec.Person.ID)
	assert.Equal(t, w.MemberA, rec.Session.UserID)
	_, unknown, err := auth.GenerateSession()
	require.NoError(t, err)
	_, err = db.LookupSession(ctx, unknown)
	assert.ErrorIs(t, err, store.ErrNotFound)

	// An old session is not the store's to judge: the caller's clock does.
	require.NoError(t, f.Exec(ctx, `UPDATE sessions SET expires_at = now() - interval '1 second', created_at = now() - interval '1 hour' WHERE token_hash = $1`, mine[:]))
	rec, err = db.LookupSession(ctx, mine)
	require.NoError(t, err)
	assert.True(t, rec.Session.ExpiresAt.Before(time.Now()))
}
