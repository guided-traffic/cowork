//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api"
	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/auth"
)

// docs/adr/0031 D1, D2, D5, D7, docs/adr/0033: a login makes a session whose
// cookie is __Host-cowork-session, HttpOnly, Secure, SameSite=Lax, Path=/, no
// Domain; only the hash of its 256 bits is stored; the session is its person;
// a login while a session exists replaces it; the login is a recorded act
// without the session's id.
func TestLocalLoginStartsASession(t *testing.T) {
	w := newWorld(t)
	names := withAccounts(t, w)
	s := newAPI(t, withLogin)
	b := s.browser(t)

	res := b.login(names["memberA"], testPassword)
	require.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, "no-store", res.Header.Get("Cache-Control"))
	assert.Equal(t, false, decode[map[string]any](t, res)["password_change_required"])

	var cookie *http.Cookie
	for _, c := range res.Cookies() {
		if c.Name == "__Host-cowork-session" {
			cookie = c
		}
	}
	require.NotNil(t, cookie)
	assert.True(t, cookie.HttpOnly)
	assert.True(t, cookie.Secure)
	assert.Equal(t, http.SameSiteLaxMode, cookie.SameSite)
	assert.Equal(t, "/", cookie.Path)
	assert.Empty(t, cookie.Domain)
	assert.Len(t, cookie.Value, 43, "256 random bits")
	assert.InDelta(t, 12*3600, cookie.MaxAge, 5, "the absolute lifetime, twelve hours by default")

	hash := auth.HashSession(cookie.Value)
	assert.EqualValues(t, 1, scalar[int64](t, `SELECT count(*) FROM sessions WHERE token_hash = $1 AND user_id = $2`, hash[:], w.MemberA))
	assert.EqualValues(t, 0, scalar[int64](t, `SELECT count(*) FROM sessions s WHERE s::text LIKE '%' || $1 || '%'`, cookie.Value),
		"the cookie's value is in no column of the session table")

	me := b.get("/api/v1/me")
	require.Equal(t, http.StatusOK, me.StatusCode)
	person := decode[apigen.Me](t, me)
	assert.Equal(t, w.MemberA, person.Id)
	assert.True(t, person.Local)
	assert.False(t, person.GlobalAdmin)
	assert.False(t, person.PasswordChangeRequired)
	assert.Len(t, person.Memberships, 1)

	// The name is trimmed and lower-cased.
	other := s.browser(t)
	require.Equal(t, http.StatusOK, other.login("  "+strings.ToUpper(names["viewerA"])+"  ", testPassword).StatusCode)

	// A login while a session exists replaces it (D5): a new value, the old one dead.
	old := b.Cookie
	require.Equal(t, http.StatusOK, b.login(names["memberA"], testPassword).StatusCode)
	assert.NotEqual(t, old, b.Cookie)
	oldBrowser := &browser{t: t, s: s, Cookie: old}
	assertProblem(t, oldBrowser.get("/api/v1/me"), http.StatusUnauthorized, "unauthenticated")
	assert.Equal(t, http.StatusOK, b.get("/api/v1/me").StatusCode)

	// The login is an act of its person; no row carries the session's id or
	// value (D7).
	assert.GreaterOrEqual(t, scalar[int64](t, `SELECT count(*) FROM audit_events
		WHERE action = 'logged_in' AND actor_user_id = $1 AND token_id IS NULL AND tenant_id IS NULL`, w.MemberA), int64(2))
	assert.EqualValues(t, 0, scalar[int64](t, `SELECT count(*) FROM audit_events WHERE action = 'logged_in'
		AND (before::text || after::text || coalesce(note, '') || coalesce(reason, '')) LIKE '%' || $1 || '%'`, b.Cookie))
}

// docs/adr/0033 D6: every refusal of the login is the same 401, an unknown
// username included, and an unknown username costs the same Argon2id
// computation — against a dummy hash — as a known one, so neither the answer nor
// the time reveals whether an account exists.
func TestEveryLoginFailureIsTheSame(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	names := withAccounts(t, w)
	f := fixtures(t)
	nobody, err := f.Person(ctx, uniqueSlug("nobody"), "Nobody")
	require.NoError(t, err)
	gone, err := f.Person(ctx, uniqueSlug("gone"), "Gone")
	require.NoError(t, err)
	require.NoError(t, f.Account(ctx, gone, testPassword, w.A, false))
	require.NoError(t, f.Exec(ctx, `UPDATE users SET deactivated_at = now() WHERE id = $1`, gone))
	s := newAPI(t, withLogin)

	cases := map[string][2]string{
		"an unknown username":                   {uniqueSlug("ghost"), testPassword},
		"a wrong password":                      {names["memberA"], "not the password"},
		"a username that cannot be one":         {"not a username!", testPassword},
		"a blank username":                      {"   ", testPassword},
		"a person without a local account":      {usernameOf(t, nobody), testPassword},
		"a deactivated account, right password": {usernameOf(t, gone), testPassword},
	}
	var first map[string]any
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			before := auth.Computations()
			res := s.browser(t).login(c[0], c[1])
			assert.EqualValues(t, 1, auth.Computations()-before, "one Argon2id computation, as for a known account")
			require.Equal(t, http.StatusUnauthorized, res.StatusCode)
			assert.Empty(t, res.Cookies(), "no cookie")
			assert.Empty(t, res.Header.Get("WWW-Authenticate"))
			body := unsafeBody(t, res)
			assert.Equal(t, "invalid_credentials", body["code"])
			if first == nil {
				first = body
			}
			assert.Equal(t, first, body, "the very same answer")
		})
	}

	before := auth.Computations()
	require.Equal(t, http.StatusOK, s.browser(t).login(names["memberA"], testPassword).StatusCode)
	assert.EqualValues(t, 1, auth.Computations()-before, "and a success costs the same")
}

