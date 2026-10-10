package chat

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path"
	"slices"
	"strings"

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
// the events (docs/adr/0076). It sends nothing outside the turn's team,
// nothing to the chat's own routes or to the event stream, and nothing that is
// no clean path.
type Loopback struct {
	Handler http.Handler
	// Tenant is the slug of the turn's team, under the name the database
	// keeps (docs/adr/0005 D1).
	Tenant string
	// RemoteAddr and ForwardedFor are the person's request's, so that the acts
	// of a tool call record the person's address (docs/adr/0035 D2).
	RemoteAddr   string
	ForwardedFor []string
}

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
	return tools.HandlerDoer{Handler: l.Handler}.Do(inner)
}

// families are the two path families of a team: the team's, which the tools'
// generated client calls, and the tenant's, its deprecated twin, which the
// server answers as the team's for one release (docs/adr/0005 D1,
// docs/adr/0023 D1). The loopback holds a call to either to the same rules:
// the server rewrites a twin to the team path before it routes it, so a
// twin's chat or event stream is the team's.
var families = []string{"/api/v1/teams/", "/api/v1/tenants/"}

func (l Loopback) allowed(p string) error {
	if path.Clean(p) != p {
		return fmt.Errorf("the path %q is not clean", p)
	}
	for _, family := range families {
		team := family + l.Tenant
		switch {
		case p == team+"/chat", strings.HasPrefix(p, team+"/chat/"):
			return errItself
		case p == team+"/events":
			return errors.New("the chat reads no event stream")
		case p == team, strings.HasPrefix(p, team+"/"):
			return nil
		}
	}
	if strings.HasPrefix(p, "/api/v1/tickets/"+l.Tenant+"/") {
		return nil
	}
	return fmt.Errorf("the chat works in the team %s and calls nothing outside it", l.Tenant)
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
// chat's agent mark, which narrows the session to an agent's holding the
// person's chat capabilities (docs/adr/0036 D3, docs/adr/0037 D1,
// docs/adr/0043 D5). An Idempotency-Key is the tools' own.
func Editor(cookie, origin, mark string) apigen.RequestEditorFn {
	return func(_ context.Context, req *http.Request) error {
		// #nosec G124 -- a request's cookie: Secure, HttpOnly and SameSite are the attributes of a cookie a response sets
		req.AddCookie(&http.Cookie{Name: auth.SessionCookie, Value: cookie})
		req.Header.Set("Origin", origin)
		req.Header.Set("X-Requested-With", "cowork")
		req.Header.Set(auth.AgentHeader, mark)
		req.Header.Set("User-Agent", "cowork-chat")
		return nil
	}
}

// Mark is the chat's agent mark, chat/<model>/<conversation>: a model's
// slashes become colons — the mark's parts are separated by slashes — and
// each part is cut to what the header takes (docs/adr/0036 D3).
func Mark(model string, conversation uuid.UUID) string {
	return tools.AgentHeader("chat", strings.ReplaceAll(model, "/", ":"), conversation.String())
}

// NewSession is the API client of a turn: the person's agent through the
// loopback, bound to the page's project or else to the team, confined to the
// team, knowing its person, and holding capabilities — the person's
// chat capabilities, which the API reads again on every call, so these only
// tell the model what it may do (docs/adr/0043 D5, D6). installation is the
// installation's URL, which the tools' links name.
func NewSession(l Loopback, installation string, editor apigen.RequestEditorFn, mark, project string, person tools.Person,
	capabilities []string) (*tools.Session, error) {
	api, err := apigen.NewClientWithResponses("http://cowork.loopback", apigen.WithHTTPClient(l), apigen.WithRequestEditorFn(editor))
	if err != nil {
		return nil, fmt.Errorf("make the chat's API client: %w", err)
	}
	s := tools.NewSession(api, installation)
	if project != "" {
		s.Bind(l.Tenant, project)
	} else {
		s.BindTeam(l.Tenant)
	}
	s.Person, s.Teams = &person, []string{l.Tenant}
	s.Assume(tools.Token{Agent: true, Mark: mark, Capabilities: slices.Clone(capabilities)})
	return s, nil
}
