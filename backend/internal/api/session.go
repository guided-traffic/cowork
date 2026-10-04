package api

import (
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/config"
	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/problem"
	"github.com/guided-traffic/cowork/backend/internal/requestid"
	"github.com/guided-traffic/cowork/backend/internal/store"
)

// authenticateSession resolves a session cookie to its person (docs/adr/0031
// D1, D3, D6). Whatever is wrong with the cookie — malformed, unknown, ended,
// past a limit, its person deactivated — is the same 401, and the answer tells
// the browser to drop it. A session has no agent flag and no scope: it acts
// with the person's whole role, which the scope admin leaves to the role. An
// X-Cowork-Agent header makes its request an agent's, with every capability
// and held to the hard-off list as a plain token's with the header — the chat
// of the UI marks its tool calls so (docs/adr/0036 D3, docs/adr/0076); the
// header only ever narrows, and a malformed one is refused.
func (h *handler) authenticateSession(r *http.Request, value string) (auth.Principal, *problem.Error) {
	ctx := r.Context()
	dead := sessionEnded()
	if !auth.WellFormedSession(value) {
		return auth.Principal{}, dead
	}
	hash := auth.HashSession(value)
	rec, err := h.opts.DB.LookupSession(ctx, hash)
	if errors.Is(err, store.ErrNotFound) {
		return auth.Principal{}, dead
	}
	if err != nil {
		h.logger.Error("session lookup failed", "request_id", requestid.From(ctx), "error", err)
		return auth.Principal{}, problem.New(problem.Internal, "internal error")
	}
	now := h.opts.Now()
	if !h.sessionLive(rec.Session.ExpiresAt, rec.Session.LastSeenAt, now) || rec.Person.DeactivatedAt != nil {
		return auth.Principal{}, dead
	}
	if rec.Session.Method == store.MethodOIDC {
		ended, err := h.checkProviderSession(ctx, rec, hash[:], now)
		if err != nil {
			h.logger.Error("the session's groups refresh failed", "request_id", requestid.From(ctx), "error", err)
			return auth.Principal{}, problem.New(problem.Internal, "internal error")
		}
		if ended {
			return auth.Principal{}, dead
		}
		// A refresh may have changed the person's administrator flag.
		if rec, err = h.opts.DB.LookupSession(ctx, hash); err != nil {
			return auth.Principal{}, dead
		}
	}
	header, perr := agentHeader(r)
	if perr != nil {
		return auth.Principal{}, perr
	}
	if err := h.opts.DB.TouchSession(ctx, rec, now); err != nil {
		h.logger.Error("touching the session failed", "request_id", requestid.From(ctx), "error", err)
	}
	p := auth.Principal{
		PersonID:               rec.Person.ID,
		DisplayName:            rec.Person.DisplayName,
		Session:                true,
		SessionHash:            hash[:],
		SessionMethod:          rec.Session.Method,
		Provider:               rec.Person.Provider,
		Scope:                  domain.ScopeAdmin,
		GlobalAdmin:            rec.Person.GlobalAdmin,
		PasswordChangeRequired: rec.Person.PasswordChangeRequired,
	}
	p.Agent, p.Capabilities = auth.Mark(false, nil, header)
	return p, nil
}

// agentHeader reads the X-Cowork-Agent header of a request: empty without
// one, a validation problem for one that breaks the rule (docs/adr/0036 D3).
func agentHeader(r *http.Request) (string, *problem.Error) {
	v := r.Header.Get(auth.AgentHeader)
	if v == "" {
		return "", nil
	}
	parsed, err := auth.ParseAgentHeader(v)
	if err != nil {
		return "", problem.Field("header:"+auth.AgentHeader, err.Error())
	}
	return parsed, nil
}

// sessionLive reports whether neither limit has passed: the absolute one, set
// at login, and the idle one, which each use moves (docs/adr/0031 D3).
func (h *handler) sessionLive(expiresAt, lastSeenAt, now time.Time) bool {
	return now.Before(expiresAt) && now.Before(lastSeenAt.Add(h.opts.SessionIdle))
}

// sessionEnded is the 401 of a cookie that names no live session. It clears
// the cookie, so the browser stops sending a dead one.
func sessionEnded() *problem.Error {
	e := unauthenticated(problem.Unauthenticated, "the session is not known or has ended")
	e.Headers["Set-Cookie"] = clearedSessionCookie()
	return e
}

// sessionCookie is the cookie a login sets: HttpOnly, Secure in every
// environment — browsers treat localhost as secure — SameSite=Lax, Path=/ and
// no Domain, which the __Host- prefix makes the browser enforce
// (docs/adr/0031 D2). maxAge is the time left to the absolute limit; the server
// decides, the cookie only stops being sent when the session cannot be live.
func sessionCookie(value string, maxAge time.Duration) string {
	return (&http.Cookie{
		Name: auth.SessionCookie, Value: value, Path: "/", MaxAge: int(maxAge.Seconds()),
		HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode,
	}).String()
}

// clearedSessionCookie asks the browser to forget the cookie.
func clearedSessionCookie() string {
	return (&http.Cookie{
		Name: auth.SessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode,
	}).String()
}

// The operations a session may call while its password is temporary: reading
// who it is, changing the password, and leaving (docs/adr/0033 D4).
var whileChangingPassword = map[string]bool{"getMe": true, "changeMyPassword": true, "logout": true}

// csrf holds a request of a session to docs/adr/0037 D1: on every unsafe
// method the Origin — or, without one, the Referer — must be the
// installation's origin exactly, and X-Requested-With: cowork must be present.
// A request with neither Origin nor Referer is refused, not waved through, and
// so is every request while no COWORK_BASE_URL is configured: the check fails
// closed. Reads never mutate, so they are not checked (D2).
func (h *handler) csrf(r *http.Request) *problem.Error {
	if safeMethod(r.Method) {
		return nil
	}
	if perr := h.checkOrigin(r); perr != nil {
		return perr
	}
	if r.Header.Get("X-Requested-With") != "cowork" {
		return problem.New(problem.Csrf, "a write of a session needs the header X-Requested-With: cowork")
	}
	return nil
}

// checkOrigin is the half of the CSRF check the login has too: it carries no
// session yet, and a cross-site login attempt is refused all the same
// (docs/adr/0037 D5).
func (h *handler) checkOrigin(r *http.Request) *problem.Error {
	if safeMethod(r.Method) {
		return nil
	}
	if h.opts.BaseOrigin == "" {
		return problem.New(problem.Csrf, "this installation has no COWORK_BASE_URL, so no write of a cookie can be checked")
	}
	if !h.fromOurOrigin(r) {
		return problem.New(problem.Csrf, "the request does not come from this installation's origin, COWORK_BASE_URL")
	}
	return nil
}

func safeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}

// fromOurOrigin compares the Origin header, or without one the origin of the
// Referer, with COWORK_BASE_URL exactly: scheme, host and port. A second
// Origin or Referer header, an Origin of "null" and a Referer that is no URL
// are mismatches.
func (h *handler) fromOurOrigin(r *http.Request) bool {
	if origins := r.Header.Values("Origin"); len(origins) > 0 {
		return len(origins) == 1 && origins[0] == h.opts.BaseOrigin
	}
	referers := r.Header.Values("Referer")
	if len(referers) != 1 {
		return false
	}
	u, err := url.Parse(referers[0])
	if err != nil || u.Host == "" {
		return false
	}
	origin, err := config.Origin(u.Scheme + "://" + u.Host)
	return err == nil && origin == h.opts.BaseOrigin
}
