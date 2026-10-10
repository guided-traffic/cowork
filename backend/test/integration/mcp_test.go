//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/oapi-codegen/nullable"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/mcpcli"
	"github.com/guided-traffic/cowork/backend/internal/tools"
	"github.com/guided-traffic/cowork/backend/test/fixture"
)

// mcpEnv is a repository on disk, an API, and the environment cowork-mcp runs
// with against it.
type mcpEnv struct {
	world
	tk     tokens
	s      apiServer
	ctx    context.Context
	repo   string
	remote string
	memory *tools.InMemory
}

func newMCPEnv(t *testing.T) mcpEnv {
	t.Helper()
	w := newWorld(t)
	e := mcpEnv{world: w, tk: issueTokens(t, w), s: newAPI(t), ctx: context.Background(), repo: t.TempDir(),
		memory: &tools.InMemory{}}
	e.remote = fmt.Sprintf("git@github.com:%s/valkey-operator.git", w.SlugA)
	e.git(t, "init", "--quiet")
	e.git(t, "remote", "add", "origin", e.remote)
	return e
}

// git runs git in the repository; the integration tier needs it as the
// hooks do.
func (e mcpEnv) git(t *testing.T, args ...string) {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", e.repo}, args...)...).CombinedOutput()
	require.NoError(t, err, "git %s: %s (the integration tier needs git)", strings.Join(args, " "), out)
}

func (e mcpEnv) env(token string) mcpcli.Env {
	vars := map[string]string{mcpcli.EnvURL: e.s.URL, mcpcli.EnvToken: token, mcpcli.EnvProjectDir: e.repo}
	return mcpcli.Env{
		Lookup: func(k string) (string, bool) { v, ok := vars[k]; return v, ok },
		Build:  mcpcli.Build{Version: "9.9.9-test", Commit: "test", Time: "0"}, Memory: e.memory,
	}
}

// run runs a subcommand as the binary would, by its own command line, and
// returns its exit code and standard output; standard error goes to the
// test's log.
func (e mcpEnv) run(t *testing.T, token, stdin string, args ...string) (int, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	env := e.env(token)
	env.Args, env.Stdout, env.Stderr = args, &stdout, &stderr
	if stdin != "" {
		env.Stdin = strings.NewReader(stdin)
	}
	code := mcpcli.Run(e.ctx, env)
	if stderr.Len() > 0 {
		t.Log(stderr.String())
	}
	return code, stdout.String()
}

// serve starts the MCP server on an in-memory transport and connects a
// client named as Claude Code names itself.
func (e mcpEnv) serve(t *testing.T, token string) *mcp.ClientSession {
	t.Helper()
	ctx, cancel := context.WithCancel(e.ctx)
	serverSide, clientSide := mcp.NewInMemoryTransports()
	env := e.env(token)
	env.Args, env.Transport = []string{"serve"}, serverSide
	env.Stdout, env.Stderr = &bytes.Buffer{}, &bytes.Buffer{}
	done := make(chan int, 1)
	go func() { done <- mcpcli.Run(ctx, env) }()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "claude-code", Version: "test"}, nil).Connect(ctx, clientSide, nil)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = cs.Close()
		cancel()
		<-done
	})
	return cs
}

func callTool(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	require.NoError(t, err)
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String(), res.IsError
}

func mustCall(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) string {
	t.Helper()
	text, isError := callTool(t, cs, name, args)
	require.False(t, isError, "%s: %s", name, text)
	return text
}

