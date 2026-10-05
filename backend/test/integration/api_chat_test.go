//go:build integration

package integration

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api"
	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/llm"
	"github.com/guided-traffic/cowork/backend/test/stubllm"
)

// withChat configures the chat against the stub: one provider, stub, in one
// of its formats; more providers with more (docs/adr/0076).
func withChat(stub *stubllm.Server, format string, edit ...func(*api.ChatOptions)) func(*api.Options) {
	return func(o *api.Options) {
		c := &api.ChatOptions{Providers: []api.ChatProvider{stubProvider(stub, "stub", format, "stub/model")}, TurnTimeout: time.Minute,
			MaxSteps: 8}
		for _, e := range edit {
			e(c)
		}
		o.Chat = c
	}
}

// stubProvider is a provider of the stub in a format, under an id and a model.
func stubProvider(stub *stubllm.Server, id, format, model string) api.ChatProvider {
	url := stub.OpenAIURL()
	if format == llm.Anthropic {
		url = stub.AnthropicURL()
	}
	provider, err := llm.New(llm.Config{Format: format, URL: url, APIKey: stubllm.Key, Model: model})
	if err != nil {
		panic(err)
	}
	return api.ChatProvider{ID: id, Name: "The stub (" + format + ")", Kind: format, Model: model, Provider: provider}
}

// sseEvent is one event of a turn's stream; a comment is the event ":".
type sseEvent struct {
	Name string
	Data json.RawMessage
}

// readEvents reads a turn's stream to its end.
func readEvents(t *testing.T, res *http.Response) []sseEvent {
	t.Helper()
	var out []sseEvent
	var cur sseEvent
	sc := bufio.NewScanner(res.Body)
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, ":"):
			out = append(out, sseEvent{Name: ":", Data: json.RawMessage(`"` + strings.TrimSpace(line[1:]) + `"`)})
		case strings.HasPrefix(line, "event: "):
			cur.Name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			cur.Data = json.RawMessage(strings.TrimPrefix(line, "data: "))
		case line == "" && cur.Name != "":
			out = append(out, cur)
			cur = sseEvent{}
		}
	}
	require.NoError(t, sc.Err())
	return out
}

// names are the events' names, the pieces of text and the comments left out.
func names(events []sseEvent) []string {
	var out []string
	for _, e := range events {
		if e.Name != "text" && e.Name != ":" {
			out = append(out, e.Name)
		}
	}
	return out
}

func eventData[T any](t *testing.T, e sseEvent) T {
	t.Helper()
	var v T
	require.NoError(t, json.Unmarshal(e.Data, &v), "%s: %s", e.Name, e.Data)
	return v
}

func lastEvent(t *testing.T, events []sseEvent, name string) sseEvent {
	t.Helper()
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Name == name {
			return events[i]
		}
	}
	require.Failf(t, "no event", "the stream has no %s event: %v", name, names(events))
	return sseEvent{}
}

// chatEnv is a world with its accounts and tokens, a stub provider, and the
// browser of the member of tenant A.
type chatEnv struct {
	world
	tk    tokens
	names map[string]string
	stub  *stubllm.Server
	s     apiServer
	b     *browser
	ctx   context.Context
}

func newChatEnv(t *testing.T, format string, edit ...func(*api.ChatOptions)) chatEnv {
	t.Helper()
	w := newWorld(t)
	e := chatEnv{world: w, tk: issueTokens(t, w), names: withAccounts(t, w), stub: stubllm.New(t), ctx: context.Background()}
	e.s = newAPI(t, withLogin, withChat(e.stub, format, edit...))
	e.b = e.s.browser(t)
	e.b.mustLogin(e.names["memberA"], testPassword)
	return e
}

// turn posts a turn of the conversation as the browser.
func (e chatEnv) turn(t *testing.T, b *browser, conversation uuid.UUID, msgs []apigen.ChatMessage) (*http.Response, []sseEvent) {
	t.Helper()
	body := apigen.ChatTurn{Conversation: conversation, Messages: msgs,
		Context: &apigen.ChatPageContext{Path: ptr("/t/" + e.SlugA + "/p/ALPHA/board"), Project: ptr("ALPHA")}}
	res := b.request(http.MethodPost, "/api/v1/tenants/"+e.SlugA+"/chat", body)
	if res.StatusCode != http.StatusOK {
		return res, nil
	}
	return res, readEvents(t, res)
}

func said(text string) apigen.ChatMessage {
	return apigen.ChatMessage{Role: apigen.ChatRoleUser, Text: &text}
}

