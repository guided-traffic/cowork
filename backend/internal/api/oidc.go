package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/oauth2"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/oidc"
	"github.com/guided-traffic/cowork/backend/internal/requestid"
	"github.com/guided-traffic/cowork/backend/internal/store"
)

// oidcStateCookie carries a login's state through the issuer and back
// (docs/adr/0029 D1): HttpOnly, Secure, SameSite=Lax — the issuer's redirect
// back is a top-level navigation, which carries it — Path=/ and no Domain,
// under the __Host- prefix like the session's cookie.
const oidcStateCookie = "__Host-cowork-oidc"

// loginStateAge is how long a login may take at the issuer: the state cookie's
// Max-Age, and the oldest state the callback takes.
const loginStateAge = 10 * time.Minute

// maxReturnTo is the longest path a login returns to; the state cookie holds
// it and stays well below a browser's limit of a cookie.
const maxReturnTo = 2048

// The codes of the login page's error parameter (the API document, GET
// /auth/callback).
const (
	oidcUnavailable     = "oidc_unavailable"
	loginFailed         = "oidc_failed"
	loginNotAllowed     = "not_allowed"
	loginNotInitialised = "not_initialised"
)

// loginPage is where a failed login through the identity provider sends the
// browser: the login page, with the code that says why and — when the login
// knows one — the path the person wanted, which the page's next attempt
// returns to. The path is a validated return_to (safeReturnTo); "/" is the
// page's default and is left out.
func loginPage(code, returnTo string) string {
	page := "/login?error=" + code
	if returnTo != "" && returnTo != "/" {
		page += "&return=" + url.QueryEscape(returnTo)
	}
	return page
}

// loginState is what the state cookie holds: the state, the nonce and the
// PKCE verifier the callback checks the issuer's answer against, where the
// browser goes after the login, and when the login began.
type loginState struct {
	State    string `json:"s"`
	Nonce    string `json:"n"`
	Verifier string `json:"v"`
	ReturnTo string `json:"r"`
	At       int64  `json:"t"`
}

// LoginOidc starts a login through the identity provider (docs/adr/0029 D1):
// the browser goes to the issuer's authorization endpoint for the code flow
// with PKCE, a state and a nonce, which the state cookie keeps for the
// callback, sealed. Without a provider, or with a gate that admits nobody, it
// goes back to the login page (docs/adr/0030 D8).
func (s *Server) LoginOidc(_ context.Context, req apigen.LoginOidcRequestObject) (apigen.LoginOidcResponseObject, error) {
	h := s.h
	returnTo := safeReturnTo(deref(req.Params.ReturnTo))
	if !h.oidcOffered() {
		return apigen.LoginOidc303Response{Headers: apigen.LoginOidc303ResponseHeaders{Location: loginPage(oidcUnavailable, returnTo)}}, nil
	}
	st := loginState{State: randomValue(), Nonce: randomValue(), Verifier: oauth2.GenerateVerifier(),
		ReturnTo: returnTo, At: h.opts.Now().Unix()}
	raw, err := json.Marshal(st)
	if err != nil {
		return nil, err
	}
	cookie := stateCookie(base64.RawURLEncoding.EncodeToString(h.loginSealer.Seal(raw, []byte(oidcStateCookie))), loginStateAge)
	return apigen.LoginOidc302Response{Headers: apigen.LoginOidc302ResponseHeaders{
		Location:  h.opts.OIDC.Provider.AuthCodeURL(st.State, st.Nonce, st.Verifier),
		SetCookie: &cookie,
	}}, nil
}

// OidcCallback completes a login through the identity provider (docs/adr/0029
// D1, D5, docs/adr/0030 D1, D2, docs/adr/0031 D5, docs/adr/0032 D5): the
// state cookie must hold the state the issuer returns; the code is redeemed
// with the verifier and the ID token verified; the gate, a deactivated person
// and the init state may refuse; otherwise the person is found or made, their
// memberships derived and the session made. Every outcome clears the state
// cookie, and every failure sends the browser to the login page with the code
// that says why — the reason is in the log, never on the page, and no log line
// holds a code or a token.
func (s *Server) OidcCallback(ctx context.Context, req apigen.OidcCallbackRequestObject) (apigen.OidcCallbackResponseObject, error) {
	h := s.h
	c, now := clientFrom(ctx), h.opts.Now()
	// The state cookie opens — or not — before anything else, so that even a
	// failure sends the browser back with the path the person wanted.
	st, opened, fresh := h.openLoginState(c.OIDCState, now)
	fail := func(code, reason string, err error) (apigen.OidcCallbackResponseObject, error) {
		args := []any{"request_id", requestid.From(ctx), "code", code, "reason", reason}
		if err != nil {
			args = append(args, "error", err)
		}
		h.logger.Info("a login through the identity provider did not succeed", args...)
		return redirect{location: loginPage(code, st.ReturnTo), cookies: []string{clearedStateCookie()}}, nil
	}
	provider := h.opts.OIDC.Provider
	if provider == nil {
		return fail(oidcUnavailable, "no identity provider is configured", nil)
	}
	if reason := callbackRefusal(req.Params, st, opened && fresh); reason != "" {
		return fail(loginFailed, reason, nil)
	}
	id, err := provider.Exchange(ctx, *req.Params.Code, st.Verifier, st.Nonce)
	if err != nil {
		return fail(loginFailed, "the code or the ID token did not hold", err)
	}
	readAt := h.opts.Now()
	value, hash, err := auth.GenerateSession()
	if err != nil {
		return nil, err
	}
	expires := now.Add(h.opts.SessionLifetime)
	in := h.oidcLogin(ctx, provider.Issuer(), id, store.NewSession{Hash: hash, Now: now, Expires: expires,
		RequestID: requestid.UUID(ctx), SourceHash: h.sourceHash(c.Client)})
	in.ReadAt = readAt
	res, err := s.db.CompleteOIDCLogin(ctx, in)
	if err != nil {
		h.logger.Error("a login through the identity provider failed", "request_id", requestid.From(ctx), "error", err)
		return fail(loginFailed, "the login could not be stored", nil)
	}
	switch res.Outcome {
	case store.OIDCNotAllowed:
		return fail(loginNotAllowed, res.Reason, nil)
	case store.OIDCNotInitialised:
		return fail(loginNotInitialised, res.Reason, nil)
	}
	return redirect{location: st.ReturnTo, cookies: []string{sessionCookie(value, expires.Sub(now)), clearedStateCookie()}}, nil
}

