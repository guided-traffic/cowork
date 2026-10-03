package tools

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// transition reads the state and sends it as the move's precondition
// (docs/adr/0045 D2), the note to done and the reason elsewhere, the block
// with blocked; a refusal is the API's (docs/adr/0042 D3).
func TestTransition(t *testing.T) {
	f := newFake(t)
	f.on("GET "+ticketPath, http.StatusOK, ticket("acme/COW-12", "in-progress"), "ETag", `"3"`)
	f.on("POST "+ticketPath+"/transitions", http.StatusOK, ticket("acme/COW-12", "review"))
	s := f.session(true)

	res := call(t, s, "transition", `{"key": "COW-12", "to": "review", "comment": "ready"}`)
	require.False(t, res.IsError, res.Text)
	assert.Equal(t, "Moved acme/COW-12 from in-progress to review.", res.Text)
	sent := f.calls(http.MethodPost, ticketPath+"/transitions")
	require.Len(t, sent, 1)
	body := decodeBody(t, sent[0])
	assert.Equal(t, "in-progress", body["from"])
	assert.Equal(t, "review", body["to"])
	assert.Equal(t, "ready", body["comment"])
	assert.NotContains(t, body, "reason")
	assert.NotEmpty(t, sent[0].Header.Get("Idempotency-Key"), "recorded on the act (docs/adr/0045 D7)")

	res = call(t, s, "transition", `{"key": "COW-12", "to": "done"}`)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Text, "verification note")
	res = call(t, s, "transition", `{"key": "COW-12", "to": "blocked", "reason_or_note": "waits"}`)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Text, "block_kind")
	assert.Len(t, f.calls(http.MethodPost, ticketPath+"/transitions"), 1, "nothing was sent for the refused calls")

	res = call(t, s, "transition", `{"key": "COW-12", "to": "done", "reason_or_note": "go test ./... passed"}`)
	require.False(t, res.IsError, res.Text)
	assert.Equal(t, "go test ./... passed", decodeBody(t, f.calls(http.MethodPost, ticketPath+"/transitions")[1])["note"])
	res = call(t, s, "transition", `{"key": "COW-12", "to": "blocked", "reason_or_note": "waits", "block_kind": "ticket", "blocked_by": "COW-3"}`)
	require.False(t, res.IsError, res.Text)
	body = decodeBody(t, f.calls(http.MethodPost, ticketPath+"/transitions")[2])
	assert.Equal(t, "waits", body["reason"])
	assert.Equal(t, map[string]any{"kind": "ticket", "ticket": "COW-3"}, body["block"])

	g := newFake(t)
	g.on("GET "+ticketPath, http.StatusOK, ticket("acme/COW-12", "analysed"), "ETag", `"3"`)
	g.refuse("POST "+ticketPath+"/transitions", http.StatusForbidden, "agent_forbidden", "missing capability: decide")
	res = call(t, g.session(true), "transition", `{"key": "COW-12", "to": "decided"}`)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Text, "403 `agent_forbidden`: missing capability: decide")
	assert.Contains(t, res.Text, "not a failure of the tool")
}

func TestSetProgress(t *testing.T) {
	f := newFake(t)
	f.on("GET "+ticketPath, http.StatusOK, ticket("acme/COW-12", "in-progress"), "ETag", `"3"`)
	f.on("PATCH "+ticketPath, http.StatusOK, ticket("acme/COW-12", "done"))
	s := f.session(true)
	res := call(t, s, "set_progress", `{"key": "COW-12", "percent": 100, "stage": "review", "note": "checked"}`)
	require.False(t, res.IsError, res.Text)
	assert.Equal(t, "Set the review stage of acme/COW-12 to 100%. The ticket moved from in-progress to done.", res.Text)
	sent := f.calls(http.MethodPatch, ticketPath)[0]
	assert.Equal(t, `"3"`, sent.Header.Get("If-Match"))
	assert.Equal(t, map[string]any{"progress_review": float64(100), "note": "checked"}, decodeBody(t, sent))

	res = call(t, s, "set_progress", `{"key": "COW-12", "percent": 40}`)
	require.False(t, res.IsError, res.Text)
	assert.Equal(t, map[string]any{"progress": float64(40)}, decodeBody(t, f.calls(http.MethodPatch, ticketPath)[1]))
	res = call(t, s, "set_progress", `{"key": "COW-12", "percent": 42}`)
	assert.True(t, res.IsError, "steps of five")
}