// docs/adr/0033 D6, docs/adr/0039 D6: five failures of one username within
// fifteen minutes lock it — a username nobody has too — and the lock ends with
// the window. A locked username answers the right password like a wrong one,
// and failures that are older than the window count for nothing.
func TestLockoutEndsWithTheWindow(t *testing.T) {
	w := newWorld(t)
	names := withAccounts(t, w)
	c := newClock()
	s := newAPI(t, withLogin, withClock(c))
	known, unknown := names["memberA"], uniqueSlug("ghost")

	for _, name := range []string{known, unknown} {
		b := s.browser(t)
		for range 5 {
			assertProblem(t, b.login(name, "wrong password!"), http.StatusUnauthorized, "invalid_credentials")
		}
		assert.True(t, scalar[bool](t, `SELECT EXISTS (SELECT 1 FROM login_locks WHERE username = $1)`, name),
			"%s is locked — whether or not an account has the name", name)
		refused := b.login(name, testPassword)
		assertProblem(t, refused, http.StatusUnauthorized, "invalid_credentials")
		assert.Empty(t, refused.Cookies())
	}

	c.Advance(14 * time.Minute)
	assertProblem(t, s.browser(t).login(known, testPassword), http.StatusUnauthorized, "invalid_credentials")
	c.Advance(2 * time.Minute)
	require.Equal(t, http.StatusOK, s.browser(t).login(known, testPassword).StatusCode, "the window has passed")

	// Failures spread over more than the window never lock.
	other := names["viewerA"]
	b := s.browser(t)
	for range 4 {
		assertProblem(t, b.login(other, "wrong password!"), http.StatusUnauthorized, "invalid_credentials")
	}
	c.Advance(16 * time.Minute)
	for range 4 {
		assertProblem(t, b.login(other, "wrong password!"), http.StatusUnauthorized, "invalid_credentials")
	}
	require.Equal(t, http.StatusOK, b.login(other, testPassword).StatusCode, "four and four in two windows are not five")

	// The record: the lock is an act naming the person, the failures give no
	// attempted password and no unknown username.
	assert.EqualValues(t, 1, scalar[int64](t, `SELECT count(*) FROM audit_events WHERE action = 'locked'
		AND actor_system = 'system:login' AND entity_id = $1`, w.MemberA))
	assert.GreaterOrEqual(t, scalar[int64](t, `SELECT count(*) FROM audit_events WHERE action = 'login_failed' AND entity_id = $1
		AND reason = 'wrong_password'`, w.MemberA), int64(5))
	assert.EqualValues(t, 0, scalar[int64](t, `SELECT count(*) FROM audit_events WHERE action IN ('login_failed', 'locked')
		AND (before::text || after::text || coalesce(note, '') || coalesce(reason, '')) ~ ($1 || '|wrong password|' || $2)`, unknown, testPassword))
	assert.GreaterOrEqual(t, scalar[int64](t, `SELECT count(*) FROM audit_events WHERE action = 'login_failed' AND entity_id IS NULL
		AND reason = 'unknown_account'`), int64(5))
}

// docs/adr/0033 D6 with COWORK_LOGIN_LOCKOUT=admin: a lock stays until an
// administrator unlocks it, however long the window has passed; the lock of a
// username nobody has ends with the window, which no answer can show.
func TestLockoutStaysUntilAnAdministratorUnlocks(t *testing.T) {
	w := newWorld(t)
	names := withAccounts(t, w)
	c := newClock()
	s := newAPI(t, withLogin, withClock(c), func(o *api.Options) { o.LoginLockout = "admin" })
	known, ghost := names["memberA"], uniqueSlug("ghost")

	b := s.browser(t)
	for range 5 {
		assertProblem(t, b.login(known, "wrong password!"), http.StatusUnauthorized, "invalid_credentials")
		assertProblem(t, b.login(ghost, "wrong password!"), http.StatusUnauthorized, "invalid_credentials")
	}
	assert.True(t, scalar[bool](t, `SELECT sticky FROM login_locks WHERE username = $1`, known))
	assert.False(t, scalar[bool](t, `SELECT sticky FROM login_locks WHERE username = $1`, ghost), "nobody can unlock a name nobody has")

	c.Advance(48 * time.Hour)
	assertProblem(t, b.login(known, testPassword), http.StatusUnauthorized, "invalid_credentials")

	admin := s.browser(t)
	admin.mustLogin(names["adminA"], testPassword)
	accounts := admin.get("/api/v1/teams/" + w.SlugA + "/accounts")
	require.Equal(t, http.StatusOK, accounts.StatusCode)
	var locked bool
	for _, a := range decode[apigen.AccountList](t, accounts).Items {
		if a.Username == known {
			locked = a.Locked
		}
	}
	assert.True(t, locked, "the administrator sees the lock")

	require.Equal(t, http.StatusNoContent, admin.request(http.MethodDelete, "/api/v1/teams/"+w.SlugA+"/accounts/"+known+"/lockout", nil).StatusCode)
	require.Equal(t, http.StatusOK, b.login(known, testPassword).StatusCode)
	assert.EqualValues(t, 1, scalar[int64](t, `SELECT count(*) FROM audit_events WHERE action = 'unlocked' AND entity_id = $1 AND actor_user_id = $2 AND tenant_id = $3`,
		w.MemberA, w.AdminA, w.A))
	// Unlocking what is not locked changes nothing and records nothing.
	require.Equal(t, http.StatusNoContent, admin.request(http.MethodDelete, "/api/v1/teams/"+w.SlugA+"/accounts/"+known+"/lockout", nil).StatusCode)
	assert.EqualValues(t, 1, scalar[int64](t, `SELECT count(*) FROM audit_events WHERE action = 'unlocked' AND entity_id = $1`, w.MemberA))
}

