// Package api serves the API under /api/v1 from the generated server
// interface (docs/adr/0046). Every request runs one pipeline before its
// handler: the operation is found in the API document; it is authenticated
// when the document says so (docs/adr/0035, 0036); a route under a tenant
// passes the tenant boundary (docs/adr/0023 D5); the request is held to its
// size and time limits (docs/adr/0039) and validated against the document
// (docs/adr/0046 D4); only then does the handler run. Every error is a
// problem details body (docs/adr/0047).
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"
	"github.com/google/uuid"

	apispec "github.com/guided-traffic/cowork/backend/api"
	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/config"
	"github.com/guided-traffic/cowork/backend/internal/events"
	"github.com/guided-traffic/cowork/backend/internal/oidc"
	"github.com/guided-traffic/cowork/backend/internal/problem"
	"github.com/guided-traffic/cowork/backend/internal/requestid"
	"github.com/guided-traffic/cowork/backend/internal/storage"
	"github.com/guided-traffic/cowork/backend/internal/store"
)

// Options configures New.
type Options struct {
	DB        *store.DB
	Logger    *slog.Logger
	Version   string
	Commit    string
	BuildTime string
	// SessionKey is the server key; list cursors are signed, and the rank
	// positions in them sealed, with keys derived from it (docs/adr/0048 D1).
	SessionKey []byte
	// MaxJSONBody, RequestTimeout, MaxPageSize and MaxQueryLength are the
	// limits of docs/adr/0039 D2; 0 disables each.
	MaxJSONBody    int64
	RequestTimeout time.Duration
	MaxPageSize    int
	MaxQueryLength int
	// Storage holds the attachments' bytes; nil refuses uploads
	// (docs/adr/0016 D1).
	Storage *storage.Client
	// AttachmentMaxBytes is the per-file maximum; AttachmentMaxPerTicket the
	// per-ticket count, 0 for none (docs/adr/0016 D6).
	AttachmentMaxBytes     int64
	AttachmentMaxPerTicket int
	// Events fans the published acts out to the event streams; nil serves
	// no stream (docs/adr/0054).
	Events *events.Hub
	// Heartbeat is the event stream's heartbeat; 0 means twenty seconds
	// (docs/adr/0054 D5).
	Heartbeat time.Duration
	// ValidateResponses checks every response against the document and
	// answers 500 when one does not match: on in the tests, so a handler that
	// drifts from the document fails a test (docs/adr/0046 D4).
	ValidateResponses bool
	// Now is the clock; nil means time.Now.
	Now func() time.Time

	// BaseOrigin is COWORK_BASE_URL as the origin a browser sends
	// (config.Origin): what the CSRF check compares the Origin header with.
	// Empty refuses every write of a session cookie and the login
	// (docs/adr/0037 D1, D6).
	BaseOrigin string
	// SessionLifetime and SessionIdle are the absolute and the idle limit of a
	// session; zero means the defaults (docs/adr/0031 D3).
	SessionLifetime time.Duration
	SessionIdle     time.Duration
	// PasswordMinLength is the shortest password; zero means the default
	// (docs/adr/0033 D3).
	PasswordMinLength int
	// LoginLockout is config.LockoutWindow, or config.LockoutAdmin for a lock
	// that stays until an administrator unlocks; empty means the window.
	// LoginMaxFailures failures of a username within the window lock it, and
	// LoginAddressLimit attempts of an address within a minute are answered
	// 429; 0 switches each off (docs/adr/0033 D6, docs/adr/0039 D6).
	LoginLockout      string
	LoginMaxFailures  int
	LoginAddressLimit int
	// TokenDefaultLifetime and TokenMaxLifetime bound the lifetime of a token
	// a person creates; zero means the defaults (docs/adr/0035 D4).
	TokenDefaultLifetime time.Duration
	TokenMaxLifetime     time.Duration
	// TrustedProxies are the networks of the proxies in front of the backend
	// (COWORK_TRUSTED_PROXIES): the client address of a request, which the
	// login throttle counts, is found by walking X-Forwarded-For from the
	// right through them. Empty: the TCP peer is the client and the header is
	// never read (docs/adr/0035 D2, docs/adr/0033 D6).
	TrustedProxies []netip.Prefix
	// OIDC is the identity provider and its gate; the zero value is none
	// (docs/adr/0029, docs/adr/0030).
	OIDC OIDCOptions
}

