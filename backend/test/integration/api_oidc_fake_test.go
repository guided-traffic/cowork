//go:build integration

package integration

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api"
	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/test/fakeissuer"
)

// fakeWorld is a tenant whose mapping makes a group of its own members, an
// issuer in the test's process whose person is in that group and behind the
// gate, and a server with a clock the test moves.
type fakeWorld struct {
	is      *fakeissuer.Issuer
	s       apiServer
	clock   *clock
	slug    string
	tenant  uuid.UUID
	group   string
	subject string
}

func newFakeWorld(t *testing.T) fakeWorld {
	t.Helper()
	ctx := context.Background()
	f := fixtures(t)
	w := fakeWorld{slug: uniqueSlug("fake"), group: uniqueSlug("team"), subject: uniqueSlug("sub")}
	var err error
	w.tenant, err = f.Tenant(ctx, w.slug, "Fake")
	require.NoError(t, err)
	require.NoError(t, f.Exec(ctx, `INSERT INTO group_mappings (tenant_id, group_name, role) VALUES ($1, $2, 'member')`, w.tenant, w.group))
	w.is = fakeissuer.Start(t)
	w.is.Add(fakeissuer.User{Subject: w.subject, Email: w.subject + "@example.com", EmailVerified: true, Name: "Fay",
		Groups: []string{"cowork-users", "cowork-admins", w.group}})
	w.clock = newClock()
	w.s = newAPI(t, withLogin, devGate(fakeProvider(t, w.is)), withClock(w.clock))
	return w
}

func (w fakeWorld) login(t *testing.T) *browser {
	t.Helper()
	b := w.s.browser(t)
	res := b.oidcLogin(w.subject+"@example.com", "/")
	require.Equal(t, "/", res.Header.Get("Location"))
	return b
}

func (w fakeWorld) person(t *testing.T) uuid.UUID {
	t.Helper()
	return scalar[uuid.UUID](t, `SELECT id FROM users WHERE oidc_subject = $1 AND oidc_issuer = $2`, w.subject, w.is.URL)
}

func (w fakeWorld) refreshes() int {
	w.is.Lock()
	defer w.is.Unlock()
	return w.is.Refreshes
}

func (w fakeWorld) turn(knob func(*fakeissuer.Issuer)) {
	w.is.Lock()
	defer w.is.Unlock()
	knob(w.is)
}

func roleIn(me apigen.Me, slug string) apigen.Role {
	for _, m := range me.Memberships {
		if m.Tenant.Slug == slug {
			return m.Role
		}
	}
	return ""
}

// docs/adr/0030 D1, D2, D5: the refresh reads the groups again, and what
// changed changes at once — the mapped membership goes, and the administrator
// flag with the administrator group — each recorded with the cause refresh.
func TestRefreshFollowsTheIssuersGroups(t *testing.T) {
	w := newFakeWorld(t)
	b := w.login(t)
	me := decode[apigen.Me](t, b.get("/api/v1/me"))
	assert.Equal(t, apigen.RoleMember, roleIn(me, w.slug))
	assert.True(t, me.GlobalAdmin)

	w.is.SetGroups(w.subject, []string{"cowork-users"})
	w.clock.Advance(10 * time.Minute)
	require.Equal(t, http.StatusOK, b.get("/api/v1/me").StatusCode)
	assert.Zero(t, w.refreshes(), "not before the interval")

	w.clock.Advance(6 * time.Minute)
	me = decode[apigen.Me](t, b.get("/api/v1/me"))
	assert.Equal(t, 1, w.refreshes())
	assert.Empty(t, roleIn(me, w.slug), "the group left, and the membership with it")
	assert.False(t, me.GlobalAdmin, "the administrator group left")
	person := w.person(t)
	assert.EqualValues(t, 1, scalar[int64](t, `SELECT count(*) FROM audit_events WHERE tenant_id = $1 AND entity_type = 'membership'
		AND action = 'deleted' AND reason = 'refresh' AND actor_system = 'system:identity-provider'`, w.tenant))
	assert.EqualValues(t, 1, scalar[int64](t, `SELECT count(*) FROM audit_events WHERE entity_id = $1 AND action = 'updated'
		AND reason = 'refresh' AND (after->>'global_admin')::boolean = false`, person))
	hash := auth.HashSession(b.Cookie)
	assert.EqualValues(t, 1, scalar[int64](t, `SELECT count(*) FROM sessions WHERE token_hash = $1 AND groups = '{cowork-users}'`, hash[:]))

	require.Equal(t, http.StatusOK, b.get("/api/v1/me").StatusCode)
	assert.Equal(t, 1, w.refreshes(), "once per interval")
}

