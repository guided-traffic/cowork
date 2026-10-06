package api

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/problem"
	"github.com/guided-traffic/cowork/backend/internal/store"
)

func ptrStr(s string) *string { return &s }
func ptrBool(b bool) *bool    { return &b }

// The callers of the tests below: a person, an agent with every capability, an
// agent without close (the "assisted" set) — each acting as a member with
// write scope.
var (
	person   = auth.Principal{Scope: domain.ScopeWrite}
	agent    = auth.Principal{Scope: domain.ScopeWrite, Agent: "claude-code/opus/s1", Capabilities: auth.AllCapabilities}
	assisted = auth.Principal{Scope: domain.ScopeWrite, Agent: "claude-code/opus/s1",
		Capabilities: []string{auth.CapDrop, auth.CapSetHorizon, auth.CapInterest, auth.CapUpload}}
)

// in is a ticket in a state, read by a member.
func in(state domain.TicketState, edit ...func(*store.TicketRow)) ticketCtx {
	tc := ticketCtx{role: domain.RoleMember, row: store.TicketRow{ID: uuid.New(), State: state}}
	for _, e := range edit {
		e(&tc.row)
	}
	return tc
}

func doneBy(hand bool, from domain.TicketState) func(*store.TicketRow) {
	return func(r *store.TicketRow) { r.DoneByHand, r.DoneFrom = hand, &from }
}

func stages(refinement, implementation, review int16) func(*store.TicketRow) {
	return func(r *store.TicketRow) {
		r.ProgressRefinement, r.Progress, r.ProgressReview = refinement, implementation, review
	}
}

func children(refinement, implementation, review int16) func(*store.TicketRow) {
	return func(r *store.TicketRow) {
		r.ProgressRefinementDerived, r.ProgressDerived, r.ProgressReviewDerived = &refinement, &implementation, &review
	}
}

func assertProblem(t *testing.T, perr *problem.Error, code problem.Code, pointer string, name string) {
	t.Helper()
	require.NotNil(t, perr, name)
	assert.Equal(t, code, perr.Code, name)
	if pointer != "" {
		require.NotEmpty(t, perr.Errors, name)
		assert.Equal(t, pointer, perr.Errors[0].Pointer, name)
	}
}

// docs/adr/0009 D5, docs/adr/0043 D4: done by hand — a person from every open
// state, an agent with close from in-progress and review only; the note
// always; over open prerequisites a person's override with a reason, never an
// agent's.
func TestDoneByHand(t *testing.T) {
	note := ptrStr("go test ./... passed")
	done := func(from domain.TicketState) apigen.Transition {
		return apigen.Transition{From: apigen.TicketState(from), To: apigen.TicketStateDone, Note: note}
	}
	for _, from := range []domain.TicketState{domain.StateFiled, domain.StateAnalysed, domain.StateDecided,
		domain.StateInProgress, domain.StateReview, domain.StateBlocked} {
		mv, perr := checkTransition(person, in(from), done(from))
		require.Nil(t, perr, from)
		assert.Equal(t, domain.MoveDone, mv, "a person closes from %s", from)
	}
	for _, from := range []domain.TicketState{domain.StateInProgress, domain.StateReview} {
		_, perr := checkTransition(agent, in(from), done(from))
		assert.Nil(t, perr, "an agent with close closes from %s", from)
	}
	for _, from := range []domain.TicketState{domain.StateFiled, domain.StateAnalysed, domain.StateDecided, domain.StateBlocked} {
		_, perr := checkTransition(agent, in(from), done(from))
		assertProblem(t, perr, problem.AgentForbidden, "", string(from))
		assert.Contains(t, perr.Detail, "close covers in-progress and review", from)
	}
	_, perr := checkTransition(assisted, in(domain.StateReview), done(domain.StateReview))
	assertProblem(t, perr, problem.AgentForbidden, "", "an agent without close")
	assert.Equal(t, "missing capability: close", perr.Detail)

	noNote := done(domain.StateFiled)
	noNote.Note = nil
	_, perr = checkTransition(person, in(domain.StateFiled), noNote)
	assertProblem(t, perr, problem.ValidationFailed, "/note", "the note is required")

	override := done(domain.StateReview)
	override.OverridePrerequisites = ptrBool(true)
	_, perr = checkTransition(person, in(domain.StateReview), override)
	assertProblem(t, perr, problem.ValidationFailed, "/reason", "an override needs a reason")
	override.Reason = ptrStr("the prerequisite is moot")
	_, perr = checkTransition(person, in(domain.StateReview), override)
	assert.Nil(t, perr, "a person overrides with a reason")
	_, perr = checkTransition(agent, in(domain.StateReview), override)
	assertProblem(t, perr, problem.AgentForbidden, "", "an agent never overrides")
	assert.Equal(t, "hard-off: overriding the prerequisite refusal", perr.Detail)

	forward := apigen.Transition{From: apigen.TicketStateInProgress, To: apigen.TicketStateReview, OverridePrerequisites: ptrBool(true),
		Reason: ptrStr("x")}
	_, perr = checkTransition(person, in(domain.StateInProgress), forward)
	assertProblem(t, perr, problem.ValidationFailed, "/override_prerequisites", "only done is refused by prerequisites")
}

