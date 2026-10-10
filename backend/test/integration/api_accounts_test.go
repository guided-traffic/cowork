//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/test/fixture"
)

func accountsPath(slug string, rest ...string) string {
	return "/api/v1/teams/" + slug + "/accounts" + strings.Join(rest, "")
}

// docs/adr/0033 D1, D4: an administrator creates an account with a temporary
// password and a marked grant; the person changes it at the first login before
// anything else — reading who they are and leaving stay possible — and a
// change ends every other session of the account (docs/adr/0031 D4).
func TestATemporaryPasswordIsChangedBeforeAnythingElse(t *testing.T) {
	w := newWorld(t)
	names := withAccounts(t, w)
	s := newAPI(t, withLogin)
	admin := s.browser(t)
	admin.mustLogin(names["adminA"], testPassword)
	username, temporary := uniqueSlug("new-person"), "temporary secret 1"

	res := admin.request(http.MethodPost, accountsPath(w.SlugA), map[string]string{
		"username": username, "display_name": "New Person", "temporary_password": temporary, "role": "member"})
	require.Equal(t, http.StatusCreated, res.StatusCode)
	account := decode[apigen.Account](t, res)
	assert.Equal(t, username, account.Username)
	assert.True(t, account.PasswordChangeRequired)
	assert.False(t, account.Locked)
	assert.Equal(t, apigen.RoleMember, account.Role.MustGet())
	assert.True(t, account.DeactivatedAt.IsNull())

	// The grant is marked, the tenant manages the account, the hash is
	// Argon2id and holds no password.
	assert.Equal(t, "member", scalar[string](t, `SELECT role::text FROM memberships WHERE tenant_id = $1 AND user_id = $2 AND source = 'grant'`, w.A, account.Id))
	assert.Equal(t, w.A, scalar[uuid.UUID](t, `SELECT managing_tenant_id FROM local_accounts WHERE user_id = $1 AND origin = 'tenant'`, account.Id))
	assert.False(t, scalar[bool](t, `SELECT global_admin FROM users WHERE id = $1`, account.Id))
	hash := scalar[string](t, `SELECT password_hash FROM local_accounts WHERE user_id = $1`, account.Id)
	assert.True(t, strings.HasPrefix(hash, "$argon2id$v=19$m=19456,t=2,p=1$"))
	assert.NotContains(t, hash, temporary)
	assert.EqualValues(t, 2, scalar[int64](t, `SELECT count(*) FROM audit_events WHERE tenant_id = $1 AND actor_user_id = $2 AND action = 'created'
		AND entity_type IN ('user', 'membership') AND (entity_id = $3 OR after->>'user' = $3::text)`, w.A, w.AdminA, account.Id))

	// The temporary password gates the session, not a token the person already
	// holds: a token never had the password.
	held, _, err := fixtures(t).Token(context.Background(), fixture.TokenSpec{UserID: account.Id})
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, s.do(t, caller{Token: held}, http.MethodGet, "/api/v1/me", nil).StatusCode)

	person, second := s.browser(t), s.browser(t)
	loginRes := person.login(username, temporary)
	require.Equal(t, http.StatusOK, loginRes.StatusCode)
	assert.Equal(t, true, decode[map[string]any](t, loginRes)["password_change_required"])
	second.mustLogin(username, temporary)

	me := person.get("/api/v1/me")
	require.Equal(t, http.StatusOK, me.StatusCode, "reading who they are stays possible")
	assert.True(t, decode[apigen.Me](t, me).PasswordChangeRequired)
	for _, path := range []string{"/api/v1/teams/" + w.SlugA, "/api/v1/me/tokens", "/api/v1/teams/" + w.SlugA + "/projects"} {
		assertProblem(t, person.get(path), http.StatusForbidden, "password_change_required")
	}
	assertProblem(t, person.request(http.MethodPost, "/api/v1/me/tokens", map[string]string{"name": "x", "scope": "read"}), http.StatusForbidden, "password_change_required")

	change := func(current, next string) *http.Response {
		return person.request(http.MethodPut, "/api/v1/me/password", map[string]string{"current_password": current, "new_password": next})
	}
	body := assertProblem(t, change("not the temporary one", "a brand new password"), http.StatusBadRequest, "validation_failed")
	assert.Equal(t, "/current_password", body["errors"].([]any)[0].(map[string]any)["pointer"])
	body = assertProblem(t, change(temporary, "short one"), http.StatusBadRequest, "validation_failed")
	assert.Equal(t, "/new_password", body["errors"].([]any)[0].(map[string]any)["pointer"])
	assert.Contains(t, body["errors"].([]any)[0].(map[string]any)["message"], "at least 12 characters")
	body = assertProblem(t, change(temporary, temporary), http.StatusBadRequest, "validation_failed")
	assert.Equal(t, "/new_password", body["errors"].([]any)[0].(map[string]any)["pointer"], "no change in disguise")
	assertProblem(t, person.request(http.MethodPut, "/api/v1/me/password", map[string]string{"current_password": temporary, "new_password": strings.Repeat("x", 1025)}),
		http.StatusBadRequest, "validation_failed")

	require.Equal(t, http.StatusNoContent, change(temporary, "a brand new password").StatusCode)
	assert.False(t, decode[apigen.Me](t, person.get("/api/v1/me")).PasswordChangeRequired)
	assert.Equal(t, http.StatusOK, person.get("/api/v1/teams/"+w.SlugA).StatusCode)
	assertProblem(t, second.get("/api/v1/me"), http.StatusUnauthorized, "unauthenticated")
	assertProblem(t, s.browser(t).login(username, temporary), http.StatusUnauthorized, "invalid_credentials")
	assert.Equal(t, http.StatusOK, s.browser(t).login(username, "a brand new password").StatusCode)

	assert.EqualValues(t, 1, scalar[int64](t, `SELECT count(*) FROM audit_events WHERE action = 'password_changed' AND actor_user_id = $1
		AND (after->>'sessions_ended')::int = 1`, account.Id))
	assert.EqualValues(t, 0, scalar[int64](t, `SELECT count(*) FROM audit_events WHERE
		(coalesce(before::text, '') || coalesce(after::text, '') || coalesce(note, '') || coalesce(reason, '')) ~ 'temporary secret|brand new password'`),
		"no password is in an audit row")
}

