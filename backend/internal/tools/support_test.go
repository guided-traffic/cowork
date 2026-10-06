package tools

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The escape hatch: any route under /api/v1/ of the installation with the
// session's token, the API's answer unchanged, a key on every POST
// (docs/adr/0042 D1, D4, docs/adr/0045 D5), and nothing outside the API.
func TestTheEscapeHatch(t *testing.T) {
	f := newFake(t)
	f.on("GET /api/v1/tenants/acme/projects", http.StatusOK, list(map[string]any{"key": "COW"}), "ETag", `W/"x"`)
	f.on("POST /api/v1/tenants/acme/projects/COW/tickets/12/time-entries", http.StatusCreated, map[string]any{"id": "x"})
	f.refuse("DELETE /api/v1/tenants/acme/projects/COW/tickets/12", http.StatusMethodNotAllowed, "method_not_allowed", "no")
	s := f.session(true)

	res := call(t, s, "api", `{"method": "GET", "path": "/api/v1/tenants/acme/projects?include_archived=true"}`)
	require.False(t, res.IsError, res.Text)
	assert.True(t, strings.HasPrefix(res.Text, "GET /api/v1/tenants/acme/projects?include_archived=true → 200 OK (ETag W/\"x\")\n\n{\"items\""), res.Text)
	got := f.calls(http.MethodGet, "/api/v1/tenants/acme/projects")[0]
	assert.Equal(t, "include_archived=true", got.Query)
	assert.Equal(t, fakeAgent, got.Header.Get("X-Cowork-Agent"))

	res = call(t, s, "api", `{"method": "POST", "path": "/api/v1/tenants/acme/projects/COW/tickets/12/time-entries", "body": {"minutes": 5}}`)
	require.False(t, res.IsError, res.Text)
	posted := f.calls(http.MethodPost, "/api/v1/tenants/acme/projects/COW/tickets/12/time-entries")[0]
	_, err := uuid.Parse(posted.Header.Get("Idempotency-Key"))
	assert.NoError(t, err)
	assert.Equal(t, "application/json", posted.Header.Get("Content-Type"))
	assert.JSONEq(t, `{"minutes": 5}`, posted.Body)

	res = call(t, s, "api", `{"method": "DELETE", "path": "/api/v1/tenants/acme/projects/COW/tickets/12"}`)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Text, "→ 405")
	assert.Contains(t, res.Text, `"code":"method_not_allowed"`, "the API's JSON unchanged")

	for _, path := range []string{"https://evil.example.com/api/v1/me", "//evil.example.com/api/v1/me", "/auth/local",
		"/api/v1/../auth/local", "/api/v1//me", "/healthz", "api/v1/me"} {
		res = call(t, s, "api", `{"method": "GET", "path": "`+path+`"}`)
		assert.True(t, res.IsError, path)
	}
	assert.Len(t, f.calls(http.MethodGet, "/api/v1/me"), 0, "nothing outside the API was sent")
}

type flakyDoer struct {
	fails int
	sent  int
}

func (d *flakyDoer) Do(req *http.Request) (*http.Response, error) {
	d.sent++
	if req.Body != nil {
		_, _ = io.ReadAll(req.Body)
	}
	if d.sent <= d.fails {
		return nil, errors.New("connection reset")
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("{}"))}, nil
}

// A transport failure is retried only where a repetition cannot act twice
// (docs/adr/0045 D5); an answer is never retried (docs/adr/0040 D3).
func TestRetrying(t *testing.T) {
	send := func(method string, key bool, fails int) (int, error) {
		d := &flakyDoer{fails: fails}
		r := Retrying{Next: d, Attempts: 3, Wait: time.Millisecond}
		req, err := http.NewRequest(method, "http://cowork.test/api/v1/x", strings.NewReader(`{"a":1}`))
		require.NoError(t, err)
		if key {
			req.Header.Set("Idempotency-Key", uuid.NewString())
		}
		_, err = r.Do(req)
		return d.sent, err
	}
	sent, err := send(http.MethodGet, false, 2)
	assert.NoError(t, err)
	assert.Equal(t, 3, sent)
	sent, err = send(http.MethodPost, true, 1)
	assert.NoError(t, err)
	assert.Equal(t, 2, sent, "a keyed POST is replayed by the API")
	sent, err = send(http.MethodPost, false, 1)
	assert.Error(t, err)
	assert.Equal(t, 1, sent, "a POST without a key is sent once")
	sent, err = send(http.MethodPatch, false, 1)
	assert.Error(t, err)
	assert.Equal(t, 1, sent, "an overwriting PATCH is sent once")
	sent, err = send(http.MethodPut, false, 5)
	assert.Error(t, err)
	assert.Equal(t, 3, sent, "at most Attempts")
}

