// Package chat is the loop of the chat in the UI (docs/adr/0076): one turn of
// a person's conversation with a model the operator configured — the answer
// streamed, and every tool the model calls run at once through the API as the
// person's agent, which holds the capabilities the person chose
// (docs/adr/0043 D5). Nothing waits for the person: the capabilities and the
// API's rules are the limit. The tools are the shared catalogue of
// internal/tools that take everything as arguments, without the api escape
// hatch, and the chat's own three that open a page. Nothing is kept between
// turns: the conversation comes with each, and the messages a turn adds go
// back with its end.
package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/llm"
	"github.com/guided-traffic/cowork/backend/internal/tools"
)

// The bounds of what a turn keeps (the API document's ChatMessage, ChatTurn).
const (
	// maxText is the longest text a message carries; maxToolText the longest
	// answer of a tool the model reads, maxSummary what the person sees of it.
	maxText     = 100000
	maxToolText = 16000
	maxSummary  = 2000
	// maxCalls is the most tool calls one answer of the model makes.
	maxCalls = 32
	// maxID is the longest id or name of a call.
	maxID = 128
)

// notRun answers the model for a call its turn ended before running — the
// person stopped the turn, or its time ran out: the conversation keeps the
// call, and every later turn answers it so.
const notRun = "This call did not run: its turn ended before it. Make it again if it is still wanted."

// Options are what every turn shares.
type Options struct {
	// Provider is the model the turn talks to, the one the person picked.
	Provider llm.Provider
	// MaxSteps bounds the calls of the model in one turn; 0 for no limit.
	MaxSteps int
	// Now is the clock; nil means time.Now.
	Now func() time.Time
}

// Page is the page the person is on: its path in the UI, the key of the
// project it shows, the short key of the ticket it shows.
type Page struct {
	Path, Project, Ticket string
}

// Turn is one turn of a conversation, which Check holds to the rules first.
type Turn struct {
	// Tenant is the slug of the team the turn runs in, TenantName its name,
	// under the names the database keeps (docs/adr/0005 D1).
	Tenant, TenantName string
	// Conversation is the conversation's id, which the calls' keys derive
	// from.
	Conversation uuid.UUID
	Page         Page
	Messages     []apigen.ChatMessage
	// Session is the API client of the person's agent (NewSession), which
	// knows the capabilities the agent holds.
	Session *tools.Session
}

// Events receives what the person sees of a turn, in order.
type Events interface {
	Text(delta string)
	ToolCall(call apigen.ChatToolCall)
	UI(path string)
	ToolResult(id string, ok bool, summary string)
}

// Outcome is how a turn ended: the messages it added, why it ended, and the
// failure of a turn that ended with ChatTurnEndError — the provider's, the
// turn's time, its stop, or the person leaving.
type Outcome struct {
	Messages []apigen.ChatMessage
	End      apigen.ChatTurnEnd
	Err      error
}

// Run runs a turn.
func Run(ctx context.Context, o Options, t Turn, ev Events) Outcome {
	if o.Now == nil {
		o.Now = time.Now
	}
	r := &runner{o: o, t: t, ev: ev, tools: map[string]tools.Tool{}, bad: map[string]string{}, added: []apigen.ChatMessage{}}
	r.catalogue()
	r.system = system(t, o.Now())
	return r.run(ctx)
}

// runner is a turn in progress.
type runner struct {
	o      Options
	t      Turn
	ev     Events
	system string
	tools  map[string]tools.Tool
	offer  []llm.Tool
	// history is what the model reads, added what the turn adds to the
	// conversation; bad are the calls the tool cannot take, with why.
	history []llm.Message
	added   []apigen.ChatMessage
	bad     map[string]string
}

// offered names every tool the chat can meet — the shared catalogue's and its
// own — and whether the model is given it. A tool that is not here is not
// offered, and a test fails while the shared catalogue holds one
// (TestEveryToolIsNamed): a tool joins the chat by a decision, not by being
// added to the catalogue. Left out are session_start, which reads a working
// directory, and the api escape hatch: raw access to the API is the MCP
// server's, and a model reading injected text gets none (docs/adr/0076 D1).
var offered = map[string]bool{
	"session_start": false, "api": false,
	"get_ticket": true, "search": true, "open_ticket": true, "open_backlog": true, "open_board": true,
	"file_ticket": true, "record_state": true, "open_question": true, "comment": true, "link": true, "watch": true,
	"place_ticket": true, "transition": true, "set_progress": true, "finish_work": true, "record_answer": true,
	"create_project": true,
}

