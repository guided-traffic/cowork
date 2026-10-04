package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"
)

// The bounds of a call of the model.
const (
	// headerTimeout is how long a provider may take before it starts to
	// answer: a server on the operator's machine loads the model on the first
	// call.
	headerTimeout = 2 * time.Minute
	// maxLine is the longest line of a stream the gateway reads, maxStream
	// what a stream may carry in all, maxBody what an answer that is not
	// streamed may.
	maxLine   = 1 << 20
	maxStream = 32 << 20
	maxBody   = 8 << 20
	// maxErrorBody is how much of an error answer is read, clipLength how
	// much of it reaches the log.
	maxErrorBody = 64 << 10
	clipLength   = 300
	// maxAnswerText is how much of an answer's text the gateway keeps and
	// passes on, maxArguments how much of one call's arguments, maxCalls how
	// many calls of one answer: a model that writes on is cut, not followed.
	maxAnswerText = 256 << 10
	maxArguments  = 64 << 10
	maxCalls      = 64
	// maxTokens is the most an answer may take; Anthropic requires it, and an
	// OpenAI server takes it as a bound.
	maxTokens = 4096
)

// idleTimeout is how long a streamed answer may stay silent; a variable so
// that the tests need not wait for it.
var idleTimeout = 90 * time.Second

// Client is the gateway's HTTP client: it follows no redirect — a provider
// answers where it was configured, or the call fails — and gives up on a
// provider that has not begun to answer within two minutes.
func Client() *http.Client {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.ResponseHeaderTimeout = headerTimeout
	return &http.Client{
		Transport:     t,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// post sends a JSON body and returns the answer when its status is 200; any
// other status is an *Error that says which, and a failure to reach the
// provider is one too. A cancelled context is returned as it is.
func post(ctx context.Context, h *http.Client, url string, header http.Header, body any, key string) (*http.Response, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode the request to the provider: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("make the request to the provider: %w", err)
	}
	req.Header = header
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream, application/json")
	req.Header.Set("User-Agent", "cowork")
	res, err := h.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &Error{Kind: ErrUnreachable, Detail: "the provider could not be reached", Clip: clip(err.Error(), key)}
	}
	if res.StatusCode != http.StatusOK {
		defer func() { _ = res.Body.Close() }()
		return nil, refused(res, key)
	}
	return res, nil
}

// refused is the error of an answer with another status than 200: the status,
// what it usually means, and the provider's message for the log.
func refused(res *http.Response, key string) *Error {
	raw, _ := io.ReadAll(io.LimitReader(res.Body, maxErrorBody))
	msg := providerMessage(raw)
	status := res.StatusCode
	detail := fmt.Sprintf("the provider answered %d", status)
	switch {
	case status >= 300 && status < 400:
		detail += ": a redirect, which the chat does not follow — the provider's COWORK_CHAT_<ID>_URL names another address than the provider's"
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		detail += ": it refused the key"
	case status == http.StatusNotFound:
		detail += ": it knows no such model or path — check the provider's COWORK_CHAT_<ID>_URL and COWORK_CHAT_<ID>_MODEL"
	case status == http.StatusTooManyRequests:
		detail += ": it limits the requests; try again later"
	case contextExceeded(msg):
		detail += ": the conversation is longer than the model reads; begin a new one"
	case status >= 400 && status < 500:
		detail += ": it refused the request"
	case status >= 500:
		detail += ": it failed"
	}
	return &Error{Kind: ErrRefused, Status: status, Detail: detail, Clip: clip(msg, key)}
}

// providerMessage is the message of an error answer: error.message as OpenAI
// and Anthropic write it, error as a string as LM Studio may, or the body.
func providerMessage(raw []byte) string {
	var body struct {
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal(raw, &body) == nil && len(body.Error) > 0 {
		var obj struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(body.Error, &obj) == nil && obj.Message != "" {
			return obj.Message
		}
		var s string
		if json.Unmarshal(body.Error, &s) == nil && s != "" {
			return s
		}
	}
	return string(raw)
}

// contextExceeded reports whether a provider's message says the conversation
// is longer than the model reads.
func contextExceeded(msg string) bool {
	m := strings.ToLower(msg)
	return strings.Contains(m, "prompt is too long") ||
		(strings.Contains(m, "context") && (strings.Contains(m, "length") || strings.Contains(m, "window") ||
			strings.Contains(m, "exceed") || strings.Contains(m, "maximum")))
}

// clip is the start of a text for the log: the key taken out, controls made
// spaces, at most clipLength characters.
func clip(s, key string) string {
	s = Cut(s, maxErrorBody)
	if key != "" {
		s = strings.ReplaceAll(s, key, "[key]")
	}
	s = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s))
	if head, cut := Head(s, clipLength); cut {
		return head + "…"
	}
	return s
}

