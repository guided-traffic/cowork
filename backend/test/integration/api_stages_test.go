//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/test/fixture"
)

var toReview = apigen.TicketStateReview

// staged sets a ticket's three stages as c and requires the 200.
func (e ticketEnv) staged(t *testing.T, c caller, tk apigen.Ticket, refinement, implementation, review int) apigen.Ticket {
	t.Helper()
	res := e.patch(t, c, tk, apigen.TicketPatch{ProgressRefinement: ptr(refinement), Progress: ptr(implementation), ProgressReview: ptr(review)})
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	return *res.JSON200
}

func stagesOf(tk apigen.Ticket) []int {
	return []int{tk.ProgressRefinement, tk.Progress, tk.ProgressReview}
}

// act is the last recorded act of an action on a ticket.
type act struct {
	Before, After map[string]any
	Reason, Note  *string
}

func lastAct(t *testing.T, f *fixture.DB, id uuid.UUID, action string) act {
	t.Helper()
	var a act
	require.NoError(t, f.QueryRow(t.Context(), `SELECT before, after, reason, note FROM audit_events
		WHERE ticket_id = $1 AND action = $2 ORDER BY id DESC LIMIT 1`, id, action).Scan(&a.Before, &a.After, &a.Reason, &a.Note))
	return a
}

// problemIn checks a problem answer the generated client read and returns its
// body.
func problemIn(t *testing.T, status, got int, raw []byte, code string) map[string]any {
	t.Helper()
	require.Equal(t, status, got, string(raw))
	var body map[string]any
	require.NoError(t, json.Unmarshal(raw, &body))
	assert.Equal(t, code, body["code"], string(raw))
	return body
}

// pointerOf is the pointer of a problem's first field error.
func pointerOf(body map[string]any) any {
	errs, _ := body["errors"].([]any)
	if len(errs) == 0 {
		return nil
	}
	return errs[0].(map[string]any)["pointer"]
}

func actions(t *testing.T, f *fixture.DB, id uuid.UUID) []string {
	t.Helper()
	rows, err := f.Query(t.Context(), "SELECT action::text FROM audit_events WHERE ticket_id = $1 AND entity_type = 'ticket' ORDER BY id", id)
	require.NoError(t, err)
	defer rows.Close()
	var out []string
	for rows.Next() {
		var a string
		require.NoError(t, rows.Scan(&a))
		out = append(out, a)
	}
	require.NoError(t, rows.Err())
	return out
}

// docs/adr/0009 D1, D3: review follows in-progress, goes back to it with a
// reason, is blocked and unblocked like any open state and dropped from;
// in-progress → review is in the agent baseline.
func TestReviewState(t *testing.T) {
	e := newTicketEnv(t)
	member := caller{Token: e.tk.MemberA}
	assisted := caller{Token: e.tk.AssistedAgentA, Agent: "claude-code/opus/s1"}
	tk := e.walk(t, member, e.file(t, member, "ALPHA", task("Check it")), toAnalysed, toDecided, toInProgress)
	tk = e.walk(t, assisted, tk, toReview)
	assert.Equal(t, apigen.TicketStateReview, tk.State)

	skip := e.move(t, member, e.walk(t, member, e.file(t, member, "ALPHA", task("Skips")), toAnalysed, toDecided),
		apigen.Transition{From: toDecided, To: toReview})
	assert.Equal(t, http.StatusConflict, skip.StatusCode(), "review follows in-progress only")

	blocked := e.move(t, member, tk, apigen.Transition{From: toReview, To: apigen.TicketStateBlocked, Reason: ptr("the reviewer is away"),
		Block: &apigen.BlockSet{Kind: apigen.BlockKindHuman}})
	require.Equal(t, http.StatusOK, blocked.StatusCode(), string(blocked.Body))
	assert.Equal(t, apigen.TicketStateReview, blocked.JSON200.Block.MustGet().From)
	out := e.move(t, member, *blocked.JSON200, apigen.Transition{From: apigen.TicketStateBlocked, To: toReview})
	require.Equal(t, http.StatusOK, out.StatusCode(), string(out.Body))
	tk = *out.JSON200

	back := e.move(t, member, tk, apigen.Transition{From: toReview, To: toInProgress})
	problemIn(t, http.StatusBadRequest, back.StatusCode(), back.Body, "validation_failed")
	back = e.move(t, assisted, tk, apigen.Transition{From: toReview, To: toInProgress, Reason: ptr("the check found work to do")})
	require.Equal(t, http.StatusOK, back.StatusCode(), "backward with a reason, open to an agent")
	tk = e.walk(t, member, *back.JSON200, toReview)
	dropped := e.move(t, member, tk, apigen.Transition{From: toReview, To: apigen.TicketStateDropped, Reason: ptr("superseded")})
	require.Equal(t, http.StatusOK, dropped.StatusCode(), string(dropped.Body))

	listed := e.titles(t, member, e.projectTickets("ALPHA"), "state=review")
	assert.Empty(t, listed)
	assert.Equal(t, []string{"Skips"}, e.titles(t, member, e.projectTickets("ALPHA"), "state=decided"))
}

