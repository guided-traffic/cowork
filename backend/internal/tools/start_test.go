package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
)

// fakeWorkspace is a repository as the tools read it.
type fakeWorkspace struct {
	remotes []Remote
	path    string
	file    *BindingFile
	worked  bool
}

func (w fakeWorkspace) Remotes(context.Context) ([]Remote, error) { return w.remotes, nil }
func (w fakeWorkspace) Path(context.Context) (string, error)      { return w.path, nil }
func (w fakeWorkspace) BindingFile(context.Context) (*BindingFile, error) {
	return w.file, nil
}
func (w fakeWorkspace) WorkedSince(context.Context, time.Time) (bool, error) { return w.worked, nil }

var origin = []Remote{{Name: "origin", URL: "git@github.com:acme/cowork.git"}}

func boundLookup() map[string]any {
	return map[string]any{"status": "bound", "remotes": []any{map[string]any{"remote": origin[0].URL, "identity": "github.com/acme/cowork"}},
		"bindings": []any{map[string]any{"tenant": map[string]any{"slug": "acme", "name": "Acme"},
			"project": map[string]any{"key": "COW", "name": "cowork"}, "identity": "github.com/acme/cowork", "path": "",
			"remote": origin[0].URL, "archived": false}}, "proposal": nil, "proposal_unavailable": nil}
}

func startSession(f *fakeAPI, ws Workspace) *Session {
	s := f.session(false)
	s.Workspace = ws
	s.Memory = &InMemory{}
	return s
}

// docs/adr/0067 D1, D7, docs/adr/0042 D5: bound by the remote, the active
// ticket's context with five comments and ten acts, the commit strings, and
// on the next start what happened since.
func TestStartBoundWithAnActiveTicket(t *testing.T) {
	f := newFake(t)
	f.on("GET /api/v1/me/repositories/lookup", http.StatusOK, boundLookup())
	f.on("GET /api/v1/tenants/acme/projects/COW/tickets", http.StatusOK, list(
		ticket("acme/COW-12", "in-progress", func(m map[string]any) { m["title"] = "Guard the gate" }),
		ticket("acme/COW-14", "in-progress")))
	f.text("GET "+ticketPath+"/context", "<!-- cowork: context of acme/COW-12 -->\nTHE CONTEXT\n")
	f.on("GET "+ticketPath+"/activity", http.StatusOK, list(
		map[string]any{"id": "0199a3c2-1d2e-7f00-8000-0000000000e2", "at": "2026-10-04T08:30:00Z",
			"actor": map[string]any{"id": samID, "display_name": "Sam"}, "actor_system": nil, "agent": nil, "action": "commented",
			"entity_type": "comment", "entity_id": nil, "before": nil, "after": nil, "reason": nil, "note": nil,
			"explained_by_comment": nil, "redacted": false},
		map[string]any{"id": "0199a3c2-1d2e-7f00-8000-0000000000e1", "at": "2026-10-02T08:30:00Z",
			"actor": map[string]any{"id": samID, "display_name": "Sam"}, "actor_system": nil, "agent": nil, "action": "edited",
			"entity_type": "comment", "entity_id": nil, "before": nil, "after": nil, "reason": nil, "note": nil,
			"explained_by_comment": nil, "redacted": false}))
	f.on("GET /api/v1/tenants/acme/projects/COW/tickets/14/activity", http.StatusOK, list(
		map[string]any{"id": "0199a3c2-1d2e-7f00-8000-0000000000e4", "at": "2026-10-04T07:00:00Z",
			"actor": map[string]any{"id": samID, "display_name": "Sam"}, "agent": "claude-code/opus/x", "action": "transitioned",
			"before": map[string]any{"state": "decided"}, "after": map[string]any{"state": "in-progress"}, "redacted": false}))
	f.on("GET /api/v1/tenants/acme/tickets", http.StatusOK, list(ticket("acme/COW-20", "review", func(m map[string]any) { m["title"] = "Other" })))
	s := startSession(f, fakeWorkspace{remotes: origin, path: "backend"})

	block, silent, err := Start(context.Background(), s, StartOptions{Record: true})
	require.NoError(t, err)
	assert.False(t, silent)
	assert.True(t, strings.HasPrefix(block, "# cowork: acme/COW — cowork\n"), block)
	assert.Contains(t, block, "bound by the remote `github.com/acme/cowork` (origin), as the server binds it")
	assert.Contains(t, block, "## Active ticket: acme/COW-12 — Guard the gate (in-progress)")
	assert.Contains(t, block, "Also in progress for you: acme/COW-14 — Ship it.")
	assert.Contains(t, block, "THE CONTEXT")
	assert.Contains(t, block, "`Cowork-Ticket: acme/COW-12`")
	assert.NotContains(t, block, "Since your last session", "a first session has no last one (docs/adr/0042 D5)")
	assert.Equal(t, "acme/COW", s.Binding().Key())

	lookup := f.calls(http.MethodGet, "/api/v1/me/repositories/lookup")[0]
	q, _ := url.ParseQuery(lookup.Query)
	assert.Equal(t, []string{origin[0].URL}, q["remote"])
	assert.Equal(t, "backend", q.Get("path"))
	ctxQuery, _ := url.ParseQuery(f.calls(http.MethodGet, ticketPath+"/context")[0].Query)
	assert.Equal(t, "5", ctxQuery.Get("comments"))
	assert.Equal(t, "10", ctxQuery.Get("activity"))
	listQuery, _ := url.ParseQuery(f.calls(http.MethodGet, "/api/v1/tenants/acme/projects/COW/tickets")[0].Query)
	assert.Equal(t, []string{"in-progress"}, listQuery["state"])
	assert.Equal(t, []string{"me"}, listQuery["assignee"])

	// The memory now holds this start; the next one shows what happened since.
	at, ok, err := s.Memory.LastStart(MemoryKey{Installation: s.Installation, Tenant: "acme", Project: "COW"})
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, s.Memory.SetLastStart(MemoryKey{Installation: s.Installation, Tenant: "acme", Project: "COW"}, at.Add(-24*time.Hour)))
	block, _, err = Start(context.Background(), s, StartOptions{Record: false})
	require.NoError(t, err)
	assert.Contains(t, block, "## Since your last session (2026-10-03 09:00 UTC)")
	assert.Contains(t, block, "- acme/COW-12: one act, under Recent activity above", "the active ticket's acts are in its context")
	assert.Contains(t, block, "- 2026-10-04 07:00 UTC acme/COW-14 — Sam via claude-code/opus/x transitioned decided → in-progress")
	assert.Contains(t, block, "- acme/COW-20 — Other (review), changed")
	changed, _ := url.ParseQuery(f.calls(http.MethodGet, "/api/v1/tenants/acme/tickets")[0].Query)
	assert.Equal(t, []string{"COW"}, changed["project"])
	assert.Equal(t, "2026-10-03T09:00:00Z", changed.Get("updated_after"))
}

