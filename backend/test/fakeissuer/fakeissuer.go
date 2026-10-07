// Package fakeissuer is an OpenID Connect issuer in the test's process: the
// discovery, the keys, an authorization endpoint that logs the next person in
// without a form, the token endpoint with the code and the refresh grant, and
// UserInfo. It does what a real issuer does, and on request what Dex cannot be
// made to do: sign with a wrong key, name another audience, issue an expired or
// a wrongly bound token, keep the groups in UserInfo only, change them at a
// refresh, refuse or fail a refresh, answer prompt=none with login_required
// (docs/adr/0029 D3, D6, the tests' issuer). It is never part of the binary.
package fakeissuer

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/coreos/go-oidc/v3/oidc/oidctest"
	jose "github.com/go-jose/go-jose/v4"
)

// The client the issuer knows.
const (
	ClientID     = "cowork"
	ClientSecret = "fake-issuer-secret" // #nosec G101 -- a test issuer's client secret, which protects nothing
)

// The ids of the issuer's two published keys.
const (
	keyID  = "fake-key"
	keyID2 = "fake-key-2"
)

// User is a person of the issuer. Groups is what the ID token's groups claim
// holds — a []string, a string, or nil to leave the claim out — and
// UserInfoGroups what UserInfo's holds, the same by default.
type User struct {
	Subject        string
	Email          string
	EmailVerified  any
	Name           string
	Groups         any
	UserInfoGroups any
	// GroupsInUserInfoOnly leaves the claim out of the ID token.
	GroupsInUserInfoOnly bool
}

// Issuer is the running issuer. Its exported fields are knobs a test turns
// between requests; Lock and Unlock guard them while the server runs.
type Issuer struct {
	URL string
	// Next is the subject the authorization endpoint logs in.
	Next string
	// Audience overrides the ID token's aud; WrongKey signs with a key the
	// issuer does not publish; Expired issues ID tokens that expired an hour
	// ago; Nonce overrides the nonce the ID token carries.
	Audience string
	WrongKey bool
	Expired  bool
	Nonce    string
	// NoRefreshToken issues no refresh token; RefreshError answers a refresh
	// grant with that OAuth error code and 400; RefreshStatus answers it with
	// that status, and the OAuth error when RefreshError is set too, else none;
	// CloseOnRefresh drops the connection.
	NoRefreshToken bool
	RefreshError   string
	RefreshStatus  int
	CloseOnRefresh bool
	// IDTokenOnRefresh sends a refreshed ID token with the refresh grant's
	// answer, as Dex does.
	IDTokenOnRefresh bool
	// EndSession names an end_session_endpoint in the discovery.
	EndSession bool
	// NoSession is an issuer that holds no session of the person: it answers
	// an authorization request with prompt=none with the error
	// login_required (OIDC Core 1.0 3.1.2.6), and logs the next person in at
	// any other, as its form would. Prompts records the prompt of every
	// authorization request, "" for none.
	NoSession bool
	Prompts   []string
	// RotatedKey signs with a second key, which the keys then publish beside
	// the first under another key id; KeysDown answers the keys with 503.
	RotatedKey bool
	KeysDown   bool
	// ExtraAudience adds an audience to the ID token's; AZP sets its
	// authorized party.
	ExtraAudience string
	AZP           string
	// Discovery overrides fields of the discovery document; DiscoveryStatus,
	// when set, answers it with that status and a Location elsewhere;
	// PadDiscovery and PadKeys add a field of that many bytes to the
	// discovery's or the keys' document.
	Discovery       map[string]any
	DiscoveryStatus int
	PadDiscovery    int
	PadKeys         int

	mu      sync.Mutex
	srv     *httptest.Server
	key     *rsa.PrivateKey
	key2    *rsa.PrivateKey
	rogue   *rsa.PrivateKey
	hang    chan struct{}
	users   map[string]*User
	codes   map[string]grant
	refresh map[string]string
	access  map[string]string
	// Refreshes counts the refresh grants asked for.
	Refreshes int
	issued    []string
}

