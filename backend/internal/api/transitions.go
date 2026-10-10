package api

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/problem"
	"github.com/guided-traffic/cowork/backend/internal/store"
	"github.com/guided-traffic/cowork/backend/internal/store/readq"
	"github.com/guided-traffic/cowork/backend/internal/store/writeq"
)

const (
	actionTransitioned = "transitioned"
	fieldState         = "state"
	fieldDoneByHand    = "done_by_hand"
	// pointerTo is the transition's target in a refusal.
	pointerTo = "/to"
)

// TransitionTicket moves a ticket between states (docs/adr/0009). The state
// the request names must be the current one (docs/adr/0045 D2).
func (s *Server) TransitionTicket(ctx context.Context, req apigen.TransitionTicketRequestObject) (apigen.TransitionTicketResponseObject, error) {
	t := tenantFrom(ctx)
	body := *req.Body
	var out shown
	_, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		tc, err := visibleTicket(ctx, w.Reader, t, req.Project, req.Number)
		if err != nil {
			return err
		}
		mv, perr := checkTransition(principal(ctx), tc, body)
		if perr != nil {
			return perr
		}
		ev := store.Event{EntityType: entityTicket, EntityID: tc.row.ID, TicketID: tc.row.ID, TicketKey: ticketKey(t, tc.row),
			Reason: deref(body.Reason), Note: deref(body.Note)}
		if req.Params.IdempotencyKey != nil {
			ev.IdempotencyKey = *req.Params.IdempotencyKey
		}
		var row store.TicketRow
		if mv == domain.MoveWithdraw && domain.WithdrawalStaysDone(hasChildren(tc.row), stagesOf(tc.row)) {
			row, err = keepDoneByStages(ctx, w, t, tc, body.Comment, ev)
		} else {
			row, err = transition(ctx, w, t, tc, mv, body, ev)
		}
		out, err = showing(ctx, w.Reader, row, err)
		return err
	})
	if err != nil {
		return nil, err
	}
	return apigen.TransitionTicket200JSONResponse{Body: ticketView(t, out, s.h.opts.Now()), Headers: apigen.TransitionTicket200ResponseHeaders{ETag: etag(out.row.Version)}}, nil
}

// transition writes a move of the matrix with its act. A block that names the
// ticket it waits on adds a blocks link, so it takes the installation's lock
// of the blocks graph before anything else — before the ticket row is written
// and the walk runs (docs/developer/data-access.md#advisory-locks).
func transition(ctx context.Context, w *store.Writer, t tenantScope, tc ticketCtx, mv domain.Move, body apigen.Transition, ev store.Event) (store.TicketRow, error) {
	if mv == domain.MoveBlock && body.Block != nil && body.Block.Ticket != nil {
		if err := w.LockGraph(ctx, store.GraphBlocks); err != nil {
			return store.TicketRow{}, err
		}
	}
	rank, err := reopenRank(ctx, w, t, tc, mv)
	if err != nil {
		return store.TicketRow{}, err
	}
	if ev.ExplainedBy, err = explain(ctx, w, t, tc, body.Comment); err != nil {
		return store.TicketRow{}, err
	}
	from, to := domain.TicketState(body.From), domain.TicketState(body.To)
	after := map[string]any{fieldState: string(to)}
	if mv == domain.MoveDone {
		after[fieldDoneByHand] = true
		if err := closeOver(ctx, w.Reader, t, tc, overrides(body.OverridePrerequisites), pointerTo, after, &ev); err != nil {
			return store.TicketRow{}, err
		}
	}
	c := stateChange{from: from, to: to, rank: rank, doneByHand: mv == domain.MoveDone, bump: true}
	if mv == domain.MoveBlock {
		c.block, c.reason = body.Block, body.Reason
	}
	waitsOn, err := move(ctx, w, t, tc, c, after)
	if err != nil {
		return store.TicketRow{}, err
	}
	if waitsOn != uuid.Nil {
		ev.Refs = append(ev.Refs, waitsOn)
	}
	ev.Action, ev.Before, ev.After = actionTransitioned, map[string]any{fieldState: string(from)}, after
	ev.Notices = stateNotices(to)
	w.Record(ev)
	if err := tellBlockedElsewhere(ctx, w, tc.row, to); err != nil {
		return store.TicketRow{}, err
	}
	return reread(ctx, w, t, tc.row.ID)
}

