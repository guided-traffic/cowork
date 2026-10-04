package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/chat"
	"github.com/guided-traffic/cowork/backend/internal/llm"
	"github.com/guided-traffic/cowork/backend/internal/problem"
	"github.com/guided-traffic/cowork/backend/internal/requestid"
	"github.com/guided-traffic/cowork/backend/internal/tools"
)

// opRunChatTurn is a turn of the chat, served outside the generated server
// like the event stream: a stream is not a response a handler returns.
const opRunChatTurn = "runChatTurn"

// defaultKeepAlive is how long a turn stays silent before a comment keeps the
// proxies in front from closing it while the model thinks.
const defaultKeepAlive = 10 * time.Second

// stopWait is how long DELETE …/chat/turns waits for the turns it stopped to
// end, so that a turn sent right after the stop finds their places free.
const stopWait = 5 * time.Second

// ChatProvider is one model the chat talks to: what the availability shows of
// it, and the gateway (docs/adr/0076).
type ChatProvider struct {
	// ID names it in a turn, Name for the person; Kind is its wire format and
	// Model the model's name. Its address and key stay in the gateway.
	ID, Name, Kind, Model string
	Provider              llm.Provider
}

// ChatOptions configures the chat in the UI (docs/adr/0076).
type ChatOptions struct {
	// Providers are the models the person picks from, in the configured
	// order: the first is a turn's default.
	Providers []ChatProvider
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
// conversation, and with which providers (docs/adr/0076).
func (s *Server) GetChatAvailability(ctx context.Context, _ apigen.GetChatAvailabilityRequestObject) (apigen.GetChatAvailabilityResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, read); perr != nil {
		return nil, perr
	}
	return apigen.GetChatAvailability200JSONResponse(s.h.chatAvailability()), nil
}

// chatAvailability is the chat's availability: every tenant has it once a
// provider is configured. A provider's address and key are never part of it.
func (h *handler) chatAvailability() apigen.ChatAvailability {
	a := apigen.ChatAvailability{Providers: []apigen.ChatProvider{}, Reason: nullableOf[apigen.ChatUnavailableReason](nil)}
	if c := h.opts.Chat; c != nil {
		for _, p := range c.Providers {
			a.Providers = append(a.Providers, apigen.ChatProvider{Id: p.ID, Name: p.Name, Kind: apigen.ChatProviderKind(p.Kind), Model: p.Model})
		}
	}
	a.Available = len(a.Providers) > 0
	if !a.Available {
		reason := apigen.ChatUnavailableReasonNotConfigured
		a.Reason = nullableOf(&reason)
	}
	return a
}

// serveChat runs a turn of the chat (docs/adr/0076). The pipeline has
// authenticated the person by their session, held the request to the CSRF
// check, refused it to an agent, admitted the person to the tenant, and read
// and validated the body within the request timeout. The turn is bounded by
// its own, can be stopped by DELETE …/chat/turns, and answers its events as
// they come, each flushed.
func (h *handler) serveChat(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	t := tenantFrom(ctx)
	p := principal(ctx)
	if perr := auth.Authorize(p, t.Role, read); perr != nil {
		problem.Write(w, r, perr)
		return
	}
	if !h.chatAvailability().Available {
		problem.Write(w, r, problem.New(problem.ChatUnavailable, "this installation configures no chat provider"))
		return
	}
	var body apigen.ChatTurn
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		problem.Write(w, r, problem.New(problem.ValidationFailed, "the request body is not valid JSON for this route"))
		return
	}
	provider, perr := h.chatProvider(body.Provider)
	if perr != nil {
		problem.Write(w, r, perr)
		return
	}
	if perr := chat.Check(body.Messages); perr != nil {
		problem.Write(w, r, perr)
		return
	}
	capabilities, err := h.chatCapabilities(ctx, p.PersonID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	turn, perr := h.chatTurn(r, t, body, provider, capabilities)
	if perr != nil {
		problem.Write(w, r, perr)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		problem.Write(w, r, problem.New(problem.Internal, "this server cannot stream a turn"))
		return
	}
	turnCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	done, perr := h.startTurn(p.PersonID, t.ID, cancel)
	if perr != nil {
		problem.Write(w, r, perr)
		return
	}
	defer done()
	h.streamTurn(turnCtx, w, r, flusher, provider, turn)
}

// chatProvider is the provider a turn names, or the first configured.
func (h *handler) chatProvider(id *string) (ChatProvider, *problem.Error) {
	providers := h.opts.Chat.Providers
	if id == nil {
		return providers[0], nil
	}
	for _, p := range providers {
		if p.ID == *id {
			return p, nil
		}
	}
	return ChatProvider{}, problem.Field("/provider", "this installation configures no such provider; GET …/chat lists them")
}

// streamTurn runs a turn and answers its events: the stream is open before
// the model is asked, and ends with done whatever ends the turn — its time,
// the provider, the person's stop, the server's shutdown.
func (h *handler) streamTurn(ctx context.Context, w http.ResponseWriter, r *http.Request, flusher http.Flusher, provider ChatProvider,
	turn chat.Turn) {
	c := h.opts.Chat
	ctx, cancel := context.WithCancelCause(ctx)
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
	end := chat.Run(ctx, chat.Options{Provider: provider.Provider, MaxSteps: c.MaxSteps, Now: h.opts.Now}, turn, out)
	reason := end.End
	switch cause := context.Cause(ctx); {
	case end.Err == nil:
	case errors.Is(cause, errStopped):
		// The person stopped it: no failure to report.
		reason = apigen.ChatTurnEndStopped
	default:
		if errors.Is(cause, errShutdown) {
			end.Err = errShutdown
		}
		out.event("error", problem.BodyOf(r, h.turnProblem(r, end.Err)))
	}
	out.event("done", apigen.ChatDoneEvent{Messages: end.Messages, Reason: reason})
}

