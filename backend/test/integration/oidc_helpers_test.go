//go:build integration

package integration

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api"
	"github.com/guided-traffic/cowork/backend/internal/config"
	"github.com/guided-traffic/cowork/backend/internal/oidc"
	"github.com/guided-traffic/cowork/backend/test/fakeissuer"
)

// The test issuer's client and its users' password, as hack/dex/config.yaml
// declares them: development values that protect nothing.
const (
	dexClientID     = "cowork"
	dexClientSecret = "cowork-dev-dex-secret"
	dexPassword     = "dev-only-dex"
	// testRedirect is the redirect URI the test issuer knows for the tests:
	// the test intercepts the issuer's redirect to it and replays it against
	// its own server, so the name never has to resolve.
	testRedirect = "http://cowork.test/auth/callback"
	// stateCookie is the cookie a login through the identity provider carries
	// its state in.
	stateCookieName = "__Host-cowork-oidc"
)

func dexConfig() oidc.Config {
	return oidc.Config{Issuer: env.OIDCIssuer, ClientID: dexClientID, ClientSecret: dexClientSecret, RedirectURL: testRedirect,
		Scopes: strings.Fields(config.DefaultOIDCScopes), GroupsClaim: config.DefaultOIDCGroupsClaim}
}

// dexProvider discovers the test issuer.
func dexProvider(t *testing.T) *oidc.Provider {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	p, err := oidc.Discover(ctx, dexConfig())
	require.NoError(t, err)
	return p
}

// fakeProvider discovers an issuer in the test's process, for what Dex cannot
// be made to do.
func fakeProvider(t *testing.T, is *fakeissuer.Issuer) *oidc.Provider {
	t.Helper()
	p, err := oidc.Discover(context.Background(), oidc.Config{Issuer: is.URL, ClientID: fakeissuer.ClientID,
		ClientSecret: fakeissuer.ClientSecret, RedirectURL: testRedirect, Scopes: strings.Fields(config.DefaultOIDCScopes),
		GroupsClaim: config.DefaultOIDCGroupsClaim})
	require.NoError(t, err)
	return p
}

// withIdentity gives a server an identity provider and its gate, as make
// dev's configuration has it unless a test says otherwise.
func withIdentity(p *oidc.Provider, allowed []string, admin string) func(*api.Options) {
	return func(o *api.Options) {
		o.OIDC = api.OIDCOptions{Provider: p, AllowedGroups: allowed, AdminGroup: admin, DisplayName: "Dex"}
	}
}

// devGate is make dev's gate: cowork-users may log in, cowork-admins
// administer the installation.
func devGate(p *oidc.Provider) func(*api.Options) {
	return withIdentity(p, []string{"cowork-users"}, "cowork-admins")
}

// withState sends the state cookie of a login besides the browser's session
// cookie.
func withState(value string) reqOpt {
	return func(r *http.Request) { r.AddCookie(&http.Cookie{Name: stateCookieName, Value: value}) }
}

// cookieValue returns the value a response sets for a cookie, and whether it
// sets one.
func cookieValue(res *http.Response, name string) (string, bool) {
	for _, c := range res.Cookies() {
		if c.Name == name {
			return c.Value, true
		}
	}
	return "", false
}

// oidcLogin logs the browser in through the identity provider as a browser
// does: the start sets the state cookie and sends the browser to the issuer;
// the issuer's pages are walked — Dex's password form filled for login — up to
// its redirect to testRedirect, which is replayed against the test server
// with the browser's cookies. It returns the callback's answer, and the
// browser keeps the session cookie a login sets.
func (b *browser) oidcLogin(login, returnTo string) *http.Response {
	b.t.Helper()
	start := b.get("/auth/oidc/login?return_to=" + url.QueryEscape(returnTo))
	require.Equal(b.t, http.StatusFound, start.StatusCode, "the start sends the browser to the issuer")
	state, ok := cookieValue(start, stateCookieName)
	require.True(b.t, ok, "the start sets the state cookie")
	back := walkIssuer(b.t, start.Header.Get("Location"), login)
	u, err := url.Parse(back)
	require.NoError(b.t, err)
	res := b.get("/auth/callback?"+u.RawQuery, withState(state))
	if value, ok := cookieValue(res, "__Host-cowork-session"); ok && value != "" {
		b.Cookie = value
	}
	return res
}

// walkIssuer follows the issuer's redirects and fills its password form until
// it sends the browser to testRedirect, and returns that URL.
func walkIssuer(t *testing.T, start, login string) string {
	t.Helper()
	client := &http.Client{Timeout: 10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	next := start
	for range 12 {
		res, err := client.Get(next)
		require.NoError(t, err)
		body, _ := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if res.StatusCode == http.StatusOK && strings.Contains(string(body), `name="login"`) {
			res, err = client.PostForm(next, url.Values{"login": {login}, "password": {dexPassword}})
			require.NoError(t, err)
			body, _ = io.ReadAll(res.Body)
			_ = res.Body.Close()
		}
		location := res.Header.Get("Location")
		require.NotEmpty(t, location, "the issuer answered %d without a redirect: %.300s", res.StatusCode, body)
		target, err := url.Parse(next)
		require.NoError(t, err)
		resolved, err := target.Parse(location)
		require.NoError(t, err)
		if strings.HasPrefix(resolved.String(), testRedirect) {
			return resolved.String()
		}
		next = resolved.String()
	}
	t.Fatal("the issuer never sent the browser back")
	return ""
}

// provider is a person of the identity provider made over the administrative
// connection, as their first login would make them.
func providerPerson(t *testing.T, f interface {
	Exec(ctx context.Context, sql string, args ...any) error
}, issuer, email string, verified *bool, groups []string) uuid.UUID {
	t.Helper()
	id := uuid.Must(uuid.NewV7())
	require.NoError(t, f.Exec(context.Background(),
		`INSERT INTO users (id, display_name, oidc_issuer, oidc_subject, email, email_verified, oidc_groups, oidc_groups_at, gate_checked_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, now(), now())`,
		id, strings.Split(email, "@")[0], issuer, "sub-"+id.String(), email, verified, groups))
	return id
}
