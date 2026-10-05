//go:build integration

package integration

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api"
	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/test/fixture"
)

// docs/adr/0035 D1, D5: only the person, in a browser session, creates a token
// — a token never does — and the plaintext is in the answer once: the database
// holds its SHA-256, and no audit row, stored answer or list shows it.
func TestOnlyASessionCreatesATokenAndShowsItOnce(t *testing.T) {
	w := newWorld(t)
	names := withAccounts(t, w)
	tokens := issueTokens(t, w)
	c := newClock()
	s := newAPI(t, withLogin, withClock(c))
	body := map[string]any{"name": "laptop", "scope": "write"}

	for label, token := range map[string]string{"a write token": tokens.MemberA, "an admin token": tokens.AdminA, "an agent token": tokens.AgentA} {
		assertProblem(t, s.do(t, caller{Token: token}, http.MethodPost, "/api/v1/me/tokens", body), http.StatusForbidden, "session_required")
		_ = label
	}
	assertProblem(t, s.do(t, caller{}, http.MethodPost, "/api/v1/me/tokens", body), http.StatusUnauthorized, "unauthenticated")

	b := s.browser(t)
	b.mustLogin(names["memberA"], testPassword)
	res := b.request(http.MethodPost, "/api/v1/me/tokens", body)
	require.Equal(t, http.StatusCreated, res.StatusCode)
	created := decode[apigen.TokenCreated](t, res)
	plaintext := *created.Token
	assert.True(t, auth.WellFormedToken(plaintext))
	assert.Equal(t, "laptop", created.Name)
	assert.Equal(t, apigen.ScopeWrite, created.Scope)
	assert.False(t, created.Agent)
	assert.Empty(t, created.Capabilities)
	assert.Equal(t, apigen.TokenStateActive, created.State)
	assert.WithinDuration(t, c.Now().Add(90*24*time.Hour), created.ExpiresAt, time.Second, "ninety days by default (docs/adr/0035 D4)")

	// It works as its person's token — and, being a token, it cannot make one.
	me := s.do(t, caller{Token: plaintext}, http.MethodGet, "/api/v1/me", nil)
	require.Equal(t, http.StatusOK, me.StatusCode)
	assert.Equal(t, w.MemberA, decode[apigen.Me](t, me).Id)
	assertProblem(t, s.do(t, caller{Token: plaintext}, http.MethodPost, "/api/v1/me/tokens", body), http.StatusForbidden, "session_required")

	// Listed with its metadata; never again with the plaintext.
	list := decode[map[string]any](t, b.get("/api/v1/me/tokens"))
	var listed map[string]any
	for _, item := range list["items"].([]any) {
		if item.(map[string]any)["id"] == created.Id.String() {
			listed = item.(map[string]any)
		}
	}
	require.NotNil(t, listed)
	assert.NotContains(t, listed, "token")

	hash := auth.HashToken(plaintext)
	assert.EqualValues(t, 1, scalar[int64](t, `SELECT count(*) FROM tokens WHERE token_hash = $1 AND user_id = $2`, hash[:], w.MemberA))
	assert.EqualValues(t, 0, scalar[int64](t, `SELECT count(*) FROM tokens t WHERE t::text LIKE '%' || $1 || '%'`, plaintext), "the plaintext is in no column")
	assert.EqualValues(t, 0, scalar[int64](t, `SELECT count(*) FROM audit_events a WHERE a::text LIKE '%' || $1 || '%'`, plaintext))
	assert.EqualValues(t, 0, scalar[int64](t, `SELECT count(*) FROM idempotency_keys k WHERE k::text LIKE '%' || $1 || '%'`, plaintext))
	assert.EqualValues(t, 1, scalar[int64](t, `SELECT count(*) FROM audit_events WHERE action = 'created' AND entity_type = 'token' AND entity_id = $1
		AND actor_user_id = $2 AND token_id IS NULL AND tenant_id IS NULL`, created.Id, w.MemberA), "a recorded act of the person in a session")
}