// docs/adr/0009 D5: done by hand from any open state for a person, the stages
// left as they are and editable; withdrawn with a reason to the state it came
// from, ranked at the bottom — unless the three stages are full, when the
// ticket stays done by them and leaves done only by a lower stage.
func TestDoneByHand(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	member := caller{Token: e.tk.MemberA}
	e.file(t, member, "ALPHA", task("other"))
	tk := e.file(t, member, "ALPHA", task("Closed early"))

	noNote := e.move(t, member, tk, apigen.Transition{From: apigen.TicketStateFiled, To: toDone})
	problemIn(t, http.StatusBadRequest, noNote.StatusCode(), noNote.Body, "validation_failed")
	res := e.move(t, member, tk, apigen.Transition{From: apigen.TicketStateFiled, To: toDone, Note: ptr("nothing left to do")})
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	tk = *res.JSON200
	assert.True(t, tk.DoneByHand)
	assert.Equal(t, apigen.TicketStateFiled, tk.DoneFrom.MustGet())
	assert.Equal(t, []int{0, 0, 0}, stagesOf(tk), "done by hand leaves the stages")
	assert.False(t, tk.DoneAt.IsNull())
	assert.Nil(t, rankOf(t, f, tk.Id), "done takes the rank away")
	done := lastAct(t, f, tk.Id, "transitioned")
	assert.Equal(t, map[string]any{"state": "done", "done_by_hand": true}, done.After)
	assert.Equal(t, "nothing left to do", *done.Note)

	tk = e.staged(t, member, tk, 50, 20, 0)
	assert.Equal(t, apigen.TicketStateDone, tk.State, "the stages stay editable, the ticket done")
	tk = e.staged(t, member, tk, 10, 20, 0)
	assert.Equal(t, apigen.TicketStateDone, tk.State, "lowering a stage of a ticket done by hand reopens nothing")

	wrong := e.move(t, member, tk, apigen.Transition{From: toDone, To: toAnalysed, Reason: ptr("x")})
	problemIn(t, http.StatusConflict, wrong.StatusCode(), wrong.Body, "state_conflict")
	noReason := e.move(t, member, tk, apigen.Transition{From: toDone, To: apigen.TicketStateFiled})
	problemIn(t, http.StatusBadRequest, noReason.StatusCode(), noReason.Body, "validation_failed")
	back := e.move(t, member, tk, apigen.Transition{From: toDone, To: apigen.TicketStateFiled, Reason: ptr("closed too early")})
	require.Equal(t, http.StatusOK, back.StatusCode(), string(back.Body))
	tk = *back.JSON200
	assert.Equal(t, apigen.TicketStateFiled, tk.State)
	assert.False(t, tk.DoneByHand)
	assert.True(t, tk.DoneFrom.IsNull())
	assert.True(t, tk.DoneAt.IsNull())
	assert.Equal(t, []string{"other", "Closed early"}, e.titles(t, member, e.projectTickets("ALPHA"), ""), "back at the bottom")

	res = e.move(t, member, tk, apigen.Transition{From: apigen.TicketStateFiled, To: toDone, Note: ptr("really done")})
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	tk = e.staged(t, member, *res.JSON200, 100, 100, 100)
	require.True(t, tk.DoneByHand, "filling the stages of a ticket done by hand keeps it done by hand")
	version := tk.Version
	stays := e.move(t, member, tk, apigen.Transition{From: toDone, To: apigen.TicketStateFiled, Reason: ptr("the stages say it")})
	require.Equal(t, http.StatusOK, stays.StatusCode(), string(stays.Body))
	tk = *stays.JSON200
	assert.Equal(t, apigen.TicketStateDone, tk.State, "with the three stages full it stays done, by them")
	assert.False(t, tk.DoneByHand)
	assert.Equal(t, version+1, tk.Version)
	kept := lastAct(t, f, tk.Id, "updated")
	assert.Equal(t, map[string]any{"done_by_hand": true}, kept.Before)
	assert.Equal(t, map[string]any{"done_by_hand": false}, kept.After)
	assert.Equal(t, "the stages say it", *kept.Reason)

	out := e.move(t, member, tk, apigen.Transition{From: toDone, To: apigen.TicketStateFiled, Reason: ptr("again")})
	body := problemIn(t, http.StatusConflict, out.StatusCode(), out.Body, "state_conflict")
	assert.Contains(t, body["detail"], "lower a stage")
	lowered := e.patch(t, member, tk, apigen.TicketPatch{ProgressReview: ptr(50), Reason: ptr("the review was not done")})
	require.Equal(t, http.StatusOK, lowered.StatusCode(), string(lowered.Body))
	assert.Equal(t, apigen.TicketStateFiled, lowered.JSON200.State, "lowered, it returns to the state it was done from")
}

// docs/adr/0043 D4: an agent closes by hand with close, from in-progress and
// review only; withdrawing a done by hand, lowering a stage that reopens and
// the reopen of a dropped ticket need no capability (the open gate).
func TestAgentDoneByHand(t *testing.T) {
	e := newTicketEnv(t)
	member := caller{Token: e.tk.MemberA}
	agent := caller{Token: e.tk.AgentA, Agent: "claude-code/opus/s1"}
	assisted := caller{Token: e.tk.AssistedAgentA, Agent: "claude-code/opus/s1"}
	note := ptr("go test ./... passed")

	decided := e.walk(t, member, e.file(t, member, "ALPHA", task("Decided")), toAnalysed, toDecided)
	res := e.move(t, agent, decided, apigen.Transition{From: toDecided, To: toDone, Note: note})
	body := problemIn(t, http.StatusForbidden, res.StatusCode(), res.Body, "agent_forbidden")
	assert.Contains(t, body["detail"], "close covers in-progress and review")

	review := e.walk(t, member, decided, toInProgress, toReview)
	res = e.move(t, assisted, review, apigen.Transition{From: toReview, To: toDone, Note: note})
	body = problemIn(t, http.StatusForbidden, res.StatusCode(), res.Body, "agent_forbidden")
	assert.Equal(t, "missing capability: close", body["detail"])
	res = e.move(t, agent, review, apigen.Transition{From: toReview, To: toDone, Note: note})
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	assert.True(t, res.JSON200.DoneByHand)

	inProgress := e.walk(t, member, e.file(t, member, "ALPHA", task("In progress")), toAnalysed, toDecided, toInProgress)
	res = e.move(t, agent, inProgress, apigen.Transition{From: toInProgress, To: toDone, Note: note})
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	withdrawn := e.move(t, assisted, *res.JSON200, apigen.Transition{From: toDone, To: toInProgress, Reason: ptr("not verified")})
	require.Equal(t, http.StatusOK, withdrawn.StatusCode(), "withdrawing is no close: the open gate")

	staged := e.staged(t, member, e.walk(t, member, e.file(t, member, "ALPHA", task("Staged")), toAnalysed, toDecided, toInProgress, toReview),
		100, 100, 95)
	closed := e.patch(t, member, staged, apigen.TicketPatch{ProgressReview: ptr(100), Note: note})
	require.Equal(t, http.StatusOK, closed.StatusCode(), string(closed.Body))
	lowered := e.patch(t, assisted, *closed.JSON200, apigen.TicketPatch{ProgressReview: ptr(90), Reason: ptr("the review missed a case")})
	require.Equal(t, http.StatusOK, lowered.StatusCode(), "a lower stage that reopens is no close: the open gate")
	assert.Equal(t, apigen.TicketStateReview, lowered.JSON200.State)

	dropped := e.move(t, member, e.file(t, member, "ALPHA", task("Dropped")), apigen.Transition{From: apigen.TicketStateFiled,
		To: apigen.TicketStateDropped, Reason: ptr("not now")})
	require.Equal(t, http.StatusOK, dropped.StatusCode())
	reopened := e.move(t, assisted, *dropped.JSON200, apigen.Transition{From: apigen.TicketStateDropped, To: apigen.TicketStateFiled,
		Reason: ptr("now after all")})
	require.Equal(t, http.StatusOK, reopened.StatusCode(), "an agent's reopen: the open gate")
}