// docs/adr/0009 D3, D5: review sits between in-progress and done, and done is
// left by the withdrawal of a done by hand only — to the state it came from,
// with a reason; a ticket done by its stages leaves done only by a lower
// stage; the reopen to filed is dropped's alone.
func TestLeavingReviewAndDone(t *testing.T) {
	mv, perr := checkTransition(agent, in(domain.StateInProgress), apigen.Transition{From: apigen.TicketStateInProgress, To: apigen.TicketStateReview})
	require.Nil(t, perr)
	assert.Equal(t, domain.MoveForward, mv, "in-progress → review is the next step, in the agent baseline")
	back := apigen.Transition{From: apigen.TicketStateReview, To: apigen.TicketStateInProgress}
	_, perr = checkTransition(assisted, in(domain.StateReview), back)
	assertProblem(t, perr, problem.ValidationFailed, "/reason", "review → in-progress needs a reason")
	back.Reason = ptrStr("the check found work to do")
	mv, perr = checkTransition(assisted, in(domain.StateReview), back)
	require.Nil(t, perr)
	assert.Equal(t, domain.MoveBackward, mv)

	byHand := in(domain.StateDone, doneBy(true, domain.StateReview))
	withdraw := apigen.Transition{From: apigen.TicketStateDone, To: apigen.TicketStateReview}
	_, perr = checkTransition(person, byHand, withdraw)
	assertProblem(t, perr, problem.ValidationFailed, "/reason", "the withdrawal needs a reason")
	withdraw.Reason = ptrStr("closed too early")
	mv, perr = checkTransition(person, byHand, withdraw)
	require.Nil(t, perr)
	assert.Equal(t, domain.MoveWithdraw, mv)
	_, perr = checkTransition(person, byHand, apigen.Transition{From: apigen.TicketStateDone, To: apigen.TicketStateFiled, Reason: ptrStr("x")})
	assertProblem(t, perr, problem.StateConflict, "/to", "a done ticket returns to the state it was done from only")

	byStages := in(domain.StateDone, doneBy(false, domain.StateReview), stages(100, 100, 100))
	_, perr = checkTransition(person, byStages, withdraw)
	assertProblem(t, perr, problem.StateConflict, "/to", "done by its stages")
	assert.Contains(t, perr.Detail, "lower a stage")

	parent := in(domain.StateDone, doneBy(false, domain.StateDecided), children(100, 100, 100))
	mv, perr = checkTransition(person, parent, apigen.Transition{From: apigen.TicketStateDone, To: apigen.TicketStateDecided, Reason: ptrStr("x")})
	require.Nil(t, perr, "a parent is done by hand, whatever its flag says")
	assert.Equal(t, domain.MoveWithdraw, mv)

	legacy := in(domain.StateDone, func(r *store.TicketRow) { r.DoneByHand = true })
	assert.Equal(t, domain.StateInProgress, origin(legacy.row), "a ticket closed before the stages was done from in-progress")

	dropped := in(domain.StateDropped)
	mv, perr = checkTransition(person, dropped, apigen.Transition{From: apigen.TicketStateDropped, To: apigen.TicketStateFiled, Reason: ptrStr("x")})
	require.Nil(t, perr)
	assert.Equal(t, domain.MoveReopen, mv)
}