// docs/adr/0035 D4: the lifetime is the default, or what the request asks for,
// and never more than the maximum — a longer one is shortened, and the answer
// says what the token got.
func TestATokensLifetimeIsClampedToTheMaximum(t *testing.T) {
	w := newWorld(t)
	names := withAccounts(t, w)
	c := newClock()
	s := newAPI(t, withLogin, withClock(c), func(o *api.Options) {
		o.TokenDefaultLifetime = 48 * time.Hour
		o.TokenMaxLifetime = 10 * 24 * time.Hour
	})
	b := s.browser(t)
	b.mustLogin(names["memberA"], testPassword)
	create := func(extra map[string]any) apigen.TokenCreated {
		body := map[string]any{"name": "t", "scope": "read"}
		for k, v := range extra {
			body[k] = v
		}
		res := b.request(http.MethodPost, "/api/v1/me/tokens", body)
		require.Equal(t, http.StatusCreated, res.StatusCode)
		return decode[apigen.TokenCreated](t, res)
	}
	assert.WithinDuration(t, c.Now().Add(48*time.Hour), create(nil).ExpiresAt, time.Second, "the configured default")
	assert.WithinDuration(t, c.Now().Add(3*24*time.Hour), create(map[string]any{"lifetime_days": 3}).ExpiresAt, time.Second)
	clamped := create(map[string]any{"lifetime_days": 365})
	assert.WithinDuration(t, c.Now().Add(10*24*time.Hour), clamped.ExpiresAt, time.Second, "shortened to the maximum")
	stored := scalar[time.Time](t, `SELECT expires_at FROM tokens WHERE id = $1`, clamped.Id)
	assert.WithinDuration(t, c.Now().Add(10*24*time.Hour), stored, time.Second)
	assertProblem(t, b.request(http.MethodPost, "/api/v1/me/tokens", map[string]any{"name": "t", "scope": "read", "lifetime_days": 0}), http.StatusBadRequest, "validation_failed")
}

