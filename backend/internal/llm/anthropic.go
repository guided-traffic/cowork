package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sort"
)

// anthropicVersion is the version of the Messages API the gateway speaks.
const anthropicVersion = "2023-06-01"

// The content blocks the gateway writes and reads.
const (
	blockText       = "text"
	blockToolUse    = "tool_use"
	blockToolResult = "tool_result"
)

// anthropic speaks the Anthropic Messages API: POST {base}/v1/messages with
// x-api-key and anthropic-version, the tools with an input_schema, the
// answer streamed as message_start, content_block_start (text or tool_use),
// content_block_delta (text_delta or input_json_delta), content_block_stop,
// message_delta with the stop_reason, and message_stop.
type anthropic struct {
	http  *http.Client
	url   string
	key   string
	model string
}

type anRequest struct {
	Model      string        `json:"model"`
	MaxTokens  int           `json:"max_tokens"`
	System     string        `json:"system,omitempty"`
	Messages   []anMessage   `json:"messages"`
	Tools      []anTool      `json:"tools,omitempty"`
	ToolChoice *anToolChoice `json:"tool_choice,omitempty"`
	Stream     bool          `json:"stream"`
}

type anMessage struct {
	Role    string    `json:"role"`
	Content []anBlock `json:"content"`
}

type anBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}

type anTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type anToolChoice struct {
	Type string `json:"type"`
}

type anUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// anEvent is one event of a streamed answer; a thinking block and its deltas
// are read past.
type anEvent struct {
	Type    string `json:"type"`
	Index   int    `json:"index"`
	Message *struct {
		Usage *anUsage `json:"usage"`
	} `json:"message"`
	ContentBlock *struct {
		Type string `json:"type"`
		ID   string `json:"id"`
		Name string `json:"name"`
		Text string `json:"text"`
	} `json:"content_block"`
	Delta *struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		PartialJSON string `json:"partial_json"`
		StopReason  string `json:"stop_reason"`
	} `json:"delta"`
	Usage *anUsage `json:"usage"`
}

