//go:build integration

package integration

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/test/fixture"
)

// into files a ticket into a horizon.
func into(horizon apigen.Urgency) func(*apigen.TicketCreate) {
	return func(b *apigen.TicketCreate) { b.Urgency = &horizon }
}

// docs/adr/0010 D3, docs/adr/0014 D2 as amended 2026-10-04: a filing names its
// horizon and its place in it. Without a horizon the ticket is later, without
// a place at the bottom — the end of its horizon; another horizon is the
// override, set by the filer without a reason; the filing act records both.
func TestFilingIntoAHorizonAtAPlace(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	member := caller{Token: e.tk.MemberA}

	first := e.file(t, member, "ALPHA", task("first next", into(apigen.UrgencyNext)))
	assert.Equal(t, apigen.UrgencyNext, first.Urgency)
	set := first.UrgencyOverride.MustGet()
	assert.Equal(t, apigen.UrgencyNext, set.Value)
	assert.True(t, set.Reason.IsNull(), "a filing gives no reason")
	assert.Equal(t, e.MemberA, set.By.MustGet().Id, "set by the filer")
	assert.Equal(t, apigen.UrgencyLater, first.UrgencyDerived)
	assert.Equal(t, "v2:default", first.UrgencyRule)

	plain := e.file(t, member, "ALPHA", task("plain"))
	assert.Equal(t, apigen.UrgencyLater, plain.Urgency, "no horizon is later")
	assert.True(t, plain.UrgencyOverride.IsNull())
	explicit := e.file(t, member, "ALPHA", task("explicit later", into(apigen.UrgencyLater)))
	assert.True(t, explicit.UrgencyOverride.IsNull(), "later named is no override")

	e.file(t, member, "ALPHA", task("before first", into(apigen.UrgencyNext), func(b *apigen.TicketCreate) { b.Before = &first.Number }))
	after := e.file(t, member, "ALPHA", task("after first", into(apigen.UrgencyNext), func(b *apigen.TicketCreate) { b.After = &first.Number }))
	e.file(t, member, "ALPHA", task("end of next", into(apigen.UrgencyNext)))
	assert.Equal(t, []string{"before first", "first next", "after first", "end of next"},
		e.titles(t, member, e.projectTickets("ALPHA"), "urgency=next"), "the places in the horizon, the end without one")
	assert.Equal(t, []string{"before first", "first next", "after first", "plain", "explicit later", "end of next"},
		e.titles(t, member, e.projectTickets("ALPHA"), ""))

	var horizon, neighbour string
	require.NoError(t, f.QueryRow(e.ctx, "SELECT after->>'urgency', after->>'after' FROM audit_events WHERE ticket_id = $1 AND action = 'created'",
		after.Id).Scan(&horizon, &neighbour))
	assert.Equal(t, "next", horizon)
	assert.Equal(t, first.Key, neighbour)

	dropped := e.file(t, member, "ALPHA", task("dropped"))
	require.Equal(t, http.StatusOK, e.move(t, member, dropped, apigen.Transition{From: apigen.TicketStateFiled,
		To: apigen.TicketStateDropped, Reason: ptr("not needed")}).StatusCode())
	before := len(e.titles(t, member, e.projectTickets("ALPHA"), "include_terminal=true"))

	res := e.create(t, member, "ALPHA", task("x", into(apigen.UrgencyNext), func(b *apigen.TicketCreate) { b.After = &plain.Number }))
	body := problemIn(t, http.StatusBadRequest, res.StatusCode(), res.Body, "validation_failed")
	assert.Equal(t, "/after", pointerOf(body))
	assert.Contains(t, string(res.Body), plain.Key+" stands in the horizon later, not in next")

	res = e.create(t, member, "ALPHA", task("x", func(b *apigen.TicketCreate) { b.After, b.Before = &plain.Number, &explicit.Number }))
	body = problemIn(t, http.StatusBadRequest, res.StatusCode(), res.Body, "validation_failed")
	assert.Equal(t, "/before", pointerOf(body))

	res = e.create(t, member, "ALPHA", task("x", func(b *apigen.TicketCreate) { b.Before = &dropped.Number }))
	problemIn(t, http.StatusConflict, res.StatusCode(), res.Body, "state_conflict")

	res = e.create(t, member, "ALPHA", task("x", func(b *apigen.TicketCreate) { b.After = ptr(99999) }))
	body = problemIn(t, http.StatusBadRequest, res.StatusCode(), res.Body, "validation_failed")
	assert.Equal(t, "/after", pointerOf(body))

	assert.Len(t, e.titles(t, member, e.projectTickets("ALPHA"), "include_terminal=true"), before, "a refused filing files nothing")
}

// docs/adr/0043 D4 as amended 2026-10-04: an agent files into a horizon other
// than later with override-urgency, and names a place with rank.
func TestAnAgentFilesIntoAHorizonWithItsCapabilities(t *testing.T) {
	e := newTicketEnv(t)
	member := caller{Token: e.tk.MemberA}
	baselineToken, _, err := fixtures(t).Token(e.ctx, fixture.TokenSpec{UserID: e.MemberA, Agent: true, Capabilities: []string{}})
	require.NoError(t, err)
	baseline := caller{Token: baselineToken, Agent: "claude-code/opus/s1"}
	assisted := caller{Token: e.tk.AssistedAgentA, Agent: "claude-code/opus/s1"}
	full := caller{Token: e.tk.AgentA, Agent: "claude-code/opus/s1"}
	anchor := e.file(t, member, "ALPHA", task("anchor", into(apigen.UrgencyNext)))

	res := e.create(t, baseline, "ALPHA", task("x", into(apigen.UrgencyNow)))
	body := problemIn(t, http.StatusForbidden, res.StatusCode(), res.Body, "agent_forbidden")
	assert.Contains(t, body["detail"], "override-urgency")
	assert.Equal(t, apigen.UrgencyLater, e.file(t, baseline, "ALPHA", task("later by the baseline", into(apigen.UrgencyLater))).Urgency,
		"later needs nothing beyond the baseline")

	assert.Equal(t, apigen.UrgencyNext, e.file(t, assisted, "ALPHA", task("next by the assisted", into(apigen.UrgencyNext))).Urgency)
	res = e.create(t, assisted, "ALPHA", task("x", into(apigen.UrgencyNext), func(b *apigen.TicketCreate) { b.After = &anchor.Number }))
	body = problemIn(t, http.StatusForbidden, res.StatusCode(), res.Body, "agent_forbidden")
	assert.Contains(t, body["detail"], "rank")

	placed := e.file(t, full, "ALPHA", task("next by the agent", into(apigen.UrgencyNext), func(b *apigen.TicketCreate) { b.Before = &anchor.Number }))
	assert.Equal(t, e.MemberA, placed.UrgencyOverride.MustGet().By.MustGet().Id, "an agent sets the horizon as its person")
	assert.Equal(t, []string{"next by the agent", "anchor", "next by the assisted"},
		e.titles(t, member, e.projectTickets("ALPHA"), "urgency=next"))
}
