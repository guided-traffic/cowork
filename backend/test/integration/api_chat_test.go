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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api"
	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/llm"
	"github.com/guided-traffic/cowork/backend/test/stubllm"
)

// stubHost is the host the tests' fingerprint names for the stub provider: a
// name of its own, so that no audit row of the run holds an address
// (TestAuditRowsCarryTheSourceHash).
const stubHost = "stub.llm.test"

// withChat configures the chat against the stub in one of its formats;
// inside declares the provider inside the installation (docs/adr/0076).
func withChat(stub *stubllm.Server, format string, inside bool, edit ...func(*api.ChatOptions)) func(*api.Options) {
	return func(o *api.Options) {
		url := stub.OpenAIURL()
		if format == llm.Anthropic {
			url = stub.AnthropicURL()
		}
		provider, err := llm.New(llm.Config{Format: format, URL: url, APIKey: stubllm.Key, Model: "stub/model"})
		if err != nil {
			panic(err)
		}
		c := &api.ChatOptions{Provider: provider, Kind: format, Model: "stub/model", Inside: inside, TurnTimeout: time.Minute, MaxSteps: 8,
			Fingerprint: format + " " + stubHost + " stub/model"}
		for _, e := range edit {
			e(c)
		}
		o.Chat = c
	}
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

// chatEnv is a world with its accounts and tokens, a stub provider declared
// inside the installation, and the browser of the member of tenant A.
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
	e.s = newAPI(t, withLogin, withChat(e.stub, format, true, edit...))
	e.b = e.s.browser(t)
	e.b.mustLogin(e.names["memberA"], testPassword)
	return e
}

// turn posts a turn of the conversation as the browser.
func (e chatEnv) turn(t *testing.T, b *browser, conversation uuid.UUID, msgs []apigen.ChatMessage, decisions ...apigen.ChatConfirmation) (*http.Response, []sseEvent) {
	t.Helper()
	body := apigen.ChatTurn{Conversation: conversation, Messages: msgs,
		Context: &apigen.ChatPageContext{Path: ptr("/t/" + e.SlugA + "/p/ALPHA/board"), Project: ptr("ALPHA")}}
	if len(decisions) > 0 {
		body.Confirmations = &decisions
	}
	res := b.request(http.MethodPost, "/api/v1/tenants/"+e.SlugA+"/chat", body)
	if res.StatusCode != http.StatusOK {
		return res, nil
	}
	return res, readEvents(t, res)
}

func said(text string) apigen.ChatMessage {
	return apigen.ChatMessage{Role: apigen.ChatRoleUser, Text: &text}
}