// callbackRefusal says why the issuer's return cannot be a login before it is
// asked anything — the state cookie, the issuer's error, the state, the code —
// or "" when it can.
func callbackRefusal(p apigen.OidcCallbackParams, st loginState, opened bool) string {
	switch {
	case !opened:
		return "the state cookie is missing, stale or does not open"
	case p.Error != nil:
		return "the issuer answered with the error " + shorten(*p.Error, 64)
	case p.State == nil || subtle.ConstantTimeCompare([]byte(*p.State), []byte(st.State)) != 1:
		return "the state is not the one of the login the browser began"
	case p.Code == nil || *p.Code == "":
		return "the issuer sent no code"
	}
	return ""
}

// oidcLogin is what the store needs of a verified login: the person as the
// issuer says them, what the gate makes of their groups, and the session to
// make — replacing the one the request presented (docs/adr/0031 D5) — with the
// refresh token sealed for it.
func (h *handler) oidcLogin(ctx context.Context, issuer string, id oidc.Identity, session store.NewSession) store.OIDCLogin {
	c := clientFrom(ctx)
	admitted, admin := h.gate(&issuer, id.Groups)
	in := store.OIDCLogin{
		Issuer: issuer, Subject: id.Subject, DisplayName: id.Name, Email: id.Email, EmailVerified: id.EmailVerified,
		Groups: id.Groups, Admitted: admitted, Admin: admin, Session: session,
	}
	if c.UserAgent != "" {
		ua := sha256.Sum256([]byte(c.UserAgent))
		in.Session.UserAgentHash = ua[:]
	}
	if auth.WellFormedSession(c.Cookie) {
		old := auth.HashSession(c.Cookie)
		in.Session.Replaces = old[:]
	}
	if id.RefreshToken != "" {
		in.RefreshTokenSealed = h.refreshSealer.Seal([]byte(id.RefreshToken), session.Hash[:])
	} else {
		h.noRefreshToken.Do(func() {
			h.logger.Warn("the issuer gave no refresh token: a session keeps the groups of its login until it ends; request the scope offline_access (COWORK_OIDC_SCOPES)")
		})
	}
	return in
}

// openLoginState opens the state cookie: opened says it opens with the server
// key, fresh that the login began within loginStateAge by the backend's clock —
// a minute ahead passes for another replica's clock. A stale state is no
// login, but its return_to, which the start validated and sealed, is still the
// path the person wanted. A state that does not open yields nothing.
func (h *handler) openLoginState(value string, now time.Time) (st loginState, opened, fresh bool) {
	if value == "" || len(value) > 8192 {
		return loginState{}, false, false
	}
	sealed, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return loginState{}, false, false
	}
	raw, err := h.loginSealer.Open(sealed, []byte(oidcStateCookie))
	if err != nil || json.Unmarshal(raw, &st) != nil || st.State == "" || st.Verifier == "" {
		return loginState{}, false, false
	}
	st.ReturnTo = safeReturnTo(st.ReturnTo)
	age := now.Sub(time.Unix(st.At, 0))
	return st, true, age > -time.Minute && age <= loginStateAge
}

// safeReturnTo keeps a return_to that is a path of this installation: it
// starts with one slash — not two, nor a slash and a backslash, which a
// browser reads as another host — holds no control character, which a browser
// drops before it reads the URL, and no backslash, and is at most maxReturnTo
// long. Anything else is "/": a bad link still leads to a login.
func safeReturnTo(v string) string {
	if v == "" || len(v) > maxReturnTo || v[0] != '/' || strings.HasPrefix(v, "//") || !utf8.ValidString(v) {
		return "/"
	}
	for _, r := range v {
		if r < 0x20 || r == 0x7f || r == '\\' {
			return "/"
		}
	}
	return v
}

// shorten cuts a value a log line quotes from the request, so a long one does
// not fill the log.
func shorten(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// randomValue is 256 random bits in base64url, a state or a nonce.
func randomValue() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand does not fail
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func stateCookie(value string, maxAge time.Duration) string {
	return (&http.Cookie{
		Name: oidcStateCookie, Value: value, Path: "/", MaxAge: int(maxAge.Seconds()),
		HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode,
	}).String()
}

func clearedStateCookie() string {
	return (&http.Cookie{
		Name: oidcStateCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode,
	}).String()
}

// redirect is the callback's answer, 303 with every cookie it sets: the
// session's and the cleared state cookie, two Set-Cookie headers the generated
// answer, which has one, cannot carry.
type redirect struct {
	location string
	cookies  []string
}

// VisitOidcCallbackResponse writes the answer.
func (r redirect) VisitOidcCallbackResponse(w http.ResponseWriter) error {
	for _, c := range r.cookies {
		w.Header().Add("Set-Cookie", c)
	}
	w.Header().Set("Location", r.location)
	w.WriteHeader(http.StatusSeeOther)
	return nil
}
