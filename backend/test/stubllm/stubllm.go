// Package stubllm is a language model in the test's process: one server that
// speaks OpenAI Chat Completions under /v1/chat/completions and the Anthropic
// Messages API under /v1/messages, and answers from a script — text streamed
// in pieces, tool calls with their arguments in fragments, an error status, a
// broken or an oversized stream, a pause. It records every request in the
// neutral form, so a test asserts what the chat sent whichever format it
// spoke (docs/adr/0076, the tests' provider). It is never part of the binary.
package stubllm

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// The formats, and what the two wire formats spell more than once.
const (
	formatOpenAI    = "openai"
	formatAnthropic = "anthropic"
	openAIID        = "chatcmpl-stub"
	roleAssistant   = "assistant"
	blockText       = "text"
	blockToolUse    = "tool_use"
)

// Key is the API key the stub expects, when a test sets RequireKey.
const Key = "stub-key-0123456789" // #nosec G101 -- a test provider's key, which protects nothing

// Call is a tool call of a Reply, or one the stub received: its arguments as
// JSON text, which the stub streams in fragments.
type Call struct {
	ID, Name, Arguments string
}

// Reply is one answer of the model.
type Reply struct {
	// Text is streamed in pieces of a few characters.
	Text string
	// Calls are the tool calls, after the text.
	Calls []Call
	// Status answers with this status and Body instead, an error.
	Status int
	Body   string
	// Raw replaces the whole answer with these bytes as a stream: a broken or
	// an oversized one.
	Raw string
	// Plain answers with one JSON body instead of a stream.
	Plain bool
	// Delay holds the answer back; Stall stops a stream after its first piece
	// for that long.
	Delay, Stall time.Duration
	// Finish overrides the reason the answer ends with.
	Finish string
}

// Message is a message as the stub received it, in the neutral form.
type Message struct {
	Role       string
	Text       string
	Calls      []Call
	ToolCallID string
	IsError    bool
}

// Request is a request as the stub received it.
type Request struct {
	// Format is "openai" or "anthropic".
	Format   string
	Header   http.Header
	Model    string
	System   string
	Messages []Message
	// Tools are the names of the tools offered, in order; Schemas their
	// schemas by name.
	Tools   []string
	Schemas map[string]json.RawMessage
	Stream  bool
	// MaxTokens is the bound of the answer the request names; 0 for none.
	MaxTokens int
}

// LastToolText is the text of the last tool message of the request.
func (r Request) LastToolText() string {
	for i := len(r.Messages) - 1; i >= 0; i-- {
		if r.Messages[i].Role == "tool" {
			return r.Messages[i].Text
		}
	}
	return ""
}

// Server is the running stub.
type Server struct {
	// URL is the server's root: the OpenAI format's base is URL + "/v1", the
	// Anthropic format's URL itself.
	URL string
	// RequireKey refuses a request without Key with 401.
	RequireKey bool

	t      testing.TB
	srv    *httptest.Server
	mu     sync.Mutex
	script []func(Request) Reply
	got    []Request
}

// New starts a stub that the test closes when it ends.
func New(t testing.TB) *Server {
	t.Helper()
	s := &Server{t: t}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/chat/completions", s.serve(formatOpenAI))
	mux.HandleFunc("POST /v1/messages", s.serve(formatAnthropic))
	s.srv = httptest.NewServer(mux)
	s.URL = s.srv.URL
	t.Cleanup(s.srv.Close)
	return s
}

// OpenAIURL is the base URL of the OpenAI format.
func (s *Server) OpenAIURL() string { return s.URL + "/v1" }

// AnthropicURL is the base URL of the Anthropic format.
func (s *Server) AnthropicURL() string { return s.URL }

// Reply queues answers, one per request, in order.
func (s *Server) Reply(replies ...Reply) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range replies {
		s.script = append(s.script, func(Request) Reply { return r })
	}
}

// ReplyWith queues an answer made from the request it answers.
func (s *Server) ReplyWith(f func(Request) Reply) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.script = append(s.script, f)
}

// Requests are the requests received so far.
func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Request(nil), s.got...)
}

// Left is how many queued answers wait.
func (s *Server) Left() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.script)
}

