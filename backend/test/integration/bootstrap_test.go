//go:build integration

package integration

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/bootstrap"
	"github.com/guided-traffic/cowork/backend/test/fixture"
)

func audit(t *testing.T, i isolated, where string, args ...any) int64 {
	t.Helper()
	n, err := i.F.QueryCount(context.Background(), `SELECT count(*) FROM audit_events WHERE `+where, args...)
	require.NoError(t, err)
	return n
}

func hashOfRoot(t *testing.T, i isolated) string {
	t.Helper()
	var hash string
	require.NoError(t, i.F.QueryRow(context.Background(),
		`SELECT a.password_hash FROM local_accounts a JOIN users u ON u.id = a.user_id WHERE u.username = 'root'`).Scan(&hash))
	return hash
}

// docs/adr/0032 D1–D3, D6, D7: every start keeps the configured account in step
// with the configuration — created, left alone when nothing changed, re-hashed
// with its sessions ended when the password did, deactivated when the variables
// go, reactivated when they return — and creates the bootstrap tenant only
// while none exists.
func TestBootstrapKeepsTheConfiguredAdministrator(t *testing.T) {
	ctx := context.Background()
	iso := newIsolated(t)
	logger := (&recordingLogger{}).logger()
	p := bootstrap.Params{Username: "root", Password: testPassword, TenantSlug: "main", TenantName: "Main"}
	require.NoError(t, bootstrap.Sync(ctx, iso.DB, p, logger))

	var id uuid.UUID
	var global bool
	var origin string
	var managing *uuid.UUID
	require.NoError(t, iso.F.QueryRow(ctx, `SELECT u.id, u.global_admin, a.origin, a.managing_tenant_id FROM users u
		JOIN local_accounts a ON a.user_id = u.id WHERE u.username = 'root'`).Scan(&id, &global, &origin, &managing))
	assert.True(t, global, "the account is a global administrator (D1)")
	assert.Equal(t, "config", origin)
	assert.Nil(t, managing, "no tenant manages it")
	ok, err := auth.VerifyPassword(ctx, testPassword, hashOfRoot(t, iso))
	require.NoError(t, err)
	assert.True(t, ok)
	tenantID := scalar2[uuid.UUID](t, iso, `SELECT id FROM tenants WHERE slug = 'main'`)
	assert.Equal(t, "admin", scalar2[string](t, iso, `SELECT role::text FROM memberships WHERE tenant_id = $1 AND user_id = $2 AND source = 'grant'`, tenantID, id),
		"the marked grant as administrator of the bootstrap tenant (D6, D7)")
	assert.EqualValues(t, 3, audit(t, iso, `actor_system = 'system:bootstrap' AND tenant_id IS NULL AND action = 'created'`),
		"the account, the tenant and the grant, recorded as system:bootstrap")

	// A start that finds everything as configured changes and records nothing.
	rows := audit(t, iso, `true`)
	hash := hashOfRoot(t, iso)
	require.NoError(t, bootstrap.Sync(ctx, iso.DB, p, logger))
	assert.Equal(t, rows, audit(t, iso, `true`))
	assert.Equal(t, hash, hashOfRoot(t, iso), "a password that fits is not re-hashed")

	// The bootstrap tenant does nothing once a tenant exists, whatever it says.
	require.NoError(t, bootstrap.Sync(ctx, iso.DB, bootstrap.Params{Username: "root", Password: testPassword, TenantSlug: "other", TenantName: "Other"}, logger))
	assert.Zero(t, scalar2[int64](t, iso, `SELECT count(*) FROM tenants WHERE slug = 'other'`))

	// A changed password: re-hashed, every session ended, failures and lock
	// forgotten — how a locked administrator is recovered (D2, docs/adr/0033 D6).
	s := newAPI(t, withLogin, iso.option)
	b := s.browser(t)
	b.mustLogin("root", testPassword)
	for range 5 {
		assertProblem(t, s.browser(t).login("root", "wrong password!"), http.StatusUnauthorized, "invalid_credentials")
	}
	assert.EqualValues(t, 1, scalar2[int64](t, iso, `SELECT count(*) FROM login_locks WHERE username = 'root'`))
	changed := bootstrap.Params{Username: "root", Password: "a rotated password", TenantSlug: "main", TenantName: "Main"}
	require.NoError(t, bootstrap.Sync(ctx, iso.DB, changed, logger))
	assert.NotEqual(t, hash, hashOfRoot(t, iso))
	assertProblem(t, b.get("/api/v1/me"), http.StatusUnauthorized, "unauthenticated")
	assert.Zero(t, scalar2[int64](t, iso, `SELECT count(*) FROM login_locks WHERE username = 'root'`))
	assertProblem(t, s.browser(t).login("root", testPassword), http.StatusUnauthorized, "invalid_credentials")
	require.Equal(t, http.StatusOK, s.browser(t).login("root", "a rotated password").StatusCode)
	assert.EqualValues(t, 1, audit(t, iso, `actor_system = 'system:bootstrap' AND action = 'password_changed' AND reason = 'configuration' AND entity_id = $1`, id))
	assert.EqualValues(t, 0, audit(t, iso, `(coalesce(before::text, '') || coalesce(after::text, '') || coalesce(note, '') || coalesce(reason, '')) ~ 'a rotated password|correct horse'`))

	// The variables unset: the account is deactivated, its sessions ended, its
	// tokens revoked — and the person stays.
	live := s.browser(t)
	live.mustLogin("root", "a rotated password")
	token, _, err := iso.F.Token(ctx, fixture.TokenSpec{UserID: id})
	require.NoError(t, err)
	require.NoError(t, bootstrap.Sync(ctx, iso.DB, bootstrap.Params{}, logger))
	assertProblem(t, live.get("/api/v1/me"), http.StatusUnauthorized, "unauthenticated")
	assertProblem(t, s.browser(t).login("root", "a rotated password"), http.StatusUnauthorized, "invalid_credentials")
	assertProblem(t, s.do(t, caller{Token: token}, http.MethodGet, "/api/v1/me", nil), http.StatusUnauthorized, "token_revoked")
	assert.NotNil(t, scalar2[*string](t, iso, `SELECT deactivated_at::text FROM users WHERE id = $1`, id), "deactivated, never deleted")
	assert.EqualValues(t, 1, audit(t, iso, `actor_system = 'system:bootstrap' AND action = 'deactivated' AND entity_id = $1`, id))
	require.NoError(t, bootstrap.Sync(ctx, iso.DB, bootstrap.Params{}, logger))
	assert.EqualValues(t, 1, audit(t, iso, `actor_system = 'system:bootstrap' AND action = 'deactivated' AND entity_id = $1`, id), "once")

	// Set again: reactivated; a revoked token stays revoked (docs/adr/0035 D6).
	require.NoError(t, bootstrap.Sync(ctx, iso.DB, changed, logger))
	require.Equal(t, http.StatusOK, s.browser(t).login("root", "a rotated password").StatusCode)
	assert.EqualValues(t, 1, audit(t, iso, `action = 'reactivated' AND entity_id = $1`, id))
	assertProblem(t, s.do(t, caller{Token: token}, http.MethodGet, "/api/v1/me", nil), http.StatusUnauthorized, "token_revoked")

	// Another username: the account it kept is deactivated, the new one made.
	require.NoError(t, bootstrap.Sync(ctx, iso.DB, bootstrap.Params{Username: "ada", Password: testPassword}, logger))
	assert.NotNil(t, scalar2[*string](t, iso, `SELECT deactivated_at::text FROM users WHERE id = $1`, id))
	assert.Nil(t, scalar2[*string](t, iso, `SELECT deactivated_at::text FROM users WHERE username = 'ada'`))
	assert.Equal(t, http.StatusOK, s.browser(t).login("ada", testPassword).StatusCode)
}

