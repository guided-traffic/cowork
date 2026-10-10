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

	"github.com/guided-traffic/cowork/backend/internal/api"
	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/test/fakeissuer"
	"github.com/guided-traffic/cowork/backend/test/fixture"
)

// The tests of the security review of 2026-10-04, item by item.

// M1: a refresh waits on the issuer with no connection and no lock held. With
// a pool of two connections, an issuer that stops answering, and four
// requests of one session at once, three are served on the session's groups
// without waiting, another person's request answers, and the database still
// answers its readiness check; the refresh completes once the issuer does.
func TestARefreshWaitsForNoOneElse(t *testing.T) {
	ctx := context.Background()
	f := fixtures(t)
	slug, group := uniqueSlug("pool"), uniqueSlug("team")
	tenant, err := f.Tenant(ctx, slug, "Pool")
	require.NoError(t, err)
	require.NoError(t, f.Exec(ctx, `INSERT INTO group_mappings (tenant_id, group_name, role) VALUES ($1, $2, 'member')`, tenant, group))
	other, err := f.Person(ctx, uniqueSlug("other"), "Other")
	require.NoError(t, err)
	require.NoError(t, f.Account(ctx, other, testPassword, tenant, false))
	require.NoError(t, f.Member(ctx, tenant, other, domain.RoleMember))

	is := fakeissuer.Start(t)
	subject := uniqueSlug("sub")
	is.Add(fakeissuer.User{Subject: subject, Email: subject + "@example.com", EmailVerified: true, Groups: []string{"cowork-users", group}})
	small := openStore(t, env.RuntimeURL+"&pool_max_conns=2")
	c := newClock()
	s := newAPI(t, withLogin, devGate(fakeProvider(t, is)), withClock(c), func(o *api.Options) { o.DB = small })
	a := s.browser(t)
	require.Equal(t, "/", a.oidcLogin(subject+"@example.com", "/").Header.Get("Location"))
	b := s.browser(t)
	b.mustLogin(usernameOf(t, other), testPassword)

	c.Advance(16 * time.Minute)
	is.Hang()
	statuses := make(chan int, 4)
	for range 4 {
		go func() {
			req, err := http.NewRequest(http.MethodGet, s.URL+"/api/v1/me", nil)
			if err != nil {
				statuses <- 0
				return
			}
			req.AddCookie(&http.Cookie{Name: auth.SessionCookie, Value: a.Cookie})
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				statuses <- 0
				return
			}
			_ = res.Body.Close()
			statuses <- res.StatusCode
		}()
	}
	deadline := time.After(5 * time.Second)
	for range 3 {
		select {
		case code := <-statuses:
			assert.Equal(t, http.StatusOK, code, "served on the session's groups while another request refreshes them")
		case <-deadline:
			t.Fatal("a request of the session waited for the refresh another request holds")
		}
	}
	answered := make(chan int, 1)
	go func() {
		req, _ := http.NewRequest(http.MethodGet, s.URL+"/api/v1/me", nil)
		req.AddCookie(&http.Cookie{Name: auth.SessionCookie, Value: b.Cookie})
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			answered <- 0
			return
		}
		_ = res.Body.Close()
		answered <- res.StatusCode
	}()
	select {
	case code := <-answered:
		assert.Equal(t, http.StatusOK, code)
	case <-time.After(3 * time.Second):
		t.Fatal("another person's request waited for the issuer")
	}
	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	assert.NoError(t, small.Ping(pingCtx), "the readiness check answers while the issuer does not")

	is.Release()
	select {
	case code := <-statuses:
		assert.Equal(t, http.StatusOK, code, "the refresh completes once the issuer answers")
	case <-time.After(10 * time.Second):
		t.Fatal("the refreshing request never completed")
	}
	hash := auth.HashSession(a.Cookie)
	assert.EqualValues(t, 1, scalar[int64](t, `SELECT count(*) FROM sessions WHERE token_hash = $1 AND refresh_retry_at IS NULL
		AND groups_refreshed_at > now() - interval '1 hour'`, hash[:]), "the answer applied, the lease released")
}