// actionPrerequisiteSettled is the act recorded on a blocked ticket of
// another team when its prerequisite reaches done or dropped
// (docs/adr/0012 D5 as made concrete 2026-10-10).
const actionPrerequisiteSettled = "prerequisite_settled"

// tellBlockedElsewhere tells the watchers of the tickets of other teams that
// a ticket reaching done or dropped blocks, as its own team's watchers are
// told (docs/adr/0012 D5, docs/adr/0020 D2): an act on each blocked ticket in
// its own team's record that names the prerequisite in its refs alone — no
// head of it stays in that record, so a later confidential flag or a purge
// leaves nothing of it behind —, whose notices reach that ticket's watchers
// who see it. The closer who holds no role in that team is no actor there
// (RecordElsewhere). Nothing for any other state.
func tellBlockedElsewhere(ctx context.Context, w *store.Writer, row store.TicketRow, to domain.TicketState) error {
	if !to.Terminal() {
		return nil
	}
	far, err := w.RelationsElsewhere(ctx, row.ID)
	if err != nil {
		return err
	}
	for _, f := range far {
		if f.Kind != store.RelationLink || f.LinkType == nil || *f.LinkType != domain.LinkBlocks || f.Outgoing == nil || !*f.Outgoing {
			continue
		}
		if err := w.RecordElsewhere(ctx, f.Far, store.Event{EntityType: entityTicket, Action: actionPrerequisiteSettled,
			Refs: []uuid.UUID{row.ID}, Notices: []store.Notice{{Reason: store.NoticeBlockerClosed, Watchers: true}}}); err != nil {
			return err
		}
	}
	return nil
}

// stateNotices are whom a state change tells (docs/adr/0020 D2): the watchers
// of the ticket — a block's reason comes only with a move into blocked —, and
// when it reaches done or dropped the watchers of every ticket it blocks.
func stateNotices(to domain.TicketState) []store.Notice {
	n := []store.Notice{{Reason: store.NoticeStateChanged, Watchers: true}}
	if to.Terminal() {
		n = append(n, store.Notice{Reason: store.NoticeBlockerClosed, Blocked: true})
	}
	return n
}

// keepDoneByStages withdraws a done by hand from a ticket without children
// whose three stages are full: it stays done, by its stages, and the act is
// the change of done_by_hand (docs/adr/0009 D5).
func keepDoneByStages(ctx context.Context, w *store.Writer, t tenantScope, tc ticketCtx, comment *string, ev store.Event) (store.TicketRow, error) {
	var err error
	if ev.ExplainedBy, err = explain(ctx, w, t, tc, comment); err != nil {
		return store.TicketRow{}, err
	}
	if _, err := w.EndDoneByHand(ctx, writeq.EndDoneByHandParams{TenantID: t.ID, ID: tc.row.ID}); errors.Is(err, pgx.ErrNoRows) {
		return store.TicketRow{}, problem.New(problem.StateConflict, "the ticket's state changed meanwhile; read it again")
	} else if err != nil {
		return store.TicketRow{}, err
	}
	ev.Action, ev.Before, ev.After = actionUpdated, map[string]any{fieldDoneByHand: true}, map[string]any{fieldDoneByHand: false}
	w.Record(ev)
	return reread(ctx, w, t, tc.row.ID)
}