// docs/adr/0009 D5, docs/adr/0017 D4, D5: the write that fills the last stage
// is the done act — the note, the field act and the transition, the version
// raised once, the rank taken away; the write that lowers a stage reopens to
// the state it was done from, with a reason, at the bottom of the rank.
func TestDoneByTheStages(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	member := caller{Token: e.tk.MemberA}
	tk := e.walk(t, member, e.file(t, member, "ALPHA", task("Staged")), toAnalysed, toDecided, toInProgress, toReview)
	e.file(t, member, "ALPHA", task("below"))
	tk = e.staged(t, member, tk, 100, 100, 95)
	assert.Equal(t, apigen.TicketStateReview, tk.State, "two full stages close nothing")

	refused := e.patch(t, member, tk, apigen.TicketPatch{ProgressReview: ptr(100)})
	body := problemIn(t, http.StatusBadRequest, refused.StatusCode(), refused.Body, "validation_failed")
	assert.Contains(t, body["detail"], "completes the ticket")
	assert.Equal(t, "/note", pointerOf(body))
	assert.Equal(t, 95, e.get(t, member, "ALPHA", tk.Number).JSON200.ProgressReview, "the stage keeps its value")

	version := tk.Version
	res := e.patch(t, member, tk, apigen.TicketPatch{ProgressReview: ptr(100), Note: ptr("reviewed the diff, ran make test-integration")})
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	tk = *res.JSON200
	assert.Equal(t, apigen.TicketStateDone, tk.State)
	assert.False(t, tk.DoneByHand)
	assert.Equal(t, apigen.TicketStateReview, tk.DoneFrom.MustGet())
	assert.False(t, tk.DoneAt.IsNull())
	assert.Equal(t, version+1, tk.Version, "one write, one version")
	assert.Nil(t, rankOf(t, f, tk.Id), "done takes the rank away")
	acts := actions(t, f, tk.Id)
	assert.Equal(t, []string{"updated", "transitioned"}, acts[len(acts)-2:], "the transition beside the field act")
	transitioned := lastAct(t, f, tk.Id, "transitioned")
	assert.Equal(t, map[string]any{"state": "review"}, transitioned.Before)
	assert.Equal(t, map[string]any{"state": "done", "done_by_hand": false}, transitioned.After)
	assert.Equal(t, "reviewed the diff, ran make test-integration", *transitioned.Note)
	updated := lastAct(t, f, tk.Id, "updated")
	assert.Equal(t, map[string]any{"progress_review": float64(100)}, updated.After)

	noReason := e.patch(t, member, tk, apigen.TicketPatch{Progress: ptr(90)})
	body = problemIn(t, http.StatusBadRequest, noReason.StatusCode(), noReason.Body, "validation_failed")
	assert.Contains(t, body["detail"], "reopens it")
	noted := e.patch(t, member, tk, apigen.TicketPatch{Title: ptr("Staged again"), Note: ptr("x")})
	problemIn(t, http.StatusBadRequest, noted.StatusCode(), noted.Body, "validation_failed")

	version = tk.Version
	res = e.patch(t, member, tk, apigen.TicketPatch{Progress: ptr(90), Reason: ptr("a regression in the build")})
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	tk = *res.JSON200
	assert.Equal(t, apigen.TicketStateReview, tk.State, "back to the state it was done from")
	assert.Equal(t, []int{100, 90, 100}, stagesOf(tk))
	assert.True(t, tk.DoneAt.IsNull())
	assert.Equal(t, version+1, tk.Version)
	assert.Equal(t, []string{"below", "Staged"}, e.titles(t, member, e.projectTickets("ALPHA"), ""), "ranked at the bottom")
	reopened := lastAct(t, f, tk.Id, "transitioned")
	assert.Equal(t, map[string]any{"state": "done"}, reopened.Before)
	assert.Equal(t, map[string]any{"state": "review"}, reopened.After)
	assert.Equal(t, "a regression in the build", *reopened.Reason)

	export := e.s.do(t, member, http.MethodGet, fmt.Sprintf("%s/%d/markdown", e.projectTickets("ALPHA"), tk.Number), nil)
	require.Equal(t, http.StatusOK, export.StatusCode)
	raw, err := io.ReadAll(export.Body)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "state: review\n", "the export carries the state review (docs/adr/0044 D1)")
	assert.Contains(t, string(raw), "progress-refinement: 100\nprogress: 90\nprogress-review: 100\n", "and the three stages")
}

