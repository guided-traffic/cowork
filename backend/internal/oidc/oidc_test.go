package oidc

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	"github.com/guided-traffic/cowork/backend/test/fakeissuer"
)

const redirect = "https://cowork.test/auth/callback"

func provider(t *testing.T, is *fakeissuer.Issuer) *Provider {
	t.Helper()
	p, err := Discover(context.Background(), Config{
		Issuer: is.URL, ClientID: fakeissuer.ClientID, ClientSecret: fakeissuer.ClientSecret, RedirectURL: redirect,
		Scopes: []string{"openid", "email", "groups", "offline_access"}, GroupsClaim: "groups",
	})
	require.NoError(t, err)
	return p
}

// login runs the code flow without a browser: a code for the next person,
// redeemed with its verifier and nonce.
func login(t *testing.T, is *fakeissuer.Issuer, p *Provider) (Identity, error) {
	t.Helper()
	verifier := oauth2.GenerateVerifier()
	return p.Exchange(context.Background(), is.Code("the-nonce", verifier, redirect), verifier, "the-nonce")
}

func bob() fakeissuer.User {
	return fakeissuer.User{Subject: "bob-sub", Email: "bob@example.com", EmailVerified: true, Name: "Bob",
		Groups: []string{"cowork-users", "team-red"}}
}

// docs/adr/0029 D1, D2, D5: the code flow with PKCE yields the verified
// person — subject, name, address, the issuer's word on it, groups — and the
// refresh token the issuer gave.
func TestExchangeYieldsTheVerifiedPerson(t *testing.T) {
	is := fakeissuer.Start(t)
	is.Add(bob())
	p := provider(t, is)
	id, err := login(t, is, p)
	require.NoError(t, err)
	assert.Equal(t, "bob-sub", id.Subject)
	assert.Equal(t, "Bob", id.Name)
	assert.Equal(t, "bob@example.com", id.Email)
	require.NotNil(t, id.EmailVerified)
	assert.True(t, *id.EmailVerified)
	assert.Equal(t, []string{"cowork-users", "team-red"}, id.Groups)
	assert.NotEmpty(t, id.RefreshToken)
	assert.Equal(t, is.URL, p.Issuer())

	u, err := url.Parse(p.AuthCodeURL("the-state", "the-nonce", "the-verifier", false))
	require.NoError(t, err)
	q := u.Query()
	assert.Equal(t, "the-state", q.Get("state"))
	assert.Equal(t, "the-nonce", q.Get("nonce"))
	assert.Equal(t, "S256", q.Get("code_challenge_method"), "PKCE")
	assert.Equal(t, oauth2.S256ChallengeFromVerifier("the-verifier"), q.Get("code_challenge"))
	assert.Equal(t, redirect, q.Get("redirect_uri"))
	assert.Equal(t, "openid email groups offline_access", q.Get("scope"))
	assert.False(t, q.Has("prompt"), "a login the person started may show the issuer's pages")
}

// docs/adr/0029 D6: a silent login asks the issuer with prompt=none to answer
// without a page of its own, and is the same code flow otherwise.
func TestASilentLoginAsksForNoPage(t *testing.T) {
	is := fakeissuer.Start(t)
	p := provider(t, is)
	u, err := url.Parse(p.AuthCodeURL("the-state", "the-nonce", "the-verifier", true))
	require.NoError(t, err)
	q := u.Query()
	assert.Equal(t, []string{"none"}, q["prompt"])
	assert.Equal(t, "the-state", q.Get("state"))
	assert.Equal(t, "the-nonce", q.Get("nonce"))
	assert.Equal(t, oauth2.S256ChallengeFromVerifier("the-verifier"), q.Get("code_challenge"))
	assert.Equal(t, "code", q.Get("response_type"))
}

// docs/adr/0029 D1: the ID token's signature, audience, expiry and nonce are
// checked, and a token that fails one is no login.
func TestExchangeRefusesAnIDTokenThatDoesNotVerify(t *testing.T) {
	for name, knob := range map[string]func(*fakeissuer.Issuer){
		"a signature of another key": func(is *fakeissuer.Issuer) { is.WrongKey = true },
		"another audience":           func(is *fakeissuer.Issuer) { is.Audience = "another-client" },
		"an expired token":           func(is *fakeissuer.Issuer) { is.Expired = true },
		"another login's nonce":      func(is *fakeissuer.Issuer) { is.Nonce = "a-replayed-nonce" },
	} {
		t.Run(name, func(t *testing.T) {
			is := fakeissuer.Start(t)
			is.Add(bob())
			p := provider(t, is)
			is.Lock()
			knob(is)
			is.Unlock()
			_, err := login(t, is, p)
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrLogin)
		})
	}
}

