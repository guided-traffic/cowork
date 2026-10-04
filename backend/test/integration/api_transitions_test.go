//go:build integration

package integration

import (
	"fmt"
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/test/fixture"
)

// move sends a transition as c.
func (e ticketEnv) move(t *testing.T, c caller, tk apigen.Ticket, body apigen.Transition) *apigen.TransitionTicketResponse {
	t.Helper()
	res, err := e.s.client(t, c).TransitionTicketWithResponse(e.ctx, e.SlugA, tk.Project, tk.Number, &apigen.TransitionTicketParams{}, body)
	require.NoError(t, err)
	return res
}

// walk moves a ticket through the given states, requiring each step.
func (e ticketEnv) walk(t *testing.T, c caller, tk apigen.Ticket, to ...apigen.TicketState) apigen.Ticket {
	t.Helper()
	for _, s := range to {
		body := apigen.Transition{From: tk.State, To: s}
		if s == apigen.TicketStateDone {
			body.Note = ptr("go test ./... passed")
		}
		res := e.move(t, c, tk, body)
		require.Equal(t, http.StatusOK, res.StatusCode(), "%s → %s: %s", tk.State, s, res.Body)
		tk = *res.JSON200
	}
	return tk
}

var (
	toAnalysed   = apigen.TicketStateAnalysed
	toDecided    = apigen.TicketStateDecided
	toInProgress = apigen.TicketStateInProgress
	toDone       = apigen.TicketStateDone
)

// docs/adr/0009 D2–D6: the matrix, its required inputs, the acts and the
// dates; a repeated transition is a stale from.
func TestTransitionMatrix(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	member := caller{Token: e.tk.MemberA}
	tk := e.file(t, member, "ALPHA", task("Walk"))

	skip := e.move(t, member, tk, apigen.Transition{From: apigen.TicketStateFiled, To: apigen.TicketStateDecided})
	require.Equal(t, http.StatusConflict, skip.StatusCode(), "forward is one step")
	assert.Equal(t, "state_conflict", string(skip.ApplicationproblemJSONDefault.Code))

	tk = e.walk(t, member, tk, toAnalysed)
	again := e.move(t, member, tk, apigen.Transition{From: apigen.TicketStateFiled, To: apigen.TicketStateAnalysed})
	require.Equal(t, http.StatusConflict, again.StatusCode(), "a repeated transition names a stale state")
	assert.Equal(t, "analysed", (*again.ApplicationproblemJSONDefault.Errors)[0].Current.MustGet())

	tk = e.walk(t, member, tk, toDecided)
	require.False(t, tk.DecidedAt.IsNull(), "reaching decided sets decided_at")
	back := e.move(t, member, tk, apigen.Transition{From: tk.State, To: apigen.TicketStateAnalysed})
	assert.Equal(t, http.StatusBadRequest, back.StatusCode(), "a backward move needs a reason")
	back = e.move(t, member, tk, apigen.Transition{From: tk.State, To: apigen.TicketStateAnalysed, Reason: ptr("the analysis missed the importer")})
	require.Equal(t, http.StatusOK, back.StatusCode(), string(back.Body))
	tk = e.walk(t, member, *back.JSON200, toDecided, toInProgress)

	noNote := e.move(t, member, tk, apigen.Transition{From: tk.State, To: toDone})
	assert.Equal(t, http.StatusBadRequest, noNote.StatusCode(), "done needs a verification note")
	tk = e.walk(t, member, tk, toDone)
	assert.Equal(t, 0, tk.Progress, "done by hand leaves the stages as they are (docs/adr/0017 D5)")
	assert.False(t, tk.DoneAt.IsNull())

	reopen := e.move(t, member, tk, apigen.Transition{From: toDone, To: apigen.TicketStateFiled, Reason: ptr("x")})
	assert.Equal(t, http.StatusConflict, reopen.StatusCode(), "a done ticket returns to the state it was done from only")
	reopen = e.move(t, member, tk, apigen.Transition{From: toDone, To: toInProgress, Reason: ptr("the fix regressed")})
	require.Equal(t, http.StatusOK, reopen.StatusCode(), string(reopen.Body))
	assert.True(t, reopen.JSON200.DoneAt.IsNull(), "a withdrawn ticket is not done")
	back = e.move(t, member, *reopen.JSON200, apigen.Transition{From: toInProgress, To: apigen.TicketStateAnalysed, Reason: ptr("rethink")})
	require.Equal(t, http.StatusOK, back.StatusCode(), string(back.Body))

	drop := e.move(t, member, *back.JSON200, apigen.Transition{From: apigen.TicketStateAnalysed, To: apigen.TicketStateDropped})
	assert.Equal(t, http.StatusBadRequest, drop.StatusCode(), "dropped needs a reason")

	var note string
	require.NoError(t, f.QueryRow(e.ctx, `SELECT note FROM audit_events WHERE ticket_id = $1 AND action = 'transitioned'
		AND after->>'state' = 'done'`, tk.Id).Scan(&note))
	assert.Equal(t, "go test ./... passed", note, "the note lives on the act (docs/adr/0009 D5)")
	n, err := f.QueryCount(e.ctx, "SELECT count(*) FROM audit_events WHERE ticket_id = $1 AND action = 'transitioned'", tk.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 8, n, "one act per transition and none for a refusal")
}

