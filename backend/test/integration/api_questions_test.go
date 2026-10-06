//go:build integration

package integration

import (
	"fmt"
	"net/http"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/test/fixture"
)

func (e ticketEnv) ask(t *testing.T, c caller, tk apigen.Ticket, body apigen.QuestionCreate) *apigen.AskQuestionResponse {
	t.Helper()
	params := &apigen.AskQuestionParams{}
	if c.Agent != "" {
		params.IdempotencyKey = newKey()
	}
	res, err := e.s.client(t, c).AskQuestionWithResponse(e.ctx, e.SlugA, tk.Project, tk.Number, params, body)
	require.NoError(t, err)
	return res
}

func (e ticketEnv) answer(t *testing.T, c caller, tk apigen.Ticket, q apigen.Question, text string) *apigen.AnswerQuestionResponse {
	t.Helper()
	etag := strconv.Quote(strconv.Itoa(q.Version))
	res, err := e.s.client(t, c).AnswerQuestionWithResponse(e.ctx, e.SlugA, tk.Project, tk.Number, q.Number,
		&apigen.AnswerQuestionParams{IfMatch: &etag}, apigen.AnswerSet{Answer: text})
	require.NoError(t, err)
	return res
}

func (e ticketEnv) withdraw(t *testing.T, c caller, tk apigen.Ticket, q apigen.Question) *apigen.WithdrawQuestionResponse {
	t.Helper()
	res, err := e.s.client(t, c).WithdrawQuestionWithResponse(e.ctx, e.SlugA, tk.Project, tk.Number, q.Number)
	require.NoError(t, err)
	return res
}

// docs/adr/0011 D2, D4: open → answered and open → withdrawn; numbers stay
// when a question is withdrawn; only the asker edits or withdraws.
func TestQuestionLifecycle(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	member, both := caller{Token: e.tk.MemberA}, caller{Token: e.tk.Both}
	tk := e.file(t, member, "ALPHA", task("Needs a call"))

	first := e.ask(t, member, tk, apigen.QuestionCreate{Question: "Postgres or SQLite?", Options: ptr("- A: Postgres\n- B: SQLite"),
		Recommendation: ptr("A"), AskedOf: &e.Both})
	require.Equal(t, http.StatusCreated, first.StatusCode(), string(first.Body))
	q1 := *first.JSON201
	assert.Equal(t, 1, q1.Number)
	assert.Equal(t, apigen.QuestionStatusOpen, q1.Status)
	assert.Equal(t, e.Both, q1.AskedOf.MustGet().Id)
	assert.Equal(t, fmt.Sprintf("/api/v1/tenants/%s/projects/ALPHA/tickets/%d/questions/1", e.SlugA, tk.Number), *first.Headers201.Location)
	q2 := *e.ask(t, member, tk, apigen.QuestionCreate{Question: "Which port?"}).JSON201
	assert.True(t, q2.AskedOf.IsNull(), "no one named: open in the tenant")

	etag := strconv.Quote(strconv.Itoa(q2.Version))
	edited, err := e.s.client(t, member).UpdateQuestionWithResponse(e.ctx, e.SlugA, "ALPHA", tk.Number, q2.Number,
		&apigen.UpdateQuestionParams{IfMatch: &etag}, apigen.QuestionPatch{Question: ptr("Which port, 8080 or 9090?")})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, edited.StatusCode(), string(edited.Body))
	notAsker, err := e.s.client(t, both).UpdateQuestionWithResponse(e.ctx, e.SlugA, "ALPHA", tk.Number, q2.Number,
		&apigen.UpdateQuestionParams{IfMatch: ptr(`"2"`)}, apigen.QuestionPatch{Question: ptr("mine now")})
	require.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, notAsker.StatusCode(), "only the asker edits")

	assert.Equal(t, http.StatusForbidden, e.withdraw(t, both, tk, q2).StatusCode(), "only the asker withdraws")
	w := e.withdraw(t, member, tk, *edited.JSON200)
	require.Equal(t, http.StatusOK, w.StatusCode(), string(w.Body))
	assert.Equal(t, apigen.QuestionStatusWithdrawn, w.JSON200.Status)
	assert.Equal(t, http.StatusOK, e.withdraw(t, member, tk, *w.JSON200).StatusCode(), "withdrawing is idempotent")
	assertProblem(t, e.s.do(t, both, http.MethodPut, fmt.Sprintf("%s/%d/questions/2/answer", e.projectTickets("ALPHA"), tk.Number),
		map[string]any{"answer": "8080"}), http.StatusConflict, "state_conflict")
	q3 := *e.ask(t, member, tk, apigen.QuestionCreate{Question: "Third"}).JSON201
	assert.Equal(t, 3, q3.Number, "a withdrawn question keeps its number")

	assert.Equal(t, http.StatusForbidden, e.answer(t, member, tk, q1, "Postgres").StatusCode(), "asked of another person")
	answered := e.answer(t, both, tk, q1, "Postgres, for the row-level security")
	require.Equal(t, http.StatusOK, answered.StatusCode(), string(answered.Body))
	a := *answered.JSON200
	assert.Equal(t, apigen.QuestionStatusAnswered, a.Status)
	assert.Equal(t, e.Both, a.AnsweredBy.MustGet().Id)
	assert.False(t, a.RecordedByAgent)
	stale := e.answer(t, both, tk, q1, "SQLite after all")
	assert.Equal(t, http.StatusPreconditionFailed, stale.StatusCode(), "a changed answer needs the version read")
	changed := e.answer(t, both, tk, a, "Postgres 18")
	require.Equal(t, http.StatusOK, changed.StatusCode(), "a person changes their own answer")
	assert.Equal(t, http.StatusConflict, e.withdraw(t, member, tk, *changed.JSON200).StatusCode(), "an answered question stays")

	open := e.answer(t, caller{Token: e.tk.AdminA}, tk, q3, "anyone may answer an open question")
	assert.Equal(t, http.StatusOK, open.StatusCode(), string(open.Body))
	assert.Equal(t, http.StatusForbidden, e.ask(t, caller{Token: e.tk.ViewerA}, tk, apigen.QuestionCreate{Question: "viewer?"}).StatusCode())

	list, err := e.s.client(t, member).ListQuestionsWithResponse(e.ctx, e.SlugA, "ALPHA", tk.Number, &apigen.ListQuestionsParams{})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, list.StatusCode())
	numbers := make([]int, 0, len(list.JSON200.Items))
	for _, q := range list.JSON200.Items {
		numbers = append(numbers, q.Number)
	}
	assert.Equal(t, []int{1, 2, 3}, numbers)
	acts, err := f.QueryCount(e.ctx, "SELECT count(*) FROM audit_events WHERE ticket_id = $1 AND entity_type = 'question'", tk.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 8, acts, "asked ×3, edited, withdrawn, answered ×3")
}