// docs/adr/0042 D6, docs/adr/0066 D3: the working day through the MCP server
// against the API — the proposal, the project created on the person's yes,
// a ticket filed, decided, worked, asked about, answered, ranked, finished —
// each act the agent's, with the client's name in the record.
func TestTheMCPServerRunsTheWorkingDay(t *testing.T) {
	e := newMCPEnv(t)
	cs := e.serve(t, e.tk.AgentA)
	api := e.s.client(t, caller{Token: e.tk.MemberA})

	listed, err := cs.ListTools(e.ctx, nil)
	require.NoError(t, err)
	assert.Len(t, listed.Tools, len(tools.Catalogue()))

	start := mustCall(t, cs, "session_start", nil)
	assert.Contains(t, start, "this repository is bound to no project")
	assert.Contains(t, start, fmt.Sprintf("`%s/VO`", e.SlugA), "the key from the repository's name, in the person's only team")
	assert.Contains(t, start, fmt.Sprintf("`create_project(team: %q, key: \"VO\"", e.SlugA))
	assert.NotContains(t, strings.ToLower(strings.ReplaceAll(start, e.SlugA, "")), "tenant", "the agent reads team (docs/adr/0005 D1)")

	created := mustCall(t, cs, "create_project", map[string]any{"team": e.SlugA, "key": "VO", "name": "valkey-operator", "remote": e.remote})
	assert.Contains(t, created, "Created the project "+e.SlugA+"/VO")
	// The argument's name before is still taken for one release, as team.
	again := mustCall(t, cs, "create_project", map[string]any{"tenant": e.SlugA, "key": "VO", "name": "valkey-operator", "remote": e.remote})
	assert.Contains(t, again, "The repository is bound already, to "+e.SlugA+"/VO")
	start = mustCall(t, cs, "session_start", nil)
	assert.Contains(t, start, "# cowork: "+e.SlugA+"/VO — valkey-operator")
	assert.Contains(t, start, "No ticket of yours is in progress")

	filed := mustCall(t, cs, "file_ticket", map[string]any{"type": "bug", "title": "Guard the failover gate", "severity": "high",
		"security": "none", "effort": "M", "body": "## Current state\n\nFound."})
	key := e.SlugA + "/VO-1"
	assert.Contains(t, filed, "Filed "+key)
	assert.Contains(t, filed, "`fix/VO-1-guard-failover-gate`")
	got, err := api.ResolveTicketWithResponse(e.ctx, e.SlugA, "VO-1")
	require.NoError(t, err)
	require.Equal(t, 200, got.StatusCode())
	_, err = api.UpdateTicketWithResponse(e.ctx, e.SlugA, "VO", 1, &apigen.UpdateTicketParams{IfMatch: ptr(`"1"`)},
		apigen.TicketPatch{Assignee: nullable.NewNullableWithValue(e.MemberA)})
	require.NoError(t, err)

	for _, to := range []string{"analysed", "decided", "in-progress"} {
		assert.Contains(t, mustCall(t, cs, "transition", map[string]any{"key": "VO-1", "to": to}), "to "+to)
	}
	assert.Contains(t, mustCall(t, cs, "record_state", map[string]any{"key": "VO-1", "body": "## Current state\n\nHalf done."}), "Recorded the current state of "+key)
	asked := mustCall(t, cs, "open_question", map[string]any{"key": "VO-1", "question": "Retry or fail?", "options": "- retry\n- fail",
		"recommendation": "retry", "asked_of": "me"})
	assert.Contains(t, asked, "Asked Q1 on "+key)
	assert.Contains(t, mustCall(t, cs, "record_answer", map[string]any{"key": "VO-1", "question": 1, "answer": "retry"}), "marked as recorded by the agent")
	commented := mustCall(t, cs, "comment", map[string]any{"key": "VO-1", "text": "Retrying now, @admin-a.", "mentions": []any{"admin-a"}})
	assert.Contains(t, commented, "mentioning admin-a", "a display name resolves through the member list")
	mustCall(t, cs, "set_progress", map[string]any{"key": "VO-1", "percent": 50})
	mustCall(t, cs, "watch", map[string]any{"key": key})
	ranked := mustCall(t, cs, "place_ticket", map[string]any{"key": "VO-1", "horizon": "now", "reason": "the failover gates the release"})
	assert.Contains(t, ranked, "Moved "+key+" from the horizon later to now")
	other := mustCall(t, cs, "file_ticket", map[string]any{"type": "task", "title": "Write the retry", "severity": "low",
		"security": "none", "effort": "S", "horizon": "now", "before": "VO-1",
		"links": []any{map[string]any{"type": "relates-to", "key": "VO-1"}}})
	assert.Contains(t, other, "Filed "+e.SlugA+"/VO-2 — Write the retry (task, filed), in the horizon now, directly before VO-1.")
	assert.Contains(t, other, "Linked: "+e.SlugA+"/VO-2 relates-to "+key)
	assert.Contains(t, mustCall(t, cs, "place_ticket", map[string]any{"key": "VO-1", "before": "VO-2"}), "Placed "+key+", directly before VO-2.")
	assert.Contains(t, mustCall(t, cs, "search", map[string]any{"query": "failover"}), key)

	start = mustCall(t, cs, "session_start", nil)
	assert.Contains(t, start, "## Active ticket: "+key+" — Guard the failover gate (in-progress)")
	assert.Contains(t, start, "<!-- cowork: context of "+key)
	assert.Contains(t, start, "**Answer:** retry")

	ticket := mustCall(t, cs, "get_ticket", map[string]any{"key": key})
	assert.Contains(t, ticket, "## Links\n\n- relates to "+e.SlugA+"/VO-2")
	assert.Contains(t, ticket, "> Retrying now, @admin-a.")
	// What an agent reads names the horizon by its word (docs/adr/0010 D1).
	whole := mustCall(t, cs, "get_ticket", map[string]any{"key": key, "activity": 50})
	assert.Contains(t, whole, "\nhorizon: now\n")
	assert.Contains(t, whole, `set the horizon to now — reason: "the failover gates the release"`)
	assert.NotContains(t, whole, "urgency")
	assert.NotContains(t, whole, "overridden")
	assert.Contains(t, mustCall(t, cs, "api", map[string]any{"method": "GET", "path": "/api/v1/me"}), "200 OK")

	finished := mustCall(t, cs, "finish_work", map[string]any{"key": "VO-1", "verification_note": "go test ./... passed against the fixture"})
	assert.Contains(t, finished, "(now done)")
	assert.Contains(t, finished, "docs/adr/0069 D5")
	got, err = api.ResolveTicketWithResponse(e.ctx, e.SlugA, "VO-1")
	require.NoError(t, err)
	assert.Equal(t, apigen.TicketStateDone, got.JSON200.State)
	assert.True(t, got.JSON200.DoneByHand)

	agents, err := fixtures(t).QueryCount(e.ctx, `SELECT count(*) FROM audit_events WHERE ticket_key = $1 AND actor_user_id = $2
		AND agent LIKE 'claude-code/unknown/%' AND action <> 'exported'`, key, e.MemberA)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, agents, int64(10), "every act is the person's, marked as the agent's (docs/adr/0036)")
	keyless, err := fixtures(t).QueryCount(e.ctx, `SELECT count(*) FROM audit_events WHERE ticket_key = $1 AND action IN
		('created', 'commented', 'asked') AND idempotency_key IS NULL`, key)
	require.NoError(t, err)
	assert.Zero(t, keyless, "every creating POST carried an Idempotency-Key (docs/adr/0045 D5)")
}

