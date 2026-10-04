// Package llm is the chat's gateway to a language model (docs/adr/0076): one
// interface over two wire formats — OpenAI Chat Completions, which OpenAI, LM
// Studio, Ollama and vLLM speak, and the Anthropic Messages API — and a
// neutral message model between them.
//
// Where the gateway connects and with which key is the operator's
// configuration, never a request's. It follows no redirect, bounds what it
// reads, and gives up on a provider that stays silent. What a provider
// answers in an error never reaches the person and reaches the log only as a
// short clip without the key.
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// The wire formats a provider speaks.
const (
	OpenAI    = "openai"
	Anthropic = "anthropic"
)

// Role is whom a message is from.
type Role string

// The roles of the neutral model.
const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message is one message of a conversation. A user message has Text; an
// assistant message Text, ToolCalls or both; a tool message answers the call
// ToolCallID with Text, IsError when the call failed. The instructions are the
// Request's System, not a message.
type Message struct {
	Role       Role
	Text       string
	ToolCalls  []ToolCall
	ToolCallID string
	IsError    bool
}

// ToolCall is a call of a tool the model made: Arguments is the JSON the
// model wrote, an object unless the model wrote something else. TooLong says
// the model wrote more than the gateway keeps of a call's arguments, which
// are then {}.
type ToolCall struct {
	ID        string
	Name      string
	Arguments json.RawMessage
	TooLong   bool
}

// Tool is a tool the model may call: Schema is the JSON Schema of its input,
// an object.
type Tool struct {
	Name        string
	Description string
	Schema      json.RawMessage
}

// Request is one call of the model.
type Request struct {
	System   string
	Messages []Message
	Tools    []Tool
}

// Finish is why the model stopped.
type Finish string

// The reasons a model stops.
const (
	// FinishStop is an answer the model ended itself.
	FinishStop Finish = "stop"
	// FinishToolCalls is an answer that waits for its tool calls' results.
	FinishToolCalls Finish = "tool_calls"
	// FinishLength is an answer cut at the provider's limit of tokens.
	FinishLength Finish = "length"
	// FinishOther is any other reason a provider gave.
	FinishOther Finish = "other"
)

// Usage is what the provider counted, where it said.
type Usage struct {
	InputTokens, OutputTokens int
}

// Response is the model's whole answer.
type Response struct {
	Text      string
	ToolCalls []ToolCall
	Finish    Finish
	// Usage is nil when the provider counted nothing.
	Usage *Usage
}

// Provider is a model behind its wire format.
type Provider interface {
	// Complete sends the request and reads the answer as it streams: onText
	// receives each piece of the answer's text as it arrives, in order, and the
	// Response holds the whole answer once the provider ended it. The context
	// bounds the call.
	Complete(ctx context.Context, req Request, onText func(string)) (Response, error)
}

// Config is the provider's configuration (config.Chat).
type Config struct {
	// Format is OpenAI or Anthropic.
	Format string
	// URL is the base URL: the OpenAI format appends /chat/completions, the
	// Anthropic format /v1/messages.
	URL    string
	APIKey string
	Model  string
	// HTTP is the client; nil makes the gateway's own (Client).
	HTTP *http.Client
}

// New returns the provider of a configuration.
func New(c Config) (Provider, error) {
	h := c.HTTP
	if h == nil {
		h = Client()
	}
	base := strings.TrimRight(c.URL, "/")
	switch c.Format {
	case OpenAI:
		return &openAI{http: h, url: base + "/chat/completions", key: c.APIKey, model: c.Model}, nil
	case Anthropic:
		return &anthropic{http: h, url: base + "/v1/messages", key: c.APIKey, model: c.Model}, nil
	}
	return nil, fmt.Errorf("the chat's provider %q is not one of %s, %s", c.Format, OpenAI, Anthropic)
}

// ErrorKind is what failed in a call of the model.
type ErrorKind string

// The kinds of a failed call.
const (
	// ErrUnreachable is a provider that could not be reached or stopped
	// answering.
	ErrUnreachable ErrorKind = "unreachable"
	// ErrRefused is a provider that answered with an error status.
	ErrRefused ErrorKind = "refused"
	// ErrMalformed is an answer the gateway cannot read, or one larger than
	// it reads.
	ErrMalformed ErrorKind = "malformed"
)

// Error is a call of the model that failed. Error() is for the person and
// never carries the provider's answer; Clip is its start, for the log only,
// with the key taken out.
type Error struct {
	Kind ErrorKind
	// Status is the provider's HTTP status; 0 when it gave none.
	Status int
	Detail string
	Clip   string
}

func (e *Error) Error() string { return e.Detail }
