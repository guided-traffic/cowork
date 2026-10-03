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
	"github.com/guided-traffic/cowork/backend/internal/events"
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
	// SessionKey is the server key; list cursors are signed with a key
	// derived from it (docs/adr/0048 D1).
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
	doc, served, err := loadDocument(opts.Version)
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
	ctx := withAccept(r.Context(), r.Header.Get("Accept"))
	if requiresBearer(h.doc, route.Operation) {
		p, perr := h.authenticate(r)
		if perr != nil {
			problem.Write(w, r, perr)
			return
		}
		ctx = auth.WithPrincipal(ctx, p)
		ctx = store.WithCaller(ctx, callerOf(p, requestid.UUID(ctx)))
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

// requiresBearer reports whether an operation declares the bearer scheme,
// on itself or through the document's default.
func requiresBearer(doc *openapi3.T, op *openapi3.Operation) bool {
	reqs := doc.Security
	if op.Security != nil {
		reqs = *op.Security
	}
	for _, req := range reqs {
		if _, ok := req["bearerToken"]; ok {
			return true
		}
	}
	return false
}

// callerOf turns the principal into what the store records on its acts.
func callerOf(p auth.Principal, requestID uuid.UUID) store.Caller {
	return store.Caller{
		UserID:              p.PersonID,
		TokenID:             p.TokenID,
		RestrictedProjectID: p.RestrictedProjectID,
		Agent:               p.Agent,
		Capabilities:        p.Capabilities,
		RequestID:           requestID,
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