// A code is redeemed with its own verifier only (PKCE, docs/adr/0029 D1).
func TestExchangeNeedsTheVerifier(t *testing.T) {
	is := fakeissuer.Start(t)
	is.Add(bob())
	p := provider(t, is)
	code := is.Code("n", oauth2.GenerateVerifier(), redirect)
	_, err := p.Exchange(context.Background(), code, oauth2.GenerateVerifier(), "n")
	assert.ErrorIs(t, err, ErrLogin)
}

// docs/adr/0029 D2: a claim absent from the ID token is read from UserInfo,
// and a claim that is a string is one group.
func TestGroupsFromUserInfoAndAsAString(t *testing.T) {
	is := fakeissuer.Start(t)
	u := bob()
	u.GroupsInUserInfoOnly = true
	is.Add(u)
	p := provider(t, is)
	id, err := login(t, is, p)
	require.NoError(t, err)
	assert.Equal(t, []string{"cowork-users", "team-red"}, id.Groups, "from UserInfo")

	u = bob()
	u.Subject, u.Groups = "single", "cowork-users"
	is.Add(u)
	id, err = login(t, is, p)
	require.NoError(t, err)
	assert.Equal(t, []string{"cowork-users"}, id.Groups, "a string is one group")
}

func TestGroupsClaimShapes(t *testing.T) {
	for name, c := range map[string]struct {
		claims map[string]any
		groups []string
		known  bool
		err    bool
	}{
		"a list":                  {map[string]any{"groups": []any{"a", "b"}}, []string{"a", "b"}, true, false},
		"one string":              {map[string]any{"groups": "a"}, []string{"a"}, true, false},
		"an empty list":           {map[string]any{"groups": []any{}}, []string{}, true, false},
		"repetitions and blanks":  {map[string]any{"groups": []any{"a", "", "a", "b"}}, []string{"a", "b"}, true, false},
		"absent":                  {map[string]any{}, []string{}, false, false},
		"null":                    {map[string]any{"groups": nil}, []string{}, false, false},
		"a number among names":    {map[string]any{"groups": []any{"a", 7.0}}, nil, false, true},
		"neither list nor string": {map[string]any{"groups": map[string]any{"a": true}}, nil, false, true},
	} {
		t.Run(name, func(t *testing.T) {
			groups, known, err := Groups(c.claims, "groups")
			if c.err {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, c.groups, groups)
			assert.Equal(t, c.known, known)
		})
	}
}

func TestDisplayNameAndVerifiedAddress(t *testing.T) {
	assert.Equal(t, "Ada", displayName(map[string]any{"name": " Ada ", "preferred_username": "ada"}, "ada@x", "s"))
	assert.Equal(t, "ada", displayName(map[string]any{"preferred_username": "ada"}, "ada@x", "s"), "Dex has no preferred_username, others do")
	assert.Equal(t, "ada@x", displayName(map[string]any{}, "ada@x", "s"))
	assert.Equal(t, "s", displayName(map[string]any{}, "", "s"))
	assert.Len(t, []rune(displayName(map[string]any{"name": string(make([]rune, 300))}, "", "s")), 200)

	yes, no := true, false
	assert.Equal(t, &yes, verified(true))
	assert.Equal(t, &no, verified("false"), "some issuers send a string")
	assert.Nil(t, verified(nil))
	assert.Nil(t, verified("perhaps"))
}

// docs/adr/0030 D5: the refresh grant reads the groups again — they may have
// changed — and the issuer's rotated refresh token is the next one.
func TestRefreshReadsTheGroupsAgain(t *testing.T) {
	for name, idOnRefresh := range map[string]bool{"from UserInfo": false, "from the refreshed ID token": true} {
		t.Run(name, func(t *testing.T) {
			is := fakeissuer.Start(t)
			is.Add(bob())
			is.Lock()
			is.IDTokenOnRefresh = idOnRefresh
			is.Unlock()
			p := provider(t, is)
			id, err := login(t, is, p)
			require.NoError(t, err)
			is.SetGroups("bob-sub", []string{"cowork-users"})
			r, err := p.Refresh(context.Background(), id.RefreshToken, "bob-sub")
			require.NoError(t, err)
			assert.True(t, r.GroupsKnown)
			assert.Equal(t, []string{"cowork-users"}, r.Groups, "team-red is gone")
			assert.NotEqual(t, id.RefreshToken, r.RefreshToken, "rotated")

			_, err = p.Refresh(context.Background(), id.RefreshToken, "bob-sub")
			assert.ErrorIs(t, err, ErrRefreshRefused, "a spent refresh token is refused, as Dex refuses it")
			again, err := p.Refresh(context.Background(), r.RefreshToken, "bob-sub")
			require.NoError(t, err)
			assert.Equal(t, []string{"cowork-users"}, again.Groups)
		})
	}
}

