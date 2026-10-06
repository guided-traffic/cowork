package tools

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const ticketPath = "/api/v1/tenants/acme/projects/COW/tickets/12"

// get_ticket reads the context (docs/adr/0044 D4) and hands the strings a
// commit carries (docs/adr/0068 D4); a short key needs a bound session.
func TestGetTicket(t *testing.T) {
	f := newFake(t)
	f.text("GET "+ticketPath+"/context", "<!-- cowork: context of acme/COW-12 -->\n---\nkey: acme/COW-12\n---\n")
	f.on("GET "+ticketPath, http.StatusOK, ticket("acme/COW-12", "in-progress", func(m map[string]any) {
		m["type"], m["title"] = "bug", "Guard the failover gate"
	}), "ETag", `"3"`)

	res := call(t, f.session(true), "get_ticket", `{"key": "COW-12", "comments": 3}`)
	require.False(t, res.IsError, res.Text)
	assert.Contains(t, res.Text, "context of acme/COW-12")
	assert.Contains(t, res.Text, "`(COW-12)`")
	assert.Contains(t, res.Text, "`Cowork-Ticket: acme/COW-12`")
	assert.Contains(t, res.Text, "`fix/COW-12-guard-failover-gate`")
	assert.Contains(t, res.Text, "https://cowork.example.com/t/acme/tickets/COW-12")
	got := f.calls(http.MethodGet, ticketPath+"/context")
	require.Len(t, got, 1)
	q, err := url.ParseQuery(got[0].Query)
	require.NoError(t, err)
	assert.Equal(t, "3", q.Get("comments"))
	assert.Equal(t, "10", q.Get("activity"))
	assert.Equal(t, "Bearer "+fakeToken, got[0].Header.Get("Authorization"))
	assert.Equal(t, fakeAgent, got[0].Header.Get("X-Cowork-Agent"), "every request is marked as the agent's (docs/adr/0036 D3)")

	res = call(t, f.session(false), "get_ticket", `{"key": "COW-12"}`)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Text, "write the full key")
	res = call(t, f.session(false), "get_ticket", `{"key": "acme/COW-12"}`)
	assert.False(t, res.IsError, res.Text)

	f.refuse("GET /api/v1/tenants/acme/projects/COW/tickets/13/context", http.StatusNotFound, "not_found", "no such ticket")
	res = call(t, f.session(true), "get_ticket", `{"key": "COW-13"}`)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Text, "404 `not_found`: no such ticket", "the API's error with its code (docs/adr/0042 D4)")
}

func TestSearch(t *testing.T) {
	f := newFake(t)
	f.on("GET /api/v1/tenants/acme/projects/COW/tickets", http.StatusOK, list(ticket("acme/COW-3", "decided")))
	f.on("GET /api/v1/tenants/acme/tickets", http.StatusOK, list(ticket("acme/COW-4", "filed")))
	f.on("GET /api/v1/tenants/beta/tickets", http.StatusOK, list(ticket("beta/OPS-1", "done")))
	f.on("GET /api/v1/me", http.StatusOK, map[string]any{"id": uuid.NewString(), "display_name": "Ada", "global_admin": false,
		"local": true, "password_change_required": false, "memberships": []any{
			map[string]any{"tenant": map[string]any{"slug": "acme", "name": "Acme"}, "role": "member"},
			map[string]any{"tenant": map[string]any{"slug": "beta", "name": "Beta"}, "role": "member"}}})

	res := call(t, f.session(true), "search", `{"query": "export", "state": ["decided"], "assigned_to_me": true}`)
	require.False(t, res.IsError, res.Text)
	assert.Contains(t, res.Text, "Tickets in acme/COW matching \"export\"")
	assert.Contains(t, res.Text, "acme/COW-3")
	q, _ := url.ParseQuery(f.calls(http.MethodGet, "/api/v1/tenants/acme/projects/COW/tickets")[0].Query)
	assert.Equal(t, "export", q.Get("q"))
	assert.Equal(t, []string{"decided"}, q["state"])
	assert.Equal(t, []string{"me"}, q["assignee"])

	res = call(t, f.session(true), "search", `{"query": "x", "scope": "tenant"}`)
	assert.Contains(t, res.Text, "acme/COW-4")
	res = call(t, f.session(false), "search", `{"query": "x"}`)
	require.False(t, res.IsError, res.Text)
	assert.Contains(t, res.Text, "every tenant of the person")
	assert.Contains(t, res.Text, "acme/COW-4")
	assert.Contains(t, res.Text, "beta/OPS-1")
	res = call(t, f.session(false), "search", `{"query": "x", "scope": "tenant"}`)
	assert.True(t, res.IsError)

	// Without words a search lists the project in rank order, and sends no q;
	// outside one project it asks for words.
	listed := len(f.calls(http.MethodGet, "/api/v1/tenants/acme/projects/COW/tickets"))
	for _, args := range []string{`{}`, `{"query": " "}`} {
		res = call(t, f.session(true), "search", args)
		require.False(t, res.IsError, res.Text)
		assert.Contains(t, res.Text, "Tickets in acme/COW, in rank order:")
		assert.Contains(t, res.Text, "acme/COW-3")
	}
	for _, sent := range f.calls(http.MethodGet, "/api/v1/tenants/acme/projects/COW/tickets")[listed:] {
		q, _ := url.ParseQuery(sent.Query)
		assert.False(t, q.Has("q"), sent.Query)
	}
	res = call(t, f.session(true), "search", `{"scope": "tenant"}`)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Text, "give words to find, or search one project to list its tickets")
	res = call(t, f.session(false), "search", `{"query": ""}`)
	assert.True(t, res.IsError)
}