// OIDCOptions is the identity provider the browser logs in through, and the
// groups that decide who may (docs/adr/0029 D4, docs/adr/0030 D1, D5, D8).
type OIDCOptions struct {
	// Provider is the discovered issuer; nil without one, and then the gate
	// admits nobody: a person of a provider that is no longer configured is
	// refused (docs/adr/0030 D8).
	Provider *oidc.Provider
	// AllowedGroups and AdminGroup are the gate; a member of AdminGroup is a
	// global administrator.
	AllowedGroups []string
	AdminGroup    string
	// GroupsRefresh is how often a session's groups are read again and a
	// token's person is checked against the gate; zero means the default.
	GroupsRefresh time.Duration
	// DisplayName is the provider's name on the login page's button.
	DisplayName string
}

// handler is the API: the router over the document, the generated mux and
// the pipeline in front of them.
type handler struct {
	opts      Options
	doc       *openapi3.T
	served    []byte
	router    routers.Router
	mux       *http.ServeMux
	server    *Server
	logger    *slog.Logger
	touchedMu sync.Mutex
	touched   map[uuid.UUID]string
	// addressKey keys the hash of a login's client address; dummyHash is what
	// a password is verified against when the username names no usable account.
	addressKey []byte
	dummyHash  string
	// fingerprintKey keys the fingerprint of an idempotent request.
	fingerprintKey []byte
	// trusted are the proxies the client address is walked through.
	trusted trustedProxies
	// sourceKey keys the hash of the client address an audit row carries
	// (docs/adr/0035 D2).
	sourceKey []byte
	// loginSealer seals the state of a login through the identity provider
	// into its cookie, refreshSealer a session's refresh token
	// (docs/adr/0031 D1).
	loginSealer, refreshSealer auth.Sealer
	// noRefreshToken and noGroups warn once per process: an issuer that gives
	// no refresh token leaves a session on its login's groups, and one whose
	// refresh carries no groups claim makes the refresh read nothing
	// (docs/adr/0030 D5).
	noRefreshToken, noGroups sync.Once
}

// New builds the API handler. It fails only when the embedded document does
// not load, which is a build defect.
func New(opts Options) (http.Handler, error) {
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	withDefaults(&opts)
	doc, served, err := loadDocument(opts.Version)
	if err != nil {
		return nil, err
	}
	dummy, err := auth.DummyHash(context.Background())
	if err != nil {
		return nil, err
	}
	router, err := gorillamux.NewRouter(doc)
	if err != nil {
		return nil, fmt.Errorf("route the API document: %w", err)
	}
	h := &handler{
		opts:    opts,
		doc:     doc,
		served:  served,
		router:  router,
		mux:     http.NewServeMux(),
		logger:  opts.Logger,
		touched: map[uuid.UUID]string{},

		addressKey:     newAddressKey(opts.SessionKey),
		dummyHash:      dummy,
		fingerprintKey: newFingerprintKey(opts.SessionKey),
		trusted:        newTrustedProxies(opts.TrustedProxies),
		sourceKey:      newSourceKey(opts.SessionKey),
		loginSealer:    auth.NewSealer(opts.SessionKey, auth.LabelOIDCLogin),
		refreshSealer:  auth.NewSealer(opts.SessionKey, auth.LabelRefreshToken),
	}
	h.server = &Server{h: h, db: opts.DB, cursors: newCursorCodec(opts.SessionKey), storage: opts.Storage,
		uploads: make(chan struct{}, uploadSlots(opts.AttachmentMaxBytes))}
	strict := apigen.NewStrictHandlerWithOptions(h.server, nil, apigen.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			problem.Write(w, r, &problem.Error{Code: problem.ValidationFailed, Detail: "the request body is not valid JSON for this route"})
		},
		ResponseErrorHandlerFunc: h.writeError,
	})
	apigen.HandlerWithOptions(strict, apigen.StdHTTPServerOptions{
		BaseRouter: h.mux,
		ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			problem.Write(w, r, problem.New(problem.ValidationFailed, err.Error()))
		},
	})
	return h, nil
}

