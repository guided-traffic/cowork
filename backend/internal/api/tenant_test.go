package api

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/problem"
)

// docs/adr/0034 D2: a global administrator reaches a tenant in which they hold
// no role in a browser session that no agent marks, and only its
// administration and the grant to themselves; a token, an agent and anybody
// who is not a global administrator do not.
func TestAGlobalAdministratorOverseesTheAdministrationOnly(t *testing.T) {
	global := auth.Principal{GlobalAdmin: true, Session: true, Scope: domain.ScopeAdmin}
	for _, op := range []string{"getTeam", "listMembers", "listGroupMappings", "setMemberGrant"} {
		assert.True(t, oversees(global, op), op)
	}
	for _, op := range []string{"listProjects", "listTeamTickets", "getTicket", "listTeamTime", "uploadAttachment",
		opStreamEvents, "runChatTurn", "listAudit", "updateTeam", "addMember", "removeMemberGrant", "listAccounts",
		"createGroupMapping", "deleteGroupMapping", "resolveTicket"} {
		assert.False(t, oversees(global, op), "%s is the team's work or another act", op)
	}
	// A twin's operation is never routed (TestNoTwinIsEverRouted); were one, it
	// would oversee nothing.
	for _, op := range []string{"getTenant", "listMembersDeprecated"} {
		assert.False(t, oversees(global, op), op)
	}
	token := auth.Principal{GlobalAdmin: true, Scope: domain.ScopeAdmin}
	assert.False(t, oversees(token, "listMembers"), "a token keeps the reach of its person's memberships")
	agent := global
	agent.Agent = "chat/model/conversation"
	assert.False(t, oversees(agent, "listMembers"), "an agent-marked session does not oversee")
	person := auth.Principal{Session: true, Scope: domain.ScopeAdmin}
	assert.False(t, oversees(person, "listMembers"), "a person who is not a global administrator does not")
}

// The reads of the administration admit an overseeing global administrator by
// the boundary's admission, and everybody else by their role; every other need
// refuses a scope without a role.
func TestAdministrationReadsAdmitTheOverseer(t *testing.T) {
	global := auth.Principal{GlobalAdmin: true, Session: true, Scope: domain.ScopeAdmin}
	overseen := tenantScope{Oversight: true}
	assert.Nil(t, administrationRead(global, overseen, adminRead))
	assert.Nil(t, administrationRead(global, overseen, read))
	perr := auth.Authorize(global, overseen.Role, read)
	if assert.NotNil(t, perr, "a scope without a role passes no need of its own") {
		assert.Equal(t, problem.Forbidden, perr.Code)
	}
	viewer := auth.Principal{Session: true, Scope: domain.ScopeAdmin}
	assert.Nil(t, administrationRead(viewer, tenantScope{Role: domain.RoleViewer}, read))
	assert.NotNil(t, administrationRead(viewer, tenantScope{Role: domain.RoleViewer}, adminRead))
}

// docs/adr/0034 D2: a global administrator sets their own grant in a tenant in
// which they do not hold admin — without a role there, or with a lower one —
// in a session no agent marks; nobody else's grant, nobody who is not a global
// administrator, and not where they administer already.
func TestAGlobalAdministratorSetsTheirOwnGrantBelowAdmin(t *testing.T) {
	me := uuid.Must(uuid.NewV7())
	global := auth.Principal{PersonID: me, GlobalAdmin: true, Session: true, Scope: domain.ScopeAdmin}
	assert.True(t, ownGrant(global, tenantScope{Oversight: true}, me), "without a role")
	assert.True(t, ownGrant(global, tenantScope{Role: domain.RoleViewer}, me), "a viewer")
	assert.True(t, ownGrant(global, tenantScope{Role: domain.RoleMember}, me), "a member")
	assert.False(t, ownGrant(global, tenantScope{Role: domain.RoleAdmin}, me), "an administrator acts as one")
	assert.False(t, ownGrant(global, tenantScope{Role: domain.RoleViewer}, uuid.Must(uuid.NewV7())), "nobody else's grant")
	person := global
	person.GlobalAdmin = false
	assert.False(t, ownGrant(person, tenantScope{Role: domain.RoleViewer}, me), "a viewer who is no global administrator")
	token := global
	token.Session = false
	assert.False(t, ownGrant(token, tenantScope{Role: domain.RoleViewer}, me), "a token")
	agent := global
	agent.Agent = "chat/model/conversation"
	assert.False(t, ownGrant(agent, tenantScope{Role: domain.RoleViewer}, me), "an agent")
}
