package chat

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/google/uuid"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/requestid"
	"github.com/guided-traffic/cowork/backend/internal/tools"
)

// errItself is a tool call to the chat: a turn never starts another.
var errItself = errors.New("the chat does not call itself")

// Loopback sends a turn's tool calls to the server in the same process, each
// as a request of its own that the whole pipeline runs — validation,
// authentication, the CSRF check, authorization, the agent rules, the audit,
// the events (docs/adr/0076). It sends nothing outside the turn's tenant —
// the tenant's consent to the provider covers that tenant alone — nothing to
// the chat or to the event stream, and nothing that is no clean path.
type Loopback struct {
	Handler http.Handler
	Tenant  string
	// RemoteAddr and ForwardedFor are the person's request's, so that the acts
	// of a tool call record the person's address (docs/adr/0035 D2).
	RemoteAddr   string
	ForwardedFor []string
	// Confidential is set when an answer holds a confidential ticket
	// (docs/adr/0065): the turn's writes wait for the person from then on.
	Confidential *atomic.Bool
}

// confidential finds a confidential ticket in an answer of the API: every
// ticket the API answers carries the flag.
var confidential = regexp.MustCompile(`"confidential"\s*:\s*true`)

// Do serves a request of a tool on a context that ends with the turn and
// carries nothing of the turn's request but its deadline: the pipeline
// authenticates, admits and records the call anew, under a request id of its
// own.
func (l Loopback) Do(req *http.Request) (*http.Response, error) {
	if err := l.allowed(req.URL.Path); err != nil {
		return nil, err
	}
	ctx, cancel := detached(req.Context())
	defer cancel()
	inner := req.Clone(ctx)
	inner.RemoteAddr = l.RemoteAddr
	inner.Header.Del("X-Forwarded-For")
	for _, v := range l.ForwardedFor {
		inner.Header.Add("X-Forwarded-For", v)
	}
	res, err := tools.HandlerDoer{Handler: l.Handler}.Do(inner)
	if err != nil || l.Confidential == nil {
		return res, err
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if confidential.Match(body) {
		l.Confidential.Store(true)
	}
	res.Body = io.NopCloser(bytes.NewReader(body))
	return res, nil
}

func (l Loopback) allowed(p string) error {
	tenant := "/api/v1/tenants/" + l.Tenant
	switch {
	case path.Clean(p) != p:
		return fmt.Errorf("the path %q is not clean", p)
	case p == tenant+"/chat":
		return errItself
	case p == tenant+"/events":
		return errors.New("the chat reads no event stream")
	case p == tenant, strings.HasPrefix(p, tenant+"/"), strings.HasPrefix(p, "/api/v1/tickets/"+l.Tenant+"/"):
		return nil
	}
	return fmt.Errorf("the chat works in the tenant %s and calls nothing outside it", l.Tenant)
}

// detached is a context that ends when parent ends — its deadline copied, so
// that a turn's time running out reads as a timeout — and carries none of its
// values, with a request id of its own.
func detached(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	if deadline, ok := parent.Deadline(); ok {
		var cancelDeadline context.CancelFunc
		ctx, cancelDeadline = context.WithDeadline(ctx, deadline)
		inner := cancel
		cancel = func() { cancelDeadline(); inner() }
	}
	stop := context.AfterFunc(parent, cancel)
	return requestid.With(ctx, requestid.New()), func() {
		stop()
		cancel()
	}
}

// Editor marks every request of a turn as the person's agent's: the person's
// session cookie, the Origin and the custom header of the CSRF check, and the
// chat's agent mark, which narrows the session to an agent's
// (docs/adr/0036 D3, docs/adr/0037 D1). A call the person decided carries the
// mark with "+confirmed" after the conversation, so its acts record the
// person's Run. An Idempotency-Key is the tools' own.
func Editor(cookie, origin, mark string) apigen.RequestEditorFn {
	return func(ctx context.Context, req *http.Request) error {
		// #nosec G124 -- a request's cookie: Secure, HttpOnly and SameSite are the attributes of a cookie a response sets
		req.AddCookie(&http.Cookie{Name: auth.SessionCookie, Value: cookie})
		req.Header.Set("Origin", origin)
		req.Header.Set("X-Requested-With", "cowork")
		if ctx.Value(decidedKey{}) == true {
			req.Header.Set(auth.AgentHeader, mark+confirmedMark)
		} else {
			req.Header.Set(auth.AgentHeader, mark)
		}
		req.Header.Set("User-Agent", "cowork-chat")
		return nil
	}
}

// confirmedMark ends the agent mark of a call the person decided.
const confirmedMark = "+confirmed"

// Mark is the chat's agent mark, chat/<model>/<conversation>: a model's
// slashes become colons — the mark's parts are separated by slashes — and
// each part is cut to what the header takes (docs/adr/0036 D3).
func Mark(model string, conversation uuid.UUID) string {
	return tools.AgentHeader("chat", strings.ReplaceAll(model, "/", ":"), conversation.String())
}

// NewSession is the API client of a turn: the person's agent through the
// loopback, bound to the page's project or else to the tenant, confined to
// the tenant, knowing its person, with every capability the mark gives a
// session (docs/adr/0043 D4). installation is the installation's URL, which
// the tools' links name.
func NewSession(l Loopback, installation string, editor apigen.RequestEditorFn, mark, project string, person tools.Person) (*tools.Session, error) {
	api, err := apigen.NewClientWithResponses("http://cowork.loopback", apigen.WithHTTPClient(l), apigen.WithRequestEditorFn(editor))
	if err != nil {
		return nil, fmt.Errorf("make the chat's API client: %w", err)
	}
	s := tools.NewSession(api, installation)
	if project != "" {
		s.Bind(l.Tenant, project)
	} else {
		s.BindTenant(l.Tenant)
	}
	s.Person, s.Tenants = &person, []string{l.Tenant}
	s.Assume(tools.Token{Agent: true, Mark: mark, Capabilities: slices.Clone(auth.AllCapabilities)})
	return s, nil
}
