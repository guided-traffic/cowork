package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/test/stubllm"
)

// provider is a gateway of one format against the stub.
func provider(t *testing.T, stub *stubllm.Server, format, key string) Provider {
	t.Helper()
	url := stub.OpenAIURL()
	if format == Anthropic {
		url = stub.AnthropicURL()
	}
	p, err := New(Config{Format: format, URL: url + "/", APIKey: key, Model: "stub-model"})
	require.NoError(t, err)
	return p
}

// conversation is a turn in the neutral model: the person's question, a tool
// call and its failed answer, and the question again.
func conversation() Request {
	return Request{
		System: "You are the assistant.",
		Messages: []Message{
			{Role: RoleUser, Text: "Show COW-12"},
			{Role: RoleAssistant, Text: "Reading it.", ToolCalls: []ToolCall{{ID: "call_1", Name: "get_ticket", Arguments: json.RawMessage(`{"key":"COW-12"}`)}}},
			{Role: RoleTool, ToolCallID: "call_1", Text: "cowork answered 404", IsError: true},
			{Role: RoleUser, Text: "Try acme/COW-12"},
		},
		Tools: []Tool{{Name: "get_ticket", Description: "Read a ticket", Schema: json.RawMessage(`{"type":"object","properties":{"key":{"type":"string"}}}`)}},
	}
}

var formats = []string{OpenAI, Anthropic}

// The answer streams in pieces, and the tool calls are put together from
// their fragments; the request carries the instructions, the conversation
// with its tool call and answer, the tools, and the key where the format
// takes it (docs/adr/0076).
func TestAStreamedAnswer(t *testing.T) {
	for _, format := range formats {
		t.Run(format, func(t *testing.T) {
			stub := stubllm.New(t)
			stub.Reply(stubllm.Reply{Text: "Opening acme/COW-12 for you.", Calls: []stubllm.Call{
				{ID: "call_7", Name: "get_ticket", Arguments: `{"key": "acme/COW-12", "comments": 3}`},
				{ID: "call_8", Name: "open_ticket", Arguments: `{"key": "acme/COW-12"}`}}})
			var pieces []string
			res, err := provider(t, stub, format, "sk-secret").Complete(context.Background(), conversation(), func(s string) { pieces = append(pieces, s) })
			require.NoError(t, err)

			assert.Equal(t, "Opening acme/COW-12 for you.", res.Text)
			assert.Greater(t, len(pieces), 3, "the text arrives in pieces")
			assert.Equal(t, res.Text, strings.Join(pieces, ""))
			require.Len(t, res.ToolCalls, 2)
			assert.Equal(t, "call_7", res.ToolCalls[0].ID)
			assert.Equal(t, "get_ticket", res.ToolCalls[0].Name)
			assert.JSONEq(t, `{"key": "acme/COW-12", "comments": 3}`, string(res.ToolCalls[0].Arguments))
			assert.Equal(t, "open_ticket", res.ToolCalls[1].Name)
			assert.Equal(t, FinishToolCalls, res.Finish)
			require.NotNil(t, res.Usage)
			assert.Equal(t, 7, res.Usage.OutputTokens)

			got := stub.Requests()
			require.Len(t, got, 1)
			r := got[0]
			assert.Equal(t, "stub-model", r.Model)
			assert.True(t, r.Stream)
			assert.Equal(t, "You are the assistant.", r.System)
			assert.Equal(t, []string{"get_ticket"}, r.Tools)
			require.Len(t, r.Messages, 4)
			assert.Equal(t, "user", r.Messages[0].Role)
			assert.Equal(t, "assistant", r.Messages[1].Role)
			assert.Equal(t, []stubllm.Call{{ID: "call_1", Name: "get_ticket", Arguments: `{"key":"COW-12"}`}}, r.Messages[1].Calls)
			assert.Equal(t, "tool", r.Messages[2].Role)
			assert.Equal(t, "call_1", r.Messages[2].ToolCallID)
			assert.Equal(t, "cowork answered 404", r.Messages[2].Text)
			assert.Equal(t, "Try acme/COW-12", r.Messages[3].Text)
			if format == Anthropic {
				assert.True(t, r.Messages[2].IsError, "the failure is the tool_result's is_error")
				assert.Equal(t, "sk-secret", r.Header.Get("x-api-key"))
				assert.Equal(t, "2023-06-01", r.Header.Get("anthropic-version"))
			} else {
				assert.Equal(t, "Bearer sk-secret", r.Header.Get("Authorization"))
			}
		})
	}
}

