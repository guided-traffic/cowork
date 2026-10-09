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

	session := self
	session.Session = true
	assert.Nil(t, mayAssign(session, true, &former, other), "a person's assignment in a session")
	assert.Nil(t, mayAssign(session, true, nil, other), "a person's filing in a session")
}

// docs/adr/0035 D5 as amended 2026-10-07: admitting another person to a
// confidential ticket gives access that would outlive a leaked token's
// revocation, so a person's token assigns one only to its own person or keeps
// the assignee; anything else takes a browser session.
func TestATokenAssignsAConfidentialTicketOnlyToItsPerson(t *testing.T) {
	me, other, former := uuid.New(), uuid.New(), uuid.New()
	token := person
	token.PersonID = me

	perr := mayAssign(token, true, &former, other)
	require.NotNil(t, perr, "another person admitted by a token")
	assert.Equal(t, problem.SessionRequired, perr.Code)
	assert.NotNil(t, mayAssign(token, true, nil, other), "on a filing as well")
	assert.Nil(t, mayAssign(token, true, &other, me), "to its own person")
	assert.Nil(t, mayAssign(token, true, &other, other), "the assignee as it was")
	assert.Nil(t, mayAssign(token, false, &former, other), "a ticket that is not confidential")
}