// docs/adr/0030 D5: an issuer that cannot be reached leaves the session
// served — a dropped connection, a 5xx, a 4xx that is no OAuth error — and the
// next attempt waits a minute.
func TestAnUnreachableIssuerLeavesTheSessionServed(t *testing.T) {
	for name, knob := range map[string]func(*fakeissuer.Issuer){
		"a dropped connection": func(is *fakeissuer.Issuer) { is.CloseOnRefresh = true },
		"a 503":                func(is *fakeissuer.Issuer) { is.RefreshStatus = http.StatusServiceUnavailable },
		"a 429, no OAuth error": func(is *fakeissuer.Issuer) {
			is.RefreshStatus = http.StatusTooManyRequests
		},
	} {
		t.Run(name, func(t *testing.T) {
			w := newFakeWorld(t)
			b := w.login(t)
			w.turn(knob)
			w.clock.Advance(16 * time.Minute)
			require.Equal(t, http.StatusOK, b.get("/api/v1/me").StatusCode, "served")
			assert.Equal(t, 1, w.refreshes())
			require.Equal(t, http.StatusOK, b.get("/api/v1/me").StatusCode)
			assert.Equal(t, 1, w.refreshes(), "not again within the minute")
			w.clock.Advance(61 * time.Second)
			require.Equal(t, http.StatusOK, b.get("/api/v1/me").StatusCode)
			assert.Equal(t, 2, w.refreshes(), "again after it")

			w.turn(func(is *fakeissuer.Issuer) { is.CloseOnRefresh, is.RefreshStatus = false, 0 })
			w.clock.Advance(61 * time.Second)
			require.Equal(t, http.StatusOK, b.get("/api/v1/me").StatusCode)
			assert.Equal(t, 3, w.refreshes(), "and the issuer back")
			hash := auth.HashSession(b.Cookie)
			assert.EqualValues(t, 1, scalar[int64](t, `SELECT count(*) FROM sessions WHERE token_hash = $1 AND refresh_retry_at IS NULL`, hash[:]))
		})
	}
}

// The coordinator's rule (2026-10-04): an OAuth error answer of the token
// endpoint refuses the refresh token, and the session ends, recorded with the
// cause identity-provider.
func TestARefusedRefreshTokenEndsTheSession(t *testing.T) {
	for _, code := range []string{"invalid_grant", "invalid_request"} {
		t.Run(code, func(t *testing.T) {
			w := newFakeWorld(t)
			b := w.login(t)
			other := w.login(t)
			w.turn(func(is *fakeissuer.Issuer) { is.RefreshError = code })
			w.clock.Advance(16 * time.Minute)
			assertProblem(t, b.get("/api/v1/me"), http.StatusUnauthorized, "unauthenticated")
			hash := auth.HashSession(other.Cookie)
			assert.EqualValues(t, 1, scalar[int64](t, `SELECT count(*) FROM sessions WHERE token_hash = $1`, hash[:]),
				"the refused session ends, not the person's others")
			assert.EqualValues(t, 1, scalar[int64](t, `SELECT count(*) FROM audit_events WHERE entity_id = $1 AND action = 'revoked'
				AND reason = 'identity-provider' AND (after->>'sessions_ended')::int = 1`, w.person(t)))
		})
	}
}

// docs/adr/0030 D5: without a refresh token there is nothing to ask the issuer;
// the groups of the login hold, judged against the gate as it is now.
func TestWithoutARefreshTokenTheLoginsGroupsHold(t *testing.T) {
	w := newFakeWorld(t)
	w.turn(func(is *fakeissuer.Issuer) { is.NoRefreshToken = true })
	b := w.login(t)
	hash := auth.HashSession(b.Cookie)
	assert.EqualValues(t, 1, scalar[int64](t, `SELECT count(*) FROM sessions WHERE token_hash = $1 AND refresh_token_sealed IS NULL`, hash[:]))
	w.clock.Advance(16 * time.Minute)
	require.Equal(t, http.StatusOK, b.get("/api/v1/me").StatusCode)
	assert.Zero(t, w.refreshes())

	narrower := newAPI(t, withLogin, withIdentity(fakeProvider(t, w.is), []string{"someone-else"}, ""), withClock(w.clock))
	onNarrower := &browser{t: t, s: narrower, Cookie: b.Cookie}
	w.clock.Advance(16 * time.Minute)
	assertProblem(t, onNarrower.get("/api/v1/me"), http.StatusUnauthorized, "unauthenticated")
}