// Head is the first n characters of s, and whether s has more; it reads no
// further than it keeps.
func Head(s string, n int) (string, bool) {
	i := 0
	for count := 0; i < len(s); count++ {
		if count == n {
			return s[:i], true
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
	}
	return s, false
}

// Cut is s cut to at most n bytes, at the start of a character.
func Cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// answerText is an answer's text as it arrives: white space before its first
// word is dropped — an answer of white space only is no text —, the rest
// kept and passed on up to maxAnswerText, and what comes after dropped.
type answerText struct {
	b       strings.Builder
	onText  func(string)
	started bool
}

func (a *answerText) add(piece string) {
	if !a.started {
		piece = strings.TrimLeftFunc(piece, unicode.IsSpace)
		a.started = piece != ""
	}
	piece = Cut(piece, maxAnswerText-a.b.Len())
	if piece == "" {
		return
	}
	a.b.WriteString(piece)
	a.onText(piece)
}

func (a *answerText) String() string { return a.b.String() }

// argumentsOf are a call's arguments as the gateway keeps them, at most
// maxArguments.
type argumentsOf struct {
	b       strings.Builder
	tooLong bool
}

func (a *argumentsOf) add(piece string) {
	if a.tooLong || a.b.Len()+len(piece) > maxArguments {
		a.tooLong = true
		return
	}
	a.b.WriteString(piece)
}

// call is the call the arguments belong to.
func (a *argumentsOf) call(id, name string) ToolCall {
	if a.tooLong {
		return ToolCall{ID: id, Name: name, Arguments: json.RawMessage("{}"), TooLong: true}
	}
	return ToolCall{ID: id, Name: name, Arguments: arguments(a.b.String())}
}

func malformed(detail string) *Error {
	return &Error{Kind: ErrMalformed, Detail: detail}
}

// streamed reports whether an answer is a stream of server-sent events; a
// provider that ignored stream: true answers one JSON body.
func streamed(res *http.Response) bool {
	mediaType, _, err := mime.ParseMediaType(res.Header.Get("Content-Type"))
	return err == nil && mediaType == "text/event-stream"
}

// readBody reads an answer that is not streamed, at most maxBody.
func readBody(ctx context.Context, res *http.Response, key string) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxBody+1))
	switch {
	case ctx.Err() != nil:
		return nil, ctx.Err()
	case err != nil:
		return nil, &Error{Kind: ErrUnreachable, Detail: "the provider's answer broke off", Clip: clip(err.Error(), key)}
	case len(raw) > maxBody:
		return nil, malformed("the provider's answer is larger than the chat reads")
	}
	return raw, nil
}

// readEvents hands f the payload of every data: line of an event stream,
// until f says the stream ended or it does. A line longer than maxLine, a
// stream longer than maxStream and idleTimeout without a line end it with an
// *Error; ctx is the caller's, which ended when its error is returned, and
// cancel ends the call's own.
func readEvents(ctx context.Context, cancel context.CancelFunc, body io.Reader, key string, f func(data []byte) (end bool, err error)) error {
	var silent atomic.Bool
	watch := time.AfterFunc(idleTimeout, func() {
		silent.Store(true)
		cancel()
	})
	defer watch.Stop()
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 0, 64<<10), maxLine)
	total := 0
	for sc.Scan() {
		watch.Reset(idleTimeout)
		line := sc.Bytes()
		if total += len(line) + 1; total > maxStream {
			return malformed("the provider's answer is longer than the chat reads")
		}
		data, ok := bytes.CutPrefix(line, []byte("data:"))
		if !ok {
			continue
		}
		end, err := f(bytes.TrimSpace(data))
		if err != nil || end {
			return err
		}
	}
	err := sc.Err()
	switch {
	case errors.Is(err, bufio.ErrTooLong):
		return malformed("a line of the provider's answer is longer than the chat reads")
	case silent.Load():
		return &Error{Kind: ErrUnreachable, Detail: fmt.Sprintf("the provider sent nothing for %s", idleTimeout)}
	case ctx.Err() != nil:
		return ctx.Err()
	case err != nil:
		return &Error{Kind: ErrUnreachable, Detail: "the provider's answer broke off", Clip: clip(err.Error(), key)}
	}
	return nil
}

// arguments are a tool call's arguments as the gateway keeps them: the JSON
// the model wrote, or {} for none.
func arguments(raw string) json.RawMessage {
	if strings.TrimSpace(raw) == "" {
		return json.RawMessage("{}")
	}
	return json.RawMessage(raw)
}