// docs/adr/0043 D6: an assisted token's limits are in the descriptions, and
// finish_work stops at review and says what remains for a person.
func TestTheMCPServerKnowsAnAssistedToken(t *testing.T) {
	e := newMCPEnv(t)
	cs := e.serve(t, e.tk.AssistedAgentA)
	listed, err := cs.ListTools(e.ctx, nil)
	require.NoError(t, err)
	for _, tool := range listed.Tools {
		if tool.Name == "transition" {
			assert.Contains(t, tool.Description, "lacks decide, close")
		}
	}
	member := e.s.client(t, caller{Token: e.tk.MemberA})
	created, err := member.CreateTicketWithResponse(e.ctx, e.SlugA, "ALPHA", &apigen.CreateTicketParams{}, task("Assisted work"))
	require.NoError(t, err)
	n := created.JSON201.Number
	for _, to := range []apigen.TicketState{apigen.TicketStateAnalysed, apigen.TicketStateDecided, apigen.TicketStateInProgress} {
		cur, err := member.GetTicketWithResponse(e.ctx, e.SlugA, "ALPHA", n)
		require.NoError(t, err)
		res, err := member.TransitionTicketWithResponse(e.ctx, e.SlugA, "ALPHA", n, &apigen.TransitionTicketParams{},
			apigen.Transition{From: cur.JSON200.State, To: to})
		require.NoError(t, err)
		require.Equal(t, 200, res.StatusCode(), string(res.Body))
	}
	key := fmt.Sprintf("%s/ALPHA-%d", e.SlugA, n)
	refused, isError := callTool(t, cs, "transition", map[string]any{"key": key, "to": "done", "reason_or_note": "ok"})
	assert.True(t, isError)
	assert.Contains(t, refused, "403 `agent_forbidden`: missing capability: close")
	finished := mustCall(t, cs, "finish_work", map[string]any{"key": key, "verification_note": "checked by hand"})
	assert.Contains(t, finished, "(now review)")
	assert.Contains(t, finished, "this agent lacks close, so done is the person's")
}