// A provider that ignores stream: true answers one body; it is read the same.
func TestAnAnswerThatIsNotStreamed(t *testing.T) {
	for _, format := range formats {
		t.Run(format, func(t *testing.T) {
			stub := stubllm.New(t)
			stub.Reply(stubllm.Reply{Plain: true, Text: "Done.", Calls: []stubllm.Call{{ID: "c1", Name: "watch", Arguments: `{"key":"COW-1"}`}}})
			var text string
			res, err := provider(t, stub, format, "k").Complete(context.Background(), conversation(), func(s string) { text += s })
			require.NoError(t, err)
			assert.Equal(t, "Done.", res.Text)
			assert.Equal(t, "Done.", text)
			require.Len(t, res.ToolCalls, 1)
			assert.JSONEq(t, `{"key":"COW-1"}`, string(res.ToolCalls[0].Arguments))
		})
	}
}

// An OpenAI-compatible server needs no key; none is sent.
func TestNoKeyNoHeader(t *testing.T) {
	stub := stubllm.New(t)
	stub.Reply(stubllm.Reply{Text: "Hi."})
	_, err := provider(t, stub, OpenAI, "").Complete(context.Background(), conversation(), func(string) {})
	require.NoError(t, err)
	assert.Empty(t, stub.Requests()[0].Header.Get("Authorization"))
}

// An error status is the provider's refusal: the person reads the status and
// what it usually means, never the provider's answer; the log's clip has the
// key taken out (docs/adr/0047 D3).
func TestARefusalNeverEchoesTheProvider(t *testing.T) {
	for _, format := range formats {
		t.Run(format, func(t *testing.T) {
			stub := stubllm.New(t)
			stub.RequireKey = true
			_, err := provider(t, stub, format, "sk-wrong-key").Complete(context.Background(), conversation(), func(string) {})
			var e *Error
			require.ErrorAs(t, err, &e)
			assert.Equal(t, ErrRefused, e.Kind)
			assert.Equal(t, http.StatusUnauthorized, e.Status)
			assert.Equal(t, "the provider answered 401: it refused the key", e.Error())
			assert.Contains(t, e.Clip, "invalid api key")
			assert.NotContains(t, e.Clip, "sk-wrong-key", "the key is taken out of the log's clip")
			assert.Contains(t, e.Clip, "[key]")
		})
	}
	stub := stubllm.New(t)
	stub.Reply(
		stubllm.Reply{Status: http.StatusBadRequest, Body: `{"error": {"message": "This model's maximum context length is 32768 tokens"}}`},
		stubllm.Reply{Status: http.StatusInternalServerError, Body: strings.Repeat("x", 5000)},
		stubllm.Reply{Status: http.StatusNotFound, Body: `{"error": "model not found"}`},
	)
	p := provider(t, stub, OpenAI, "")
	for _, want := range []string{"the conversation is longer than the model reads", "it failed", "it knows no such model"} {
		_, err := p.Complete(context.Background(), conversation(), func(string) {})
		var e *Error
		require.ErrorAs(t, err, &e)
		assert.Contains(t, e.Error(), want)
		assert.LessOrEqual(t, len([]rune(e.Clip)), clipLength+1, "a long answer is clipped")
	}
}

// No redirect is followed: a provider answers where it was configured
// (docs/adr/0076, the gateway).
func TestNoRedirectIsFollowed(t *testing.T) {
	elsewhere := stubllm.New(t)
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+"/v1/chat/completions", http.StatusTemporaryRedirect)
	}))
	t.Cleanup(redirect.Close)
	p, err := New(Config{Format: OpenAI, URL: redirect.URL + "/v1", Model: "m"})
	require.NoError(t, err)
	_, err = p.Complete(context.Background(), conversation(), func(string) {})
	var e *Error
	require.ErrorAs(t, err, &e)
	assert.Equal(t, http.StatusTemporaryRedirect, e.Status)
	assert.Contains(t, e.Error(), "a redirect, which the chat does not follow")
	assert.Empty(t, elsewhere.Requests(), "the redirect's target is never asked")
}

