package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/problem"
	"github.com/guided-traffic/cowork/backend/internal/requestid"
	"github.com/guided-traffic/cowork/backend/internal/store"
)

// authenticate resolves the request's bearer token to its person
// (docs/adr/0035, docs/adr/0036). A token is presented in the Authorization
// header only, never in a query parameter or a cookie (docs/adr/0035 D7).
func (h *handler) authenticate(r *http.Request) (auth.Principal, *problem.Error) {
	plaintext, ok := bearer(r.Header.Get("Authorization"))
	if !ok || !auth.WellFormedToken(plaintext) {
		return auth.Principal{}, unauthenticated(problem.Unauthenticated, "a personal access token is required: Authorization: Bearer cwk_…")
	}
	ctx := r.Context()
	rec, err := h.opts.DB.LookupToken(ctx, auth.HashToken(plaintext))
	if errors.Is(err, store.ErrNotFound) {
		return auth.Principal{}, unauthenticated(problem.Unauthenticated, "the token is not known")
	}
	if err != nil {
		h.logger.Error("token lookup failed", "request_id", requestid.From(ctx), "error", err)
		return auth.Principal{}, problem.New(problem.Internal, "internal error")
	}
	now := h.opts.Now()
	switch {
	case rec.Token.RevokedAt != nil || rec.Person.DeactivatedAt != nil:
		h.recordRefusal(r, rec, "revoked")
		return auth.Principal{}, unauthenticated(problem.TokenRevoked, "the token was revoked")
	case !rec.Token.ExpiresAt.After(now):
		h.recordRefusal(r, rec, "expired")
		return auth.Principal{}, unauthenticated(problem.TokenExpired, "the token expired on "+rec.Token.ExpiresAt.UTC().Format("2006-01-02"))
	}

	header := ""
	if v := r.Header.Get(auth.AgentHeader); v != "" {
		parsed, err := auth.ParseAgentHeader(v)
		if err != nil {
			return auth.Principal{}, problem.Field("header:"+auth.AgentHeader, err.Error())
		}
		header = parsed
	}
	agent, capabilities := auth.Mark(rec.Token.Agent, rec.Token.Capabilities, header)
	h.touch(r, rec, now)

	p := auth.Principal{
		PersonID:     rec.Person.ID,
		DisplayName:  rec.Person.DisplayName,
		TokenID:      rec.Token.ID,
		Scope:        rec.Token.Scope,
		Agent:        agent,
		Capabilities: capabilities,
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
// and hour (docs/adr/0035 D9); the request log has every refusal. A failure
// to record is logged and does not change the answer.
func (h *handler) recordRefusal(r *http.Request, rec store.TokenRecord, reason string) {
	ctx := r.Context()
	h.logger.Info("token refused", "request_id", requestid.From(ctx), "token_id", rec.Token.ID, "reason", reason)
	if err := h.opts.DB.RecordTokenRefusal(ctx, rec, reason, requestid.UUID(ctx)); err != nil {
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