// Issued is every secret the issuer handed out: codes, access, refresh and ID
// tokens, for a test that looks for them where they must not be.
func (is *Issuer) Issued() []string {
	is.mu.Lock()
	defer is.mu.Unlock()
	return append([]string{}, is.issued...)
}

type grant struct {
	subject, nonce, challenge, redirect, scope string
}

// Start runs an issuer for the test's lifetime.
func Start(t testing.TB) *Issuer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	rogue, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	key2, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	is := &Issuer{key: key, key2: key2, rogue: rogue, users: map[string]*User{}, codes: map[string]grant{},
		refresh: map[string]string{}, access: map[string]string{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", is.discovery)
	mux.HandleFunc("GET /keys", is.keys)
	mux.HandleFunc("GET /auth", is.authorize)
	mux.HandleFunc("POST /token", is.token)
	mux.HandleFunc("GET /userinfo", is.userinfo)
	is.srv = httptest.NewServer(mux)
	is.URL = is.srv.URL
	t.Cleanup(func() {
		is.Release()
		is.srv.Close()
	})
	return is
}

// Hang makes every refresh grant wait until Release, or until its client gives
// up: an issuer that stops answering.
func (is *Issuer) Hang() {
	is.mu.Lock()
	defer is.mu.Unlock()
	if is.hang == nil {
		is.hang = make(chan struct{})
	}
}

// Release lets the refresh grants Hang holds answer.
func (is *Issuer) Release() {
	is.mu.Lock()
	defer is.mu.Unlock()
	if is.hang != nil {
		close(is.hang)
		is.hang = nil
	}
}

// Lock guards the knobs while the server runs.
func (is *Issuer) Lock() { is.mu.Lock() }

// Unlock releases them.
func (is *Issuer) Unlock() { is.mu.Unlock() }

// Add makes a person of the issuer and the next one to log in.
func (is *Issuer) Add(u User) *User {
	is.mu.Lock()
	defer is.mu.Unlock()
	if u.UserInfoGroups == nil {
		u.UserInfoGroups = u.Groups
	}
	is.users[u.Subject] = &u
	is.Next = u.Subject
	return &u
}

// SetGroups changes a person's groups, in the ID token and in UserInfo.
func (is *Issuer) SetGroups(subject string, groups any) {
	is.mu.Lock()
	defer is.mu.Unlock()
	u := is.users[subject]
	u.Groups, u.UserInfoGroups = groups, groups
}

// SetEmail changes a person's address.
func (is *Issuer) SetEmail(subject, email string) {
	is.mu.Lock()
	defer is.mu.Unlock()
	is.users[subject].Email = email
}

// Stop takes the issuer down: every call to it fails as unreachable.
func (is *Issuer) Stop() { is.srv.Close() }

func (is *Issuer) discovery(w http.ResponseWriter, _ *http.Request) {
	is.mu.Lock()
	defer is.mu.Unlock()
	if is.DiscoveryStatus != 0 {
		w.Header().Set("Location", "http://127.0.0.1:1/elsewhere")
		w.WriteHeader(is.DiscoveryStatus)
		return
	}
	doc := map[string]any{
		"issuer":                                is.URL,
		"authorization_endpoint":                is.URL + "/auth",
		"token_endpoint":                        is.URL + "/token",
		"jwks_uri":                              is.URL + "/keys",
		"userinfo_endpoint":                     is.URL + "/userinfo",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"code_challenge_methods_supported":      []string{"S256"},
	}
	if is.EndSession {
		doc["end_session_endpoint"] = is.URL + "/logout"
	}
	for k, v := range is.Discovery {
		doc[k] = v
	}
	writePadded(w, doc, is.PadDiscovery)
}

func (is *Issuer) keys(w http.ResponseWriter, _ *http.Request) {
	is.mu.Lock()
	defer is.mu.Unlock()
	if is.KeysDown {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	keys := []jose.JSONWebKey{{Key: is.key.Public(), KeyID: keyID, Algorithm: gooidc.RS256, Use: "sig"}}
	if is.RotatedKey {
		keys = append(keys, jose.JSONWebKey{Key: is.key2.Public(), KeyID: keyID2, Algorithm: gooidc.RS256, Use: "sig"})
	}
	writePadded(w, map[string]any{"keys": keys}, is.PadKeys)
}

// writePadded writes the document as JSON, with a field of pad bytes in it
// when pad is set: a reader has to read them to read the document.
func writePadded(w http.ResponseWriter, doc map[string]any, pad int) {
	if pad > 0 {
		doc["padding"] = strings.Repeat("x", pad)
	}
	writeJSON(w, http.StatusOK, doc)
}

// authorize logs the next person in at once and sends the browser back with a
// code, the way an issuer does after its form — or, under NoSession, answers
// prompt=none with login_required.
func (is *Issuer) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("client_id") != ClientID || q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" {
		http.Error(w, "bad authorization request", http.StatusBadRequest)
		return
	}
	back, err := url.Parse(q.Get("redirect_uri"))
	if err != nil {
		http.Error(w, "bad redirect_uri", http.StatusBadRequest)
		return
	}
	v := back.Query()
	is.mu.Lock()
	is.Prompts = append(is.Prompts, q.Get("prompt"))
	if is.NoSession && q.Get("prompt") == "none" {
		v.Set("error", "login_required")
	} else {
		code := random()
		is.codes[code] = grant{subject: is.Next, nonce: q.Get("nonce"), challenge: q.Get("code_challenge"),
			redirect: q.Get("redirect_uri"), scope: q.Get("scope")}
		is.issued = append(is.issued, code)
		v.Set("code", code)
	}
	is.mu.Unlock()
	v.Set("state", q.Get("state"))
	back.RawQuery = v.Encode()
	// #nosec G710 -- an issuer sends the browser back to the client's redirect URI; this one serves tests only
	http.Redirect(w, r, back.String(), http.StatusFound)
}

