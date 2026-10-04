package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
)

// openAI speaks OpenAI Chat Completions: POST {base}/chat/completions with
// the tools as functions, the answer streamed as server-sent events whose
// chunks carry the text and the tool calls' arguments in fragments by index,
// ended by [DONE].
type openAI struct {
	http  *http.Client
	url   string
	key   string
	model string
}

type oaRequest struct {
	Model      string      `json:"model"`
	Messages   []oaMessage `json:"messages"`
	Tools      []oaTool    `json:"tools,omitempty"`
	ToolChoice string      `json:"tool_choice,omitempty"`
	MaxTokens  int         `json:"max_tokens"`
	Stream     bool        `json:"stream"`
}

type oaMessage struct {
	Role string `json:"role"`
	// Content is null on an assistant message that only calls tools.
	Content    *string      `json:"content"`
	ToolCalls  []oaToolCall `json:"tool_calls,omitempty"`
	ToolCallID string       `json:"tool_call_id,omitempty"`
}

type oaToolCall struct {
	ID       string     `json:"id"`
	Type     string     `json:"type"`
	Function oaFunction `json:"function"`
}

type oaFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type oaTool struct {
	Type     string        `json:"type"`
	Function oaFunctionDef `json:"function"`
}

type oaFunctionDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

type oaUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

