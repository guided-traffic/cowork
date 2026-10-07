package api

import (
	"context"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/config"
	"github.com/guided-traffic/cowork/backend/internal/metrics"
	"github.com/guided-traffic/cowork/backend/internal/problem"
	"github.com/guided-traffic/cowork/backend/internal/requestid"
	"github.com/guided-traffic/cowork/backend/internal/store"
)

// clientFacts are what the login handlers need of the connection and the
// headers, which a strict handler does not see: the address of the client, the
// session cookie the request presented, the state cookie of a login through
// the identity provider, and the User-Agent.
type clientFacts struct {
	// Client is the client's address under the rule of docs/adr/0035 D2: the
	// TCP peer, or — behind the trusted proxies of COWORK_TRUSTED_PROXIES — the
	// first address of X-Forwarded-For, from the right, that is not one of them.
	Client    string
	Cookie    string
	OIDCState string
	UserAgent string
}

type clientKey struct{}

func withClient(ctx context.Context, r *http.Request, trusted trustedProxies) context.Context {
	c := clientFacts{Client: trusted.clientAddress(r.RemoteAddr, r.Header.Values("X-Forwarded-For")), UserAgent: r.UserAgent()}
	if cookie, err := r.Cookie(auth.SessionCookie); err == nil {
		c.Cookie = cookie.Value
	}
	if cookie, err := r.Cookie(oidcStateCookie); err == nil {
		c.OIDCState = cookie.Value
	}
	return context.WithValue(ctx, clientKey{}, c)
}

func clientFrom(ctx context.Context) clientFacts {
	c, _ := ctx.Value(clientKey{}).(clientFacts)
	return c
}

// newAddressKey derives the key that hashes the source address of a login from
// the server key, under a label of its own, so the cursors, the addresses and
// any later use of the server key never share a key.
func newAddressKey(sessionKey []byte) []byte {
	key, err := hkdf.Key(sha256.New, sessionKey, nil, "cowork login address v1", sha256.Size)
	if err != nil {
		panic(err) // only an impossible key length fails
	}
	return key
}

// newSourceKey derives the key that hashes the client address an audit row
// carries (docs/adr/0035 D2), under a label of its own.
func newSourceKey(sessionKey []byte) []byte {
	key, err := hkdf.Key(sha256.New, sessionKey, nil, "cowork audit address v1", sha256.Size)
	if err != nil {
		panic(err) // only an impossible key length fails
	}
	return key
}

// sourceHash is the keyed hash of the client address an audit row written for
// the request carries (docs/adr/0035 D2): the whole address, unlike the
// throttle's, which counts an IPv6 client by its /64. clientAddress has already
// unmapped it and dropped its zone and port. A remote that is no address — a
// test's — is hashed as it is.
func (h *handler) sourceHash(client string) []byte {
	mac := hmac.New(sha256.New, h.sourceKey)
	mac.Write([]byte(client))
	return mac.Sum(nil)
}

// addressHash is the keyed hash of a client address: what the throttle counts
// by, and all it keeps of an address. The address is the client's under the
// rule of docs/adr/0035 D2 (clientAddress), so behind the trusted proxies it is
// the browser's and not the Ingress controller's; with no trusted proxy it is
// the TCP peer's, and every browser behind one controller pod shares its
// address (docs/security/local-accounts.md). An IPv6 client counts by its /64, the
// network one subscriber is given: it holds 2^64 addresses, and a bucket per
// address would give it a fresh one with every attempt.
func (h *handler) addressHash(client string) []byte {
	host, _, err := net.SplitHostPort(client)
	if err != nil {
		host = client
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		addr = addr.Unmap().WithZone("")
		host = addr.String()
		if addr.Is6() {
			if network, err := addr.Prefix(64); err == nil {
				host = network.String()
			}
		}
	}
	mac := hmac.New(sha256.New, h.addressKey)
	mac.Write([]byte(host))
	return mac.Sum(nil)
}