// docs/adr/0009 D2: blocked keeps its origin and leaves only to it; a block
// that waits on a ticket records the blocks link.
func TestBlockingTickets(t *testing.T) {
	e := newTicketEnv(t)
	member := caller{Token: e.tk.MemberA}
	tk := e.walk(t, member, e.file(t, member, "ALPHA", task("Blocked work")), toAnalysed, toDecided)
	release := e.file(t, member, "ALPHA", task("Release 2.0"))

	noBlock := e.move(t, member, tk, apigen.Transition{From: toDecided, To: apigen.TicketStateBlocked, Reason: ptr("waiting")})
	assert.Equal(t, http.StatusBadRequest, noBlock.StatusCode())
	block := &apigen.BlockSet{Kind: apigen.BlockKindRelease, Ticket: ptr("ALPHA-" + strconv.Itoa(release.Number))}
	res := e.move(t, member, tk, apigen.Transition{From: toDecided, To: apigen.TicketStateBlocked, Reason: ptr("needs 2.0 out"), Block: block})
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	b := res.JSON200.Block.MustGet()
	assert.Equal(t, apigen.TicketStateDecided, b.From)
	assert.Equal(t, "needs 2.0 out", b.Reason)
	assert.Equal(t, release.Key, b.Ticket.MustGet())
	assert.Equal(t, apigen.UrgencyLater, res.JSON200.Urgency, "a block moves no horizon (docs/adr/0010 D3)")
	assert.Equal(t, []string{"blocks " + tk.Key}, e.links(t, member, release), "the block records its blocks link")

	wrong := e.move(t, member, *res.JSON200, apigen.Transition{From: apigen.TicketStateBlocked, To: toInProgress})
	assert.Equal(t, http.StatusConflict, wrong.StatusCode(), "blocked leaves only to where it came from")
	out := e.move(t, member, *res.JSON200, apigen.Transition{From: apigen.TicketStateBlocked, To: toDecided})
	require.Equal(t, http.StatusOK, out.StatusCode(), string(out.Body))
	assert.True(t, out.JSON200.Block.IsNull())
	assert.Equal(t, apigen.UrgencyLater, out.JSON200.Urgency)
	assert.Equal(t, http.StatusOK, e.move(t, member, *out.JSON200,
		apigen.Transition{From: toDecided, To: apigen.TicketStateBlocked, Reason: ptr("again"), Block: &apigen.BlockSet{Kind: apigen.BlockKindHuman}}).StatusCode(),
		"a ticket may be blocked any number of times")
}