// oaChunk is one chunk of a streamed answer. A reasoning model's
// reasoning_content is not declared: it is not the answer.
type oaChunk struct {
	Choices []struct {
		Delta struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				Index    *int   `json:"index"`
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *oaUsage        `json:"usage"`
	Error json.RawMessage `json:"error"`
}

// oaBody is an answer that was not streamed.
type oaBody struct {
	Choices []struct {
		Message struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				ID       string     `json:"id"`
				Function oaFunction `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *oaUsage `json:"usage"`
}

func (p *openAI) Complete(ctx context.Context, req Request, onText func(string)) (Response, error) {
	callCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	header := http.Header{}
	if p.key != "" {
		header.Set("Authorization", "Bearer "+p.key)
	}
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

// request is the neutral request in the OpenAI format: the instructions as
// the system message, a tool's answer as a tool message.
func (p *openAI) request(req Request) oaRequest {
	r := oaRequest{Model: p.model, MaxTokens: maxTokens, Stream: true}
	text := func(s string) *string { return &s }
	if req.System != "" {
		r.Messages = append(r.Messages, oaMessage{Role: "system", Content: text(req.System)})
	}
	for _, m := range req.Messages {
		switch m.Role {
		case RoleUser:
			r.Messages = append(r.Messages, oaMessage{Role: "user", Content: text(m.Text)})
		case RoleAssistant:
			om := oaMessage{Role: "assistant"}
			if m.Text != "" {
				om.Content = text(m.Text)
			}
			for _, c := range m.ToolCalls {
				om.ToolCalls = append(om.ToolCalls, oaToolCall{ID: c.ID, Type: "function",
					Function: oaFunction{Name: c.Name, Arguments: string(arguments(string(c.Arguments)))}})
			}
			r.Messages = append(r.Messages, om)
		case RoleTool:
			r.Messages = append(r.Messages, oaMessage{Role: "tool", Content: text(m.Text), ToolCallID: m.ToolCallID})
		}
	}
	for _, t := range req.Tools {
		r.Tools = append(r.Tools, oaTool{Type: "function", Function: oaFunctionDef{Name: t.Name, Description: t.Description, Parameters: t.Schema}})
	}
	if len(r.Tools) > 0 {
		r.ToolChoice = "auto"
	}
	return r
}

// oaCall is a tool call as its fragments arrive.
type oaCall struct {
	id, name string
	args     argumentsOf
}

// oaStream is a streamed answer as its chunks arrive: the text, which the
// reasoning filter passes on, the tool calls by index, the finish reason.
type oaStream struct {
	key     string
	out     Response
	text    answerText
	think   thinkFilter
	calls   []*oaCall
	byIndex map[int]*oaCall
	finish  string
	ended   bool
}

func (p *openAI) stream(ctx context.Context, cancel context.CancelFunc, body io.Reader, onText func(string)) (Response, error) {
	s := &oaStream{key: p.key, text: answerText{onText: onText}, byIndex: map[int]*oaCall{}}
	if err := readEvents(ctx, cancel, body, p.key, s.chunk); err != nil {
		return Response{}, err
	}
	return s.response()
}

// chunk reads one data line of the stream; [DONE] ends it.
func (s *oaStream) chunk(data []byte) (bool, error) {
	if string(data) == "[DONE]" {
		s.ended = true
		return true, nil
	}
	var chunk oaChunk
	if err := json.Unmarshal(data, &chunk); err != nil {
		return false, malformed("the provider's answer is not the OpenAI format")
	}
	if len(chunk.Error) > 0 && string(chunk.Error) != "null" {
		return false, &Error{Kind: ErrRefused, Detail: "the provider failed while it answered", Clip: clip(providerMessage(data), s.key)}
	}
	if chunk.Usage != nil {
		s.out.Usage = &Usage{InputTokens: chunk.Usage.PromptTokens, OutputTokens: chunk.Usage.CompletionTokens}
	}
	for _, c := range chunk.Choices {
		s.text.add(s.think.feed(c.Delta.Content))
		for _, tc := range c.Delta.ToolCalls {
			s.fragment(tc.Index, tc.ID, tc.Function.Name, tc.Function.Arguments)
		}
		if c.FinishReason != "" {
			s.finish = c.FinishReason
		}
	}
	return false, nil
}

// fragment adds a piece of a tool call to the call of its index; a server
// that sends no index starts a call with a new id and continues the last
// one otherwise.
func (s *oaStream) fragment(index *int, id, name, args string) {
	i := len(s.calls)
	if index != nil {
		i = *index
	} else if id == "" && len(s.calls) > 0 {
		i = len(s.calls) - 1
	}
	call := s.byIndex[i]
	if call == nil || (id != "" && call.id != "" && id != call.id) {
		if len(s.calls) == maxCalls {
			return
		}
		call = &oaCall{}
		s.byIndex[i] = call
		s.calls = append(s.calls, call)
	}
	if id != "" {
		call.id = id
	}
	if name != "" {
		call.name = name
	}
	call.args.add(args)
}

// response is the whole answer once the stream ended.
func (s *oaStream) response() (Response, error) {
	if !s.ended && s.finish == "" {
		return Response{}, malformed("the provider's answer ended before its end")
	}
	s.text.add(s.think.flush())
	s.out.Text = s.text.String()
	for _, c := range s.calls {
		s.out.ToolCalls = append(s.out.ToolCalls, c.args.call(c.id, c.name))
	}
	s.out.Finish = openAIFinish(s.finish, len(s.out.ToolCalls) > 0)
	return s.out, nil
}

func (p *openAI) body(raw []byte, onText func(string)) (Response, error) {
	var b oaBody
	if err := json.Unmarshal(raw, &b); err != nil || len(b.Choices) == 0 {
		return Response{}, malformed("the provider's answer is not the OpenAI format")
	}
	c := b.Choices[0]
	var think thinkFilter
	text := answerText{onText: onText}
	text.add(think.feed(c.Message.Content) + think.flush())
	out := Response{Text: text.String()}
	for i, tc := range c.Message.ToolCalls {
		if i == maxCalls {
			break
		}
		var args argumentsOf
		args.add(tc.Function.Arguments)
		out.ToolCalls = append(out.ToolCalls, args.call(tc.ID, tc.Function.Name))
	}
	if b.Usage != nil {
		out.Usage = &Usage{InputTokens: b.Usage.PromptTokens, OutputTokens: b.Usage.CompletionTokens}
	}
	out.Finish = openAIFinish(c.FinishReason, len(out.ToolCalls) > 0)
	return out, nil
}

func openAIFinish(reason string, calls bool) Finish {
	switch {
	case reason == "tool_calls" || reason == "function_call" || calls:
		return FinishToolCalls
	case reason == "stop" || reason == "":
		return FinishStop
	case reason == "length":
		return FinishLength
	}
	return FinishOther
}