// docs/adr/0033 D6: a wrong current password is a failed attempt of the
// account, so a stolen session cannot guess it; the local administrator's
// password is the configuration's.
func TestChangingAPasswordCountsAndTheAdministratorsIsTheConfigurations(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	names := withAccounts(t, w)
	f := fixtures(t)
	s := newAPI(t, withLogin)

	b := s.browser(t)
	b.mustLogin(names["memberA"], testPassword)
	for range 5 {
		assertProblem(t, b.request(http.MethodPut, "/api/v1/me/password", map[string]string{"current_password": "wrong password!", "new_password": "some other password"}),
			http.StatusBadRequest, "validation_failed")
	}
	assertProblem(t, b.request(http.MethodPut, "/api/v1/me/password", map[string]string{"current_password": testPassword, "new_password": "some other password"}),
		http.StatusBadRequest, "validation_failed")
	assertProblem(t, s.browser(t).login(names["memberA"], testPassword), http.StatusUnauthorized, "invalid_credentials")

	root, err := f.Person(ctx, uniqueSlug("root"), "Root")
	require.NoError(t, err)
	require.NoError(t, f.Account(ctx, root, testPassword, uuid.Nil, false))
	rb := s.browser(t)
	rb.mustLogin(usernameOf(t, root), testPassword)
	res := rb.request(http.MethodPut, "/api/v1/me/password", map[string]string{"current_password": testPassword, "new_password": "some other password"})
	body := assertProblem(t, res, http.StatusForbidden, "forbidden")
	assert.Contains(t, body["detail"], "COWORK_LOCAL_ADMIN_PASSWORD")
}