// A long context is cut to the block's bound (docs/adr/0067 D7).
func TestStartKeepsTheBlockBounded(t *testing.T) {
	f := newFake(t)
	f.on("GET /api/v1/me/repositories/lookup", http.StatusOK, boundLookup())
	f.on("GET /api/v1/tenants/acme/projects/COW/tickets", http.StatusOK, list(ticket("acme/COW-12", "in-progress")))
	f.text("GET "+ticketPath+"/context", strings.Repeat("a long line of the ticket's body\n", 1000))
	block, _, err := Start(context.Background(), startSession(f, fakeWorkspace{remotes: origin}), StartOptions{})
	require.NoError(t, err)
	assert.Less(t, len(block), 10000)
	assert.Contains(t, block, "cut to fit the session start; get_ticket shows the whole ticket")
	assert.Len(t, f.calls(http.MethodGet, ticketPath+"/context"), 2, "a smaller context first, then the cut")
}

// Without a ticket in progress: the top of the backlog by rank.
func TestStartShowsCandidates(t *testing.T) {
	f := newFake(t)
	f.on("GET /api/v1/me/repositories/lookup", http.StatusOK, boundLookup())
	n := 0
	f.mux.HandleFunc("GET /api/v1/tenants/acme/projects/COW/tickets", func(w http.ResponseWriter, r *http.Request) {
		n++
		w.Header().Set("Content-Type", "application/json")
		if n == 1 {
			_, _ = w.Write([]byte(`{"items": [], "next_cursor": null}`))
			return
		}
		_, _ = w.Write([]byte(`{"items": [{"key": "acme/COW-3", "title": "Pick", "state": "decided", "effort": "M", "assignee": null}], "next_cursor": null}`))
	})
	block, _, err := Start(context.Background(), startSession(f, fakeWorkspace{remotes: origin}), StartOptions{})
	require.NoError(t, err)
	assert.Contains(t, block, "## No ticket of yours is in progress in acme/COW")
	assert.Contains(t, block, "1. acme/COW-3 — Pick (decided, M, unassigned)")
	calls := f.calls(http.MethodGet, "/api/v1/tenants/acme/projects/COW/tickets")
	q, _ := url.ParseQuery(calls[1].Query)
	assert.Equal(t, []string{"review", "decided", "analysed", "filed"}, q["state"])
	assert.Equal(t, []string{"me", "none"}, q["assignee"])
	assert.Equal(t, "false", q.Get("blocked"))
	assert.Equal(t, "5", q.Get("limit"))
}

