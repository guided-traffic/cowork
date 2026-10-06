package api

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/problem"
)

// docs/adr/0018 D5 as amended 2026-10-06: the owner changes their filter; a
// tenant administrator, with admin scope and never as an agent, unshares
// another person's shared filter — a patch of shared false and nothing else —
// or deletes it.
func TestWhoChangesASavedFilter(t *testing.T) {
	me, other := uuid.New(), uuid.New()
	session := auth.Principal{PersonID: me, Scope: domain.ScopeAdmin}
	writeToken := auth.Principal{PersonID: me, Scope: domain.ScopeWrite}
	markedSession := auth.Principal{PersonID: me, Scope: domain.ScopeAdmin, Agent: "chat/model/c1", Capabilities: auth.AllCapabilities}
	unshare := &apigen.SavedFilterPatch{Shared: ptrBool(false)}

	cases := map[string]struct {
		p       auth.Principal
		role    domain.Role
		owner   uuid.UUID
		patch   *apigen.SavedFilterPatch
		another bool
		code    string
	}{
		"the owner renames":                       {p: writeToken, role: domain.RoleViewer, owner: me, patch: &apigen.SavedFilterPatch{Name: ptrStr("x")}},
		"the owner deletes":                       {p: writeToken, role: domain.RoleViewer, owner: me},
		"an administrator unshares":               {p: session, role: domain.RoleAdmin, owner: other, patch: unshare, another: true},
		"an administrator deletes":                {p: session, role: domain.RoleAdmin, owner: other, another: true},
		"a member unshares":                       {p: session, role: domain.RoleMember, owner: other, patch: unshare, code: problem.Forbidden.Code},
		"a member deletes":                        {p: session, role: domain.RoleMember, owner: other, code: problem.Forbidden.Code},
		"an administrator renames":                {p: session, role: domain.RoleAdmin, owner: other, patch: &apigen.SavedFilterPatch{Name: ptrStr("x")}, code: problem.Forbidden.Code},
		"an administrator unshares and renames":   {p: session, role: domain.RoleAdmin, owner: other, patch: &apigen.SavedFilterPatch{Name: ptrStr("x"), Shared: ptrBool(false)}, code: problem.Forbidden.Code},
		"an administrator changes the conditions": {p: session, role: domain.RoleAdmin, owner: other, patch: &apigen.SavedFilterPatch{Parameters: &apigen.SavedFilterParameters{}, Shared: ptrBool(false)}, code: problem.Forbidden.Code},
		"an administrator shares":                 {p: session, role: domain.RoleAdmin, owner: other, patch: &apigen.SavedFilterPatch{Shared: ptrBool(true)}, code: problem.Forbidden.Code},
		"an administrator sends nothing":          {p: session, role: domain.RoleAdmin, owner: other, patch: &apigen.SavedFilterPatch{}, code: problem.Forbidden.Code},
		"an administrator's write token":          {p: writeToken, role: domain.RoleAdmin, owner: other, patch: unshare, code: problem.InsufficientScope.Code},
		"an administrator's agent":                {p: markedSession, role: domain.RoleAdmin, owner: other, code: problem.AgentForbidden.Code},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			another, perr := mayChangeFilter(c.p, c.role, c.owner, c.patch)
			if c.code == "" {
				assert.Nil(t, perr)
				assert.Equal(t, c.another, another)
				return
			}
			if assert.NotNil(t, perr) {
				assert.Equal(t, c.code, perr.Code.Code)
			}
			assert.False(t, another)
		})
	}
}