// docs/adr/0012 D7, docs/adr/0043 D3, D4: the done act of the stages is
// refused over the open prerequisites the closer can see; a person overrides
// with a reason, an agent never; an agent closes with close from in-progress
// or review, and without it the stage keeps its value. open_prerequisites
// counts what the caller can see.
func TestDoneByTheStagesRefusals(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	member := caller{Token: e.tk.MemberA}
	agent := caller{Token: e.tk.AgentA, Agent: "claude-code/opus/s1"}
	assisted := caller{Token: e.tk.AssistedAgentA, Agent: "claude-code/opus/s1"}
	both := caller{Token: e.tk.Both}
	note := ptr("verified against staging")

	prereq := e.file(t, member, "ALPHA", task("Prerequisite"))
	hidden := e.file(t, member, "ALPHA", task("Hidden prerequisite", func(b *apigen.TicketCreate) {
		b.Security, b.Threat = apigen.SecurityClassLive, ptr("leak")
	}))
	tk := e.staged(t, member, e.walk(t, member, e.file(t, member, "ALPHA", task("Dependent")), toAnalysed, toDecided, toInProgress, toReview),
		100, 100, 95)
	require.Equal(t, http.StatusCreated, e.link(t, member, prereq, apigen.LinkTypeBlocks, tk).StatusCode)
	require.Equal(t, http.StatusCreated, e.link(t, member, hidden, apigen.LinkTypeBlocks, tk).StatusCode)
	tk = *e.get(t, member, "ALPHA", tk.Number).JSON200
	assert.Equal(t, 2, tk.OpenPrerequisites, "the reporter sees both")
	assert.Equal(t, 1, e.get(t, both, "ALPHA", tk.Number).JSON200.OpenPrerequisites, "a hidden prerequisite is never counted")
	listed := e.listed(t, both, "ALPHA", "", 50)
	for _, it := range listed {
		if it.Id == tk.Id {
			assert.Equal(t, 1, it.OpenPrerequisites, "nor in a list")
		}
	}

	res := e.patch(t, member, tk, apigen.TicketPatch{ProgressReview: ptr(100), Note: note})
	body := problemIn(t, http.StatusConflict, res.StatusCode(), res.Body, "open_prerequisites")
	assert.Len(t, body["errors"], 2)
	assert.Equal(t, "/progress_review", pointerOf(body), "the field that closes")
	assert.Equal(t, 95, e.get(t, member, "ALPHA", tk.Number).JSON200.ProgressReview)

	override := apigen.TicketPatch{ProgressReview: ptr(100), Note: note, OverridePrerequisites: ptr(true)}
	res = e.patch(t, member, tk, override)
	problemIn(t, http.StatusBadRequest, res.StatusCode(), res.Body, "validation_failed")
	override.Reason = ptr("the prerequisites are moot")
	res = e.patch(t, agent, tk, override)
	body = problemIn(t, http.StatusForbidden, res.StatusCode(), res.Body, "agent_forbidden")
	assert.Equal(t, "hard-off: overriding the prerequisite refusal", body["detail"])
	res = e.patch(t, member, tk, override)
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	assert.Equal(t, apigen.TicketStateDone, res.JSON200.State)
	assert.Equal(t, 2, res.JSON200.OpenPrerequisites)
	act := lastAct(t, f, tk.Id, "transitioned")
	assert.ElementsMatch(t, []any{prereq.Key, hidden.Key}, act.After["overridden_prerequisites"])
	assert.Equal(t, "the prerequisites are moot", *act.Reason)

	review := e.staged(t, member, e.walk(t, member, e.file(t, member, "ALPHA", task("Review")), toAnalysed, toDecided, toInProgress, toReview),
		100, 100, 95)
	res = e.patch(t, assisted, review, apigen.TicketPatch{ProgressReview: ptr(100), Note: note})
	body = problemIn(t, http.StatusForbidden, res.StatusCode(), res.Body, "agent_forbidden")
	assert.Equal(t, "missing capability: close", body["detail"])
	assert.Equal(t, 95, e.get(t, member, "ALPHA", review.Number).JSON200.ProgressReview, "the stage keeps its value")
	res = e.patch(t, agent, review, apigen.TicketPatch{ProgressReview: ptr(100), Note: note})
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	assert.Equal(t, apigen.TicketStateDone, res.JSON200.State, "an agent with close from review")

	decided := e.staged(t, member, e.walk(t, member, e.file(t, member, "ALPHA", task("Decided")), toAnalysed, toDecided), 100, 100, 95)
	res = e.patch(t, agent, decided, apigen.TicketPatch{ProgressReview: ptr(100), Note: note})
	body = problemIn(t, http.StatusForbidden, res.StatusCode(), res.Body, "agent_forbidden")
	assert.Contains(t, body["detail"], "close covers in-progress and review")
	res = e.patch(t, agent, decided, apigen.TicketPatch{ProgressReview: ptr(90)})
	require.Equal(t, http.StatusOK, res.StatusCode(), "a stage that closes nothing is the agent's baseline")

	filed := e.staged(t, member, e.file(t, member, "ALPHA", task("Filed")), 100, 100, 95)
	res = e.patch(t, member, filed, apigen.TicketPatch{ProgressReview: ptr(100), Note: note})
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	assert.Equal(t, apigen.TicketStateFiled, res.JSON200.DoneFrom.MustGet(), "a person closes by the stages from filed")
}

// docs/adr/0009 D2, D5: a ticket done from blocked keeps its block and takes
// it back when the done by hand is withdrawn; the horizon stays later through
// all of it (docs/adr/0010 D3 as amended 2026-10-04).
func TestDoneFromBlocked(t *testing.T) {
	e := newTicketEnv(t)
	member := caller{Token: e.tk.MemberA}
	tk := e.walk(t, member, e.file(t, member, "ALPHA", task("Waits on 2.0")), toAnalysed, toDecided)
	res := e.move(t, member, tk, apigen.Transition{From: toDecided, To: apigen.TicketStateBlocked, Reason: ptr("needs 2.0 out"),
		Block: &apigen.BlockSet{Kind: apigen.BlockKindRelease, ExternalRef: ptr("RELEASE-2.0")}})
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	assert.Equal(t, apigen.HorizonLater, res.JSON200.Horizon, "a block on a release moves no horizon")

	res = e.move(t, member, *res.JSON200, apigen.Transition{From: apigen.TicketStateBlocked, To: toDone, Note: ptr("2.0 shipped it")})
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	assert.Equal(t, apigen.TicketStateBlocked, res.JSON200.DoneFrom.MustGet())
	assert.True(t, res.JSON200.Block.IsNull(), "a done ticket shows no block")
	assert.Equal(t, apigen.HorizonLater, res.JSON200.Horizon)

	res = e.move(t, member, *res.JSON200, apigen.Transition{From: toDone, To: apigen.TicketStateBlocked, Reason: ptr("2.0 slipped")})
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	b := res.JSON200.Block.MustGet()
	assert.Equal(t, apigen.TicketStateDecided, b.From, "the block it kept")
	assert.Equal(t, apigen.BlockKindRelease, b.Kind)
	assert.Equal(t, "needs 2.0 out", b.Reason)
	assert.Equal(t, "RELEASE-2.0", b.ExternalRef.MustGet())
	assert.Equal(t, apigen.HorizonLater, res.JSON200.Horizon)
	res = e.move(t, member, *res.JSON200, apigen.Transition{From: apigen.TicketStateBlocked, To: toDecided})
	require.Equal(t, http.StatusOK, res.StatusCode(), "and leaves it to where it came from")
}