// M2: a refresh that read nothing from the issuer — here, sessions without a
// refresh token — judges the person's groups as they stand and changes only
// the session's row: the older session's groups never go over the person's
// newer ones, and the administrator flag and mapped role a later login took
// away do not come back.
func TestARefreshThatReadNothingKeepsThePersonsNewerGroups(t *testing.T) {
	w := newFakeWorld(t)
	w.turn(func(is *fakeissuer.Issuer) { is.NoRefreshToken = true })
	older := w.login(t)
	me := decode[apigen.Me](t, older.get("/api/v1/me"))
	require.True(t, me.GlobalAdmin)
	require.Equal(t, apigen.RoleMember, roleIn(me, w.slug))

	w.is.SetGroups(w.subject, []string{"cowork-users"})
	newer := w.login(t)
	assert.False(t, decode[apigen.Me](t, newer.get("/api/v1/me")).GlobalAdmin)

	w.clock.Advance(16 * time.Minute)
	me = decode[apigen.Me](t, older.get("/api/v1/me"))
	assert.False(t, me.GlobalAdmin, "the older session's groups did not come back")
	assert.Empty(t, roleIn(me, w.slug))
	assert.EqualValues(t, 1, scalar[int64](t, `SELECT count(*) FROM users WHERE id = $1 AND oidc_groups = '{cowork-users}'
		AND NOT global_admin`, w.person(t)))
	assert.EqualValues(t, 0, scalar[int64](t, `SELECT count(*) FROM memberships WHERE tenant_id = $1`, w.tenant))
	w.clock.Advance(16 * time.Minute)
	assert.False(t, decode[apigen.Me](t, older.get("/api/v1/me")).GlobalAdmin, "nor at the next refresh")
}

// m2: the issuer refusing cowork's client is the configuration's error: the
// session is served, and the log says it at error level.
func TestAClientTheIssuerRefusesIsTheConfigurationsError(t *testing.T) {
	logs := &recordingLogger{}
	w := newFakeWorld(t)
	s := newAPI(t, withLogin, devGate(fakeProvider(t, w.is)), withClock(w.clock), func(o *api.Options) { o.Logger = logs.logger() })
	b := s.browser(t)
	require.Equal(t, "/", b.oidcLogin(w.subject+"@example.com", "/").Header.Get("Location"))
	for _, knob := range []func(*fakeissuer.Issuer){
		func(is *fakeissuer.Issuer) { is.RefreshError = "invalid_client" },
		func(is *fakeissuer.Issuer) {
			is.RefreshStatus, is.RefreshError = http.StatusTooManyRequests, "invalid_grant"
		},
	} {
		w.turn(knob)
		w.clock.Advance(16 * time.Minute)
		require.Equal(t, http.StatusOK, b.get("/api/v1/me").StatusCode, "served")
		w.turn(func(is *fakeissuer.Issuer) { is.RefreshStatus, is.RefreshError = 0, "" })
	}
	assert.Contains(t, logs.text(), "level=ERROR msg=\"the issuer refuses cowork's client")
}

// Item 12: a refreshed ID token that no published key verifies ends the
// session; keys that cannot be fetched leave it served.
func TestARefreshedIDTokenThatDoesNotVerify(t *testing.T) {
	w := newFakeWorld(t)
	b := w.login(t)
	w.turn(func(is *fakeissuer.Issuer) { is.IDTokenOnRefresh, is.RotatedKey, is.KeysDown = true, true, true })
	w.clock.Advance(16 * time.Minute)
	require.Equal(t, http.StatusOK, b.get("/api/v1/me").StatusCode, "keys that cannot be fetched are the issuer's trouble")
	w.turn(func(is *fakeissuer.Issuer) { is.RotatedKey, is.KeysDown, is.WrongKey = false, false, true })
	w.clock.Advance(2 * time.Minute)
	assertProblem(t, b.get("/api/v1/me"), http.StatusUnauthorized, "unauthenticated")
	assert.EqualValues(t, 1, scalar[int64](t, `SELECT count(*) FROM audit_events WHERE entity_id = $1 AND action = 'revoked'
		AND reason = 'identity-provider'`, w.person(t)))
}

// m3: a mapping's change derives the memberships of the persons who can act,
// and leaves a deactivated person and one the gate refused as they are.
func TestARederivationLeavesWhoCannotAct(t *testing.T) {
	ctx := context.Background()
	a := newAdminWorld(t)
	a.globalAdmin(t)
	yes := true
	group := uniqueSlug("g")
	active := a.person(t, address("active"), &yes, group)
	gone := a.person(t, address("gone"), &yes, group)
	refused := a.person(t, address("refused"), &yes, group)
	require.NoError(t, fixtures(t).Exec(ctx, `UPDATE users SET deactivated_at = now() WHERE id = $1`, gone))
	require.NoError(t, fixtures(t).Exec(ctx, `UPDATE users SET gate_checked_at = NULL WHERE id = $1`, refused))
	require.Equal(t, http.StatusCreated, a.admin.request(http.MethodPost, a.path("/group-mappings"),
		map[string]string{"group": group, "role": "member"}).StatusCode)
	list := members(t, a.admin, a.path("/members"))
	assert.Contains(t, list, active)
	assert.NotContains(t, list, gone)
	assert.NotContains(t, list, refused)
}

