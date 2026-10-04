package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/chat"
	"github.com/guided-traffic/cowork/backend/internal/llm"
	"github.com/guided-traffic/cowork/backend/internal/problem"
	"github.com/guided-traffic/cowork/backend/internal/requestid"
	"github.com/guided-traffic/cowork/backend/internal/store"
	"github.com/guided-traffic/cowork/backend/internal/tools"
)

// opRunChatTurn is a turn of the chat, served outside the generated server
// like the event stream: a stream is not a response a handler returns.
const opRunChatTurn = "runChatTurn"

// defaultKeepAlive is how long a turn stays silent before a comment keeps the
// proxies from closing it, well below the frontend's read timeout.
const defaultKeepAlive = 10 * time.Second

// ChatOptions configures the chat in the UI (docs/adr/0076).
type ChatOptions struct {
	// Provider is the gateway to the model; Kind its wire format and Model
	// the model's name, which the availability shows.
	Provider llm.Provider
	Kind     string
	Model    string
	// Inside declares the provider inside the installation's trust boundary:
	// the chat is available in every tenant, not only where the tenant's
	// administrators allowed an outside one. Fingerprint names the provider
	// a tenant's consent is given to (config.Chat.Fingerprint).
	Inside      bool
	Fingerprint string
	// TurnTimeout bounds a turn, MaxSteps the calls of the model in it, and
	// TurnsPerPerson the turns one person runs at once on this replica; 0
	// switches each off (docs/adr/0039 D2).
	TurnTimeout    time.Duration
	MaxSteps       int
	TurnsPerPerson int
	// Shutdown ends every running turn when it is done: the server is going
	// down, and a turn says so and ends instead of holding the drain
	// (docs/adr/0054 D9). nil never ends one.
	Shutdown context.Context
	// KeepAlive is the silence after which a turn writes a comment; zero
	// means ten seconds.
	KeepAlive time.Duration
	// Loopback is the whole server's handler, which a tool call of the chat
	// goes through as any request does; nil sends it to the API's pipeline
	// alone.
	Loopback func() http.Handler
}

// GetChatAvailability answers whether the tenant's members may hold a
// conversation, and with which model (docs/adr/0076).
func (s *Server) GetChatAvailability(ctx context.Context, _ apigen.GetChatAvailabilityRequestObject) (apigen.GetChatAvailabilityResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, read); perr != nil {
		return nil, perr
	}
	a, err := s.h.chatAvailability(ctx, t)
	if err != nil {
		return nil, err
	}
	return apigen.GetChatAvailability200JSONResponse(a), nil
}

// chatAvailability is the chat's availability in a tenant: a provider
// configured, and declared inside the installation's trust boundary or
// allowed by the tenant. The provider's address is never part of it.
func (h *handler) chatAvailability(ctx context.Context, t tenantScope) (apigen.ChatAvailability, error) {
	c := h.opts.Chat
	if c == nil {
		reason := apigen.ChatUnavailableReasonNotConfigured
		return apigen.ChatAvailability{Provider: nullableOf[apigen.ChatProvider](nil), Model: nullableOf[string](nil),
			Reason: nullableOf(&reason)}, nil
	}
	kind := apigen.ChatProvider(c.Kind)
	a := apigen.ChatAvailability{Provider: nullableOf(&kind), Model: nullableOf(&c.Model), Inside: c.Inside,
		Reason: nullableOf[apigen.ChatUnavailableReason](nil), Available: c.Inside}
	if c.Inside {
		return a, nil
	}
	err := h.opts.DB.InTenant(ctx, t.ID, func(r *store.Reader) error {
		row, err := r.GetTenant(ctx, t.ID)
		a.Available = consented(row.ChatExternalAllowed, row.ChatExternalProvider, c.Fingerprint)
		return err
	})
	if err != nil {
		return apigen.ChatAvailability{}, err
	}
	if !a.Available {
		reason := apigen.ChatUnavailableReasonNotAllowedInTenant
		a.Reason = nullableOf(&reason)
	}
	return a, nil
}