// docs/adr/0017 D3: each stage of a parent is the effort-weighted mean of its
// children's same stage, a done child at 100 in each, a dropped one left out;
// a parent takes no value of its own and is never done by its stages, and a
// parent done by hand shows them as its children make them.
func TestStagesOfAParent(t *testing.T) {
	e := newTicketEnv(t)
	member := caller{Token: e.tk.MemberA}
	get := func(tk apigen.Ticket) apigen.Ticket { return *e.get(t, member, "ALPHA", tk.Number).JSON200 }
	parent := e.file(t, member, "ALPHA", task("Epic"))
	version := parent.Version
	child := func(title string, effort apigen.Effort) apigen.Ticket {
		return e.file(t, member, "ALPHA", task(title, func(b *apigen.TicketCreate) {
			b.Effort, b.Parent = effort, ptr("ALPHA-"+strconv.Itoa(parent.Number))
		}))
	}
	small, large := child("small", apigen.EffortXS), child("large", apigen.EffortL)
	small = e.staged(t, member, small, 100, 50, 0)
	got := get(parent)
	assert.True(t, got.ProgressDerived)
	assert.Equal(t, []int{15, 10, 0}, stagesOf(got), "(1×100 + 5×0) / 6 = 16.7 → 15; (1×50) / 6 = 8.3 → 10")
	assert.Equal(t, version, got.Version, "a derived change bumps no version")

	refused := e.patch(t, member, got, apigen.TicketPatch{ProgressReview: ptr(50)})
	problemIn(t, http.StatusConflict, refused.StatusCode(), refused.Body, "state_conflict")

	res := e.move(t, member, large, apigen.Transition{From: apigen.TicketStateFiled, To: toDone, Note: ptr("done elsewhere")})
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	assert.Equal(t, []int{0, 0, 0}, stagesOf(*res.JSON200), "the child's own stages stay")
	assert.Equal(t, []int{100, 90, 85}, stagesOf(get(parent)), "a done child counts 100 in each stage: 600/6, 550/6, 500/6")

	res2 := e.patch(t, member, small, apigen.TicketPatch{ProgressRefinement: ptr(100), Progress: ptr(100), ProgressReview: ptr(100),
		Note: ptr("ran the tests")})
	require.Equal(t, http.StatusOK, res2.StatusCode(), string(res2.Body))
	got = get(parent)
	assert.Equal(t, []int{100, 100, 100}, stagesOf(got))
	assert.Equal(t, apigen.TicketStateFiled, got.State, "a parent is never done by its stages")

	res = e.move(t, member, got, apigen.Transition{From: apigen.TicketStateFiled, To: toDone, Note: ptr("all children done")})
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	assert.True(t, res.JSON200.DoneByHand)
	assert.Equal(t, []int{100, 100, 100}, stagesOf(*res.JSON200), "shown as the children make them")
	back := e.move(t, member, *res.JSON200, apigen.Transition{From: toDone, To: apigen.TicketStateFiled, Reason: ptr("one more child")})
	require.Equal(t, http.StatusOK, back.StatusCode(), string(back.Body))
	assert.Equal(t, apigen.TicketStateFiled, back.JSON200.State, "a parent returns, its stages full or not")

	dropped := e.file(t, member, "ALPHA", task("Dropped part", func(b *apigen.TicketCreate) {
		b.Effort, b.Parent = apigen.EffortM, ptr("ALPHA-"+strconv.Itoa(parent.Number))
	}))
	assert.Equal(t, []int{65, 65, 65}, stagesOf(get(parent)), "(1×100 + 5×100 + 3×0) / 9 = 66.7 → 65 in each stage")
	res = e.move(t, member, dropped, apigen.Transition{From: apigen.TicketStateFiled, To: apigen.TicketStateDropped, Reason: ptr("not needed")})
	require.Equal(t, http.StatusOK, res.StatusCode())
	assert.Equal(t, []int{100, 100, 100}, stagesOf(get(parent)), "a dropped child is left out")

	detached := e.patch(t, member, get(small), apigen.TicketPatch{Parent: nullable.NewNullNullable[string]()})
	require.Equal(t, http.StatusOK, detached.StatusCode(), string(detached.Body))
	assert.Equal(t, []int{100, 100, 100}, stagesOf(get(parent)), "the done child that is left counts 100")

	leaf := e.staged(t, member, e.file(t, member, "ALPHA", task("Closed leaf")), 100, 100, 95)
	closed := e.patch(t, member, leaf, apigen.TicketPatch{ProgressReview: ptr(100), Note: ptr("verified")})
	require.Equal(t, http.StatusOK, closed.StatusCode(), string(closed.Body))
	require.False(t, closed.JSON200.DoneByHand)
	e.file(t, member, "ALPHA", task("Late child", func(b *apigen.TicketCreate) { b.Parent = ptr("ALPHA-" + strconv.Itoa(leaf.Number)) }))
	got = get(leaf)
	assert.True(t, got.DoneByHand, "a done ticket that gains children is done by hand from then on")
	assert.Equal(t, []int{0, 0, 0}, stagesOf(got), "its stages are its child's")
	back = e.move(t, member, got, apigen.Transition{From: toDone, To: apigen.TicketStateFiled, Reason: ptr("the child is open")})
	require.Equal(t, http.StatusOK, back.StatusCode(), "and its way out is the withdrawal")
}

