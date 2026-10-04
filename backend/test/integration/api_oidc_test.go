//go:build integration

package integration

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/domain"
)

// dexWorld is an installation of its own with make dev's shape: the tenant
// dev, whose mapping makes team-red members, and a local account that
// administers it. Each test that logs Dex's users in gets one, because their
// persons — one per subject — outlive a test.
type dexWorld struct {
	iso      isolated
	tenant   uuid.UUID
	adminSID string // the local administrator's username
}

func newDexWorld(t *testing.T) dexWorld {
	t.Helper()
	ctx := context.Background()
	iso := newIsolated(t)
	tenant, err := iso.F.Tenant(ctx, "dev", "Dev")
	require.NoError(t, err)
	require.NoError(t, iso.F.Exec(ctx, `INSERT INTO group_mappings (tenant_id, group_name, role) VALUES ($1, 'team-red', 'member')`, tenant))
	root, err := iso.F.Person(ctx, "root", "Root")
	require.NoError(t, err)
	require.NoError(t, iso.F.Account(ctx, root, testPassword, tenant, false))
	require.NoError(t, iso.F.Member(ctx, tenant, root, domain.RoleAdmin))
	return dexWorld{iso: iso, tenant: tenant, adminSID: "root"}
}

func (d dexWorld) count(t *testing.T, sql string, args ...any) int64 {
	t.Helper()
	n, err := d.iso.F.QueryCount(context.Background(), sql, args...)
	require.NoError(t, err)
	return n
}

// bob is the person Dex's bob became at his first login.
func (d dexWorld) bob(t *testing.T) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	require.NoError(t, d.iso.F.QueryRow(context.Background(), `SELECT id FROM users WHERE email = 'bob@example.com'`).Scan(&id))
	return id
}