// What the gateway cannot read ends the call: a line longer than it reads, a
// stream that is not the format, one that breaks off before its end.
func TestAnAnswerTheGatewayCannotRead(t *testing.T) {
	for name, c := range map[string]struct {
		format, raw, want string
	}{
		"an oversized line":           {OpenAI, "data: " + strings.Repeat("x", maxLine+10) + "\n\n", "a line of the provider's answer is longer"},
		"not the OpenAI format":       {OpenAI, "data: {not json}\n\n", "not the OpenAI format"},
		"an OpenAI stream cut off":    {OpenAI, `data: {"choices":[{"delta":{"content":"Hel"}}]}` + "\n\n", "ended before its end"},
		"not the Anthropic format":    {Anthropic, "event: x\ndata: [1,2]\n\n", "not the Anthropic format"},
		"an Anthropic stream cut off": {Anthropic, "event: message_start\ndata: {\"type\":\"message_start\"}\n\n", "ended before its end"},
		"an error in the stream": {Anthropic, "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"Overloaded\"}}\n\n",
			"failed while it answered"},
	} {
		t.Run(name, func(t *testing.T) {
			stub := stubllm.New(t)
			stub.Reply(stubllm.Reply{Raw: c.raw})
			_, err := provider(t, stub, c.format, "k").Complete(context.Background(), conversation(), func(string) {})
			var e *Error
			require.ErrorAs(t, err, &e)
			assert.Contains(t, e.Error(), c.want)
		})
	}
}

// A provider that cannot be reached, or stops answering, ends the call; the
// caller's own end is the caller's error.
func TestASilentOrAbsentProvider(t *testing.T) {
	gone := httptest.NewServer(http.NotFoundHandler())
	gone.Close()
	p, err := New(Config{Format: OpenAI, URL: gone.URL + "/v1", Model: "m"})
	require.NoError(t, err)
	_, err = p.Complete(context.Background(), conversation(), func(string) {})
	var e *Error
	require.ErrorAs(t, err, &e)
	assert.Equal(t, ErrUnreachable, e.Kind)
	assert.Equal(t, "the provider could not be reached", e.Error())

	defer func(d time.Duration) { idleTimeout = d }(idleTimeout)
	idleTimeout = 100 * time.Millisecond
	stub := stubllm.New(t)
	stub.Reply(stubllm.Reply{Text: "slow answer", Stall: 2 * time.Second})
	_, err = provider(t, stub, Anthropic, "k").Complete(context.Background(), conversation(), func(string) {})
	require.ErrorAs(t, err, &e)
	assert.Contains(t, e.Error(), "the provider sent nothing for 100ms")

	stub.Reply(stubllm.Reply{Text: "slow answer", Delay: 2 * time.Second})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = provider(t, stub, OpenAI, "k").Complete(ctx, conversation(), func(string) {})
	assert.True(t, errors.Is(err, context.DeadlineExceeded), "the caller's deadline is the caller's: %v", err)
}

// A reasoning model's <think> block is not the answer, however the stream
// cuts it.
func TestTheThinkFilter(t *testing.T) {
	for name, pieces := range map[string][]string{
		"whole":        {"<think>I should look.</think>\n\nThe answer."},
		"cut in tags":  {"<thi", "nk>pondering", " more</th", "ink>", "The answer."},
		"cut per char": strings.Split("<think>x</think>The answer.", ""),
		"no think":     {"The ", "answer."},
	} {
		var f thinkFilter
		var out strings.Builder
		for _, p := range pieces {
			out.WriteString(f.feed(p))
		}
		out.WriteString(f.flush())
		assert.Equal(t, "The answer.", out.String(), name)
	}
	var f thinkFilter
	assert.Equal(t, "a <thin", f.feed("a <thin")+f.flush(), "an unfinished tag at the end is text")

	stub := stubllm.New(t)
	stub.Reply(stubllm.Reply{Text: "<think>The person wants COW-12.</think>\n\nHere it is."})
	var streamed string
	res, err := provider(t, stub, OpenAI, "").Complete(context.Background(), conversation(), func(s string) { streamed += s })
	require.NoError(t, err)
	assert.Equal(t, "Here it is.", res.Text)
	assert.Equal(t, "Here it is.", streamed)
}