// docs/adr/0033 D6: more attempts from one address within a minute than the
// limit allows are 429 — the right password too, before any hash is computed —
// and the minute slides. The server key is the test's own, so its address is
// not the one the other tests of the run share.
func TestAddressThrottle(t *testing.T) {
	w := newWorld(t)
	names := withAccounts(t, w)
	c := newClock()
	s := newAPI(t, withLogin, withClock(c), func(o *api.Options) {
		o.SessionKey = []byte("address-throttle-key-0123456789!")
		o.LoginAddressLimit = 3
		o.LoginMaxFailures = 0
	})
	b := s.browser(t)
	for range 3 {
		assertProblem(t, b.login(names["memberA"], "wrong password!"), http.StatusUnauthorized, "invalid_credentials")
	}
	before := auth.Computations()
	res := b.login(names["memberA"], testPassword)
	assert.Equal(t, "60", res.Header.Get("Retry-After"))
	assertProblem(t, res, http.StatusTooManyRequests, "too_many_attempts")
	assertProblem(t, b.login(uniqueSlug("ghost"), "x"), http.StatusTooManyRequests, "too_many_attempts")
	assert.Zero(t, auth.Computations()-before, "a throttled attempt computes nothing")

	c.Advance(61 * time.Second)
	assert.Equal(t, http.StatusOK, b.login(names["memberA"], testPassword).StatusCode, "the minute has passed; the throttled attempts did not count")

	// A success is an attempt too.
	for range 2 {
		assert.Equal(t, http.StatusOK, b.login(names["memberA"], testPassword).StatusCode)
	}
	assertProblem(t, b.login(names["memberA"], testPassword), http.StatusTooManyRequests, "too_many_attempts")

	// A limit of 0 switches the throttle off (docs/adr/0039 D6).
	open := newAPI(t, withLogin, func(o *api.Options) {
		o.SessionKey = []byte("another-key-0123456789abcdef0123")
		o.LoginAddressLimit = 0
	})
	for range 6 {
		assert.Equal(t, http.StatusOK, open.browser(t).login(names["viewerA"], testPassword).StatusCode)
	}
}