// docs/adr/0076, the owner's answer of 2026-10-04: the chat is available in
// every tenant once the installation configures a provider — no tenant is
// asked — and the availability lists the providers in their order for every
// member, never an address or a key.
func TestChatAvailability(t *testing.T) {
	w := newWorld(t)
	tk := issueTokens(t, w)
	accounts := withAccounts(t, w)
	none := newAPI(t, withLogin)
	got := decode[map[string]any](t, none.do(t, caller{Token: tk.MemberA}, http.MethodGet, "/api/v1/tenants/"+w.SlugA+"/chat", nil))
	assert.Equal(t, map[string]any{"available": false, "providers": []any{}, "reason": "not_configured"}, got)
	b := none.browser(t)
	b.mustLogin(accounts["memberA"], testPassword)
	assertProblem(t, b.request(http.MethodPost, "/api/v1/tenants/"+w.SlugA+"/chat", apigen.ChatTurn{Conversation: uuid.New(),
		Messages: []apigen.ChatMessage{said("Hello")}}), http.StatusConflict, "chat_unavailable")

	stub := stubllm.New(t)
	two := newAPI(t, withLogin, withChat(stub, llm.OpenAI, func(c *api.ChatOptions) {
		c.Providers = append(c.Providers, stubProvider(stub, "hosted", llm.Anthropic, "stub/hosted"))
	}))
	for _, token := range []string{tk.MemberA, tk.ViewerA, tk.MemberB} {
		slug := w.SlugA
		if token == tk.MemberB {
			slug = w.SlugB
		}
		res := two.do(t, caller{Token: token}, http.MethodGet, "/api/v1/tenants/"+slug+"/chat", nil)
		require.Equal(t, http.StatusOK, res.StatusCode)
		raw := decode[map[string]any](t, res)
		assert.Equal(t, map[string]any{"available": true, "reason": nil, "providers": []any{
			map[string]any{"id": "stub", "name": "The stub (openai)", "kind": "openai", "model": "stub/model"},
			map[string]any{"id": "hosted", "name": "The stub (anthropic)", "kind": "anthropic", "model": "stub/hosted"},
		}}, raw, "every tenant, every member, no address")
	}
}

// docs/adr/0076: a turn talks to the provider the person picked — its wire
// format, its model, its model in the mark of the acts — or to the first;
// one the installation does not configure is refused before the stream.
func TestTheProviderIsThePersonsPick(t *testing.T) {
	hosted := stubllm.New(t)
	e := newChatEnv(t, llm.OpenAI, func(c *api.ChatOptions) {
		c.Providers = append(c.Providers, stubProvider(hosted, "hosted", llm.Anthropic, "stub/hosted"))
	})
	path := "/api/v1/tenants/" + e.SlugA + "/chat"
	turn := func(provider *string) {
		conversation := uuid.Must(uuid.NewV7())
		res := e.b.request(http.MethodPost, path, apigen.ChatTurn{Conversation: conversation, Provider: provider,
			Messages: []apigen.ChatMessage{said("Watch ALPHA-1")}})
		require.Equal(t, http.StatusOK, res.StatusCode)
		assert.Equal(t, "done", lastEvent(t, readEvents(t, res), "done").Name)
	}
	created, err := e.s.client(t, caller{Token: e.tk.MemberA}).CreateTicketWithResponse(e.ctx, e.SlugA, "ALPHA",
		&apigen.CreateTicketParams{}, task("Watched by the hosted model"))
	require.NoError(t, err)
	hosted.Reply(stubllm.Reply{Calls: []stubllm.Call{{ID: "toolu_watch", Name: "watch",
		Arguments: fmt.Sprintf(`{"key": "ALPHA-%d"}`, created.JSON201.Number)}}}, stubllm.Reply{Text: "Watching."})
	turn(ptr("hosted"))
	require.Len(t, hosted.Requests(), 2)
	assert.Equal(t, "anthropic", hosted.Requests()[0].Format)
	assert.Equal(t, "stub/hosted", hosted.Requests()[0].Model)
	assert.Empty(t, e.stub.Requests(), "the default provider was not asked")
	n, err := fixtures(t).QueryCount(e.ctx, `SELECT count(*) FROM audit_events WHERE ticket_key = $1 AND action = 'interest'
		AND agent LIKE 'chat/stub:hosted/%'`, created.JSON201.Key)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n, "the mark names the picked provider's model")

	e.stub.Reply(stubllm.Reply{Text: "The default."})
	turn(nil)
	require.Len(t, e.stub.Requests(), 1, "no provider named: the first configured")
	assert.Len(t, hosted.Requests(), 2)

	problemOf := assertProblem(t, e.b.request(http.MethodPost, path, apigen.ChatTurn{Conversation: uuid.New(), Provider: ptr("gone"),
		Messages: []apigen.ChatMessage{said("Hello")}}), http.StatusBadRequest, "validation_failed")
	assert.Equal(t, "/provider", problemOf["errors"].([]any)[0].(map[string]any)["pointer"])
}

var filedKey = regexp.MustCompile(`Filed (\S+) —`)

