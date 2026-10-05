package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
)

const (
	fakeToken = "cwk_0000000000000000000000000000000000000000000"
	fakeAgent = "claude-code/unknown/test"
)

// fakeAPI is the API as the tools see it: routes answering canned bodies, and
// every request recorded with its headers and body. It is reached through
// HandlerDoer, in process, as a host inside the backend reaches the real one.
type fakeAPI struct {
	t   *testing.T
	mux *http.ServeMux
	mu  sync.Mutex
	got []request
}

type request struct {
	Method, Path, Query string
	Header              http.Header
	Body                string
}

func newFake(t *testing.T) *fakeAPI {
	t.Helper()
	return &fakeAPI{t: t, mux: http.NewServeMux()}
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.got = append(f.got, request{Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery, Header: r.Header.Clone(), Body: string(raw)})
	f.mu.Unlock()
	r.Body = io.NopCloser(strings.NewReader(string(raw)))
	if _, pattern := f.mux.Handler(r); pattern == "" {
		problem(w, http.StatusNotFound, "not_found", "no route "+r.Method+" "+r.URL.Path)
		return
	}
	f.mux.ServeHTTP(w, r)
}

// on answers a route with a status and a JSON body.
func (f *fakeAPI) on(pattern string, status int, body any, headers ...string) {
	f.mux.HandleFunc(pattern, func(w http.ResponseWriter, _ *http.Request) {
		for i := 0; i+1 < len(headers); i += 2 {
			w.Header().Set(headers[i], headers[i+1])
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	})
}

// text answers a route with Markdown.
func (f *fakeAPI) text(pattern, body string) {
	f.mux.HandleFunc(pattern, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		_, _ = io.WriteString(w, body)
	})
}

// refuse answers a route with a problem.
func (f *fakeAPI) refuse(pattern string, status int, code, detail string) {
	f.mux.HandleFunc(pattern, func(w http.ResponseWriter, _ *http.Request) { problem(w, status, code, detail) })
}

func problem(w http.ResponseWriter, status int, code, detail string) {
	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"type": "https://cowork.dev/problems/" + code, "title": code, "status": status,
		"detail": detail, "code": code, "request_id": "0199a3c2-1d2e-7f00-8000-0000000000ff"})
}

// calls are the recorded requests to a method and path.
func (f *fakeAPI) calls(method, path string) []request {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []request
	for _, c := range f.got {
		if c.Method == method && c.Path == path {
			out = append(out, c)
		}
	}
	return out
}

// writes are the recorded requests that are no GET.
func (f *fakeAPI) writes() []request {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []request
	for _, c := range f.got {
		if c.Method != http.MethodGet {
			out = append(out, c)
		}
	}
	return out
}

// session is a session against the fake, bound to acme/COW unless unbound.
func (f *fakeAPI) session(bound bool) *Session {
	f.t.Helper()
	api, err := apigen.NewClientWithResponses("http://cowork.test", apigen.WithHTTPClient(HandlerDoer{Handler: f}),
		apigen.WithRequestEditorFn(Editor(fakeToken, func() string { return fakeAgent }, "cowork-mcp/test")))
	require.NoError(f.t, err)
	s := NewSession(api, "https://cowork.example.com")
	s.Now = func() time.Time { return time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC) }
	if bound {
		s.Bind("acme", "COW")
	}
	return s
}

// call runs a tool of the catalogue with JSON arguments.
func call(t *testing.T, s *Session, name, args string) Result {
	t.Helper()
	for _, tool := range Catalogue() {
		if tool.Name == name {
			return tool.Call(context.Background(), s, json.RawMessage(args))
		}
	}
	t.Fatalf("no tool %s", name)
	return Result{}
}

// ticket is a ticket's JSON as the API answers it.
func ticket(key, state string, edit ...func(map[string]any)) map[string]any {
	project, number, _ := strings.Cut(key[strings.Index(key, "/")+1:], "-")
	var n int
	_, _ = fmt.Sscan(number, &n)
	t := map[string]any{
		"id": uuid.NewString(), "key": key, "project": project, "number": n, "type": "task", "title": "Ship it",
		"body": "", "state": state, "block": nil, "severity": "medium", "security": "none", "threat": nil,
		"horizon": "later", "horizon_set": nil, "effort": "S", "progress": 40, "progress_refinement": 100,
		"urgency": "later", "urgency_derived": "later", "urgency_rule": "v2:default", "urgency_override": nil,
		"progress_review": 0, "progress_derived": false,
		"parent": nil, "reporter": map[string]any{"id": uuid.NewString(), "display_name": "Ada"}, "assignee": nil,
		"confidential": false, "opened_at": "2026-10-01T00:00:00Z", "decided_at": nil, "done_at": nil, "done_from": nil,
		"done_by_hand": false, "open_prerequisites": 0, "version": 3, "created_at": "2026-10-01T00:00:00Z",
		"updated_at": "2026-10-02T00:00:00Z",
	}
	for _, e := range edit {
		e(t)
	}
	return t
}

// list is a page of items.
func list(items ...any) map[string]any {
	if items == nil {
		items = []any{}
	}
	return map[string]any{"items": items, "next_cursor": nil}
}

// decodeBody reads a recorded JSON body.
func decodeBody(t *testing.T, c request) map[string]any {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(c.Body), &m))
	return m
}