// docs/adr/0012 D7: done over open prerequisites is refused, listing them; a
// person overrides with a reason, an agent cannot; dropped is never refused;
// a prerequisite the caller cannot see neither shows nor refuses.
func TestDoneOverOpenPrerequisites(t *testing.T) {
	e := newTicketEnv(t)
	member := caller{Token: e.tk.MemberA}
	agent := caller{Token: e.tk.AgentA, Agent: "claude-code/opus/s1"}
	prereq := e.file(t, member, "ALPHA", task("Prerequisite"))
	tk := e.walk(t, member, e.file(t, member, "ALPHA", task("Dependent")), toAnalysed, toDecided, toInProgress)
	require.Equal(t, http.StatusCreated, e.link(t, member, prereq, apigen.LinkTypeBlocks, tk).StatusCode)

	done := apigen.Transition{From: toInProgress, To: toDone, Note: ptr("verified")}
	refused := e.move(t, member, tk, done)
	require.Equal(t, http.StatusConflict, refused.StatusCode())
	assert.Equal(t, "open_prerequisites", string(refused.ApplicationproblemJSONDefault.Code))
	assert.Contains(t, (*refused.ApplicationproblemJSONDefault.Errors)[0].Message, prereq.Key)

	byAgent := done
	byAgent.OverridePrerequisites, byAgent.Reason = ptr(true), ptr("the agent insists")
	res := e.move(t, agent, tk, byAgent)
	require.Equal(t, http.StatusForbidden, res.StatusCode())
	assert.Equal(t, "hard-off: overriding the prerequisite refusal", *res.ApplicationproblemJSONDefault.Detail)

	noReason := done
	noReason.OverridePrerequisites = ptr(true)
	assert.Equal(t, http.StatusBadRequest, e.move(t, member, tk, noReason).StatusCode(), "an override needs a reason")

	dropped := e.move(t, member, tk, apigen.Transition{From: toInProgress, To: apigen.TicketStateDropped, Reason: ptr("superseded")})
	require.Equal(t, http.StatusOK, dropped.StatusCode(), "dropped is never refused by a prerequisite")
	reopened := e.move(t, member, *dropped.JSON200, apigen.Transition{From: apigen.TicketStateDropped, To: apigen.TicketStateFiled, Reason: ptr("needed after all")})
	require.Equal(t, http.StatusOK, reopened.StatusCode())
	tk = e.walk(t, member, *reopened.JSON200, toAnalysed, toDecided, toInProgress)

	byPerson := done
	byPerson.OverridePrerequisites, byPerson.Reason = ptr(true), ptr("the prerequisite is moot since the redesign")
	res = e.move(t, member, tk, byPerson)
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	var overridden string
	require.NoError(t, fixtures(t).QueryRow(e.ctx, `SELECT after->>'overridden_prerequisites' FROM audit_events
		WHERE ticket_id = $1 AND action = 'transitioned' AND after->>'state' = 'done'`, tk.Id).Scan(&overridden))
	assert.Contains(t, overridden, prereq.Key, "the act records the close over open prerequisites")

	settled := e.walk(t, member, e.file(t, member, "ALPHA", task("Settled later")), toAnalysed, toDecided, toInProgress)
	first := e.file(t, member, "ALPHA", task("First"))
	require.Equal(t, http.StatusCreated, e.link(t, member, first, apigen.LinkTypeBlocks, settled).StatusCode)
	e.walk(t, member, first, toAnalysed, toDecided, toInProgress, toDone)
	assert.Equal(t, http.StatusOK, e.move(t, member, settled, done).StatusCode(), "allowed once the prerequisite is done")

	hidden := e.file(t, member, "ALPHA", task("Hidden prerequisite", func(b *apigen.TicketCreate) {
		b.Security, b.Threat = apigen.SecurityClassLive, ptr("leak")
	}))
	both := caller{Token: e.tk.Both}
	blind := e.walk(t, both, e.file(t, both, "ALPHA", task("Closed blind")), toAnalysed, toDecided, toInProgress)
	require.Equal(t, http.StatusCreated, e.link(t, member, hidden, apigen.LinkTypeBlocks, blind).StatusCode)
	assert.Equal(t, http.StatusOK, e.move(t, both, blind, done).StatusCode(), "a prerequisite the closer cannot see does not refuse")
}