// docs/adr/0066 D3: unbound with a proposal, the call to make on yes; several
// bindings as the data error they are; nothing to say without a remote.
func TestStartUnbound(t *testing.T) {
	f := newFake(t)
	f.on("GET /api/v1/me/repositories/lookup", http.StatusOK, map[string]any{"status": "unbound",
		"remotes":  []any{map[string]any{"remote": "git@github.com:acme/valkey-operator.git", "identity": "github.com/acme/valkey-operator"}},
		"bindings": []any{}, "proposal_unavailable": nil, "proposal": map[string]any{
			"identity": "github.com/acme/valkey-operator", "remote": "git@github.com:acme/valkey-operator.git", "name": "valkey-operator",
			"tenant": "acme", "reason": "only-tenant", "tenants": []any{map[string]any{"slug": "acme", "name": "Acme", "key": "VO"}}}})
	block, silent, err := Start(context.Background(), startSession(f, fakeWorkspace{remotes: []Remote{{Name: "origin",
		URL: "git@github.com:acme/valkey-operator.git"}}}), StartOptions{Record: true})
	require.NoError(t, err)
	assert.False(t, silent)
	assert.Contains(t, block, "# cowork: this repository is bound to no project")
	assert.Contains(t, block, "> Create the project `acme/VO` (\"valkey-operator\") for `github.com/acme/valkey-operator`?")
	assert.Contains(t, block, `create_project(tenant: "acme", key: "VO", name: "valkey-operator", remote: "git@github.com:acme/valkey-operator.git")`)

	g := newFake(t)
	g.on("GET /api/v1/me/repositories/lookup", http.StatusOK, map[string]any{"status": "ambiguous", "remotes": []any{},
		"bindings": []any{
			map[string]any{"tenant": map[string]any{"slug": "acme", "name": "Acme"}, "project": map[string]any{"key": "COW", "name": "c"},
				"identity": "github.com/acme/cowork", "path": "", "remote": "x", "archived": false},
			map[string]any{"tenant": map[string]any{"slug": "beta", "name": "Beta"}, "project": map[string]any{"key": "OPS", "name": "o"},
				"identity": "github.com/acme/cowork", "path": "", "remote": "x", "archived": false}},
		"proposal": nil, "proposal_unavailable": nil})
	block, _, err = Start(context.Background(), startSession(g, fakeWorkspace{remotes: origin}), StartOptions{})
	require.NoError(t, err)
	assert.Contains(t, block, "is bound to acme/COW and beta/OPS")
	assert.Contains(t, block, "docs/adr/0066 D6")

	h := newFake(t)
	block, silent, err = Start(context.Background(), startSession(h, fakeWorkspace{}), StartOptions{Record: true})
	require.NoError(t, err)
	assert.True(t, silent, "no remote and no binding file: the hook prints nothing (docs/adr/0067 D2)")
	assert.Empty(t, block)
	assert.Empty(t, h.calls(http.MethodGet, "/api/v1/me/repositories/lookup"))
}

// docs/adr/0066 D4: the file wins, and a disagreement with the server is
// reported; a file of another installation is ignored.
func TestStartWithABindingFile(t *testing.T) {
	f := newFake(t)
	f.on("GET /api/v1/me/repositories/lookup", http.StatusOK, boundLookup())
	f.on("GET /api/v1/tenants/acme/projects/UP", http.StatusOK, map[string]any{"key": "UP", "name": "Upstream"})
	f.on("GET /api/v1/tenants/acme/projects/UP/tickets", http.StatusOK, list())
	s := startSession(f, fakeWorkspace{remotes: origin, file: &BindingFile{Tenant: "acme", Project: "UP", File: "/r/.cowork.yaml"}})
	block, _, err := Start(context.Background(), s, StartOptions{})
	require.NoError(t, err)
	assert.Contains(t, block, "# cowork: acme/UP — Upstream")
	assert.Contains(t, block, "bound by .cowork.yaml")
	assert.Contains(t, block, "> Drift: the server binds github.com/acme/cowork to acme/COW, /r/.cowork.yaml binds acme/UP; the file wins")
	assert.Equal(t, "acme/UP", s.Binding().Key())

	other := startSession(f, fakeWorkspace{remotes: origin, file: &BindingFile{Tenant: "acme", Project: "UP", File: "/r/.cowork.yaml",
		URL: "https://elsewhere.example.com"}})
	_, err = Resolve(context.Background(), other)
	require.NoError(t, err)
	assert.Equal(t, "acme/COW", other.Binding().Key(), "a file of another installation is ignored")

	missing := newFake(t)
	missing.refuse("GET /api/v1/tenants/acme/projects/GONE", http.StatusNotFound, "not_found", "no such project")
	s = startSession(missing, fakeWorkspace{file: &BindingFile{Tenant: "acme", Project: "GONE", File: "/r/.cowork.yaml"}})
	block, _, err = Start(context.Background(), s, StartOptions{})
	require.NoError(t, err)
	assert.Contains(t, block, "a project that does not exist or that you cannot see")
	assert.Nil(t, s.Binding())
}

