package tools

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/auth"
)

// Session is what the tools run against: the API client, and the memory and
// the working directory of the host where it has them. A host holds one per
// conversation with a model; it keeps the binding the session found and the
// token it read, and nothing else.
type Session struct {
	// API is the generated client of the API document (docs/adr/0040 D2),
	// over whatever HTTP doer the host chooses; it carries the credential
	// and the agent mark on every request.
	API *apigen.ClientWithResponses
	// Installation is the installation's URL, which names the memory and
	// the links a tool shows a person.
	Installation string
	// Memory keeps when session_start last ran for a binding; nil keeps
	// nothing, and "since the last session" is then left out.
	Memory Memory
	// Workspace is the repository the host runs in; nil where there is
	// none, and the terminal's tools are then not offered.
	Workspace Workspace
	// Now is the clock; NewKey makes the Idempotency-Key of each creating
	// POST, one per act, the same on every retry of it (docs/adr/0045 D5).
	Now    func() time.Time
	NewKey func() uuid.UUID

	mu      sync.Mutex
	binding *Binding
	token   *Token
}

// NewSession returns a session with the system clock and UUIDv7 keys.
func NewSession(api *apigen.ClientWithResponses, installation string) *Session {
	return &Session{API: api, Installation: strings.TrimRight(installation, "/"), Now: time.Now,
		NewKey: func() uuid.UUID { return uuid.Must(uuid.NewV7()) }}
}

// Binding is the tenant and the project a session works in, and how it was
// found (docs/adr/0066).
type Binding struct {
	Tenant, Project, ProjectName string
	// Source is "remote" when the server's binding of a remote decided, and
	// "file" when .cowork.yaml did (docs/adr/0066 D4), "host" when the host
	// set it.
	Source string
	// Remote and Identity name the repository that bound it; Path is the
	// sub-directory of a monorepo.
	Remote, Identity, Path string
	// Drift is a disagreement between the file and the server, reported and
	// not resolved (docs/adr/0006 D3).
	Drift string
}

// How a binding was found.
const (
	sourceRemote = "remote"
	sourceFile   = "file"
	sourceHost   = "host"
)

// Key is the binding as tenant/KEY.
func (b Binding) Key() string { return b.Tenant + "/" + b.Project }

// Bind sets the binding a host already knows — a page that shows a project,
// say — so short keys resolve without session_start.
func (s *Session) Bind(tenant, project string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.binding = &Binding{Tenant: tenant, Project: project, Source: sourceHost}
}

func (s *Session) setBinding(b *Binding) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.binding = b
}

// Binding returns the binding the session found, or nil.
func (s *Session) Binding() *Binding {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.binding == nil {
		return nil
	}
	b := *s.binding
	return &b
}

// Token is the token of the session and what the API makes of its requests
// (GET /api/v1/me/token, docs/adr/0043 D6).
type Token struct {
	// Known is false until the token was read.
	Known bool
	Name  string
	Scope string
	// Agent says the requests are an agent's; Mark is the agent they record.
	// Flagged is the token's own agent flag (docs/adr/0036 D2).
	Agent   bool
	Mark    string
	Flagged bool
	// Capabilities are what an agent's requests may do beyond the baseline.
	Capabilities []string
	// Tenant and Project are the token's restriction, slug and key.
	Tenant, Project string
	ExpiresAt       time.Time
}

// Can reports whether the token's requests may do what a capability grants;
// a person's request is not bounded by the capabilities, and an unknown
// token is assumed to have them, the API deciding.
func (t Token) Can(capability string) bool {
	return !t.Known || !t.Agent || slices.Contains(t.Capabilities, capability)
}

// ReadToken reads the token of the session (docs/adr/0043 D6) and keeps it.
func (s *Session) ReadToken(ctx context.Context) (Token, error) {
	res, err := s.API.GetMyTokenWithResponse(ctx)
	if err := check(res, err, http.StatusOK); err != nil {
		return Token{}, err
	}
	ct := res.JSON200
	tok := Token{Known: true, Name: ct.Name, Scope: string(ct.Scope), Agent: ct.Request.Agent, Flagged: ct.Agent, ExpiresAt: ct.ExpiresAt}
	tok.Mark, _ = ct.Request.AgentMark.Get()
	tok.Tenant, _ = ct.RestrictedTenant.Get()
	tok.Project, _ = ct.RestrictedProject.Get()
	for _, c := range ct.Request.Capabilities {
		tok.Capabilities = append(tok.Capabilities, string(c))
	}
	s.mu.Lock()
	s.token = &tok
	s.mu.Unlock()
	return tok, nil
}

// Token returns the token read, or one that is not Known.
func (s *Session) Token() Token {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token == nil {
		return Token{}
	}
	return *s.token
}

// TokenPage is the installation's page where a person makes and revokes
// tokens, named by every message about a token (docs/adr/0041 D5).
func (s *Session) TokenPage() string { return s.Installation + "/me/tokens" }

// TicketPage is a ticket's page in the installation's UI.
func (s *Session) TicketPage(tenant, shortKey string) string {
	return s.Installation + "/t/" + tenant + "/tickets/" + shortKey
}

// key returns a new Idempotency-Key.
func (s *Session) key() *uuid.UUID {
	k := s.NewKey()
	return &k
}

// AgentHeader is the X-Cowork-Agent value of a client: name/model/session,
// each part sanitised to what the API accepts (docs/adr/0036 D3).
func AgentHeader(name, model, session string) string {
	return agentPart(name, "cowork-mcp") + "/" + agentPart(model, "unknown") + "/" + agentPart(session, "session")
}

func agentPart(s, fallback string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(s) {
		if r > 0x20 && r < 0x7f && r != '/' {
			b.WriteRune(r)
		}
		if b.Len() == 64 {
			break
		}
	}
	if b.Len() == 0 {
		return fallback
	}
	return b.String()
}

// Editor returns the request editor of a client: the bearer token and the
// agent header on every request (docs/adr/0036 D3), the header read each time
// so a host may learn its client's name late.
func Editor(token string, agent func() string, userAgent string) apigen.RequestEditorFn {
	return func(_ context.Context, req *http.Request) error {
		req.Header.Set("Authorization", "Bearer "+token)
		if a := agent(); a != "" {
			if _, err := auth.ParseAgentHeader(a); err != nil {
				return fmt.Errorf("the agent header %q: %w", a, err)
			}
			req.Header.Set(auth.AgentHeader, a)
		}
		if userAgent != "" {
			req.Header.Set("User-Agent", userAgent)
		}
		return nil
	}
}