// docs/adr/0009 D5, docs/adr/0017 D3: a parent whose last child leaves takes
// the last derived values as its own, a done child at 100 in each, so an open
// ticket without children can hold three full stages. No write of the stages
// brings the last of them to 100 then: such a ticket is closed by hand, and
// withdrawing that done leaves it done by its full stages.
func TestAnOpenTicketWithFullStagesIsClosedByHand(t *testing.T) {
	e := newTicketEnv(t)
	member := caller{Token: e.tk.MemberA}
	parent := e.file(t, member, "ALPHA", task("Epic"))
	child := e.file(t, member, "ALPHA", task("Part", func(b *apigen.TicketCreate) { b.Parent = ptr("ALPHA-" + strconv.Itoa(parent.Number)) }))
	done := e.move(t, member, child, apigen.Transition{From: apigen.TicketStateFiled, To: toDone, Note: ptr("done elsewhere")})
	require.Equal(t, http.StatusOK, done.StatusCode(), string(done.Body))
	detached := e.patch(t, member, *done.JSON200, apigen.TicketPatch{Parent: nullable.NewNullNullable[string]()})
	require.Equal(t, http.StatusOK, detached.StatusCode(), string(detached.Body))
	tk := *e.get(t, member, "ALPHA", parent.Number).JSON200
	require.False(t, tk.ProgressDerived)
	require.Equal(t, []int{100, 100, 100}, stagesOf(tk), "the last derived values")
	assert.Equal(t, apigen.TicketStateFiled, tk.State, "full stages close nothing by themselves")

	res := e.patch(t, member, tk, apigen.TicketPatch{ProgressReview: ptr(100), Note: ptr("verified")})
	body := problemIn(t, http.StatusBadRequest, res.StatusCode(), res.Body, "validation_failed")
	assert.Equal(t, "/note", pointerOf(body), "a stage already full brings nothing to 100")
	closed := e.move(t, member, tk, apigen.Transition{From: apigen.TicketStateFiled, To: toDone, Note: ptr("verified")})
	require.Equal(t, http.StatusOK, closed.StatusCode(), string(closed.Body))
	assert.True(t, closed.JSON200.DoneByHand)
	stays := e.move(t, member, *closed.JSON200, apigen.Transition{From: toDone, To: apigen.TicketStateFiled, Reason: ptr("the stages say it")})
	require.Equal(t, http.StatusOK, stays.StatusCode(), string(stays.Body))
	assert.Equal(t, apigen.TicketStateDone, stays.JSON200.State)
	assert.False(t, stays.JSON200.DoneByHand, "done by its full stages")
}

// The release before the stages, its statements verbatim from its
// queries/write/tickets.sql with sqlc's named arguments made positional: what
// its image writes over this schema when an operator rolls back to it
// (docs/adr/0028 D4).
const (
	previousUpdateTicketFields = `UPDATE tickets
SET type = $1, title = $2, severity = $3,
    security = $4, threat = $5, effort = $6,
    parent_id = $7, assignee_id = $8,
    progress = $9, confidential = $10,
    version = version + 1, updated_at = now()
WHERE tenant_id = $11 AND id = $12 AND version = $13
RETURNING version`
	previousRefreshDerivedProgress = `WITH d AS (SELECT ticket_derived_progress($1, $2) AS v)
UPDATE tickets t
SET progress_derived = d.v,
    progress = CASE WHEN d.v IS NULL THEN coalesce(t.progress_derived, t.progress) ELSE t.progress END,
    updated_at = now()
FROM d
WHERE t.tenant_id = $1 AND t.id = $2 AND t.progress_derived IS DISTINCT FROM d.v
RETURNING t.parent_id`
	previousTransitionTicket = `UPDATE tickets
SET state = $1,
    blocked_from = $2, block_kind = $3,
    block_reason = $4, block_ticket_id = $5,
    block_external_ref = $6,
    rank = CASE WHEN $1::ticket_state IN ('done', 'dropped') THEN NULL
                ELSE coalesce($7::text, rank) END,
    progress = CASE WHEN $1::ticket_state = 'done' THEN 100 ELSE progress END,
    decided_at = CASE WHEN $1::ticket_state = 'decided' THEN now() ELSE decided_at END,
    done_at = CASE WHEN $1::ticket_state = 'done' THEN now()
                   WHEN $1::ticket_state = 'filed' THEN NULL
                   ELSE done_at END,
    version = version + 1, updated_at = now()
WHERE tenant_id = $8 AND id = $9 AND state = $10
RETURNING version`
)

// detachByThePreviousRelease takes a ticket without children from its parent
// the way the release before the stages does: its PATCH, then its derivation
// of the parent left behind.
func (e ticketEnv) detachByThePreviousRelease(t *testing.T, f *fixture.DB, child apigen.Ticket, parent uuid.UUID) {
	t.Helper()
	var version int
	var grandparent *uuid.UUID
	require.NoError(t, f.QueryRow(e.ctx, previousUpdateTicketFields, string(child.Type), child.Title, string(child.Severity),
		string(child.Security), nil, string(child.Effort), nil, nil, child.Progress, child.Confidential, e.A, child.Id, child.Version).
		Scan(&version))
	require.NoError(t, f.QueryRow(e.ctx, previousRefreshDerivedProgress, e.A, parent).Scan(&grandparent))
}