// A model that writes on is cut, not followed: the text the gateway keeps and
// passes on, a call's arguments, the number of calls; both formats bound the
// answer by max_tokens (docs/adr/0076, the gateway).
func TestTheGatewayBoundsAnAnswer(t *testing.T) {
	for _, format := range formats {
		t.Run(format, func(t *testing.T) {
			stub := stubllm.New(t)
			calls := make([]stubllm.Call, 1, maxCalls+6)
			calls[0] = stubllm.Call{ID: "big", Name: "comment", Arguments: `{"text": "` + strings.Repeat("x", maxArguments) + `"}`}
			for i := range maxCalls + 5 {
				calls = append(calls, stubllm.Call{ID: fmt.Sprintf("c%d", i), Name: "watch", Arguments: `{}`})
			}
			stub.Reply(stubllm.Reply{Plain: true, Text: strings.Repeat("y", maxAnswerText+100), Calls: calls})
			passed := 0
			res, err := provider(t, stub, format, "k").Complete(context.Background(), conversation(), func(s string) { passed += len(s) })
			require.NoError(t, err)
			assert.Len(t, res.Text, maxAnswerText, "the text is cut at the bound")
			assert.Equal(t, maxAnswerText, passed, "and what is cut is not passed on")
			require.Len(t, res.ToolCalls, maxCalls)
			assert.True(t, res.ToolCalls[0].TooLong)
			assert.JSONEq(t, `{}`, string(res.ToolCalls[0].Arguments))
			assert.False(t, res.ToolCalls[1].TooLong)
			assert.Equal(t, maxTokens, stub.Requests()[0].MaxTokens)
		})
	}
	stub := stubllm.New(t)
	stub.Reply(stubllm.Reply{Calls: []stubllm.Call{{ID: "big", Name: "comment", Arguments: `{"text": "` + strings.Repeat("z", maxArguments) + `"}`}}})
	res, err := provider(t, stub, Anthropic, "k").Complete(context.Background(), conversation(), func(string) {})
	require.NoError(t, err)
	require.Len(t, res.ToolCalls, 1)
	assert.True(t, res.ToolCalls[0].TooLong, "the fragments of a stream are bounded alike")
}

// An answer of white space only is no text, in either format: nothing is
// passed on, and the answer's text is empty (a message of the model that
// carries only white space is no message the next turn can send).
func TestAnAnswerOfWhiteSpace(t *testing.T) {
	for _, format := range formats {
		for _, plain := range []bool{false, true} {
			stub := stubllm.New(t)
			stub.Reply(stubllm.Reply{Text: "\n \n\t", Plain: plain})
			passed := 0
			res, err := provider(t, stub, format, "k").Complete(context.Background(), conversation(), func(string) { passed++ })
			require.NoError(t, err, format)
			assert.Empty(t, res.Text, "%s plain=%t", format, plain)
			assert.Zero(t, passed, "%s plain=%t", format, plain)
		}
		stub := stubllm.New(t)
		stub.Reply(stubllm.Reply{Text: "\n\nHello  there.\n"})
		res, err := provider(t, stub, format, "k").Complete(context.Background(), conversation(), func(string) {})
		require.NoError(t, err)
		assert.Equal(t, "Hello  there.\n", res.Text, "%s: only the white space before the first word goes", format)
	}
}

func TestHeadAndCut(t *testing.T) {
	head, cut := Head("héllo wörld", 5)
	assert.Equal(t, "héllo", head)
	assert.True(t, cut)
	head, cut = Head("héllo", 5)
	assert.Equal(t, "héllo", head)
	assert.False(t, cut)
	assert.Equal(t, "h", Cut("hé", 2), "never in the middle of a character")
	assert.Equal(t, "hé", Cut("hé", 3))
	assert.LessOrEqual(t, len(clip(strings.Repeat("ü", 1<<20), "")), clipLength*2+len("…"))
}
