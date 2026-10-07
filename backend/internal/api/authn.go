package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/metrics"
	"github.com/guided-traffic/cowork/backend/internal/problem"
	"github.com/guided-traffic/cowork/backend/internal/requestid"
	"github.com/guided-traffic/cowork/backend/internal/store"
)

// credentials are what an operation accepts, read from the security
// requirements the document declares for it (docs/adr/0046 D6): the bearer
// token, the session cookie, or either.
type credentials struct{ bearer, session bool }

func (c credentials) any() bool { return c.bearer || c.session }

// credentialsOf reads the operation's own requirements, or the document's
// default when it declares none.
func credentialsOf(doc *openapi3.T, op *openapi3.Operation) credentials {
	reqs := doc.Security
	if op.Security != nil {
		reqs = *op.Security
	}
	var c credentials
	for _, req := range reqs {
		if _, ok := req["bearerToken"]; ok {
			c.bearer = true
		}
		if _, ok := req["sessionCookie"]; ok {
			c.session = true
		}
	}
	return c
}

// authenticate resolves a request to its person by one resolver for both
// credentials (docs/adr/0031 D6). A request with an Authorization header is a
// token's, whatever else it carries — its cookie, if any, is not looked at, so
// the CSRF check that belongs to cookies is never one request's way past a
// token and a token never borrows a session. Otherwise the session cookie
// decides, where the operation takes one. A token on an operation that takes
// a session only is refused with 403 once it has proved to be a token.
func (h *handler) authenticate(r *http.Request, accepts credentials) (auth.Principal, *problem.Error) {
	if r.Header.Get("Authorization") != "" {
		p, perr := h.authenticateToken(r)
		if perr != nil {
			return auth.Principal{}, perr
		}
		if !accepts.bearer {
			h.opts.Metrics.TokenRefused(metrics.TokenSessionOnly)
			return auth.Principal{}, problem.New(problem.SessionRequired, "this route is for a person in a browser session; a token cannot call it")
		}
		return p, nil
	}
	if accepts.session {
		if cookie, err := r.Cookie(auth.SessionCookie); err == nil {
			return h.authenticateSession(r, cookie.Value)
		}
	}
	return auth.Principal{}, unauthenticated(problem.Unauthenticated, "a personal access token or a session is required")
}

// authenticateToken resolves the request's bearer token to its person
// (docs/adr/0035, docs/adr/0036). A token is presented in the Authorization
// header only, never in a query parameter or a cookie (docs/adr/0035 D7).
func (h *handler) authenticateToken(r *http.Request) (auth.Principal, *problem.Error) {
	plaintext, ok := bearer(r.Header.Get("Authorization"))
	if !ok || !auth.WellFormedToken(plaintext) {
		h.opts.Metrics.TokenRefused(metrics.TokenMalformed)
		return auth.Principal{}, unauthenticated(problem.Unauthenticated, "a personal access token is required: Authorization: Bearer cwk_…")
	}
	ctx := r.Context()
	rec, err := h.opts.DB.LookupToken(ctx, auth.HashToken(plaintext))
	if errors.Is(err, store.ErrNotFound) {
		h.opts.Metrics.TokenRefused(metrics.TokenUnknown)
		return auth.Principal{}, unauthenticated(problem.Unauthenticated, "the token is not known")
	}
	if err != nil {
		h.logger.Error("token lookup failed", "request_id", requestid.From(ctx), "error", err)
		return auth.Principal{}, problem.New(problem.Internal, "internal error")
	}
	now := h.opts.Now()
	switch {
	case rec.Token.RevokedAt != nil || rec.Person.DeactivatedAt != nil:
		h.recordRefusal(r, rec, metrics.TokenRevoked)
		return auth.Principal{}, unauthenticated(problem.TokenRevoked, "the token was revoked")
	case !rec.Token.ExpiresAt.After(now):
		h.recordRefusal(r, rec, metrics.TokenExpired)
		return auth.Principal{}, unauthenticated(problem.TokenExpired, "the token expired on "+rec.Token.ExpiresAt.UTC().Format("2006-01-02"))
	}

	if perr := h.tokenGate(r, rec, now); perr != nil {
		return auth.Principal{}, perr
	}
	header, perr := agentHeader(r)
	if perr != nil {
		return auth.Principal{}, perr
	}
	agent, capabilities := auth.Mark(rec.Token.Agent, rec.Token.Capabilities, header)
	h.touch(r, rec, now)

	p := auth.Principal{
		PersonID:     rec.Person.ID,
		DisplayName:  rec.Person.DisplayName,
		TokenID:      rec.Token.ID,
		TokenName:    rec.Token.Name,
		Scope:        rec.Token.Scope,
		Agent:        agent,
		Capabilities: capabilities,
		Provider:     rec.Person.Provider,
	}
	if rec.Token.RestrictedTenantID != nil {
		p.RestrictedTenantID = *rec.Token.RestrictedTenantID
	}
	if rec.Token.RestrictedProjectID != nil {
		p.RestrictedProjectID = *rec.Token.RestrictedProjectID
	}
	return p, nil
}

// bearer extracts the credential of an "Authorization: Bearer …" header.
func bearer(header string) (string, bool) {
	scheme, credential, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	return strings.TrimSpace(credential), true
}

func unauthenticated(code problem.Code, detail string) *problem.Error {
	return &problem.Error{
		Code:    code,
		Detail:  detail,
		Headers: map[string]string{"WWW-Authenticate": `Bearer realm="cowork"`},
	}
}

// recordRefusal records the use of a dead token, bounded per token, reason
// and hour (docs/adr/0035 D9); the request log and the metrics have every
// refusal. A failure to record is logged and does not change the answer.
func (h *handler) recordRefusal(r *http.Request, rec store.TokenRecord, reason metrics.TokenRefusal) {
	ctx := r.Context()
	h.opts.Metrics.TokenRefused(reason)
	h.logger.Info("token refused", "request_id", requestid.From(ctx), "token_id", rec.Token.ID, "reason", reason)
	if err := h.opts.DB.RecordTokenRefusal(ctx, rec, string(reason), requestid.UUID(ctx), h.sourceHash(clientFrom(ctx).Client)); err != nil {
		h.logger.Error("recording a token refusal failed", "request_id", requestid.From(ctx), "error", err)
	}
}

// touch sets the token's last-used date once per UTC day (docs/adr/0035 D2);
// a process-local note saves the write for the rest of the day. A failure is
// logged: bookkeeping never fails a request.
func (h *handler) touch(r *http.Request, rec store.TokenRecord, now time.Time) {
	day := now.UTC().Format(time.DateOnly)
	h.touchedMu.Lock()
	seen := h.touched[rec.Token.ID] == day
	h.touched[rec.Token.ID] = day
	h.touchedMu.Unlock()
	if seen || (rec.Token.LastUsedOn != nil && rec.Token.LastUsedOn.UTC().Format(time.DateOnly) == day) {
		return
	}
	ctx := r.Context()
	if err := h.opts.DB.TouchTokenLastUsed(ctx, rec.Token.UserID, rec.Token.ID, now); err != nil {
		h.logger.Error("touching the token failed", "request_id", requestid.From(ctx), "error", err)
	}
}