// docs/adr/0076: a turn files a ticket and ranks it to now through the API as
// the person's agent — every act the person's, marked as the chat's, a key on
// the creation — while the person watches the events arrive; the model reads
// each tool's answer, and never the provider's address or the api tool.
func TestAChatTurnFilesARankedTicket(t *testing.T) {
	e := newChatEnv(t, llm.OpenAI)
	e.stub.Reply(stubllm.Reply{Text: "Filing it.", Calls: []stubllm.Call{{ID: "call_file", Name: "file_ticket",
		Arguments: `{"type": "bug", "title": "The gate stays open", "severity": "high", "security": "none", "effort": "S"}`}}})
	e.stub.ReplyWith(func(r stubllm.Request) stubllm.Reply {
		key := filedKey.FindStringSubmatch(r.LastToolText())
		if key == nil {
			return stubllm.Reply{Text: "I could not file it."}
		}
		return stubllm.Reply{Calls: []stubllm.Call{{ID: "call_rank", Name: "place_ticket",
			Arguments: fmt.Sprintf(`{"key": %q, "horizon": "now", "reason": "the person asked to rank it to now"}`, key[1])}}}
	})
	e.stub.Reply(stubllm.Reply{Text: "Filed it and ranked it to now."})

	conversation := uuid.Must(uuid.NewV7())
	res, events := e.turn(t, e.b, conversation, []apigen.ChatMessage{said("File a bug: the gate stays open. Rank it to now.")})
	require.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, "text/event-stream", res.Header.Get("Content-Type"))
	assert.Equal(t, "no", res.Header.Get("X-Accel-Buffering"))
	assert.Equal(t, []string{"tool_call", "tool_result", "tool_call", "tool_result", "done"}, names(events))
	done := eventData[apigen.ChatDoneEvent](t, lastEvent(t, events, "done"))
	assert.Equal(t, apigen.ChatTurnEndAnswered, done.Reason)
	require.Len(t, done.Messages, 5)
	for _, ev := range events {
		if ev.Name == "tool_result" {
			assert.True(t, eventData[apigen.ChatToolResultEvent](t, ev).Ok, string(ev.Data))
		}
	}

	key := filedKey.FindStringSubmatch(*done.Messages[1].Text)[1]
	parts := strings.SplitN(key, "/", 2)
	member := e.s.client(t, caller{Token: e.tk.MemberA})
	ticket, err := member.ResolveTicketWithResponse(e.ctx, e.SlugA, parts[1])
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, ticket.StatusCode())
	assert.Equal(t, apigen.HorizonNow, ticket.JSON200.Horizon)
	assert.Equal(t, "The gate stays open", ticket.JSON200.Title)

	mark := "chat/stub:model/" + conversation.String()
	for action, keyed := range map[string]bool{"created": true, "overridden": false} {
		n, err := fixtures(t).QueryCount(e.ctx, `SELECT count(*) FROM audit_events WHERE ticket_key = $1 AND action = $2
			AND actor_user_id = $3 AND agent = $4 AND token_id IS NULL AND agent_capabilities = $6::text[]
			AND (idempotency_key IS NOT NULL OR NOT $5)`, key, action, e.MemberA, mark, keyed, auth.DefaultChatCapabilities)
		require.NoError(t, err)
		assert.EqualValues(t, 1, n, "%s: the person's act, marked as the chat's, with the default capabilities (docs/adr/0036, 0043 D5)", action)
	}

	got := e.stub.Requests()
	require.Len(t, got, 3)
	first := got[0]
	assert.Contains(t, first.System, fmt.Sprintf(`the tenant "Tenant A" (%s)`, e.SlugA))
	assert.Contains(t, first.System, "/t/"+e.SlugA+"/p/ALPHA/board")
	assert.NotContains(t, first.Tools, "api")
	assert.Contains(t, first.Tools, "place_ticket")
	assert.Contains(t, first.Tools, "open_board")
	assert.Equal(t, "stub/model", first.Model)
	assert.Contains(t, got[1].LastToolText(), "Filed "+key)
}