// m4: two administrators who take each other's admin role away at the same
// moment are decided one after the other, and the tenant keeps an
// administrator.
func TestTwoAdministratorsCannotRemoveEachOther(t *testing.T) {
	ctx := context.Background()
	f := fixtures(t)
	slug := uniqueSlug("pair")
	tenant, err := f.Tenant(ctx, slug, "Pair")
	require.NoError(t, err)
	s := newAPI(t, withLogin)
	ids := make([]uuid.UUID, 0, 2)
	browsers := make([]*browser, 0, 2)
	for range 2 {
		id, err := f.Person(ctx, uniqueSlug("admin"), "Admin")
		require.NoError(t, err)
		require.NoError(t, f.Account(ctx, id, testPassword, tenant, false))
		b := s.browser(t)
		ids, browsers = append(ids, id), append(browsers, b)
	}
	for round := range 8 {
		for i, id := range ids {
			require.NoError(t, f.Member(ctx, tenant, id, domain.RoleAdmin))
			browsers[i].mustLogin(usernameOf(t, id), testPassword)
		}
		remove := func(i int) func() int {
			return func() int {
				res := browsers[i].request(http.MethodDelete, "/api/v1/teams/"+slug+"/members/"+ids[1-i].String()+"/grant", nil)
				return res.StatusCode
			}
		}
		// The one decided second meets last_admin, or — when the first came
		// before its tenant boundary — is no member any more.
		codes := simultaneously(remove(0), remove(1))
		for _, code := range codes {
			assert.Contains(t, []int{http.StatusNoContent, http.StatusConflict, http.StatusNotFound}, code, "round %d", round)
		}
		assert.Positive(t, scalar[int64](t, `SELECT count(*) FROM memberships WHERE tenant_id = $1 AND role = 'admin'`, tenant),
			"round %d: the tenant keeps an administrator", round)
	}
}

// m5: only an administrator who can log in counts for last_admin — not a
// deactivated one, not one the gate refused, not one of another issuer.
func TestTheLastAdministratorMustBeAbleToAct(t *testing.T) {
	ctx := context.Background()
	a := newAdminWorld(t)
	yes := true
	f := fixtures(t)
	provider := a.person(t, address("padmin"), &yes, "cowork-users")
	require.NoError(t, f.Member(ctx, a.A, provider, domain.RoleAdmin))
	own := a.path("/members/" + a.AdminA.String() + "/grant")
	for name, set := range map[string]string{
		"refused by the gate": `UPDATE users SET gate_checked_at = NULL WHERE id = $1`,
		"of another issuer":   `UPDATE users SET oidc_issuer = 'https://another-issuer.example.com' WHERE id = $1`,
		"deactivated":         `UPDATE users SET deactivated_at = now() WHERE id = $1`,
	} {
		require.NoError(t, f.Exec(ctx, set, provider), name)
		assertProblem(t, a.admin.request(http.MethodDelete, own, nil), http.StatusConflict, "last_admin")
		require.NoError(t, f.Exec(ctx, `UPDATE users SET gate_checked_at = now(), oidc_issuer = $2, deactivated_at = NULL WHERE id = $1`,
			provider, a.issuer))
	}
	assert.Equal(t, http.StatusNoContent, a.admin.request(http.MethodDelete, own, nil).StatusCode,
		"an administrator who can act is enough")
}

// m6: a person whose issuer is not the configured one is outside the gate at
// once, whatever their groups and their last check: their token is refused and
// their sessions end.
func TestAPersonOfAnotherIssuerIsOutsideTheGate(t *testing.T) {
	w := newFakeWorld(t)
	b := w.login(t)
	created := b.request(http.MethodPost, "/api/v1/me/tokens", map[string]any{"name": "script", "scope": "read"})
	require.Equal(t, http.StatusCreated, created.StatusCode)
	token := caller{Token: *decode[apigen.TokenCreated](t, created).Token}

	another := fakeissuer.Start(t)
	elsewhere := newAPI(t, withLogin, devGate(fakeProvider(t, another)), withClock(w.clock))
	assertProblem(t, elsewhere.do(t, token, http.MethodGet, "/api/v1/me", nil), http.StatusUnauthorized, "not_allowed")
	onElsewhere := &browser{t: t, s: elsewhere, Cookie: b.Cookie}
	assertProblem(t, onElsewhere.get("/api/v1/me"), http.StatusUnauthorized, "unauthenticated")
	assert.EqualValues(t, 0, scalar[int64](t, `SELECT count(*) FROM sessions WHERE user_id = $1`, w.person(t)))

	none := newAPI(t, withLogin, withClock(w.clock))
	assertProblem(t, none.do(t, token, http.MethodGet, "/api/v1/me", nil), http.StatusUnauthorized, "not_allowed")
	assert.Equal(t, http.StatusOK, w.s.do(t, token, http.MethodGet, "/api/v1/me", nil).StatusCode,
		"refused, not revoked: under the configured issuer it works")
}