// docs/adr/0035 D3, docs/adr/0036 D5, docs/adr/0043 D4: an agent token has at
// most write scope and every capability unless some are named; a plain token
// has none; a restriction names a tenant the person belongs to and a project of
// it they see, and the token reaches no other.
func TestTokenCreationRules(t *testing.T) {
	w := newWorld(t)
	names := withAccounts(t, w)
	s := newAPI(t, withLogin)
	b := s.browser(t)
	b.mustLogin(names["both"], testPassword)
	create := func(body map[string]any) *http.Response {
		if _, ok := body["name"]; !ok {
			body["name"] = "t"
		}
		return b.request(http.MethodPost, "/api/v1/me/tokens", body)
	}
	pointer := func(res *http.Response) string {
		body := assertProblem(t, res, http.StatusBadRequest, "validation_failed")
		return body["errors"].([]any)[0].(map[string]any)["pointer"].(string)
	}

	agent := decode[apigen.TokenCreated](t, created(t, create(map[string]any{"scope": "write", "agent": true})))
	assert.True(t, agent.Agent)
	assert.Len(t, agent.Capabilities, 9, "every capability by default")
	assisted := decode[apigen.TokenCreated](t, created(t, create(map[string]any{"scope": "write", "agent": true, "capabilities": []string{"drop", "upload"}})))
	assert.ElementsMatch(t, []apigen.Capability{apigen.CapabilityDrop, apigen.CapabilityUpload}, assisted.Capabilities)
	baseline := decode[apigen.TokenCreated](t, created(t, create(map[string]any{"scope": "write", "agent": true, "capabilities": []string{}})))
	assert.NotNil(t, baseline.Capabilities)
	assert.Empty(t, baseline.Capabilities, "an empty list is no capability, the baseline only; only a list left out is every capability")

	assert.Equal(t, "/scope", pointer(create(map[string]any{"scope": "admin", "agent": true})))
	assert.Equal(t, "/capabilities", pointer(create(map[string]any{"scope": "write", "capabilities": []string{"drop"}})))
	assert.Equal(t, "/name", pointer(create(map[string]any{"scope": "read", "name": "   "})))
	assert.Equal(t, "/project", pointer(create(map[string]any{"scope": "read", "project": "ALPHA"})), "a project needs its tenant")
	notMine := pointer(create(map[string]any{"scope": "read", "tenant": uniqueSlug("nobody")}))
	assert.Equal(t, "/tenant", notMine)
	assert.Equal(t, "/tenant", pointer(create(map[string]any{"scope": "read", "tenant": w.SlugB + "-x"})), "unknown")
	assert.Equal(t, "/project", pointer(create(map[string]any{"scope": "read", "tenant": w.SlugA, "project": "BETA"})), "a project of another tenant")
	assertProblem(t, create(map[string]any{"scope": "read", "unknown": 1}), http.StatusBadRequest, "validation_failed")
	assertProblem(t, create(map[string]any{"scope": "superuser"}), http.StatusBadRequest, "validation_failed")

	// A tenant the person does not belong to is "no such tenant", exactly like
	// one that does not exist.
	member := s.browser(t)
	member.mustLogin(names["memberB"], testPassword)
	res := member.request(http.MethodPost, "/api/v1/me/tokens", map[string]any{"name": "t", "scope": "read", "tenant": w.SlugA})
	assert.Equal(t, "/tenant", pointer(res))

	narrow := decode[apigen.TokenCreated](t, created(t, create(map[string]any{"scope": "write", "tenant": w.SlugA, "project": "ALPHA"})))
	assert.Equal(t, w.SlugA, narrow.RestrictedTenant.MustGet())
	assert.Equal(t, "ALPHA", narrow.RestrictedProject.MustGet())
	assert.Equal(t, w.ProjectA, narrow.RestrictedProjectId.MustGet()) //nolint:staticcheck // SA1019: the deprecated field is still answered
	assert.Equal(t, http.StatusOK, s.do(t, caller{Token: *narrow.Token}, http.MethodGet, "/api/v1/tenants/"+w.SlugA+"/projects/ALPHA", nil).StatusCode)
	assertProblem(t, s.do(t, caller{Token: *narrow.Token}, http.MethodGet, "/api/v1/tenants/"+w.SlugB, nil), http.StatusNotFound, "not_found")
}

// docs/adr/0035 D3: a token names the project it is restricted to by its key,
// in the list and in the token's own answer, as long as the person sees the
// project in a tenant they belong to; the id stays beside it for the clients
// that read it.
func TestATokenNamesItsProjectByKey(t *testing.T) {
	w := newWorld(t)
	names := withAccounts(t, w)
	s := newAPI(t, withLogin)
	b := s.browser(t)
	b.mustLogin(names["both"], testPassword)
	narrow := decode[apigen.TokenCreated](t, created(t, b.request(http.MethodPost, "/api/v1/me/tokens",
		map[string]any{"name": "narrow", "scope": "read", "tenant": w.SlugA, "project": "ALPHA"})))
	tenantOnly := decode[apigen.TokenCreated](t, created(t, b.request(http.MethodPost, "/api/v1/me/tokens",
		map[string]any{"name": "tenant", "scope": "read", "tenant": w.SlugB})))
	listed := func() map[uuid.UUID]apigen.Token {
		list := decode[apigen.TokenList](t, b.get("/api/v1/me/tokens"))
		out := map[uuid.UUID]apigen.Token{}
		for _, tok := range list.Items {
			out[tok.Id] = tok
		}
		return out
	}
	own := func() apigen.CurrentToken {
		return decode[apigen.CurrentToken](t, s.do(t, caller{Token: *narrow.Token}, http.MethodGet, "/api/v1/me/token", nil))
	}

	tokens := listed()
	assert.Equal(t, "ALPHA", tokens[narrow.Id].RestrictedProject.MustGet())
	assert.Equal(t, w.ProjectA, tokens[narrow.Id].RestrictedProjectId.MustGet()) //nolint:staticcheck // SA1019: the deprecated field is still answered
	assert.True(t, tokens[tenantOnly.Id].RestrictedProject.IsNull(), "a tenant restriction names no project")
	assert.Equal(t, "ALPHA", own().RestrictedProject.MustGet())

	// The project restricted, and the person on no list of it: the key is gone, the id stays.
	require.NoError(t, fixtures(t).Exec(context.Background(), "UPDATE projects SET restricted = true WHERE id = $1", w.ProjectA))
	tokens = listed()
	assert.True(t, tokens[narrow.Id].RestrictedProject.IsNull(), "a project the person no longer sees has no key")
	assert.Equal(t, w.ProjectA, tokens[narrow.Id].RestrictedProjectId.MustGet()) //nolint:staticcheck // SA1019: the deprecated field is still answered
	assert.Equal(t, http.StatusNotFound, s.do(t, caller{Token: *narrow.Token}, http.MethodGet, "/api/v1/tenants/"+w.SlugA+"/projects/ALPHA", nil).StatusCode,
		"and the token reaches nothing")

	// Open again, but the person left the tenant: no key either.
	require.NoError(t, fixtures(t).Exec(context.Background(), "UPDATE projects SET restricted = false WHERE id = $1", w.ProjectA))
	assert.Equal(t, "ALPHA", listed()[narrow.Id].RestrictedProject.MustGet())
	require.NoError(t, fixtures(t).Exec(context.Background(), "DELETE FROM memberships WHERE tenant_id = $1 AND user_id = $2", w.A, w.Both))
	assert.True(t, listed()[narrow.Id].RestrictedProject.IsNull(), "a tenant the person left names no project")
}