// docs/adr/0033 D6: the throttle holds for a burst of parallel attempts of one
// address — the count and the attempt are one step under the address's lock,
// before any hash is computed — so the burst makes as many guesses as the
// limit allows and no more; each attempt is one row, its reservation replaced
// by its outcome. The current password of a change is held to the same
// throttle, where the lockout is switched off.
func TestTheAddressThrottleHoldsForParallelAttemptsAndThePasswordChange(t *testing.T) {
	w := newWorld(t)
	names := withAccounts(t, w)
	const limit = 4
	throttled := func(key string) apiServer {
		return newAPI(t, withLogin, func(o *api.Options) {
			o.SessionKey = []byte(key)
			o.LoginAddressLimit = limit
			o.LoginMaxFailures = 0
		})
	}
	s := throttled("parallel-throttle-key-0123456789")
	body, err := json.Marshal(map[string]string{"username": names["memberA"], "password": "wrong password!"})
	require.NoError(t, err)
	login := func() int {
		req, err := http.NewRequest(http.MethodPost, s.URL+"/auth/local", bytes.NewReader(body))
		if err != nil {
			return 0
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", testOrigin)
		res, err := browserClient.Do(req)
		if err != nil {
			return 0
		}
		_ = res.Body.Close()
		return res.StatusCode
	}
	guesses := 0
	for _, code := range simultaneously(times(6*limit, login)...) {
		switch code {
		case http.StatusUnauthorized:
			guesses++
		case http.StatusTooManyRequests:
		default:
			t.Errorf("a parallel login answered %d", code)
		}
	}
	assert.Equal(t, limit, guesses, "the burst made as many guesses as the limit allows")
	assert.EqualValues(t, limit, scalar[int64](t, `SELECT count(*) FROM login_attempts WHERE username = $1 AND failed`, names["memberA"]))
	assert.Zero(t, scalar[int64](t, `SELECT count(*) FROM login_attempts WHERE username = $1 AND NOT failed`, names["memberA"]),
		"every reservation gave way to its outcome")

	change := throttled("password-change-throttle-key-012")
	session := sessionOf(t, w.ViewerA)
	wrong := map[string]string{"current_password": "not the password", "new_password": "a new password of length"}
	before := auth.Computations()
	for range limit {
		assertProblem(t, change.do(t, session, http.MethodPut, "/api/v1/me/password", wrong), http.StatusBadRequest, "validation_failed")
	}
	assert.EqualValues(t, limit, auth.Computations()-before, "each counted guess computed one hash")
	res := change.do(t, session, http.MethodPut, "/api/v1/me/password", wrong)
	assertProblem(t, res, http.StatusTooManyRequests, "too_many_attempts")
	assert.Equal(t, "60", res.Header.Get("Retry-After"))
	assert.EqualValues(t, limit, auth.Computations()-before, "a throttled change computes nothing")
	assert.Zero(t, scalar[int64](t, `SELECT count(*) FROM login_locks WHERE username = $1`, names["viewerA"]), "the lockout is off")
}

// docs/adr/0035 D2, docs/adr/0033 D6: behind a trusted proxy the throttle counts
// the client, not the proxy. The test server plays the proxy — every request
// reaches the backend from 127.0.0.1, which COWORK_TRUSTED_PROXIES names — and
// the clients are the entries of X-Forwarded-For, read from the right.
func TestAddressThrottleCountsTheClientBehindTrustedProxies(t *testing.T) {
	w := newWorld(t)
	names := withAccounts(t, w)
	s := newAPI(t, withLogin, func(o *api.Options) {
		o.SessionKey = []byte("trusted-proxies-throttle-key-01!")
		o.LoginAddressLimit = 3
		o.LoginMaxFailures = 0
		o.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("::1/128")}
	})
	attempt := func(chain ...string) *http.Response {
		opts := make([]reqOpt, 0, len(chain))
		for _, line := range chain {
			opts = append(opts, func(r *http.Request) { r.Header.Add("X-Forwarded-For", line) })
		}
		return s.browser(t).login(names["memberA"], "wrong password!", opts...)
	}
	exhaust := func(chain ...string) {
		t.Helper()
		for range 3 {
			assertProblem(t, attempt(chain...), http.StatusUnauthorized, "invalid_credentials")
		}
		assertProblem(t, attempt(chain...), http.StatusTooManyRequests, "too_many_attempts")
	}

	// Two clients behind the one proxy have a bucket each: the first being
	// throttled does not touch the second.
	exhaust("203.0.113.1")
	for range 3 {
		assertProblem(t, attempt("203.0.113.2"), http.StatusUnauthorized, "invalid_credentials")
	}
	assertProblem(t, attempt("203.0.113.2"), http.StatusTooManyRequests, "too_many_attempts")

	// What stands to the left of the client the proxy saw was written by the
	// client, and moves nobody: the exhausted client cannot slip into another
	// bucket by claiming one, and a fresh client cannot be framed as the
	// exhausted one.
	assertProblem(t, attempt("203.0.113.99, 203.0.113.1"), http.StatusTooManyRequests, "too_many_attempts")
	assertProblem(t, attempt("203.0.113.99", "203.0.113.1"), http.StatusTooManyRequests, "too_many_attempts")
	assertProblem(t, attempt("203.0.113.1, 203.0.113.3"), http.StatusUnauthorized, "invalid_credentials")
	assertProblem(t, attempt("203.0.113.2, 203.0.113.4"), http.StatusUnauthorized, "invalid_credentials")
	// A trusted-looking claim on the left is no more than any other.
	assertProblem(t, attempt("127.0.0.1, 203.0.113.5"), http.StatusUnauthorized, "invalid_credentials")

	// One client is one bucket however its address is written.
	assertProblem(t, attempt("::ffff:203.0.113.1"), http.StatusTooManyRequests, "too_many_attempts")
	exhaust("2001:db8::1")
	assertProblem(t, attempt("2001:0DB8:0:0:0:0:0:1"), http.StatusTooManyRequests, "too_many_attempts")

	// A hop of ours that appended its own address is walked through; an entry
	// that is no address stops the walk, and the client is the hop before it.
	exhaust("203.0.113.6, 127.0.0.5")
	assertProblem(t, attempt("203.0.113.7, 127.0.0.5"), http.StatusUnauthorized, "invalid_credentials")
	exhaust("garbage")
	assertProblem(t, attempt("203.0.113.1, garbage"), http.StatusTooManyRequests, "too_many_attempts")

	// The hop before the malformed entry was the TCP peer itself: that is the
	// bucket of a request with no header at all.
	assertProblem(t, attempt(), http.StatusTooManyRequests, "too_many_attempts")
}