// docs/adr/0033 D1, D5, docs/adr/0031 D4, docs/adr/0024 D5: the account routes
// belong to the tenant's administrators, manage the accounts that tenant
// created and no others, and keep an administrator off their own account where
// that would get round a check.
func TestAccountAdministration(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	names := withAccounts(t, w)
	f := fixtures(t)
	tokens := issueTokens(t, w)
	s := newAPI(t, withLogin)

	adminB, err := f.Person(ctx, uniqueSlug("admin-b"), "Admin B")
	require.NoError(t, err)
	require.NoError(t, f.Member(ctx, w.B, adminB, "admin"))
	require.NoError(t, f.Account(ctx, adminB, testPassword, w.B, false))
	boss, err := f.Person(ctx, uniqueSlug("boss"), "Boss")
	require.NoError(t, err)
	require.NoError(t, f.GlobalAdmin(ctx, boss))
	require.NoError(t, f.Member(ctx, w.A, boss, "member"))
	require.NoError(t, f.Account(ctx, boss, testPassword, uuid.Nil, false))

	admin, adminOfB := s.browser(t), s.browser(t)
	admin.mustLogin(names["adminA"], testPassword)
	adminOfB.mustLogin(usernameOf(t, adminB), testPassword)
	base := accountsPath(w.SlugA)

	// A tenant's administrators only, with admin scope, never an agent.
	member, viewer := s.browser(t), s.browser(t)
	member.mustLogin(names["memberA"], testPassword)
	viewer.mustLogin(names["viewerA"], testPassword)
	for _, who := range []*browser{member, viewer} {
		assertProblem(t, who.get(base), http.StatusForbidden, "forbidden")
		assertProblem(t, who.request(http.MethodPost, base, map[string]string{"username": "x1", "display_name": "x", "temporary_password": "temporary secret 1", "role": "viewer"}),
			http.StatusForbidden, "forbidden")
		assertProblem(t, who.request(http.MethodDelete, accountsPath(w.SlugA, "/", names["memberA"], "/sessions"), nil), http.StatusForbidden, "forbidden")
	}
	agent := s.do(t, caller{Token: tokens.AdminA, Agent: "claude-code/opus/s1"}, http.MethodGet, base, nil)
	assertProblem(t, agent, http.StatusForbidden, "agent_forbidden")
	assertProblem(t, s.do(t, caller{Token: tokens.AdminAWrite}, http.MethodGet, base, nil), http.StatusForbidden, "insufficient_scope")
	assert.Equal(t, http.StatusOK, s.do(t, caller{Token: tokens.AdminA}, http.MethodGet, base, nil).StatusCode, "an admin-scope token of an administrator may")

	// Another tenant's administrator is outside this tenant: the same 404 as an
	// unknown tenant.
	assertProblem(t, adminOfB.get(base), http.StatusNotFound, "not_found")

	// Create, and the failures of creating.
	create := func(username, password, role string) *http.Response {
		return admin.request(http.MethodPost, base, map[string]string{"username": username, "display_name": "Person " + username, "temporary_password": password, "role": role})
	}
	target := uniqueSlug("target")
	created := create(target, "temporary secret 1", "viewer")
	require.Equal(t, http.StatusCreated, created.StatusCode)
	targetID := decode[apigen.Account](t, created).Id
	taken := assertProblem(t, create(names["memberB"], "temporary secret 1", "member"), http.StatusConflict, "username_taken")
	assert.Equal(t, "/username", taken["errors"].([]any)[0].(map[string]any)["pointer"], "a name of another tenant is taken too")
	assertProblem(t, create(uniqueSlug("short"), "short", "member"), http.StatusBadRequest, "validation_failed")
	assertProblem(t, create("Not A Name", "temporary secret 1", "member"), http.StatusBadRequest, "validation_failed")
	assertProblem(t, create(uniqueSlug("role"), "temporary secret 1", "owner"), http.StatusBadRequest, "validation_failed")
	assertProblem(t, admin.request(http.MethodPost, base, map[string]string{"username": uniqueSlug("blank"), "display_name": "   ", "temporary_password": "temporary secret 1", "role": "member"}),
		http.StatusBadRequest, "validation_failed")
	// Creating takes a session only (docs/adr/0033 D1): a token — an administrator's,
	// with admin scope, or an agent's — is refused by the resolver, and nothing is written.
	viaToken := map[string]string{"username": uniqueSlug("via-token"), "display_name": "Via Token", "temporary_password": "temporary secret 1", "role": "member"}
	assertProblem(t, s.do(t, caller{Token: tokens.AdminA}, http.MethodPost, base, viaToken), http.StatusForbidden, "session_required")
	assertProblem(t, s.do(t, caller{Token: tokens.AdminA, Agent: "claude-code/opus/s1"}, http.MethodPost, base, viaToken), http.StatusForbidden, "session_required")
	assert.Zero(t, scalar[int64](t, `SELECT count(*) FROM users WHERE username = $1`, viaToken["username"]), "a refused creation writes nothing")

	// The list: the accounts this tenant manages, and only those.
	list := decode[apigen.AccountList](t, admin.get(base+"?limit=200"))
	listed := map[string]apigen.Account{}
	for _, a := range list.Items {
		listed[a.Username] = a
	}
	assert.Contains(t, listed, target)
	assert.Contains(t, listed, names["memberA"])
	assert.NotContains(t, listed, names["memberB"], "another tenant's account")
	assert.NotContains(t, listed, usernameOf(t, boss), "a global administrator is nobody's to manage")
	assert.Equal(t, apigen.RoleViewer, listed[target].Role.MustGet())
	page := decode[apigen.AccountList](t, admin.get(base+"?limit=1"))
	require.Len(t, page.Items, 1)
	require.False(t, page.NextCursor.IsNull())

	// Accounts the tenant does not manage answer like a username nobody has.
	for _, route := range []struct{ method, tail string }{
		{http.MethodPut, "/password"}, {http.MethodDelete, "/lockout"}, {http.MethodPut, "/deactivation"}, {http.MethodDelete, "/sessions"},
	} {
		var body any
		if route.tail == "/password" {
			body = map[string]string{"temporary_password": "temporary secret 2"}
		}
		ghost := admin.request(route.method, accountsPath(w.SlugA, "/", uniqueSlug("ghost"), route.tail), body)
		want := unsafeBody(t, ghost)
		require.Equal(t, http.StatusNotFound, ghost.StatusCode)
		for label, name := range map[string]string{"another tenant's account": names["memberB"], "a global administrator": usernameOf(t, boss)} {
			res := admin.request(route.method, accountsPath(w.SlugA, "/", name, route.tail), body)
			require.Equal(t, http.StatusNotFound, res.StatusCode, "%s %s", label, route.tail)
			got := unsafeBody(t, res)
			delete(got, "instance")
			delete(want, "instance")
			assert.Equal(t, want, got, "%s %s is indistinguishable from an unknown username", label, route.tail)
		}
		// The other tenant's own administrator may touch it only in their tenant.
		res := adminOfB.request(route.method, accountsPath(w.SlugA, "/", target, route.tail), body)
		assert.Equal(t, http.StatusNotFound, res.StatusCode)
		res = adminOfB.request(route.method, accountsPath(w.SlugB, "/", target, route.tail), body)
		assert.Equal(t, http.StatusNotFound, res.StatusCode, "an account of tenant A is not tenant B's to manage")
	}

	// Not their own account where that would get round a check.
	mine := names["adminA"]
	assertProblem(t, admin.request(http.MethodPut, accountsPath(w.SlugA, "/", mine, "/password"), map[string]string{"temporary_password": "temporary secret 2"}), http.StatusForbidden, "forbidden")
	assertProblem(t, admin.request(http.MethodDelete, accountsPath(w.SlugA, "/", mine, "/lockout"), nil), http.StatusForbidden, "forbidden")
	assertProblem(t, admin.request(http.MethodPut, accountsPath(w.SlugA, "/", mine, "/deactivation"), nil), http.StatusForbidden, "forbidden")
	assert.Equal(t, http.StatusOK, admin.get("/api/v1/me").StatusCode)

	// Reset: a new temporary password, every session of the account ends.
	person := s.browser(t)
	person.mustLogin(target, "temporary secret 1")
	// A token cannot reset it, whatever its scope (docs/adr/0033 D5): the password is
	// unchanged and no session ends.
	hashBefore := scalar[string](t, `SELECT password_hash FROM local_accounts WHERE user_id = $1`, targetID)
	assertProblem(t, s.do(t, caller{Token: tokens.AdminA}, http.MethodPut, accountsPath(w.SlugA, "/", target, "/password"),
		map[string]string{"temporary_password": "temporary secret 2"}), http.StatusForbidden, "session_required")
	assert.Equal(t, hashBefore, scalar[string](t, `SELECT password_hash FROM local_accounts WHERE user_id = $1`, targetID), "a refused reset changes nothing")
	assert.Equal(t, http.StatusOK, person.get("/api/v1/me").StatusCode, "and ends no session")
	require.Equal(t, http.StatusNoContent, admin.request(http.MethodPut, accountsPath(w.SlugA, "/", target, "/password"), map[string]string{"temporary_password": "temporary secret 2"}).StatusCode)
	assertProblem(t, person.get("/api/v1/me"), http.StatusUnauthorized, "unauthenticated")
	assertProblem(t, s.browser(t).login(target, "temporary secret 1"), http.StatusUnauthorized, "invalid_credentials")
	again := s.browser(t).login(target, "temporary secret 2")
	require.Equal(t, http.StatusOK, again.StatusCode)
	assert.Equal(t, true, decode[map[string]any](t, again)["password_change_required"])
	assertProblem(t, admin.request(http.MethodPut, accountsPath(w.SlugA, "/", target, "/password"), map[string]string{"temporary_password": "short"}), http.StatusBadRequest, "validation_failed")
	assert.EqualValues(t, 1, scalar[int64](t, `SELECT count(*) FROM audit_events WHERE action = 'password_reset' AND entity_id = $1 AND actor_user_id = $2 AND tenant_id = $3`,
		targetID, w.AdminA, w.A))

	// End the sessions: tokens are untouched, and the second call changes nothing.
	tokenPlain, _, err := f.Token(ctx, fixture.TokenSpec{UserID: targetID})
	require.NoError(t, err)
	live := s.browser(t)
	live.mustLogin(target, "temporary secret 2")
	require.Equal(t, http.StatusNoContent, admin.request(http.MethodDelete, accountsPath(w.SlugA, "/", target, "/sessions"), nil).StatusCode)
	assertProblem(t, live.get("/api/v1/me"), http.StatusUnauthorized, "unauthenticated")
	assert.Equal(t, http.StatusOK, s.do(t, caller{Token: tokenPlain}, http.MethodGet, "/api/v1/me", nil).StatusCode)
	require.Equal(t, http.StatusNoContent, admin.request(http.MethodDelete, accountsPath(w.SlugA, "/", target, "/sessions"), nil).StatusCode)
	assert.EqualValues(t, 1, scalar[int64](t, `SELECT count(*) FROM audit_events WHERE action = 'revoked' AND entity_type = 'user' AND entity_id = $1 AND tenant_id = $2`, targetID, w.A))
	// Ending their own is allowed.
	require.Equal(t, http.StatusNoContent, admin.request(http.MethodDelete, accountsPath(w.SlugA, "/", mine, "/sessions"), nil).StatusCode)
	assertProblem(t, admin.get("/api/v1/me"), http.StatusUnauthorized, "unauthenticated")
	admin.mustLogin(mine, testPassword)

	// Deactivate: no login, tokens revoked, sessions ended; the person and their
	// grant stay; the second call changes nothing.
	live.mustLogin(target, "temporary secret 2")
	require.Equal(t, http.StatusNoContent, admin.request(http.MethodPut, accountsPath(w.SlugA, "/", target, "/deactivation"), nil).StatusCode)
	assertProblem(t, live.get("/api/v1/me"), http.StatusUnauthorized, "unauthenticated")
	assertProblem(t, s.browser(t).login(target, "temporary secret 2"), http.StatusUnauthorized, "invalid_credentials")
	assertProblem(t, s.do(t, caller{Token: tokenPlain}, http.MethodGet, "/api/v1/me", nil), http.StatusUnauthorized, "token_revoked")
	assert.Equal(t, w.AdminA, scalar[uuid.UUID](t, `SELECT revoked_by FROM tokens WHERE user_id = $1`, targetID), "revoked by the administrator")
	assert.EqualValues(t, 1, scalar[int64](t, `SELECT count(*) FROM memberships WHERE tenant_id = $1 AND user_id = $2`, w.A, targetID))
	require.Equal(t, http.StatusNoContent, admin.request(http.MethodPut, accountsPath(w.SlugA, "/", target, "/deactivation"), nil).StatusCode)
	assert.EqualValues(t, 1, scalar[int64](t, `SELECT count(*) FROM audit_events WHERE action = 'deactivated' AND entity_id = $1 AND tenant_id = $2 AND actor_user_id = $3`, targetID, w.A, w.AdminA))
	deactivated := decode[apigen.AccountList](t, admin.get(base+"?limit=200"))
	for _, a := range deactivated.Items {
		if a.Username == target {
			assert.False(t, a.DeactivatedAt.IsNull(), "the list shows it")
		}
	}
}