// withDefaults fills what a zero value leaves to the configuration's defaults.
func withDefaults(o *Options) {
	if o.SessionLifetime <= 0 {
		o.SessionLifetime = config.DefaultSessionLifetime
	}
	if o.SessionIdle <= 0 {
		o.SessionIdle = config.DefaultSessionIdle
	}
	if o.PasswordMinLength <= 0 {
		o.PasswordMinLength = config.DefaultPasswordMinLength
	}
	if o.LoginLockout == "" {
		o.LoginLockout = config.LockoutWindow
	}
	if o.TokenDefaultLifetime <= 0 {
		o.TokenDefaultLifetime = config.DefaultTokenDefaultLifetime
	}
	if o.TokenMaxLifetime <= 0 {
		o.TokenMaxLifetime = config.DefaultTokenMaxLifetime
	}
	if o.OIDC.GroupsRefresh <= 0 {
		o.OIDC.GroupsRefresh = config.DefaultOIDCGroupsRefresh
	}
}

// loadDocument loads the embedded document and returns it with the backend
// version as info.version, and its JSON as served (docs/adr/0046 D5).
func loadDocument(version string) (*openapi3.T, []byte, error) {
	var raw map[string]any
	if err := json.Unmarshal(apispec.Document, &raw); err != nil {
		return nil, nil, fmt.Errorf("decode the API document: %w", err)
	}
	if info, ok := raw["info"].(map[string]any); ok && version != "" {
		info["version"] = version
	}
	served, err := json.Marshal(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("encode the API document: %w", err)
	}
	doc, err := openapi3.NewLoader().LoadFromData(served)
	if err != nil {
		return nil, nil, fmt.Errorf("load the API document: %w", err)
	}
	// The server is mounted at /, so the document's paths are matched as
	// written whatever host the request names.
	doc.Servers = nil
	return doc, served, nil
}

var allMethods = []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete}

// ServeHTTP runs the pipeline.
func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	route, pathParams, err := h.router.FindRoute(r)
	if err != nil {
		h.writeRouteError(w, r, err)
		return
	}
	ctx := withClient(withAccept(r.Context(), r.Header.Get("Accept")), r, h.trusted)
	opID := route.Operation.OperationID
	if accepts := credentialsOf(h.doc, route.Operation); accepts.any() {
		p, perr := h.authenticate(r.WithContext(ctx), accepts)
		if perr != nil {
			problem.Write(w, r, perr)
			return
		}
		if perr := h.sessionRules(r, p, opID); perr != nil {
			problem.Write(w, r, perr)
			return
		}
		ctx = auth.WithPrincipal(ctx, p)
		ctx = store.WithCaller(ctx, callerOf(p, requestid.UUID(ctx), h.sourceHash(clientFrom(ctx).Client)))
	} else if originChecked(route.Operation) {
		if perr := h.checkOrigin(r); perr != nil {
			problem.Write(w, r, perr)
			return
		}
	}
	if slug, ok := pathParams["tenant"]; ok {
		scope, perr := h.boundary(ctx, slug, route.Path, route.Operation.OperationID)
		if perr != nil {
			problem.Write(w, r, perr)
			return
		}
		ctx = withTenant(ctx, scope)
	}
	if route.Operation.OperationID == opStreamEvents {
		// A stream lives longer than any request timeout (docs/adr/0039 D2).
		r = r.WithContext(ctx)
		if perr := h.validateRequest(r, route, pathParams); perr != nil {
			problem.Write(w, r, perr)
			return
		}
		h.serveEvents(w, r)
		return
	}
	if h.opts.RequestTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, h.opts.RequestTimeout)
		defer cancel()
	}
	r = r.WithContext(ctx)
	bodyDeadline(w, r)
	if perr := h.limitBody(w, r); perr != nil {
		problem.Write(w, r, perr)
		return
	}
	if perr := h.validateRequest(r, route, pathParams); perr != nil {
		problem.Write(w, r, perr)
		return
	}
	if h.opts.ValidateResponses {
		h.serveValidated(w, r, route, pathParams)
		return
	}
	h.mux.ServeHTTP(w, r)
}

