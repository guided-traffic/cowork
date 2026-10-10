//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/test/fixture"
)

// sender sends one request as one credential — a token, with or without the
// agent header, or a browser session — with headers as name, value pairs.
type sender func(method, path string, body any, headers ...string) *http.Response

func (e ticketEnv) asToken(t *testing.T, c caller) sender {
	return func(method, path string, body any, headers ...string) *http.Response {
		t.Helper()
		return e.s.do(t, c, method, path, body, headers...)
	}
}

func asBrowser(b *browser) sender {
	return func(method, path string, body any, headers ...string) *http.Response {
		b.t.Helper()
		opts := make([]reqOpt, 0, len(headers)/2)
		for i := 0; i+1 < len(headers); i += 2 {
			opts = append(opts, withHeader(headers[i], headers[i+1]))
		}
		return b.request(method, path, body, opts...)
	}
}

// actMark is how an act was made, as its rows and answers must say: the agent
// mark, empty for a person's own act, and the token, uuid.Nil for a browser
// session.
type actMark struct {
	agent     string
	token     uuid.UUID
	tokenName string
}

func present[T any](v nullable.Nullable[T]) bool { return v.IsSpecified() && !v.IsNull() }

// assertOn holds one answer's agent mark and token to the act's.
func (m actMark) assertOn(t *testing.T, what string, agent nullable.Nullable[string], token nullable.Nullable[apigen.TokenMark]) {
	t.Helper()
	if m.agent == "" {
		assert.False(t, present(agent), "%s: no agent mark", what)
	} else if assert.True(t, present(agent), "%s: the agent mark", what) {
		assert.Equal(t, m.agent, agent.MustGet(), "%s: the agent mark", what)
	}
	if m.token == uuid.Nil {
		assert.False(t, present(token), "%s: no token", what)
		return
	}
	if !assert.True(t, present(token), "%s: the token", what) {
		return
	}
	got := token.MustGet()
	assert.Equal(t, m.token, got.Id, "%s: the token's id", what)
	if assert.True(t, present(got.Name), "%s: the token's name", what) {
		assert.Equal(t, m.tokenName, got.Name.MustGet(), "%s: the token's name", what)
	}
}

func ifMatch(version int) []string { return []string{"If-Match", strconv.Quote(strconv.Itoa(version))} }

func keyed() []string { return []string{"Idempotency-Key", uuid.NewString()} }

// pngUpload is a multipart body with one PNG file, and its content type.
func pngUpload(t *testing.T) (string, string) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", `form-data; name="file"; filename="shot.png"`)
	h.Set("Content-Type", "image/png")
	part, err := mw.CreatePart(h)
	require.NoError(t, err)
	_, err = part.Write(pngBytes)
	require.NoError(t, err)
	require.NoError(t, mw.Close())
	return body.String(), mw.FormDataContentType()
}