// catalogue is the turn's tools, each described with the capabilities the
// agent holds, so the model knows before calling what stays the person's
// (docs/adr/0042 D3, docs/adr/0043 D6).
func (r *runner) catalogue() {
	tok := r.t.Session.Token()
	all := append(tools.Catalogue(tools.Anywhere), uiTools(r.t, r.ev)...)
	for _, tool := range all {
		if !offered[tool.Name] {
			continue
		}
		r.tools[tool.Name] = tool
		r.offer = append(r.offer, llm.Tool{Name: tool.Name, Description: tool.Describe(&tok), Schema: tool.Schema()})
	}
}

func (r *runner) run(ctx context.Context) Outcome {
	r.history = split(r.t.Messages)
	for steps := 0; ; steps++ {
		if ctx.Err() != nil {
			return r.end(apigen.ChatTurnEndError, ctx.Err())
		}
		if r.o.MaxSteps > 0 && steps == r.o.MaxSteps {
			return r.end(apigen.ChatTurnEndStepLimit, nil)
		}
		res, err := r.o.Provider.Complete(ctx, llm.Request{System: r.system, Messages: r.history, Tools: r.offer}, r.ev.Text)
		if err != nil {
			return r.end(apigen.ChatTurnEndError, err)
		}
		calls := r.calls(res.ToolCalls)
		if text := clipText(res.Text, maxText); strings.TrimSpace(text) != "" || len(calls) > 0 {
			r.addAssistant(text, calls)
		}
		if len(calls) == 0 {
			return r.end(apigen.ChatTurnEndAnswered, nil)
		}
		for _, call := range calls {
			r.ev.ToolCall(call)
			r.runCall(ctx, call)
			if ctx.Err() != nil {
				return r.end(apigen.ChatTurnEndError, ctx.Err())
			}
		}
	}
}

// runCall runs a call at once and answers it: a tool's refusal — an act that
// needs a capability the person did not give the chat among them — is an
// answer the model reads, not a failed turn. Its creating POSTs carry keys
// derived from the conversation and the call, so the same call sent again
// replays instead of acting twice (docs/adr/0045).
func (r *runner) runCall(ctx context.Context, call apigen.ChatToolCall) {
	tool, ok := r.tools[call.Name]
	r.t.Session.NewKey = keys(r.t.Conversation, call.Id)
	var res tools.Result
	switch {
	case !ok:
		res = tools.Result{Text: fmt.Sprintf("There is no tool %q. The tools are those offered with this conversation.", call.Name), IsError: true}
	case r.bad[call.Id] != "":
		res = tools.Result{Text: r.bad[call.Id], IsError: true}
	default:
		res = tool.Call(ctx, r.t.Session, call.Arguments)
	}
	r.answer(call.Id, !res.IsError, res.Text)
}

// answer adds a call's answer: the model reads it clipped, the person sees its
// start.
func (r *runner) answer(id string, ok bool, text string) {
	r.ev.ToolResult(id, ok, clipText(text, maxSummary))
	text = clipText(text, maxToolText)
	r.history = append(r.history, llm.Message{Role: llm.RoleTool, ToolCallID: id, Text: text, IsError: !ok})
	r.added = append(r.added, apigen.ChatMessage{Role: apigen.ChatRoleTool, ToolCallId: &id, Ok: &ok, Text: &text})
}

// addAssistant adds the model's message; text of white space only is none, so
// the message is one the next turn can send.
func (r *runner) addAssistant(text string, calls []apigen.ChatToolCall) {
	if strings.TrimSpace(text) == "" {
		text = ""
	}
	m := apigen.ChatMessage{Role: apigen.ChatRoleAssistant}
	lm := llm.Message{Role: llm.RoleAssistant, Text: text}
	if text != "" {
		m.Text = &text
	}
	if len(calls) > 0 {
		m.ToolCalls = &calls
		for _, c := range calls {
			lm.ToolCalls = append(lm.ToolCalls, llm.ToolCall{ID: c.Id, Name: c.Name, Arguments: c.Arguments})
		}
	}
	r.history = append(r.history, lm)
	r.added = append(r.added, m)
}