// docs/adr/0035 D2: a peer that is not a trusted proxy is the client, whatever
// X-Forwarded-For says — and with no trusted proxy at all, which is the default,
// the header is never read. Nobody chooses their bucket with a header.
func TestAddressThrottleIgnoresTheHeaderOfAnUntrustedPeer(t *testing.T) {
	w := newWorld(t)
	names := withAccounts(t, w)
	for name, proxies := range map[string][]netip.Prefix{
		"no trusted proxy":           nil,
		"a network that is not ours": {netip.MustParsePrefix("10.0.0.0/8")},
	} {
		t.Run(name, func(t *testing.T) {
			s := newAPI(t, withLogin, func(o *api.Options) {
				o.SessionKey = []byte("untrusted-peer/" + name + "/0123456789abcdef")
				o.LoginAddressLimit = 3
				o.LoginMaxFailures = 0
				o.TrustedProxies = proxies
			})
			attempt := func(chain string) *http.Response {
				return s.browser(t).login(names["memberA"], "wrong password!", withHeader("X-Forwarded-For", chain))
			}
			for _, chain := range []string{"203.0.113.1", "203.0.113.2", "203.0.113.3"} {
				assertProblem(t, attempt(chain), http.StatusUnauthorized, "invalid_credentials")
			}
			for _, chain := range []string{"203.0.113.4", "203.0.113.1, 203.0.113.5", "10.0.0.1"} {
				assertProblem(t, attempt(chain), http.StatusTooManyRequests, "too_many_attempts")
			}
		})
	}
}

// docs/adr/0032 D5: while no tenant exists only global administrators may log
// in; anyone else with the right password gets 403 not_initialised and no
// session — and a wrong password stays a 401, so the 403 tells nothing to
// someone who does not know the password. The first tenant ends the state.
func TestInitStateAdmitsGlobalAdministratorsOnly(t *testing.T) {
	ctx := context.Background()
	iso := newIsolated(t)
	root, err := iso.F.Person(ctx, "root", "Root")
	require.NoError(t, err)
	require.NoError(t, iso.F.GlobalAdmin(ctx, root))
	require.NoError(t, iso.F.Account(ctx, root, testPassword, uuid.Nil, false))
	plain, err := iso.F.Person(ctx, "plain", "Plain")
	require.NoError(t, err)
	require.NoError(t, iso.F.Account(ctx, plain, testPassword, uuid.Nil, false))
	s := newAPI(t, withLogin, iso.option)

	options := s.browser(t).get("/auth/options")
	require.Equal(t, http.StatusOK, options.StatusCode)
	assert.Equal(t, map[string]any{"local": true, "oidc": false, "oidc_name": nil, "password_min_length": float64(12),
		"token_max_lifetime_days": float64(365)}, decode[map[string]any](t, options))

	b := s.browser(t)
	res := b.login("plain", testPassword)
	body := assertProblem(t, res, http.StatusForbidden, "not_initialised")
	assert.Contains(t, body["detail"], "not initialised")
	assert.Empty(t, res.Cookies(), "no session")
	assertProblem(t, b.login("plain", "wrong password!"), http.StatusUnauthorized, "invalid_credentials")
	n, err := iso.F.QueryCount(ctx, `SELECT count(*) FROM sessions`)
	require.NoError(t, err)
	assert.Zero(t, n)

	admin := s.browser(t)
	admin.mustLogin("root", testPassword)
	me := decode[apigen.Me](t, admin.get("/api/v1/me"))
	assert.True(t, me.GlobalAdmin)
	assert.Empty(t, me.Memberships, "the create-the-first-tenant page")

	created := admin.request(http.MethodPost, "/api/v1/teams", map[string]string{"slug": "first", "name": "First"})
	require.Equal(t, http.StatusCreated, created.StatusCode)
	assert.Equal(t, "/api/v1/teams/first", created.Header.Get("Location"))
	me = decode[apigen.Me](t, admin.get("/api/v1/me"))
	require.Len(t, me.Memberships, 1)
	assert.Equal(t, apigen.RoleAdmin, me.Memberships[0].Role, "the creator is its first administrator (docs/adr/0032 D7)")

	require.Equal(t, http.StatusOK, s.browser(t).login("plain", testPassword).StatusCode, "the installation is initialised")
}

// docs/adr/0033 D8: the login page offers the local form when an active local
// account exists, and never an identity provider that is not there.
func TestAuthOptions(t *testing.T) {
	ctx := context.Background()
	iso := newIsolated(t)
	s := newAPI(t, withLogin, iso.option)
	got := decode[map[string]any](t, s.browser(t).get("/auth/options"))
	assert.Equal(t, map[string]any{"local": false, "oidc": false, "oidc_name": nil, "password_min_length": float64(12),
		"token_max_lifetime_days": float64(365)}, got, "no account: the login page says it is not configured")

	longer := newAPI(t, withLogin, iso.option, func(o *api.Options) { o.PasswordMinLength = 20 })
	got = decode[map[string]any](t, longer.browser(t).get("/auth/options"))
	assert.Equal(t, float64(20), got["password_min_length"], "the policy the password forms follow")

	// docs/adr/0035 D4: the longest lifetime of a new token, in whole days rounded down, is the
	// bound of the token form; under a day only the default fits.
	for lifetime, days := range map[time.Duration]float64{30 * 24 * time.Hour: 30, 36 * time.Hour: 1, 12 * time.Hour: 0} {
		bounded := newAPI(t, withLogin, iso.option, func(o *api.Options) {
			o.TokenDefaultLifetime, o.TokenMaxLifetime = lifetime, lifetime
		})
		got = decode[map[string]any](t, bounded.browser(t).get("/auth/options"))
		assert.Equal(t, days, got["token_max_lifetime_days"], "COWORK_TOKEN_MAX_LIFETIME=%s", lifetime)
	}

	person, err := iso.F.Person(ctx, "ada", "Ada")
	require.NoError(t, err)
	require.NoError(t, iso.F.Account(ctx, person, testPassword, uuid.Nil, false))
	got = decode[map[string]any](t, s.browser(t).get("/auth/options"))
	assert.Equal(t, true, got["local"])

	require.NoError(t, iso.F.Exec(ctx, `UPDATE users SET deactivated_at = now() WHERE id = $1`, person))
	got = decode[map[string]any](t, s.browser(t).get("/auth/options"))
	assert.Equal(t, false, got["local"], "a deactivated account offers nothing")
}

