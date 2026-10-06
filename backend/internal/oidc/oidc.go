// Package oidc is cowork as a standard OpenID Connect relying party
// (docs/adr/0029): discovery at start, the authorization code flow with PKCE,
// the ID token verified against the issuer's keys, the groups read from a
// configurable claim — the ID token's, else UserInfo's — the refresh grant
// that reads them again (docs/adr/0030 D5), and the issuer's logout. It assumes
// no provider: no endpoint, parameter or claim beyond the standard ones.
//
// Every call to the issuer goes through one client that follows no redirect
// and reads at most 1 MiB of an answer, and no error of this package carries
// an answer's body (the security review of 2026-10-04, m7).
package oidc

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// Config is the relying party's configuration (docs/adr/0029 D4).
type Config struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	// RedirectURL is COWORK_BASE_URL + /auth/callback.
	RedirectURL string
	Scopes      []string
	// GroupsClaim names the claim that carries the groups (D2).
	GroupsClaim string
	// Now is the clock an ID token's expiry is checked against; nil means
	// time.Now.
	Now func() time.Time
	// Logger receives what the discovery drops; nil discards it.
	Logger *slog.Logger
}

// Provider is a discovered issuer.
type Provider struct {
	cfg        Config
	provider   *gooidc.Provider
	verifier   *gooidc.IDTokenVerifier
	oauth      oauth2.Config
	client     *http.Client
	userInfo   bool
	endSession string
}

// discovery is what cowork reads of the issuer's discovery document.
type discovery struct {
	Issuer     string   `json:"issuer"`
	Auth       string   `json:"authorization_endpoint"`
	Token      string   `json:"token_endpoint"`
	UserInfo   string   `json:"userinfo_endpoint"`
	JWKS       string   `json:"jwks_uri"`
	EndSession string   `json:"end_session_endpoint"`
	Algorithms []string `json:"id_token_signing_alg_values_supported"`
}

// Discover fetches the issuer's discovery document and keeps what the login
// needs (D1). A configured issuer that cannot be discovered refuses the start
// (D4): no answer, an answer that is not 200 and JSON, another issuer than the
// configured one, an authorization, token, keys or UserInfo endpoint that is
// neither https nor http on a loopback host, no signature algorithm cowork
// verifies. An end_session_endpoint that fails the rule is dropped, and the
// logout then ends no session at the issuer. The error names the issuer, never
// the secret or an answer.
func Discover(ctx context.Context, cfg Config) (*Provider, error) {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.DiscardHandler)
	}
	client := newClient()
	var d discovery
	err := getJSON(ctx, client, strings.TrimSuffix(cfg.Issuer, "/")+"/.well-known/openid-configuration",
		func(r io.Reader) error { return json.NewDecoder(r).Decode(&d) })
	if err != nil {
		return nil, fmt.Errorf("discover the issuer %s: %w", cfg.Issuer, err)
	}
	if err := d.check(cfg); err != nil {
		return nil, fmt.Errorf("discover the issuer %s: %w", cfg.Issuer, err)
	}
	algs, names := acceptedAlgs(d.Algorithms)
	if len(algs) == 0 {
		return nil, fmt.Errorf("discover the issuer %s: it signs ID tokens with no algorithm cowork verifies", cfg.Issuer)
	}
	ctx = gooidc.ClientContext(ctx, client)
	provider := (&gooidc.ProviderConfig{IssuerURL: d.Issuer, AuthURL: d.Auth, TokenURL: d.Token, UserInfoURL: d.UserInfo,
		JWKSURL: d.JWKS, Algorithms: names}).NewProvider(ctx)
	keys := &keySet{client: client, url: d.JWKS, algs: algs}
	return &Provider{
		cfg:      cfg,
		provider: provider,
		verifier: gooidc.NewVerifier(d.Issuer, keys, &gooidc.Config{ClientID: cfg.ClientID, Now: cfg.Now, SupportedSigningAlgs: names}),
		oauth: oauth2.Config{
			ClientID: cfg.ClientID, ClientSecret: cfg.ClientSecret, RedirectURL: cfg.RedirectURL,
			Endpoint: oauth2.Endpoint{AuthURL: d.Auth, TokenURL: d.Token}, Scopes: cfg.Scopes,
		},
		client:     client,
		userInfo:   d.UserInfo != "",
		endSession: d.EndSession,
	}, nil
}

