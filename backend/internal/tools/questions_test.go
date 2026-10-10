package tools

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	adaID = "0199a3c2-1d2e-7f00-8000-0000000000a1"
	samID = "0199a3c2-1d2e-7f00-8000-0000000000a2"
)

func question(number int, status string) map[string]any {
	return map[string]any{"id": "0199a3c2-1d2e-7f00-8000-0000000000c1", "number": number, "question": "Zip or tar?",
		"options": "", "recommendation": "", "answer": nil, "status": status,
		"asked_by": map[string]any{"id": adaID, "display_name": "Ada"}, "asked_by_agent": nil, "asked_of": nil,
		"answered_by": map[string]any{"id": adaID, "display_name": "Ada"}, "answered_at": nil, "recorded_by_agent": true,
		"withdrawn_at": nil, "version": 2, "created_at": "2026-10-01T00:00:00Z", "updated_at": "2026-10-01T00:00:00Z"}
}

// open_question asks of a person resolved by name, and says the answer is
// theirs (docs/adr/0011 D2); one question at a time.
func TestOpenQuestion(t *testing.T) {
	f := newFake(t)
	f.on("POST "+ticketPath+"/questions", http.StatusCreated, question(2, "open"))
	f.on("GET "+ticketPath+"/questions", http.StatusOK, list(question(1, "open"), question(2, "open")))
	f.on("GET /api/v1/teams/acme/members", http.StatusOK, list(
		map[string]any{"person": map[string]any{"id": adaID, "username": "ada", "display_name": "Ada"}, "role": "admin"},
		map[string]any{"person": map[string]any{"id": samID, "username": "sam", "display_name": "Sam Doe"}, "role": "member"}))
	f.on("GET /api/v1/me", http.StatusOK, map[string]any{"id": adaID, "display_name": "Ada", "memberships": []any{}})
	s := f.session(true)

	res := call(t, s, "open_question", `{"key": "COW-12", "question": "Zip or tar?", "options": "- zip\n- tar",
		"recommendation": "zip", "asked_of": "Sam Doe"}`)
	require.False(t, res.IsError, res.Text)
	assert.Contains(t, res.Text, "Asked Q2 on acme/COW-12, asked of Sam Doe")
	assert.Contains(t, res.Text, `record_answer(key: "acme/COW-12", question: 2)`)
	assert.Contains(t, res.Text, "1 more questions are open on this ticket: put them to the person one at a time.")
	body := decodeBody(t, f.calls(http.MethodPost, ticketPath+"/questions")[0])
	assert.Equal(t, samID, body["asked_of"])
	assert.Equal(t, "zip", body["recommendation"])

	res = call(t, s, "open_question", `{"key": "COW-12", "question": "q", "options": "", "recommendation": "", "asked_of": "me"}`)
	require.False(t, res.IsError, res.Text)
	assert.Equal(t, adaID, decodeBody(t, f.calls(http.MethodPost, ticketPath+"/questions")[1])["asked_of"])
	res = call(t, s, "open_question", `{"key": "COW-12", "question": "q", "options": "", "recommendation": ""}`)
	require.False(t, res.IsError, res.Text)
	assert.Contains(t, res.Text, "open to the team")
	res = call(t, s, "open_question", `{"key": "COW-12", "question": "q", "options": "", "recommendation": "", "asked_of": "nobody"}`)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Text, "no member of the team acme")
}

// record_answer writes the person's answer down, with If-Match once the
// question is answered (docs/adr/0066 D8); without record-answer the API
// refuses, and the refusal is said to be one.
func TestRecordAnswer(t *testing.T) {
	f := newFake(t)
	f.on("GET "+ticketPath+"/questions/1", http.StatusOK, question(1, "open"), "ETag", `"1"`)
	f.on("GET "+ticketPath+"/questions/2", http.StatusOK, question(2, "answered"), "ETag", `"2"`)
	f.on("PUT "+ticketPath+"/questions/1/answer", http.StatusOK, question(1, "answered"))
	f.on("PUT "+ticketPath+"/questions/2/answer", http.StatusOK, question(2, "answered"))
	s := f.session(true)

	res := call(t, s, "record_answer", `{"key": "COW-12", "question": 1, "answer": "zip"}`)
	require.False(t, res.IsError, res.Text)
	assert.Equal(t, "Recorded Ada's answer to Q1 on acme/COW-12, marked as recorded by the agent.", res.Text)
	first := f.calls(http.MethodPut, ticketPath+"/questions/1/answer")[0]
	assert.Empty(t, first.Header.Get("If-Match"))
	assert.Equal(t, "zip", decodeBody(t, first)["answer"])

	res = call(t, s, "record_answer", `{"key": "COW-12", "question": 2, "answer": "tar after all"}`)
	require.False(t, res.IsError, res.Text)
	assert.Equal(t, `"2"`, f.calls(http.MethodPut, ticketPath+"/questions/2/answer")[0].Header.Get("If-Match"))

	g := newFake(t)
	g.on("GET "+ticketPath+"/questions/1", http.StatusOK, question(1, "open"), "ETag", `"1"`)
	g.refuse("PUT "+ticketPath+"/questions/1/answer", http.StatusForbidden, "agent_forbidden", "missing capability: record-answer")
	res = call(t, g.session(true), "record_answer", `{"key": "COW-12", "question": 1, "answer": "zip"}`)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Text, "missing capability: record-answer")
	assert.Contains(t, res.Text, "the act is a person's")
}

// A host that knows the person answers "me" itself, and a host confined to a
// team searches that team for "every team": neither asks GET /api/v1/me,
// which the chat in the backend may not call.
func TestAHostThatKnowsThePerson(t *testing.T) {
	f := newFake(t)
	f.on("POST "+ticketPath+"/questions", http.StatusCreated, map[string]any{"number": 1, "question": "Retry?"})
	f.on("GET "+ticketPath+"/questions", http.StatusOK, list())
	f.on("GET /api/v1/teams/acme/tickets", http.StatusOK, list(ticket("acme/COW-12", "decided")))
	s := f.session(true)
	s.Person = &Person{ID: uuid.MustParse(samID), Name: "Sam Doe"}
	s.Teams = []string{"acme"}

	res := call(t, s, "open_question", `{"key": "COW-12", "question": "Retry?", "options": "-", "recommendation": "retry", "asked_of": "me"}`)
	require.False(t, res.IsError, res.Text)
	assert.Contains(t, res.Text, "asked of Sam Doe")
	assert.Equal(t, samID, decodeBody(t, f.calls(http.MethodPost, ticketPath+"/questions")[0])["asked_of"])

	res = call(t, s, "search", `{"query": "gate", "scope": "all"}`)
	require.False(t, res.IsError, res.Text)
	assert.Contains(t, res.Text, "Tickets in the teams this session works in, acme matching")
	assert.Len(t, f.calls(http.MethodGet, "/api/v1/teams/acme/tickets"), 1)
	assert.Empty(t, f.calls(http.MethodGet, "/api/v1/me"), "the person and the teams came from the host")
}