// serveChat runs a turn of the chat (docs/adr/0076). The pipeline has
// authenticated the person by their session, held the request to the CSRF
// check, refused it to an agent, admitted the person to the tenant, and read
// and validated the body within the request timeout. The turn is bounded by
// its own, and answers its events as they come, each flushed.
func (h *handler) serveChat(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, read); perr != nil {
		problem.Write(w, r, perr)
		return
	}
	a, err := h.chatAvailability(ctx, t)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	if !a.Available {
		problem.Write(w, r, problem.New(problem.ChatUnavailable, unavailable(a)))
		return
	}
	var body apigen.ChatTurn
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		problem.Write(w, r, problem.New(problem.ValidationFailed, "the request body is not valid JSON for this route"))
		return
	}
	decisions := deref(body.Confirmations)
	if perr := chat.Check(body.Messages, decisions); perr != nil {
		problem.Write(w, r, perr)
		return
	}
	turn, perr := h.chatTurn(r, t, body, decisions)
	if perr != nil {
		problem.Write(w, r, perr)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		problem.Write(w, r, problem.New(problem.Internal, "this server cannot stream a turn"))
		return
	}
	done, perr := h.startTurn(principal(ctx).PersonID)
	if perr != nil {
		problem.Write(w, r, perr)
		return
	}
	defer done()
	h.streamTurn(w, r, flusher, t, turn)
}