// docs/adr/0076: the chat is available where a provider is configured and
// declared inside the installation, or where the tenant's administrators
// allowed an outside one — which takes a session and is recorded — and the
// availability never shows the provider's address.
func TestChatAvailability(t *testing.T) {
	w := newWorld(t)
	tk := issueTokens(t, w)
	accounts := withAccounts(t, w)
	none := newAPI(t, withLogin)
	got := decode[apigen.ChatAvailability](t, none.do(t, caller{Token: tk.MemberA}, http.MethodGet, "/api/v1/tenants/"+w.SlugA+"/chat", nil))
	assert.False(t, got.Available)
	reason, _ := got.Reason.Get()
	assert.Equal(t, apigen.ChatUnavailableReasonNotConfigured, reason)
	b := none.browser(t)
	b.mustLogin(accounts["memberA"], testPassword)
	assertProblem(t, b.request(http.MethodPost, "/api/v1/tenants/"+w.SlugA+"/chat", apigen.ChatTurn{Conversation: uuid.New(),
		Messages: []apigen.ChatMessage{said("Hello")}}), http.StatusConflict, "chat_unavailable")

	stub := stubllm.New(t)
	inside := newAPI(t, withLogin, withChat(stub, llm.OpenAI, true))
	for _, token := range []string{tk.MemberA, tk.ViewerA, tk.MemberB} {
		slug := w.SlugA
		if token == tk.MemberB {
			slug = w.SlugB
		}
		res := inside.do(t, caller{Token: token}, http.MethodGet, "/api/v1/tenants/"+slug+"/chat", nil)
		require.Equal(t, http.StatusOK, res.StatusCode)
		raw := decode[map[string]any](t, res)
		assert.Equal(t, map[string]any{"available": true, "provider": "openai", "model": "stub/model", "inside": true, "reason": nil}, raw,
			"every tenant, every member, no address")
	}

	outside := newAPI(t, withLogin, withChat(stub, llm.Anthropic, false))
	availability := func(slug string) apigen.ChatAvailability {
		return decode[apigen.ChatAvailability](t, outside.do(t, caller{Token: tk.Both}, http.MethodGet, "/api/v1/tenants/"+slug+"/chat", nil))
	}
	got = availability(w.SlugA)
	assert.False(t, got.Available)
	reason, _ = got.Reason.Get()
	assert.Equal(t, apigen.ChatUnavailableReasonNotAllowedInTenant, reason)
	model, _ := got.Model.Get()
	assert.Equal(t, "stub/model", model, "the administrators see what they would allow")

	admin := outside.client(t, caller{Token: tk.AdminA})
	tenant, err := admin.GetTenantWithResponse(context.Background(), w.SlugA)
	require.NoError(t, err)
	assert.False(t, tenant.JSON200.ChatExternalAllowed)
	etag := tenant.HTTPResponse.Header.Get("ETag")
	refused, err := admin.UpdateTenantWithResponse(context.Background(), w.SlugA, &apigen.UpdateTenantParams{IfMatch: &etag},
		apigen.TenantPatch{ChatExternalAllowed: ptr(true)})
	require.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, refused.StatusCode())
	assert.Equal(t, "session_required", string(refused.ApplicationproblemJSONDefault.Code), "a token cannot let the data out")

	ab := outside.browser(t)
	ab.mustLogin(accounts["adminA"], testPassword)
	res := ab.request(http.MethodPatch, "/api/v1/tenants/"+w.SlugA, map[string]any{"chat_external_allowed": true}, withHeader("If-Match", etag))
	require.Equal(t, http.StatusOK, res.StatusCode)
	assert.True(t, decode[apigen.Tenant](t, res).ChatExternalAllowed)
	assert.True(t, availability(w.SlugA).Available)
	assert.False(t, availability(w.SlugB).Available, "one tenant's consent is not another's")
	provider := "anthropic " + stubHost + " stub/model"
	recorded, err := fixtures(t).QueryCount(context.Background(), `SELECT count(*) FROM audit_events WHERE tenant_id = $1
		AND entity_type = 'tenant' AND action = 'updated' AND actor_user_id = $2
		AND before = '{"chat_external_allowed": false, "chat_external_provider": null}'
		AND after = jsonb_build_object('chat_external_allowed', true, 'chat_external_provider', $3::text)`, w.A, w.AdminA, provider)
	require.NoError(t, err)
	assert.EqualValues(t, 1, recorded, "the consent is a recorded act, naming the provider it is given to")

	// The operator points the chat at another model: the tenant's yes was
	// given to the old one, and counts for nothing until it is given again.
	moved := newAPI(t, withLogin, func(o *api.Options) {
		withChat(stub, llm.Anthropic, false)(o)
		o.Chat.Model, o.Chat.Fingerprint = "stub/other-model", "anthropic "+stubHost+" stub/other-model"
	})
	elsewhere := decode[apigen.ChatAvailability](t, moved.do(t, caller{Token: tk.Both}, http.MethodGet, "/api/v1/tenants/"+w.SlugA+"/chat", nil))
	assert.False(t, elsewhere.Available)
	reason, _ = elsewhere.Reason.Get()
	assert.Equal(t, apigen.ChatUnavailableReasonNotAllowedInTenant, reason)
	seenThere, err := moved.client(t, caller{Token: tk.AdminA}).GetTenantWithResponse(context.Background(), w.SlugA)
	require.NoError(t, err)
	assert.False(t, seenThere.JSON200.ChatExternalAllowed, "the tenant reads its consent for the provider configured now")

	tenant, err = admin.GetTenantWithResponse(context.Background(), w.SlugA)
	require.NoError(t, err)
	etag = tenant.HTTPResponse.Header.Get("ETag")
	withdrawn, err := admin.UpdateTenantWithResponse(context.Background(), w.SlugA, &apigen.UpdateTenantParams{IfMatch: &etag},
		apigen.TenantPatch{ChatExternalAllowed: ptr(false)})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, withdrawn.StatusCode(), "a token may take the consent back")
	assert.False(t, availability(w.SlugA).Available)
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
		return stubllm.Reply{Calls: []stubllm.Call{{ID: "call_rank", Name: "set_urgency",
			Arguments: fmt.Sprintf(`{"key": %q, "urgency": "now", "reason": "the person asked to rank it to now"}`, key[1])}}}
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
	assert.Equal(t, apigen.UrgencyNow, ticket.JSON200.Urgency)
	assert.Equal(t, "The gate stays open", ticket.JSON200.Title)

	mark := "chat/stub:model/" + conversation.String()
	for action, keyed := range map[string]bool{"created": true, "overridden": false} {
		n, err := fixtures(t).QueryCount(e.ctx, `SELECT count(*) FROM audit_events WHERE ticket_key = $1 AND action = $2
			AND actor_user_id = $3 AND agent = $4 AND token_id IS NULL AND cardinality(agent_capabilities) = 9
			AND (idempotency_key IS NOT NULL OR NOT $5)`, key, action, e.MemberA, mark, keyed)
		require.NoError(t, err)
		assert.EqualValues(t, 1, n, "%s: the person's act, marked as the chat's (docs/adr/0036)", action)
	}

	got := e.stub.Requests()
	require.Len(t, got, 3)
	first := got[0]
	assert.Contains(t, first.System, fmt.Sprintf(`the tenant "Tenant A" (%s)`, e.SlugA))
	assert.Contains(t, first.System, "/t/"+e.SlugA+"/p/ALPHA/board")
	assert.NotContains(t, first.Tools, "api")
	assert.Contains(t, first.Tools, "set_urgency")
	assert.Contains(t, first.Tools, "open_board")
	assert.Equal(t, "stub/model", first.Model)
	assert.Contains(t, got[1].LastToolText(), "Filed "+key)
}