func (is *Issuer) token(w http.ResponseWriter, r *http.Request) {
	id, secret, ok := r.BasicAuth()
	if !ok {
		id, secret = r.PostFormValue("client_id"), r.PostFormValue("client_secret")
	}
	if id != ClientID || secret != ClientSecret {
		oauthError(w, http.StatusUnauthorized, "invalid_client")
		return
	}
	switch r.PostFormValue("grant_type") {
	case "authorization_code":
		is.codeGrant(w, r)
	case "refresh_token":
		is.refreshGrant(w, r)
	default:
		oauthError(w, http.StatusBadRequest, "unsupported_grant_type")
	}
}

func (is *Issuer) codeGrant(w http.ResponseWriter, r *http.Request) {
	is.mu.Lock()
	defer is.mu.Unlock()
	code := r.PostFormValue("code")
	g, ok := is.codes[code]
	delete(is.codes, code)
	sum := sha256.Sum256([]byte(r.PostFormValue("code_verifier")))
	switch {
	case !ok, g.redirect != r.PostFormValue("redirect_uri"):
		oauthError(w, http.StatusBadRequest, "invalid_grant")
		return
	case base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge:
		oauthError(w, http.StatusBadRequest, "invalid_grant")
		return
	}
	nonce := g.nonce
	if is.Nonce != "" {
		nonce = is.Nonce
	}
	is.answer(w, g.subject, nonce, !is.NoRefreshToken, true)
}

