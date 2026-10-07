// Package httpserver assembles the HTTP handler — the health endpoints, the
// API under /api/ and the browser's login flows under /auth/ — and runs the
// server. The web UI is not served here; it is the frontend container's job,
// and the Ingress routes /api/ and /auth/ to this one (docs/adr/0001 D3).
// Every request gets an id (X-Request-Id), a recovery from panics and one
// line in the request log; every error is an RFC 9457 problem details body
// (docs/adr/0047). The metrics are served by a listener of their own
// (NewMetrics), which shares the API listener's lifecycle (ServeAll,
// docs/adr/0060 D1).
package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/guided-traffic/cowork/backend/internal/metrics"
	"github.com/guided-traffic/cowork/backend/internal/problem"
	"github.com/guided-traffic/cowork/backend/internal/requestid"
)

// Options configures New.
type Options struct {
	// Ready reports whether the server can do work, typically a database
	// ping. nil means always ready.
	Ready func(ctx context.Context) error
	// API serves every path under /api/ and /auth/. nil answers them with 404.
	API http.Handler
	// Logger receives one line per request. nil discards them.
	Logger *slog.Logger
	// Metrics records every request in the HTTP instruments; nil records
	// nothing (docs/adr/0060 D4).
	Metrics *metrics.Metrics
}

// New builds the handler.
func New(opts Options) http.Handler {
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	mux := http.NewServeMux()
	handleGet(mux, "/healthz", handleHealthz)
	handleGet(mux, "/readyz", handleReadyz(opts.Ready, opts.Logger))
	if opts.API != nil {
		mux.Handle("/api/", opts.API)
		mux.Handle("/auth/", opts.API)
	}
	mux.HandleFunc("/", handleNotFound)
	return withRequestID(instrument(opts.Metrics, requestLog(opts.Logger, recoverer(opts.Logger, mux))))
}

// NewMetrics builds the handler of the metrics listener (docs/adr/0060 D1):
// GET /metrics answers scrape, every other path 404 and another method 405,
// as problems with a request id. It writes no request log: a scrape every few
// seconds is not a request a person made, and the listener records nothing in
// the HTTP instruments, which count the API listener's requests.
func NewMetrics(scrape http.Handler, logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	mux := http.NewServeMux()
	handleGet(mux, "/metrics", scrape.ServeHTTP)
	mux.HandleFunc("/", handleNotFound)
	return withRequestID(recoverer(logger, mux))
}

// handleGet registers h for GET (and HEAD) on pattern and answers every other
// method on the same path with 405. Without the second registration the
// catch-all "/" would match those requests and answer 404. Both name the
// path as the request's route in the metrics.
func handleGet(mux *http.ServeMux, pattern string, h http.HandlerFunc) {
	mux.HandleFunc("GET "+pattern, func(w http.ResponseWriter, r *http.Request) {
		metrics.SetRoute(r.Context(), pattern)
		h(w, r)
	})
	mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		metrics.SetRoute(r.Context(), pattern)
		w.Header().Set("Allow", "GET, HEAD")
		problem.Write(w, r, problem.New(problem.MethodNotAllowed, r.Method+" is not allowed on "+r.URL.Path))
	})
}

// ListenAndServe serves handler on addr until ctx is cancelled, then shuts
// down gracefully within shutdownTimeout. A clean shutdown returns nil.
// onShutdown runs when the shutdown begins: what ends the long-lived streams
// so the drain does not wait for them (docs/adr/0054 D9).
func ListenAndServe(ctx context.Context, addr string, handler http.Handler, shutdownTimeout time.Duration, onShutdown ...func()) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return Serve(ctx, ln, handler, shutdownTimeout, onShutdown...)
}

// Listener is a bound listener and the handler it serves; OnShutdown runs
// when its shutdown begins.
type Listener struct {
	Listener   net.Listener
	Handler    http.Handler
	OnShutdown []func()
}

// ServeAll serves every listener until ctx is cancelled or one of them stops,
// then shuts every one down, each within shutdownTimeout: one lifecycle for the
// API listener and the metrics listener (docs/adr/0060 D1). It closes the
// listeners and returns their errors joined; a clean shutdown returns nil.
func ServeAll(ctx context.Context, shutdownTimeout time.Duration, listeners ...Listener) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	errs := make([]error, len(listeners))
	var wg sync.WaitGroup
	for i, l := range listeners {
		wg.Go(func() {
			errs[i] = Serve(ctx, l.Listener, l.Handler, shutdownTimeout, l.OnShutdown...)
			cancel()
		})
	}
	wg.Wait()
	return errors.Join(errs...)
}

// Serve is ListenAndServe on an existing listener. Serve closes the listener.
func Serve(ctx context.Context, ln net.Listener, handler http.Handler, shutdownTimeout time.Duration, onShutdown ...func()) error {
	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	for _, f := range onShutdown {
		srv.RegisterOnShutdown(f)
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()

	select {
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	<-errc
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleReadyz answers whether the database answers. The ping's error goes
// to the log only: it can name the host and the user (docs/adr/0047 D3).
func handleReadyz(ready func(ctx context.Context) error, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if ready != nil {
			if err := ready(r.Context()); err != nil {
				logger.Warn("not ready", "request_id", requestid.From(r.Context()), "error", err)
				problem.Write(w, r, problem.New(problem.NotReady, "the database does not answer"))
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	}
}

func handleNotFound(w http.ResponseWriter, r *http.Request) {
	problem.Write(w, r, problem.New(problem.NotFound, "no route "+r.Method+" "+r.URL.Path))
}

// withRequestID gives every request an id of the backend's making — an
// inbound X-Request-Id is not trusted — and answers it in X-Request-Id.
func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := requestid.New()
		w.Header().Set("X-Request-Id", id.String())
		next.ServeHTTP(w, r.WithContext(requestid.With(r.Context(), id)))
	})
}

// recoverer answers a panic with a 500 that names the request id and logs
// the panic; nothing internal reaches the client (docs/adr/0047 D3).
func recoverer(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				logger.Error("panic", "request_id", requestid.From(r.Context()), "method", r.Method, "path", r.URL.Path, "panic", v)
				problem.Write(w, r, problem.New(problem.Internal, "internal error"))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// statusRecorder captures the status code for the request log. It passes
// Flush on, so a streamed response is not buffered by the log.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	return s.ResponseWriter.Write(b)
}

// Flush passes a flush through to the connection.
func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets http.ResponseController reach the connection.
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// instrument records every request in the HTTP instruments
// (docs/adr/0060 D4, D5): in flight while it runs, then its route — the
// pattern the API or this server's mux names, metrics.Unmatched when none
// does —, its method, its status and its duration. A chat turn's tool calls
// come through it as requests of their own.
func instrument(m *metrics.Metrics, next http.Handler) http.Handler {
	if m == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, done := m.Request(r.Context(), r.Method)
		rec := &statusRecorder{ResponseWriter: w}
		defer func() { done(rec.status) }()
		next.ServeHTTP(rec, r.WithContext(ctx))
	})
}

// requestLog writes one line per request: method, path, status, duration and
// the request id — never a body, a header or a query (CLAUDE.md, the runtime
// page).
func requestLog(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		if rec.status == 0 {
			rec.status = http.StatusOK
		}
		logger.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration", time.Since(start),
			"request_id", requestid.From(r.Context()),
		)
	})
}