// docs/adr/0031 D4: a session of the identity provider whose issuer names an
// end_session_endpoint answers where the browser ends its session there too.
func TestLogoutAtTheIssuer(t *testing.T) {
	w := newFakeWorld(t)
	w.turn(func(is *fakeissuer.Issuer) { is.EndSession = true })
	s := newAPI(t, withLogin, devGate(fakeProvider(t, w.is)))
	b := s.browser(t)
	require.Equal(t, "/", b.oidcLogin(w.subject+"@example.com", "/").Header.Get("Location"))
	res := b.request(http.MethodPost, "/auth/logout", nil)
	require.Equal(t, http.StatusOK, res.StatusCode)
	end, err := url.Parse(decode[apigen.LogoutResult](t, res).EndSessionUrl)
	require.NoError(t, err)
	assert.Equal(t, w.is.URL+"/logout", end.Scheme+"://"+end.Host+end.Path)
	assert.Equal(t, fakeissuer.ClientID, end.Query().Get("client_id"))
	assert.Equal(t, testOrigin+"/login", end.Query().Get("post_logout_redirect_uri"))
	cleared, ok := cookieValue(res, auth.SessionCookie)
	assert.True(t, ok)
	assert.Empty(t, cleared)
	assertProblem(t, b.get("/api/v1/me"), http.StatusUnauthorized, "unauthenticated")
}

// docs/adr/0029 D1: every failure of the callback sends the browser to the
// login page with oidc_failed and clears the state cookie: no cookie, a state
// that is not the login's, an issuer's error, a stale login, an ID token of a
// wrong key, a code used twice.
func TestCallbackFailures(t *testing.T) {
	w := newFakeWorld(t)
	failed := "/login?error=oidc_failed"
	start := func(b *browser) (string, string) {
		res := b.get("/auth/oidc/login")
		state, _ := cookieValue(res, stateCookieName)
		return state, walkIssuer(t, res.Header.Get("Location"), "")
	}
	query := func(back string) url.Values {
		u, err := url.Parse(back)
		require.NoError(t, err)
		return u.Query()
	}

	b := w.s.browser(t)
	_, back := start(b)
	res := b.get("/auth/callback?" + query(back).Encode())
	assert.Equal(t, failed, res.Header.Get("Location"), "no state cookie")
	_, cleared := cookieValue(res, stateCookieName)
	assert.True(t, cleared)

	state, back := start(b)
	q := query(back)
	q.Set("state", "not-the-state")
	assert.Equal(t, failed, b.get("/auth/callback?"+q.Encode(), withState(state)).Header.Get("Location"), "another state")

	state, back = start(b)
	q = query(back)
	q.Del("code")
	q.Set("error", "access_denied")
	assert.Equal(t, failed, b.get("/auth/callback?"+q.Encode(), withState(state)).Header.Get("Location"), "the issuer's error")

	res = b.get("/auth/oidc/login?return_to=" + url.QueryEscape("/t/x/backlog"))
	state, _ = cookieValue(res, stateCookieName)
	back = walkIssuer(t, res.Header.Get("Location"), "")
	w.clock.Advance(11 * time.Minute)
	assert.Equal(t, failed+"&return=%2Ft%2Fx%2Fbacklog", b.get("/auth/callback?"+query(back).Encode(), withState(state)).Header.Get("Location"),
		"a stale login, which still knows where the person wanted to go")

	w.turn(func(is *fakeissuer.Issuer) { is.WrongKey = true })
	state, back = start(b)
	assert.Equal(t, failed, b.get("/auth/callback?"+query(back).Encode(), withState(state)).Header.Get("Location"), "a wrong key")
	w.turn(func(is *fakeissuer.Issuer) { is.WrongKey = false })

	state, back = start(b)
	assert.Equal(t, "/", b.get("/auth/callback?"+query(back).Encode(), withState(state)).Header.Get("Location"))
	assert.Equal(t, failed, b.get("/auth/callback?"+query(back).Encode(), withState(state)).Header.Get("Location"), "a code used twice")
}