// docs/adr/0043 D5, the owner's answers of 2026-10-04: nothing waits for the
// person, and the chat holds the capabilities the person chose — by default
// not close, so the API refuses the chat's close and the model reads why; the
// person gives it close in a session, and the next turn closes at once, the
// act marked as the chat's with the chosen set. A token cannot choose, nor
// can the chat itself, and the choice is the person's recorded act.
func TestTheChatHoldsThePersonsCapabilities(t *testing.T) {
	e := newChatEnv(t, llm.Anthropic)
	member := e.s.client(t, caller{Token: e.tk.MemberA})
	created, err := member.CreateTicketWithResponse(e.ctx, e.SlugA, "ALPHA", &apigen.CreateTicketParams{}, task("Close me"))
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, created.StatusCode(), string(created.Body))
	tk := *created.JSON201
	for _, to := range []apigen.TicketState{apigen.TicketStateAnalysed, apigen.TicketStateDecided, apigen.TicketStateInProgress} {
		cur, err := member.GetTicketWithResponse(e.ctx, e.SlugA, "ALPHA", tk.Number)
		require.NoError(t, err)
		res, err := member.TransitionTicketWithResponse(e.ctx, e.SlugA, "ALPHA", tk.Number, &apigen.TransitionTicketParams{},
			apigen.Transition{From: cur.JSON200.State, To: to})
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	}
	stateOf := func() apigen.TicketState {
		res, err := member.GetTicketWithResponse(e.ctx, e.SlugA, "ALPHA", tk.Number)
		require.NoError(t, err)
		return res.JSON200.State
	}
	short := fmt.Sprintf("ALPHA-%d", tk.Number)
	closeIt := func() []sseEvent {
		e.stub.Reply(stubllm.Reply{Calls: []stubllm.Call{{ID: "toolu_close", Name: "transition",
			Arguments: fmt.Sprintf(`{"key": %q, "to": "done", "reason_or_note": "go test ./... passed"}`, short)}}},
			stubllm.Reply{Text: "Reported."})
		res, events := e.turn(t, e.b, uuid.Must(uuid.NewV7()), []apigen.ChatMessage{said("Close " + short)})
		require.Equal(t, http.StatusOK, res.StatusCode)
		assert.Equal(t, []string{"tool_call", "tool_result", "done"}, names(events), "the call runs at once, nothing waits")
		return events
	}

	mine := decode[map[string]any](t, e.b.request(http.MethodGet, "/api/v1/me/chat", nil))
	assert.Equal(t, map[string]any{"chosen": false, "capabilities": []any{"rank", "set-horizon", "interest", "upload", "create-project"}},
		mine, "the default leaves decide, close, drop and record-answer to the person")
	refused := eventData[apigen.ChatToolResultEvent](t, lastEvent(t, closeIt(), "tool_result"))
	assert.False(t, refused.Ok)
	assert.Contains(t, refused.Summary, "`agent_forbidden`: missing capability: close")
	assert.Equal(t, apigen.TicketStateInProgress, stateOf())
	got := e.stub.Requests()
	assert.Contains(t, got[0].System, "these capabilities: rank, set-horizon, interest, upload, create-project.")

	path := "/api/v1/me/chat"
	choice := map[string]any{"capabilities": []string{"close", "rank", "close"}}
	assertProblem(t, e.b.request(http.MethodPut, path, choice), http.StatusBadRequest, "validation_failed")
	choice = map[string]any{"capabilities": []string{"rank", "close"}}
	assertProblem(t, e.s.do(t, caller{Token: e.tk.MemberA}, http.MethodPut, path, choice), http.StatusForbidden, "session_required")
	assertProblem(t, e.b.request(http.MethodPut, path, choice, withHeader("X-Cowork-Agent", "chat/stub:model/x")),
		http.StatusForbidden, "agent_forbidden")
	res := e.b.request(http.MethodPut, path, choice)
	require.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, map[string]any{"chosen": true, "capabilities": []any{"close", "rank"}}, decode[map[string]any](t, res),
		"in the order of the catalogue")
	assert.Equal(t, map[string]any{"chosen": true, "capabilities": []any{"close", "rank"}},
		decode[map[string]any](t, e.s.do(t, caller{Token: e.tk.MemberA}, http.MethodGet, path, nil)), "a token reads it")
	recorded, err := fixtures(t).QueryCount(e.ctx, `SELECT count(*) FROM audit_events WHERE tenant_id IS NULL AND entity_type = 'user'
		AND entity_id = $1 AND action = 'updated' AND actor_user_id = $1
		AND before = '{"chat_capabilities": ["rank", "set-horizon", "interest", "upload", "create-project"]}'
		AND after = '{"chat_capabilities": ["close", "rank"]}'`, e.MemberA)
	require.NoError(t, err)
	assert.EqualValues(t, 1, recorded, "the choice is the person's recorded act")
	require.Equal(t, http.StatusOK, e.b.request(http.MethodPut, path, choice).StatusCode)
	recorded, err = fixtures(t).QueryCount(e.ctx, `SELECT count(*) FROM audit_events WHERE entity_id = $1 AND action = 'updated'
		AND after ? 'chat_capabilities'`, e.MemberA)
	require.NoError(t, err)
	assert.EqualValues(t, 1, recorded, "the same set again changes nothing")

	done := eventData[apigen.ChatToolResultEvent](t, lastEvent(t, closeIt(), "tool_result"))
	assert.True(t, done.Ok, done.Summary)
	assert.Equal(t, apigen.TicketStateDone, stateOf())
	n, err := fixtures(t).QueryCount(e.ctx, `SELECT count(*) FROM audit_events WHERE ticket_key = $1 AND action = 'transitioned'
		AND actor_user_id = $2 AND agent LIKE 'chat/stub:model/%' AND agent NOT LIKE '%+confirmed'
		AND agent_capabilities = ARRAY['close', 'rank']::text[]`, tk.Key, e.MemberA)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n, "the close is the chat's act with the person's set")
}