// calls are the model's tool calls as the conversation keeps them: at most
// maxCalls, each with an id of its own and a name within the bounds, and an
// object for arguments — what the model wrote that is none, or more than the
// gateway keeps, is kept apart, and the call answered with it.
func (r *runner) calls(in []llm.ToolCall) []apigen.ChatToolCall {
	out := make([]apigen.ChatToolCall, 0, min(len(in), maxCalls))
	seen := map[string]bool{}
	for i, c := range in {
		if i == maxCalls {
			break
		}
		id := c.ID
		if id == "" || len(id) > maxID || seen[id] || r.known(id) {
			id = "call_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:20]
		}
		seen[id] = true
		args := bytes.TrimSpace(c.Arguments)
		var obj map[string]any
		switch {
		case c.TooLong:
			r.bad[id] = "The arguments are longer than the chat takes of a call: write less, or split the act."
			args = []byte("{}")
		case json.Unmarshal(args, &obj) != nil || obj == nil:
			r.bad[id] = "The arguments are not a JSON object: " + clipText(string(c.Arguments), 200)
			args = []byte("{}")
		}
		out = append(out, apigen.ChatToolCall{Id: id, Name: clipText(c.Name, maxID), Arguments: apigen.ChatToolArguments(args)})
	}
	return out
}

// known reports whether an id names a call of the conversation already.
func (r *runner) known(id string) bool {
	for _, m := range r.history {
		for _, c := range m.ToolCalls {
			if c.ID == id {
				return true
			}
		}
	}
	return false
}

func (r *runner) end(end apigen.ChatTurnEnd, err error) Outcome {
	return Outcome{Messages: r.added, End: end, Err: err}
}

// keySpace names the Idempotency-Keys of the chat's calls.
var keySpace = uuid.NewSHA1(uuid.NameSpaceURL, []byte("https://cowork.dev/chat/idempotency-key"))

// keys makes the Idempotency-Keys of a call's creating POSTs, the nth from
// the conversation, the call and n: the same call sent again — a turn sent
// again — carries the same keys, and the API replays its answers instead of
// acting twice (docs/adr/0045 D3, D4). A key is the person's, so no other
// person's request meets it.
func keys(conversation uuid.UUID, call string) func() uuid.UUID {
	n := 0
	return func() uuid.UUID {
		n++
		return uuid.NewSHA1(keySpace, []byte(conversation.String()+"/"+call+"/"+strconv.Itoa(n)))
	}
}

// split turns the conversation into what the model reads. A call of the
// model that has no answer — its turn ended before it ran — is answered as not
// run where it stands, on every turn, and not added to the conversation: a
// provider takes no call without its answer.
func split(msgs []apigen.ChatMessage) []llm.Message {
	var (
		history  []llm.Message
		group    []apigen.ChatToolCall
		answered = map[string]bool{}
	)
	closeGroup := func() {
		for _, c := range group {
			if !answered[c.Id] {
				history = append(history, llm.Message{Role: llm.RoleTool, ToolCallID: c.Id, Text: notRun, IsError: true})
			}
		}
		group, answered = nil, map[string]bool{}
	}
	for _, m := range msgs {
		switch m.Role {
		case apigen.ChatRoleUser:
			closeGroup()
			history = append(history, llm.Message{Role: llm.RoleUser, Text: deref(m.Text)})
		case apigen.ChatRoleAssistant:
			closeGroup()
			lm := llm.Message{Role: llm.RoleAssistant, Text: deref(m.Text)}
			if m.ToolCalls != nil {
				group = *m.ToolCalls
				for _, c := range group {
					lm.ToolCalls = append(lm.ToolCalls, llm.ToolCall{ID: c.Id, Name: c.Name, Arguments: c.Arguments})
				}
			}
			history = append(history, lm)
		case apigen.ChatRoleTool:
			id := deref(m.ToolCallId)
			answered[id] = true
			history = append(history, llm.Message{Role: llm.RoleTool, ToolCallID: id, Text: deref(m.Text), IsError: m.Ok != nil && !*m.Ok})
		}
	}
	closeGroup()
	return history
}

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}

// clipText cuts a text to n characters, saying how much was left out. It
// counts what it keeps and what it leaves, and copies no more than it keeps.
func clipText(s string, n int) string {
	if _, cut := llm.Head(s, n); !cut {
		return s
	}
	more := utf8.RuneCountInString(s) - n
	note := fmt.Sprintf("\n… (%d characters more)", more)
	head, _ := llm.Head(s, max(n-utf8.RuneCountInString(note), 0))
	return strings.TrimRight(head, " \n") + note
}