// markedActs files a ticket as send and makes on it every act a ticket shows
// as somebody's — the filing, a comment and its edit, a file, a question and its
// answer, a stake, a booking and its correction (never an agent's,
// docs/adr/0043 D3), a changed field — and holds every answer, the revisions,
// the ticket's activity and its context document to m.
func (e ticketEnv) markedActs(t *testing.T, send sender, m actMark) apigen.Ticket {
	t.Helper()
	tickets := e.projectTickets("ALPHA")
	res := send(http.MethodPost, tickets, task("Acts of "+m.tokenName+m.agent), keyed()...)
	require.Equal(t, http.StatusCreated, res.StatusCode)
	tk := decode[apigen.Ticket](t, res)
	m.assertOn(t, "the filing", tk.ReporterAgent, tk.ReporterToken)
	path := fmt.Sprintf("%s/%d", tickets, tk.Number)

	res = send(http.MethodPost, path+"/comments", map[string]any{"body": "Found it."}, keyed()...)
	require.Equal(t, http.StatusCreated, res.StatusCode)
	cm := decode[apigen.Comment](t, res)
	m.assertOn(t, "the comment", cm.Agent, cm.Token)
	res = send(http.MethodPatch, e.commentPath(tk, cm.Id), map[string]any{"body": "Found it, twice."}, ifMatch(cm.Version)...)
	require.Equal(t, http.StatusOK, res.StatusCode)
	edited := decode[apigen.Comment](t, res)
	m.assertOn(t, "the edited comment", edited.Agent, edited.Token)
	res = send(http.MethodGet, e.commentPath(tk, cm.Id)+"/revisions", nil)
	require.Equal(t, http.StatusOK, res.StatusCode)
	revisions := decode[apigen.CommentRevisionList](t, res)
	require.Len(t, revisions.Items, 1)
	m.assertOn(t, "the comment's revision", revisions.Items[0].Agent, revisions.Items[0].Token)

	body, contentType := pngUpload(t)
	res = send(http.MethodPost, path+"/attachments", body, append(keyed(), "Content-Type", contentType)...)
	require.Equal(t, http.StatusCreated, res.StatusCode)
	file := decode[apigen.Attachment](t, res)
	m.assertOn(t, "the file", file.Agent, file.Token)

	res = send(http.MethodPost, path+"/questions", map[string]any{"question": "Ship it?"}, keyed()...)
	require.Equal(t, http.StatusCreated, res.StatusCode)
	q := decode[apigen.Question](t, res)
	m.assertOn(t, "the question", q.AskedByAgent, q.AskedByToken)
	assert.False(t, present(q.AnsweredByToken), "an open question has no answer's token")
	res = send(http.MethodPut, fmt.Sprintf("%s/questions/%d/answer", path, q.Number), map[string]any{"answer": "Yes."})
	require.Equal(t, http.StatusOK, res.StatusCode)
	answered := decode[apigen.Question](t, res)
	m.assertOn(t, "the question, answered", answered.AskedByAgent, answered.AskedByToken)
	// The answer's agent is a flag, recorded_by_agent, not a mark.
	assert.Equal(t, m.agent != "", answered.RecordedByAgent, "the answer recorded by an agent")
	actMark{token: m.token, tokenName: m.tokenName}.assertOn(t, "the answer", nullable.NewNullNullable[string](),
		answered.AnsweredByToken)

	res = send(http.MethodPut, path+"/interest", map[string]any{"weight": "need", "note": "for the release"})
	require.Equal(t, http.StatusCreated, res.StatusCode)
	stake := decode[apigen.Interest](t, res)
	m.assertOn(t, "the stake", stake.Agent, stake.Token)
	res = send(http.MethodGet, path+"/interest", nil)
	require.Equal(t, http.StatusOK, res.StatusCode)
	stakes := decode[apigen.InterestList](t, res)
	require.Len(t, stakes.Items, 1)
	m.assertOn(t, "the stake as listed", stakes.Items[0].Agent, stakes.Items[0].Token)

	if m.agent == "" {
		res = send(http.MethodPost, path+"/time-entries", map[string]any{"minutes": 30, "day": "2026-10-01", "note": "work"}, keyed()...)
		require.Equal(t, http.StatusCreated, res.StatusCode)
		entry := decode[apigen.TimeEntry](t, res)
		m.assertOn(t, "the time entry", nullable.NewNullNullable[string](), entry.Token)
		res = send(http.MethodPatch, e.entryPath(tk, entry.Id), map[string]any{"minutes": 45}, ifMatch(entry.Version)...)
		require.Equal(t, http.StatusOK, res.StatusCode)
		res = send(http.MethodGet, e.entryPath(tk, entry.Id)+"/revisions", nil)
		require.Equal(t, http.StatusOK, res.StatusCode)
		corrections := decode[apigen.TimeEntryRevisionList](t, res)
		require.Len(t, corrections.Items, 1)
		m.assertOn(t, "the time entry's revision", nullable.NewNullNullable[string](), corrections.Items[0].Token)
	}

	res = send(http.MethodPatch, path, map[string]any{"title": "Renamed by " + m.tokenName + m.agent}, ifMatch(tk.Version)...)
	require.Equal(t, http.StatusOK, res.StatusCode)
	renamed := decode[apigen.Ticket](t, res)
	m.assertOn(t, "the filing, after a change", renamed.ReporterAgent, renamed.ReporterToken)

	res = send(http.MethodGet, path+"/activity", nil)
	require.Equal(t, http.StatusOK, res.StatusCode)
	activity := decode[apigen.ActivityList](t, res)
	actions := map[apigen.AuditAction]bool{}
	for _, a := range activity.Items {
		actions[a.Action] = true
		m.assertOn(t, "the act "+string(a.Action), a.Agent, a.Token)
	}
	for _, want := range []apigen.AuditAction{apigen.AuditActionCreated, apigen.AuditActionCommented, apigen.AuditActionEdited,
		apigen.AuditActionUploaded, apigen.AuditActionAsked, apigen.AuditActionAnswered, apigen.AuditActionInterest,
		apigen.AuditActionUpdated} {
		assert.True(t, actions[want], "the activity shows the act %s", want)
	}

	// The context document names a person's act through a token where it
	// names an agent's by the agent (docs/adr/0044 D2): its own first line, a
	// comment and an act.
	res = send(http.MethodGet, path+"/context?comments=10&activity=50", nil)
	require.Equal(t, http.StatusOK, res.StatusCode)
	raw, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	doc, by := string(raw), cm.Author.DisplayName
	switch {
	case m.agent != "":
		assert.Contains(t, doc, " by "+by+" (via "+m.agent+") — not an import format")
		assert.Contains(t, doc, "**"+by+"** via "+m.agent+", ")
		assert.Contains(t, doc, " — "+by+" via "+m.agent+" — commented")
	case m.token != uuid.Nil:
		assert.Contains(t, doc, " by "+by+" (through the token "+m.tokenName+") — not an import format")
		assert.Contains(t, doc, "**"+by+"** through the token "+m.tokenName+", ")
		assert.Contains(t, doc, " — "+by+" through the token "+m.tokenName+" — commented")
	default:
		assert.Contains(t, doc, " by "+by+" — not an import format")
		assert.NotContains(t, doc, " through ")
		assert.NotContains(t, doc, " via ")
	}
	return tk
}