// m11: an address is in no audit row — a changed one is recorded as changed.
func TestAnAddressIsInNoAuditRow(t *testing.T) {
	w := newFakeWorld(t)
	first := w.subject + "@example.com"
	w.login(t)
	second := "renamed-" + w.subject + "@example.com"
	w.is.SetEmail(w.subject, second)
	b := w.s.browser(t)
	require.Equal(t, "/", b.oidcLogin(second, "/").Header.Get("Location"))
	person := w.person(t)
	for _, address := range []string{first, second} {
		assert.EqualValues(t, 0, scalar[int64](t, `SELECT count(*) FROM audit_events WHERE
			coalesce(before::text, '') || coalesce(after::text, '') || coalesce(reason, '') || coalesce(note, '') ILIKE '%' || $1 || '%'`,
			address), address)
	}
	assert.EqualValues(t, 1, scalar[int64](t, `SELECT count(*) FROM audit_events WHERE entity_id = $1 AND action = 'updated'
		AND (after->>'email_changed')::boolean`, person))
}

// Item 13: the tenant's administrators see the address that tells two persons
// of one name apart; a member sees null.
func TestAdministratorsSeeTheAddress(t *testing.T) {
	ctx := context.Background()
	a := newAdminWorld(t)
	yes := true
	email := address("twin")
	person := a.person(t, email, &yes)
	require.NoError(t, fixtures(t).Member(ctx, a.A, person, domain.RoleViewer))
	asAdmin := members(t, a.admin, a.path("/members"))
	require.False(t, asAdmin[person].Email.IsNull())
	assert.Equal(t, email, asAdmin[person].Email.MustGet())
	assert.True(t, asAdmin[a.MemberA].Email.IsNull(), "a local account has none")
	member := a.s.browser(t)
	member.mustLogin(a.names["memberA"], testPassword)
	assert.True(t, members(t, member, a.path("/members"))[person].Email.IsNull(), "a member reads null")

	entry := a.admin.request(http.MethodPut, a.path("/projects/ALPHA/access/"+person.String()), map[string]string{"role": "viewer"})
	require.Equal(t, http.StatusOK, entry.StatusCode)
	assert.Equal(t, email, decode[apigen.ProjectAccessEntry](t, entry).Email.MustGet())
	list := decode[apigen.ProjectAccessList](t, a.admin.get(a.path("/projects/ALPHA/access")))
	require.Len(t, list.Items, 1)
	assert.Equal(t, email, list.Items[0].Email.MustGet())
}

// m10: a project-restricted token's stream hears of a membership act only
// when it names the token's project, or names the token's person and no
// other project.
func TestARestrictedStreamHearsOnlyItsProject(t *testing.T) {
	ctx := context.Background()
	a := newAdminWorld(t)
	plaintext, _, err := fixtures(t).Token(ctx, fixture.TokenSpec{UserID: a.MemberA, TenantID: a.A, ProjectID: a.ProjectA})
	require.NoError(t, err)
	var env ticketEnv
	bound := env.openStream(t, a.s, caller{Token: plaintext}, a.SlugA, "")
	yes := true
	stranger := a.person(t, address("stranger"), &yes)
	require.Equal(t, http.StatusCreated, a.admin.request(http.MethodPost, a.path("/members"),
		map[string]string{"person": scalar[string](t, `SELECT email FROM users WHERE id = $1`, stranger), "role": "viewer"}).StatusCode)
	require.Equal(t, http.StatusOK, a.admin.request(http.MethodPut, a.path("/projects/ALPHA/access/"+a.MemberA.String()),
		map[string]string{"role": "viewer"}).StatusCode)
	d, ok := nextMembership(t, bound)
	require.True(t, ok)
	require.NotNil(t, d.ProjectID, "the first event it hears is the entry on its project, not the stranger's grant")
	assert.Equal(t, a.ProjectA, *d.ProjectID)
}
