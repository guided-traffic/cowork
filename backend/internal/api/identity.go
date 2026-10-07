package api

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/metrics"
	"github.com/guided-traffic/cowork/backend/internal/oidc"
	"github.com/guided-traffic/cowork/backend/internal/problem"
	"github.com/guided-traffic/cowork/backend/internal/requestid"
	"github.com/guided-traffic/cowork/backend/internal/store"
)

// The times of a session's groups refresh (docs/adr/0030 D5, the security
// review of 2026-10-04, M1): refreshRetry is how long a session waits for its
// next refresh after the issuer could not be reached — it is served meanwhile;
// issuerDeadline bounds every call to the issuer of one refresh together, and
// refreshLease, longer, how long a claimed refresh is the claimant's;
// applyDeadline bounds the transaction that applies the answer.
const (
	refreshRetry   = time.Minute
	issuerDeadline = 20 * time.Second
	refreshLease   = 30 * time.Second
	applyDeadline  = 10 * time.Second
)

// ownIssuer reports whether a person is one of the configured issuer's: a
// person of another issuer, or of an identity provider that is no longer
// configured, is outside the gate whatever their groups (the security review
// of 2026-10-04, m6; docs/adr/0030 D8).
func (h *handler) ownIssuer(issuer *string) bool {
	p := h.opts.OIDC.Provider
	return p != nil && issuer != nil && *issuer == p.Issuer()
}

// issuer is the configured issuer, "" without one.
func (h *handler) issuer() string {
	if p := h.opts.OIDC.Provider; p != nil {
		return p.Issuer()
	}
	return ""
}

// gate judges a person's issuer and groups (docs/adr/0030 D1): admitted when
// the person is the configured issuer's and one of their groups is an allowed
// group or the administrator group, which also makes them a global
// administrator. Without an identity provider nobody is admitted (D8).
func (h *handler) gate(issuer *string, groups []string) (admitted, admin bool) {
	if !h.ownIssuer(issuer) {
		return false, false
	}
	o := h.opts.OIDC
	for _, g := range groups {
		if o.AdminGroup != "" && g == o.AdminGroup {
			admitted, admin = true, true
		}
		if slices.Contains(o.AllowedGroups, g) {
			admitted = true
		}
	}
	return admitted, admin
}

// oidcOffered says whether the login page offers the identity provider: one is
// configured and its gate admits somebody (docs/adr/0030 D8).
func (h *handler) oidcOffered() bool {
	o := h.opts.OIDC
	return o.Provider != nil && (len(o.AllowedGroups) > 0 || o.AdminGroup != "")
}

// checkProviderSession is what a request of a session the identity provider's
// login made meets before it is served: a person who is no longer the
// configured issuer's loses every session at once (m6), and groups older than
// the refresh interval are read again (docs/adr/0030 D5). It reports whether
// the session ended.
func (h *handler) checkProviderSession(ctx context.Context, rec store.SessionRecord, hash []byte, now time.Time) (bool, error) {
	if !h.ownIssuer(rec.Person.OidcIssuer) {
		err := h.opts.DB.EndProviderSessions(ctx, rec.Session.UserID, requestid.UUID(ctx), h.sourceHash(clientFrom(ctx).Client))
		h.logger.Info("a session of a person the configured issuer does not name ended", "request_id", requestid.From(ctx),
			"person", rec.Session.UserID)
		return true, err
	}
	if !store.RefreshDue(rec.Session.Method, rec.Session.GroupsRefreshedAt, rec.Session.RefreshRetryAt, now, h.opts.OIDC.GroupsRefresh) {
		return false, nil
	}
	return h.refreshSession(ctx, rec, hash, now)
}