// created requires the 201 of a creation and hands the response on.
func created(t *testing.T, res *http.Response) *http.Response {
	t.Helper()
	require.Equal(t, http.StatusCreated, res.StatusCode)
	return res
}

// docs/adr/0045 D3, D6: a retried token creation creates nothing twice, and the
// stored answer holds no plaintext — the repetition answers without it.
func TestTokenCreationIdempotency(t *testing.T) {
	w := newWorld(t)
	names := withAccounts(t, w)
	s := newAPI(t, withLogin)
	b := s.browser(t)
	b.mustLogin(names["memberA"], testPassword)
	key := uuid.Must(uuid.NewV7()).String()
	body := map[string]any{"name": "keyed", "scope": "read"}
	first := decode[apigen.TokenCreated](t, created(t, b.request(http.MethodPost, "/api/v1/me/tokens", body, withHeader("Idempotency-Key", key))))
	require.NotNil(t, first.Token)
	second := decode[apigen.TokenCreated](t, created(t, b.request(http.MethodPost, "/api/v1/me/tokens", body, withHeader("Idempotency-Key", key))))
	assert.Equal(t, first.Id, second.Id)
	assert.Nil(t, second.Token, "the plaintext is shown once")
	assert.EqualValues(t, 1, scalar[int64](t, `SELECT count(*) FROM tokens WHERE user_id = $1 AND name = 'keyed'`, w.MemberA))
	assert.EqualValues(t, 0, scalar[int64](t, `SELECT count(*) FROM idempotency_keys k WHERE k::text LIKE '%' || $1 || '%'`, *first.Token))
	other := map[string]any{"name": "keyed", "scope": "write"}
	assertProblem(t, b.request(http.MethodPost, "/api/v1/me/tokens", other, withHeader("Idempotency-Key", key)), http.StatusUnprocessableEntity, "idempotency_mismatch")
}

