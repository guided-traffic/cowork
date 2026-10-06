package mcpcli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apispec "github.com/guided-traffic/cowork/backend/api"
	"github.com/guided-traffic/cowork/backend/internal/tools"
)

const testToken = "cwk_0000000000000000000000000000000000000000000"

func envOf(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := m[k]
		return v, ok
	}
}

// docs/adr/0040 D4, docs/adr/0041 D5: the environment and nothing else; an
// error names the variable and never the token.
func TestConfiguration(t *testing.T) {
	for name, tc := range map[string]struct {
		env  map[string]string
		want string
	}{
		"no url":       {map[string]string{EnvToken: testToken}, "COWORK_URL is not set"},
		"no token":     {map[string]string{EnvURL: "https://cowork.example.com/"}, "COWORK_TOKEN is not set; make a token on the installation's token page, https://cowork.example.com/me/tokens"},
		"bad token":    {map[string]string{EnvURL: "https://cowork.example.com", EnvToken: "ghp_secretvalue"}, "COWORK_TOKEN is not a cowork token"},
		"plain http":   {map[string]string{EnvURL: "http://cowork.example.com", EnvToken: testToken}, "use https"},
		"not a url":    {map[string]string{EnvURL: "cowork.example.com", EnvToken: testToken}, "must be the installation's URL"},
		"with user":    {map[string]string{EnvURL: "https://u:p@cowork.example.com", EnvToken: testToken}, "without user"},
		"with a query": {map[string]string{EnvURL: "https://cowork.example.com?x=1", EnvToken: testToken}, "without user, query"},
	} {
		_, err := loadConfig(envOf(tc.env))
		require.Error(t, err, name)
		assert.Contains(t, err.Error(), tc.want, name)
		assert.NotContains(t, err.Error(), "secretvalue", "%s: the token is never echoed", name)
	}
	for _, u := range []string{"http://localhost:8080", "http://127.0.0.1:8080/", "http://[::1]:8080", "https://cowork.example.com/sub/"} {
		cfg, err := loadConfig(envOf(map[string]string{EnvURL: u, EnvToken: testToken, EnvProjectDir: "/repo"}))
		require.NoError(t, err, u)
		assert.False(t, strings.HasSuffix(cfg.url, "/"), u)
		assert.Equal(t, "/repo", cfg.dir)
	}
}

func run(t *testing.T, e Env, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	e.Args, e.Stdout, e.Stderr = args, &stdout, &stderr
	if e.Lookup == nil {
		e.Lookup = envOf(map[string]string{})
	}
	e.Build = Build{Version: "0.9.0", Commit: "abc", Time: "0"}
	code := Run(context.Background(), e)
	return code, stdout.String(), stderr.String()
}

// docs/adr/0070 D4: exit 0 success, 1 error, 2 usage.
func TestTheCommandLine(t *testing.T) {
	code, _, stderr := run(t, Env{})
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr, "Usage: cowork-mcp")
	code, _, stderr = run(t, Env{}, "export", "acme/COW", "out")
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr, `unknown command "export acme/COW out"`)
	code, stdout, _ := run(t, Env{}, "version")
	assert.Equal(t, 0, code)
	assert.Regexp(t, `^cowork-mcp 0\.9\.0 \(commit abc, built 0, API /api/v1: \d+ operations\)\n$`, stdout)
	code, stdout, _ = run(t, Env{}, "help")
	assert.Equal(t, 0, code)
	assert.Contains(t, stdout, "session-context")
	code, _, stderr = run(t, Env{}, "serve")
	assert.Equal(t, 1, code, "serve without configuration ends at once")
	assert.Contains(t, stderr, "COWORK_URL is not set")
	code, _, stderr = run(t, Env{}, "token", "check")
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "COWORK_URL is not set")
}

// fakeAPI answers the routes a command reaches.
func fakeAPI(t *testing.T, routes map[string]any) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	for pattern, body := range routes {
		status := http.StatusOK
		if p, ok := body.(problemBody); ok {
			status = p.Status
		}
		mux.HandleFunc(pattern, func(w http.ResponseWriter, _ *http.Request) {
			ct := "application/json"
			if _, ok := body.(problemBody); ok {
				ct = "application/problem+json"
			}
			w.Header().Set("Content-Type", ct)
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(body)
		})
	}
	return mux
}