// docs/adr/0024 D5: a deactivated person is refused; the login of an address
// off the site returns to "/".
func TestADeactivatedPersonIsRefused(t *testing.T) {
	w := newFakeWorld(t)
	b := w.s.browser(t)
	res := b.oidcLogin(w.subject+"@example.com", "//evil.example.com/path")
	assert.Equal(t, "/", res.Header.Get("Location"), "a return_to off the site is /")
	require.NoError(t, fixtures(t).Exec(context.Background(), `UPDATE users SET deactivated_at = now() WHERE id = $1`, w.person(t)))
	assert.Equal(t, "/login?error=not_allowed", w.s.browser(t).oidcLogin(w.subject+"@example.com", "/").Header.Get("Location"))
	assert.EqualValues(t, 1, scalar[int64](t, `SELECT count(*) FROM audit_events WHERE entity_id = $1 AND action = 'login_refused'
		AND reason = 'not_allowed' AND note = 'deactivated'`, w.person(t)))
}

// docs/adr/0030 D2, D7, docs/adr/0034 D1: the editor of a mapping sees which
// mappings hold their own groups, and the change of the one that makes them
// the tenant's only administrator is refused; with another administrator it
// goes through and changes their own role at once.
func TestTheMappingEditorsOwnRole(t *testing.T) {
	ctx := context.Background()
	w := newFakeWorld(t)
	f := fixtures(t)
	adminGroup := uniqueSlug("admins")
	require.NoError(t, f.Exec(ctx, `INSERT INTO group_mappings (tenant_id, group_name, role) VALUES ($1, $2, 'admin')`, w.tenant, adminGroup))
	w.is.SetGroups(w.subject, []string{"cowork-users", adminGroup})
	b := w.login(t)
	assert.Equal(t, apigen.RoleAdmin, roleIn(decode[apigen.Me](t, b.get("/api/v1/me")), w.slug))

	list := decode[apigen.GroupMappingList](t, b.get("/api/v1/tenants/"+w.slug+"/group-mappings"))
	require.Len(t, list.Items, 2)
	var own apigen.GroupMapping
	for _, m := range list.Items {
		assert.Equal(t, m.Group == adminGroup, m.IncludesCaller, m.Group)
		if m.Group == adminGroup {
			own = m
		}
	}
	tag := `"` + strconv.Itoa(own.Version) + `"`
	demote := b.request(http.MethodPatch, "/api/v1/tenants/"+w.slug+"/group-mappings/"+own.Id.String(),
		map[string]string{"role": "member"}, withHeader("If-Match", tag))
	assertProblem(t, demote, http.StatusConflict, "last_admin")

	other, err := f.Person(ctx, uniqueSlug("other"), "Other")
	require.NoError(t, err)
	require.NoError(t, f.Account(ctx, other, testPassword, w.tenant, false))
	added := b.request(http.MethodPost, "/api/v1/tenants/"+w.slug+"/members", map[string]string{"person": usernameOf(t, other), "role": "admin"})
	require.Equal(t, http.StatusCreated, added.StatusCode)
	assert.True(t, decode[apigen.Member](t, added).Local)

	demote = b.request(http.MethodPatch, "/api/v1/tenants/"+w.slug+"/group-mappings/"+own.Id.String(),
		map[string]string{"role": "member"}, withHeader("If-Match", tag))
	require.Equal(t, http.StatusOK, demote.StatusCode)
	assert.Equal(t, `"`+strconv.Itoa(own.Version+1)+`"`, demote.Header.Get("ETag"))
	assert.Equal(t, apigen.RoleMember, roleIn(decode[apigen.Me](t, b.get("/api/v1/me")), w.slug), "the editor's own role changed at once")
	assert.EqualValues(t, 1, scalar[int64](t, `SELECT count(*) FROM audit_events WHERE tenant_id = $1 AND entity_type = 'membership'
		AND action = 'updated' AND reason = 'mapping' AND actor_system = 'system:identity-provider'`, w.tenant))
}