// docs/adr/0033 D1, D5: creating an account and resetting a password are a
// session's alone, because an account or a password the administrator chose
// outlives the revocation of a leaked token, and so is the unlock — a token
// that could unlock between guesses would keep the lockout from ever holding
// (docs/adr/0035 D5 as amended 2026-10-07); the routes that only remove or
// restrict access stay open to an administrator's token.
func TestAccountRoutesAnAdministratorsTokenMayStillCall(t *testing.T) {
	w := newWorld(t)
	names := withAccounts(t, w)
	tokens := issueTokens(t, w)
	s := newAPI(t, withLogin)
	admin := s.browser(t)
	admin.mustLogin(names["adminA"], testPassword)
	target := uniqueSlug("by-token")
	require.Equal(t, http.StatusCreated, admin.request(http.MethodPost, accountsPath(w.SlugA), map[string]string{
		"username": target, "display_name": "By Token", "temporary_password": "temporary secret 1", "role": "member"}).StatusCode)
	viaToken := func(method, tail string) *http.Response {
		return s.do(t, caller{Token: tokens.AdminA}, method, accountsPath(w.SlugA, "/", target, tail), nil)
	}

	live := s.browser(t)
	live.mustLogin(target, "temporary secret 1")
	require.Equal(t, http.StatusNoContent, viaToken(http.MethodDelete, "/sessions").StatusCode, "ending the sessions")
	assertProblem(t, live.get("/api/v1/me"), http.StatusUnauthorized, "unauthenticated")

	for range 5 {
		assertProblem(t, s.browser(t).login(target, "wrong password!"), http.StatusUnauthorized, "invalid_credentials")
	}
	assertProblem(t, s.browser(t).login(target, "temporary secret 1"), http.StatusUnauthorized, "invalid_credentials")
	assertProblem(t, viaToken(http.MethodDelete, "/lockout"), http.StatusForbidden, "session_required")
	assertProblem(t, s.browser(t).login(target, "temporary secret 1"), http.StatusUnauthorized, "invalid_credentials")
	require.Equal(t, http.StatusNoContent, admin.request(http.MethodDelete, accountsPath(w.SlugA, "/", target, "/lockout"), nil).StatusCode,
		"unlocking, in a session")
	live.mustLogin(target, "temporary secret 1")

	require.Equal(t, http.StatusNoContent, viaToken(http.MethodPut, "/deactivation").StatusCode, "deactivating")
	assertProblem(t, live.get("/api/v1/me"), http.StatusUnauthorized, "unauthenticated")
	assertProblem(t, s.browser(t).login(target, "temporary secret 1"), http.StatusUnauthorized, "invalid_credentials")

	// Each is the administrator's act through the token: the row carries the
	// token; the unlock is the session's, and its row carries none.
	for _, action := range []string{"revoked", "deactivated"} {
		assert.EqualValues(t, 1, scalar[int64](t, `SELECT count(*) FROM audit_events WHERE tenant_id = $1 AND actor_user_id = $2 AND action = $3::audit_action
			AND token_id IS NOT NULL AND entity_type = 'user'`, w.A, w.AdminA, action), action)
	}
	assert.EqualValues(t, 1, scalar[int64](t, `SELECT count(*) FROM audit_events WHERE tenant_id = $1 AND actor_user_id = $2 AND action = 'unlocked'
		AND token_id IS NULL AND entity_type = 'user'`, w.A, w.AdminA))
}