// docs/adr/0029 D1, D5, docs/adr/0030 D1–D4, D6, docs/adr/0031 D1, D2, D5:
// the four users of the test issuer. bob is behind the gate and a member of dev
// by the mapping; cyd is behind the gate in no mapped group and becomes a
// member by an administrator's grant; bob with a grant beside his mapping has
// the higher role of the two; ada is a global administrator by the
// administrator group; dan is outside the gate and refused, whatever the
// mapping of his group says.
func TestLoginThroughDex(t *testing.T) {
	d := newDexWorld(t)
	s := newAPI(t, withLogin, d.iso.option, devGate(dexProvider(t)))

	options := decode[map[string]any](t, s.browser(t).get("/auth/options"))
	assert.Equal(t, true, options["oidc"])
	assert.Equal(t, "Dex", options["oidc_name"])

	bob := s.browser(t)
	res := bob.oidcLogin("bob@example.com", "/t/dev/backlog?state=open")
	require.Equal(t, http.StatusSeeOther, res.StatusCode)
	assert.Equal(t, "/t/dev/backlog?state=open", res.Header.Get("Location"), "back to the page the person wanted")
	var session, cleared *http.Cookie
	for _, c := range res.Cookies() {
		switch c.Name {
		case auth.SessionCookie:
			session = c
		case stateCookieName:
			cleared = c
		}
	}
	require.NotNil(t, session)
	assert.True(t, session.HttpOnly && session.Secure && session.Path == "/" && session.SameSite == http.SameSiteLaxMode)
	require.NotNil(t, cleared, "the state cookie is cleared")
	assert.Less(t, cleared.MaxAge, 0)

	me := decode[apigen.Me](t, bob.get("/api/v1/me"))
	bobID := d.bob(t)
	assert.Equal(t, bobID, me.Id)
	assert.Equal(t, "bob", me.DisplayName, "Dex's name claim")
	assert.False(t, me.Local)
	assert.False(t, me.GlobalAdmin)
	require.Len(t, me.Memberships, 1)
	assert.Equal(t, apigen.RoleMember, me.Memberships[0].Role)
	assert.Equal(t, []apigen.MembershipOrigin{{Source: apigen.MembershipSourceMapping, Role: apigen.RoleMember}}, me.Memberships[0].Origins)

	assert.EqualValues(t, 1, d.count(t, `SELECT count(*) FROM users WHERE id = $1 AND username IS NULL AND email_verified
		AND oidc_groups = '{cowork-users,team-red}' AND oidc_issuer = $2`, bobID, env.OIDCIssuer))
	assert.EqualValues(t, 1, d.count(t, `SELECT count(*) FROM sessions WHERE user_id = $1 AND method = 'oidc'
		AND groups = '{cowork-users,team-red}' AND refresh_token_sealed IS NOT NULL`, bobID), "the refresh token is kept, sealed")
	assert.EqualValues(t, 1, d.count(t, `SELECT count(*) FROM audit_events WHERE tenant_id IS NULL AND entity_id = $1
		AND action = 'created' AND actor_system = 'system:identity-provider' AND reason = 'login'`, bobID), "the provider made the person")
	assert.EqualValues(t, 1, d.count(t, `SELECT count(*) FROM audit_events WHERE tenant_id = $1 AND entity_type = 'membership'
		AND action = 'created' AND actor_system = 'system:identity-provider' AND reason = 'login' AND after->>'source' = 'mapping'`, d.tenant))
	assert.EqualValues(t, 1, d.count(t, `SELECT count(*) FROM audit_events WHERE tenant_id IS NULL AND actor_user_id = $1
		AND action = 'logged_in' AND note = 'oidc' AND octet_length(source_hash) = 32`, bobID), "the login is the person's act, with the source hash")

	cyd := s.browser(t)
	require.Equal(t, "/", cyd.oidcLogin("cyd@example.com", "/").Header.Get("Location"))
	assert.Empty(t, decode[apigen.Me](t, cyd.get("/api/v1/me")).Memberships, "behind the gate, in no mapped group")

	admin := s.browser(t)
	admin.mustLogin(d.adminSID, testPassword)
	added := admin.request(http.MethodPost, "/api/v1/tenants/dev/members", map[string]string{"person": "CYD@example.com", "role": "viewer"})
	require.Equal(t, http.StatusCreated, added.StatusCode)
	member := decode[apigen.Member](t, added)
	assert.Equal(t, []apigen.MembershipOrigin{{Source: apigen.MembershipSourceGrant, Role: apigen.RoleViewer}}, member.Origins)
	assert.Equal(t, apigen.RoleViewer, decode[apigen.Me](t, cyd.get("/api/v1/me")).Memberships[0].Role)

	both := admin.request(http.MethodPut, "/api/v1/tenants/dev/members/"+bobID.String()+"/grant", map[string]string{"role": "admin"})
	require.Equal(t, http.StatusOK, both.StatusCode)
	view := decode[apigen.Member](t, both)
	assert.Equal(t, apigen.RoleAdmin, view.Role, "the higher of the mapped and the granted role")
	assert.Equal(t, []apigen.MembershipOrigin{{Source: apigen.MembershipSourceMapping, Role: apigen.RoleMember},
		{Source: apigen.MembershipSourceGrant, Role: apigen.RoleAdmin}}, view.Origins)
	require.Equal(t, http.StatusSeeOther, bob.oidcLogin("bob@example.com", "/").StatusCode, "a new login")
	assert.Equal(t, apigen.RoleAdmin, decode[apigen.Me](t, bob.get("/api/v1/me")).Memberships[0].Role, "a login never overrides a grant")

	ada := s.browser(t)
	require.Equal(t, "/", ada.oidcLogin("ada@example.com", "/").Header.Get("Location"))
	me = decode[apigen.Me](t, ada.get("/api/v1/me"))
	assert.True(t, me.GlobalAdmin, "the administrator group")
	assert.Empty(t, me.Memberships, "and no role in a tenant it was not given (docs/adr/0034 D2)")

	dan := s.browser(t)
	refused := dan.oidcLogin("dan@example.com", "/")
	assert.Equal(t, "/login?error=not_allowed", refused.Header.Get("Location"))
	_, set := cookieValue(refused, auth.SessionCookie)
	assert.False(t, set, "no session")
	assert.EqualValues(t, 0, d.count(t, `SELECT count(*) FROM users WHERE email = 'dan@example.com'`), "no person is made")
	assert.EqualValues(t, 1, d.count(t, `SELECT count(*) FROM audit_events WHERE action = 'login_refused' AND reason = 'not_allowed'
		AND entity_id IS NULL AND tenant_id IS NULL`), "the refusal is recorded, without a person")
	assertProblem(t, dan.get("/api/v1/me"), http.StatusUnauthorized, "unauthenticated")
	assert.Equal(t, "/login?error=not_allowed&return=%2Ft%2Fdev%2Fbacklog", dan.oidcLogin("dan@example.com", "/t/dev/backlog").Header.Get("Location"),
		"a refusal keeps the path the person wanted")
}