// docs/adr/0011 D2: withdrawing stays idempotent when the withdrawals race,
// and a withdrawal that loses to an answer is a state conflict, never 500.
func TestSimultaneousWithdrawalsAnswerAlike(t *testing.T) {
	e := newTicketEnv(t)
	member, both := caller{Token: e.tk.MemberA}, caller{Token: e.tk.Both}
	tk := e.file(t, member, "ALPHA", task("Races"))
	withdraw := func(q apigen.Question) func() int {
		cl := e.s.client(t, member)
		return func() int {
			res, err := cl.WithdrawQuestionWithResponse(e.ctx, e.SlugA, tk.Project, tk.Number, q.Number)
			if err != nil {
				return 0
			}
			return res.StatusCode()
		}
	}

	q := *e.ask(t, member, tk, apigen.QuestionCreate{Question: "Now or later?"}).JSON201
	for i, code := range simultaneously(times(16, withdraw(q))...) {
		assert.Equal(t, http.StatusOK, code, "withdrawal %d", i)
	}

	answerer := e.s.client(t, both)
	for round := range 8 {
		q := *e.ask(t, member, tk, apigen.QuestionCreate{Question: fmt.Sprintf("Round %d?", round)}).JSON201
		etag := strconv.Quote(strconv.Itoa(q.Version))
		codes := simultaneously(withdraw(q), func() int {
			res, err := answerer.AnswerQuestionWithResponse(e.ctx, e.SlugA, tk.Project, tk.Number, q.Number,
				&apigen.AnswerQuestionParams{IfMatch: &etag}, apigen.AnswerSet{Answer: "now"})
			if err != nil {
				return 0
			}
			return res.StatusCode()
		})
		assert.Contains(t, []int{http.StatusOK, http.StatusConflict}, codes[0], "round %d: the withdrawal", round)
		assert.Contains(t, []int{http.StatusOK, http.StatusConflict, http.StatusPreconditionFailed}, codes[1], "round %d: the answer", round)
		assert.NotEqual(t, codes[0] == http.StatusOK, codes[1] == http.StatusOK, "round %d: exactly one wins", round)
	}
}