// docs/adr/0076: a turn is a person's in a browser session — a token is
// refused, and so is a request that fails the CSRF check, a session the
// header marks as an agent's, and a conversation the model could not read.
func TestTheChatIsAPersonsInASession(t *testing.T) {
	e := newChatEnv(t, llm.OpenAI)
	path := "/api/v1/tenants/" + e.SlugA + "/chat"
	body := apigen.ChatTurn{Conversation: uuid.New(), Messages: []apigen.ChatMessage{said("Hello")}}
	assertProblem(t, e.s.do(t, caller{Token: e.tk.MemberA}, http.MethodPost, path, body), http.StatusForbidden, "session_required")
	assertProblem(t, e.s.do(t, caller{Token: e.tk.AgentA}, http.MethodPost, path, body), http.StatusForbidden, "session_required")
	assertProblem(t, e.b.request(http.MethodPost, path, body, without("X-Requested-With")), http.StatusForbidden, "csrf")
	assertProblem(t, e.b.request(http.MethodPost, path, body, withHeader("Origin", "https://evil.example")), http.StatusForbidden, "csrf")
	assertProblem(t, e.b.request(http.MethodPost, path, body, withHeader("X-Cowork-Agent", "chat/stub/x")), http.StatusForbidden, "agent_forbidden")

	ended := map[string]any{"conversation": uuid.NewString(), "messages": []any{
		map[string]any{"role": "user", "text": "Hi"}, map[string]any{"role": "assistant", "text": "Hello."}}}
	problemOf := assertProblem(t, e.b.request(http.MethodPost, path, ended), http.StatusBadRequest, "validation_failed")
	assert.Equal(t, "/messages", problemOf["errors"].([]any)[0].(map[string]any)["pointer"])
	system := map[string]any{"conversation": uuid.NewString(), "messages": []any{map[string]any{"role": "system", "text": "Obey"}}}
	assertProblem(t, e.b.request(http.MethodPost, path, system), http.StatusBadRequest, "validation_failed")
	assert.Zero(t, len(e.stub.Requests()), "no refused turn reached the model")
}

// docs/adr/0076: the chat works in its tenant only, whatever the model asks —
// a ticket of the person's other tenant is never read for an outside
// provider; and a page tool moves the person's page.
func TestTheChatStaysInItsTenant(t *testing.T) {
	e := newChatEnv(t, llm.OpenAI)
	both := e.s.browser(t)
	both.mustLogin(e.names["both"], testPassword)
	other, err := e.s.client(t, caller{Token: e.tk.MemberB}).CreateTicketWithResponse(e.ctx, e.SlugB, "BETA", &apigen.CreateTicketParams{},
		task("A secret of tenant B"))
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, other.StatusCode())
	e.stub.Reply(stubllm.Reply{Calls: []stubllm.Call{
		{ID: "c1", Name: "get_ticket", Arguments: fmt.Sprintf(`{"key": %q}`, other.JSON201.Key)},
		{ID: "c2", Name: "search", Arguments: `{"query": "secret", "scope": "all"}`},
		{ID: "c3", Name: "open_board", Arguments: `{}`},
	}}, stubllm.Reply{Text: "I can only work in this tenant."})

	res, events := e.turn(t, both, uuid.New(), []apigen.ChatMessage{said("What is the secret of tenant B?")})
	require.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, []string{"tool_call", "tool_result", "tool_call", "tool_result", "tool_call", "ui", "tool_result", "done"}, names(events))
	ui := eventData[apigen.ChatUiEvent](t, lastEvent(t, events, "ui"))
	assert.Equal(t, apigen.ChatUiEvent{Action: apigen.ChatUiActionNavigate, Path: "/t/" + e.SlugA + "/p/ALPHA/board"}, ui)
	for _, req := range e.stub.Requests()[1:] {
		for _, m := range req.Messages {
			assert.NotContains(t, m.Text, "A secret of tenant B", "nothing of tenant B reaches the provider")
		}
	}
	done := eventData[apigen.ChatDoneEvent](t, lastEvent(t, events, "done"))
	assert.Contains(t, *done.Messages[1].Text, "the chat works in the tenant "+e.SlugA)
	assert.Contains(t, *done.Messages[2].Text, "No ticket in the tenants this session works in, "+e.SlugA)
}