func (is *Issuer) refreshGrant(w http.ResponseWriter, r *http.Request) {
	is.mu.Lock()
	hang := is.hang
	is.Refreshes++
	is.mu.Unlock()
	if hang != nil {
		select {
		case <-hang:
		case <-r.Context().Done():
			return
		}
	}
	is.mu.Lock()
	defer is.mu.Unlock()
	switch {
	case is.CloseOnRefresh:
		if hj, ok := w.(http.Hijacker); ok {
			if conn, _, err := hj.Hijack(); err == nil {
				_ = conn.Close()
				return
			}
		}
		w.WriteHeader(http.StatusBadGateway)
		return
	case is.RefreshStatus != 0 && is.RefreshError != "":
		oauthError(w, is.RefreshStatus, is.RefreshError)
		return
	case is.RefreshStatus != 0:
		w.WriteHeader(is.RefreshStatus)
		return
	case is.RefreshError != "":
		oauthError(w, http.StatusBadRequest, is.RefreshError)
		return
	}
	presented := r.PostFormValue("refresh_token")
	subject, ok := is.refresh[presented]
	if !ok {
		// Dex's answer to a spent or unknown refresh token.
		oauthError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	delete(is.refresh, presented)
	is.answer(w, subject, "", true, is.IDTokenOnRefresh)
}

// answer writes a token answer for the subject; the caller holds the lock.
func (is *Issuer) answer(w http.ResponseWriter, subject, nonce string, refresh, idToken bool) {
	u, ok := is.users[subject]
	if !ok {
		oauthError(w, http.StatusBadRequest, "invalid_grant")
		return
	}
	access := random()
	is.access[access] = subject
	is.issued = append(is.issued, access)
	out := map[string]any{"access_token": access, "token_type": "Bearer", "expires_in": 300}
	if refresh {
		rt := random()
		is.refresh[rt] = subject
		is.issued = append(is.issued, rt)
		out["refresh_token"] = rt
	}
	if idToken {
		signed := is.sign(u, nonce)
		is.issued = append(is.issued, signed)
		out["id_token"] = signed
	}
	writeJSON(w, http.StatusOK, out)
}

func (is *Issuer) sign(u *User, nonce string) string {
	now := time.Now()
	exp := now.Add(time.Hour)
	if is.Expired {
		exp = now.Add(-time.Hour)
	}
	var aud any = ClientID
	if is.Audience != "" {
		aud = is.Audience
	}
	if is.ExtraAudience != "" {
		aud = []string{aud.(string), is.ExtraAudience}
	}
	claims := map[string]any{"iss": is.URL, "sub": u.Subject, "aud": aud, "iat": now.Unix(), "exp": exp.Unix()}
	if nonce != "" {
		claims["nonce"] = nonce
	}
	if is.AZP != "" {
		claims["azp"] = is.AZP
	}
	putClaims(claims, u)
	if u.Groups != nil && !u.GroupsInUserInfoOnly {
		claims["groups"] = u.Groups
	}
	raw, err := json.Marshal(claims)
	if err != nil {
		panic(err)
	}
	key, kid := is.key, keyID
	switch {
	case is.WrongKey:
		key = is.rogue
	case is.RotatedKey:
		key, kid = is.key2, keyID2
	}
	return oidctest.SignIDToken(key, kid, gooidc.RS256, string(raw))
}

func putClaims(claims map[string]any, u *User) {
	if u.Email != "" {
		claims["email"] = u.Email
	}
	if u.EmailVerified != nil {
		claims["email_verified"] = u.EmailVerified
	}
	if u.Name != "" {
		claims["name"] = u.Name
	}
}

func (is *Issuer) userinfo(w http.ResponseWriter, r *http.Request) {
	is.mu.Lock()
	defer is.mu.Unlock()
	token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	subject, ok := is.access[token]
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	u := is.users[subject]
	claims := map[string]any{"sub": u.Subject}
	putClaims(claims, u)
	if u.UserInfoGroups != nil {
		claims["groups"] = u.UserInfoGroups
	}
	writeJSON(w, http.StatusOK, claims)
}

// Code makes a code for the next person, as the authorization endpoint would,
// for a test that drives the token endpoint without a browser.
func (is *Issuer) Code(nonce, verifier, redirect string) string {
	is.mu.Lock()
	defer is.mu.Unlock()
	sum := sha256.Sum256([]byte(verifier))
	code := random()
	is.codes[code] = grant{subject: is.Next, nonce: nonce, challenge: base64.RawURLEncoding.EncodeToString(sum[:]), redirect: redirect}
	return code
}

func oauthError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code, "error_description": fmt.Sprintf("the fake issuer says %s", code)})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func random() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