// assertRows holds every row of the ticket's acts to m, in the database: the
// ticket's filing, its stake, the audit rows, the comments and their
// revisions, the files, the questions, the time entries and their revisions.
// A table the acts left no row in is no pass.
func assertRows(t *testing.T, ticket uuid.UUID, m actMark) {
	t.Helper()
	ctx := context.Background()
	var token, name, agent any
	if m.token != uuid.Nil {
		token, name = m.token, m.tokenName
	}
	if m.agent != "" {
		agent = m.agent
	}
	const marks = `x.token_id IS DISTINCT FROM want.token_id OR x.token_name IS DISTINCT FROM want.token_name`
	f := fixtures(t)
	for _, c := range []struct{ table, rows, wrong string }{
		{"tickets", `tickets x WHERE x.id = $1`,
			`x.reporter_token_id IS DISTINCT FROM want.token_id OR x.reporter_token_name IS DISTINCT FROM want.token_name
			 OR x.reporter_agent IS DISTINCT FROM want.agent`},
		{"ticket_interest", `ticket_interest x WHERE x.ticket_id = $1`, marks + ` OR x.agent IS DISTINCT FROM want.agent`},
		{"audit_events", `audit_events x WHERE x.ticket_id = $1`, marks + ` OR x.agent IS DISTINCT FROM want.agent`},
		{"comments", `comments x WHERE x.ticket_id = $1`, marks + ` OR x.agent IS DISTINCT FROM want.agent`},
		{"comment_revisions", `comment_revisions x JOIN comments c ON c.id = x.comment_id WHERE c.ticket_id = $1`,
			marks + ` OR x.agent IS DISTINCT FROM want.agent`},
		{"attachments", `attachments x WHERE x.ticket_id = $1`, marks + ` OR x.agent IS DISTINCT FROM want.agent`},
		{"questions", `questions x WHERE x.ticket_id = $1`,
			`x.asked_by_token_id IS DISTINCT FROM want.token_id OR x.asked_by_token_name IS DISTINCT FROM want.token_name
			 OR x.answered_by_token_id IS DISTINCT FROM want.token_id OR x.answered_by_token_name IS DISTINCT FROM want.token_name
			 OR x.asked_by_agent IS DISTINCT FROM want.agent OR x.recorded_by_agent <> (want.agent IS NOT NULL)`},
		{"time_entries", `time_entries x WHERE x.ticket_id = $1`, marks},
		{"time_entry_revisions", `time_entry_revisions x JOIN time_entries e ON e.id = x.entry_id WHERE e.ticket_id = $1`, marks},
	} {
		if m.agent != "" && (c.table == "time_entries" || c.table == "time_entry_revisions") {
			continue // no agent books time
		}
		total, err := f.QueryCount(ctx, `SELECT count(*) FROM `+c.rows, ticket)
		require.NoError(t, err)
		assert.NotZero(t, total, "%s: the acts left rows", c.table)
		wrong, err := f.QueryCount(ctx, `SELECT count(*)
			FROM (SELECT $2::uuid AS token_id, $3::text AS token_name, $4::text AS agent) want, `+c.rows+` AND (`+c.wrong+`)`,
			ticket, token, name, agent)
		require.NoError(t, err)
		assert.Zero(t, wrong, "%s: every row carries the act's token and agent mark", c.table)
	}
}

