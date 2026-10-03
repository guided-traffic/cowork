//go:build integration

package integration

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/test/fixture"
)

func (e ticketEnv) stake(t *testing.T, c caller, tk apigen.Ticket, weight apigen.InterestWeight, note string) *apigen.SetInterestResponse {
	t.Helper()
	res, err := e.s.client(t, c).SetInterestWithResponse(e.ctx, e.SlugA, tk.Project, tk.Number, apigen.InterestSet{Weight: weight, Note: &note})
	require.NoError(t, err)
	return res
}

// docs/adr/0013: one stake per person and ticket, changeable and removable;
// a viewer watches only; an agent needs the interest capability beyond
// watch; stakes outlive the work as settled.
func TestInterest(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	member, viewer := caller{Token: e.tk.MemberA}, caller{Token: e.tk.ViewerA}
	tk := e.file(t, member, "ALPHA", task("Wanted"))

	res := e.stake(t, member, tk, apigen.InterestWeightNeed, "for the release on the 15th")
	require.Equal(t, http.StatusCreated, res.StatusCode(), string(res.Body))
	res = e.stake(t, member, tk, apigen.InterestWeightUrgent, "it blocks the release now")
	require.Equal(t, http.StatusOK, res.StatusCode())
	assert.Equal(t, apigen.InterestWeightUrgent, res.JSON200.Weight)
	assert.Equal(t, http.StatusOK, e.stake(t, member, tk, apigen.InterestWeightUrgent, "it blocks the release now").StatusCode())
	n, err := f.QueryCount(e.ctx, "SELECT count(*) FROM ticket_interest WHERE ticket_id = $1", tk.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n, "one row per person and ticket")
	acts, err := f.QueryCount(e.ctx, "SELECT count(*) FROM audit_events WHERE ticket_id = $1 AND action = 'interest'", tk.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 2, acts, "nothing recorded when unchanged")

	assert.Equal(t, http.StatusCreated, e.stake(t, viewer, tk, apigen.InterestWeightWatch, "").StatusCode(), "a viewer watches")
	assert.Equal(t, http.StatusForbidden, e.stake(t, viewer, tk, apigen.InterestWeightNeed, "x").StatusCode(), "and only watches")

	narrow, _, err := f.Token(e.ctx, fixture.TokenSpec{UserID: e.Both, Agent: true, Capabilities: []string{"drop"}})
	require.NoError(t, err)
	agent := caller{Token: narrow, Agent: "claude-code/opus/s1"}
	refused := e.stake(t, agent, tk, apigen.InterestWeightNeed, "x")
	require.Equal(t, http.StatusForbidden, refused.StatusCode())
	assert.Equal(t, "missing capability: interest", *refused.ApplicationproblemJSONDefault.Detail)
	assert.Equal(t, http.StatusCreated, e.stake(t, agent, tk, apigen.InterestWeightWatch, "").StatusCode(), "an agent watches at baseline")
	full := caller{Token: e.tk.AgentA, Agent: "claude-code/opus/s2"}
	assert.Equal(t, http.StatusOK, e.stake(t, full, tk, apigen.InterestWeightNeed, "x").StatusCode(), "with interest")

	e.walk(t, member, tk, toAnalysed, toDecided, toInProgress, toDone)
	list, err := e.s.client(t, member).ListInterestWithResponse(e.ctx, e.SlugA, "ALPHA", tk.Number, &apigen.ListInterestParams{})
	require.NoError(t, err)
	require.Len(t, list.JSON200.Items, 3)
	for _, i := range list.JSON200.Items {
		assert.True(t, i.Settled, "stakes outlive the work")
	}

	path := fmt.Sprintf("%s/%d/interest", e.projectTickets("ALPHA"), tk.Number)
	assert.Equal(t, http.StatusNoContent, e.s.do(t, viewer, http.MethodDelete, path, nil).StatusCode)
	assert.Equal(t, http.StatusNoContent, e.s.do(t, viewer, http.MethodDelete, path, nil).StatusCode, "idempotent")
	assert.Equal(t, http.StatusNoContent, e.s.do(t, agent, http.MethodDelete, path, nil).StatusCode, "an agent removes its person's stake: the open gate")
	assertProblem(t, e.s.do(t, caller{Token: e.tk.MemberB}, http.MethodGet, path, nil), http.StatusNotFound, "not_found")
}

// docs/adr/0049 D1: interest=me|any, negatable; a stake is visible with its
// ticket only.
func TestInterestFilter(t *testing.T) {
	e := newTicketEnv(t)
	member, both, viewer := caller{Token: e.tk.MemberA}, caller{Token: e.tk.Both}, caller{Token: e.tk.ViewerA}
	mine := e.file(t, member, "ALPHA", task("Mine"))
	theirs := e.file(t, member, "ALPHA", task("Theirs"))
	e.file(t, member, "ALPHA", task("Nobody's"))
	secret := e.file(t, member, "ALPHA", task("Secret", func(b *apigen.TicketCreate) {
		b.Security, b.Threat = apigen.SecurityClassLive, ptr("leak")
	}))
	require.Equal(t, http.StatusCreated, e.stake(t, member, mine, apigen.InterestWeightWatch, "").StatusCode())
	require.Equal(t, http.StatusCreated, e.stake(t, both, theirs, apigen.InterestWeightWatch, "").StatusCode())
	require.Equal(t, http.StatusCreated, e.stake(t, member, secret, apigen.InterestWeightWatch, "").StatusCode())

	assert.Equal(t, []string{"Secret", "Mine"}, e.titles(t, member, e.tenantTickets(), "interest=me"))
	assert.Equal(t, []string{"Secret", "Theirs", "Mine"}, e.titles(t, member, e.tenantTickets(), "interest=any"))
	assert.Equal(t, []string{"Nobody's"}, e.titles(t, member, e.tenantTickets(), "interest=!any"))
	assert.Equal(t, []string{"Theirs", "Mine"}, e.titles(t, viewer, e.tenantTickets(), "interest=any"), "a hidden ticket's stakes stay hidden")
	assertProblem(t, e.s.do(t, member, http.MethodGet, e.tenantTickets()+"?interest=all", nil), http.StatusBadRequest, "validation_failed")
	assertProblem(t, e.s.do(t, viewer, http.MethodGet, fmt.Sprintf("%s/%d/interest", e.projectTickets("ALPHA"), secret.Number), nil),
		http.StatusNotFound, "not_found")
}