// checkTransition holds a transition to the matrix (docs/adr/0009 D2–D5), to
// its required inputs and to the agent's capabilities (docs/adr/0043 D2–D4).
func checkTransition(p auth.Principal, tc ticketCtx, body apigen.Transition) (domain.Move, *problem.Error) {
	from, to := domain.TicketState(body.From), domain.TicketState(body.To)
	if from != tc.row.State {
		return 0, &problem.Error{Code: problem.StateConflict, Detail: "the ticket is " + string(tc.row.State),
			Errors: []problem.FieldError{{Pointer: "/from", Message: "not the current state", Current: string(tc.row.State)}}}
	}
	if from == domain.StateDone && !doneByHand(tc.row) {
		return 0, &problem.Error{Code: problem.StateConflict,
			Detail: "the ticket is done by its progress stages; lower a stage to reopen it",
			Errors: []problem.FieldError{{Pointer: pointerTo, Message: "done by its stages", Current: string(tc.row.State)}}}
	}
	mv := domain.ClassifyMove(from, to, origin(tc.row))
	if mv == domain.MoveInvalid {
		return 0, &problem.Error{Code: problem.StateConflict, Detail: "no transition leads from " + string(from) + " to " + string(to),
			Errors: []problem.FieldError{{Pointer: pointerTo, Message: "not reachable from " + string(from), Current: string(tc.row.State)}}}
	}
	need := work
	switch {
	case mv == domain.MoveForward && to == domain.StateDecided:
		need.Capability = auth.CapDecide
	case mv == domain.MoveDrop:
		need.Capability = auth.CapDrop
	}
	if perr := auth.Authorize(p, tc.role, need); perr != nil {
		return 0, perr
	}
	override := overrides(body.OverridePrerequisites)
	if mv == domain.MoveDone {
		if perr := mayClose(p, tc.role, from, override); perr != nil {
			return 0, perr
		}
	} else if override {
		return 0, problem.Field("/override_prerequisites", "only done is refused by prerequisites")
	}
	return mv, transitionInputs(mv, body, override)
}

// origin is the state a move out of blocked or done may return to: where the
// block came from, or the state the ticket was done from — in-progress for a
// ticket a release before the stages closed, the one way it had.
func origin(r store.TicketRow) domain.TicketState {
	switch {
	case r.State == domain.StateBlocked && r.BlockedFrom != nil:
		return *r.BlockedFrom
	case r.State == domain.StateDone && r.DoneFrom != nil:
		return *r.DoneFrom
	case r.State == domain.StateDone:
		return domain.StateInProgress
	}
	return ""
}

// mayClose holds the done act — by hand or by the stages — to the agent
// rules: close, from in-progress or review only, and never over open
// prerequisites (docs/adr/0043 D3, D4).
func mayClose(p auth.Principal, role domain.Role, from domain.TicketState, override bool) *problem.Error {
	if perr := auth.Authorize(p, role, auth.Need{Role: domain.RoleMember, Scope: domain.ScopeWrite, Capability: auth.CapClose}); perr != nil {
		return perr
	}
	if p.IsAgent() && !from.AgentCloses() {
		return problem.New(problem.AgentForbidden, "close covers in-progress and review: an agent does not close a "+string(from)+" ticket")
	}
	if override {
		return auth.Authorize(p, role, auth.Need{Role: domain.RoleMember, Scope: domain.ScopeWrite, HardOff: auth.HardOffPrereqOverride})
	}
	return nil
}

func overrides(o *bool) bool { return o != nil && *o }

// transitionInputs requires what the move requires and refuses what it would
// ignore.
func transitionInputs(mv domain.Move, body apigen.Transition, override bool) *problem.Error {
	switch {
	case (mv.NeedsReason() || override) && blank(body.Reason):
		return problem.Field("/reason", "this transition needs a reason")
	case mv == domain.MoveDone && blank(body.Note):
		return problem.Field("/note", "done needs a verification note: what was run, against what, with what result")
	}
	return blockInputs(mv, body.Block)
}