// file_ticket files with an Idempotency-Key (docs/adr/0045 D5), links in
// both directions and answers the canonical key.
func TestFileTicket(t *testing.T) {
	f := newFake(t)
	f.on("POST /api/v1/tenants/acme/projects/COW/tickets", http.StatusCreated, ticket("acme/COW-12", "filed"))
	f.on("PUT /api/v1/tenants/acme/projects/COW/tickets/12/links/{type}/{other}", http.StatusCreated, map[string]any{})
	f.on("PUT /api/v1/tenants/acme/projects/COW/tickets/3/links/{type}/{other}", http.StatusCreated, map[string]any{})

	res := call(t, f.session(true), "file_ticket", `{"type": "task", "title": "Ship it", "severity": "medium",
		"security": "none", "effort": "S", "body": "## Current state", "links": [
		{"type": "relates-to", "key": "COW-5"}, {"type": "blocks", "key": "COW-3", "direction": "incoming"},
		{"type": "blocks", "key": "other/OPS-1"}]}`)
	require.False(t, res.IsError, res.Text)
	assert.Contains(t, res.Text, "Filed acme/COW-12 — Ship it (task, filed), in the horizon later.")
	assert.Contains(t, res.Text, "Linked: acme/COW-12 relates-to acme/COW-5.")
	assert.Contains(t, res.Text, "Linked: acme/COW-3 blocks acme/COW-12.")
	assert.Contains(t, res.Text, "a link stays inside one tenant")
	posted := f.calls(http.MethodPost, "/api/v1/tenants/acme/projects/COW/tickets")
	require.Len(t, posted, 1)
	_, err := uuid.Parse(posted[0].Header.Get("Idempotency-Key"))
	assert.NoError(t, err, "a creating POST carries a key the tool made")
	body := decodeBody(t, posted[0])
	assert.Equal(t, "## Current state", body["body"])
	assert.NotContains(t, body, "threat")
	assert.Len(t, f.calls(http.MethodPut, "/api/v1/tenants/acme/projects/COW/tickets/12/links/relates-to/COW-5"), 1)
	assert.Len(t, f.calls(http.MethodPut, "/api/v1/tenants/acme/projects/COW/tickets/3/links/blocks/COW-12"), 1)

	res = call(t, f.session(false), "file_ticket", `{"type": "task", "title": "x", "severity": "low", "security": "none", "effort": "S"}`)
	assert.True(t, res.IsError, "an unbound session names the project")
	res = call(t, f.session(false), "file_ticket", `{"project": "acme/COW", "type": "task", "title": "x", "severity": "low", "security": "none", "effort": "S"}`)
	assert.False(t, res.IsError, res.Text)
}

