package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/oidc"
	"github.com/guided-traffic/cowork/backend/test/fakeissuer"
)

// docs/adr/0029 D1, D4: the identity provider's two routes are navigations of
// the browser, answered with redirects that the whole pipeline — the router,
// the request validator, the strict server and the response validator — lets
// through. Without a provider both send the browser back to the login page.
// The callback takes the parameters an issuer adds of its own; the start
// refuses a parameter it does not know, like every other route
// (docs/adr/0049 D4).
func TestOIDCRoutesWithoutAProvider(t *testing.T) {
	h, err := New(Options{SessionKey: make([]byte, 32), ValidateResponses: true})
	require.NoError(t, err)
	const unavailable = "/login?error=oidc_unavailable"
	for name, c := range map[string]struct {
		url      string
		status   int
		location string
	}{
		"the start":                                   {"/auth/oidc/login?return_to=%2Ft%2Facme%2Fbacklog", http.StatusSeeOther, unavailable + "&return=%2Ft%2Facme%2Fbacklog"},
		"the start without return_to":                 {"/auth/oidc/login", http.StatusSeeOther, unavailable},
		"the start with a return_to off the site":     {"/auth/oidc/login?return_to=https%3A%2F%2Fevil.example.com", http.StatusSeeOther, unavailable},
		"the callback with the issuer's parameters":   {"/auth/callback?code=c&state=s&iss=https%3A%2F%2Fissuer.example.com&session_state=x", http.StatusSeeOther, unavailable},
		"the callback with the issuer's error":        {"/auth/callback?error=access_denied&error_description=denied", http.StatusSeeOther, unavailable},
		"the start refuses a parameter it lacks":      {"/auth/oidc/login?prompt=none", http.StatusBadRequest, ""},
		"the callback takes no other method than GET": {"/auth/callback", http.StatusMethodNotAllowed, ""},
	} {
		t.Run(name, func(t *testing.T) {
			method := http.MethodGet
			if c.status == http.StatusMethodNotAllowed {
				method = http.MethodPost
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(method, c.url, nil))
			assert.Equal(t, c.status, rec.Code, rec.Body.String())
			assert.Equal(t, c.location, rec.Header().Get("Location"))
			assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
		})
	}
}

func fakeServer(t *testing.T, is *fakeissuer.Issuer, allowed []string) http.Handler {
	t.Helper()
	p, err := oidc.Discover(context.Background(), oidc.Config{Issuer: is.URL, ClientID: fakeissuer.ClientID,
		ClientSecret: fakeissuer.ClientSecret, RedirectURL: "https://cowork.test/auth/callback",
		Scopes: []string{"openid", "groups", "offline_access"}, GroupsClaim: "groups"})
	require.NoError(t, err)
	h, err := New(Options{SessionKey: make([]byte, 32), ValidateResponses: true,
		OIDC: OIDCOptions{Provider: p, AllowedGroups: allowed, DisplayName: "Fake"}})
	require.NoError(t, err)
	return h
}

func serve(h http.Handler, url string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, url, nil)
	for _, c := range cookies {
		r.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

// docs/adr/0029 D1: the start sends the browser to the issuer for the code
// flow with PKCE, a state and a nonce, which the state cookie — HttpOnly,
// Secure, SameSite=Lax, Path=/, ten minutes, sealed — holds for the callback.
func TestTheLoginStart(t *testing.T) {
	is := fakeissuer.Start(t)
	h := fakeServer(t, is, []string{"cowork-users"})
	rec := serve(h, "/auth/oidc/login?return_to=%2Ft%2Facme")
	require.Equal(t, http.StatusFound, rec.Code, rec.Body.String())
	to, err := url.Parse(rec.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, is.URL+"/auth", to.Scheme+"://"+to.Host+to.Path)
	q := to.Query()
	assert.Equal(t, "code", q.Get("response_type"))
	assert.Equal(t, "S256", q.Get("code_challenge_method"))
	assert.NotEmpty(t, q.Get("code_challenge"))
	assert.Len(t, q.Get("state"), 43, "256 bits")
	assert.Len(t, q.Get("nonce"), 43)
	cookies := (&http.Response{Header: rec.Header()}).Cookies()
	require.Len(t, cookies, 1)
	c := cookies[0]
	assert.Equal(t, "__Host-cowork-oidc", c.Name)
	assert.True(t, c.HttpOnly && c.Secure)
	assert.Equal(t, http.SameSiteLaxMode, c.SameSite)
	assert.Equal(t, "/", c.Path)
	assert.Equal(t, 600, c.MaxAge)
	assert.NotContains(t, c.Value, q.Get("state"), "sealed")
	assert.NotContains(t, c.Value, "acme")

	gateless := fakeServer(t, is, nil)
	rec = serve(gateless, "/auth/oidc/login")
	assert.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, "/login?error=oidc_unavailable", rec.Header().Get("Location"), "a gate that admits nobody offers no login (docs/adr/0030 D8)")
}