// docs/adr/0032 D5: while no tenant exists only the administrator group's
// members log in; anyone else behind the gate is not_initialised, and no person
// is made of them.
func TestInitStateThroughDex(t *testing.T) {
	iso := newIsolated(t)
	s := newAPI(t, withLogin, iso.option, devGate(dexProvider(t)))

	bob := s.browser(t)
	assert.Equal(t, "/login?error=not_initialised", bob.oidcLogin("bob@example.com", "/").Header.Get("Location"))
	n, err := iso.F.QueryCount(context.Background(), `SELECT count(*) FROM users`)
	require.NoError(t, err)
	assert.Zero(t, n, "no person")

	ada := s.browser(t)
	require.Equal(t, "/", ada.oidcLogin("ada@example.com", "/").Header.Get("Location"))
	assert.True(t, decode[apigen.Me](t, ada.get("/api/v1/me")).GlobalAdmin)
	created := ada.request(http.MethodPost, "/api/v1/tenants", map[string]string{"slug": "first", "name": "First"})
	require.Equal(t, http.StatusCreated, created.StatusCode, "the administrator group makes the first tenant")

	assert.Equal(t, "/", s.browser(t).oidcLogin("bob@example.com", "/").Header.Get("Location"), "initialised")
}

// docs/adr/0030 D5, docs/adr/0035 D8, the phase's verification: a person whose
// groups the allow-list no longer admits — here a second server over the same
// database, whose gate admits only cowork-admins and whose clock is past the
// refresh interval — loses every session at the next refresh, and their token
// is refused, not revoked: back behind a gate that admits them, it works.
func TestLeavingTheAllowList(t *testing.T) {
	d := newDexWorld(t)
	provider := dexProvider(t)
	clockA := newClock()
	a := newAPI(t, withLogin, d.iso.option, devGate(provider), withClock(clockA))
	bob := a.browser(t)
	require.Equal(t, http.StatusSeeOther, bob.oidcLogin("bob@example.com", "/").StatusCode)
	created := bob.request(http.MethodPost, "/api/v1/me/tokens", map[string]any{"name": "script", "scope": "read"})
	require.Equal(t, http.StatusCreated, created.StatusCode)
	token := *decode[apigen.TokenCreated](t, created).Token

	clockB := newClock()
	clockB.Advance(16 * time.Minute)
	b := newAPI(t, withLogin, d.iso.option, withIdentity(provider, []string{"cowork-admins"}, "cowork-admins"), withClock(clockB))
	assertProblem(t, b.do(t, caller{Token: token}, http.MethodGet, "/api/v1/me", nil), http.StatusUnauthorized, "not_allowed")
	assertProblem(t, b.do(t, caller{Token: token}, http.MethodGet, "/api/v1/tenants/dev/projects", nil), http.StatusUnauthorized, "not_allowed")
	onB := &browser{t: t, s: b, Cookie: bob.Cookie}
	assertProblem(t, onB.get("/api/v1/me"), http.StatusUnauthorized, "unauthenticated")
	assertProblem(t, bob.get("/api/v1/me"), http.StatusUnauthorized, "unauthenticated")
	bobID := d.bob(t)
	assert.EqualValues(t, 0, d.count(t, `SELECT count(*) FROM sessions WHERE user_id = $1`, bobID), "every session of the person ends")
	assert.EqualValues(t, 1, d.count(t, `SELECT count(*) FROM audit_events WHERE entity_id = $1 AND action = 'revoked'
		AND reason = 'gate' AND actor_system = 'system:identity-provider'`, bobID))
	assert.EqualValues(t, 0, d.count(t, `SELECT count(*) FROM tokens WHERE user_id = $1 AND revoked_at IS NOT NULL`, bobID), "the token is not revoked")

	clockA.Advance(16 * time.Minute)
	assert.Equal(t, http.StatusOK, a.do(t, caller{Token: token}, http.MethodGet, "/api/v1/me", nil).StatusCode,
		"behind a gate that admits the person, the token works again")
}