// The token's request set is what the agent holds (docs/adr/0043 D6).
func TestReadTokenHoldsTheRequestSet(t *testing.T) {
	f := newFake(t)
	f.on("GET /api/v1/me/token", http.StatusOK, map[string]any{"id": "0199a3c2-1d2e-7f00-8000-000000000001", "name": "laptop",
		"scope": "write", "agent": true, "capabilities": []any{"rank", "set-horizon"}, "created_at": "2026-10-01T00:00:00Z",
		"expires_at": "2026-12-01T00:00:00Z", "state": "active", "restricted_project": nil,
		"request": map[string]any{"agent": true, "agent_mark": fakeAgent, "capabilities": []any{"rank", "set-horizon"}}})
	tok, err := f.session(false).ReadToken(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"rank", "set-horizon"}, tok.Capabilities)
	assert.True(t, tok.Can(capSetHorizon))
	assert.Equal(t, "This agent holds set-horizon, rank.", capsLine(tok, capSetHorizon, capRank))
}

func TestAPIErrorMarkdown(t *testing.T) {
	f := newFake(t)
	f.mux.HandleFunc("GET /api/v1/me/token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"type":"t","title":"Token expired","status":401,"code":"token_expired","detail":"the token expired",
			"request_id":"r1","errors":[{"pointer":"/x","message":"m","current":3}]}`)
	})
	_, err := f.session(false).ReadToken(context.Background())
	var api *APIError
	require.ErrorAs(t, err, &api)
	assert.Equal(t, "token_expired", api.Code())
	md := api.Markdown()
	assert.Contains(t, md, "401 `token_expired`: the token expired")
	assert.Contains(t, md, "- `/x`: m (now: 3)")
	assert.Contains(t, md, "(request r1)")
	assert.Contains(t, md, "the person makes a new one")
	assert.Equal(t, "cowork answered 401 token_expired: the token expired", api.Error())

	plain := &APIError{Status: http.StatusBadGateway, Body: "<html>bad gateway</html>"}
	assert.Contains(t, plain.Markdown(), "502 without a problem body")
}

func TestFileMemory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cowork-mcp")
	m := FileMemory{Dir: dir}
	key := MemoryKey{Installation: "https://cowork.example.com:8443", Tenant: "acme", Project: "COW"}
	_, ok, err := m.LastStart(key)
	require.NoError(t, err)
	assert.False(t, ok, "a missing file is no previous session")

	at := time.Date(2026, 10, 4, 9, 0, 0, 0, time.FixedZone("x", 3600))
	require.NoError(t, m.SetLastStart(key, at))
	got, ok, err := m.LastStart(key)
	require.NoError(t, err)
	require.True(t, ok)
	assert.True(t, got.Equal(at))
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "one file, no leftovers")
	assert.Equal(t, "cowork.example.com_8443_acme_COW.json", entries[0].Name())
	info, err := os.Stat(filepath.Join(dir, entries[0].Name()))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	dirInfo, err := os.Stat(dir)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), dirInfo.Mode().Perm())

	require.NoError(t, os.WriteFile(filepath.Join(dir, entries[0].Name()), []byte("not json"), 0o600))
	_, ok, err = m.LastStart(key)
	require.NoError(t, err)
	assert.False(t, ok, "a damaged file is no previous session")
	_, ok, _ = m.LastStart(MemoryKey{Installation: key.Installation, Tenant: "acme", Project: "OPS"})
	assert.False(t, ok, "one file per binding")
}

func TestParseRemotes(t *testing.T) {
	out := "upstream\thttps://github.com/acme/cowork.git (fetch)\nupstream\thttps://github.com/acme/cowork.git (push)\n" +
		"origin\thttps://me:ghp_secret@github.com/me/cowork.git (fetch)\norigin\tgit@github.com:me/cowork.git (push)\n" +
		"backup\t/srv/backup.git (fetch)\n"
	assert.Equal(t, []Remote{
		{Name: "origin", URL: "https://github.com/me/cowork.git"},
		{Name: "backup", URL: "/srv/backup.git"},
		{Name: "upstream", URL: "https://github.com/acme/cowork.git"},
	}, parseRemotes([]byte(out)), "origin first, the fetch URLs, without credentials")
	assert.Empty(t, parseRemotes(nil))
}

func TestChangedPaths(t *testing.T) {
	out := " M backend/a.go\x00?? new file.txt\x00R  new.go\x00old.go\x00D  gone.go\x00"
	assert.Equal(t, []string{"backend/a.go", "new file.txt", "new.go", "gone.go"}, changedPaths([]byte(out)))
}

func TestReadBindingFile(t *testing.T) {
	dir := t.TempDir()
	write := func(body string) string {
		p := filepath.Join(dir, BindingFileName)
		require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
		return p
	}
	f, err := readBindingFile(write("tenant: acme\nproject: COW\npath: /services/a/\nurl: https://cowork.example.com/\n"))
	require.NoError(t, err)
	assert.Equal(t, &BindingFile{Tenant: "acme", Project: "COW", Path: "services/a", URL: "https://cowork.example.com",
		File: filepath.Join(dir, BindingFileName)}, f)
	for _, bad := range []string{"tenant: acme\n", "tenant: Acme\nproject: COW\n", "tenant: acme\nproject: cow\n",
		"tenant: acme\nproject: COW\nextra: 1\n", "tenant: acme\nproject: COW\npath: ../x\n", "[not a map"} {
		_, err := readBindingFile(write(bad))
		assert.Error(t, err, bad)
	}
	f, err = readBindingFile(filepath.Join(dir, "missing.yaml"))
	require.NoError(t, err)
	assert.Nil(t, f)
}

func TestCommitLines(t *testing.T) {
	ref := ticketRef{Tenant: "acme", Project: "VKO", Number: 12}
	assert.Equal(t, "Commits for acme/VKO-12: the subject ends with `(VKO-12)`, the body ends with the trailer "+
		"`Cowork-Ticket: acme/VKO-12`; the branch is `fix/VKO-12-guard-failover-gate` unless the person names another.",
		commitLines(ref, "bug", "Guard the failover gate"))
	for title, want := range map[string]string{
		"Guard the failover gate":                         "guard-failover-gate",
		"An über-long title with many, many more words":   "long-title-with-many-many",
		"Café: naïve export":                              "export",
		"supercalifragilisticexpialidociousness-and-more": "supercalifragilisticexpialidociousness",
		"": "",
	} {
		assert.Equal(t, want, branchSlug(title), title)
	}
	assert.Equal(t, "feat", commitType("task"))
	assert.Equal(t, "feat", commitType("feature"))
	assert.Equal(t, "docs", commitType("decision"))
	assert.Equal(t, "docs", commitType("question"))
}

// docs/adr/0040 D5: another major version, or an operation the served
// document lacks, is an API the client does not know.
func TestCompatibility(t *testing.T) {
	serve := func(version string, ops ...string) *Session {
		f := newFake(t)
		f.on("GET /api/v1/version", http.StatusOK, map[string]any{"version": version, "commit": "c", "build_time": "0"})
		paths := map[string]any{}
		for i, op := range ops {
			paths["/p"+string(rune('a'+i))] = map[string]any{"get": map[string]any{"operationId": op}}
		}
		f.on("GET /api/v1/openapi.json", http.StatusOK, map[string]any{"paths": paths})
		return f.session(false)
	}
	catalogue := Catalogue()
	all := Operations(catalogue)
	assert.NoError(t, CheckCompatibility(context.Background(), serve("0.3.0", all...), catalogue, "0.4.1"))
	assert.NoError(t, CheckCompatibility(context.Background(), serve("0.3.0", all...), catalogue, "dev"), "a development build compares no version")

	err := CheckCompatibility(context.Background(), serve("1.0.0", all...), catalogue, "0.4.1")
	var inc *IncompatibleError
	require.ErrorAs(t, err, &inc)
	assert.Contains(t, err.Error(), "another major version")

	err = CheckCompatibility(context.Background(), serve("0.3.0", all[1:]...), catalogue, "0.4.1")
	require.ErrorAs(t, err, &inc)
	assert.Equal(t, []string{all[0]}, inc.Missing)
	assert.Contains(t, err.Error(), "lacks "+all[0])
}