// writeRouteError answers a request no operation of the document matches:
// 405 with Allow when the path exists with other methods, 404 otherwise.
func (h *handler) writeRouteError(w http.ResponseWriter, r *http.Request, err error) {
	if !errors.Is(err, routers.ErrMethodNotAllowed) {
		problem.Write(w, r, problem.New(problem.NotFound, "no route "+r.Method+" "+r.URL.Path))
		return
	}
	var allowed []string
	for _, m := range allMethods {
		probe := r.Clone(r.Context())
		probe.Method = m
		if _, _, err := h.router.FindRoute(probe); err == nil {
			allowed = append(allowed, m)
		}
	}
	w.Header().Set("Allow", joinMethods(allowed))
	problem.Write(w, r, problem.New(problem.MethodNotAllowed, r.Method+" is not allowed on "+r.URL.Path))
}

// joinMethods is the Allow header: the methods the document declares on the
// path. HEAD is not among them — the router serves what the document
// declares, and the document declares no HEAD.
func joinMethods(methods []string) string {
	return strings.Join(methods, ", ")
}

// originChecked reports whether a public operation says it is origin-checked:
// the login, which no session protects yet (docs/adr/0037 D5).
func originChecked(op *openapi3.Operation) bool {
	v, _ := op.Extensions["x-cowork-origin-check"].(bool)
	return v
}

// openQuery reports whether an operation takes query parameters the document
// does not name: the identity provider's callback, to which the issuer may add
// its own, such as iss or session_state, without cowork assuming any of them
// (docs/adr/0029 D1).
func openQuery(op *openapi3.Operation) bool {
	v, _ := op.Extensions["x-cowork-open-query"].(bool)
	return v
}

// sessionRules are what holds a request authenticated by a session and no
// other: the CSRF check on its writes (docs/adr/0037 D1), and the temporary
// password that has to be changed before anything else (docs/adr/0033 D4). A
// token's request has no cookie, and neither applies (docs/adr/0035 D7).
func (h *handler) sessionRules(r *http.Request, p auth.Principal, opID string) *problem.Error {
	if !p.Session {
		return nil
	}
	if perr := h.csrf(r); perr != nil {
		return perr
	}
	if p.PasswordChangeRequired && !whileChangingPassword[opID] {
		return problem.New(problem.PasswordChangeRequired, "the password of this account is temporary: change it with PUT /api/v1/me/password first")
	}
	return nil
}

// callerOf turns the principal into what the store records on its acts. The
// session's hash is what finds its row again; the audit rows never carry it
// (docs/adr/0031 D7). They carry the hash of the client address instead
// (docs/adr/0035 D2).
func callerOf(p auth.Principal, requestID uuid.UUID, sourceHash []byte) store.Caller {
	return store.Caller{
		UserID:              p.PersonID,
		TokenID:             p.TokenID,
		SessionHash:         p.SessionHash,
		RestrictedProjectID: p.RestrictedProjectID,
		Agent:               p.Agent,
		Capabilities:        p.Capabilities,
		RequestID:           requestID,
		SourceHash:          sourceHash,
	}
}

// writeError answers a handler's error: a problem as it is, a missing or
// invisible row as 404, a cancelled deadline as 504, anything else as 500
// with the details in the log only (docs/adr/0047 D3).
func (h *handler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	var perr *problem.Error
	switch {
	case errors.As(err, &perr):
		problem.Write(w, r, perr)
	case errors.Is(err, store.ErrNotFound):
		problem.Write(w, r, problem.New(problem.NotFound, ""))
	case errors.Is(err, store.ErrIdempotencyMismatch):
		problem.Write(w, r, problem.New(problem.IdempotencyMismatch, "the Idempotency-Key was used before with a different request"))
	case errors.Is(err, context.DeadlineExceeded):
		problem.Write(w, r, problem.New(problem.Timeout, "the request took longer than the configured limit"))
	default:
		h.logger.Error("request failed", "request_id", requestid.From(r.Context()), "method", r.Method, "path", r.URL.Path, "error", err)
		problem.Write(w, r, problem.New(problem.Internal, "internal error"))
	}
}