// docs/adr/0009 D2, D5: done from blocked keeps the block, and the way back to
// blocked takes it back; every other move but a block clears it.
func TestKeepsBlock(t *testing.T) {
	assert.True(t, keepsBlock(domain.StateBlocked, domain.StateDone))
	assert.True(t, keepsBlock(domain.StateDone, domain.StateBlocked))
	assert.False(t, keepsBlock(domain.StateBlocked, domain.StateDecided), "unblocked")
	assert.False(t, keepsBlock(domain.StateBlocked, domain.StateDropped))
	assert.False(t, keepsBlock(domain.StateDone, domain.StateReview))
}

// docs/adr/0017 D2, D3: a stage takes 0 to 100 in steps of five in every
// state but dropped, and none on a ticket with children; the write's effect on
// the state is decided over the stages the ticket shows.
func TestApplyStages(t *testing.T) {
	v := func(n int) *int { return &n }
	var ch ticketChange
	perr := applyStages(in(domain.StateDropped).row, apigen.TicketPatch{ProgressReview: v(50)}, &ch)
	assertProblem(t, perr, problem.StateConflict, "/progress_review", "dropped")

	parent := in(domain.StateReview, children(100, 60, 0)).row
	perr = applyStages(parent, apigen.TicketPatch{ProgressRefinement: v(50)}, &ch)
	assertProblem(t, perr, problem.StateConflict, "/progress_refinement", "derived")
	assert.Equal(t, 100, perr.Errors[0].Current, "the derived value is the current one")

	perr = applyStages(in(domain.StateReview).row, apigen.TicketPatch{Progress: v(42)}, &ch)
	assertProblem(t, perr, problem.ValidationFailed, "/progress", "steps of five")

	for name, c := range map[string]struct {
		row   store.TicketRow
		patch apigen.TicketPatch
		want  domain.StageEffect
	}{
		"the last stage filled": {in(domain.StateReview, stages(100, 100, 95)).row, apigen.TicketPatch{ProgressReview: v(100)},
			domain.StagesComplete},
		"a stage short": {in(domain.StateReview, stages(100, 95, 95)).row, apigen.TicketPatch{ProgressReview: v(100)}, domain.StagesKeep},
		"lowered on done by the stages": {in(domain.StateDone, doneBy(false, domain.StateReview), stages(100, 100, 100)).row,
			apigen.TicketPatch{Progress: v(90)}, domain.StagesReopen},
		"lowered on done by hand": {in(domain.StateDone, doneBy(true, domain.StateDecided), stages(100, 40, 0)).row,
			apigen.TicketPatch{Progress: v(20)}, domain.StagesKeep},
		"raised on done by hand": {in(domain.StateDone, doneBy(true, domain.StateDecided), stages(100, 40, 0)).row,
			apigen.TicketPatch{Progress: v(100), ProgressReview: v(100)}, domain.StagesKeep},
	} {
		ch := ticketChange{}
		ch.params.ProgressRefinement, ch.params.Progress, ch.params.ProgressReview = c.row.ProgressRefinement, c.row.Progress, c.row.ProgressReview
		require.Nil(t, applyStages(c.row, c.patch, &ch), name)
		assert.Equal(t, c.want, ch.effect, name)
	}
}