// docs/adr/0076, confirmations: closing a ticket waits for the person — the
// turn ends with confirm and the call without an answer — and the next turn
// runs it as the person's decision says, or skips it; over the Anthropic
// format.
func TestAChatConfirmationRound(t *testing.T) {
	e := newChatEnv(t, llm.Anthropic)
	member := e.s.client(t, caller{Token: e.tk.MemberA})
	working := func(title string) apigen.Ticket {
		created, err := member.CreateTicketWithResponse(e.ctx, e.SlugA, "ALPHA", &apigen.CreateTicketParams{}, task(title))
		require.NoError(t, err)
		require.Equal(t, http.StatusCreated, created.StatusCode(), string(created.Body))
		for _, to := range []apigen.TicketState{apigen.TicketStateAnalysed, apigen.TicketStateDecided, apigen.TicketStateInProgress} {
			cur, err := member.GetTicketWithResponse(e.ctx, e.SlugA, "ALPHA", created.JSON201.Number)
			require.NoError(t, err)
			res, err := member.TransitionTicketWithResponse(e.ctx, e.SlugA, "ALPHA", created.JSON201.Number, &apigen.TransitionTicketParams{},
				apigen.Transition{From: cur.JSON200.State, To: to})
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
		}
		return *created.JSON201
	}
	stateOf := func(n int) apigen.TicketState {
		res, err := member.GetTicketWithResponse(e.ctx, e.SlugA, "ALPHA", n)
		require.NoError(t, err)
		return res.JSON200.State
	}

	for _, run := range []bool{true, false} {
		tk := working(fmt.Sprintf("Close me (%t)", run))
		short := fmt.Sprintf("ALPHA-%d", tk.Number)
		e.stub.Reply(stubllm.Reply{Calls: []stubllm.Call{{ID: "toolu_close", Name: "transition",
			Arguments: fmt.Sprintf(`{"key": %q, "to": "done", "reason_or_note": "go test ./... passed"}`, short)}}})
		conversation := uuid.Must(uuid.NewV7())
		msgs := make([]apigen.ChatMessage, 1, 4)
		msgs[0] = said("Close " + short)
		res, events := e.turn(t, e.b, conversation, msgs)
		require.Equal(t, http.StatusOK, res.StatusCode)
		assert.Equal(t, []string{"tool_call", "confirm", "done"}, names(events))
		confirm := eventData[apigen.ChatConfirmEvent](t, lastEvent(t, events, "confirm"))
		assert.Equal(t, "toolu_close", confirm.Id)
		assert.Contains(t, confirm.Description, "Close "+tk.Key)
		done := eventData[apigen.ChatDoneEvent](t, lastEvent(t, events, "done"))
		assert.Equal(t, apigen.ChatTurnEndConfirm, done.Reason)
		require.Len(t, done.Messages, 1)
		assert.Equal(t, apigen.TicketStateInProgress, stateOf(tk.Number), "nothing ran before the decision")

		e.stub.Reply(stubllm.Reply{Text: "Done as you decided."})
		msgs = append(msgs, done.Messages...)
		res, events = e.turn(t, e.b, conversation, msgs, apigen.ChatConfirmation{ToolCallId: "toolu_close", Run: run})
		require.Equal(t, http.StatusOK, res.StatusCode)
		assert.Equal(t, []string{"tool_result", "done"}, names(events))
		result := eventData[apigen.ChatToolResultEvent](t, lastEvent(t, events, "tool_result"))
		assert.Equal(t, run, result.Ok)
		if run {
			assert.Equal(t, apigen.TicketStateDone, stateOf(tk.Number))
			n, err := fixtures(t).QueryCount(e.ctx, `SELECT count(*) FROM audit_events WHERE ticket_key = $1 AND action = 'transitioned'
				AND agent = $2 AND actor_user_id = $3`, tk.Key, "chat/stub:model/"+conversation.String()+"+confirmed", e.MemberA)
			require.NoError(t, err)
			assert.EqualValues(t, 1, n)
		} else {
			assert.Equal(t, apigen.TicketStateInProgress, stateOf(tk.Number))
			assert.Contains(t, result.Summary, "skipped")
		}
		got := e.stub.Requests()
		last := got[len(got)-1]
		assert.Equal(t, "tool", last.Messages[len(last.Messages)-1].Role, "the model reads the decision's answer")
		assert.Equal(t, !run, last.Messages[len(last.Messages)-1].IsError)
	}
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

// docs/adr/0036 D3: the agent header makes a session's request an agent's —
// every capability, the hard-off list, a key on its POSTs, the mark on its
// acts — and a malformed one is refused.
func TestTheAgentHeaderOnASession(t *testing.T) {
	e := newChatEnv(t, llm.OpenAI)
	path := "/api/v1/tenants/" + e.SlugA + "/projects/ALPHA/tickets"
	mark := withHeader("X-Cowork-Agent", "claude-code/opus/s1")
	assertProblem(t, e.b.request(http.MethodPost, path, task("Keyless"), mark), http.StatusBadRequest, "idempotency_key_required")
	res := e.b.request(http.MethodPost, path, task("Keyed"), mark, withHeader("Idempotency-Key", uuid.NewString()))
	require.Equal(t, http.StatusCreated, res.StatusCode)
	created := decode[apigen.Ticket](t, res)
	n, err := fixtures(t).QueryCount(e.ctx, `SELECT count(*) FROM audit_events WHERE ticket_key = $1 AND action = 'created'
		AND agent = 'claude-code/opus/s1' AND actor_user_id = $2 AND token_id IS NULL AND cardinality(agent_capabilities) = 9`, created.Key, e.MemberA)
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
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, e.s.URL+"/api/v1/tenants/"+e.SlugA+"/chat", strings.NewReader(string(raw)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", testOrigin)
	req.Header.Set("X-Requested-With", "cowork")
	req.AddCookie(&http.Cookie{Name: "__Host-cowork-session", Value: e.b.Cookie})
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

// docs/adr/0065, docs/adr/0076: once the chat read a confidential ticket,
// a write waits for the person; the person's Run is recorded on the act, and
// a Run sent twice makes one comment — the second is answered from what the
// first stored (docs/adr/0045).
func TestAConfidentialTicketWaitsAndARunReplays(t *testing.T) {
	e := newChatEnv(t, llm.OpenAI)
	member := e.s.client(t, caller{Token: e.tk.MemberA})
	secret, err := member.CreateTicketWithResponse(e.ctx, e.SlugA, "ALPHA", &apigen.CreateTicketParams{}, task("A live finding", func(b *apigen.TicketCreate) {
		b.Security, b.Threat = apigen.SecurityClassLive, ptr("the gate stays open")
	}))
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, secret.StatusCode(), string(secret.Body))
	require.True(t, secret.JSON201.Confidential)
	plainTicket, err := member.CreateTicketWithResponse(e.ctx, e.SlugA, "ALPHA", &apigen.CreateTicketParams{}, task("A plain ticket"))
	require.NoError(t, err)
	short := fmt.Sprintf("ALPHA-%d", plainTicket.JSON201.Number)

	e.stub.Reply(stubllm.Reply{Calls: []stubllm.Call{
		{ID: "read", Name: "get_ticket", Arguments: fmt.Sprintf(`{"key": %q}`, secret.JSON201.Key)},
		{ID: "write", Name: "comment", Arguments: fmt.Sprintf(`{"key": %q, "text": "Seen it."}`, short)},
	}})
	conversation := uuid.Must(uuid.NewV7())
	msgs := make([]apigen.ChatMessage, 1, 6)
	msgs[0] = said("Read the finding, then note it")
	_, events := e.turn(t, e.b, conversation, msgs)
	assert.Equal(t, []string{"tool_call", "tool_result", "tool_call", "confirm", "done"}, names(events))
	read := eventData[apigen.ChatToolResultEvent](t, events[1])
	assert.True(t, strings.HasPrefix(read.Summary, "[This answer holds a confidential ticket"), read.Summary)
	assert.Contains(t, eventData[apigen.ChatConfirmEvent](t, lastEvent(t, events, "confirm")).Description, "read a confidential ticket")
	comments := func() int64 {
		n, err := fixtures(t).QueryCount(e.ctx, `SELECT count(*) FROM comments WHERE ticket_id = $1`, plainTicket.JSON201.Id)
		require.NoError(t, err)
		return n
	}
	assert.Zero(t, comments(), "the write waits")

	msgs = append(msgs, eventData[apigen.ChatDoneEvent](t, lastEvent(t, events, "done")).Messages...)
	for range 2 {
		e.stub.Reply(stubllm.Reply{Text: "Noted."})
		res, events := e.turn(t, e.b, conversation, msgs, apigen.ChatConfirmation{ToolCallId: "write", Run: true})
		require.Equal(t, http.StatusOK, res.StatusCode)
		assert.True(t, eventData[apigen.ChatToolResultEvent](t, lastEvent(t, events, "tool_result")).Ok)
	}
	assert.EqualValues(t, 1, comments(), "a Run sent twice makes one comment")
	marked, err := fixtures(t).QueryCount(e.ctx, `SELECT count(*) FROM audit_events WHERE ticket_key = $1 AND action = 'commented'
		AND agent = $2`, plainTicket.JSON201.Key, "chat/stub:model/"+conversation.String()+"+confirmed")
	require.NoError(t, err)
	assert.EqualValues(t, 1, marked, "the act records the person's Run")
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