func TestAuthRoutesKnowTheirMethods(t *testing.T) {
	s := newAPI(t, withLogin)
	b := s.browser(t)
	res := b.get("/auth/local")
	assertProblem(t, res, http.StatusMethodNotAllowed, "method_not_allowed")
	assert.Equal(t, "POST", res.Header.Get("Allow"))
	assertProblem(t, b.get("/auth/nothing"), http.StatusNotFound, "not_found")
	assertProblem(t, b.request(http.MethodPost, "/auth/options", nil), http.StatusMethodNotAllowed, "method_not_allowed")
	bad := b.request(http.MethodPost, "/auth/local", `{"username":"ada"}`)
	assertProblem(t, bad, http.StatusBadRequest, "validation_failed")
	long := b.request(http.MethodPost, "/auth/local", map[string]string{"username": "ada", "password": strings.Repeat("x", 1025)})
	assertProblem(t, long, http.StatusBadRequest, "validation_failed")
}

// docs/adr/0031 D3: an absolute limit and an idle limit, and a request within
// the idle window extends the session up to the absolute one.
func TestSessionLifetimes(t *testing.T) {
	w := newWorld(t)
	names := withAccounts(t, w)
	c := newClock()
	s := newAPI(t, withLogin, withClock(c))

	idle := s.browser(t)
	idle.mustLogin(names["memberA"], testPassword)
	c.Advance(119 * time.Minute)
	require.Equal(t, http.StatusOK, idle.get("/api/v1/me").StatusCode, "inside the idle window")
	c.Advance(90 * time.Minute)
	require.Equal(t, http.StatusOK, idle.get("/api/v1/me").StatusCode, "a use extended the session")
	c.Advance(2*time.Hour + time.Minute)
	res := idle.get("/api/v1/me")
	assertProblem(t, res, http.StatusUnauthorized, "unauthenticated")
	var cleared bool
	for _, ck := range res.Cookies() {
		cleared = cleared || (ck.Name == auth.SessionCookie && ck.MaxAge < 0)
	}
	assert.True(t, cleared, "the answer tells the browser to drop the cookie")
	assert.Equal(t, `Bearer realm="cowork"`, res.Header.Get("WWW-Authenticate"))

	absolute := s.browser(t)
	absolute.mustLogin(names["viewerA"], testPassword)
	for range 11 {
		c.Advance(time.Hour)
		require.Equal(t, http.StatusOK, absolute.get("/api/v1/me").StatusCode)
	}
	c.Advance(59 * time.Minute)
	require.Equal(t, http.StatusOK, absolute.get("/api/v1/me").StatusCode, "just inside the twelve hours")
	c.Advance(2 * time.Minute)
	assertProblem(t, absolute.get("/api/v1/me"), http.StatusUnauthorized, "unauthenticated")
	assert.Equal(t, http.StatusOK, idle.login(names["memberA"], testPassword).StatusCode, "a new login starts a new session")
}