// GetAuthOptions answers what the login page offers (docs/adr/0033 D8): the
// identity provider's button while one is configured and its gate admits
// somebody (docs/adr/0030 D8); and the policies the forms follow, the length
// of a password and the longest lifetime of a token in whole days
// (docs/adr/0035 D4).
func (s *Server) GetAuthOptions(ctx context.Context, _ apigen.GetAuthOptionsRequestObject) (apigen.GetAuthOptionsResponseObject, error) {
	local, err := s.db.LocalLoginAvailable(ctx)
	if err != nil {
		return nil, err
	}
	o := s.h.opts.OIDC
	out := apigen.GetAuthOptions200JSONResponse{Local: local, Oidc: s.h.oidcOffered(), OidcName: nullableOf[string](nil),
		PasswordMinLength: s.h.opts.PasswordMinLength, TokenMaxLifetimeDays: int(s.h.opts.TokenMaxLifetime / (24 * time.Hour))}
	if o.Provider != nil {
		out.OidcName = nullableOf(&o.DisplayName)
	}
	return out, nil
}

func invalidCredentials() *problem.Error {
	return problem.New(problem.InvalidCredentials, "the username or the password is wrong")
}

// LoginLocal verifies a username and a password and starts a session
// (docs/adr/0033, docs/adr/0031 D5). Every way to fail is the same 401 in the
// same time: the password is verified against the account's hash, or against a
// dummy hash when the username names no usable account; the attempt is counted
// and locked by the username as presented, known or not; and one transaction
// under the username's lock decides the outcome (docs/adr/0033 D6).
func (s *Server) LoginLocal(ctx context.Context, req apigen.LoginLocalRequestObject) (apigen.LoginLocalResponseObject, error) {
	c := clientFrom(ctx)
	now := s.h.opts.Now()
	address := s.h.addressHash(c.Client)
	if perr := s.throttled(ctx, address, now); perr != nil {
		return nil, perr
	}
	username := auth.NormaliseUsername(req.Body.Username)
	acc, err := s.db.LookupLogin(ctx, username)
	if err != nil {
		return nil, err
	}
	usable := acc.Found && !acc.Deactivated
	hash := acc.Hash
	if !usable {
		hash = s.h.dummyHash
	}
	matched, err := s.passwordFits(ctx, req.Body.Password, hash)
	if err != nil {
		return nil, err
	}
	attempt := s.attempt(ctx, username, acc, matched && usable, address, "login")
	if matched && usable && !acc.GlobalAdmin && !acc.Initialised {
		attempt.Refusal = "not_initialised"
	}
	outcome, err := s.db.RecordLoginAttempt(ctx, attempt)
	if err != nil {
		return nil, err
	}
	if outcome == store.LoginSucceeded {
		res, err := s.startSession(ctx, acc, c, now)
		if err == nil {
			s.h.opts.Metrics.Login(metrics.LoginLocal, metrics.LoginSuccess)
		}
		return res, err
	}
	s.h.opts.Metrics.Login(metrics.LoginLocal, localOutcome(outcome))
	if outcome == store.LoginRefused {
		return nil, problem.New(problem.NotInitialised, "this installation is not initialised; contact an administrator")
	}
	return nil, invalidCredentials()
}

// localOutcome is what the metrics call a local login that made no session
// (docs/adr/0060 D4).
func localOutcome(o store.LoginOutcome) metrics.LoginOutcome {
	switch o {
	case store.LoginLocked:
		return metrics.LoginLocked
	case store.LoginRefused:
		return metrics.LoginRefused
	}
	return metrics.LoginFailure
}

// throttled answers 429 once the address has made as many attempts within the
// minute as COWORK_LOGIN_ADDRESS_LIMIT allows, before any hash is computed
// (docs/adr/0033 D6). A throttled attempt is not counted, so the minute slides.
func (s *Server) throttled(ctx context.Context, address []byte, now time.Time) error {
	limit := s.h.opts.LoginAddressLimit
	if limit <= 0 {
		return nil
	}
	n, err := s.db.AddressAttempts(ctx, address, now.Add(-store.AddressWindow))
	if err != nil {
		return err
	}
	if n < int64(limit) {
		return nil
	}
	s.h.opts.Metrics.Login(metrics.LoginLocal, metrics.LoginThrottled)
	return &problem.Error{Code: problem.TooManyAttempts, Detail: "too many login attempts from this address; wait a minute",
		Headers: map[string]string{"Retry-After": strconv.Itoa(int(store.AddressWindow.Seconds()))}}
}