// file_ticket files into a horizon at a place in one request, the neighbour a
// ticket of the same project; two neighbours, or one elsewhere, send nothing
// (docs/adr/0010 D3, docs/adr/0014 D2).
func TestFileTicketIntoAHorizon(t *testing.T) {
	f := newFake(t)
	f.on("POST /api/v1/tenants/acme/projects/COW/tickets", http.StatusCreated, ticket("acme/COW-12", "filed", func(m map[string]any) {
		m["horizon"] = "next"
	}))
	s := f.session(true)
	const filing = `"type": "feature", "title": "An idea", "severity": "low", "security": "none", "effort": "M"`

	res := call(t, s, "file_ticket", `{`+filing+`, "horizon": "next", "after": "COW-3"}`)
	require.False(t, res.IsError, res.Text)
	assert.Contains(t, res.Text, "Filed acme/COW-12 — Ship it (task, filed), in the horizon next, directly after COW-3.")
	posted := f.calls(http.MethodPost, "/api/v1/tenants/acme/projects/COW/tickets")
	require.Len(t, posted, 1)
	body := decodeBody(t, posted[0])
	assert.Equal(t, "next", body["horizon"])
	assert.Equal(t, float64(3), body["after"])
	assert.NotContains(t, body, "before")

	for args, want := range map[string]string{
		`{` + filing + `, "after": "COW-3", "before": "COW-4"}`: "not both",
		`{` + filing + `, "after": "OPS-3"}`:                    "same project",
		`{` + filing + `, "horizon": "soon"}`:                   "horizon",
	} {
		res := call(t, s, "file_ticket", args)
		assert.True(t, res.IsError, args)
		assert.Contains(t, res.Text, want, args)
	}
	assert.Len(t, f.calls(http.MethodPost, "/api/v1/tenants/acme/projects/COW/tickets"), 1, "nothing was sent for the refused calls")
}

// record_state replaces the body with the version it read; a stale version
// is the API's 412, surfaced (docs/adr/0050).
func TestRecordState(t *testing.T) {
	f := newFake(t)
	f.on("GET "+ticketPath, http.StatusOK, ticket("acme/COW-12", "in-progress"), "ETag", `"3"`)
	f.on("PUT "+ticketPath+"/body", http.StatusOK, ticket("acme/COW-12", "in-progress", func(m map[string]any) { m["version"] = 4 }))
	res := call(t, f.session(true), "record_state", `{"key": "COW-12", "body": "## Current state\n\nDone half.", "comment": "why"}`)
	require.False(t, res.IsError, res.Text)
	assert.Equal(t, "Recorded the current state of acme/COW-12 (version 4).", res.Text)
	put := f.calls(http.MethodPut, ticketPath+"/body")
	require.Len(t, put, 1)
	assert.Equal(t, `"3"`, put[0].Header.Get("If-Match"))
	assert.Equal(t, "why", decodeBody(t, put[0])["comment"])

	g := newFake(t)
	g.on("GET "+ticketPath, http.StatusOK, ticket("acme/COW-12", "in-progress"), "ETag", `"3"`)
	g.refuse("PUT "+ticketPath+"/body", http.StatusPreconditionFailed, "precondition_failed", "the entity changed since you read it")
	res = call(t, g.session(true), "record_state", `{"key": "COW-12", "body": "x"}`)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Text, "412 `precondition_failed`")
	assert.Len(t, g.calls(http.MethodPut, ticketPath+"/body"), 1, "never retried into success (docs/adr/0040 D3)")
}

func TestCommentLinkAndWatch(t *testing.T) {
	f := newFake(t)
	f.on("POST "+ticketPath+"/comments", http.StatusCreated, map[string]any{"id": uuid.NewString()})
	f.on("GET "+ticketPath, http.StatusOK, ticket("acme/COW-12", "in-progress"), "ETag", `"3"`)
	f.on("PUT "+ticketPath+"/links/blocks/COW-3", http.StatusOK, map[string]any{})
	f.on("PUT "+ticketPath+"/interest", http.StatusCreated, map[string]any{})
	s := f.session(true)

	res := call(t, s, "comment", `{"key": "COW-12", "text": "Seen."}`)
	require.False(t, res.IsError, res.Text)
	assert.Contains(t, res.Text, "Commented on acme/COW-12")
	assert.Contains(t, res.Text, "Cowork-Ticket: acme/COW-12")
	c := f.calls(http.MethodPost, ticketPath+"/comments")[0]
	assert.NotEmpty(t, c.Header.Get("Idempotency-Key"))
	assert.Equal(t, "Seen.", decodeBody(t, c)["body"])

	res = call(t, s, "link", `{"key": "COW-12", "type": "blocks", "other_key": "acme/COW-3"}`)
	require.False(t, res.IsError, res.Text)
	assert.Equal(t, "Linked: acme/COW-12 blocks acme/COW-3.", res.Text)
	res = call(t, s, "link", `{"key": "COW-12", "type": "blocks", "other_key": "beta/COW-3"}`)
	assert.True(t, res.IsError)

	res = call(t, s, "watch", `{"key": "COW-12"}`)
	require.False(t, res.IsError, res.Text)
	assert.Equal(t, "watch", decodeBody(t, f.calls(http.MethodPut, ticketPath+"/interest")[0])["weight"])
	assert.NotContains(t, decodeBody(t, c), "mentions", "a comment that mentions nobody sends no list")
}