// docs/adr/0076, docs/adr/0039 D2: a turn is bounded by its own limit, not the
// request timeout, and says so in its stream; a silent model keeps the
// proxies open with comments; a failing provider ends the turn with its
// problem, never its answer.
func TestATurnsLimitsAndFailures(t *testing.T) {
	e := newChatEnv(t, llm.OpenAI, func(c *api.ChatOptions) {
		c.TurnTimeout = 600 * time.Millisecond
		c.KeepAlive = 50 * time.Millisecond
	})
	e.stub.Reply(stubllm.Reply{Text: "Too late.", Delay: 3 * time.Second})
	res, events := e.turn(t, e.b, uuid.New(), []apigen.ChatMessage{said("Think hard")})
	require.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, []string{"error", "done"}, names(events))
	problemOf := eventData[map[string]any](t, lastEvent(t, events, "error"))
	assert.Equal(t, "timeout", problemOf["code"])
	assert.EqualValues(t, http.StatusGatewayTimeout, problemOf["status"])
	assert.Equal(t, res.Header.Get("X-Request-Id"), problemOf["request_id"])
	assert.Equal(t, apigen.ChatTurnEndError, eventData[apigen.ChatDoneEvent](t, lastEvent(t, events, "done")).Reason)
	assert.Equal(t, ":", events[0].Name, "the silence was kept alive")

	e.stub.Reply(stubllm.Reply{Status: http.StatusUnauthorized, Body: `{"error": {"message": "invalid key ` + stubllm.Key + `"}}`})
	_, events = e.turn(t, e.b, uuid.New(), []apigen.ChatMessage{said("Hello")})
	problemOf = eventData[map[string]any](t, lastEvent(t, events, "error"))
	assert.Equal(t, "chat_provider_failed", problemOf["code"])
	assert.Equal(t, "the provider answered 401: it refused the key", problemOf["detail"])
	assert.NotContains(t, string(lastEvent(t, events, "error").Data), stubllm.Key)
}

// docs/adr/0036 D3, docs/adr/0043 D5: the agent header makes a session's
// request an agent's — the person's chat capabilities, the hard-off list, a
// key on its POSTs, the mark on its acts — and a malformed one is refused.
func TestTheAgentHeaderOnASession(t *testing.T) {
	e := newChatEnv(t, llm.OpenAI)
	path := "/api/v1/tenants/" + e.SlugA + "/projects/ALPHA/tickets"
	mark := withHeader("X-Cowork-Agent", "claude-code/opus/s1")
	assertProblem(t, e.b.request(http.MethodPost, path, task("Keyless"), mark), http.StatusBadRequest, "idempotency_key_required")
	res := e.b.request(http.MethodPost, path, task("Keyed"), mark, withHeader("Idempotency-Key", uuid.NewString()))
	require.Equal(t, http.StatusCreated, res.StatusCode)
	created := decode[apigen.Ticket](t, res)
	n, err := fixtures(t).QueryCount(e.ctx, `SELECT count(*) FROM audit_events WHERE ticket_key = $1 AND action = 'created'
		AND agent = 'claude-code/opus/s1' AND actor_user_id = $2 AND token_id IS NULL AND agent_capabilities = $3::text[]`, created.Key, e.MemberA,
		auth.DefaultChatCapabilities)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)
	assert.Equal(t, http.StatusCreated, e.b.request(http.MethodPost, path, task("A person's"), withHeader("Idempotency-Key", uuid.NewString())).StatusCode,
		"without the header the session is the person's")

	assertProblem(t, e.b.request(http.MethodPost, "/api/v1/me/tokens", map[string]any{"name": "x", "scope": "write"}, mark),
		http.StatusForbidden, "agent_forbidden")
	admin := e.s.browser(t)
	admin.mustLogin(e.names["adminA"], testPassword)
	assertProblem(t, admin.request(http.MethodPatch, "/api/v1/tenants/"+e.SlugA, map[string]any{"name": "Renamed"}, mark,
		withHeader("If-Match", `"1"`)), http.StatusForbidden, "agent_forbidden")
	assertProblem(t, e.b.request(http.MethodGet, "/api/v1/me", nil, withHeader("X-Cowork-Agent", "no-slashes")),
		http.StatusBadRequest, "validation_failed")
	assert.Equal(t, http.StatusOK, e.b.request(http.MethodGet, "/api/v1/me", nil, mark).StatusCode)
}

// postTurn posts a turn outside the test's goroutine, as a second tab would.
func postTurn(e chatEnv, body apigen.ChatTurn) (*http.Response, error) {
	return postTurnAs(e, e.b, e.SlugA, body)
}