// blockInputs requires the block entering blocked, and only there
// (docs/adr/0009 D2).
func blockInputs(mv domain.Move, b *apigen.BlockSet) *problem.Error {
	switch {
	case mv == domain.MoveBlock && b == nil:
		return problem.Field("/block", "entering blocked needs the block: its kind, and the ticket or the reference it waits on")
	case mv != domain.MoveBlock && b != nil:
		return problem.Field("/block", "a block goes with to: blocked only")
	case b != nil && b.Kind == apigen.BlockKindTicket && blank(b.Ticket):
		return problem.Field("/block/ticket", "a block of kind ticket names the ticket")
	}
	return nil
}

func blank(s *string) bool { return s == nil || strings.TrimSpace(*s) == "" }

// closeOver refuses the done act over open direct blocks sources whose state
// the caller reads in a head — of any team, a project restricted from them
// included, never a placeholder — unless the person overrides; the act then
// names the keys overridden (docs/adr/0012 D7 as amended 2026-10-10). pointer
// is the request's field that closes.
func closeOver(ctx context.Context, r *store.Reader, _ tenantScope, tc ticketCtx, override bool, pointer string, after map[string]any, ev *store.Event) error {
	open, err := r.OpenPrerequisiteHeads(ctx, tc.row.ID)
	if err != nil || len(open) == 0 {
		return err
	}
	keys := make([]string, 0, len(open))
	errs := make([]problem.FieldError, 0, len(open))
	for _, o := range open {
		key := o.Head.Key()
		keys = append(keys, key)
		errs = append(errs, problem.FieldError{Pointer: pointer, Message: "open prerequisite " + key + ": " + o.Head.Title,
			Current: string(o.Head.State)})
	}
	if !override {
		return &problem.Error{Code: problem.OpenPrerequisites,
			Detail: "tickets that block this one are open; settle them, or override with a reason", Errors: errs}
	}
	after["overridden_prerequisites"] = keys
	for _, o := range open {
		ev.Refs = append(ev.Refs, o.ID)
	}
	return nil
}

// reopenRank is the key a ticket that returns from done or dropped to an open
// state joins its project's rank with, at the bottom, taken before the
// transition writes anything; nil for every other move, which keeps the rank
// or — into done or dropped — takes it away (docs/adr/0014 D1).
func reopenRank(ctx context.Context, w *store.Writer, t tenantScope, tc ticketCtx, mv domain.Move) (*string, error) {
	if mv != domain.MoveReopen && mv != domain.MoveWithdraw {
		return nil, nil
	}
	key, err := rankAtBottom(ctx, w, t, tc.project.ID)
	if err != nil {
		return nil, err
	}
	return &key, nil
}

// stateChange is a move to write: where it goes, the key of a ticket that
// returns to an open state, the block entering blocked with its text, whether
// done is set by hand, and whether the write raises the version — not where
// the request raised it already.
type stateChange struct {
	from, to   domain.TicketState
	rank       *string
	block      *apigen.BlockSet
	reason     *string
	doneByHand bool
	bump       bool
}

// move writes the state and its effects: the rank a reopen brings, the block
// and the link it names, and the stages of its parent (docs/adr/0017 D3). The
// horizon stays: a state never moves a ticket to another (docs/adr/0010 D3).
// It returns the ticket a block waits on, uuid.Nil without one.
func move(ctx context.Context, w *store.Writer, t tenantScope, tc ticketCtx, c stateChange, after map[string]any) (uuid.UUID, error) {
	waits, err := writeState(ctx, w, t, tc, c, after)
	if err != nil {
		return uuid.Nil, err
	}
	return waits, refreshProgress(ctx, w, tc.row.ParentID)
}