// docs/adr/0043 D2–D4: the baseline moves, a capability per gated move, and
// backward moves and reopens open to agents (the open gate).
func TestAgentTransitions(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	member := caller{Token: e.tk.MemberA}
	agent := caller{Token: e.tk.AgentA, Agent: "claude-code/opus/s1"}
	none, _, err := f.Token(e.ctx, fixture.TokenSpec{UserID: e.MemberA, Agent: true, Capabilities: []string{"interest"}})
	require.NoError(t, err)
	bare := caller{Token: none, Agent: "claude-code/opus/s2"}

	tk := e.walk(t, bare, e.file(t, member, "ALPHA", task("Agent work")), toAnalysed)
	for _, c := range []struct {
		to         apigen.TicketState
		capability string
		body       apigen.Transition
	}{
		{toDecided, "decide", apigen.Transition{From: toAnalysed, To: toDecided}},
		{apigen.TicketStateDropped, "drop", apigen.Transition{From: toAnalysed, To: apigen.TicketStateDropped, Reason: ptr("no")}},
	} {
		res := e.move(t, bare, tk, c.body)
		require.Equal(t, http.StatusForbidden, res.StatusCode(), c.capability)
		assert.Equal(t, "missing capability: "+c.capability, *res.ApplicationproblemJSONDefault.Detail)
	}
	tk = e.walk(t, agent, tk, toDecided)
	tk = e.walk(t, bare, tk, toInProgress)
	blocked := e.move(t, bare, tk, apigen.Transition{From: toInProgress, To: apigen.TicketStateBlocked, Reason: ptr("needs a person"),
		Block: &apigen.BlockSet{Kind: apigen.BlockKindHuman}})
	require.Equal(t, http.StatusOK, blocked.StatusCode(), "into blocked and back is the baseline")
	tk = e.walk(t, bare, *blocked.JSON200, toInProgress)
	res := e.move(t, bare, tk, apigen.Transition{From: toInProgress, To: toDone, Note: ptr("ok")})
	require.Equal(t, http.StatusForbidden, res.StatusCode())
	assert.Equal(t, "missing capability: close", *res.ApplicationproblemJSONDefault.Detail)
	back := e.move(t, bare, tk, apigen.Transition{From: toInProgress, To: toAnalysed, Reason: ptr("the analysis was wrong")})
	require.Equal(t, http.StatusOK, back.StatusCode(), "an agent's backward move: the open gate")
	tk = e.walk(t, agent, *back.JSON200, toDecided, toInProgress, toDone)
	reopen := e.move(t, bare, tk, apigen.Transition{From: toDone, To: toInProgress, Reason: ptr("regressed")})
	require.Equal(t, http.StatusOK, reopen.StatusCode(), "an agent withdraws a done by hand: the open gate")

	assert.Equal(t, http.StatusForbidden, e.move(t, caller{Token: e.tk.ViewerA}, *reopen.JSON200,
		apigen.Transition{From: toInProgress, To: apigen.TicketStateReview}).StatusCode())
	assertProblem(t, e.s.do(t, caller{Token: e.tk.MemberB}, http.MethodPost,
		fmt.Sprintf("%s/%d/transitions", e.projectTickets("ALPHA"), tk.Number), map[string]any{"from": "filed", "to": "analysed"}),
		http.StatusNotFound, "not_found")
}

// docs/adr/0010 D3 as amended 2026-10-04: a decision that blocks a ticket,
// settles, reopens or stops being a decision moves no ticket to another
// horizon, and leaves the version of what it blocks alone.
func TestSettlingADecisionLeavesTheHorizonOfWhatItBlocks(t *testing.T) {
	e := newTicketEnv(t)
	member := caller{Token: e.tk.MemberA}
	work := e.file(t, member, "ALPHA", task("Waits for the call", into(apigen.UrgencyNext)))
	decision := e.file(t, member, "ALPHA", task("Pick the queue", func(b *apigen.TicketCreate) { b.Type = apigen.TicketTypeDecision }))
	require.Equal(t, http.StatusCreated, e.link(t, member, decision, apigen.LinkTypeBlocks, work).StatusCode)
	assert.Equal(t, apigen.UrgencyNext, e.get(t, member, "ALPHA", work.Number).JSON200.Urgency)
	version := e.get(t, member, "ALPHA", work.Number).JSON200.Version

	res := e.move(t, member, decision, apigen.Transition{From: apigen.TicketStateFiled, To: apigen.TicketStateDropped, Reason: ptr("moot")})
	require.Equal(t, http.StatusOK, res.StatusCode())
	got := e.get(t, member, "ALPHA", work.Number).JSON200
	assert.Equal(t, apigen.UrgencyNext, got.Urgency, "settled")
	assert.Equal(t, version, got.Version)

	res = e.move(t, member, *res.JSON200, apigen.Transition{From: apigen.TicketStateDropped, To: apigen.TicketStateFiled, Reason: ptr("not moot")})
	require.Equal(t, http.StatusOK, res.StatusCode())
	assert.Equal(t, apigen.UrgencyNext, e.get(t, member, "ALPHA", work.Number).JSON200.Urgency, "reopened")

	retyped := e.patch(t, member, *res.JSON200, apigen.TicketPatch{Type: ptr(apigen.TicketTypeTask)})
	require.Equal(t, http.StatusOK, retyped.StatusCode(), string(retyped.Body))
	got = e.get(t, member, "ALPHA", work.Number).JSON200
	assert.Equal(t, apigen.UrgencyNext, got.Urgency, "no longer a decision")
	assert.Equal(t, version, got.Version)
}