// postTurnAs posts a turn of a browser in a tenant outside the test's
// goroutine.
func postTurnAs(e chatEnv, b *browser, slug string, body apigen.ChatTurn) (*http.Response, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, e.s.URL+"/api/v1/tenants/"+slug+"/chat", strings.NewReader(string(raw)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", testOrigin)
	req.Header.Set("X-Requested-With", "cowork")
	req.AddCookie(&http.Cookie{Name: "__Host-cowork-session", Value: b.Cookie})
	return browserClient.Do(req)
}

// docs/adr/0076, docs/adr/0054 D9: a person runs as many turns at once as
// COWORK_CHAT_TURNS_PER_PERSON allows, and one more is 429 before its stream;
// the server's shutdown ends a running turn at once, which says so.
func TestTheChatsTurnLimitAndShutdown(t *testing.T) {
	shutdown, stop := context.WithCancel(context.Background())
	defer stop()
	e := newChatEnv(t, llm.OpenAI, func(c *api.ChatOptions) {
		c.TurnsPerPerson = 1
		c.Shutdown = shutdown
	})
	e.stub.Reply(stubllm.Reply{Text: "Slow.", Delay: 10 * time.Second})
	type result struct {
		events []sseEvent
		err    error
	}
	first := make(chan result, 1)
	go func() {
		res, err := postTurn(e, apigen.ChatTurn{Conversation: uuid.New(), Messages: []apigen.ChatMessage{said("Think long")}})
		if err != nil {
			first <- result{err: err}
			return
		}
		defer func() { _ = res.Body.Close() }()
		first <- result{events: readEventsOf(res)}
	}()
	require.Eventually(t, func() bool { return len(e.stub.Requests()) == 1 }, 5*time.Second, 10*time.Millisecond, "the first turn asks the model")

	busy, _ := e.turn(t, e.b, uuid.New(), []apigen.ChatMessage{said("And this")})
	assertProblem(t, busy, http.StatusTooManyRequests, "chat_busy")

	started := time.Now()
	stop()
	r := <-first
	require.NoError(t, r.err)
	assert.Less(t, time.Since(started), 5*time.Second, "the turn ended at the shutdown, not with the model")
	assert.Equal(t, []string{"error", "done"}, names(r.events))
	problemOf := eventData[map[string]any](t, lastEvent(t, r.events, "error"))
	assert.Equal(t, "not_ready", problemOf["code"])
	assert.Contains(t, problemOf["detail"], "shutting down")
}

// readEventsOf reads a stream without a test, for a goroutine.
func readEventsOf(res *http.Response) []sseEvent {
	var out []sseEvent
	var cur sseEvent
	sc := bufio.NewScanner(res.Body)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			cur.Name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			cur.Data = json.RawMessage(strings.TrimPrefix(line, "data: "))
		case line == "" && cur.Name != "":
			out = append(out, cur)
			cur = sseEvent{}
		}
	}
	return out
}

// L1: a question asked of "me" is asked of the person the chat acts for —
// the tools know the person, and never ask GET /api/v1/me through the chat.
func TestTheChatAsksOfThePerson(t *testing.T) {
	e := newChatEnv(t, llm.OpenAI)
	member := e.s.client(t, caller{Token: e.tk.MemberA})
	created, err := member.CreateTicketWithResponse(e.ctx, e.SlugA, "ALPHA", &apigen.CreateTicketParams{}, task("Retry or fail?"))
	require.NoError(t, err)
	e.stub.Reply(stubllm.Reply{Calls: []stubllm.Call{{ID: "ask", Name: "open_question", Arguments: fmt.Sprintf(
		`{"key": "ALPHA-%d", "question": "Retry or fail?", "options": "- retry\n- fail", "recommendation": "retry", "asked_of": "me"}`,
		created.JSON201.Number)}}}, stubllm.Reply{Text: "Asked you."})
	_, events := e.turn(t, e.b, uuid.New(), []apigen.ChatMessage{said("Ask me whether to retry")})
	result := eventData[apigen.ChatToolResultEvent](t, events[1])
	require.True(t, result.Ok, result.Summary)
	n, err := fixtures(t).QueryCount(e.ctx, `SELECT count(*) FROM questions WHERE ticket_id = $1 AND asked_of = $2`, created.JSON201.Id, e.MemberA)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)
}