// writeState writes the state change: a block entering blocked, with the link
// to the ticket it waits on; the block a ticket done from blocked keeps, and
// takes back when it returns there (docs/adr/0009 D2, D5); none otherwise.
func writeState(ctx context.Context, w *store.Writer, t tenantScope, tc ticketCtx, c stateChange, after map[string]any) (uuid.UUID, error) {
	params := writeq.TransitionTicketParams{TenantID: t.ID, ID: tc.row.ID, FromState: c.from, ToState: c.to, Rank: c.rank,
		DoneByHand: c.doneByHand, Bump: c.bump}
	var waitsOn *ticketCtx
	switch {
	case c.block != nil:
		var err error
		if waitsOn, err = blockTicket(ctx, w.Reader, t, tc, c.block); err != nil {
			return uuid.Nil, err
		}
		kind := domain.BlockKind(c.block.Kind)
		params.BlockedFrom, params.BlockKind, params.BlockReason, params.BlockExternalRef = &c.from, &kind, c.reason, c.block.ExternalRef
		block := map[string]any{"kind": string(kind)}
		if waitsOn != nil {
			params.BlockTicketID = &waitsOn.row.ID
			block["ticket"] = ticketKey(t, waitsOn.row)
		}
		after["block"] = block
	case keepsBlock(c.from, c.to):
		r := tc.row
		params.BlockedFrom, params.BlockKind, params.BlockReason = r.BlockedFrom, r.BlockKind, r.BlockReason
		params.BlockTicketID, params.BlockExternalRef = r.BlockTicketID, r.BlockExternalRef
	}
	if _, err := w.TransitionTicket(ctx, params); errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, problem.New(problem.StateConflict, "the ticket's state changed meanwhile; read it again")
	} else if err != nil {
		return uuid.Nil, err
	}
	if waitsOn == nil {
		return uuid.Nil, nil
	}
	if err := linkWaitsOn(ctx, w, t, tc, *waitsOn); err != nil {
		return uuid.Nil, err
	}
	return waitsOn.row.ID, nil
}

// keepsBlock reports whether a move keeps the block columns: done from
// blocked keeps them, and the way back to blocked takes them back.
func keepsBlock(from, to domain.TicketState) bool {
	return (from == domain.StateBlocked && to == domain.StateDone) || (from == domain.StateDone && to == domain.StateBlocked)
}

// blockTicket reads the ticket a block waits on, through the predicate.
func blockTicket(ctx context.Context, r *store.Reader, t tenantScope, tc ticketCtx, b *apigen.BlockSet) (*ticketCtx, error) {
	if b.Ticket == nil {
		return nil, nil
	}
	k, err := domain.ParseTicketKey(*b.Ticket)
	if err == nil {
		k, err = k.InTenant(t.Slug)
	}
	if err != nil {
		return nil, problem.Field("/block/ticket", err.Error())
	}
	other, err := visibleTicket(ctx, r, t, k.Project, int(k.Number))
	var perr *problem.Error
	if errors.As(err, &perr) && perr.Code == problem.NotFound {
		return nil, problem.Field("/block/ticket", "no such ticket")
	}
	if err != nil {
		return nil, err
	}
	if other.row.ID == tc.row.ID {
		return nil, problem.Field("/block/ticket", "a ticket does not wait on itself")
	}
	return &other, nil
}

// linkWaitsOn records what a block waits on — a ticket of the team the caller
// sees — as a blocks link from that ticket, when there is none yet
// (docs/adr/0009 D2); one a racing writer stored meanwhile stands as it is.
// The transition took the blocks graph's lock first.
func linkWaitsOn(ctx context.Context, w *store.Writer, t tenantScope, tc, waitsOn ticketCtx) error {
	r := waitsOn.row
	other := store.Readable{ID: r.ID, TenantID: t.ID, ProjectID: r.ProjectID, Head: store.Head{TeamSlug: t.Slug, TeamName: t.Name,
		ProjectKey: r.ProjectKey, Number: r.Number, Title: r.Title, Type: r.Type, State: r.State, Sight: store.SightSees}}
	e := linkEnds{path: tc, other: other, typ: domain.LinkBlocks, source: r.ID, target: tc.row.ID,
		sourceKey: ticketKey(t, r), targetKey: ticketKey(t, tc.row)}
	_, err := w.GetLink(ctx, readq.GetLinkParams{TenantID: t.ID, Type: e.typ, SourceID: e.source, TargetID: e.target})
	if err == nil || !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	_, _, err = addLink(ctx, w, t, e, "/block/ticket")
	return err
}