// Groups the issuer carries nowhere at a refresh are nobody's word.
func TestRefreshWithoutTheClaim(t *testing.T) {
	is := fakeissuer.Start(t)
	u := bob()
	u.UserInfoGroups = nil
	is.Add(u)
	p := provider(t, is)
	id, err := login(t, is, p)
	require.NoError(t, err)
	is.SetGroups("bob-sub", nil)
	r, err := p.Refresh(context.Background(), id.RefreshToken, "bob-sub")
	require.NoError(t, err)
	assert.False(t, r.GroupsKnown)
}

// The rules of 2026-10-04 (the coordinator after Dex, m2 of the security
// review): an OAuth error answer of the token endpoint about the person —
// invalid_grant, Dex's invalid_request, access_denied, any other — refuses the
// refresh token, and the session ends. No answer, a 5xx, a 429 — with an
// OAuth body or without — a 4xx that is no OAuth error, and the OAuth errors
// that are a temporary state of the issuer or cowork's own configuration leave
// the session to be refreshed again later; invalid_client and
// unauthorized_client say the configuration is wrong.
func TestRefreshRefusalAndUnreachability(t *testing.T) {
	refused, served, client := "refused", "served", "client"
	for name, c := range map[string]struct {
		knob func(*fakeissuer.Issuer)
		want string
	}{
		"invalid_grant":                        {func(is *fakeissuer.Issuer) { is.RefreshError = "invalid_grant" }, refused},
		"invalid_request, Dex's answer":        {func(is *fakeissuer.Issuer) { is.RefreshError = "invalid_request" }, refused},
		"access_denied":                        {func(is *fakeissuer.Issuer) { is.RefreshError = "access_denied" }, refused},
		"an unknown OAuth error about it":      {func(is *fakeissuer.Issuer) { is.RefreshError = "consent_required" }, refused},
		"invalid_client":                       {func(is *fakeissuer.Issuer) { is.RefreshError = "invalid_client" }, client},
		"unauthorized_client":                  {func(is *fakeissuer.Issuer) { is.RefreshError = "unauthorized_client" }, client},
		"invalid_scope":                        {func(is *fakeissuer.Issuer) { is.RefreshError = "invalid_scope" }, served},
		"temporarily_unavailable":              {func(is *fakeissuer.Issuer) { is.RefreshError = "temporarily_unavailable" }, served},
		"slow_down":                            {func(is *fakeissuer.Issuer) { is.RefreshError = "slow_down" }, served},
		"server_error":                         {func(is *fakeissuer.Issuer) { is.RefreshError = "server_error" }, served},
		"a 429 with an OAuth body":             {func(is *fakeissuer.Issuer) { is.RefreshStatus, is.RefreshError = 429, "invalid_grant" }, served},
		"a 503 with an OAuth body":             {func(is *fakeissuer.Issuer) { is.RefreshStatus, is.RefreshError = 503, "invalid_grant" }, served},
		"a 503":                                {func(is *fakeissuer.Issuer) { is.RefreshStatus = http.StatusServiceUnavailable }, served},
		"a 500":                                {func(is *fakeissuer.Issuer) { is.RefreshStatus = http.StatusInternalServerError }, served},
		"a 429 without an OAuth error":         {func(is *fakeissuer.Issuer) { is.RefreshStatus = http.StatusTooManyRequests }, served},
		"a 400 that is no OAuth error":         {func(is *fakeissuer.Issuer) { is.RefreshStatus = http.StatusBadRequest }, served},
		"a dropped connection":                 {func(is *fakeissuer.Issuer) { is.CloseOnRefresh = true }, served},
		"an issuer that is not there any more": {nil, served},
	} {
		t.Run(name, func(t *testing.T) {
			is := fakeissuer.Start(t)
			is.Add(bob())
			p := provider(t, is)
			id, err := login(t, is, p)
			require.NoError(t, err)
			if c.knob == nil {
				is.Stop()
			} else {
				is.Lock()
				c.knob(is)
				is.Unlock()
			}
			_, err = p.Refresh(context.Background(), id.RefreshToken, "bob-sub")
			require.Error(t, err)
			assert.Equal(t, c.want == refused, errors.Is(err, ErrRefreshRefused), err.Error())
			assert.Equal(t, c.want == client, errors.Is(err, ErrClientRejected), err.Error())
			assert.NotContains(t, err.Error(), "the fake issuer says", "an error never carries the answer's body")
		})
	}
}