// tenantLockNamespace is the first key of a tenant's lock, "cowt"
// (store.Writer.LockTenant; docs/developer/data-access.md).
const tenantLockNamespace int32 = 0x636f7774

// tenantAdmins makes n persons with an account the tenant manages, each its
// administrator by a grant, and returns their ids and usernames.
func tenantAdmins(t *testing.T, tenant uuid.UUID, n int) ([]uuid.UUID, []string) {
	t.Helper()
	ctx := context.Background()
	f := fixtures(t)
	ids, names := make([]uuid.UUID, 0, n), make([]string, 0, n)
	for range n {
		id, err := f.Person(ctx, uniqueSlug("admin"), "Admin")
		require.NoError(t, err)
		require.NoError(t, f.Account(ctx, id, testPassword, tenant, false))
		require.NoError(t, f.Member(ctx, tenant, id, domain.RoleAdmin))
		ids, names = append(ids, id), append(names, usernameOf(t, id))
	}
	return ids, names
}

// holdTenantLock takes the tenant's lock in a transaction of the test's own
// over the administrative connection; release ends the transaction.
func holdTenantLock(t *testing.T, tenant uuid.UUID) (release func()) {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, env.AdminURL)
	require.NoError(t, err)
	tx, err := conn.Begin(ctx)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1, hashtext($2::text))", tenantLockNamespace, tenant)
	require.NoError(t, err)
	var once sync.Once
	release = func() {
		once.Do(func() {
			_ = tx.Rollback(ctx)
			_ = conn.Close(ctx)
		})
	}
	t.Cleanup(release)
	return release
}