type problemBody struct {
	Status int    `json:"status"`
	Code   string `json:"code"`
	Title  string `json:"title"`
	Type   string `json:"type"`
	Detail string `json:"detail"`
}

type noRepo struct{}

func (noRepo) Remotes(context.Context) ([]tools.Remote, error)         { return nil, nil }
func (noRepo) Path(context.Context) (string, error)                    { return "", nil }
func (noRepo) BindingFile(context.Context) (*tools.BindingFile, error) { return nil, nil }
func (noRepo) WorkedSince(context.Context, time.Time) (bool, error)    { return false, nil }

var configured = envOf(map[string]string{EnvURL: "https://cowork.example.com", EnvToken: testToken})

// docs/adr/0067 D2, D5: a hook is silent without a configuration or a
// binding, says in one line what is wrong, and always exits 0.
func TestTheHooks(t *testing.T) {
	version := map[string]any{"version": "0.9.1", "commit": "c", "build_time": "0"}
	code, stdout, _ := run(t, Env{}, "session-context")
	assert.Equal(t, 0, code)
	assert.Empty(t, stdout, "no installation configured: silence")

	mux := fakeAPI(t, map[string]any{"GET /api/v1/version": version})
	code, stdout, _ = run(t, Env{Lookup: configured, Doer: tools.HandlerDoer{Handler: mux}, Workspace: noRepo{},
		Memory: &tools.InMemory{}, Stdin: strings.NewReader(`{"session_id":"s1","source":"startup"}`)}, "session-context")
	assert.Equal(t, 0, code)
	assert.Empty(t, stdout, "no remote and no binding file: silence")

	mux = fakeAPI(t, map[string]any{"GET /api/v1/version": problemBody{Status: 401, Code: "token_expired", Title: "Token expired", Type: "t"}})
	code, stdout, _ = run(t, Env{Lookup: configured, Doer: tools.HandlerDoer{Handler: mux}, Workspace: noRepo{}}, "session-context")
	assert.Equal(t, 0, code)
	assert.Equal(t, "cowork: the token in COWORK_TOKEN does not work against https://cowork.example.com (token_expired); "+
		"make a new one on https://cowork.example.com/me/tokens.\n", stdout)
	assert.NotContains(t, stdout, testToken)

	mux = fakeAPI(t, map[string]any{"GET /api/v1/version": map[string]any{"version": "1.2.0", "commit": "c", "build_time": "0"}})
	code, stdout, _ = run(t, Env{Lookup: configured, Doer: tools.HandlerDoer{Handler: mux}, Workspace: noRepo{}}, "session-context")
	assert.Equal(t, 0, code)
	assert.Contains(t, stdout, "another major version", "an API the binary does not know is said, not served")

	code, stdout, _ = run(t, Env{Lookup: configured, Doer: tools.HandlerDoer{Handler: http.NewServeMux()}, Workspace: noRepo{},
		Memory: &tools.InMemory{}}, "session-end")
	assert.Equal(t, 0, code)
	assert.Empty(t, stdout, "nothing bound, nothing to remind")
	code, stdout, _ = run(t, Env{Lookup: configured, Stdin: strings.NewReader(`{"stop_hook_active": true}`)}, "session-end")
	assert.Equal(t, 0, code)
	assert.Empty(t, stdout)
}

// sessionStartInput is the input Claude Code hands a SessionStart hook, as
// its hook reference shows it (code.claude.com/docs/en/hooks, "SessionStart
// input"): the model is a string, which Claude Code may leave out.
const sessionStartInput = `{
  "session_id": "abc123",
  "transcript_path": "/Users/.../.claude/projects/.../00893aaf-19fa-41d2-8238-13269b9b3ca0.jsonl",
  "cwd": "/Users/...",
  "hook_event_name": "SessionStart",
  "source": "resume",
  "model": "claude-opus-5",
  "seconds_since_last_response": 5400,
  "context_tokens": 182340,
  "prompt_cache_likely_expired": true,
  "estimated_cache_write_usd": 1.1396
}`