// Item 12 of the security review: a refreshed ID token that does not verify
// refuses the refresh — a signature of no published key, another audience,
// another authorized party — unless the issuer's keys cannot be fetched, which
// is the issuer's trouble; a rotation to a key the issuer publishes holds.
func TestRefreshedIDTokens(t *testing.T) {
	for name, c := range map[string]struct {
		knob func(*fakeissuer.Issuer)
		want string
	}{
		"a key the issuer rotated to":       {func(is *fakeissuer.Issuer) { is.RotatedKey = true }, "ok"},
		"a key the issuer does not publish": {func(is *fakeissuer.Issuer) { is.WrongKey = true }, "refused"},
		"another audience":                  {func(is *fakeissuer.Issuer) { is.Audience = "another-client" }, "refused"},
		"another authorized party":          {func(is *fakeissuer.Issuer) { is.AZP = "another-client" }, "refused"},
		"a rotated key and the keys down":   {func(is *fakeissuer.Issuer) { is.RotatedKey, is.KeysDown = true, true }, "served"},
		"a rotated key and oversized keys":  {func(is *fakeissuer.Issuer) { is.RotatedKey, is.PadKeys = true, 2<<20 }, "served"},
	} {
		t.Run(name, func(t *testing.T) {
			is := fakeissuer.Start(t)
			is.Add(bob())
			p := provider(t, is)
			id, err := login(t, is, p)
			require.NoError(t, err)
			is.Lock()
			is.IDTokenOnRefresh = true
			c.knob(is)
			is.Unlock()
			r, err := p.Refresh(context.Background(), id.RefreshToken, "bob-sub")
			switch c.want {
			case "ok":
				require.NoError(t, err)
				assert.True(t, r.GroupsKnown)
			case "refused":
				assert.ErrorIs(t, err, ErrRefreshRefused)
			default:
				require.Error(t, err)
				assert.NotErrorIs(t, err, ErrRefreshRefused)
			}
		})
	}
}

// m8 of the security review: a token for several audiences names cowork as
// its authorized party, and a token that names a party names cowork.
func TestAuthorizedParty(t *testing.T) {
	for name, c := range map[string]struct {
		extra, azp string
		ok         bool
	}{
		"one audience, no azp":           {"", "", true},
		"one audience, azp cowork":       {"", fakeissuer.ClientID, true},
		"one audience, azp another":      {"", "another", false},
		"several audiences, no azp":      {"another", "", false},
		"several audiences, azp cowork":  {"another", fakeissuer.ClientID, true},
		"several audiences, azp another": {"another", "another", false},
	} {
		t.Run(name, func(t *testing.T) {
			is := fakeissuer.Start(t)
			is.Add(bob())
			p := provider(t, is)
			is.Lock()
			is.ExtraAudience, is.AZP = c.extra, c.azp
			is.Unlock()
			_, err := login(t, is, p)
			if c.ok {
				assert.NoError(t, err)
			} else {
				assert.ErrorIs(t, err, ErrLogin)
			}
		})
	}
}

