package api

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/problem"
)

// docs/adr/0043 D3, docs/adr/0065 D9: an agent assigns a confidential ticket
// only to its own person; an assignee left as it was admits nobody new, a
// ticket that is not confidential admits nobody, and a person assigns anyone.
func TestAnAgentAssignsAConfidentialTicketOnlyToItsPerson(t *testing.T) {
	me, other, former := uuid.New(), uuid.New(), uuid.New()
	mine := agent
	mine.PersonID = me
	self := person
	self.PersonID = me

	perr := mayAssign(mine, true, &former, other)
	require.NotNil(t, perr, "another person admitted by an agent")
	assert.Equal(t, problem.AgentForbidden, perr.Code)
	assert.Equal(t, "hard-off: assigning a confidential ticket to anyone but the agent's person", perr.Detail)
	assert.NotNil(t, mayAssign(mine, true, nil, other), "on a filing as well")
	assert.NotNil(t, mayAssign(mine, true, &me, other), "away from its own person")

	assert.Nil(t, mayAssign(mine, true, &other, me), "to its own person")
	assert.Nil(t, mayAssign(mine, true, nil, me), "a filing assigned to its own person")
	assert.Nil(t, mayAssign(mine, true, &other, other), "the assignee as it was")
	assert.Nil(t, mayAssign(mine, false, &former, other), "a ticket that is not confidential")
	assert.Nil(t, mayAssign(self, true, &former, other), "a person's assignment")
}