// waitForTenantLock returns once a transaction waits for the tenant's lock, and
// fails when the request answered without waiting for it.
func waitForTenantLock(t *testing.T, tenant uuid.UUID, answered <-chan *http.Response) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		select {
		case res := <-answered:
			require.Failf(t, "the request did not wait for the tenant's lock", "it answered %d", res.StatusCode)
		default:
		}
		if scalar[int64](t, `SELECT count(*) FROM pg_locks WHERE locktype = 'advisory' AND NOT granted
			AND classid = $1::int4::oid AND objid = hashtext($2::text)::oid AND objsubid = 2`, tenantLockNamespace, tenant) > 0 {
			return
		}
	}
	require.Fail(t, "no request waited for the tenant's lock")
}

// docs/adr/0034 D1, docs/adr/0024 D5: a deactivation changes who administers
// its tenant, so it waits for the tenant's lock and decides on what committed
// before it — an administrator deactivated a moment earlier counts no more.
// With no administrator who can log in left it is 409 last_admin and changes
// nothing: no token revoked, no session ended, no act recorded. With one left
// it goes through.
func TestADeactivationLeavesTheTenantAnAdministrator(t *testing.T) {
	ctx := context.Background()
	f := fixtures(t)
	s := newAPI(t, withLogin)
	deactivations := `SELECT count(*) FROM audit_events WHERE action = 'deactivated' AND entity_id = $1`
	for _, tc := range []struct {
		name   string
		admins int
	}{
		{"the last administrator", 2},
		{"another administrator remains", 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			slug := uniqueSlug("keep")
			tenant, err := f.Tenant(ctx, slug, "Keep")
			require.NoError(t, err)
			ids, names := tenantAdmins(t, tenant, tc.admins)
			actor, target := ids[0], ids[1]
			admin, person := s.browser(t), s.browser(t)
			admin.mustLogin(names[0], testPassword)
			person.mustLogin(names[1], testPassword)
			token, _, err := f.Token(ctx, fixture.TokenSpec{UserID: target})
			require.NoError(t, err)

			// The request waits for the lock the test holds; meanwhile another
			// administrator deactivates the actor and commits first.
			release := holdTenantLock(t, tenant)
			answered := make(chan *http.Response, 1)
			path := accountsPath(slug, "/", names[1], "/deactivation")
			go func() { answered <- admin.request(http.MethodPut, path, nil) }()
			waitForTenantLock(t, tenant, answered)
			require.NoError(t, f.Exec(ctx, `UPDATE users SET deactivated_at = now() WHERE id = $1`, actor))
			release()
			res := <-answered

			if tc.admins == 2 {
				assertProblem(t, res, http.StatusConflict, "last_admin")
				assert.False(t, scalar[bool](t, `SELECT deactivated_at IS NOT NULL FROM users WHERE id = $1`, target), "the person stays active")
				assert.Equal(t, http.StatusOK, person.get("/api/v1/me").StatusCode, "no session ended")
				assert.Equal(t, http.StatusOK, s.do(t, caller{Token: token}, http.MethodGet, "/api/v1/me", nil).StatusCode, "no token revoked")
				assert.Zero(t, scalar[int64](t, deactivations, target), "no act recorded")
				return
			}
			require.Equal(t, http.StatusNoContent, res.StatusCode, "the third administrator remains")
			assertProblem(t, person.get("/api/v1/me"), http.StatusUnauthorized, "unauthenticated")
			assertProblem(t, s.do(t, caller{Token: token}, http.MethodGet, "/api/v1/me", nil), http.StatusUnauthorized, "token_revoked")
			assert.EqualValues(t, 1, scalar[int64](t, deactivations, target))
		})
	}
}