// docs/adr/0017 D3, docs/adr/0028 D4: a ticket without children shows and
// writes its own stages, whatever the derived refinement and review hold — the
// previous release, run over this schema in a rollback, clears progress_derived
// alone when a parent's last child leaves — so a change that sends no stage
// closes nothing.
func TestStagesWithoutChildrenAreTheTicketsOwn(t *testing.T) {
	left := func(refinement, review int16) func(*store.TicketRow) {
		return func(r *store.TicketRow) { r.ProgressRefinementDerived, r.ProgressReviewDerived = &refinement, &review }
	}
	detached := in(domain.StateInProgress, stages(10, 40, 5), left(80, 20)).row
	assert.False(t, hasChildren(detached))
	assert.Equal(t, domain.Stages{Refinement: 10, Implementation: 40, Review: 5}, stagesOf(detached))
	v := ticketView(tenantScope{ID: uuid.New(), Slug: "acme"}, detached, time.Time{})
	assert.Equal(t, []int{10, 40, 5}, []int{v.ProgressRefinement, v.Progress, v.ProgressReview})

	full := in(domain.StateReview, stages(100, 100, 100), left(50, 0)).row
	ch := ticketChange{}
	ch.params.ProgressRefinement, ch.params.Progress, ch.params.ProgressReview = 100, 100, 100
	require.Nil(t, applyStages(full, apigen.TicketPatch{}, &ch))
	assert.Equal(t, domain.StagesKeep, ch.effect, "a change of the title is no done act")
}

// docs/adr/0009 D5, docs/adr/0028 D4: a done ticket is done by its stages only
// without children and with the three stages full. The previous release leaves
// done tickets that are neither and carry no done by hand — its own done in a
// rollback (progress 100, the other stages as they were), a parent it closed
// whose last child left (the last derived progress as its own), a closed
// ticket that gained children. Each is done by hand: shown so, withdrawn with
// a reason, its stages free to change without reopening it.
func TestDoneByHandWhateverTheFlagSays(t *testing.T) {
	ts := tenantScope{ID: uuid.New(), Slug: "acme"}
	withdraw := apigen.Transition{From: apigen.TicketStateDone, To: apigen.TicketStateInProgress, Reason: ptrStr("not verified")}
	for name, tc := range map[string]ticketCtx{
		"closed by the previous release":      in(domain.StateDone, stages(0, 100, 0)),
		"its last child left after it closed": in(domain.StateDone, stages(100, 40, 100)),
		"a parent":                            in(domain.StateDone, stages(100, 100, 100), children(80, 60, 40)),
	} {
		assert.True(t, ticketView(ts, tc.row, time.Time{}).DoneByHand, name)
		mv, perr := checkTransition(person, tc, withdraw)
		require.Nil(t, perr, name)
		assert.Equal(t, domain.MoveWithdraw, mv, name)
	}
	assert.False(t, ticketView(ts, in(domain.StateDone, stages(100, 100, 100)).row, time.Time{}).DoneByHand, "full and without children: by its stages")

	lower := 35
	ch := ticketChange{}
	ch.params.ProgressRefinement, ch.params.Progress, ch.params.ProgressReview = 100, 40, 100
	require.Nil(t, applyStages(in(domain.StateDone, stages(100, 40, 100)).row, apigen.TicketPatch{Progress: &lower}, &ch))
	assert.Equal(t, domain.StagesKeep, ch.effect, "a lower stage reopens nothing done by hand")
}