// docs/adr/0036 D6, the owner's decision of 2026-10-04: every act made through
// a token is marked with the token — its id and its name, never its secret —
// beside the agent mark, in every row and every answer that shows the act; a
// plain token keeps its person's authority (D1) and is marked as a token, an
// agent token carries its agent mark and the token, the person's own browser
// session carries neither. The name is the token's as the act recorded it: a
// member who may not read another person's tokens reads it, and it stays after
// the token is revoked.
func TestEveryActThroughATokenIsMarkedWithIt(t *testing.T) {
	w := newWorld(t)
	e := ticketEnv{world: w, tk: issueTokens(t, w), s: newAPI(t, withLogin), ctx: context.Background()}
	names := withAccounts(t, w)
	f := fixtures(t)
	plain, plainID, err := f.Token(e.ctx, fixture.TokenSpec{UserID: w.MemberA, Name: "ci-script"})
	require.NoError(t, err)
	agent, agentID, err := f.Token(e.ctx, fixture.TokenSpec{UserID: w.MemberA, Name: "claude-laptop", Agent: true})
	require.NoError(t, err)

	plainMark := actMark{token: plainID, tokenName: "ci-script"}
	byPlain := e.markedActs(t, e.asToken(t, caller{Token: plain}), plainMark)
	assertRows(t, byPlain.Id, plainMark)

	agentMark := actMark{agent: "claude-code/opus/s-1", token: agentID, tokenName: "claude-laptop"}
	byAgent := e.markedActs(t, e.asToken(t, caller{Token: agent, Agent: agentMark.agent}), agentMark)
	assertRows(t, byAgent.Id, agentMark)

	b := e.s.browser(t)
	b.mustLogin(names["memberA"], testPassword)
	bySession := e.markedActs(t, asBrowser(b), actMark{})
	assertRows(t, bySession.Id, actMark{})

	// The tenant's audit view names the token beside its id, as JSON and as CSV
	// (docs/adr/0026 D6).
	admin := e.asToken(t, caller{Token: e.tk.AdminA})
	audit := "/api/v1/teams/" + e.SlugA + "/audit?token=" + plainID.String()
	res := admin(http.MethodGet, audit, nil)
	require.Equal(t, http.StatusOK, res.StatusCode)
	acts := decode[apigen.AuditList](t, res)
	require.NotEmpty(t, acts.Items)
	for _, a := range acts.Items {
		name, err := a.TokenName.Get()
		require.NoError(t, err, "%s: the audit view names the token", a.Action)
		assert.Equal(t, "ci-script", name, a.Action)
	}
	res = admin(http.MethodGet, audit, nil, "Accept", "text/csv")
	require.Equal(t, http.StatusOK, res.StatusCode)
	records, err := csv.NewReader(res.Body).ReadAll()
	require.NoError(t, err)
	require.Len(t, records, len(acts.Items)+1)
	// The released columns keep their places; the name is the last column.
	require.Equal(t, []string{"id", "created_at", "actor_user_id", "actor_system", "agent", "token_id",
		"entity_type", "entity_id", "ticket_key", "action", "reason", "note", "request_id", "before", "after",
		"token_name"}, records[0])
	for _, r := range records[1:] {
		assert.Equal(t, plainID.String(), r[5])
		assert.Equal(t, "ci-script", r[len(r)-1])
	}

	// The plain token is revoked; its acts keep its name, which another member
	// of the tenant reads — the tokens policy would not let them read its row —
	// and the person's time an administrator reads.
	res = e.s.do(t, caller{Token: plain}, http.MethodDelete, "/api/v1/me/tokens/"+plainID.String(), nil)
	require.Equal(t, http.StatusNoContent, res.StatusCode)
	assertProblem(t, e.s.do(t, caller{Token: plain}, http.MethodGet, "/api/v1/me", nil), http.StatusUnauthorized, "token_revoked")
	member := e.asToken(t, caller{Token: e.tk.Both})
	path := fmt.Sprintf("%s/%d", e.projectTickets("ALPHA"), byPlain.Number)
	for _, part := range []string{"", "/comments", "/attachments", "/questions", "/interest", "/activity"} {
		res := member(http.MethodGet, path+part, nil)
		require.Equal(t, http.StatusOK, res.StatusCode)
		b, err := io.ReadAll(res.Body)
		require.NoError(t, err)
		raw := string(b)
		assert.Contains(t, raw, `token":{"id":"`+plainID.String()+`","name":"ci-script"}`, "%s after the revocation", part)
		assert.NotContains(t, raw, plain, "%s: never the token itself", part)
		assert.NotContains(t, raw, "cwk_", "%s: never a part of a token", part)
	}
	res = member(http.MethodGet, path+"/questions", nil)
	questions := decode[apigen.QuestionList](t, res)
	require.Len(t, questions.Items, 1)
	plainMark.assertOn(t, "the answer after the revocation", nullable.NewNullNullable[string](), questions.Items[0].AnsweredByToken)
	res = e.asToken(t, caller{Token: e.tk.AdminA})(http.MethodGet, path+"/time-entries", nil)
	require.Equal(t, http.StatusOK, res.StatusCode)
	entries := decode[apigen.TimeEntryList](t, res)
	require.Len(t, entries.Items, 1)
	plainMark.assertOn(t, "the time entry after the revocation", nullable.NewNullNullable[string](), entries.Items[0].Token)

	// A stake carries the mark of the write that set it as it stands: the
	// person's own session clears the token's, and an agent's change marks it
	// as the agent's.
	restake := func(send sender, tk apigen.Ticket, note string) apigen.Interest {
		t.Helper()
		res := send(http.MethodPut, fmt.Sprintf("%s/%d/interest", e.projectTickets("ALPHA"), tk.Number),
			map[string]any{"weight": "urgent", "note": note})
		require.Equal(t, http.StatusOK, res.StatusCode)
		return decode[apigen.Interest](t, res)
	}
	cleared := restake(asBrowser(b), byPlain, "my own words")
	actMark{}.assertOn(t, "the stake the person changed", cleared.Agent, cleared.Token)
	taken := restake(e.asToken(t, caller{Token: agent, Agent: agentMark.agent}), bySession, "said in chat")
	agentMark.assertOn(t, "the stake the agent changed", taken.Agent, taken.Token)
	n, err := f.QueryCount(e.ctx, `SELECT count(*) FROM ticket_interest WHERE ticket_id = $1 AND agent IS NULL
		AND token_id IS NULL AND token_name IS NULL`, byPlain.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n, "the person's change cleared the token's mark")
}