// docs/adr/0034 D1: two administrators who deactivate each other at the same
// moment are decided one after the other. One goes through; the other meets
// last_admin — or, authenticated only after the first committed, finds its
// session ended. The tenant keeps an administrator who can log in.
func TestTwoAdministratorsCannotDeactivateEachOther(t *testing.T) {
	ctx := context.Background()
	f := fixtures(t)
	s := newAPI(t, withLogin)
	refusals := 0
	for round := range 8 {
		slug := uniqueSlug("pair")
		tenant, err := f.Tenant(ctx, slug, "Pair")
		require.NoError(t, err)
		_, names := tenantAdmins(t, tenant, 2)
		browsers := []*browser{s.browser(t), s.browser(t)}
		for i, b := range browsers {
			b.mustLogin(names[i], testPassword)
		}
		problems := make([]string, 2)
		deactivate := func(i int) func() int {
			return func() int {
				res := browsers[i].request(http.MethodPut, accountsPath(slug, "/", names[1-i], "/deactivation"), nil)
				var body struct {
					Code string `json:"code"`
				}
				_ = json.NewDecoder(res.Body).Decode(&body)
				problems[i] = body.Code
				return res.StatusCode
			}
		}
		codes := simultaneously(deactivate(0), deactivate(1))
		winner := slices.Index(codes, http.StatusNoContent)
		require.NotEqual(t, -1, winner, "round %d: one goes through, %v", round, codes)
		switch loser := 1 - winner; codes[loser] {
		case http.StatusConflict:
			assert.Equal(t, "last_admin", problems[loser], "round %d", round)
			refusals++
		case http.StatusUnauthorized:
			assert.Equal(t, "unauthenticated", problems[loser], "round %d", round)
		default:
			assert.Failf(t, "the other is refused", "round %d: %v", round, codes)
		}
		assert.EqualValues(t, 1, scalar[int64](t, `SELECT count(*) FROM memberships m JOIN users u ON u.id = m.user_id
			WHERE m.tenant_id = $1 AND m.role = 'admin' AND u.deactivated_at IS NULL`, tenant), "round %d: the tenant keeps an administrator", round)
	}
	assert.Positive(t, refusals, "a round in which both were authenticated before either committed")
}