// docs/adr/0017 D3, docs/adr/0028 D4: when the release before the stages takes
// a parent's last child — in a rollback — it clears progress_derived and leaves
// the derived refinement and review as they were. This release shows and
// writes the ticket's own stages then, counts them in a new parent's, and
// takes a change that sends no stage for no done act.
func TestStagesAfterThePreviousReleaseDetachesTheLastChild(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	member := caller{Token: e.tk.MemberA}
	assisted := caller{Token: e.tk.AssistedAgentA, Agent: "claude-code/opus/s1"}
	get := func(tk apigen.Ticket) apigen.Ticket { return *e.get(t, member, "ALPHA", tk.Number).JSON200 }
	childOf := func(parent apigen.Ticket, title string) apigen.Ticket {
		return e.file(t, member, "ALPHA", task(title, func(b *apigen.TicketCreate) { b.Parent = ptr("ALPHA-" + strconv.Itoa(parent.Number)) }))
	}

	parent := e.file(t, member, "ALPHA", task("Epic"))
	child := e.staged(t, member, childOf(parent, "Part"), 80, 40, 20)
	require.Equal(t, []int{80, 40, 20}, stagesOf(get(parent)))
	e.detachByThePreviousRelease(t, f, child, parent.Id)
	got := get(parent)
	assert.False(t, got.ProgressDerived)
	assert.Equal(t, []int{0, 40, 0}, stagesOf(got), "its own stages, the implementation the last derived value")
	res := e.patch(t, member, got, apigen.TicketPatch{ProgressRefinement: ptr(10), ProgressReview: ptr(5)})
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	assert.Equal(t, []int{10, 40, 5}, stagesOf(*res.JSON200), "what is written is what it shows")
	grand := e.file(t, member, "ALPHA", task("Grand"))
	moved := e.patch(t, member, *res.JSON200, apigen.TicketPatch{Parent: nullable.NewNullableWithValue("ALPHA-" + strconv.Itoa(grand.Number))})
	require.Equal(t, http.StatusOK, moved.StatusCode(), string(moved.Body))
	assert.Equal(t, []int{10, 40, 5}, stagesOf(get(grand)), "a parent counts the stages its child shows")

	// Own stages full, the derived refinement and review the previous release
	// left short of full: a change of the title closes nothing.
	full := e.file(t, member, "ALPHA", task("Full"))
	done := e.move(t, member, childOf(full, "Done part"), apigen.Transition{From: apigen.TicketStateFiled, To: toDone, Note: ptr("done elsewhere")})
	require.Equal(t, http.StatusOK, done.StatusCode(), string(done.Body))
	detached := e.patch(t, member, *done.JSON200, apigen.TicketPatch{Parent: nullable.NewNullNullable[string]()})
	require.Equal(t, http.StatusOK, detached.StatusCode(), string(detached.Body))
	half := e.staged(t, member, childOf(full, "Half part"), 50, 100, 0)
	require.Equal(t, []int{50, 100, 0}, stagesOf(get(full)))
	e.detachByThePreviousRelease(t, f, half, full.Id)
	got = get(full)
	assert.Equal(t, []int{100, 100, 100}, stagesOf(got), "its own stages, full since its done child left")
	res = e.patch(t, member, got, apigen.TicketPatch{Title: ptr("Full, renamed")})
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	assert.Equal(t, apigen.TicketStateFiled, res.JSON200.State, "no done act")
	res = e.patch(t, assisted, *res.JSON200, apigen.TicketPatch{Title: ptr("Full, renamed again")})
	require.Equal(t, http.StatusOK, res.StatusCode(), "an agent without close changes the title: %s", res.Body)
}

// docs/adr/0009 D5, docs/adr/0028 D4: the release before the stages closes a
// ticket in a rollback by setting progress to 100 and leaving the other stages
// and done_by_hand as they were. Short of full, the ticket is done by hand:
// shown so, its stages free to change, withdrawn with a reason to
// in-progress, the one state that release closes from.
func TestDoneByThePreviousRelease(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	member := caller{Token: e.tk.MemberA}
	tk := e.walk(t, member, e.file(t, member, "ALPHA", task("Closed in a rollback")), toAnalysed, toDecided, toInProgress)
	e.file(t, member, "ALPHA", task("below"))
	var version int
	require.NoError(t, f.QueryRow(e.ctx, previousTransitionTicket, "done", nil, nil, nil, nil, nil, nil, e.A, tk.Id, "in-progress").Scan(&version))

	tk = *e.get(t, member, "ALPHA", tk.Number).JSON200
	assert.Equal(t, apigen.TicketStateDone, tk.State)
	assert.Equal(t, []int{0, 100, 0}, stagesOf(tk))
	assert.True(t, tk.DoneByHand, "short of full: done by hand")
	assert.Equal(t, apigen.TicketStateInProgress, tk.DoneFrom.MustGet())
	res := e.patch(t, member, tk, apigen.TicketPatch{Progress: ptr(90)})
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	assert.Equal(t, apigen.TicketStateDone, res.JSON200.State, "a lower stage reopens nothing done by hand")
	back := e.move(t, member, *res.JSON200, apigen.Transition{From: toDone, To: toInProgress, Reason: ptr("not verified")})
	require.Equal(t, http.StatusOK, back.StatusCode(), string(back.Body))
	assert.Equal(t, apigen.TicketStateInProgress, back.JSON200.State)
	assert.Equal(t, []string{"below", "Closed in a rollback"}, e.titles(t, member, e.projectTickets("ALPHA"), ""), "ranked at the bottom")
}