func (s *Server) serve(format string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		req, err := decode(format, raw)
		if err != nil {
			http.Error(w, `{"error":{"message":"the stub cannot read the request: `+err.Error()+`"}}`, http.StatusBadRequest)
			return
		}
		req.Header = r.Header.Clone()
		s.mu.Lock()
		s.got = append(s.got, req)
		var next func(Request) Reply
		if len(s.script) > 0 {
			next, s.script = s.script[0], s.script[1:]
		}
		s.mu.Unlock()
		if s.RequireKey && r.Header.Get("x-api-key") != Key && r.Header.Get("Authorization") != "Bearer "+Key {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			// #nosec G705 -- the answer of a test provider to the gateway under test, never a page: it echoes the key on purpose, so a test sees the key taken out of the log
			_, _ = io.WriteString(w, `{"error":{"message":"invalid api key `+r.Header.Get("x-api-key")+r.Header.Get("Authorization")+`"}}`)
			return
		}
		if next == nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"error":{"message":"the stub has no answer left"}}`)
			return
		}
		reply := next(req)
		if reply.Delay > 0 {
			select {
			case <-time.After(reply.Delay):
			case <-r.Context().Done():
				return
			}
		}
		s.answer(w, r, format, reply)
	}
}

func (s *Server) answer(w http.ResponseWriter, r *http.Request, format string, reply Reply) {
	switch {
	case reply.Status != 0:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(reply.Status)
		_, _ = io.WriteString(w, reply.Body)
	case reply.Raw != "":
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, reply.Raw)
	case reply.Plain:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(plain(format, reply))
	default:
		w.Header().Set("Content-Type", "text/event-stream")
		events := openAIEvents(reply)
		if format == formatAnthropic {
			events = anthropicEvents(reply)
		}
		flusher, _ := w.(http.Flusher)
		for i, e := range events {
			_, _ = io.WriteString(w, e)
			if flusher != nil {
				flusher.Flush()
			}
			if i == 0 && reply.Stall > 0 {
				select {
				case <-time.After(reply.Stall):
				case <-r.Context().Done():
					return
				}
			}
		}
	}
}

// pieces cuts a text into pieces of three characters, as a model streams.
func pieces(s string) []string {
	r := []rune(s)
	var out []string
	for len(r) > 0 {
		n := min(3, len(r))
		out = append(out, string(r[:n]))
		r = r[n:]
	}
	return out
}

func sse(event string, data any) string {
	b, _ := json.Marshal(data)
	if event == "" {
		return "data: " + string(b) + "\n\n"
	}
	return "event: " + event + "\ndata: " + string(b) + "\n\n"
}

// The OpenAI format as the stub writes it.
type (
	oaChunk struct {
		ID      string     `json:"id"`
		Object  string     `json:"object"`
		Choices []oaChoice `json:"choices"`
		Usage   *oaUsage   `json:"usage,omitempty"`
	}
	oaChoice struct {
		Index        int        `json:"index"`
		Delta        *oaMessage `json:"delta,omitempty"`
		Message      *oaMessage `json:"message,omitempty"`
		FinishReason *string    `json:"finish_reason"`
	}
	oaMessage struct {
		Role      string       `json:"role,omitempty"`
		Content   *string      `json:"content,omitempty"`
		ToolCalls []oaToolCall `json:"tool_calls,omitempty"`
	}
	oaToolCall struct {
		Index    *int       `json:"index,omitempty"`
		ID       string     `json:"id,omitempty"`
		Type     string     `json:"type,omitempty"`
		Function oaFunction `json:"function"`
	}
	oaFunction struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments"`
	}
	oaUsage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	}
)

// The Anthropic format as the stub writes it.
type (
	anEvent struct {
		Type         string     `json:"type"`
		Index        *int       `json:"index,omitempty"`
		Message      *anMessage `json:"message,omitempty"`
		ContentBlock *anBlock   `json:"content_block,omitempty"`
		Delta        *anDelta   `json:"delta,omitempty"`
		Usage        *anUsage   `json:"usage,omitempty"`
	}
	anMessage struct {
		ID         string    `json:"id"`
		Type       string    `json:"type"`
		Role       string    `json:"role"`
		Content    []anBlock `json:"content"`
		StopReason string    `json:"stop_reason,omitempty"`
		Usage      *anUsage  `json:"usage,omitempty"`
	}
	anBlock struct {
		Type  string          `json:"type"`
		Text  *string         `json:"text,omitempty"`
		ID    string          `json:"id,omitempty"`
		Name  string          `json:"name,omitempty"`
		Input json.RawMessage `json:"input,omitempty"`
	}
	anDelta struct {
		Type        string `json:"type,omitempty"`
		Text        string `json:"text,omitempty"`
		PartialJSON string `json:"partial_json,omitempty"`
		StopReason  string `json:"stop_reason,omitempty"`
	}
	anUsage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	}
)

