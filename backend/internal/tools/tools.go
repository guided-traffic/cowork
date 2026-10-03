// Package tools is the catalogue of cowork's workflow tools
// (docs/adr/0042): each tool is a name, a description that names the agent
// limits it can run into (D3), a JSON Schema of its input, and a procedure
// that works only through the cowork API (docs/adr/0040 D3) and answers
// Markdown with the canonical key of what it touched (docs/adr/0042 D4).
//
// The catalogue knows no transport and holds no global state. A host builds a
// Session — the generated API client over whatever HTTP doer it chooses, a
// memory for session_start, and the working directory where it has one — and
// calls the tools with it: the MCP server of cmd/cowork-mcp over stdio, and
// any other host the same way.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
)

// Surface says where a tool makes sense.
type Surface int

const (
	// Anywhere is a tool that takes everything it needs as arguments.
	Anywhere Surface = iota
	// Terminal is a tool that reads the working directory — the git remotes
	// and .cowork.yaml (docs/adr/0041 D4); a host without one leaves it out.
	Terminal
)

// Tool is one entry of the catalogue.
type Tool struct {
	// Name is how a model calls the tool.
	Name string
	// Description is what the model reads about it; Describe adds what the
	// token in use may and may not do.
	Description string
	// Surface says where the tool makes sense.
	Surface Surface
	// ReadOnly marks a tool that changes nothing.
	ReadOnly bool
	// Operations are the API operations the tool calls, by operationId: the
	// start-up check refuses an API that lacks one (docs/adr/0040 D5), and a
	// test holds each to the API document (docs/adr/0042 D6).
	Operations []string

	limits   func(Token) string
	schema   *jsonschema.Schema
	resolved *jsonschema.Resolved
	run      func(ctx context.Context, s *Session, args json.RawMessage) (string, error)
}

// Schema is the JSON Schema (draft 2020-12) of the tool's input, an object.
func (t Tool) Schema() json.RawMessage {
	b, err := json.Marshal(t.schema)
	if err != nil {
		panic(fmt.Sprintf("tool %s: encode its schema: %v", t.Name, err))
	}
	return b
}

// Describe is the description with the limits of the token in use, so the
// model knows before calling what is a person's (docs/adr/0042 D3,
// docs/adr/0043 D6). A nil token describes the limits without it.
func (t Tool) Describe(tok *Token) string {
	if t.limits == nil {
		return t.Description
	}
	var known Token
	if tok != nil {
		known = *tok
	}
	if l := t.limits(known); l != "" {
		return t.Description + "\n\n" + l
	}
	return t.Description
}

// Result is what a call answers: Markdown, and whether it is a failure — the
// API's refusal with its code, or input the schema refuses.
type Result struct {
	Text    string
	IsError bool
}

// Call validates the arguments against the schema and runs the tool. A
// refusal of the API is a result like any other, marked as an error, with the
// code and the message the API gave (docs/adr/0040 D3).
func (t Tool) Call(ctx context.Context, s *Session, args json.RawMessage) Result {
	if len(strings.TrimSpace(string(args))) == 0 || string(args) == "null" {
		args = json.RawMessage("{}")
	}
	var instance any
	if err := json.Unmarshal(args, &instance); err != nil {
		return Result{Text: "The arguments are not JSON: " + err.Error(), IsError: true}
	}
	if err := t.resolved.Validate(instance); err != nil {
		return Result{Text: "The arguments do not match the tool's schema: " + err.Error(), IsError: true}
	}
	text, err := t.run(ctx, s, args)
	if err != nil {
		return Result{Text: failure(err), IsError: true}
	}
	return Result{Text: text}
}

// define builds a tool whose input is the type In: its schema is inferred
// from the type — json names, jsonschema descriptions, omitempty optional —
// and shaped further by shape, and the arguments are decoded into it.
func define[In any](t Tool, shape func(*jsonschema.Schema), run func(ctx context.Context, s *Session, in In) (string, error)) Tool {
	schema, err := jsonschema.For[In](nil)
	if err != nil {
		panic(fmt.Sprintf("tool %s: infer its schema: %v", t.Name, err))
	}
	if shape != nil {
		shape(schema)
	}
	if t.resolved, err = schema.Resolve(nil); err != nil {
		panic(fmt.Sprintf("tool %s: resolve its schema: %v", t.Name, err))
	}
	t.schema = schema
	t.run = func(ctx context.Context, s *Session, args json.RawMessage) (string, error) {
		var in In
		if err := json.Unmarshal(args, &in); err != nil {
			return "", fmt.Errorf("decode the arguments: %w", err)
		}
		return run(ctx, s, in)
	}
	return t
}

// enum restricts a property to a list of values.
func enum(s *jsonschema.Schema, property string, values ...string) {
	p := s.Properties[property]
	if p == nil {
		panic("no property " + property)
	}
	target := p
	if p.Items != nil {
		target = p.Items
	}
	target.Enum = make([]any, 0, len(values))
	for _, v := range values {
		target.Enum = append(target.Enum, v)
	}
}

// bound restricts an integer property to a range.
func bound(s *jsonschema.Schema, property string, lo, hi float64) {
	p := s.Properties[property]
	if p == nil {
		panic("no property " + property)
	}
	p.Minimum, p.Maximum = &lo, &hi
}

// Catalogue is the tool set of the first release (docs/adr/0042 D1, D2): the
// twelve workflow tools, record_answer and create_project, and the api
// escape hatch. surfaces narrows it; none is every tool.
func Catalogue(surfaces ...Surface) []Tool {
	all := []Tool{
		sessionStartTool(), getTicketTool(), searchTool(), fileTicketTool(), recordStateTool(), openQuestionTool(),
		recordAnswerTool(), commentTool(), transitionTool(), setProgressTool(), linkTool(), watchTool(),
		finishWorkTool(), createProjectTool(), apiTool(),
	}
	if len(surfaces) == 0 {
		return all
	}
	out := make([]Tool, 0, len(all))
	for _, t := range all {
		if slices.Contains(surfaces, t.Surface) {
			out = append(out, t)
		}
	}
	return out
}

// Operations are the API operations the tools call, each once.
func Operations(catalogue []Tool) []string {
	var ops []string
	for _, t := range catalogue {
		for _, op := range t.Operations {
			if !slices.Contains(ops, op) {
				ops = append(ops, op)
			}
		}
	}
	slices.Sort(ops)
	return ops
}

// errUsage is a call the tool itself refuses before asking the API: a key it
// cannot resolve, a combination of arguments that means nothing.
var errUsage = errors.New("usage")

func usage(format string, args ...any) error {
	return fmt.Errorf("%w: %s", errUsage, fmt.Sprintf(format, args...))
}

// failure renders an error as the tool's answer.
func failure(err error) string {
	var api *APIError
	var text textError
	switch {
	case errors.As(err, &text):
		return string(text)
	case errors.As(err, &api):
		return api.Markdown()
	case errors.Is(err, errUsage):
		return strings.TrimPrefix(err.Error(), errUsage.Error()+": ")
	default:
		return "The call failed before cowork answered: " + err.Error()
	}
}
