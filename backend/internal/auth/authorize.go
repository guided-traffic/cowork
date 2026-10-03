package auth

import (
	"fmt"

	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/problem"
)

// The hard-off rules of docs/adr/0043 D3 that phase-2 routes meet: acts no
// agent token can be given. The name is what a refusal says.
const (
	HardOffAdministration = "administration"
	HardOffBookingTime    = "booking time"
	HardOffPrereqOverride = "overriding the prerequisite refusal"
	HardOffConfidential   = "setting or lifting the confidential flag"
	HardOffTokens         = "token administration"
)

// Need is what an act requires: a tenant role, a token scope, and for an
// agent's request a capability, unless the act is on the hard-off list.
type Need struct {
	// Role is the person's minimum role in the tenant; empty when the act is
	// not a tenant's.
	Role domain.Role
	// Scope is the token's minimum scope (docs/adr/0035 D3).
	Scope domain.Scope
	// Capability is what an agent's request must hold (docs/adr/0043 D4).
	Capability string
	// HardOff names the rule that keeps every agent from the act
	// (docs/adr/0043 D3).
	HardOff string
}

// Authorize decides an act as the intersection of the token's scope, the
// person's role and the agent rules (docs/adr/0035 D3, docs/adr/0034 D6). It
// returns nil when the act is allowed. role is the person's effective role in
// the tenant the act belongs to.
func Authorize(p Principal, role domain.Role, need Need) *problem.Error {
	if need.Role != "" && !role.AtLeast(need.Role) {
		return problem.New(problem.Forbidden, fmt.Sprintf("the act needs the %s role", need.Role))
	}
	if need.Scope != "" && !p.Scope.AtLeast(need.Scope) {
		return problem.New(problem.InsufficientScope, fmt.Sprintf("the act needs the %s scope; the token has %s", need.Scope, p.Scope))
	}
	if !p.IsAgent() {
		return nil
	}
	if need.HardOff != "" {
		return problem.New(problem.AgentForbidden, "hard-off: "+need.HardOff)
	}
	if need.Capability != "" && !p.Can(need.Capability) {
		return problem.New(problem.AgentForbidden, "missing capability: "+need.Capability)
	}
	return nil
}