// anBody is an answer that was not streamed.
type anBody struct {
	Content []struct {
		Type  string          `json:"type"`
		Text  string          `json:"text"`
		ID    string          `json:"id"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	} `json:"content"`
	StopReason string   `json:"stop_reason"`
	Usage      *anUsage `json:"usage"`
}

func (p *anthropic) Complete(ctx context.Context, req Request, onText func(string)) (Response, error) {
	callCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	header := http.Header{}
	header.Set("x-api-key", p.key)
	header.Set("anthropic-version", anthropicVersion)
	res, err := post(callCtx, p.http, p.url, header, p.request(req), p.key)
	if err != nil {
		return Response{}, err
	}
	defer func() { _ = res.Body.Close() }()
	if !streamed(res) {
		raw, err := readBody(ctx, res, p.key)
		if err != nil {
			return Response{}, err
		}
		return p.body(raw, onText)
	}
	return p.stream(ctx, cancel, res.Body, onText)
}

// request is the neutral request in the Anthropic format: the instructions as
// system, a tool's answer as a tool_result in the next user message, and
// messages of one role in a row merged into one, a tool's answers first.
func (p *anthropic) request(req Request) anRequest {
	r := anRequest{Model: p.model, MaxTokens: maxTokens, System: req.System, Stream: true, Messages: []anMessage{}}
	add := func(role string, blocks ...anBlock) {
		if len(blocks) == 0 {
			return
		}
		if n := len(r.Messages); n > 0 && r.Messages[n-1].Role == role {
			r.Messages[n-1].Content = append(r.Messages[n-1].Content, blocks...)
			return
		}
		r.Messages = append(r.Messages, anMessage{Role: role, Content: blocks})
	}
	for _, m := range req.Messages {
		switch m.Role {
		case RoleUser:
			if m.Text != "" {
				add("user", anBlock{Type: blockText, Text: m.Text})
			}
		case RoleAssistant:
			var blocks []anBlock
			if m.Text != "" {
				blocks = append(blocks, anBlock{Type: blockText, Text: m.Text})
			}
			for _, c := range m.ToolCalls {
				blocks = append(blocks, anBlock{Type: blockToolUse, ID: c.ID, Name: c.Name, Input: object(c.Arguments)})
			}
			add("assistant", blocks...)
		case RoleTool:
			add("user", anBlock{Type: blockToolResult, ToolUseID: m.ToolCallID, Content: m.Text, IsError: m.IsError})
		}
	}
	for _, t := range req.Tools {
		r.Tools = append(r.Tools, anTool{Name: t.Name, Description: t.Description, InputSchema: t.Schema})
	}
	if len(r.Tools) > 0 {
		r.ToolChoice = &anToolChoice{Type: "auto"}
	}
	return r
}

// object is a tool call's input as the API takes it: a JSON object, {} for
// anything else.
func object(raw json.RawMessage) json.RawMessage {
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil || m == nil {
		return json.RawMessage("{}")
	}
	return raw
}

// anBlockParts is a content block as its deltas arrive.
type anBlockParts struct {
	kind, id, name string
	input          argumentsOf
}

// anStream is a streamed answer as its events arrive: the text, the content
// blocks by index, the stop reason.
type anStream struct {
	key    string
	out    Response
	text   answerText
	blocks map[int]*anBlockParts
	stop   string
	ended  bool
}

func (p *anthropic) stream(ctx context.Context, cancel context.CancelFunc, body io.Reader, onText func(string)) (Response, error) {
	s := &anStream{key: p.key, text: answerText{onText: onText}, blocks: map[int]*anBlockParts{}}
	if err := readEvents(ctx, cancel, body, p.key, s.event); err != nil {
		return Response{}, err
	}
	return s.response()
}

// event reads one event of the stream; message_stop ends it.
func (s *anStream) event(data []byte) (bool, error) {
	var ev anEvent
	if err := json.Unmarshal(data, &ev); err != nil {
		return false, malformed("the provider's answer is not the Anthropic format")
	}
	switch ev.Type {
	case "message_start":
		if ev.Message != nil && ev.Message.Usage != nil {
			s.out.Usage = &Usage{InputTokens: ev.Message.Usage.InputTokens}
		}
	case "content_block_start":
		s.start(ev)
	case "content_block_delta":
		s.delta(ev)
	case "message_delta":
		s.messageDelta(ev)
	case "message_stop":
		s.ended = true
		return true, nil
	case "error":
		return false, &Error{Kind: ErrRefused, Detail: "the provider failed while it answered", Clip: clip(providerMessage(data), s.key)}
	}
	return false, nil
}

func (s *anStream) start(ev anEvent) {
	if ev.ContentBlock == nil || (s.blocks[ev.Index] == nil && len(s.blocks) == maxCalls) {
		return
	}
	s.blocks[ev.Index] = &anBlockParts{kind: ev.ContentBlock.Type, id: ev.ContentBlock.ID, name: ev.ContentBlock.Name}
	if ev.ContentBlock.Type == blockText {
		s.text.add(ev.ContentBlock.Text)
	}
}

func (s *anStream) delta(ev anEvent) {
	b := s.blocks[ev.Index]
	switch {
	case b == nil || ev.Delta == nil:
	case ev.Delta.Type == "text_delta":
		s.text.add(ev.Delta.Text)
	case ev.Delta.Type == "input_json_delta":
		b.input.add(ev.Delta.PartialJSON)
	}
}

func (s *anStream) messageDelta(ev anEvent) {
	if ev.Delta != nil && ev.Delta.StopReason != "" {
		s.stop = ev.Delta.StopReason
	}
	if ev.Usage != nil {
		if s.out.Usage == nil {
			s.out.Usage = &Usage{}
		}
		s.out.Usage.OutputTokens = ev.Usage.OutputTokens
	}
}

// response is the whole answer once the stream ended: its tool calls in the
// order of their blocks.
func (s *anStream) response() (Response, error) {
	if !s.ended && s.stop == "" {
		return Response{}, malformed("the provider's answer ended before its end")
	}
	s.out.Text = s.text.String()
	indexes := make([]int, 0, len(s.blocks))
	for i := range s.blocks {
		indexes = append(indexes, i)
	}
	sort.Ints(indexes)
	for _, i := range indexes {
		if b := s.blocks[i]; b.kind == blockToolUse {
			s.out.ToolCalls = append(s.out.ToolCalls, b.input.call(b.id, b.name))
		}
	}
	s.out.Finish = anthropicFinish(s.stop)
	return s.out, nil
}

func (p *anthropic) body(raw []byte, onText func(string)) (Response, error) {
	var b anBody
	if err := json.Unmarshal(raw, &b); err != nil {
		return Response{}, malformed("the provider's answer is not the Anthropic format")
	}
	var out Response
	text := answerText{onText: onText}
	for _, c := range b.Content {
		switch {
		case c.Type == blockText:
			text.add(c.Text)
		case c.Type == blockToolUse && len(out.ToolCalls) < maxCalls:
			var args argumentsOf
			args.add(string(c.Input))
			out.ToolCalls = append(out.ToolCalls, args.call(c.ID, c.Name))
		}
	}
	out.Text = text.String()
	if b.Usage != nil {
		out.Usage = &Usage{InputTokens: b.Usage.InputTokens, OutputTokens: b.Usage.OutputTokens}
	}
	out.Finish = anthropicFinish(b.StopReason)
	return out, nil
}

func anthropicFinish(reason string) Finish {
	switch reason {
	case "end_turn", "stop_sequence", "":
		return FinishStop
	case "tool_use":
		return FinishToolCalls
	case "max_tokens":
		return FinishLength
	}
	return FinishOther
}