// docs/adr/0024 D7, docs/adr/0043 D3: an agent never deletes — not through the
// escape hatch with an administrator's admin token either —, and a deleted
// ticket answers the tools like a missing one.
func TestTheToolsNeverDeleteAndMissADeletedTicket(t *testing.T) {
	e := newMCPEnv(t)
	cs := e.serve(t, e.tk.AdminA)
	created, err := e.s.client(t, caller{Token: e.tk.MemberA}).CreateTicketWithResponse(e.ctx, e.SlugA, "ALPHA",
		&apigen.CreateTicketParams{}, task("Pasted into the wrong team"))
	require.NoError(t, err)
	n := created.JSON201.Number
	key := fmt.Sprintf("%s/ALPHA-%d", e.SlugA, n)
	// The team's path and its deprecated twin, which the server answers as
	// the team's for one release (docs/adr/0005 D1), under the same rules.
	for _, family := range []string{"teams", "tenants"} {
		path := fmt.Sprintf("/api/v1/%s/%s/projects/ALPHA/tickets/%d", family, e.SlugA, n)
		refused, isError := callTool(t, cs, "api", map[string]any{"method": "DELETE", "path": path})
		assert.True(t, isError, path)
		assert.Contains(t, refused, "403", path)
		assert.Contains(t, refused, "hard-off: deleting, restoring or purging", path)
	}
	assert.Contains(t, mustCall(t, cs, "get_ticket", map[string]any{"key": key}), "Pasted into the wrong team", "nothing was deleted")

	res, err := e.s.client(t, caller{Token: e.tk.AdminA}).DeleteTicketWithResponse(e.ctx, e.SlugA, "ALPHA", n)
	require.NoError(t, err)
	require.Equal(t, 204, res.StatusCode(), string(res.Body))
	missing, isError := callTool(t, cs, "get_ticket", map[string]any{"key": key})
	assert.True(t, isError)
	assert.Contains(t, missing, "404")
	assert.NotContains(t, missing, "Pasted into the wrong team")
	assert.NotContains(t, mustCall(t, cs, "search", map[string]any{"query": "wrong team"}), key)
	inBin := fmt.Sprintf("/api/v1/teams/%s/deleted-tickets/ALPHA-%d", e.SlugA, n)
	answer, isError := callTool(t, cs, "api", map[string]any{"method": "PUT", "path": inBin + "/restore"})
	assert.True(t, isError)
	assert.Contains(t, answer, "hard-off: deleting, restoring or purging", "an agent restores nothing")
	answer, isError = callTool(t, cs, "api", map[string]any{"method": "DELETE", "path": inBin})
	assert.True(t, isError)
	assert.Contains(t, answer, "session_required", "nor purges: a token never does (docs/adr/0024 D7)")
}