// docs/adr/0067 D5, docs/adr/0036 D3: the model Claude Code names to the
// SessionStart hook is the model of the agent mark of the server in the same
// project directory — when the hook runs after the server started, too — and
// not of another directory's server.
func TestTheSessionStartHookNamesTheModelOfTheServer(t *testing.T) {
	mux := fakeAPI(t, map[string]any{
		"GET /api/v1/version": map[string]any{"version": "0.9.1", "commit": "c", "build_time": "0"},
		"GET /api/v1/me/token": map[string]any{"id": "0199a3c2-1d2e-7f00-8000-000000000001", "name": "laptop", "scope": "write",
			"agent": true, "capabilities": []string{}, "created_at": "2026-10-01T00:00:00Z",
			"expires_at": time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339), "state": "active",
			"request": map[string]any{"agent": true, "agent_mark": "claude-code/unknown/x", "capabilities": []string{}}},
		"PUT /api/v1/tenants/{tenant}/projects/{project}/tickets/{number}/interest": map[string]any{},
	})
	mux.HandleFunc("GET /api/v1/openapi.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(apispec.Document)
	})
	var mu sync.Mutex
	var marks []string
	api := tools.HandlerDoer{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/interest") {
			mu.Lock()
			marks = append(marks, r.Header.Get("X-Cowork-Agent"))
			mu.Unlock()
		}
		mux.ServeHTTP(w, r)
	})}
	in := func(projectDir string) func(string) (string, bool) {
		return envOf(map[string]string{EnvURL: "https://cowork.example.com", EnvToken: testToken, EnvProjectDir: projectDir})
	}
	memory := &tools.InMemory{}

	serverSide, clientSide := mcp.NewInMemoryTransports()
	done := make(chan int, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		var out, errs bytes.Buffer
		done <- Run(ctx, Env{Args: []string{"serve"}, Lookup: in("/Users/ada/src/app"), Doer: api, Workspace: noRepo{},
			Memory: memory, Transport: serverSide, Stdout: &out, Stderr: &errs, Build: Build{Version: "0.9.0"}})
	}()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "claude-code", Version: "test"}, nil).Connect(ctx, clientSide, nil)
	require.NoError(t, err)
	watch := func() string {
		t.Helper()
		res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "watch", Arguments: map[string]any{"key": "acme/COW-1"}})
		require.NoError(t, err)
		require.False(t, res.IsError, res.Content[0].(*mcp.TextContent).Text)
		mu.Lock()
		defer mu.Unlock()
		return marks[len(marks)-1]
	}
	hook := func(projectDir, input string) {
		t.Helper()
		code, stdout, _ := run(t, Env{Lookup: in(projectDir), Doer: api, Workspace: noRepo{}, Memory: memory,
			Stdin: strings.NewReader(input)}, "session-context")
		assert.Equal(t, 0, code)
		assert.Empty(t, stdout, "no remote and no binding file: silence")
	}

	assert.Regexp(t, `^claude-code/unknown/[0-9a-f]{8}$`, watch(), "no session start has named a model yet")
	hook("/Users/ada/src/app", sessionStartInput)
	mark := watch()
	assert.Regexp(t, `^claude-code/claude-opus-5/[0-9a-f]{8}$`, mark, "the hook's model, the server's own session id")
	hook("/Users/ada/src/other", strings.Replace(sessionStartInput, "claude-opus-5", "claude-haiku-4-5", 1))
	assert.Equal(t, mark, watch(), "a session in another project directory names its own server's model")
	hook("/Users/ada/src/app", `{"session_id": "abc123", "hook_event_name": "SessionStart", "source": "clear"}`)
	assert.Equal(t, mark, watch(), "after /clear the running Claude Code keeps its model: the one recorded before stands")
	hook("/Users/ada/src/app", `{"session_id": "abc123", "hook_event_name": "SessionStart", "source": "compact"}`)
	assert.Equal(t, mark, watch(), "a compaction continues the session: the one recorded before stands")
	hook("/Users/ada/src/app", `{"session_id": "def456", "hook_event_name": "SessionStart", "source": "resume"}`)
	assert.Equal(t, "claude-code/unknown/"+strings.Split(mark, "/")[2], watch(),
		"a session restored without a model is not marked with an older session's")

	require.NoError(t, cs.Close())
	select {
	case code := <-done:
		assert.Equal(t, 0, code)
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not end with its session")
	}
}