// docs/adr/0033 D1: a lock someone caused against a name before an account had
// it is not the account's.
func TestAnAccountDoesNotInheritTheLockOfItsName(t *testing.T) {
	w := newWorld(t)
	names := withAccounts(t, w)
	s := newAPI(t, withLogin)
	name := uniqueSlug("later")
	for range 5 {
		assertProblem(t, s.browser(t).login(name, "wrong password!"), http.StatusUnauthorized, "invalid_credentials")
	}
	admin := s.browser(t)
	admin.mustLogin(names["adminA"], testPassword)
	require.Equal(t, http.StatusCreated, admin.request(http.MethodPost, accountsPath(w.SlugA), map[string]string{
		"username": name, "display_name": "Later", "temporary_password": "temporary secret 1", "role": "member"}).StatusCode)
	assert.Equal(t, http.StatusOK, s.browser(t).login(name, "temporary secret 1").StatusCode)
}

// docs/adr/0045 D3, D4: an Idempotency-Key sent by a browser session is scoped
// to the person — the session has no token — and a repetition replays the
// stored answer instead of acting twice.
func TestASessionsIdempotencyKeyIsScopedToThePerson(t *testing.T) {
	w := newWorld(t)
	names := withAccounts(t, w)
	s := newAPI(t, withLogin)
	b := s.browser(t)
	b.mustLogin(names["memberA"], testPassword)
	other := s.browser(t)
	other.mustLogin(names["adminA"], testPassword)
	key := uuid.Must(uuid.NewV7()).String()
	path := "/api/v1/teams/" + w.SlugA + "/projects"
	post := func(who *browser, keyValue, projectKey string) *http.Response {
		return who.request(http.MethodPost, path, map[string]string{"key": projectKey, "name": "Keyed"}, withHeader("Idempotency-Key", keyValue))
	}

	first := post(b, key, "KEYA")
	require.Equal(t, http.StatusCreated, first.StatusCode)
	id := decode[apigen.Project](t, first).Id
	replay := post(b, key, "KEYA")
	require.Equal(t, http.StatusCreated, replay.StatusCode, "the stored answer")
	assert.Equal(t, id, decode[apigen.Project](t, replay).Id)
	assert.EqualValues(t, 1, scalar[int64](t, `SELECT count(*) FROM projects WHERE tenant_id = $1 AND key = 'KEYA'`, w.A))
	assertProblem(t, post(b, key, "KEYB"), http.StatusUnprocessableEntity, "idempotency_mismatch")

	// Another person with the same key is another caller.
	mine := post(other, key, "KEYC")
	require.Equal(t, http.StatusCreated, mine.StatusCode)
	assert.NotEqual(t, id, decode[apigen.Project](t, mine).Id)
	assert.EqualValues(t, 2, scalar[int64](t, `SELECT count(*) FROM idempotency_keys WHERE key = $1::uuid AND token_id IS NULL`, key))
}