// docs/adr/0010 D3 as amended 2026-10-04: the horizon set on a ticket holds
// until a person or an agent sets another — across a block, an unblock, an
// open decision that blocks it and that link removed, none of which moves it;
// the reason is optional for a person and required of an agent.
func TestTheHorizonSetHolds(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	member := caller{Token: e.tk.MemberA}
	agent := caller{Token: e.tk.AgentA, Agent: "claude-code/opus/s1"}
	tk := e.walk(t, member, e.file(t, member, "ALPHA", task("Planned now")), toAnalysed, toDecided)
	decision := e.file(t, member, "ALPHA", task("Which queue?", func(b *apigen.TicketCreate) { b.Type = apigen.TicketTypeDecision }))
	set := func(c caller, tk apigen.Ticket, value apigen.Horizon, reason *string) *apigen.SetHorizonResponse {
		etag := strconv.Quote(strconv.Itoa(tk.Version))
		res, err := e.s.client(t, c).SetHorizonWithResponse(e.ctx, e.SlugA, "ALPHA", tk.Number,
			&apigen.SetHorizonParams{IfMatch: &etag}, apigen.HorizonUpdate{Value: value, Reason: reason})
		require.NoError(t, err)
		return res
	}

	byAgent := set(agent, tk, apigen.HorizonNow, nil)
	body := problemIn(t, http.StatusBadRequest, byAgent.StatusCode(), byAgent.Body, "validation_failed")
	assert.Equal(t, "/reason", pointerOf(body))
	res := set(member, tk, apigen.HorizonNow, nil)
	require.Equal(t, http.StatusOK, res.StatusCode(), "a person's drag between the groups needs no reason")
	tk = *res.JSON200
	assert.True(t, tk.HorizonSet.MustGet().Reason.IsNull())
	res = set(agent, tk, apigen.HorizonNext, ptr("the demo moved"))
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	res = set(member, *res.JSON200, apigen.HorizonNow, nil)
	require.Equal(t, http.StatusOK, res.StatusCode())
	tk = *res.JSON200

	steps := []struct {
		name string
		do   func() apigen.Ticket
	}{
		{"blocked on a decision", func() apigen.Ticket {
			res := e.move(t, member, tk, apigen.Transition{From: toDecided, To: apigen.TicketStateBlocked, Reason: ptr("the owner decides"),
				Block: &apigen.BlockSet{Kind: apigen.BlockKindDecision}})
			require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
			return *res.JSON200
		}},
		{"unblocked", func() apigen.Ticket {
			res := e.move(t, member, tk, apigen.Transition{From: apigen.TicketStateBlocked, To: toDecided})
			require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
			return *res.JSON200
		}},
		{"an open decision blocks it", func() apigen.Ticket {
			require.Equal(t, http.StatusCreated, e.link(t, member, decision, apigen.LinkTypeBlocks, tk).StatusCode)
			return *e.get(t, member, "ALPHA", tk.Number).JSON200
		}},
		{"the link removed", func() apigen.Ticket {
			require.Equal(t, http.StatusNoContent, e.s.do(t, member, http.MethodDelete, e.linkPath(decision, apigen.LinkTypeBlocks, tk), nil).StatusCode)
			return *e.get(t, member, "ALPHA", tk.Number).JSON200
		}},
	}
	for _, s := range steps {
		version := tk.Version
		tk = s.do()
		assert.Equal(t, apigen.HorizonNow, tk.Horizon, "%s: the horizon set holds", s.name)
		assert.Equal(t, apigen.HorizonNow, tk.HorizonSet.MustGet().Value, s.name)
		if s.name == "an open decision blocks it" || s.name == "the link removed" {
			assert.Equal(t, version, tk.Version, "%s: a link leaves the version", s.name)
		}
	}
	n, err := f.QueryCount(e.ctx, "SELECT count(*) FROM audit_events WHERE ticket_id = $1 AND action = 'overridden'", tk.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 3, n, "the three horizons set, and nothing else")
	n, err = f.QueryCount(e.ctx, "SELECT count(*) FROM tickets WHERE id = $1 AND urgency_derived = 'later' AND urgency_rule = 'v2:default'", tk.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n, "nothing derives anything but later")

	back := set(agent, tk, apigen.HorizonLater, ptr("the demo was cancelled"))
	require.Equal(t, http.StatusOK, back.StatusCode(), string(back.Body))
	assert.Equal(t, apigen.HorizonLater, back.JSON200.Horizon)
	assert.True(t, back.JSON200.HorizonSet.IsNull())
}

// docs/adr/0018 D1, docs/adr/0049 D1: done_after keeps the tickets done after
// a time — not at it, as opened_after and updated_after — the board's count of
// the last fourteen days; docs/adr/0019 D3: review takes a WIP limit.
func TestDoneAfterAndTheReviewLimit(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	member := caller{Token: e.tk.MemberA}
	recent := e.walk(t, member, e.file(t, member, "ALPHA", task("Recent")), toAnalysed, toDecided, toInProgress, toDone)
	old := e.walk(t, member, e.file(t, member, "ALPHA", task("Old")), toAnalysed, toDecided, toInProgress, toDone)
	require.NoError(t, f.Exec(e.ctx, "UPDATE tickets SET done_at = now() - interval '20 days' WHERE id = $1", old.Id))
	e.file(t, member, "ALPHA", task("Open"))
	since := url.QueryEscape(time.Now().Add(-14 * 24 * time.Hour).UTC().Format(time.RFC3339))
	assert.Equal(t, []string{"Recent"}, e.titles(t, member, e.projectTickets("ALPHA"), "state=done&done_after="+since))
	assert.Equal(t, []string{"Recent"}, e.titles(t, member, e.tenantTickets(), "include_terminal=true&done_after="+since))
	assert.Equal(t, []string{"Recent", "Old"}, e.titles(t, member, e.projectTickets("ALPHA"), "state=done"))
	exact := url.QueryEscape(recent.DoneAt.MustGet().UTC().Format(time.RFC3339Nano))
	assert.Empty(t, e.titles(t, member, e.projectTickets("ALPHA"), "state=done&done_after="+exact), "after, not at")
	before := url.QueryEscape(recent.DoneAt.MustGet().Add(-time.Microsecond).UTC().Format(time.RFC3339Nano))
	assert.Equal(t, []string{"Recent"}, e.titles(t, member, e.projectTickets("ALPHA"), "state=done&done_after="+before))
	body := assertProblem(t, e.s.do(t, member, http.MethodGet, e.projectTickets("ALPHA")+"?done_after=lately", nil), http.StatusBadRequest, "validation_failed")
	assert.Contains(t, fmt.Sprint(body["errors"]), "done_after")

	admin := e.s.client(t, caller{Token: e.tk.AdminA})
	got, err := admin.GetProjectWithResponse(e.ctx, e.SlugA, "ALPHA")
	require.NoError(t, err)
	etag := got.HTTPResponse.Header.Get("ETag")
	upd, err := admin.UpdateProjectWithResponse(e.ctx, e.SlugA, "ALPHA", &apigen.UpdateProjectParams{IfMatch: &etag},
		apigen.ProjectPatch{WipLimits: &apigen.WipLimits{Analysed: ptr(5), Review: ptr(2)}})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, upd.StatusCode(), string(upd.Body))
	assert.Equal(t, 2, *upd.JSON200.WipLimits.Review)
	assert.Equal(t, 5, *upd.JSON200.WipLimits.Analysed)
}