// docs/adr/0070 D2: token check says whose the token is and what it may do.
func TestTokenCheck(t *testing.T) {
	mux := fakeAPI(t, map[string]any{
		"GET /api/v1/version": map[string]any{"version": "0.9.1", "commit": "c", "build_time": "0"},
		"GET /api/v1/me/token": map[string]any{"id": "0199a3c2-1d2e-7f00-8000-000000000001", "name": "laptop", "scope": "write",
			"agent": true, "capabilities": []string{"close"}, "created_at": "2026-10-01T00:00:00Z",
			"expires_at": time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339), "state": "active",
			"restricted_tenant": "acme", "restricted_project": "COW",
			"request": map[string]any{"agent": true, "agent_mark": "cowork-mcp/unknown/token-check", "capabilities": []string{"close"}}},
		"GET /api/v1/me": map[string]any{"id": "0199a3c2-1d2e-7f00-8000-000000000002", "username": "ada", "display_name": "Ada",
			"memberships": []any{}},
	})
	code, stdout, _ := run(t, Env{Lookup: configured, Doer: tools.HandlerDoer{Handler: mux}}, "token", "check")
	assert.Equal(t, 0, code, stdout)
	assert.Contains(t, stdout, "The token works against https://cowork.example.com (cowork 0.9.1).")
	assert.Contains(t, stdout, "Person:        Ada (ada)")
	assert.Contains(t, stdout, "laptop, scope write, an agent token")
	assert.Contains(t, stdout, "Restriction:   the project acme/COW")
	assert.Contains(t, stdout, "capabilities: close")

	code, stdout, _ = run(t, Env{Lookup: configured, Doer: tools.HandlerDoer{Handler: mux}}, "token", "check", "--json")
	assert.Equal(t, 0, code)
	var report map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &report))
	assert.Equal(t, true, report["valid"])
	assert.Equal(t, "COW", report["project"])

	revoked := fakeAPI(t, map[string]any{"GET /api/v1/me/token": problemBody{Status: 401, Code: "token_revoked", Title: "Token revoked", Type: "t", Detail: "the token was revoked"}})
	code, stdout, _ = run(t, Env{Lookup: configured, Doer: tools.HandlerDoer{Handler: revoked}}, "token", "check")
	assert.Equal(t, 1, code)
	assert.Contains(t, stdout, "does not work against https://cowork.example.com: cowork answered 401 token_revoked")
	assert.Contains(t, stdout, "https://cowork.example.com/me/tokens")
}

func TestLookupWithoutARepository(t *testing.T) {
	code, stdout, _ := run(t, Env{Lookup: configured, Doer: tools.HandlerDoer{Handler: http.NewServeMux()}, Workspace: noRepo{}}, "lookup")
	assert.Equal(t, 0, code)
	assert.Equal(t, "Bound to no project.\n", stdout)
	code, stdout, _ = run(t, Env{Lookup: configured, Doer: tools.HandlerDoer{Handler: http.NewServeMux()}, Workspace: noRepo{}}, "lookup", "--json")
	assert.Equal(t, 0, code)
	assert.JSONEq(t, `{"bound": false, "remotes": [], "notes": []}`, stdout)
}

// docs/adr/0040 D5: serve refuses every tool of an API it does not know.
func TestServeRefusesAnUnknownAPI(t *testing.T) {
	mux := fakeAPI(t, map[string]any{"GET /api/v1/version": map[string]any{"version": "3.0.0", "commit": "c", "build_time": "0"}})
	serverSide, clientSide := mcp.NewInMemoryTransports()
	done := make(chan int, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		var out, errs bytes.Buffer
		done <- Run(ctx, Env{Args: []string{"serve"}, Lookup: configured, Doer: tools.HandlerDoer{Handler: mux},
			Workspace: noRepo{}, Memory: &tools.InMemory{}, Transport: serverSide, Stdout: &out, Stderr: &errs,
			Build: Build{Version: "0.9.0"}})
	}()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "claude-code", Version: "test"}, nil).Connect(ctx, clientSide, nil)
	require.NoError(t, err)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "watch", Arguments: map[string]any{"key": "acme/COW-1"}})
	require.NoError(t, err)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content[0].(*mcp.TextContent).Text, "another major version")
	require.NoError(t, cs.Close())
	select {
	case code := <-done:
		assert.Equal(t, 0, code, "the server ends when the host closes the session")
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not end with its session")
	}
}