func str(s string) *string { return &s }

func openAIEvents(reply Reply) []string {
	chunk := func(delta oaMessage, finish *string) string {
		return sse("", oaChunk{ID: openAIID, Object: "chat.completion.chunk", Choices: []oaChoice{{Delta: &delta, FinishReason: finish}}})
	}
	out := []string{chunk(oaMessage{Role: roleAssistant, Content: str("")}, nil)}
	for _, p := range pieces(reply.Text) {
		out = append(out, chunk(oaMessage{Content: str(p)}, nil))
	}
	for i, c := range reply.Calls {
		out = append(out, chunk(oaMessage{ToolCalls: []oaToolCall{{Index: &i, ID: c.ID, Type: "function",
			Function: oaFunction{Name: c.Name}}}}, nil))
		for _, p := range pieces(c.Arguments) {
			out = append(out, chunk(oaMessage{ToolCalls: []oaToolCall{{Index: &i, Function: oaFunction{Arguments: p}}}}, nil))
		}
	}
	finish := reply.Finish
	if finish == "" {
		finish = "stop"
		if len(reply.Calls) > 0 {
			finish = "tool_calls"
		}
	}
	return append(out, chunk(oaMessage{}, &finish),
		sse("", oaChunk{ID: openAIID, Object: "chat.completion.chunk", Choices: []oaChoice{}, Usage: &oaUsage{PromptTokens: 12, CompletionTokens: 7}}),
		"data: [DONE]\n\n")
}

func anthropicEvents(reply Reply) []string {
	out := []string{
		sse("message_start", anEvent{Type: "message_start", Message: &anMessage{ID: "msg_stub", Type: "message", Role: roleAssistant,
			Content: []anBlock{}, Usage: &anUsage{InputTokens: 12}}}),
		sse("ping", anEvent{Type: "ping"}),
	}
	block := func(index int, b anBlock, deltas []anDelta) {
		out = append(out, sse("content_block_start", anEvent{Type: "content_block_start", Index: &index, ContentBlock: &b}))
		for _, d := range deltas {
			out = append(out, sse("content_block_delta", anEvent{Type: "content_block_delta", Index: &index, Delta: &d}))
		}
		out = append(out, sse("content_block_stop", anEvent{Type: "content_block_stop", Index: &index}))
	}
	index := 0
	if reply.Text != "" {
		deltas := make([]anDelta, 0, len(reply.Text)/3+1)
		for _, p := range pieces(reply.Text) {
			deltas = append(deltas, anDelta{Type: "text_delta", Text: p})
		}
		block(index, anBlock{Type: blockText, Text: str("")}, deltas)
		index++
	}
	for _, c := range reply.Calls {
		deltas := make([]anDelta, 0, len(c.Arguments)/3+1)
		for _, p := range pieces(c.Arguments) {
			deltas = append(deltas, anDelta{Type: "input_json_delta", PartialJSON: p})
		}
		block(index, anBlock{Type: blockToolUse, ID: c.ID, Name: c.Name, Input: json.RawMessage("{}")}, deltas)
		index++
	}
	stop := reply.Finish
	if stop == "" {
		stop = "end_turn"
		if len(reply.Calls) > 0 {
			stop = "tool_use"
		}
	}
	return append(out, sse("message_delta", anEvent{Type: "message_delta", Delta: &anDelta{StopReason: stop}, Usage: &anUsage{OutputTokens: 7}}),
		sse("message_stop", anEvent{Type: "message_stop"}))
}