// docs/adr/0076, the owner's answer of 2026-10-04 — "a running agent must be
// stoppable at once": DELETE …/chat/turns ends every running turn of the
// session's person in the tenant within a second, the provider's request
// cancelled and the stream ended with done, stopped; a turn of another person
// and the person's turn in another tenant run on; a token and the chat itself
// cannot stop one.
func TestStopEndsThePersonsRunningTurns(t *testing.T) {
	e := newChatEnv(t, llm.OpenAI)
	admin, both := e.s.browser(t), e.s.browser(t)
	admin.mustLogin(e.names["adminA"], testPassword)
	both.mustLogin(e.names["both"], testPassword)
	// Each turn's model writes its first piece and then stalls, as a slow
	// local model does that does not stop by itself.
	for range 3 {
		e.stub.Reply(stubllm.Reply{Text: "Thinking about it at length", Stall: 30 * time.Second})
	}
	type result struct {
		events []sseEvent
		err    error
	}
	run := func(b *browser, slug string) chan result {
		out := make(chan result, 1)
		go func() {
			res, err := postTurnAs(e, b, slug, apigen.ChatTurn{Conversation: uuid.New(), Messages: []apigen.ChatMessage{said("Think")}})
			if err != nil {
				out <- result{err: err}
				return
			}
			defer func() { _ = res.Body.Close() }()
			out <- result{events: readEventsOf(res)}
		}()
		return out
	}
	mine, theirs, elsewhere := run(both, e.SlugA), run(admin, e.SlugA), run(both, e.SlugB)
	require.Eventually(t, func() bool { return len(e.stub.Requests()) == 3 }, 5*time.Second, 10*time.Millisecond, "the three turns ask the model")

	stopPath := "/api/v1/tenants/" + e.SlugA + "/chat/turns"
	assertProblem(t, e.s.do(t, caller{Token: e.tk.Both}, http.MethodDelete, stopPath, nil), http.StatusForbidden, "session_required")
	assertProblem(t, both.request(http.MethodDelete, stopPath, nil, withHeader("X-Cowork-Agent", "chat/stub:model/x")),
		http.StatusForbidden, "agent_forbidden")
	assertProblem(t, both.request(http.MethodDelete, stopPath, nil, without("X-Requested-With")), http.StatusForbidden, "csrf")
	assert.Zero(t, e.stub.Cancelled(), "nothing refused stopped a turn")

	started := time.Now()
	res := both.request(http.MethodDelete, stopPath, nil)
	require.Equal(t, http.StatusNoContent, res.StatusCode)
	select {
	case r := <-mine:
		require.NoError(t, r.err)
		assert.Less(t, time.Since(started), time.Second, "the turn ended at the stop, not with the model")
		assert.Equal(t, []string{"done"}, names(r.events), "a stop is no failure: no error event")
		assert.Equal(t, apigen.ChatTurnEndStopped, eventData[apigen.ChatDoneEvent](t, lastEvent(t, r.events, "done")).Reason)
	case <-time.After(5 * time.Second):
		t.Fatal("the stopped turn runs on")
	}
	require.Eventually(t, func() bool { return e.stub.Cancelled() == 1 }, time.Second, 10*time.Millisecond,
		"the provider saw its request cancelled")
	select {
	case <-theirs:
		t.Fatal("another person's turn ended with the stop")
	case <-elsewhere:
		t.Fatal("the person's turn in another tenant ended with the stop")
	case <-time.After(300 * time.Millisecond):
	}
	assert.Equal(t, 1, e.stub.Cancelled(), "the other turns' requests run on")

	require.Equal(t, http.StatusNoContent, admin.request(http.MethodDelete, stopPath, nil).StatusCode)
	require.Equal(t, http.StatusNoContent, both.request(http.MethodDelete, "/api/v1/tenants/"+e.SlugB+"/chat/turns", nil).StatusCode)
	for _, c := range []chan result{theirs, elsewhere} {
		select {
		case r := <-c:
			require.NoError(t, r.err)
			assert.Equal(t, apigen.ChatTurnEndStopped, eventData[apigen.ChatDoneEvent](t, lastEvent(t, r.events, "done")).Reason)
		case <-time.After(5 * time.Second):
			t.Fatal("a stopped turn runs on")
		}
	}
	assert.Equal(t, http.StatusNoContent, both.request(http.MethodDelete, stopPath, nil).StatusCode, "nothing to stop is no error")
}

// docs/adr/0043 D5, docs/adr/0021 D6, migration 24: a person's chat
// capabilities are the person's alone — read, written the first time and
// changed by the person, by nobody else, an administrator of the person's
// tenant included; never deleted; and only the nine capabilities.
func TestTheChatCapabilitiesArePersonal(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	require.NoError(t, fixtures(t).Exec(ctx, `INSERT INTO chat_capabilities (user_id, capabilities) VALUES ($1, '{rank}')`, w.MemberB))
	memberA, adminA, memberB := ctxOf(w.MemberA, uuid.Nil), ctxOf(w.AdminA, w.A), ctxOf(w.MemberB, uuid.Nil)
	theirs := `SELECT count(*) FROM chat_capabilities WHERE user_id = $1`

	assert.EqualValues(t, 1, count(t, memberB, theirs, w.MemberB), "the person reads their own")
	assert.EqualValues(t, 0, count(t, memberA, theirs, w.MemberB), "nobody else reads it")
	assert.EqualValues(t, 0, count(t, settings{}, theirs, w.MemberB))
	insert := `INSERT INTO chat_capabilities (user_id, capabilities) VALUES ($1, '{close}')`
	affects(t, 1, memberA, insert, w.MemberA)
	denied(t, memberA, insert, w.ViewerA)
	denied(t, adminA, insert, w.MemberA)
	denied(t, settings{}, insert, w.MemberA)
	update := `UPDATE chat_capabilities SET capabilities = '{decide, close, drop}' WHERE user_id = $1`
	affects(t, 1, memberB, update, w.MemberB)
	affects(t, 0, memberA, update, w.MemberB)
	affects(t, 0, ctxOf(w.AdminA, uuid.Nil), update, w.MemberB)
	denied(t, memberB, `UPDATE chat_capabilities SET user_id = $1 WHERE user_id = $2`, w.MemberA, w.MemberB)
	denied(t, memberB, `DELETE FROM chat_capabilities WHERE user_id = $1`, w.MemberB)
	_, err := run(t, memberA, `INSERT INTO chat_capabilities (user_id, capabilities) VALUES ($1, '{delete}')`, w.MemberA)
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	assert.Equal(t, "23514", pgErr.Code, "a capability the catalogue does not have")
}
