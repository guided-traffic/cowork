package auth

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/guided-traffic/cowork/backend/internal/domain"
)

// AgentHeader is the header an agent names itself with (docs/adr/0036 D3).
const AgentHeader = "X-Cowork-Agent"

// UnknownAgent is the record of an agent token that sent no header
// (docs/adr/0036 D4).
const UnknownAgent = "unknown-agent"

// The agent capabilities (docs/adr/0043 D4).
const (
	CapDecide        = "decide"
	CapClose         = "close"
	CapDrop          = "drop"
	CapRank          = "rank"
	CapSetHorizon    = "set-horizon"
	CapInterest      = "interest"
	CapUpload        = "upload"
	CapCreateProject = "create-project"
	CapRecordAnswer  = "record-answer"
)

// AllCapabilities is the "full" set, every selectable capability on: the
// default of an agent token, and the set of a request a plain token marks as
// an agent's with the header (docs/adr/0043 D4).
var AllCapabilities = []string{CapDecide, CapClose, CapDrop, CapRank, CapSetHorizon,
	CapInterest, CapUpload, CapCreateProject, CapRecordAnswer}

// DefaultChatCapabilities is what the chat in the UI holds for a person who
// never chose (docs/adr/0043 D5): every capability but decide, close and drop,
// which the owner keeps a person's, and record-answer, because without a
// confirmation an injected text could record an answer in the person's name.
var DefaultChatCapabilities = []string{CapRank, CapSetHorizon, CapInterest, CapUpload, CapCreateProject}

// Canonical is a capability set as this release reads it: in the order
// given, each name once (docs/adr/0043 D4).
func Canonical(capabilities []string) []string {
	out := make([]string, 0, len(capabilities))
	for _, c := range capabilities {
		if !slices.Contains(out, c) {
			out = append(out, c)
		}
	}
	return out
}

// Principal is who a request acts for, after authentication: a person through
// a personal access token or through a browser session, resolved by one
// authentication step (docs/adr/0031 D6).
type Principal struct {
	PersonID    uuid.UUID
	DisplayName string
	// TokenID is the token the request presented; uuid.Nil for a session.
	// TokenName is its name, which every act the request makes records
	// beside it (docs/adr/0036 D6); empty for a session.
	TokenID   uuid.UUID
	TokenName string
	// Session says the request was authenticated by the session cookie, and
	// SessionHash is the SHA-256 of the cookie value it presented; both are
	// zero for a token. The hash is what finds the session's row again and
	// is recorded nowhere (docs/adr/0031 D7).
	Session     bool
	SessionHash []byte
	// SessionMethod is how the session's person logged in, local or oidc
	// (docs/adr/0031 D1); empty for a token.
	SessionMethod string
	// Provider says the person is one of the identity provider, whom the gate
	// holds (docs/adr/0030 D1, docs/adr/0035 D8).
	Provider bool
	// Scope is the token's scope. A session has no scope of its own: it
	// acts with the person's full role, which is what the scope admin
	// leaves to the role (docs/adr/0035 D3).
	Scope domain.Scope
	// GlobalAdmin is the person's global administrator flag (docs/adr/0004
	// D4); PasswordChangeRequired says a temporary password is still to be
	// changed (docs/adr/0033 D4).
	GlobalAdmin            bool
	PasswordChangeRequired bool
	// RestrictedTenantID and RestrictedProjectID are the token's restriction
	// (docs/adr/0035 D3); uuid.Nil when there is none.
	RestrictedTenantID  uuid.UUID
	RestrictedProjectID uuid.UUID
	// Agent is the agent mark: the X-Cowork-Agent value, UnknownAgent, or
	// empty for a person's own request.
	Agent string
	// Capabilities is the agent's capability set; empty for a person.
	Capabilities []string
}

// IsAgent reports whether the request is an agent's, by the token's flag or
// by the header (docs/adr/0036 D2, D3).
func (p Principal) IsAgent() bool { return p.Agent != "" }

// Can reports whether the agent holds a capability.
func (p Principal) Can(capability string) bool {
	return slices.Contains(p.Capabilities, capability)
}

// ParseAgentHeader validates an X-Cowork-Agent value: three parts separated by
// "/", each one to sixty-four printable ASCII characters that are not "/"
// and neither start nor end with a space (docs/adr/0036 D3). A malformed
// header is refused, never ignored.
func ParseAgentHeader(v string) (string, error) {
	parts := strings.Split(v, "/")
	if len(parts) != 3 {
		return "", errors.New("must be name/model/session, three parts separated by /")
	}
	for i, part := range parts {
		if err := checkAgentPart(part); err != nil {
			return "", fmt.Errorf("part %d: %w", i+1, err)
		}
	}
	return v, nil
}

func checkAgentPart(part string) error {
	if part == "" || len(part) > 64 {
		return errors.New("must be 1 to 64 characters")
	}
	if part[0] == ' ' || part[len(part)-1] == ' ' {
		return errors.New("must not start or end with a space")
	}
	for i := range len(part) {
		if part[i] < 0x20 || part[i] > 0x7e {
			return errors.New("must be printable ASCII")
		}
	}
	return nil
}

// Mark decides the agent mark and the capability set of a token's request: a
// flagged token is an agent's whatever the header says, recorded by the header
// or as unknown-agent; a plain token is an agent's only when the header says
// so, and then holds every capability (docs/adr/0036 D2–D4, docs/adr/0043 D4).
// header is the validated header value or empty. A session the header marks
// holds the person's chat capabilities instead (docs/adr/0043 D5), which the
// session's resolver reads. The token's set is read each name once
// (Canonical).
func Mark(flagged bool, tokenCapabilities []string, header string) (agent string, capabilities []string) {
	switch {
	case flagged && header != "":
		return header, Canonical(tokenCapabilities)
	case flagged:
		return UnknownAgent, Canonical(tokenCapabilities)
	case header != "":
		return header, slices.Clone(AllCapabilities)
	default:
		return "", nil
	}
}

type principalKey struct{}

// WithPrincipal returns a context that carries p.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// PrincipalFrom returns the principal the context carries.
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}