// plain is an answer as one JSON body.
func plain(format string, reply Reply) any {
	if format == formatAnthropic {
		content := []anBlock{}
		if reply.Text != "" {
			content = append(content, anBlock{Type: blockText, Text: str(reply.Text)})
		}
		stop := "end_turn"
		for _, c := range reply.Calls {
			content = append(content, anBlock{Type: blockToolUse, ID: c.ID, Name: c.Name, Input: json.RawMessage(c.Arguments)})
			stop = "tool_use"
		}
		return anMessage{ID: "msg_stub", Type: "message", Role: roleAssistant, Content: content, StopReason: stop,
			Usage: &anUsage{InputTokens: 12, OutputTokens: 7}}
	}
	msg := oaMessage{Role: roleAssistant, Content: str(reply.Text)}
	finish := "stop"
	for _, c := range reply.Calls {
		msg.ToolCalls = append(msg.ToolCalls, oaToolCall{ID: c.ID, Type: "function", Function: oaFunction{Name: c.Name, Arguments: c.Arguments}})
		finish = "tool_calls"
	}
	return oaChunk{ID: openAIID, Object: "chat.completion", Choices: []oaChoice{{Message: &msg, FinishReason: &finish}},
		Usage: &oaUsage{PromptTokens: 12, CompletionTokens: 7}}
}

// decode reads a request of either format into the neutral form.
func decode(format string, raw []byte) (Request, error) {
	if format == formatAnthropic {
		return decodeAnthropic(raw)
	}
	return decodeOpenAI(raw)
}

func decodeOpenAI(raw []byte) (Request, error) {
	var body struct {
		Model     string `json:"model"`
		Stream    bool   `json:"stream"`
		MaxTokens int    `json:"max_tokens"`
		Messages  []struct {
			Role       string  `json:"role"`
			Content    *string `json:"content"`
			ToolCallID string  `json:"tool_call_id"`
			ToolCalls  []struct {
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"messages"`
		Tools []struct {
			Function struct {
				Name       string          `json:"name"`
				Parameters json.RawMessage `json:"parameters"`
			} `json:"function"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return Request{}, err
	}
	req := Request{Format: formatOpenAI, Model: body.Model, Stream: body.Stream, MaxTokens: body.MaxTokens, Schemas: map[string]json.RawMessage{}}
	for _, m := range body.Messages {
		text := ""
		if m.Content != nil {
			text = *m.Content
		}
		if m.Role == "system" {
			req.System = text
			continue
		}
		msg := Message{Role: m.Role, Text: text, ToolCallID: m.ToolCallID}
		for _, c := range m.ToolCalls {
			msg.Calls = append(msg.Calls, Call{ID: c.ID, Name: c.Function.Name, Arguments: c.Function.Arguments})
		}
		req.Messages = append(req.Messages, msg)
	}
	for _, t := range body.Tools {
		req.Tools = append(req.Tools, t.Function.Name)
		req.Schemas[t.Function.Name] = t.Function.Parameters
	}
	return req, nil
}

func decodeAnthropic(raw []byte) (Request, error) {
	var body struct {
		Model     string `json:"model"`
		Stream    bool   `json:"stream"`
		System    string `json:"system"`
		MaxTokens int    `json:"max_tokens"`
		Messages  []struct {
			Role    string `json:"role"`
			Content []struct {
				Type      string          `json:"type"`
				Text      string          `json:"text"`
				ID        string          `json:"id"`
				Name      string          `json:"name"`
				Input     json.RawMessage `json:"input"`
				ToolUseID string          `json:"tool_use_id"`
				Content   string          `json:"content"`
				IsError   bool            `json:"is_error"`
			} `json:"content"`
		} `json:"messages"`
		Tools []struct {
			Name        string          `json:"name"`
			InputSchema json.RawMessage `json:"input_schema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return Request{}, err
	}
	if body.MaxTokens <= 0 {
		return Request{}, fmt.Errorf("max_tokens is required")
	}
	req := Request{Format: formatAnthropic, Model: body.Model, Stream: body.Stream, System: body.System, MaxTokens: body.MaxTokens,
		Schemas: map[string]json.RawMessage{}}
	for _, m := range body.Messages {
		var text []string
		var calls []Call
		for _, b := range m.Content {
			switch b.Type {
			case blockText:
				text = append(text, b.Text)
			case blockToolUse:
				calls = append(calls, Call{ID: b.ID, Name: b.Name, Arguments: string(b.Input)})
			case "tool_result":
				req.Messages = append(req.Messages, Message{Role: "tool", ToolCallID: b.ToolUseID, Text: b.Content, IsError: b.IsError})
			}
		}
		if len(text) > 0 || len(calls) > 0 {
			req.Messages = append(req.Messages, Message{Role: m.Role, Text: strings.Join(text, "\n"), Calls: calls})
		}
	}
	for _, t := range body.Tools {
		req.Tools = append(req.Tools, t.Name)
		req.Schemas[t.Name] = t.InputSchema
	}
	return req, nil
}