// comment mentions persons named as open_question names the person asked —
// me, a username, a display name, an id — and sends their ids beside the text,
// each once (docs/adr/0015 D5); a name that is no member is refused before
// anything is written.
func TestCommentMentions(t *testing.T) {
	f := newFake(t)
	f.on("POST "+ticketPath+"/comments", http.StatusCreated, map[string]any{"id": uuid.NewString()})
	f.on("GET "+ticketPath, http.StatusOK, ticket("acme/COW-12", "in-progress"), "ETag", `"3"`)
	f.on("GET /api/v1/tenants/acme/members", http.StatusOK, list(
		map[string]any{"person": map[string]any{"id": adaID, "username": "ada", "display_name": "Ada"}, "role": "admin"},
		map[string]any{"person": map[string]any{"id": samID, "username": "sam", "display_name": "Sam Doe"}, "role": "member"}))
	f.on("GET /api/v1/me", http.StatusOK, map[string]any{"id": adaID, "display_name": "Ada", "memberships": []any{}})
	s := f.session(true)

	res := call(t, s, "comment", `{"key": "COW-12", "text": "@Sam Doe, @Ada: done.", "mentions": ["Sam Doe", "me", "sam"]}`)
	require.False(t, res.IsError, res.Text)
	assert.Contains(t, res.Text, "mentioning Sam Doe, Ada")
	body := decodeBody(t, f.calls(http.MethodPost, ticketPath+"/comments")[0])
	assert.Equal(t, "@Sam Doe, @Ada: done.", body["body"])
	assert.Equal(t, []any{samID, adaID}, body["mentions"], "each person once, by id")

	res = call(t, s, "comment", `{"key": "COW-12", "text": "x", "mentions": ["nobody"]}`)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Text, "no member of the tenant acme")
	assert.Len(t, f.calls(http.MethodPost, ticketPath+"/comments"), 1, "nothing written for a name that is no member")
}

