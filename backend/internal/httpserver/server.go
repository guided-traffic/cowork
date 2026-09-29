// Package httpserver assembles the HTTP handler — the health endpoints and
// the JSON API under /api/v1/ — and runs the server. The web UI is not served
// here; it is the frontend container's job, which proxies /api/ to this one.
package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// Options configures New.
type Options struct {
	Version   string
	Commit    string
	BuildTime string
	// Ready reports whether the server can do work, typically a database
	// ping. nil means always ready.
	Ready func(ctx context.Context) error
	// Logger receives one line per request. nil discards them.
	Logger *slog.Logger
}

// New builds the handler.
func New(opts Options) http.Handler {
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	mux := http.NewServeMux()
	handleGet(mux, "/healthz", handleHealthz)
	handleGet(mux, "/readyz", handleReadyz(opts.Ready))
	handleGet(mux, "/api/v1/version", handleVersion(opts))
	mux.HandleFunc("/", handleNotFound)
	return requestLog(opts.Logger, mux)
}

// handleGet registers h for GET (and HEAD) on pattern and answers every other
// method on the same path with 405. Without the second registration the
// catch-all "/" would match those requests and answer 404.
func handleGet(mux *http.ServeMux, pattern string, h http.HandlerFunc) {
	mux.HandleFunc("GET "+pattern, h)
	mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Allow", "GET, HEAD")
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", r.Method+" is not allowed on "+r.URL.Path)
	})
}

// ListenAndServe serves handler on addr until ctx is cancelled, then shuts
// down gracefully within shutdownTimeout. A clean shutdown returns nil.
func ListenAndServe(ctx context.Context, addr string, handler http.Handler, shutdownTimeout time.Duration) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return Serve(ctx, ln, handler, shutdownTimeout)
}

// Serve is ListenAndServe on an existing listener. Serve closes the listener.
func Serve(ctx context.Context, ln net.Listener, handler http.Handler, shutdownTimeout time.Duration) error {
	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
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

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]apiError{"error": {Code: code, Message: message}})
}

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func handleReadyz(ready func(ctx context.Context) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if ready != nil {
			if err := ready(r.Context()); err != nil {
				writeError(w, http.StatusServiceUnavailable, "not_ready", err.Error())
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	}
}

func handleVersion(opts Options) http.HandlerFunc {
	body := map[string]string{
		"version":   opts.Version,
		"commit":    opts.Commit,
		"buildTime": opts.BuildTime,
	}
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, body)
	}
}

func handleNotFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, "not_found", "no route "+r.Method+" "+r.URL.Path)
}

// statusRecorder captures the status code for the request log.
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
		)
	})
}