// docs/adr/0005 D5, docs/adr/0032 D7: a global administrator, in a session,
// creates a tenant and becomes its first administrator by a marked grant, in
// the same transaction; nobody else creates one, and a global administrator has
// no other role anywhere: the work of another tenant stays closed to them
// (docs/adr/0034 D2).
func TestOnlyAGlobalAdministratorCreatesATenant(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	names := withAccounts(t, w)
	tokens := issueTokens(t, w)
	f := fixtures(t)
	root, err := f.Person(ctx, uniqueSlug("root"), "Root")
	require.NoError(t, err)
	require.NoError(t, f.GlobalAdmin(ctx, root))
	require.NoError(t, f.Account(ctx, root, testPassword, uuid.Nil, false))
	rootToken, _, err := f.Token(ctx, fixture.TokenSpec{UserID: root, Scope: domain.ScopeAdmin})
	require.NoError(t, err)
	s := newAPI(t, withLogin)
	slug := uniqueSlug("created")
	body := map[string]string{"slug": slug, "name": "  Created  "}

	member := s.browser(t)
	member.mustLogin(names["memberA"], testPassword)
	assertProblem(t, member.request(http.MethodPost, "/api/v1/tenants", body), http.StatusForbidden, "forbidden")
	assertProblem(t, s.do(t, caller{Token: rootToken}, http.MethodPost, "/api/v1/tenants", body), http.StatusForbidden, "session_required")
	assertProblem(t, s.do(t, caller{Token: tokens.AdminA}, http.MethodPost, "/api/v1/tenants", body), http.StatusForbidden, "session_required")
	assertProblem(t, s.do(t, caller{}, http.MethodPost, "/api/v1/tenants", body), http.StatusUnauthorized, "unauthenticated")
	assert.Zero(t, scalar[int64](t, `SELECT count(*) FROM tenants WHERE slug = $1`, slug))

	b := s.browser(t)
	b.mustLogin(usernameOf(t, root), testPassword)
	assertProblem(t, b.get("/api/v1/tenants/"+w.SlugA+"/projects"), http.StatusNotFound, "not_found")
	res := b.request(http.MethodPost, "/api/v1/tenants", body)
	require.Equal(t, http.StatusCreated, res.StatusCode)
	assert.Equal(t, "/api/v1/tenants/"+slug, res.Header.Get("Location"))
	assert.Equal(t, `"1"`, res.Header.Get("ETag"))
	tenant := decode[apigen.Tenant](t, res)
	assert.Equal(t, slug, tenant.Slug)
	assert.Equal(t, "Created", tenant.Name)

	tenantID := scalar[uuid.UUID](t, `SELECT id FROM tenants WHERE slug = $1`, slug)
	assert.Equal(t, "admin", scalar[string](t, `SELECT role::text FROM memberships WHERE tenant_id = $1 AND user_id = $2 AND source = 'grant'`, tenantID, root),
		"the creator's marked grant as admin")
	assert.EqualValues(t, 2, scalar[int64](t, `SELECT count(*) FROM audit_events WHERE actor_user_id = $1 AND tenant_id IS NULL AND action = 'created'
		AND entity_type IN ('tenant', 'membership') AND (entity_id = $2 OR after->>'tenant' = $3)`, root, tenantID, slug))
	assert.Equal(t, http.StatusOK, b.get("/api/v1/tenants/"+slug+"/projects").StatusCode, "the first administrator works in the tenant")
	assertProblem(t, b.get("/api/v1/tenants/"+w.SlugB+"/projects"), http.StatusNotFound, "not_found")

	assertProblem(t, b.request(http.MethodPost, "/api/v1/tenants", body), http.StatusConflict, "tenant_slug_taken")
	assertProblem(t, b.request(http.MethodPost, "/api/v1/tenants", map[string]string{"slug": "Bad_Slug", "name": "x"}), http.StatusBadRequest, "validation_failed")
	assertProblem(t, b.request(http.MethodPost, "/api/v1/tenants", map[string]string{"slug": uniqueSlug("blank"), "name": "   "}), http.StatusBadRequest, "validation_failed")

	key := uuid.Must(uuid.NewV7()).String()
	keyed := map[string]string{"slug": uniqueSlug("keyed"), "name": "Keyed"}
	require.Equal(t, http.StatusCreated, b.request(http.MethodPost, "/api/v1/tenants", keyed, withHeader("Idempotency-Key", key)).StatusCode)
	replay := b.request(http.MethodPost, "/api/v1/tenants", keyed, withHeader("Idempotency-Key", key))
	require.Equal(t, http.StatusCreated, replay.StatusCode)
	assert.Equal(t, "/api/v1/tenants/"+keyed["slug"], replay.Header.Get("Location"))
	assert.EqualValues(t, 1, scalar[int64](t, `SELECT count(*) FROM tenants WHERE slug = $1`, keyed["slug"]))
}