// docs/adr/0032 D2: the account of the configured name that a tenant's
// administrator made first is the operator's now: the configured password, no
// session, no token, no tenant that manages it.
func TestBootstrapTakesOverAnAccountOfTheSameName(t *testing.T) {
	ctx := context.Background()
	iso := newIsolated(t)
	tenant, err := iso.F.Tenant(ctx, "acme", "Acme")
	require.NoError(t, err)
	person, err := iso.F.Person(ctx, "root", "Root")
	require.NoError(t, err)
	require.NoError(t, iso.F.Member(ctx, tenant, person, "member"))
	require.NoError(t, iso.F.Account(ctx, person, "the tenants password", tenant, false))
	token, _, err := iso.F.Token(ctx, fixture.TokenSpec{UserID: person})
	require.NoError(t, err)
	s := newAPI(t, withLogin, iso.option)
	b := s.browser(t)
	b.mustLogin("root", "the tenants password")

	require.NoError(t, bootstrap.Sync(ctx, iso.DB, bootstrap.Params{Username: "root", Password: testPassword}, (&recordingLogger{}).logger()))
	assert.Equal(t, "config", scalar2[string](t, iso, `SELECT origin FROM local_accounts WHERE user_id = $1`, person))
	assert.Nil(t, scalar2[*uuid.UUID](t, iso, `SELECT managing_tenant_id FROM local_accounts WHERE user_id = $1`, person))
	assert.True(t, scalar2[bool](t, iso, `SELECT global_admin FROM users WHERE id = $1`, person))
	assertProblem(t, b.get("/api/v1/me"), http.StatusUnauthorized, "unauthenticated")
	assertProblem(t, s.browser(t).login("root", "the tenants password"), http.StatusUnauthorized, "invalid_credentials")
	assertProblem(t, s.do(t, caller{Token: token}, http.MethodGet, "/api/v1/me", nil), http.StatusUnauthorized, "token_revoked")
	require.Equal(t, http.StatusOK, s.browser(t).login("root", testPassword).StatusCode)
	assert.EqualValues(t, 1, audit(t, iso, `action = 'password_changed' AND entity_id = $1 AND (after->>'taken_over')::boolean`, person))
}

// docs/adr/0032 D2: several replicas start at once; the advisory lock lets one
// work and the others find nothing left.
func TestConcurrentBootstrapsAgree(t *testing.T) {
	ctx := context.Background()
	iso := newIsolated(t)
	p := bootstrap.Params{Username: "root", Password: testPassword, TenantSlug: "main", TenantName: "Main"}
	logger := (&recordingLogger{}).logger()
	errs := make([]error, 4)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Go(func() { errs[i] = bootstrap.Sync(ctx, iso.DB, p, logger) })
	}
	wg.Wait()
	for _, err := range errs {
		require.NoError(t, err)
	}
	assert.EqualValues(t, 1, scalar2[int64](t, iso, `SELECT count(*) FROM users WHERE username = 'root'`))
	assert.EqualValues(t, 1, scalar2[int64](t, iso, `SELECT count(*) FROM tenants`))
	assert.EqualValues(t, 3, audit(t, iso, `actor_system = 'system:bootstrap'`))
}

func scalar2[T any](t *testing.T, i isolated, sql string, args ...any) T {
	t.Helper()
	var v T
	require.NoError(t, i.F.QueryRow(context.Background(), sql, args...).Scan(&v))
	return v
}