// errShutdown ends the turns the server's shutdown finds running, errStopped
// the turns their person stopped.
var (
	errShutdown = errors.New("the server is shutting down")
	errStopped  = errors.New("the person stopped the turn")
)

// runningTurn is a turn this replica runs: the tenant it runs in, the cancel
// that ends it, and done, closed once it ended.
type runningTurn struct {
	tenant uuid.UUID
	cancel context.CancelCauseFunc
	done   chan struct{}
}

// startTurn registers a person's turn while it runs: one more than
// TurnsPerPerson is refused before its stream opens. The count and the
// registry are this replica's (docs/adr/0039, docs/adr/0076).
func (h *handler) startTurn(person, tenant uuid.UUID, cancel context.CancelCauseFunc) (done func(), perr *problem.Error) {
	limit := h.opts.Chat.TurnsPerPerson
	h.turnsMu.Lock()
	defer h.turnsMu.Unlock()
	if limit > 0 && len(h.turns[person]) >= limit {
		return nil, problem.New(problem.ChatBusy, fmt.Sprintf("%d turns of yours are running already; one ends, or is stopped, first", limit))
	}
	rt := &runningTurn{tenant: tenant, cancel: cancel, done: make(chan struct{})}
	if h.turns[person] == nil {
		h.turns[person] = map[*runningTurn]struct{}{}
	}
	h.turns[person][rt] = struct{}{}
	return func() {
		h.turnsMu.Lock()
		delete(h.turns[person], rt)
		if len(h.turns[person]) == 0 {
			delete(h.turns, person)
		}
		h.turnsMu.Unlock()
		close(rt.done)
	}, nil
}

// stopTurns ends every turn a person runs in a tenant on this replica, at
// once, and returns what closes as each has ended.
func (h *handler) stopTurns(person, tenant uuid.UUID) []<-chan struct{} {
	h.turnsMu.Lock()
	defer h.turnsMu.Unlock()
	var ended []<-chan struct{}
	for rt := range h.turns[person] {
		if rt.tenant == tenant {
			rt.cancel(errStopped)
			ended = append(ended, rt.done)
		}
	}
	return ended
}

// StopChatTurns stops every running turn of the session's person in the
// tenant on this replica (docs/adr/0076): their contexts end at once, which
// cancels the call of the model and a tool call in flight; the answer waits
// until they have ended, at most stopWait. A turn of another replica is not
// reached. The document takes a session only, and the pipeline refuses a
// session the agent header marks.
func (s *Server) StopChatTurns(ctx context.Context, _ apigen.StopChatTurnsRequestObject) (apigen.StopChatTurnsResponseObject, error) {
	t := tenantFrom(ctx)
	p := principal(ctx)
	if perr := auth.Authorize(p, t.Role, read); perr != nil {
		return nil, perr
	}
	deadline := time.NewTimer(stopWait)
	defer deadline.Stop()
	for _, ended := range s.h.stopTurns(p.PersonID, t.ID) {
		select {
		case <-ended:
		case <-deadline.C:
			return apigen.StopChatTurns204Response{}, nil
		case <-ctx.Done():
			return apigen.StopChatTurns204Response{}, nil
		}
	}
	return apigen.StopChatTurns204Response{}, nil
}

// chatTurn is the turn of a request: the conversation, the page, and the API
// client of the person's agent — the session cookie of the request, the
// installation's origin, the chat's mark with the provider's model and the
// conversation's id, the capabilities the person gave the chat.
func (h *handler) chatTurn(r *http.Request, t tenantScope, body apigen.ChatTurn, provider ChatProvider, capabilities []string) (chat.Turn, *problem.Error) {
	cookie, err := r.Cookie(auth.SessionCookie)
	if err != nil {
		return chat.Turn{}, problem.New(problem.Unauthenticated, "a turn of the chat needs the session cookie")
	}
	page := chat.Page{}
	if body.Context != nil {
		page = chat.Page{Path: deref(body.Context.Path), Project: deref(body.Context.Project), Ticket: deref(body.Context.Ticket)}
	}
	loop := chat.Loopback{Handler: h, Tenant: t.Slug, RemoteAddr: r.RemoteAddr, ForwardedFor: r.Header.Values("X-Forwarded-For")}
	if l := h.opts.Chat.Loopback; l != nil && l() != nil {
		loop.Handler = l()
	}
	p := principal(r.Context())
	mark := chat.Mark(provider.Model, body.Conversation)
	session, err := chat.NewSession(loop, h.opts.BaseOrigin, chat.Editor(cookie.Value, h.opts.BaseOrigin, mark), mark, page.Project,
		tools.Person{ID: p.PersonID, Name: p.DisplayName}, capabilities)
	if err != nil {
		h.logger.Error("the chat's session failed", "request_id", requestid.From(r.Context()), "error", err)
		return chat.Turn{}, problem.New(problem.Internal, "internal error")
	}
	return chat.Turn{Tenant: t.Slug, TenantName: t.Name, Conversation: body.Conversation, Page: page, Messages: body.Messages,
		Session: session}, nil
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