// docs/adr/0037 D1, D2, D5, D6, docs/adr/0035 D7: a write of a session needs the
// installation's Origin — or its Referer — and X-Requested-With: cowork; reads
// do not, nor does a token, which carries no cookie; the check fails closed
// without a COWORK_BASE_URL; and the login has the origin check too.
func TestSessionWritesAreCSRFChecked(t *testing.T) {
	w := newWorld(t)
	names := withAccounts(t, w)
	tokens := issueTokens(t, w)
	s := newAPI(t, withLogin)
	b := s.browser(t)
	b.mustLogin(names["memberA"], testPassword)
	path := "/api/v1/tenants/" + w.SlugA + "/projects"
	write := func(opts ...reqOpt) *http.Response {
		return b.request(http.MethodPost, path, map[string]string{"key": strings.ToUpper(strings.ReplaceAll(uniqueSlug("c"), "-", "")), "name": "p"}, opts...)
	}
	created := func() int64 { return scalar[int64](t, `SELECT count(*) FROM projects WHERE tenant_id = $1`, w.A) }

	before := created()
	require.Equal(t, http.StatusCreated, write().StatusCode, "origin and header")
	assert.Equal(t, before+1, created())
	before = created()
	for name, opts := range map[string][]reqOpt{
		"no custom header":                    {without("X-Requested-With")},
		"another header value":                {withHeader("X-Requested-With", "XMLHttpRequest")},
		"another origin":                      {withHeader("Origin", "https://evil.example.com")},
		"the null origin":                     {withHeader("Origin", "null")},
		"another scheme":                      {withHeader("Origin", "http://cowork.test")},
		"another port":                        {withHeader("Origin", testOrigin+":8443")},
		"neither Origin nor Referer":          {without("Origin")},
		"another Referer":                     {without("Origin"), withHeader("Referer", "https://evil.example.com/x")},
		"the Origin wins over a good Referer": {withHeader("Origin", "https://evil.example.com"), withHeader("Referer", testOrigin+"/t/x")},
	} {
		t.Run(name, func(t *testing.T) {
			assertProblem(t, write(opts...), http.StatusForbidden, "csrf")
		})
	}
	assert.Equal(t, before, created(), "a refused write changed nothing")
	require.Equal(t, http.StatusCreated, write(without("Origin"), withHeader("Referer", testOrigin+"/t/acme/tickets/COW-1?x=1")).StatusCode, "the Referer stands in for a missing Origin")

	// A read never mutates, so it is not checked.
	assert.Equal(t, http.StatusOK, b.get("/api/v1/me", without("X-Requested-With")).StatusCode)

	// A token has no cookie: neither header, and it still writes. A request that
	// carries a token and a session's cookie is the token's, and the cookie is
	// not looked at.
	viewer := s.browser(t)
	viewer.mustLogin(names["viewerA"], testPassword)
	key := strings.ToUpper(strings.ReplaceAll(uniqueSlug("t"), "-", ""))
	res := viewer.request(http.MethodPost, path, map[string]string{"key": key, "name": "p"}, without("Origin"), without("X-Requested-With"), withBearer(tokens.MemberA))
	require.Equal(t, http.StatusCreated, res.StatusCode, "a viewer's cookie beside a member's token")
	assert.EqualValues(t, 1, scalar[int64](t, `SELECT count(*) FROM audit_events WHERE entity_type = 'project' AND action = 'created'
		AND actor_user_id = $1 AND tenant_id = $2 AND token_id IS NOT NULL AND after->>'key' = $3`, w.MemberA, w.A, key))
	viewerWrite := viewer.request(http.MethodPost, path, map[string]string{"key": "VIEWA", "name": "p"})
	assertProblem(t, viewerWrite, http.StatusForbidden, "forbidden")
	assertProblem(t, viewer.request(http.MethodPost, path, map[string]string{"key": "VIEWB", "name": "p"}, withBearer("cwk_"+strings.Repeat("A", 43))),
		http.StatusUnauthorized, "unauthenticated")

	// The login: the origin, not the custom header.
	login := func(opts ...reqOpt) *http.Response {
		return s.browser(t).login(names["memberA"], testPassword, opts...)
	}
	assertProblem(t, login(withHeader("Origin", "https://evil.example.com")), http.StatusForbidden, "csrf")
	assertProblem(t, login(without("Origin")), http.StatusForbidden, "csrf")
	assert.Equal(t, http.StatusOK, login(without("X-Requested-With")).StatusCode)
	assert.Equal(t, http.StatusOK, login(without("Origin"), withHeader("Referer", testOrigin+"/login")).StatusCode)

	// Without a COWORK_BASE_URL nothing of a cookie can be checked, so nothing
	// is let through; reads still work.
	open := newAPI(t)
	ob := open.browser(t)
	assertProblem(t, ob.login(names["memberA"], testPassword), http.StatusForbidden, "csrf")
	assertProblem(t, ob.request(http.MethodPost, "/auth/logout", nil), http.StatusUnauthorized, "unauthenticated")
}