// docs/adr/0066 D8: only a person decides an answer; an agent with
// record-answer writes its person's answer down, marked so, and changes only
// an answer an agent of the same person recorded.
func TestAgentsRecordAnswers(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	member := caller{Token: e.tk.MemberA}
	mint := func(person uuid.UUID, caps ...string) string {
		tok, _, err := f.Token(e.ctx, fixture.TokenSpec{UserID: person, Agent: true, Capabilities: caps})
		require.NoError(t, err)
		return tok
	}
	bothAgent := caller{Token: mint(e.Both), Agent: "claude-code/opus/both"}
	memberAgent := caller{Token: mint(e.MemberA), Agent: "claude-code/opus/member"}
	noRecord := caller{Token: mint(e.Both, "interest"), Agent: "claude-code/opus/narrow"}
	tk := e.file(t, member, "ALPHA", task("Agent answers"))
	q := *e.ask(t, member, tk, apigen.QuestionCreate{Question: "Ship on Friday?", AskedOf: &e.Both}).JSON201

	refused := e.answer(t, noRecord, tk, q, "yes")
	require.Equal(t, http.StatusForbidden, refused.StatusCode())
	assert.Equal(t, "missing capability: record-answer", *refused.ApplicationproblemJSONDefault.Detail)
	assert.Equal(t, http.StatusForbidden, e.answer(t, memberAgent, tk, q, "yes").StatusCode(), "the agent of another person")

	rec := e.answer(t, bothAgent, tk, q, "yes, said in chat")
	require.Equal(t, http.StatusOK, rec.StatusCode(), string(rec.Body))
	assert.True(t, rec.JSON200.RecordedByAgent)
	assert.Equal(t, e.Both, rec.JSON200.AnsweredBy.MustGet().Id, "the actor stays the person")
	upd := e.answer(t, bothAgent, tk, *rec.JSON200, "yes, after the demo")
	require.Equal(t, http.StatusOK, upd.StatusCode(), "an agent updates what an agent of the same person recorded")
	own := e.answer(t, caller{Token: e.tk.Both}, tk, *upd.JSON200, "yes — my words")
	require.Equal(t, http.StatusOK, own.StatusCode())
	assert.False(t, own.JSON200.RecordedByAgent)
	res := e.answer(t, bothAgent, tk, *own.JSON200, "overwritten by the agent")
	require.Equal(t, http.StatusForbidden, res.StatusCode())
	assert.Equal(t, "agent_forbidden", string(res.ApplicationproblemJSONDefault.Code), "the person's own answer is not the agent's to change")

	byAgent := e.ask(t, memberAgent, tk, apigen.QuestionCreate{Question: "Asked by the agent"})
	require.Equal(t, http.StatusCreated, byAgent.StatusCode(), string(byAgent.Body))
	assert.Equal(t, "claude-code/opus/member", byAgent.JSON201.AskedByAgent.MustGet())
	byPerson := *e.ask(t, member, tk, apigen.QuestionCreate{Question: "Asked by the person"}).JSON201
	assert.Equal(t, http.StatusForbidden, e.withdraw(t, memberAgent, tk, byPerson).StatusCode(), "not what an agent asked")
	etag := strconv.Quote(strconv.Itoa(byPerson.Version))
	reworded, err := e.s.client(t, memberAgent).UpdateQuestionWithResponse(e.ctx, e.SlugA, "ALPHA", tk.Number, byPerson.Number,
		&apigen.UpdateQuestionParams{IfMatch: &etag}, apigen.QuestionPatch{Question: ptr("Asked by the person, reworded")})
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, reworded.StatusCode(), "an agent rewords its person's open question: the baseline by the owner's decision (docs/adr/0043 D2)")
	assert.Equal(t, http.StatusOK, e.withdraw(t, memberAgent, tk, *byAgent.JSON201).StatusCode())
}

// The person asked must see the ticket; a question is visible with its
// ticket only; has_open_questions filters (docs/adr/0049 D1).
func TestQuestionVisibility(t *testing.T) {
	e := newTicketEnv(t)
	member, viewer := caller{Token: e.tk.MemberA}, caller{Token: e.tk.ViewerA}
	secret := e.file(t, member, "ALPHA", task("Secret question", func(b *apigen.TicketCreate) {
		b.Security, b.Threat = apigen.SecurityClassLive, ptr("leak")
	}))
	res := e.ask(t, member, secret, apigen.QuestionCreate{Question: "Rotate now?", AskedOf: &e.ViewerA})
	require.Equal(t, http.StatusBadRequest, res.StatusCode(), "the person asked cannot see the ticket")
	q := e.ask(t, member, secret, apigen.QuestionCreate{Question: "Rotate now?", AskedOf: &e.AdminA})
	require.Equal(t, http.StatusCreated, q.StatusCode(), string(q.Body))
	path := fmt.Sprintf("%s/%d/questions", e.projectTickets("ALPHA"), secret.Number)
	assertProblem(t, e.s.do(t, viewer, http.MethodGet, path, nil), http.StatusNotFound, "not_found")
	assertProblem(t, e.s.do(t, viewer, http.MethodGet, path+"/1", nil), http.StatusNotFound, "not_found")
	assertProblem(t, e.s.do(t, caller{Token: e.tk.MemberB}, http.MethodGet, path, nil), http.StatusNotFound, "not_found")

	plain := e.file(t, member, "ALPHA", task("Plain"))
	e.ask(t, member, plain, apigen.QuestionCreate{Question: "Anyone?"})
	e.file(t, member, "ALPHA", task("Quiet"))
	assert.Equal(t, []string{"Plain"}, e.titles(t, viewer, e.tenantTickets(), "has_open_questions=true"))
	assert.Equal(t, []string{"Plain", "Secret question"}, e.titles(t, member, e.tenantTickets(), "has_open_questions=true"))
	assert.Equal(t, []string{"Quiet"}, e.titles(t, member, e.tenantTickets(), "has_open_questions=false"))

	etag := `"1"`
	moved, err := e.s.client(t, member).UpdateQuestionWithResponse(e.ctx, e.SlugA, "ALPHA", plain.Number, 1,
		&apigen.UpdateQuestionParams{IfMatch: &etag}, apigen.QuestionPatch{AskedOf: nullable.NewNullableWithValue(e.MemberB)})
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, moved.StatusCode(), "a person of another tenant is no one to ask")
}