// docs/adr/0031 D1, D7, the spec of the login: no code, ID token, access or
// refresh token the issuer handed out reaches a log line or an audit row —
// through a login, a refresh, an issuer that cannot be reached and one that
// refuses — and the stored refresh token is sealed.
func TestNoIssuerSecretIsLoggedOrRecorded(t *testing.T) {
	logs := &recordingLogger{}
	w := newFakeWorld(t)
	s := newAPI(t, withLogin, devGate(fakeProvider(t, w.is)), withClock(w.clock), func(o *api.Options) { o.Logger = logs.logger() })
	b := s.browser(t)
	require.Equal(t, "/", b.oidcLogin(w.subject+"@example.com", "/").Header.Get("Location"))
	w.clock.Advance(16 * time.Minute)
	require.Equal(t, http.StatusOK, b.get("/api/v1/me").StatusCode)
	hash := auth.HashSession(b.Cookie)
	stored := scalar[[]byte](t, `SELECT refresh_token_sealed FROM sessions WHERE token_hash = $1`, hash[:])
	require.NotEmpty(t, stored)
	for _, secret := range w.is.Issued() {
		assert.NotContains(t, string(stored), secret, "the refresh token is sealed, never in clear")
	}
	w.turn(func(is *fakeissuer.Issuer) { is.CloseOnRefresh = true })
	w.clock.Advance(16 * time.Minute)
	require.Equal(t, http.StatusOK, b.get("/api/v1/me").StatusCode)
	w.turn(func(is *fakeissuer.Issuer) { is.CloseOnRefresh, is.RefreshError = false, "invalid_grant" })
	w.clock.Advance(2 * time.Minute)
	assertProblem(t, b.get("/api/v1/me"), http.StatusUnauthorized, "unauthenticated")

	secrets := w.is.Issued()
	require.GreaterOrEqual(t, len(secrets), 5, "a code, two answers' tokens")
	text := logs.text()
	require.Contains(t, text, "refresh", "the log has the reasons")
	for _, secret := range secrets {
		assert.NotContains(t, text, secret, "a secret of the issuer in the log")
		assert.EqualValues(t, 0, scalar[int64](t, `SELECT count(*) FROM audit_events WHERE
			coalesce(before::text, '') || coalesce(after::text, '') || coalesce(reason, '') || coalesce(note, '') LIKE '%' || $1 || '%'`, secret))
	}
}

// docs/adr/0035 D8, docs/adr/0030 D5: the moment the issuer says a person is
// outside the gate — at a refresh, or at a login it refuses — their tokens meet
// the gate at their next request, not one refresh interval later, and their
// sessions end.
func TestLeavingTheGateStopsTheTokensAtOnce(t *testing.T) {
	for _, how := range []string{"at a refresh", "at a refused login"} {
		t.Run(how, func(t *testing.T) {
			w := newFakeWorld(t)
			b := w.login(t)
			created := b.request(http.MethodPost, "/api/v1/me/tokens", map[string]any{"name": "script", "scope": "read"})
			require.Equal(t, http.StatusCreated, created.StatusCode)
			token := caller{Token: *decode[apigen.TokenCreated](t, created).Token}
			me := func() *http.Response { return w.s.do(t, token, http.MethodGet, "/api/v1/me", nil) }
			w.is.SetGroups(w.subject, []string{})

			if how == "at a refresh" {
				w.clock.Advance(16 * time.Minute)
				require.Equal(t, http.StatusOK, me().StatusCode, "the token's check, on the groups of the login")
				assertProblem(t, b.get("/api/v1/me"), http.StatusUnauthorized, "unauthenticated")
			} else {
				require.Equal(t, http.StatusOK, me().StatusCode)
				assert.Equal(t, "/login?error=not_allowed", w.s.browser(t).oidcLogin(w.subject+"@example.com", "/").Header.Get("Location"))
				assertProblem(t, b.get("/api/v1/me"), http.StatusUnauthorized, "unauthenticated")
				assert.EqualValues(t, 1, scalar[int64](t, `SELECT count(*) FROM memberships WHERE tenant_id = $1`, w.tenant),
					"a refused login derives no membership (m5): the person cannot use it, and keeps it for their return")
				assert.EqualValues(t, 1, scalar[int64](t, `SELECT count(*) FROM users WHERE id = $1 AND oidc_groups = '{}'
					AND gate_checked_at IS NULL`, w.person(t)), "the groups the issuer said are kept, and the gate's check cleared")
			}
			assertProblem(t, me(), http.StatusUnauthorized, "not_allowed")
		})
	}
}