// docs/adr/0031 D3 as amended 2026-10-07: every request of a session moves its
// idle clock, at most once a minute — a read, the event stream's connection, a
// write — but a write the CSRF check refuses, so a forged write from a page of
// the same site extends no session. An open tab whose stream reconnects keeps
// its session up to the absolute limit (docs/security/sessions.md H-109).
func TestEveryRequestButARefusedWriteMovesTheIdleClock(t *testing.T) {
	w := newWorld(t)
	names := withAccounts(t, w)
	c := newClock()
	s := newAPI(t, withLogin, withClock(c))
	b := s.browser(t)
	b.mustLogin(names["memberA"], testPassword)
	hash := auth.HashSession(b.Cookie)
	seen := func() time.Time {
		t.Helper()
		return scalar[time.Time](t, `SELECT last_seen_at FROM sessions WHERE token_hash = $1`, hash[:])
	}
	token := map[string]any{"name": "script", "scope": "read"}
	login := seen()

	c.Advance(2 * time.Minute)
	assertProblem(t, b.request(http.MethodPost, "/api/v1/me/tokens", token, without("X-Requested-With")), http.StatusForbidden, "csrf")
	assert.Equal(t, login, seen(), "a write the CSRF check refuses")
	assertProblem(t, b.request(http.MethodPost, "/api/v1/me/tokens", token, withHeader("Origin", "https://a.example.com")), http.StatusForbidden, "csrf")
	assert.Equal(t, login, seen(), "a write from a sibling host")

	require.Equal(t, http.StatusOK, b.get("/api/v1/me").StatusCode)
	moved := seen()
	assert.WithinDuration(t, c.Now(), moved, time.Millisecond, "a read")

	c.Advance(30 * time.Second)
	require.Equal(t, http.StatusOK, b.get("/api/v1/me").StatusCode)
	require.Equal(t, http.StatusCreated, b.request(http.MethodPost, "/api/v1/me/tokens", token).StatusCode)
	assert.Equal(t, moved, seen(), "at most once a minute")

	c.Advance(31 * time.Second)
	require.Equal(t, http.StatusCreated, b.request(http.MethodPost, "/api/v1/me/tokens", token).StatusCode)
	moved = seen()
	assert.WithinDuration(t, c.Now(), moved, time.Millisecond, "a write")

	// A tab nobody uses: only its stream reconnects, an hour apart, and the
	// session lives on until its absolute limit.
	for range 10 {
		c.Advance(time.Hour)
		stream := b.get("/api/v1/teams/" + w.SlugA + "/events")
		require.Equal(t, http.StatusOK, stream.StatusCode)
		require.NoError(t, stream.Body.Close())
		assert.WithinDuration(t, c.Now(), seen(), time.Millisecond, "the event stream's connection")
	}
	c.Advance(119 * time.Minute)
	assertProblem(t, b.get("/api/v1/me"), http.StatusUnauthorized, "unauthenticated")
}

// docs/adr/0031 D4, D6, docs/adr/0027 D5: a session lives in the database, so
// another server over the same database serves it, and the job that removes
// what expired takes nothing that is alive.
func TestSessionSurvivesARestartAndTheExpiryJobKeepsTheLiving(t *testing.T) {
	w := newWorld(t)
	names := withAccounts(t, w)
	c := newClock()
	first := newAPI(t, withLogin, withClock(c))
	b := first.browser(t)
	b.mustLogin(names["memberA"], testPassword)

	second := newAPI(t, withLogin, withClock(c))
	b.s = second
	me := b.get("/api/v1/me")
	require.Equal(t, http.StatusOK, me.StatusCode, "a new server over the same database")
	assert.Equal(t, w.MemberA, decode[apigen.Me](t, me).Id)

	db := openRuntime(t)
	removed, err := db.ExpireSessions(context.Background(), c.Now(), 2*time.Hour)
	require.NoError(t, err)
	assert.Zero(t, removed)
	require.Equal(t, http.StatusOK, b.get("/api/v1/me").StatusCode)

	dead := second.browser(t)
	dead.mustLogin(names["viewerA"], testPassword)
	c.Advance(3 * time.Hour)
	removed, err = db.ExpireSessions(context.Background(), c.Now(), 2*time.Hour)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, removed, int64(2), "the idle sessions of this test at least")
	assertProblem(t, dead.get("/api/v1/me"), http.StatusUnauthorized, "unauthenticated")
	assert.GreaterOrEqual(t, scalar[int64](t, `SELECT count(*) FROM audit_events WHERE action = 'expired' AND entity_type = 'sessions'
		AND actor_system = 'system:session-expiry'`), int64(1))
}

// loginLockNamespace is the first key of the lock an attempt takes on its
// username, "cowl" (store.RecordLoginAttempt).
const loginLockNamespace int32 = 0x636f776c

// holdLoginLock takes a username's login lock in a transaction of the test's
// own over the administrative connection; release ends the transaction.
func holdLoginLock(t *testing.T, username string) (release func()) {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, env.AdminURL)
	require.NoError(t, err)
	tx, err := conn.Begin(ctx)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1, hashtext($2))", loginLockNamespace, username)
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

// waitForLoginLock returns once a transaction waits for the username's login
// lock, and fails when the request answered without waiting for it.
func waitForLoginLock(t *testing.T, username string, answered <-chan *http.Response) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		select {
		case res := <-answered:
			require.Failf(t, "the login did not wait for its username's lock", "it answered %d", res.StatusCode)
		default:
		}
		if scalar[int64](t, `SELECT count(*) FROM pg_locks WHERE locktype = 'advisory' AND NOT granted
			AND classid = $1::int4::oid AND objid = hashtext($2)::oid AND objsubid = 2`, loginLockNamespace, username) > 0 {
			return
		}
	}
	require.Fail(t, "no login waited for its username's lock")
}