// create_project creates and binds in one call (docs/adr/0066 D3, D5) and
// binds the session.
func TestCreateProject(t *testing.T) {
	f := newFake(t)
	f.on("POST /api/v1/tenants/acme/projects", http.StatusCreated, map[string]any{"key": "VO", "name": "valkey-operator"})
	s := f.session(false)
	res := call(t, s, "create_project", `{"tenant": "acme", "key": "VO", "name": "valkey-operator", "remote": "git@github.com:acme/valkey-operator.git"}`)
	require.False(t, res.IsError, res.Text)
	assert.Contains(t, res.Text, "Created the project acme/VO (valkey-operator)")
	assert.Contains(t, res.Text, "tenant: acme\nproject: VO")
	body := decodeBody(t, f.calls(http.MethodPost, "/api/v1/tenants/acme/projects")[0])
	assert.Equal(t, map[string]any{"remote": "git@github.com:acme/valkey-operator.git"}, body["repository"])
	assert.NotEmpty(t, f.calls(http.MethodPost, "/api/v1/tenants/acme/projects")[0].Header.Get("Idempotency-Key"))
	assert.Equal(t, "acme/VO", s.Binding().Key())

	g := newFake(t)
	g.on("POST /api/v1/tenants/acme/projects", http.StatusOK, map[string]any{"key": "VO", "name": "valkey-operator"})
	res = call(t, g.session(false), "create_project", `{"tenant": "acme", "key": "VO2", "name": "x", "remote": "git@github.com:acme/valkey-operator.git"}`)
	require.False(t, res.IsError, res.Text)
	assert.Contains(t, res.Text, "bound already, to acme/VO")

	res = call(t, g.session(false), "create_project", `{"tenant": "acme", "key": "vo", "name": "x", "remote": "r"}`)
	assert.True(t, res.IsError, "the key's pattern")
}

// docs/adr/0067 D4: a hint when a ticket of the person is in progress, the
// repository shows work and nothing was recorded since the start; silent
// otherwise.
func TestRemind(t *testing.T) {
	setup := func(actor string) *Session {
		f := newFake(t)
		f.on("GET /api/v1/me/repositories/lookup", http.StatusOK, boundLookup())
		f.on("GET /api/v1/tenants/acme/projects/COW/tickets", http.StatusOK, list(ticket("acme/COW-12", "in-progress")))
		f.on("GET /api/v1/me", http.StatusOK, map[string]any{"id": adaID, "display_name": "Ada", "memberships": []any{}})
		f.on("GET "+ticketPath+"/activity", http.StatusOK, list(map[string]any{"id": "0199a3c2-1d2e-7f00-8000-0000000000e3",
			"at": "2026-10-04T08:30:00Z", "actor": map[string]any{"id": actor, "display_name": "x"}, "action": "commented",
			"entity_type": "comment", "redacted": false}))
		s := startSession(f, fakeWorkspace{remotes: origin, worked: true})
		require.NoError(t, s.Memory.SetLastStart(MemoryKey{Installation: s.Installation, Tenant: "acme", Project: "COW"},
			time.Date(2026, 10, 4, 8, 0, 0, 0, time.UTC)))
		return s
	}
	s := setup(samID)
	line, err := Remind(context.Background(), s)
	require.NoError(t, err)
	assert.Equal(t, "acme/COW-12 is still in progress and nothing was recorded on it since this session started "+
		"(2026-10-04 08:00 UTC) — record_state, or finish_work with a verification note?", line)

	s = setup(adaID)
	line, err = Remind(context.Background(), s)
	require.NoError(t, err)
	assert.Empty(t, line, "the person's own act since the start")

	s = setup(samID)
	s.Workspace = fakeWorkspace{remotes: origin, worked: false}
	line, err = Remind(context.Background(), s)
	require.NoError(t, err)
	assert.Empty(t, line, "a session that only read is not reminded")

	s = setup(samID)
	s.Memory = &InMemory{}
	line, err = Remind(context.Background(), s)
	require.NoError(t, err)
	assert.Empty(t, line, "no known start")
}