// refreshSession reads a session's groups again (docs/adr/0030 D5,
// docs/adr/0031 D3, D4; the security review of 2026-10-04, M1, m1): a short
// transaction claims the refresh by a lease — another request finds it taken
// and is served on the groups the session holds, without waiting — the issuer
// is asked with no connection and no lock held, on a context the client's
// leaving does not end, so a refresh token the issuer rotated is never lost
// with the request; a second short transaction applies the answer. It reports
// whether the session ended.
func (h *handler) refreshSession(ctx context.Context, rec store.SessionRecord, hash []byte, now time.Time) (bool, error) {
	lease, claimed, err := h.opts.DB.ClaimSessionRefresh(ctx, store.RefreshClaim{Hash: hash, PersonID: rec.Session.UserID, Now: now,
		Interval: h.opts.OIDC.GroupsRefresh, Lease: refreshLease})
	if err != nil || !claimed {
		return false, err
	}
	detached := context.WithoutCancel(ctx)
	decision := h.askIssuer(detached, lease.Sealed, hash, deref(rec.Person.OidcSubject), now)
	applyCtx, cancel := context.WithTimeout(detached, applyDeadline)
	defer cancel()
	res, err := h.opts.DB.ApplySessionRefresh(applyCtx, store.SessionRefresh{
		Hash: hash, PersonID: rec.Session.UserID, Lease: lease.Until, Now: h.opts.Now(), RetryAfter: refreshRetry,
		RequestID: requestid.UUID(ctx), SourceHash: h.sourceHash(clientFrom(ctx).Client), Decision: decision, Judge: h.gate,
	})
	if err != nil {
		return false, err
	}
	if res.Reason != "" {
		h.logger.Info("session groups refresh", "request_id", requestid.From(ctx), "person", rec.Session.UserID,
			"ended", res.Ended, "reason", res.Reason)
	}
	return res.Ended, nil
}

// askIssuer asks the issuer for the groups with the session's refresh token,
// within issuerDeadline. Without a refresh token, or without a provider, there
// is nothing to ask, and the person's groups as they stand are judged again.
// The refresh token never reaches a log.
func (h *handler) askIssuer(ctx context.Context, sealed, hash []byte, subject string, readAt time.Time) store.RefreshDecision {
	provider := h.opts.OIDC.Provider
	if sealed == nil || provider == nil {
		return store.RefreshDecision{Verdict: store.RefreshJudged}
	}
	token, err := h.refreshSealer.Open(sealed, hash)
	if err != nil {
		// A server key that changed cannot open the token any more: the
		// session cannot be refreshed, so it ends, and the person logs in
		// again.
		h.logger.Warn("a session's refresh token does not open; the session ends", "request_id", requestid.From(ctx))
		return store.RefreshDecision{Verdict: store.RefreshRefused}
	}
	callCtx, cancel := context.WithTimeout(ctx, issuerDeadline)
	defer cancel()
	refreshed, err := provider.Refresh(callCtx, string(token), subject)
	var rotated []byte
	if refreshed.RefreshToken != "" && refreshed.RefreshToken != string(token) {
		rotated = h.refreshSealer.Seal([]byte(refreshed.RefreshToken), hash)
	}
	switch {
	case errors.Is(err, oidc.ErrRefreshRefused):
		h.logger.Info("the issuer refused a session's refresh", "request_id", requestid.From(ctx), "error", err)
		return store.RefreshDecision{Verdict: store.RefreshRefused}
	case errors.Is(err, oidc.ErrClientRejected):
		h.logger.Error("the issuer refuses cowork's client; check COWORK_OIDC_CLIENT_ID and COWORK_OIDC_CLIENT_SECRET",
			"request_id", requestid.From(ctx), "error", err)
		return store.RefreshDecision{Verdict: store.RefreshUnreachable, Sealed: rotated}
	case err != nil:
		h.logger.Warn("the issuer could not refresh a session's groups; serving it and trying again later",
			"request_id", requestid.From(ctx), "error", err)
		return store.RefreshDecision{Verdict: store.RefreshUnreachable, Sealed: rotated}
	case !refreshed.GroupsKnown:
		h.noGroups.Do(func() {
			h.logger.Warn("the issuer's refresh carries no groups claim, in the ID token or UserInfo; sessions keep the groups of their login")
		})
		return store.RefreshDecision{Verdict: store.RefreshJudged, Sealed: rotated}
	}
	return store.RefreshDecision{Verdict: store.RefreshRead, Groups: refreshed.Groups, ReadAt: readAt, Sealed: rotated}
}