// docs/adr/0031 D4, docs/adr/0054 D5: an open event stream checks its session
// at every heartbeat and ends when the session does.
func TestEventStreamEndsWithItsSession(t *testing.T) {
	w := newWorld(t)
	names := withAccounts(t, w)
	s := newAPI(t, withLogin, func(o *api.Options) { o.Heartbeat = 100 * time.Millisecond })
	b := s.browser(t)
	b.mustLogin(names["memberA"], testPassword)

	stream := b.get("/api/v1/tenants/" + w.SlugA + "/events")
	require.Equal(t, http.StatusOK, stream.StatusCode)
	assert.Equal(t, "text/event-stream", stream.Header.Get("Content-Type"))
	ended := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, stream.Body)
		close(ended)
	}()
	select {
	case <-ended:
		require.Fail(t, "the stream ended while its session lived")
	case <-time.After(400 * time.Millisecond):
	}
	other := &browser{t: t, s: s, Cookie: b.Cookie}
	require.Equal(t, http.StatusNoContent, other.request(http.MethodPost, "/auth/logout", nil).StatusCode)
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		require.Fail(t, "the stream outlived its session")
	}
}

// docs/adr/0021 D6, docs/adr/0023 D5: the walk of the tenant-boundary harness,
// with a session instead of a token — every route under a tenant refuses a
// person of another tenant exactly like an unknown tenant, the account routes
// included.
func TestEveryTenantRouteRefusesAnotherTenantForASessionToo(t *testing.T) {
	w := newWorld(t)
	names := withAccounts(t, w)
	s := newAPI(t, withLogin)
	b := s.browser(t)
	b.mustLogin(names["adminA"], testPassword)
	routes := tenantRoutes(t)
	require.Greater(t, len(routes), 40)
	strip := func(res *http.Response) map[string]any {
		body := problemBody(t, res)
		delete(body, "instance")
		delete(body, "request_id")
		return body
	}
	for _, r := range routes {
		var body any
		if r.method != http.MethodGet && r.method != http.MethodDelete {
			body = map[string]any{}
		}
		other := b.request(r.method, strings.ReplaceAll(r.path, "{tenant}", w.SlugB), body)
		unknown := b.request(r.method, strings.ReplaceAll(r.path, "{tenant}", "no-such-tenant-9"), body)
		require.Equal(t, http.StatusNotFound, other.StatusCode, "%s %s", r.method, r.path)
		require.Equal(t, http.StatusNotFound, unknown.StatusCode, "%s %s", r.method, r.path)
		assert.Equal(t, strip(unknown), strip(other), "%s %s answers another tenant like no tenant", r.method, r.path)
	}
}