// finish_work: the note as a comment, the implementation stage to 100, and
// the furthest move the token may (docs/adr/0042 D1, docs/adr/0043 D6), with
// what remains for a person and the extraction (docs/adr/0069 D5).
func TestFinishWork(t *testing.T) {
	token := func(caps ...string) map[string]any {
		c := make([]any, 0, len(caps))
		for _, x := range caps {
			c = append(c, x)
		}
		return map[string]any{"id": "0199a3c2-1d2e-7f00-8000-000000000001", "name": "laptop", "scope": "write", "agent": true,
			"capabilities": c, "created_at": "2026-10-01T00:00:00Z", "expires_at": "2026-12-01T00:00:00Z", "state": "active",
			"restricted_project": nil, "request": map[string]any{"agent": true, "agent_mark": fakeAgent, "capabilities": c}}
	}
	setup := func(state string, caps ...string) *fakeAPI {
		f := newFake(t)
		f.on("GET "+ticketPath, http.StatusOK, ticket("acme/COW-12", state), "ETag", `"3"`)
		f.on("POST "+ticketPath+"/comments", http.StatusCreated, map[string]any{"id": "0199a3c2-1d2e-7f00-8000-000000000002"})
		f.on("PATCH "+ticketPath, http.StatusOK, ticket("acme/COW-12", state, func(m map[string]any) { m["progress"] = 100 }))
		f.on("GET /api/v1/me/token", http.StatusOK, token(caps...))
		return f
	}

	f := setup("in-progress", "close")
	f.on("POST "+ticketPath+"/transitions", http.StatusOK, ticket("acme/COW-12", "done"))
	res := call(t, f.session(true), "finish_work", `{"key": "COW-12", "verification_note": "make test passed"}`)
	require.False(t, res.IsError, res.Text)
	assert.Contains(t, res.Text, "Finished the work on acme/COW-12 (now done): wrote the verification note as a comment; set the implementation stage to 100; closed it, done by hand with the note.")
	assert.NotContains(t, res.Text, "What remains")
	assert.Contains(t, res.Text, "docs/adr/0069 D5")
	assert.Equal(t, "Verification: make test passed", decodeBody(t, f.calls(http.MethodPost, ticketPath+"/comments")[0])["body"])
	move := decodeBody(t, f.calls(http.MethodPost, ticketPath+"/transitions")[0])
	assert.Equal(t, map[string]any{"from": "in-progress", "to": "done", "note": "make test passed"}, move)

	f = setup("in-progress")
	f.on("POST "+ticketPath+"/transitions", http.StatusOK, ticket("acme/COW-12", "review"))
	res = call(t, f.session(true), "finish_work", `{"key": "COW-12", "verification_note": "ok"}`)
	require.False(t, res.IsError, res.Text)
	assert.Contains(t, res.Text, "moved it to review")
	assert.Contains(t, res.Text, "this token lacks close, so done is the person's")
	assert.Equal(t, "review", decodeBody(t, f.calls(http.MethodPost, ticketPath+"/transitions")[0])["to"])

	f = setup("in-progress", "close")
	n := 0
	f.mux.HandleFunc("POST "+ticketPath+"/transitions", func(w http.ResponseWriter, r *http.Request) {
		n++
		if n == 1 {
			problem(w, http.StatusConflict, "open_prerequisites", "tickets that block this one are open")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"key": "acme/COW-12", "state": "review", "type": "task", "title": "Ship it"}`))
	})
	res = call(t, f.session(true), "finish_work", `{"key": "COW-12", "verification_note": "ok"}`)
	require.False(t, res.IsError, res.Text)
	assert.Contains(t, res.Text, "open_prerequisites")
	assert.Contains(t, res.Text, "a person may close over them")
	assert.Contains(t, res.Text, "moved it to review")

	f = setup("decided", "close")
	res = call(t, f.session(true), "finish_work", `{"key": "COW-12", "verification_note": "ok"}`)
	require.False(t, res.IsError, res.Text)
	assert.Contains(t, res.Text, "an agent closes from in-progress or review only")
	assert.Empty(t, f.calls(http.MethodPost, ticketPath+"/transitions"))

	f2 := newFake(t)
	f2.on("GET "+ticketPath, http.StatusOK, ticket("acme/COW-12", "review", func(m map[string]any) { m["progress_review"] = 100 }), "ETag", `"3"`)
	f2.on("POST "+ticketPath+"/comments", http.StatusCreated, map[string]any{"id": "0199a3c2-1d2e-7f00-8000-000000000002"})
	f2.on("PATCH "+ticketPath, http.StatusOK, ticket("acme/COW-12", "done"))
	f2.on("GET /api/v1/me/token", http.StatusOK, token("close"))
	res = call(t, f2.session(true), "finish_work", `{"key": "COW-12", "verification_note": "ok"}`)
	require.False(t, res.IsError, res.Text)
	assert.Contains(t, res.Text, "closed it: its three stages are full", "the last stage closes the ticket by its stages")
	patch := decodeBody(t, f2.calls(http.MethodPatch, ticketPath)[0])
	assert.Equal(t, "ok", patch["note"])
	assert.Empty(t, f2.calls(http.MethodPost, ticketPath+"/transitions"))
	assert.True(t, strings.Contains(res.Text, "Cowork-Ticket: acme/COW-12"))
}