// place_ticket moves a ticket to another horizon with a reason and the
// version read, places it next to a ticket of that horizon, or both; without a
// reason, next to a ticket of another horizon, next to itself or with nothing
// to do, nothing is sent; a refusal is the API's (docs/adr/0010 D3,
// docs/adr/0014 D2, docs/adr/0042 D3).
func TestPlaceTicket(t *testing.T) {
	f := newFake(t)
	f.on("GET "+ticketPath, http.StatusOK, ticket("acme/COW-12", "decided"), "ETag", `"3"`)
	f.on("GET /api/v1/tenants/acme/projects/COW/tickets/3", http.StatusOK, ticket("acme/COW-3", "filed", func(m map[string]any) {
		m["horizon"] = "next"
	}))
	f.on("GET /api/v1/tenants/acme/projects/COW/tickets/4", http.StatusOK, ticket("acme/COW-4", "in-progress"))
	f.on("PUT "+ticketPath+"/horizon", http.StatusOK, ticket("acme/COW-12", "decided", func(m map[string]any) {
		m["horizon"] = "next"
	}))
	f.on("PUT "+ticketPath+"/rank", http.StatusOK, ticket("acme/COW-12", "decided"))
	s := f.session(true)

	res := call(t, s, "place_ticket", `{"key": "COW-12", "horizon": "next", "after": "COW-3", "reason": "the person wants it next"}`)
	require.False(t, res.IsError, res.Text)
	assert.Equal(t, "Moved acme/COW-12 from the horizon later to next, with the reason: the person wants it next.\n"+
		"Placed acme/COW-12, directly after COW-3.", res.Text)
	put := f.calls(http.MethodPut, ticketPath+"/horizon")
	require.Len(t, put, 1)
	assert.Equal(t, `"3"`, put[0].Header.Get("If-Match"))
	assert.Equal(t, map[string]any{"value": "next", "reason": "the person wants it next"}, decodeBody(t, put[0]))
	rank := f.calls(http.MethodPut, ticketPath+"/rank")
	require.Len(t, rank, 1)
	assert.Equal(t, map[string]any{"after": float64(3)}, decodeBody(t, rank[0]))

	res = call(t, s, "place_ticket", `{"key": "COW-12", "before": "COW-4"}`)
	require.False(t, res.IsError, res.Text)
	assert.Equal(t, "Placed acme/COW-12, directly before COW-4.", res.Text, "a place in its own horizon, later")
	assert.Equal(t, map[string]any{"before": float64(4)}, decodeBody(t, f.calls(http.MethodPut, ticketPath+"/rank")[1]))

	res = call(t, s, "place_ticket", `{"key": "COW-12", "horizon": "later", "reason": "x"}`)
	require.False(t, res.IsError, res.Text)
	assert.Equal(t, "acme/COW-12 stands in the horizon later already.", res.Text)

	for args, want := range map[string]string{
		`{"key": "COW-12", "horizon": "now"}`:                                   "needs a reason",
		`{"key": "COW-12", "horizon": "now", "reason": "  "}`:                   "needs a reason",
		`{"key": "COW-12"}`:                                                     "pass a horizon",
		`{"key": "COW-12", "after": "COW-3", "before": "COW-4"}`:                "not both",
		`{"key": "COW-12", "after": "COW-3"}`:                                   "acme/COW-3 stands in the horizon next, not in later",
		`{"key": "COW-12", "horizon": "now", "reason": "x", "before": "COW-4"}`: "acme/COW-4 stands in the horizon later, not in now",
		`{"key": "COW-12", "after": "COW-12"}`:                                  "not placed next to itself",
		`{"key": "COW-12", "after": "OPS-3"}`:                                   "same project",
		`{"key": "COW-12", "horizon": "soon", "reason": "x"}`:                   "horizon",
	} {
		res := call(t, s, "place_ticket", args)
		assert.True(t, res.IsError, args)
		assert.Contains(t, res.Text, want, args)
	}
	assert.Len(t, f.calls(http.MethodPut, ticketPath+"/horizon"), 1, "nothing was sent for the refused calls")
	assert.Len(t, f.calls(http.MethodPut, ticketPath+"/rank"), 2, "nothing was sent for the refused calls")

	g := newFake(t)
	g.on("GET "+ticketPath, http.StatusOK, ticket("acme/COW-12", "decided"), "ETag", `"3"`)
	g.refuse("PUT "+ticketPath+"/horizon", http.StatusForbidden, "agent_forbidden", "missing capability: set-horizon")
	res = call(t, g.session(true), "place_ticket", `{"key": "COW-12", "horizon": "now", "reason": "x"}`)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Text, "403 `agent_forbidden`: missing capability: set-horizon")

	// Back to later is the same route: the API clears the horizon set.
	h := newFake(t)
	h.on("GET "+ticketPath, http.StatusOK, ticket("acme/COW-12", "decided", func(m map[string]any) {
		m["horizon"] = "now"
		m["horizon_set"] = map[string]any{"value": "now", "reason": nil, "by": nil, "at": "2026-10-04T00:00:00Z"}
	}), "ETag", `"5"`)
	h.on("PUT "+ticketPath+"/horizon", http.StatusOK, ticket("acme/COW-12", "decided"))
	res = call(t, h.session(true), "place_ticket", `{"key": "COW-12", "horizon": "later", "reason": "not this month"}`)
	require.False(t, res.IsError, res.Text)
	assert.Equal(t, "Moved acme/COW-12 from the horizon now to later, with the reason: not this month.", res.Text)
	put = h.calls(http.MethodPut, ticketPath+"/horizon")
	require.Len(t, put, 1)
	assert.Equal(t, `"5"`, put[0].Header.Get("If-Match"))
	assert.Equal(t, map[string]any{"value": "later", "reason": "not this month"}, decodeBody(t, put[0]))
}

// A session bound to a tenant and no project — the chat on a page that shows
// none — searches the tenant by default, resolves short keys in it, and asks
// for the project where a tool needs one.
func TestATenantBinding(t *testing.T) {
	f := newFake(t)
	f.on("GET /api/v1/tenants/acme/tickets", http.StatusOK, list(ticket("acme/COW-12", "decided")))
	f.on("GET "+ticketPath, http.StatusOK, ticket("acme/COW-12", "decided"), "ETag", `"3"`)
	s := f.session(false)
	s.BindTenant("acme")

	res := call(t, s, "search", `{"query": "gate"}`)
	require.False(t, res.IsError, res.Text)
	assert.Contains(t, res.Text, "Tickets in the tenant acme matching")
	assert.Len(t, f.calls(http.MethodGet, "/api/v1/tenants/acme/tickets"), 1)
	assert.Empty(t, f.calls(http.MethodGet, "/api/v1/me"), "nothing outside the tenant")

	res = call(t, s, "place_ticket", `{"key": "COW-12", "horizon": "later", "reason": "x"}`)
	assert.Len(t, f.calls(http.MethodGet, ticketPath), 1, "a short key resolves in the tenant: %s", res.Text)

	res = call(t, s, "file_ticket", `{"type": "task", "title": "x", "severity": "low", "security": "none", "effort": "S"}`)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Text, "name the project: its key in the tenant acme")
}