// docs/adr/0033 D4, docs/adr/0031 D7, docs/adr/0035 D1, docs/adr/0047 D3: a
// password, a session's cookie and a token's plaintext are in no log line, no
// audit row, no error body and no column that is not their hash. The request log
// is recorded at every level and searched, with the audit record, the tables of
// the login, and the bodies of the answers.
func TestNoPasswordCookieOrTokenIsLoggedOrRecorded(t *testing.T) {
	w := newWorld(t)
	names := withAccounts(t, w)
	rec := &recordingLogger{}
	s := newAPI(t, withLogin, func(o *api.Options) { o.Logger = rec.logger() })
	base := accountsPath(w.SlugA)

	var bodies []string
	keep := func(res *http.Response) *http.Response {
		raw, err := io.ReadAll(res.Body)
		require.NoError(t, err)
		if res.Request.URL.Path != "/api/v1/me/tokens" || res.Request.Method != http.MethodPost {
			// The answer that creates the token is the one place its plaintext is.
			bodies = append(bodies, string(raw))
		}
		res.Body = io.NopCloser(strings.NewReader(string(raw)))
		return res
	}
	secrets := make([]string, 0, 8)
	secrets = append(secrets, "temporary secret 1", "a brand new password", "wrong password!", "another wrong one")

	admin := s.browser(t)
	keep(admin.login(names["adminA"], testPassword))
	secrets = append(secrets, testPassword, admin.Cookie)
	username := uniqueSlug("quiet")
	keep(admin.request(http.MethodPost, base, map[string]string{"username": username, "display_name": "Quiet", "temporary_password": "temporary secret 1", "role": "member"}))
	keep(s.browser(t).login(username, "wrong password!"))
	keep(s.browser(t).login(uniqueSlug("ghost"), "another wrong one"))
	person := s.browser(t)
	keep(person.login(username, "temporary secret 1"))
	secrets = append(secrets, person.Cookie)
	keep(person.request(http.MethodPut, "/api/v1/me/password", map[string]string{"current_password": "wrong password!", "new_password": "a brand new password"}))
	keep(person.request(http.MethodPut, "/api/v1/me/password", map[string]string{"current_password": "temporary secret 1", "new_password": "short"}))
	keep(person.request(http.MethodPut, "/api/v1/me/password", map[string]string{"current_password": "temporary secret 1", "new_password": "a brand new password"}))
	res := keep(person.request(http.MethodPost, "/api/v1/me/tokens", map[string]any{"name": "quiet", "scope": "read"}))
	require.Equal(t, http.StatusCreated, res.StatusCode)
	token := *decode[apigen.TokenCreated](t, res).Token
	keep(admin.request(http.MethodPut, accountsPath(w.SlugA, "/", username, "/password"), map[string]string{"temporary_password": "temporary secret 1"}))
	keep(admin.request(http.MethodPut, accountsPath(w.SlugA, "/", username, "/deactivation"), nil))
	keep(person.request(http.MethodPost, "/auth/logout", nil))
	assertProblem(t, s.do(t, caller{Token: token}, http.MethodGet, "/api/v1/me", nil), http.StatusUnauthorized, "token_revoked")
	secrets = append(secrets, token)

	logs := rec.text()
	require.Contains(t, logs, "path=/auth/local", "the request log was recorded")
	for _, secret := range secrets {
		assert.NotContains(t, logs, secret, "the log")
		for _, body := range bodies {
			assert.NotContains(t, body, secret, "an answer")
		}
		for _, table := range []string{"audit_events", "local_accounts", "login_attempts", "login_locks", "idempotency_keys", "users", "sessions", "tokens"} {
			assert.Zero(t, scalar[int64](t, `SELECT count(*) FROM `+table+` r WHERE r::text LIKE '%' || $1 || '%'`, secret), "%s holds %q", table, secret)
		}
	}
}