// docs/adr/0067, docs/adr/0070 D6: the hook modes and the subcommands run
// against the fixture environment by their command line.
func TestTheSubcommands(t *testing.T) {
	e := newMCPEnv(t)
	token := e.tk.AgentA
	code, stdout := e.run(t, token, `{"session_id":"s1","source":"startup"}`, "session-context")
	assert.Equal(t, 0, code)
	assert.Contains(t, stdout, "this repository is bound to no project", "the proposal, before the first prompt")

	code, stdout = e.run(t, token, "", "lookup", "--json")
	assert.Equal(t, 0, code)
	var report map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &report))
	assert.Equal(t, false, report["bound"])

	admin := e.s.client(t, caller{Token: e.tk.AdminAWrite})
	bound, err := admin.BindRepositoryWithResponse(e.ctx, e.SlugA, "ALPHA", &apigen.BindRepositoryParams{},
		apigen.RepositoryBind{Remote: "https://github.com/" + e.SlugA + "/valkey-operator"})
	require.NoError(t, err)
	require.Equal(t, 201, bound.StatusCode(), string(bound.Body))
	code, stdout = e.run(t, token, "", "lookup")
	assert.Equal(t, 0, code)
	assert.Contains(t, stdout, "Bound to "+e.SlugA+"/ALPHA (Alpha) by the remote github.com/"+e.SlugA+"/valkey-operator.")
	// lookup --json names the team under Team and, for one release, under
	// Tenant, the key it printed before (docs/adr/0005 D1).
	code, stdout = e.run(t, token, "", "lookup", "--json")
	assert.Equal(t, 0, code)
	var boundReport struct {
		Binding map[string]any `json:"binding"`
		Lookup  struct {
			Bindings []map[string]any `json:"bindings"`
		} `json:"lookup"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &boundReport), stdout)
	assert.Equal(t, e.SlugA, boundReport.Binding["Team"])
	assert.Equal(t, e.SlugA, boundReport.Binding["Tenant"])
	require.Len(t, boundReport.Lookup.Bindings, 1)
	assert.Equal(t, boundReport.Lookup.Bindings[0]["team"], boundReport.Lookup.Bindings[0]["tenant"])

	member := e.s.client(t, caller{Token: e.tk.MemberA})
	created, err := member.CreateTicketWithResponse(e.ctx, e.SlugA, "ALPHA", &apigen.CreateTicketParams{}, task("Hooked", func(b *apigen.TicketCreate) {
		b.Assignee = &e.MemberA
	}))
	require.NoError(t, err)
	n := created.JSON201.Number
	for _, to := range []apigen.TicketState{apigen.TicketStateAnalysed, apigen.TicketStateDecided, apigen.TicketStateInProgress} {
		cur, err := member.GetTicketWithResponse(e.ctx, e.SlugA, "ALPHA", n)
		require.NoError(t, err)
		_, err = member.TransitionTicketWithResponse(e.ctx, e.SlugA, "ALPHA", n, &apigen.TransitionTicketParams{},
			apigen.Transition{From: cur.JSON200.State, To: to})
		require.NoError(t, err)
	}
	key := fmt.Sprintf("%s/ALPHA-%d", e.SlugA, n)

	code, stdout = e.run(t, token, `{"session_id":"s2","source":"startup","model":"claude-opus-5"}`, "session-context")
	assert.Equal(t, 0, code)
	assert.Contains(t, stdout, "## Active ticket: "+key+" — Hooked (in-progress)")
	assert.Less(t, len(stdout), 10000, "within what a hook may hand Claude Code (docs/adr/0067 D7)")
	marked, err := fixtures(t).QueryCount(e.ctx, `SELECT count(*) FROM audit_events WHERE ticket_key = $1 AND action = 'exported'
		AND agent = 'claude-code/claude-opus-5/s2'`, key)
	require.NoError(t, err)
	assert.EqualValues(t, 1, marked, "the hook's requests carry the session's model and id")

	code, stdout = e.run(t, token, `{"session_id":"s2"}`, "session-end")
	assert.Equal(t, 0, code)
	assert.Empty(t, stdout, "nothing was worked on: no reminder")
	time.Sleep(1100 * time.Millisecond)
	require.NoError(t, os.WriteFile(filepath.Join(e.repo, "change.txt"), []byte("work"), 0o600))
	code, stdout = e.run(t, token, `{"session_id":"s2"}`, "session-end")
	assert.Equal(t, 0, code)
	var stop map[string]string
	require.NoError(t, json.Unmarshal([]byte(stdout), &stop), stdout)
	assert.Contains(t, stop["systemMessage"], key+" is still in progress and nothing was recorded on it")

	_, err = member.AddCommentWithResponse(e.ctx, e.SlugA, "ALPHA", n, &apigen.AddCommentParams{}, apigen.CommentWrite{Body: "recorded"})
	require.NoError(t, err)
	code, stdout = e.run(t, token, `{"session_id":"s2"}`, "session-end")
	assert.Equal(t, 0, code)
	assert.Empty(t, stdout, "the person recorded something since the start")

	code, stdout = e.run(t, token, "", "token", "check")
	assert.Equal(t, 0, code, stdout)
	assert.Contains(t, stdout, "The token works against "+e.s.URL+" (cowork 9.9.9-test).")
	assert.Contains(t, stdout, "scope write, an agent token")

	revoked, _, err := fixtures(t).Token(e.ctx, fixture.TokenSpec{UserID: e.MemberA, Agent: true, Revoked: true})
	require.NoError(t, err)
	code, stdout = e.run(t, revoked, "", "token", "check")
	assert.Equal(t, 1, code)
	assert.Contains(t, stdout, "token_revoked")
	code, stdout = e.run(t, revoked, `{"session_id":"s3"}`, "session-context")
	assert.Equal(t, 0, code, "a hook never fails the session")
	assert.Contains(t, stdout, "make a new one on "+e.s.URL+"/me/tokens")
	assert.NotContains(t, stdout, revoked)

	// token check names the restriction's team, and --json under team and,
	// for one release, under tenant, the key it printed before
	// (docs/adr/0005 D1).
	restricted, _, err := fixtures(t).Token(e.ctx, fixture.TokenSpec{UserID: e.MemberA, Agent: true, TenantID: e.A})
	require.NoError(t, err)
	code, stdout = e.run(t, restricted, "", "token", "check")
	assert.Equal(t, 0, code, stdout)
	assert.Contains(t, stdout, "Restriction:   the team "+e.SlugA)
	code, stdout = e.run(t, restricted, "", "token", "check", "--json")
	assert.Equal(t, 0, code, stdout)
	var tokenReport map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &tokenReport), stdout)
	assert.Equal(t, e.SlugA, tokenReport["team"])
	assert.Equal(t, e.SlugA, tokenReport["tenant"])
}

// docs/adr/0070 D6: the subcommands by running the binary, as a person and
// the hooks run it — main's own wiring, the environment and the memory file
// under the user's cache directory included.
func TestTheBinaryRunsItsSubcommands(t *testing.T) {
	e := newMCPEnv(t)
	bin := filepath.Join(t.TempDir(), "cowork-mcp")
	out, err := exec.Command("go", "build", "-o", bin, "github.com/guided-traffic/cowork/backend/cmd/cowork-mcp").CombinedOutput()
	require.NoError(t, err, "go build: %s", out)
	home := t.TempDir()
	run := func(env map[string]string, stdin string, args ...string) (int, string, string) {
		t.Helper()
		cmd := exec.Command(bin, args...)
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "XDG_CACHE_HOME=" + filepath.Join(home, ".cache")}
		for k, v := range env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
		cmd.Stdin = strings.NewReader(stdin)
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		code := 0
		if err := cmd.Run(); err != nil {
			var exit *exec.ExitError
			require.True(t, errors.As(err, &exit), "run %s: %v", strings.Join(args, " "), err)
			code = exit.ExitCode()
		}
		return code, stdout.String(), stderr.String()
	}
	configured := map[string]string{mcpcli.EnvURL: e.s.URL, mcpcli.EnvToken: e.tk.AgentA, mcpcli.EnvProjectDir: e.repo}

	code, stdout, _ := run(nil, "", "version")
	assert.Equal(t, 0, code)
	assert.True(t, strings.HasPrefix(stdout, "cowork-mcp dev (commit unknown"), stdout)
	code, _, stderr := run(nil, "")
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr, "Usage: cowork-mcp <command>")
	code, _, stderr = run(nil, "", "serve")
	assert.Equal(t, 1, code, "serve without its configuration ends at once")
	assert.Contains(t, stderr, "COWORK_URL is not set")
	code, stdout, _ = run(nil, `{"session_id":"b0","source":"startup"}`, "session-context")
	assert.Equal(t, 0, code)
	assert.Empty(t, stdout, "a hook without its configuration is silent")

	code, stdout, _ = run(configured, "", "token", "check")
	assert.Equal(t, 0, code, stdout)
	assert.Contains(t, stdout, "The token works against "+e.s.URL)
	code, stdout, _ = run(configured, "", "lookup", "--json")
	assert.Equal(t, 0, code, stdout)
	var report map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &report))
	assert.Equal(t, false, report["bound"])
	code, stdout, _ = run(configured, `{"session_id":"b1","source":"startup"}`, "session-context")
	assert.Equal(t, 0, code)
	assert.Contains(t, stdout, "this repository is bound to no project")

	admin := e.s.client(t, caller{Token: e.tk.AdminAWrite})
	bound, err := admin.BindRepositoryWithResponse(e.ctx, e.SlugA, "ALPHA", &apigen.BindRepositoryParams{}, apigen.RepositoryBind{Remote: e.remote})
	require.NoError(t, err)
	require.Equal(t, 201, bound.StatusCode(), string(bound.Body))
	code, stdout, _ = run(configured, `{"session_id":"b2","source":"startup"}`, "session-context")
	assert.Equal(t, 0, code)
	assert.Contains(t, stdout, e.SlugA+"/ALPHA")
	var memory []string
	require.NoError(t, filepath.WalkDir(home, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(path, "_"+e.SlugA+"_ALPHA.json") {
			memory = append(memory, path)
		}
		return err
	}))
	assert.Len(t, memory, 1, "the session start is remembered in one file under the cache directory")

	code, stdout, stderr = run(configured, `{"session_id":"b2","hook_event_name":"PostModelSwitch","from_model":"claude-opus-5",`+
		`"to_model":"claude-sonnet-5","source":"command"}`, "model-switch")
	assert.Equal(t, 0, code)
	assert.Empty(t, stdout+stderr, "the switch hook prints nothing, which Claude Code would add to the model's context")
	var recorded struct {
		ProjectDir string `json:"project_dir"`
		Model      string `json:"model"`
	}
	require.NoError(t, filepath.WalkDir(home, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasPrefix(d.Name(), "model-") {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return json.Unmarshal(raw, &recorded)
	}))
	assert.Equal(t, e.repo, recorded.ProjectDir)
	assert.Equal(t, "claude-sonnet-5", recorded.Model, "the switch is in the file the server of the project directory reads")

	code, stdout, _ = run(configured, `{"session_id":"b2"}`, "session-end")
	assert.Equal(t, 0, code)
	assert.Empty(t, stdout, "nothing in progress: no reminder")

	// docs/adr/0070 D2, D5: the export by the binary, into a new directory.
	target := filepath.Join(t.TempDir(), "backup")
	code, stdout, stderr = run(configured, "", "export", e.SlugA+"/ALPHA", target)
	assert.Equal(t, 0, code, stderr)
	assert.Contains(t, stdout, "Exported "+e.SlugA+"/ALPHA into "+target)
	for _, name := range []string{"manifest.json", "links.json", "attachments.json"} {
		_, err := os.Stat(filepath.Join(target, name))
		assert.NoError(t, err, name)
	}
	code, _, stderr = run(configured, "", "export", e.SlugA+"/ALPHA", target)
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "is not empty: an export never overwrites")

	// docs/adr/0070 D2: the import's dry run by the binary, the agent token's.
	code, stdout, stderr = run(configured, "", "import", "--dry-run", e.SlugA+"/ALPHA", target)
	assert.Equal(t, 0, code, stderr)
	assert.Contains(t, stdout, "Dry run into "+e.SlugA+"/ALPHA")
}

// docs/adr/0070 D2, D6, docs/adr/0051 D2, D6: the import subcommand by its
// command line, an agent's — a directory of a repository's ticket files
// packed, the dry run's report, nothing imported with --dry-run; the
// execution, its report, every ticket marked as the binary's act; the same
// files again, each left out as a conflict.
func TestTheImportSubcommand(t *testing.T) {
	e := newMCPEnv(t)
	dir := filepath.Join(e.repo, "docs", "tickets")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "archive"), 0o755))
	front := "---\ntitle: %s\nstate: %s\nseverity: low\nsecurity: none\neffort: S\nopened: 2026-10-01\n%s---\n\n## Current state\n\nText.\n"
	for name, body := range map[string]string{
		"001-the-parent.md":           fmt.Sprintf(front, "the parent", "filed", ""),
		"archive/002-a-done-child.md": fmt.Sprintf(front, "a done child", "done", "shipped: built and run\n"),
		"README.md":                   "# Tickets\n",
	} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, filepath.FromSlash(name)), []byte(body), 0o600))
	}

	code, stdout := e.run(t, e.tk.MemberA, "", "import", e.SlugA+"/ALPHA", dir, "--dry-run")
	require.Equal(t, 0, code, stdout)
	assert.Contains(t, stdout, "2 tickets to create (1 open, 0 confidential)")
	assert.Contains(t, stdout, "tickets/001-the-parent.md: create "+e.SlugA+"/ALPHA-1 \"the parent\" (task, filed)")
	assert.Contains(t, stdout, "tickets/README.md: skip\n  why: not a ticket file")
	none := e.s.do(t, caller{Token: e.tk.MemberA}, "GET", ticketPath(e.SlugA, "ALPHA", 1), nil)
	assert.Equal(t, 404, none.StatusCode, "a dry run imports nothing")

	code, stdout = e.run(t, e.tk.MemberA, "", "import", e.SlugA+"/ALPHA", dir)
	require.Equal(t, 0, code, stdout)
	assert.Contains(t, stdout, "Imported into "+e.SlugA+"/ALPHA")
	assert.Contains(t, stdout, "2 tickets created (1 open, 0 confidential)")
	assert.Contains(t, stdout, "tickets/archive/002-a-done-child.md: created "+e.SlugA+"/ALPHA-2")
	assert.Contains(t, stdout, "The importer sets no parent a file does not name")
	n, err := fixtures(t).QueryCount(e.ctx, `SELECT count(*) FROM audit_events WHERE tenant_id = $1 AND action = 'created'
		AND entity_type = 'ticket' AND after ? 'import_job' AND agent LIKE 'cowork-mcp/%/import'`, e.A)
	require.NoError(t, err)
	assert.EqualValues(t, 2, n, "every ticket is the binary's act, an agent's")

	code, stdout = e.run(t, e.tk.MemberA, "", "import", e.SlugA+"/ALPHA", dir)
	require.Equal(t, 0, code, stdout)
	assert.Contains(t, stdout, "0 tickets created")
	assert.Contains(t, stdout, "tickets/001-the-parent.md: conflict "+e.SlugA+"/ALPHA-1")
	assert.Contains(t, stdout, "why: left out of the import: the project holds its number as "+e.SlugA+"/ALPHA-1")
	code, _ = e.run(t, e.tk.ViewerA, "", "import", e.SlugA+"/ALPHA", dir)
	assert.Equal(t, 1, code, "a viewer imports nothing")
}

// docs/adr/0070 D2, D5, D6: the export subcommand by its command line — the
// project's documents named by key and the manifests, into an empty or a new
// directory; a directory that is not empty, a project the token cannot read
// and a project that is none refused, each with its exit code.
func TestTheExportSubcommand(t *testing.T) {
	e := newMCPEnv(t)
	member := e.s.client(t, caller{Token: e.tk.MemberA})
	created, err := member.CreateTicketWithResponse(e.ctx, e.SlugA, "ALPHA", &apigen.CreateTicketParams{},
		task("Exported", func(b *apigen.TicketCreate) { b.Body = ptr("## Current state\n\nWritten.") }))
	require.NoError(t, err)
	require.Equal(t, 201, created.StatusCode(), string(created.Body))

	target := t.TempDir()
	code, stdout := e.run(t, e.tk.MemberA, "", "export", e.SlugA+"/ALPHA", target)
	assert.Equal(t, 0, code)
	assert.Contains(t, stdout, "1 tickets as Markdown")
	doc, err := os.ReadFile(filepath.Join(target, e.SlugA, "ALPHA-1.md"))
	require.NoError(t, err)
	assert.Contains(t, string(doc), "key: "+e.SlugA+"/ALPHA-1\ntitle: Exported\n")
	manifest, err := os.ReadFile(filepath.Join(target, "manifest.json"))
	require.NoError(t, err)
	var m apigen.ExportManifest
	require.NoError(t, json.Unmarshal(manifest, &m))
	assert.Equal(t, 1, m.Tickets)
	n, err := fixtures(t).QueryCount(e.ctx, `SELECT count(*) FROM audit_events WHERE tenant_id = $1 AND action = 'exported'
		AND entity_type = 'project' AND agent LIKE 'cowork-mcp/%/export'`, e.A)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n, "the export is recorded, marked as the binary's (docs/adr/0059 D3)")

	code, _ = e.run(t, e.tk.MemberA, "", "export", e.SlugA+"/ALPHA", target)
	assert.Equal(t, 1, code, "never into a directory that is not empty")
	code, _ = e.run(t, e.tk.MemberB, "", "export", e.SlugA+"/ALPHA", filepath.Join(t.TempDir(), "other"))
	assert.Equal(t, 1, code, "another team's project is not_found")
	code, _ = e.run(t, e.tk.MemberA, "", "export", "ALPHA", filepath.Join(t.TempDir(), "usage"))
	assert.Equal(t, 2, code)
}