// docs/adr/0030 D5, the coordinator's rule after Dex (2026-10-04): a refresh
// token the issuer refuses — here a spent one, which Dex refuses with
// invalid_request — ends the session; a refresh that succeeds rotates the token.
func TestASpentRefreshTokenEndsTheSession(t *testing.T) {
	d := newDexWorld(t)
	c := newClock()
	s := newAPI(t, withLogin, d.iso.option, devGate(dexProvider(t)), withClock(c))
	bob := s.browser(t)
	require.Equal(t, http.StatusSeeOther, bob.oidcLogin("bob@example.com", "/").StatusCode)
	hash := auth.HashSession(bob.Cookie)
	sealed := func() []byte {
		var b []byte
		require.NoError(t, d.iso.F.QueryRow(context.Background(), `SELECT refresh_token_sealed FROM sessions WHERE token_hash = $1`, hash[:]).Scan(&b))
		return b
	}
	spent := sealed()

	c.Advance(16 * time.Minute)
	require.Equal(t, http.StatusOK, bob.get("/api/v1/me").StatusCode, "refreshed at the issuer")
	assert.NotEqual(t, spent, sealed(), "the issuer rotated the refresh token")

	require.NoError(t, d.iso.F.Exec(context.Background(), `UPDATE sessions SET refresh_token_sealed = $1 WHERE token_hash = $2`, spent, hash[:]))
	c.Advance(16 * time.Minute)
	assertProblem(t, bob.get("/api/v1/me"), http.StatusUnauthorized, "unauthenticated")
	assert.EqualValues(t, 1, d.count(t, `SELECT count(*) FROM audit_events WHERE entity_id = $1 AND action = 'revoked'
		AND reason = 'identity-provider'`, d.bob(t)))
}

// The phase's verification (docs/adr/0034 D1, D3, D4, docs/adr/0023 D5): a
// viewer cannot write, a member of one tenant cannot list another, and a
// member outside a restricted project cannot read it — not in the list, not
// by its key, not in a search, not on its board.
func TestRolesFromTheIdentityProviderHold(t *testing.T) {
	ctx := context.Background()
	d := newDexWorld(t)
	f := d.iso.F
	users, err := f.Tenant(ctx, "users", "Users")
	require.NoError(t, err)
	require.NoError(t, f.Exec(ctx, `INSERT INTO group_mappings (tenant_id, group_name, role) VALUES ($1, 'cowork-users', 'viewer')`, users))
	_, err = f.Project(ctx, users, "OPEN", "Open")
	require.NoError(t, err)
	secret, err := f.Project(ctx, d.tenant, "SECRET", "Secret")
	require.NoError(t, err)
	require.NoError(t, f.Exec(ctx, `UPDATE projects SET restricted = true WHERE id = $1`, secret))
	s := newAPI(t, withLogin, d.iso.option, devGate(dexProvider(t)))

	cyd := s.browser(t)
	require.Equal(t, http.StatusSeeOther, cyd.oidcLogin("cyd@example.com", "/").StatusCode)
	write := cyd.request(http.MethodPost, "/api/v1/tenants/users/projects/OPEN/tickets",
		map[string]any{"type": "task", "title": "x", "severity": "low", "security": "none", "effort": "S"})
	assertProblem(t, write, http.StatusForbidden, "forbidden")
	assert.Equal(t, http.StatusOK, cyd.get("/api/v1/tenants/users/projects").StatusCode, "a viewer reads")
	assertProblem(t, cyd.get("/api/v1/tenants/dev/projects"), http.StatusNotFound, "not_found")

	bob := s.browser(t)
	require.Equal(t, http.StatusSeeOther, bob.oidcLogin("bob@example.com", "/").StatusCode)
	var reporter uuid.UUID
	require.NoError(t, f.QueryRow(ctx, `SELECT id FROM users WHERE username = 'root'`).Scan(&reporter))
	_, _, err = f.Ticket(ctx, d.tenant, secret, reporter, "a secret plan")
	require.NoError(t, err)
	list := decode[apigen.ProjectList](t, bob.get("/api/v1/tenants/dev/projects"))
	for _, p := range list.Items {
		assert.NotEqual(t, "SECRET", p.Key, "not in the list")
	}
	assertProblem(t, bob.get("/api/v1/tenants/dev/projects/SECRET"), http.StatusNotFound, "not_found")
	assertProblem(t, bob.get("/api/v1/tenants/dev/projects/SECRET/tickets"), http.StatusNotFound, "not_found")
	hits := decode[apigen.TicketList](t, bob.get("/api/v1/tenants/dev/tickets?q=secret"))
	assert.Empty(t, hits.Items, "not in a search")

	admin := s.browser(t)
	admin.mustLogin(d.adminSID, testPassword)
	bobID := d.bob(t)
	entry := admin.request(http.MethodPut, "/api/v1/tenants/dev/projects/SECRET/access/"+bobID.String(), map[string]string{"role": "viewer"})
	require.Equal(t, http.StatusOK, entry.StatusCode)
	assert.Equal(t, http.StatusOK, bob.get("/api/v1/tenants/dev/projects/SECRET").StatusCode, "on the list, the project is there")
	assert.Len(t, decode[apigen.TicketList](t, bob.get("/api/v1/tenants/dev/tickets?q=secret")).Items, 1)
}