// docs/adr/0009 D5, docs/adr/0017 D4, docs/adr/0043 D4: the write that fills
// the last stage needs the note, of an agent close and in-progress or review,
// and an override only from a person with a reason; the write that lowers a
// stage of a ticket done by its stages needs a reason; a write that moves no
// state takes neither note nor override nor reason.
func TestStageInputs(t *testing.T) {
	note, reason := ptrStr("make test-integration passed"), ptrStr("moved by mistake")
	complete := domain.StagesComplete

	perr := stageInputs(person, in(domain.StateFiled), complete, apigen.TicketPatch{})
	assertProblem(t, perr, problem.ValidationFailed, "/note", "the note is missing")
	assert.Contains(t, perr.Detail, "completes the ticket")
	assert.Nil(t, stageInputs(person, in(domain.StateFiled), complete, apigen.TicketPatch{Note: note}), "a person from filed")
	assert.Nil(t, stageInputs(agent, in(domain.StateReview), complete, apigen.TicketPatch{Note: note}), "an agent with close from review")
	assert.Nil(t, stageInputs(agent, in(domain.StateInProgress), complete, apigen.TicketPatch{Note: note}), "from in-progress")

	perr = stageInputs(assisted, in(domain.StateReview), complete, apigen.TicketPatch{Note: note})
	assertProblem(t, perr, problem.AgentForbidden, "", "an agent without close")
	assert.Equal(t, "missing capability: close", perr.Detail)
	perr = stageInputs(agent, in(domain.StateDecided), complete, apigen.TicketPatch{Note: note})
	assertProblem(t, perr, problem.AgentForbidden, "", "an agent from a state before in-progress")
	assert.Contains(t, perr.Detail, "close covers in-progress and review")

	override := apigen.TicketPatch{Note: note, OverridePrerequisites: ptrBool(true)}
	perr = stageInputs(person, in(domain.StateReview), complete, override)
	assertProblem(t, perr, problem.ValidationFailed, "/reason", "an override needs a reason")
	override.Reason = reason
	assert.Nil(t, stageInputs(person, in(domain.StateReview), complete, override))
	perr = stageInputs(agent, in(domain.StateReview), complete, override)
	assertProblem(t, perr, problem.AgentForbidden, "", "an agent never overrides")

	reopen := domain.StagesReopen
	perr = stageInputs(person, in(domain.StateDone), reopen, apigen.TicketPatch{})
	assertProblem(t, perr, problem.ValidationFailed, "/reason", "the reopen needs a reason")
	assert.Contains(t, perr.Detail, "reopens it")
	assert.Nil(t, stageInputs(assisted, in(domain.StateDone), reopen, apigen.TicketPatch{Reason: reason}),
		"lowering a stage is no close: an agent reopens with a reason")
	perr = stageInputs(person, in(domain.StateDone), reopen, apigen.TicketPatch{Reason: reason, OverridePrerequisites: ptrBool(true)})
	assertProblem(t, perr, problem.ValidationFailed, "/override_prerequisites", "nothing to override on a reopen")
	perr = stageInputs(person, in(domain.StateDone), reopen, apigen.TicketPatch{Reason: reason, Note: note})
	assertProblem(t, perr, problem.ValidationFailed, "/note", "a reopen takes no note")

	keep := domain.StagesKeep
	for name, c := range map[string]struct {
		patch   apigen.TicketPatch
		pointer string
	}{
		"a note":      {apigen.TicketPatch{Note: note}, "/note"},
		"an override": {apigen.TicketPatch{OverridePrerequisites: ptrBool(true), Reason: reason}, "/override_prerequisites"},
		"a reason":    {apigen.TicketPatch{Reason: reason}, "/reason"},
	} {
		assertProblem(t, stageInputs(person, in(domain.StateReview), keep, c.patch), problem.ValidationFailed, c.pointer, name)
	}
	assert.Nil(t, stageInputs(person, in(domain.StateReview), keep, apigen.TicketPatch{OverridePrerequisites: ptrBool(false)}))
}