// check holds the document to the configured issuer and its endpoints to the
// issuer's rule; it drops an end_session_endpoint that fails it.
func (d *discovery) check(cfg Config) error {
	if d.Issuer != cfg.Issuer {
		return errors.New("its discovery names another issuer than the configured one")
	}
	for _, e := range []struct{ name, url string }{
		{"authorization_endpoint", d.Auth}, {"token_endpoint", d.Token}, {"jwks_uri", d.JWKS},
	} {
		if err := checkEndpoint(e.url); err != nil {
			return fmt.Errorf("its %s %w", e.name, err)
		}
	}
	if d.UserInfo != "" {
		if err := checkEndpoint(d.UserInfo); err != nil {
			return fmt.Errorf("its userinfo_endpoint %w", err)
		}
	}
	if d.EndSession != "" {
		if err := checkEndpoint(d.EndSession); err != nil {
			cfg.Logger.Warn("the issuer's end_session_endpoint is dropped: a logout ends no session at the issuer",
				"issuer", cfg.Issuer, "reason", err.Error())
			d.EndSession = ""
		}
	}
	return nil
}

// Issuer is the issuer's identifier, the first half of a person's identity
// (D5).
func (p *Provider) Issuer() string { return p.cfg.Issuer }

// AuthCodeURL is where a login sends the browser: the authorization code flow
// with the state, the nonce and the PKCE challenge of the verifier (D1). A
// silent login adds prompt=none (OIDC Core 1.0 3.1.2.1, D6): the issuer is to
// answer without a page of its own — with a code while it holds a session of
// the person, else with an error such as login_required. An issuer that
// ignores the parameter, as Dex does, shows its form instead.
func (p *Provider) AuthCodeURL(state, nonce, verifier string, silent bool) string {
	opts := []oauth2.AuthCodeOption{gooidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)}
	if silent {
		opts = append(opts, oauth2.SetAuthURLParam("prompt", "none"))
	}
	return p.oauth.AuthCodeURL(state, opts...)
}

// EndSessionURL is the issuer's logout for the browser to go to, with the
// client and where to come back to, or "" when the discovery names no
// end_session_endpoint (docs/adr/0031 D4). No ID token is kept (D1), so it
// carries no id_token_hint.
func (p *Provider) EndSessionURL(postLogoutRedirect string) string {
	if p.endSession == "" {
		return ""
	}
	u, err := url.Parse(p.endSession)
	if err != nil {
		return ""
	}
	q := u.Query()
	q.Set("client_id", p.cfg.ClientID)
	q.Set("post_logout_redirect_uri", postLogoutRedirect)
	u.RawQuery = q.Encode()
	return u.String()
}

// Identity is what a verified login says of the person.
type Identity struct {
	Subject string
	// Name is the display name: the name claim, else preferred_username, else
	// the address, else the subject (D5).
	Name string
	// Email is "" when the issuer named none; EmailVerified is nil when it
	// said nothing about it.
	Email         string
	EmailVerified *bool
	Groups        []string
	// RefreshToken is "" when the issuer gave none.
	RefreshToken string
}

// ErrLogin is what every failure of a login wraps: the browser's login page
// says oidc_failed, and the log says which.
var ErrLogin = errors.New("the login through the identity provider failed")

// Exchange redeems the code with the PKCE verifier and verifies the ID token:
// its signature against the issuer's keys, which rotate, its issuer, audience,
// authorized party and expiry, and its nonce (D1). The groups are read from the
// claim, in the ID token or — when it does not carry it — from UserInfo (D2).
func (p *Provider) Exchange(ctx context.Context, code, verifier, nonce string) (Identity, error) {
	ctx = p.context(ctx)
	tok, err := p.oauth.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return Identity{}, fmt.Errorf("%w: exchange the code: %w", ErrLogin, grantError(err))
	}
	raw, ok := tok.Extra("id_token").(string)
	if !ok || raw == "" {
		return Identity{}, fmt.Errorf("%w: the token answer holds no ID token", ErrLogin)
	}
	idToken, claims, err := p.verify(ctx, raw)
	if err != nil {
		return Identity{}, fmt.Errorf("%w: %w", ErrLogin, err)
	}
	if subtle.ConstantTimeCompare([]byte(idToken.Nonce), []byte(nonce)) != 1 || nonce == "" {
		return Identity{}, fmt.Errorf("%w: the ID token's nonce is not the login's", ErrLogin)
	}
	id := Identity{Subject: idToken.Subject, RefreshToken: tok.RefreshToken}
	id.Email, _ = claims["email"].(string)
	id.EmailVerified = verified(claims["email_verified"])
	id.Name = displayName(claims, id.Email, id.Subject)
	groups, known, err := Groups(claims, p.cfg.GroupsClaim)
	if err != nil {
		return Identity{}, fmt.Errorf("%w: %w", ErrLogin, err)
	}
	if !known {
		groups, _, err = p.userInfoGroups(ctx, tok, idToken.Subject)
		if err != nil {
			return Identity{}, fmt.Errorf("%w: %w", ErrLogin, err)
		}
	}
	id.Groups = groups
	return id, nil
}