// docs/adr/0033 D4, docs/adr/0031 D4: a login that verified the old password
// makes no session once the password changed before its session was made. The
// login waits for its username's lock — after it verified the password —
// while an administrator resets the password and commits; it then answers
// like a wrong password, and the reset's end of every session holds.
func TestALoginInFlightMakesNoSessionAfterThePasswordChanged(t *testing.T) {
	w := newWorld(t)
	names := withAccounts(t, w)
	s := newAPI(t, withLogin)
	admin := s.browser(t)
	admin.mustLogin(names["adminA"], testPassword)
	target := names["memberA"]

	release := holdLoginLock(t, target)
	answered := make(chan *http.Response, 1)
	go func() { answered <- s.browser(t).login(target, testPassword) }()
	waitForLoginLock(t, target, answered)
	reset := admin.request(http.MethodPut, accountsPath(w.SlugA, "/", target, "/password"), map[string]string{"temporary_password": "a reset temporary one"})
	require.Equal(t, http.StatusNoContent, reset.StatusCode)
	release()
	assertProblem(t, <-answered, http.StatusUnauthorized, "invalid_credentials")
	assert.Zero(t, scalar[int64](t, `SELECT count(*) FROM sessions WHERE user_id = $1`, w.MemberA), "the old password made no session")
	assert.Equal(t, http.StatusOK, s.browser(t).login(target, "a reset temporary one").StatusCode, "the new password signs in")
}

// docs/adr/0031 D4, docs/adr/0037 D5: logout deletes the row and clears the
// cookie, and a write of a session — logout included — needs the CSRF headers.
func TestLogoutEndsTheSession(t *testing.T) {
	w := newWorld(t)
	names := withAccounts(t, w)
	tokens := issueTokens(t, w)
	s := newAPI(t, withLogin)
	b := s.browser(t)
	b.mustLogin(names["memberA"], testPassword)
	hash := auth.HashSession(b.Cookie)

	assertProblem(t, b.request(http.MethodPost, "/auth/logout", nil, without("X-Requested-With")), http.StatusForbidden, "csrf")
	assertProblem(t, b.request(http.MethodPost, "/auth/logout", nil, withHeader("Origin", "https://evil.example.com")), http.StatusForbidden, "csrf")
	require.Equal(t, http.StatusOK, b.get("/api/v1/me").StatusCode, "a refused logout ended nothing")

	assertProblem(t, s.browser(t).request(http.MethodPost, "/auth/logout", nil), http.StatusUnauthorized, "unauthenticated")
	assertProblem(t, s.browser(t).request(http.MethodPost, "/auth/logout", nil, withBearer(tokens.MemberA)), http.StatusForbidden, "session_required")

	res := b.request(http.MethodPost, "/auth/logout", nil)
	require.Equal(t, http.StatusNoContent, res.StatusCode)
	var cleared bool
	for _, ck := range res.Cookies() {
		cleared = cleared || (ck.Name == auth.SessionCookie && ck.MaxAge < 0 && ck.Value == "")
	}
	assert.True(t, cleared)
	assert.Zero(t, scalar[int64](t, `SELECT count(*) FROM sessions WHERE token_hash = $1`, hash[:]))
	assertProblem(t, b.get("/api/v1/me"), http.StatusUnauthorized, "unauthenticated")
	assert.EqualValues(t, 1, scalar[int64](t, `SELECT count(*) FROM audit_events WHERE action = 'logged_out' AND actor_user_id = $1`, w.MemberA))
}

// docs/adr/0027 D5, docs/adr/0033 D6: the job that cleans the login's tables
// removes the attempts and the locks that ended with the window and keeps what
// still counts — a sticky lock for good — and records one act when it removed
// anything.
func TestExpiryOfTheLoginsState(t *testing.T) {
	ctx := context.Background()
	f := fixtures(t)
	db := openRuntime(t)
	old, fresh := uniqueSlug("old"), uniqueSlug("fresh")
	for name, age := range map[string]string{old: "20 minutes", fresh: "1 minute"} {
		require.NoError(t, f.Exec(ctx, `INSERT INTO login_attempts (username, address, failed, created_at)
			VALUES ($1, decode(repeat('01', 32), 'hex'), true, now() - $2::interval)`, name, age))
		require.NoError(t, f.Exec(ctx, `INSERT INTO login_locks (username, locked_at, sticky) VALUES ($1, now() - $2::interval, false)`, name, age))
	}
	sticky := uniqueSlug("sticky")
	require.NoError(t, f.Exec(ctx, `INSERT INTO login_locks (username, locked_at, sticky) VALUES ($1, now() - interval '3 days', true)`, sticky))

	removed, err := db.ExpireLoginState(ctx, time.Now(), 15*time.Minute)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, removed, int64(2))
	assert.Zero(t, scalar[int64](t, `SELECT count(*) FROM login_attempts WHERE username = $1`, old))
	assert.Zero(t, scalar[int64](t, `SELECT count(*) FROM login_locks WHERE username = $1`, old))
	assert.EqualValues(t, 1, scalar[int64](t, `SELECT count(*) FROM login_attempts WHERE username = $1`, fresh))
	assert.EqualValues(t, 1, scalar[int64](t, `SELECT count(*) FROM login_locks WHERE username = $1`, fresh))
	assert.EqualValues(t, 1, scalar[int64](t, `SELECT count(*) FROM login_locks WHERE username = $1`, sticky), "a lock an administrator must lift stays")
	assert.GreaterOrEqual(t, scalar[int64](t, `SELECT count(*) FROM audit_events WHERE action = 'expired' AND entity_type = 'login_attempts'
		AND actor_system = 'system:login-expiry'`), int64(1))
}