// streamTurn runs a turn and answers its events: the stream is open before
// the model is asked, and ends with done whatever ends the turn — its time,
// the provider, the consent, the server's shutdown.
func (h *handler) streamTurn(w http.ResponseWriter, r *http.Request, flusher http.Flusher, t tenantScope, turn chat.Turn) {
	c := h.opts.Chat
	ctx, cancel := context.WithCancelCause(r.Context())
	defer cancel(nil)
	if c.Shutdown != nil {
		defer context.AfterFunc(c.Shutdown, func() { cancel(errShutdown) })()
	}
	if c.TurnTimeout > 0 {
		var stop context.CancelFunc
		ctx, stop = context.WithTimeout(ctx, c.TurnTimeout)
		defer stop()
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	out := &eventWriter{w: w, flusher: flusher, last: time.Now()}
	// The comments end before the handler does, whatever ends it.
	defer out.keepAlive(orDefault(c.KeepAlive, defaultKeepAlive))()
	allowed := func(ctx context.Context) error {
		a, err := h.chatAvailability(ctx, t)
		switch {
		case err != nil:
			return err
		case !a.Available:
			return problem.New(problem.ChatUnavailable, unavailable(a)+"; the turn ends here")
		}
		return nil
	}
	end := chat.Run(ctx, chat.Options{Provider: c.Provider, MaxSteps: c.MaxSteps, Now: h.opts.Now, Allowed: allowed}, turn, out)
	if end.Err != nil {
		if context.Cause(ctx) == errShutdown {
			end.Err = errShutdown
		}
		out.event("error", problem.BodyOf(r, h.turnProblem(r, end.Err)))
	}
	out.event("done", apigen.ChatDoneEvent{Messages: end.Messages, Reason: end.End})
}

// errShutdown ends the turns the server's shutdown finds running.
var errShutdown = errors.New("the server is shutting down")

// startTurn counts a person's turn while it runs: one more than
// TurnsPerPerson is refused before its stream opens. The count is this
// replica's (docs/adr/0039).
func (h *handler) startTurn(person uuid.UUID) (done func(), perr *problem.Error) {
	limit := h.opts.Chat.TurnsPerPerson
	h.turnsMu.Lock()
	defer h.turnsMu.Unlock()
	if limit > 0 && h.turns[person] >= limit {
		return nil, problem.New(problem.ChatBusy, fmt.Sprintf("%d turns of yours are running already; one ends, or is stopped, first", limit))
	}
	h.turns[person]++
	return func() {
		h.turnsMu.Lock()
		defer h.turnsMu.Unlock()
		if h.turns[person]--; h.turns[person] <= 0 {
			delete(h.turns, person)
		}
	}, nil
}

// chatTurn is the turn of a request: the conversation, the page, and the API
// client of the person's agent — the session cookie of the request, the
// installation's origin, the chat's mark with the conversation's id.
func (h *handler) chatTurn(r *http.Request, t tenantScope, body apigen.ChatTurn, decisions []apigen.ChatConfirmation) (chat.Turn, *problem.Error) {
	cookie, err := r.Cookie(auth.SessionCookie)
	if err != nil {
		return chat.Turn{}, problem.New(problem.Unauthenticated, "a turn of the chat needs the session cookie")
	}
	page := chat.Page{}
	if body.Context != nil {
		page = chat.Page{Path: deref(body.Context.Path), Project: deref(body.Context.Project), Ticket: deref(body.Context.Ticket)}
	}
	seen := new(atomic.Bool)
	loop := chat.Loopback{Handler: h, Tenant: t.Slug, RemoteAddr: r.RemoteAddr, ForwardedFor: r.Header.Values("X-Forwarded-For"),
		Confidential: seen}
	if l := h.opts.Chat.Loopback; l != nil && l() != nil {
		loop.Handler = l()
	}
	p := principal(r.Context())
	mark := chat.Mark(h.opts.Chat.Model, body.Conversation)
	session, err := chat.NewSession(loop, h.opts.BaseOrigin, chat.Editor(cookie.Value, h.opts.BaseOrigin, mark), mark, page.Project,
		tools.Person{ID: p.PersonID, Name: p.DisplayName})
	if err != nil {
		h.logger.Error("the chat's session failed", "request_id", requestid.From(r.Context()), "error", err)
		return chat.Turn{}, problem.New(problem.Internal, "internal error")
	}
	return chat.Turn{Tenant: t.Slug, TenantName: t.Name, Conversation: body.Conversation, Page: page, Messages: body.Messages,
		Confirmations: decisions, Session: session, Confidential: seen}, nil
}

// chatProvider is the fingerprint of the chat's provider, or "" without one.
func (h *handler) chatProvider() string {
	if h.opts.Chat == nil {
		return ""
	}
	return h.opts.Chat.Fingerprint
}

// unavailable says why a tenant has no chat.
func unavailable(a apigen.ChatAvailability) string {
	if reason, err := a.Reason.Get(); err == nil && reason == apigen.ChatUnavailableReasonNotAllowedInTenant {
		return "the chat's provider is outside the installation, and the tenant's administrators have not allowed it"
	}
	return "this installation configures no chat provider"
}

// turnProblem is the problem a turn that failed reports in its stream: the
// turn's time, the provider — whose answer reaches the log only as a clip —
// or anything else, which the log has.
func (h *handler) turnProblem(r *http.Request, err error) *problem.Error {
	var provider *llm.Error
	var perr *problem.Error
	id := requestid.From(r.Context())
	switch {
	case errors.As(err, &perr):
		return perr
	case errors.Is(err, errShutdown):
		return problem.New(problem.NotReady, "the server is shutting down: send the turn again")
	case errors.Is(err, context.DeadlineExceeded):
		return problem.New(problem.Timeout, fmt.Sprintf("the turn took longer than %s, the limit of COWORK_CHAT_TURN_TIMEOUT", h.opts.Chat.TurnTimeout))
	case errors.As(err, &provider):
		h.logger.Warn("the chat's provider failed", "request_id", id, "kind", provider.Kind, "status", provider.Status, "answer", provider.Clip)
		return problem.New(problem.ChatProviderFailed, provider.Detail)
	case errors.Is(err, context.Canceled):
		return problem.New(problem.Internal, "the turn ended before its end")
	}
	h.logger.Error("a turn of the chat failed", "request_id", id, "error", err)
	return problem.New(problem.Internal, "internal error")
}

// orDefault is v, or fallback for zero.
func orDefault(v, fallback time.Duration) time.Duration {
	if v > 0 {
		return v
	}
	return fallback
}

// eventWriter writes a turn's server-sent events, each flushed as it is
// written, and a comment when the turn stays silent: one goroutine writes the
// turn's events, another the comments.
type eventWriter struct {
	mu      sync.Mutex
	w       io.Writer
	flusher http.Flusher
	last    time.Time
}

func (e *eventWriter) event(name string, data any) {
	b, err := json.Marshal(data)
	if err != nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	// #nosec G705 -- text/event-stream of a fixed event name and JSON the server encodes; no HTML
	_, _ = fmt.Fprintf(e.w, "event: %s\ndata: %s\n\n", name, b)
	e.flusher.Flush()
	e.last = time.Now()
}

// keepAlive writes ": keep-alive" whenever interval passed without a write,
// until stop is called; stop waits for the writer to end.
func (e *eventWriter) keepAlive(interval time.Duration) (stop func()) {
	done, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		tick := time.NewTicker(max(interval/5, time.Millisecond))
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case <-tick.C:
				e.mu.Lock()
				if time.Since(e.last) >= interval {
					_, _ = io.WriteString(e.w, ": keep-alive\n\n")
					e.flusher.Flush()
					e.last = time.Now()
				}
				e.mu.Unlock()
			}
		}
	}()
	return func() {
		close(done)
		<-stopped
	}
}

func (e *eventWriter) Text(delta string) { e.event("text", apigen.ChatTextEvent{Delta: delta}) }

func (e *eventWriter) ToolCall(call apigen.ChatToolCall) { e.event("tool_call", call) }

func (e *eventWriter) UI(path string) {
	e.event("ui", apigen.ChatUiEvent{Action: apigen.ChatUiActionNavigate, Path: path})
}

func (e *eventWriter) ToolResult(id string, ok bool, summary string) {
	e.event("tool_result", apigen.ChatToolResultEvent{Id: id, Ok: ok, Summary: summary})
}

func (e *eventWriter) Confirm(call apigen.ChatToolCall, description string) {
	e.event("confirm", apigen.ChatConfirmEvent{Id: call.Id, Name: call.Name, Arguments: call.Arguments, Description: description})
}