// verify checks an ID token (D1) — go-oidc's verifier with cowork's keys: the
// signature, the issuer, the audience, the expiry — and its authorized party
// (m8 of the security review): a token for several audiences must name cowork
// as the party it was issued to, and a token that names a party must name
// cowork. The error never carries the issuer's answer.
func (p *Provider) verify(ctx context.Context, raw string) (*gooidc.IDToken, map[string]any, error) {
	idToken, err := p.verifier.Verify(ctx, raw)
	if err != nil {
		return nil, nil, fmt.Errorf("the ID token does not verify: %s", shortError(err))
	}
	var claims map[string]any
	if err := idToken.Claims(&claims); err != nil {
		return nil, nil, errors.New("the ID token's claims do not read")
	}
	azp, named := claims["azp"]
	switch {
	case named && azp != p.cfg.ClientID:
		return nil, nil, errors.New("the ID token was issued to another client (azp)")
	case !named && len(idToken.Audience) > 1:
		return nil, nil, errors.New("the ID token names several audiences and no authorized party (azp)")
	}
	return idToken, claims, nil
}

// Refreshed is what a refresh grant found.
type Refreshed struct {
	// RefreshToken is the one to use next time: the issuer's rotated token,
	// or the one presented when the issuer kept it.
	RefreshToken string
	// Groups are the person's groups; GroupsKnown is false when neither the
	// refreshed ID token nor UserInfo carried the claim, and the groups are
	// then nobody's word.
	Groups      []string
	GroupsKnown bool
}

// ErrRefreshRefused says the issuer refused the refresh token, or answered the
// refresh with a token that does not hold: an OAuth error answer of its token
// endpoint — a 4xx with an error field, invalid_grant and Dex's invalid_request
// among them — that is not one of the answers below, a refreshed ID token whose
// signature no key verifies or that names another subject, client or nonce
// than the session's, UserInfo about another subject. The session ends.
var ErrRefreshRefused = errors.New("the issuer refused the refresh")

// ErrClientRejected says the issuer refuses cowork's client — invalid_client,
// unauthorized_client: the configuration's error, not the person's. The
// session is served and refreshed again later, and the log says it at error
// level.
var ErrClientRejected = errors.New("the issuer refuses cowork's client")

// notThePersons are the OAuth errors of a refresh that are a temporary state
// of the issuer or a mistake of cowork's configuration rather than anything
// about the person: the session is served, and refreshed again later (m2 of
// the security review).
var notThePersons = map[string]bool{
	"temporarily_unavailable": true, "slow_down": true, "server_error": true,
	"invalid_client": true, "unauthorized_client": true, "invalid_scope": true,
}

// grantError is the token endpoint's answer to a grant as cowork classes it,
// with its status and its OAuth error code and never its body.
func grantError(err error) error {
	var re *oauth2.RetrieveError
	if !errors.As(err, &re) || re.Response == nil {
		return transportError(err)
	}
	status, code := re.Response.StatusCode, safeCode(re.ErrorCode)
	answer := fmt.Sprintf("the token endpoint answered %d %s", status, code)
	switch {
	case re.ErrorCode == "invalid_client", re.ErrorCode == "unauthorized_client":
		return fmt.Errorf("%w: %s", ErrClientRejected, answer)
	case status == http.StatusTooManyRequests, status >= 500, status < 400, re.ErrorCode == "", notThePersons[re.ErrorCode]:
		return errors.New(answer)
	}
	return fmt.Errorf("%w: %s", ErrRefreshRefused, answer)
}