// m7 of the security review: discovery refuses what it cannot trust — an
// answer that redirects, one larger than 1 MiB, another issuer, an endpoint
// that is neither https nor http on a loopback host — and drops an
// end_session_endpoint that fails the rule; an answer beyond 1 MiB fails any
// call.
func TestDiscoveryHoldsTheIssuerToItsRules(t *testing.T) {
	for name, c := range map[string]struct {
		knob func(*fakeissuer.Issuer)
		want string
	}{
		"a redirect":           {func(is *fakeissuer.Issuer) { is.DiscoveryStatus = http.StatusFound }, "answered 302"},
		"an answer over 1 MiB": {func(is *fakeissuer.Issuer) { is.PadDiscovery = 2 << 20 }, "larger than 1 MiB"},
		"another issuer":       {func(is *fakeissuer.Issuer) { is.Discovery = map[string]any{"issuer": "https://other.example.com"} }, "another issuer"},
		"a token endpoint over http": {func(is *fakeissuer.Issuer) {
			is.Discovery = map[string]any{"token_endpoint": "http://login.example.com/token"}
		}, "token_endpoint"},
		"keys over http": {func(is *fakeissuer.Issuer) {
			is.Discovery = map[string]any{"jwks_uri": "http://login.example.com/keys"}
		}, "jwks_uri"},
		"UserInfo over http": {func(is *fakeissuer.Issuer) {
			is.Discovery = map[string]any{"userinfo_endpoint": "http://login.example.com/ui"}
		}, "userinfo_endpoint"},
		"the authorization over http": {func(is *fakeissuer.Issuer) {
			is.Discovery = map[string]any{"authorization_endpoint": "http://login.example.com/auth"}
		}, "authorization_endpoint"},
		"no algorithm cowork verifies": {func(is *fakeissuer.Issuer) {
			is.Discovery = map[string]any{"id_token_signing_alg_values_supported": []string{"HS256"}}
		}, "no algorithm"},
	} {
		t.Run(name, func(t *testing.T) {
			is := fakeissuer.Start(t)
			is.Lock()
			c.knob(is)
			is.Unlock()
			_, err := Discover(context.Background(), Config{Issuer: is.URL, ClientID: "c", ClientSecret: "never-echoed"})
			require.Error(t, err)
			assert.Contains(t, err.Error(), c.want)
			assert.Contains(t, err.Error(), is.URL)
			assert.NotContains(t, err.Error(), "never-echoed")
		})
	}

	is := fakeissuer.Start(t)
	is.Lock()
	is.Discovery = map[string]any{"end_session_endpoint": "http://login.example.com/logout"}
	is.Unlock()
	p, err := Discover(context.Background(), Config{Issuer: is.URL, ClientID: "c", ClientSecret: "s"})
	require.NoError(t, err, "an end_session_endpoint that fails the rule is dropped, not a refusal")
	assert.Empty(t, p.EndSessionURL("https://cowork.test/login"))

	is = fakeissuer.Start(t)
	is.Add(bob())
	p = provider(t, is)
	is.Lock()
	is.RotatedKey, is.PadKeys = true, 2<<20
	is.Unlock()
	_, err = login(t, is, p)
	assert.ErrorIs(t, err, ErrLogin, "keys over 1 MiB are no keys")
	assert.Contains(t, err.Error(), "larger than 1 MiB")
}

// docs/adr/0031 D4: the issuer's logout for the browser, when its discovery
// names one, with the client and the way back and without an ID token.
func TestEndSessionURL(t *testing.T) {
	is := fakeissuer.Start(t)
	assert.Empty(t, provider(t, is).EndSessionURL("https://cowork.test/login"), "Dex names none")

	is.Lock()
	is.EndSession = true
	is.Unlock()
	u, err := url.Parse(provider(t, is).EndSessionURL("https://cowork.test/login"))
	require.NoError(t, err)
	assert.Equal(t, is.URL+"/logout", u.Scheme+"://"+u.Host+u.Path)
	assert.Equal(t, fakeissuer.ClientID, u.Query().Get("client_id"))
	assert.Equal(t, "https://cowork.test/login", u.Query().Get("post_logout_redirect_uri"))
	assert.Empty(t, u.Query().Get("id_token_hint"))
}

// docs/adr/0029 D4: an issuer that cannot be discovered refuses the start; the
// error names the issuer.
func TestDiscoveryFailure(t *testing.T) {
	is := fakeissuer.Start(t)
	is.Stop()
	_, err := Discover(context.Background(), Config{Issuer: is.URL, ClientID: "c", ClientSecret: "never-echoed"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), is.URL)
	assert.NotContains(t, err.Error(), "never-echoed")
}

// m7: the end_session_endpoint discovery drops is said in the log the start
// writes, not lost.
func TestADroppedEndSessionIsLogged(t *testing.T) {
	is := fakeissuer.Start(t)
	is.Lock()
	is.Discovery = map[string]any{"end_session_endpoint": "http://login.example.com/logout"}
	is.Unlock()
	var log strings.Builder
	_, err := Discover(context.Background(), Config{Issuer: is.URL, ClientID: "c", ClientSecret: "s",
		Logger: slog.New(slog.NewTextHandler(&log, nil))})
	require.NoError(t, err)
	assert.Contains(t, log.String(), "end_session_endpoint is dropped")
	assert.NotContains(t, log.String(), "login.example.com", "the log names the issuer, not the endpoint")
}