// passwordFits verifies a presented password against a stored hash. A damaged
// hash is a password that does not fit, not a 500 that would tell an unknown
// username from a known one; only the request's own end is an error.
func (s *Server) passwordFits(ctx context.Context, password, hash string) (bool, error) {
	matched, err := auth.VerifyPassword(ctx, password, hash)
	if err != nil && ctx.Err() != nil {
		return false, err
	}
	if err != nil {
		s.h.logger.Error("a stored password hash cannot be verified", "request_id", requestid.From(ctx))
		return false, nil
	}
	return matched, nil
}

// attempt is what a login or a password change tells the store of a password it
// has verified.
func (s *Server) attempt(ctx context.Context, username string, acc store.LoginAccount, verified bool, address []byte, what string) store.LoginAttempt {
	return store.LoginAttempt{
		Username:    username,
		Account:     acc,
		Verified:    verified,
		Address:     address,
		Now:         s.h.opts.Now(),
		MaxFailures: s.h.opts.LoginMaxFailures,
		Window:      store.LoginWindow,
		Sticky:      s.h.opts.LoginLockout == config.LockoutAdmin,
		Context:     what,
		RequestID:   requestid.UUID(ctx),
		SourceHash:  s.h.sourceHash(clientFrom(ctx).Client),
	}
}

// startSession makes the session of a verified login: a new cookie value,
// never one seen before, replacing the session the request presented
// (docs/adr/0031 D5).
func (s *Server) startSession(ctx context.Context, acc store.LoginAccount, c clientFacts, now time.Time) (apigen.LoginLocalResponseObject, error) {
	value, hash, err := auth.GenerateSession()
	if err != nil {
		return nil, err
	}
	expires := now.Add(s.h.opts.SessionLifetime)
	session := store.NewSession{PersonID: acc.UserID, Hash: hash, Now: now, Expires: expires, RequestID: requestid.UUID(ctx),
		SourceHash: s.h.sourceHash(c.Client)}
	if c.UserAgent != "" {
		ua := sha256.Sum256([]byte(c.UserAgent))
		session.UserAgentHash = ua[:]
	}
	if auth.WellFormedSession(c.Cookie) {
		old := auth.HashSession(c.Cookie)
		session.Replaces = old[:]
	}
	if err := s.db.CreateSession(ctx, session); err != nil {
		return nil, err
	}
	cookie := sessionCookie(value, expires.Sub(now))
	return apigen.LoginLocal200JSONResponse{
		Body:    apigen.LocalLoginResult{PasswordChangeRequired: acc.PasswordChangeRequired},
		Headers: apigen.LoginLocal200ResponseHeaders{SetCookie: &cookie},
	}, nil
}

// Logout ends the session of the request (docs/adr/0031 D4) and clears its
// cookie. A session another request ended first is no error. A session of the
// identity provider whose issuer names an end_session_endpoint answers where
// the browser ends its session at the issuer too.
func (s *Server) Logout(ctx context.Context, _ apigen.LogoutRequestObject) (apigen.LogoutResponseObject, error) {
	p := principal(ctx)
	_, err := s.db.Mutate(ctx, uuid.Nil, func(w *store.Writer) error {
		n, err := w.DeleteSessionByHash(ctx, p.SessionHash)
		if err != nil {
			return err
		}
		if n == 0 {
			return store.ErrNoChange
		}
		w.Record(store.Event{EntityType: entityUser, EntityID: p.PersonID, Action: "logged_out"})
		return nil
	})
	if err != nil && !errors.Is(err, store.ErrNoChange) {
		return nil, err
	}
	cleared := clearedSessionCookie()
	if provider := s.h.opts.OIDC.Provider; provider != nil && p.SessionMethod == store.MethodOIDC {
		if end := provider.EndSessionURL(s.h.opts.BaseOrigin + "/login"); end != "" {
			return apigen.Logout200JSONResponse{Body: apigen.LogoutResult{EndSessionUrl: end},
				Headers: apigen.Logout200ResponseHeaders{SetCookie: &cleared}}, nil
		}
	}
	return apigen.Logout204Response{Headers: apigen.Logout204ResponseHeaders{SetCookie: &cleared}}, nil
}