// A session in a repository is bound before its first tool call, once: a
// short key works without session_start, the binding is not looked up again,
// and a repository nobody bound still answers that the session is unbound
// (docs/adr/0067 D1).
func TestAToolCallBindsTheSessionOnce(t *testing.T) {
	f := newFake(t)
	f.on("GET /api/v1/me/repositories/lookup", http.StatusOK, boundLookup())
	f.on("POST "+ticketPath+"/comments", http.StatusCreated, map[string]any{"id": "0199a3c2-1d2e-7f00-8000-000000000003"})
	f.on("GET "+ticketPath, http.StatusOK, ticket("acme/COW-12", "in-progress"), "ETag", `"3"`)
	s := startSession(f, fakeWorkspace{remotes: origin})

	for range 2 {
		res := call(t, s, "comment", `{"key": "COW-12", "text": "Seen."}`)
		require.False(t, res.IsError, res.Text)
		assert.Contains(t, res.Text, "Commented on acme/COW-12")
	}
	assert.Len(t, f.calls(http.MethodGet, "/api/v1/me/repositories/lookup"), 1, "looked up once")
	require.NotNil(t, s.Binding())
	assert.Equal(t, "acme/COW", s.Binding().Key())

	g := newFake(t)
	g.on("GET /api/v1/me/repositories/lookup", http.StatusOK, map[string]any{"status": "unbound",
		"remotes": []any{map[string]any{"remote": origin[0].URL, "identity": "github.com/acme/cowork"}}, "bindings": []any{},
		"proposal": nil, "proposal_unavailable": "no tenant"})
	u := startSession(g, fakeWorkspace{remotes: origin})
	for range 2 {
		res := call(t, u, "comment", `{"key": "COW-12", "text": "Seen."}`)
		assert.True(t, res.IsError)
		assert.Contains(t, res.Text, "bound to no project")
	}
	assert.Len(t, g.calls(http.MethodGet, "/api/v1/me/repositories/lookup"), 1, "an unbound resolution is not repeated")

	h := newFake(t)
	h.on("POST "+ticketPath+"/comments", http.StatusCreated, map[string]any{"id": "0199a3c2-1d2e-7f00-8000-000000000004"})
	h.on("GET "+ticketPath, http.StatusOK, ticket("acme/COW-12", "in-progress"), "ETag", `"3"`)
	res := call(t, h.session(true), "comment", `{"key": "COW-12", "text": "Seen."}`)
	require.False(t, res.IsError, res.Text)
	assert.Empty(t, h.calls(http.MethodGet, "/api/v1/me/repositories/lookup"), "a session without a working directory looks nothing up")
}

// An act's line names who made it as the record does: the agent where an agent
// made it, the token where a person's act came through one, nobody else for
// the person's own browser session (docs/adr/0036 D6).
func TestActLineNamesTheAgentOrTheToken(t *testing.T) {
	act := func(fields string) apigen.Activity {
		var a apigen.Activity
		require.NoError(t, json.Unmarshal([]byte(`{"id":"0199a3c2-1d2e-7f00-8000-0000000000e9","at":"2026-10-04T07:00:00Z",
			"actor":{"id":"0199a3c2-1d2e-7f00-8000-000000000002","display_name":"Sam"},"actor_system":null,
			"action":"commented","entity_type":"comment","entity_id":null,"before":null,"after":null,"reason":null,
			"note":null,"explained_by_comment":null,"redacted":false,`+fields+`}`), &a))
		return a
	}
	token := `"token":{"id":"0199a3c2-1d2e-7f00-8000-000000000005","name":"ci-script"}`
	assert.Equal(t, "Sam via claude-code/opus/x commented", actLine(act(`"agent":"claude-code/opus/x",`+token)))
	assert.Equal(t, "Sam through the token ci-script commented", actLine(act(`"agent":null,`+token)))
	assert.Equal(t, "Sam through a token commented",
		actLine(act(`"agent":null,"token":{"id":"0199a3c2-1d2e-7f00-8000-000000000005","name":null}`)))
	assert.Equal(t, "Sam commented", actLine(act(`"agent":null,"token":null`)))
}