// docs/adr/0010 D3, docs/adr/0043 D4: the reason of a horizon set is optional
// for a person and required of an agent, which needs set-horizon — for later
// as well.
func TestHorizonInputs(t *testing.T) {
	now := domain.UrgencyNow
	set := func(reason *string) horizonWrite { return horizonWrite{value: &now, reason: reason} }
	assert.Nil(t, horizonInputs(person, domain.RoleMember, set(nil)), "a person's drag between the groups")
	assert.Nil(t, horizonInputs(agent, domain.RoleMember, set(ptrStr("a customer is down"))))
	assertProblem(t, horizonInputs(agent, domain.RoleMember, set(nil)), problem.ValidationFailed, "/reason", "an agent without a reason")
	assertProblem(t, horizonInputs(agent, domain.RoleMember, set(ptrStr("  "))), problem.ValidationFailed, "/reason", "a blank reason")
	assertProblem(t, horizonInputs(agent, domain.RoleMember, horizonWrite{}), problem.ValidationFailed, "/reason",
		"an agent's later")
	assert.Nil(t, horizonInputs(person, domain.RoleMember, horizonWrite{}), "a person's later")
	perr := horizonInputs(auth.Principal{Scope: domain.ScopeWrite, Agent: "a/b/c", Capabilities: []string{auth.CapClose}},
		domain.RoleMember, set(ptrStr("x")))
	assertProblem(t, perr, problem.AgentForbidden, "", "an agent without set-horizon")
	assertProblem(t, horizonInputs(person, domain.RoleViewer, set(nil)), problem.Forbidden, "", "a viewer")
}

// docs/adr/0017 D2, D3, D5, docs/adr/0009 D5: the view shows the stages as
// they are — a done ticket's included — derived on a parent; done_from and
// done_by_hand while done; an override without a reason as null.
func TestTicketViewShowsTheStages(t *testing.T) {
	ts := tenantScope{ID: uuid.New(), Slug: "acme"}
	leaf := in(domain.StateDone, doneBy(true, domain.StateBlocked), stages(100, 40, 0)).row
	v := ticketView(ts, leaf, time.Time{})
	assert.Equal(t, []int{100, 40, 0}, []int{v.ProgressRefinement, v.Progress, v.ProgressReview}, "done by hand leaves the stages")
	assert.True(t, v.DoneByHand)
	assert.Equal(t, apigen.TicketStateBlocked, v.DoneFrom.MustGet())
	assert.False(t, v.ProgressDerived)

	parent := in(domain.StateReview, stages(0, 0, 0), children(100, 55, 10)).row
	v = ticketView(ts, parent, time.Time{})
	assert.Equal(t, []int{100, 55, 10}, []int{v.ProgressRefinement, v.Progress, v.ProgressReview})
	assert.True(t, v.ProgressDerived)
	assert.True(t, v.DoneFrom.IsNull(), "done_from only while done")
	assert.False(t, v.DoneByHand)

	now := domain.UrgencyNow
	at := leaf.CreatedAt
	leaf.UrgencyOverride, leaf.UrgencyOverrideAt, leaf.OpenPrerequisites = &now, &at, 2
	v = ticketView(ts, leaf, time.Time{})
	assert.True(t, v.HorizonSet.MustGet().Reason.IsNull(), "a horizon set without a reason")
	assert.Equal(t, 2, v.OpenPrerequisites)
}

// docs/adr/0010 D1, D3: the view answers the horizon and the horizon set.
func TestTicketViewAnswersTheHorizon(t *testing.T) {
	ts := tenantScope{ID: uuid.New(), Slug: "acme"}
	row := in(domain.StateFiled).row
	row.UrgencyDerived, row.UrgencyRule = domain.UrgencyDefault, domain.UrgencyRuleDefault
	v := ticketView(ts, row, time.Time{})
	assert.Equal(t, apigen.Horizon("later"), v.Horizon, "a ticket nobody placed stands in later")
	assert.True(t, v.HorizonSet.IsNull())

	next, by, reason := domain.UrgencyNext, uuid.New(), "after the import"
	at := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	row.UrgencyOverride, row.UrgencyOverrideAt, row.UrgencyOverrideBy, row.UrgencyOverrideReason = &next, &at, &by, &reason
	v = ticketView(ts, row, time.Time{})
	assert.Equal(t, apigen.Horizon("next"), v.Horizon)
	set := v.HorizonSet.MustGet()
	assert.Equal(t, apigen.Horizon("next"), set.Value)
	assert.Equal(t, reason, set.Reason.MustGet())
	assert.Equal(t, by, set.By.MustGet().Id)
	assert.Equal(t, at, set.At)
}