// streamStillAdmitted is what the heartbeat of an open event stream adds to its
// check of the credential (docs/adr/0054 D5): a session of the identity
// provider meets what a request of it meets — refreshed when it is due,
// without moving its idle clock, which a stream must not — and a token of a
// person of the provider meets the gate, as a request would
// (docs/adr/0030 D5, docs/adr/0035 D8).
func (h *handler) streamStillAdmitted(ctx context.Context, p auth.Principal) bool {
	now := h.opts.Now()
	if p.Session {
		if p.SessionMethod != store.MethodOIDC {
			return true
		}
		rec, err := h.opts.DB.LookupSession(ctx, [32]byte(p.SessionHash))
		if err != nil {
			return false
		}
		ended, err := h.checkProviderSession(ctx, rec, p.SessionHash, now)
		return err == nil && !ended
	}
	if !p.Provider {
		return true
	}
	var person struct {
		issuer          *string
		checked, groups *time.Time
	}
	err := h.opts.DB.Installation(ctx, func(r *store.Reader) error {
		u, err := r.GetUser(ctx, p.PersonID)
		person.issuer, person.checked, person.groups = u.OidcIssuer, u.GateCheckedAt, u.OidcGroupsAt
		return err
	})
	if err != nil || !h.ownIssuer(person.issuer) || h.groupsTooOld(person.groups, now) {
		return false
	}
	if !store.GateDue(true, person.checked, now, h.opts.OIDC.GroupsRefresh) {
		return true
	}
	admitted, err := h.opts.DB.CheckTokenGate(ctx, store.TokenGate{PersonID: p.PersonID, Now: now, Interval: h.opts.OIDC.GroupsRefresh,
		RequestID: requestid.UUID(ctx), SourceHash: h.sourceHash(clientFrom(ctx).Client), Judge: h.gate})
	return err == nil && admitted
}

// groupsTooOld reports whether a person's stored groups — read at their last
// sign-in or at the last session refresh that read them — are older than
// COWORK_OIDC_GROUPS_MAX_AGE, and so too old to judge a token by
// (docs/adr/0035 D8). Groups never read are too old.
func (h *handler) groupsTooOld(readAt *time.Time, now time.Time) bool {
	return readAt == nil || now.Sub(*readAt) > h.opts.OIDC.GroupsMaxAge
}

// tokenGate checks a token's person against the gate with the groups of their
// last login or refresh, at most once per refresh interval (docs/adr/0035 D8):
// a person outside it has no working token from that moment, and the token
// works again once they are back inside. It is not revoked. A person who is
// not the configured issuer's is outside at once, whatever was checked (m6),
// and so is one whose groups are older than the maximum age, until a sign-in in
// the browser reads them again.
func (h *handler) tokenGate(r *http.Request, rec store.TokenRecord, now time.Time) *problem.Error {
	if !rec.Person.Provider {
		return nil
	}
	refused := func() *problem.Error {
		h.recordRefusal(r, rec, metrics.TokenNotAllowed)
		return unauthenticated(problem.NotAllowed, "the person is no longer admitted by the identity provider's groups")
	}
	if !h.ownIssuer(rec.Person.OidcIssuer) {
		return refused()
	}
	if h.groupsTooOld(rec.Person.OidcGroupsAt, now) {
		h.recordRefusal(r, rec, metrics.TokenNotAllowed)
		return unauthenticated(problem.NotAllowed, "the person's groups were last read from the identity provider more than "+
			h.opts.OIDC.GroupsMaxAge.String()+" ago: sign in to cowork in the browser once, and the token works again")
	}
	if !store.GateDue(true, rec.Person.GateCheckedAt, now, h.opts.OIDC.GroupsRefresh) {
		return nil
	}
	ctx := r.Context()
	admitted, err := h.opts.DB.CheckTokenGate(ctx, store.TokenGate{
		PersonID: rec.Person.ID, Now: now, Interval: h.opts.OIDC.GroupsRefresh,
		RequestID: requestid.UUID(ctx), SourceHash: h.sourceHash(clientFrom(ctx).Client), Judge: h.gate,
	})
	if err != nil {
		h.logger.Error("the token gate failed", "request_id", requestid.From(ctx), "error", err)
		return problem.New(problem.Internal, "internal error")
	}
	if admitted {
		return nil
	}
	return refused()
}