// docs/adr/0029 D1: what the callback refuses before it asks anyone — no state
// cookie, a cookie of another key, another state, the issuer's error, no code
// — and a code whose ID token does not verify are oidc_failed, with the state
// cookie cleared.
func TestTheCallbackRefusesWhatDoesNotHold(t *testing.T) {
	is := fakeissuer.Start(t)
	is.Add(fakeissuer.User{Subject: "s", Groups: []string{"cowork-users"}})
	h := fakeServer(t, is, []string{"cowork-users"})
	start := serve(h, "/auth/oidc/login")
	state := (&http.Response{Header: start.Header()}).Cookies()[0]
	to, err := url.Parse(start.Header().Get("Location"))
	require.NoError(t, err)
	good := to.Query().Get("state")
	forged := &http.Cookie{Name: state.Name, Value: "bm90IGEgc2VhbGVkIHN0YXRl"}
	for name, c := range map[string]struct {
		url    string
		cookie *http.Cookie
	}{
		"no state cookie":         {"/auth/callback?code=c&state=" + good, nil},
		"a cookie of another key": {"/auth/callback?code=c&state=" + good, forged},
		"another state":           {"/auth/callback?code=c&state=forged", state},
		"the issuer's error":      {"/auth/callback?error=access_denied&state=" + good, state},
		"no code":                 {"/auth/callback?state=" + good, state},
		"an unknown code":         {"/auth/callback?code=unknown&state=" + good, state},
	} {
		t.Run(name, func(t *testing.T) {
			var cookies []*http.Cookie
			if c.cookie != nil {
				cookies = append(cookies, c.cookie)
			}
			rec := serve(h, c.url, cookies...)
			require.Equal(t, http.StatusSeeOther, rec.Code, rec.Body.String())
			assert.Equal(t, "/login?error=oidc_failed", rec.Header().Get("Location"), "a start without return_to returns to /")
			set := (&http.Response{Header: rec.Header()}).Cookies()
			require.Len(t, set, 1, "the state cookie is cleared, no session is set")
			assert.Equal(t, "__Host-cowork-oidc", set[0].Name)
			assert.Less(t, set[0].MaxAge, 0)
		})
	}
}

// A failed login sends the browser back to the login page with the path the
// login began with, when the state cookie still opens, so the next attempt
// lands where the person wanted.
func TestAFailedCallbackKeepsThePathThePersonWanted(t *testing.T) {
	is := fakeissuer.Start(t)
	h := fakeServer(t, is, []string{"cowork-users"})
	start := serve(h, "/auth/oidc/login?return_to=%2Ft%2Facme%2Fbacklog%3Fstate%3Dopen")
	state := (&http.Response{Header: start.Header()}).Cookies()[0]
	rec := serve(h, "/auth/callback?code=c&state=another", state)
	assert.Equal(t, "/login?error=oidc_failed&return=%2Ft%2Facme%2Fbacklog%3Fstate%3Dopen", rec.Header().Get("Location"))
	rec = serve(h, "/auth/callback?code=c&state=another")
	assert.Equal(t, "/login?error=oidc_failed", rec.Header().Get("Location"), "without the cookie there is no path to keep")
	assert.Equal(t, "/login?error=not_allowed&return=%2Fx%2Fy", loginPage("not_allowed", "/x/y"))
	assert.Equal(t, "/login?error=not_allowed", loginPage("not_allowed", "/"))
}

func TestReturnTo(t *testing.T) {
	for in, want := range map[string]string{
		"/t/acme/backlog?state=open#top": "/t/acme/backlog?state=open#top",
		"/":                              "/",
		"":                               "/",
		"//evil.example.com":             "/",
		`/\evil.example.com`:             "/",
		"/a\\b":                          "/",
		"https://evil.example.com":       "/",
		"t/acme":                         "/",
		"/\t/evil.example.com":           "/",
		"/a\nb":                          "/",
		"/" + strings.Repeat("a", 2047):  "/" + strings.Repeat("a", 2047),
		"/" + strings.Repeat("a", 2048):  "/",
		"/\xff":                          "/",
	} {
		assert.Equal(t, want, safeReturnTo(in), "%q", in)
	}
}

// docs/adr/0030 D1, D8, m6 of the security review: the gate admits a person
// of the configured issuer with an allowed group or the administrator group,
// who is a global administrator; a person of another issuer, of none, or any
// person while no provider is configured, is outside it.
func TestTheGate(t *testing.T) {
	is := fakeissuer.Start(t)
	p, err := oidc.Discover(context.Background(), oidc.Config{Issuer: is.URL, ClientID: fakeissuer.ClientID, ClientSecret: fakeissuer.ClientSecret})
	require.NoError(t, err)
	h := &handler{opts: Options{OIDC: OIDCOptions{Provider: p, AllowedGroups: []string{"users"}, AdminGroup: "admins"}}}
	own := is.URL
	for name, c := range map[string]struct {
		groups          []string
		admitted, admin bool
	}{
		"an allowed group":         {[]string{"other", "users"}, true, false},
		"the administrator group":  {[]string{"admins"}, true, true},
		"both":                     {[]string{"users", "admins"}, true, true},
		"neither":                  {[]string{"other"}, false, false},
		"none":                     {nil, false, false},
		"the case of a group name": {[]string{"Users"}, false, false},
	} {
		admitted, admin := h.gate(&own, c.groups)
		assert.Equal(t, c.admitted, admitted, name)
		assert.Equal(t, c.admin, admin, name)
	}
	other := "https://another-issuer.example.com"
	admitted, admin := h.gate(&other, []string{"users", "admins"})
	assert.False(t, admitted || admin, "a person of another issuer")
	admitted, _ = h.gate(nil, []string{"users"})
	assert.False(t, admitted, "a person of no issuer")
	none := &handler{opts: Options{OIDC: OIDCOptions{AllowedGroups: []string{"users"}}}}
	admitted, _ = none.gate(&own, []string{"users"})
	assert.False(t, admitted, "no provider admits nobody")
}