// Refresh reads the person's groups again with a refresh grant (docs/adr/0030
// D5): from the refreshed ID token when the issuer sends one and it carries
// the claim, else from UserInfo with the new access token. ErrRefreshRefused
// ends the session; every other error — no answer, a timeout, a 5xx, a 429, a
// temporary or a configuration error, keys that cannot be fetched — leaves it
// served. An error after the grant succeeded comes with the rotated refresh
// token, which replaces the spent one all the same.
func (p *Provider) Refresh(ctx context.Context, refreshToken, subject string) (Refreshed, error) {
	ctx = p.context(ctx)
	tok, err := p.oauth.TokenSource(ctx, &oauth2.Token{RefreshToken: refreshToken}).Token()
	if err != nil {
		return Refreshed{}, fmt.Errorf("refresh grant: %w", grantError(err))
	}
	out := Refreshed{RefreshToken: tok.RefreshToken}
	if out.RefreshToken == "" {
		out.RefreshToken = refreshToken
	}
	if raw, ok := tok.Extra("id_token").(string); ok && raw != "" {
		probe := &keysProbe{}
		idToken, claims, err := p.verify(withProbe(ctx, probe), raw)
		switch {
		case err != nil && probe.unavailable:
			return out, fmt.Errorf("the refreshed ID token cannot be verified now: %w", err)
		case err != nil:
			return out, fmt.Errorf("%w: %w", ErrRefreshRefused, err)
		case idToken.Subject != subject:
			return out, fmt.Errorf("%w: the refreshed ID token names another subject", ErrRefreshRefused)
		}
		if out.Groups, out.GroupsKnown, err = Groups(claims, p.cfg.GroupsClaim); err != nil {
			return out, fmt.Errorf("%w: %w", ErrRefreshRefused, err)
		}
		if out.GroupsKnown {
			return out, nil
		}
	}
	out.Groups, out.GroupsKnown, err = p.userInfoGroups(ctx, tok, subject)
	return out, err
}

// errUserInfoSubject is UserInfo answering about another subject than the
// ID token's (OIDC Core 5.3.2).
var errUserInfoSubject = errors.New("UserInfo names another subject than the ID token")

// userInfoGroups reads the groups claim from UserInfo; known is false when the
// issuer has no UserInfo endpoint or its answer does not carry the claim. The
// answer must be about the subject the ID token named; one about another is a
// refusal.
func (p *Provider) userInfoGroups(ctx context.Context, tok *oauth2.Token, subject string) ([]string, bool, error) {
	if !p.userInfo {
		return []string{}, false, nil
	}
	info, err := p.provider.UserInfo(ctx, oauth2.StaticTokenSource(tok))
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			return nil, false, fmt.Errorf("read UserInfo: %w", err)
		}
		// go-oidc's own error quotes the answer; it stays out of the log.
		return nil, false, errors.New("UserInfo answered an error or an unreadable document")
	}
	if info.Subject != subject {
		return nil, false, fmt.Errorf("%w: %w", ErrRefreshRefused, errUserInfoSubject)
	}
	var claims map[string]any
	if err := info.Claims(&claims); err != nil {
		return nil, false, errors.New("UserInfo's claims do not read")
	}
	return Groups(claims, p.cfg.GroupsClaim)
}

// context makes the calls of the oauth2 and the go-oidc packages use the
// provider's client.
func (p *Provider) context(ctx context.Context) context.Context {
	return gooidc.ClientContext(context.WithValue(ctx, oauth2.HTTPClient, p.client), p.client)
}

// Groups reads the groups claim (D2): a list of strings, or a single string
// that is one group. known is false when the claims do not carry it; empty
// names and repetitions are dropped. A claim of another shape is an error, not
// an empty list.
func Groups(claims map[string]any, claim string) (groups []string, known bool, err error) {
	v, ok := claims[claim]
	if !ok || v == nil {
		return []string{}, false, nil
	}
	groups = []string{}
	add := func(g string) {
		if g != "" && !slices.Contains(groups, g) {
			groups = append(groups, g)
		}
	}
	switch t := v.(type) {
	case string:
		add(t)
	case []any:
		for _, item := range t {
			s, ok := item.(string)
			if !ok {
				return nil, false, fmt.Errorf("the claim %s holds something that is not a group name", claim)
			}
			add(s)
		}
	default:
		return nil, false, fmt.Errorf("the claim %s is neither a list of group names nor one", claim)
	}
	return groups, true, nil
}

// verified reads email_verified, which some issuers send as a string; nil
// when it is absent or neither.
func verified(v any) *bool {
	switch t := v.(type) {
	case bool:
		return &t
	case string:
		if b, err := strconv.ParseBool(t); err == nil {
			return &b
		}
	}
	return nil
}

// maxDisplayName is the longest display name a person has.
const maxDisplayName = 200

// displayName is the name claim, else preferred_username, else the address,
// else the subject, at most two hundred characters (D5).
func displayName(claims map[string]any, email, subject string) string {
	for _, v := range []any{claims["name"], claims["preferred_username"], email, subject} {
		if s, ok := v.(string); ok {
			if s = strings.TrimSpace(s); s != "" {
				if r := []rune(s); len(r) > maxDisplayName {
					return string(r[:maxDisplayName])
				}
				return s
			}
		}
	}
	return "unnamed"
}
