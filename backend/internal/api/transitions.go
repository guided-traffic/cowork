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

const actionTransitioned = "transitioned"

// TransitionTicket moves a ticket between states (docs/adr/0009). The state
// the request names must be the current one (docs/adr/0045 D2).
func (s *Server) TransitionTicket(ctx context.Context, req apigen.TransitionTicketRequestObject) (apigen.TransitionTicketResponseObject, error) {
	t := tenantFrom(ctx)
	body := *req.Body
	var out store.TicketRow
	_, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		tc, err := visibleTicket(ctx, w.Reader, t, req.Project, req.Number)
		if err != nil {
			return err
		}
		mv, perr := checkTransition(principal(ctx), tc, body)
		if perr != nil {
			return perr
		}
		rank, err := reopenRank(ctx, w, t, tc, mv)
		if err != nil {
			return err
		}
		explainedBy, err := explain(ctx, w, t, tc, body.Comment)
		if err != nil {
			return err
		}
		ev := store.Event{EntityType: entityTicket, EntityID: tc.row.ID, TicketID: tc.row.ID, TicketKey: ticketKey(t, tc.row),
			Action: actionTransitioned, Reason: deref(body.Reason), Note: deref(body.Note), ExplainedBy: explainedBy}
		if req.Params.IdempotencyKey != nil {
			ev.IdempotencyKey = *req.Params.IdempotencyKey
		}
		after := map[string]any{"state": string(body.To)}
		if mv == domain.MoveDone {
			keys, ids, err := prerequisites(ctx, w.Reader, t, tc, body)
			if err != nil {
				return err
			}
			if len(keys) > 0 {
				after["overridden_prerequisites"] = keys
				ev.Refs = append(ev.Refs, ids...)
			}
		}
		waitsOn, err := move(ctx, w, t, tc, mv, body, after, rank)
		if err != nil {
			return err
		}
		if waitsOn != uuid.Nil {
			ev.Refs = append(ev.Refs, waitsOn)
		}
		ev.Before, ev.After = map[string]any{"state": string(body.From)}, after
		w.Record(ev)
		out, err = reread(ctx, w, t, tc.row.ID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return apigen.TransitionTicket200JSONResponse{Body: ticketView(t, out), Headers: apigen.TransitionTicket200ResponseHeaders{ETag: etag(out.Version)}}, nil
}

// checkTransition holds a transition to the matrix (docs/adr/0009 D2–D5), to
// its required inputs and to the agent's capabilities (docs/adr/0043 D2–D4).
func checkTransition(p auth.Principal, tc ticketCtx, body apigen.Transition) (domain.Move, *problem.Error) {
	from, to := domain.TicketState(body.From), domain.TicketState(body.To)
	if from != tc.row.State {
		return 0, &problem.Error{Code: problem.StateConflict, Detail: "the ticket is " + string(tc.row.State),
			Errors: []problem.FieldError{{Pointer: "/from", Message: "not the current state", Current: string(tc.row.State)}}}
	}
	var origin domain.TicketState
	if tc.row.BlockedFrom != nil {
		origin = *tc.row.BlockedFrom
	}
	mv := domain.ClassifyMove(from, to, origin)
	if mv == domain.MoveInvalid {
		return 0, &problem.Error{Code: problem.StateConflict, Detail: "no transition leads from " + string(from) + " to " + string(to),
			Errors: []problem.FieldError{{Pointer: "/to", Message: "not reachable from " + string(from), Current: string(tc.row.State)}}}
	}
	need := work
	switch {
	case mv == domain.MoveForward && to == domain.StateDecided:
		need.Capability = auth.CapDecide
	case mv == domain.MoveDone:
		need.Capability = auth.CapClose
	case mv == domain.MoveDrop:
		need.Capability = auth.CapDrop
	}
	if perr := auth.Authorize(p, tc.role, need); perr != nil {
		return 0, perr
	}
	override := body.OverridePrerequisites != nil && *body.OverridePrerequisites
	if override {
		if perr := auth.Authorize(p, tc.role, auth.Need{Role: domain.RoleMember, Scope: domain.ScopeWrite, HardOff: auth.HardOffPrereqOverride}); perr != nil {
			return 0, perr
		}
	}
	return mv, transitionInputs(mv, body, override)
}

// transitionInputs requires what the move requires and refuses what it would
// ignore.
func transitionInputs(mv domain.Move, body apigen.Transition, override bool) *problem.Error {
	switch {
	case (mv.NeedsReason() || override) && blank(body.Reason):
		return problem.Field("/reason", "this transition needs a reason")
	case mv == domain.MoveDone && blank(body.Note):
		return problem.Field("/note", "done needs a verification note: what was run, against what, with what result")
	case override && mv != domain.MoveDone:
		return problem.Field("/override_prerequisites", "only done is refused by prerequisites")
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

// prerequisites refuses done over open direct blocks sources the caller can
// see, unless the person overrides; it returns the keys overridden
// (docs/adr/0012 D7).
func prerequisites(ctx context.Context, r *store.Reader, t tenantScope, tc ticketCtx, body apigen.Transition) ([]string, []uuid.UUID, error) {
	open, err := r.ListOpenPrerequisites(ctx, readq.ListOpenPrerequisitesParams{TenantID: t.ID, TicketID: tc.row.ID})
	if err != nil || len(open) == 0 {
		return nil, nil, err
	}
	keys := make([]string, 0, len(open))
	ids := make([]uuid.UUID, 0, len(open))
	errs := make([]problem.FieldError, 0, len(open))
	for _, o := range open {
		key := domain.FullKey(t.Slug, o.ProjectKey, o.Number)
		keys, ids = append(keys, key), append(ids, o.ID)
		errs = append(errs, problem.FieldError{Pointer: "/to", Message: "open prerequisite " + key + ": " + o.Title, Current: string(o.State)})
	}
	if body.OverridePrerequisites == nil || !*body.OverridePrerequisites {
		return nil, nil, &problem.Error{Code: problem.OpenPrerequisites,
			Detail: "tickets that block this one are open; settle them, or override with a reason", Errors: errs}
	}
	return keys, ids, nil
}

// reopenRank is the key a reopened ticket joins its project's rank with, at
// the bottom, taken before the transition writes anything; nil for every
// other move, which keeps the rank or — into done or dropped — takes it away
// (docs/adr/0014 D1).
func reopenRank(ctx context.Context, w *store.Writer, t tenantScope, tc ticketCtx, mv domain.Move) (*string, error) {
	if mv != domain.MoveReopen {
		return nil, nil
	}
	key, err := rankAtBottom(ctx, w, t, tc.project.ID)
	if err != nil {
		return nil, err
	}
	return &key, nil
}

// move writes the state and its effects: the rank a reopen brings, the block
// and the link it names, the urgency of the ticket and — when a decision opens
// or settles — of the tickets it blocks (docs/adr/0010 D3). It returns the
// ticket a block waits on, uuid.Nil without one.
func move(ctx context.Context, w *store.Writer, t tenantScope, tc ticketCtx, mv domain.Move, body apigen.Transition, after map[string]any, rank *string) (uuid.UUID, error) {
	before, _, err := urgencyInputs(ctx, w.Reader, t, tc.row.ID)
	if err != nil {
		return uuid.Nil, err
	}
	var deps []dependent
	from, to := domain.TicketState(body.From), domain.TicketState(body.To)
	if tc.row.Type == domain.TypeDecision && from.Terminal() != to.Terminal() {
		if deps, err = dependentsOf(ctx, w, t, tc.row.ID); err != nil {
			return uuid.Nil, err
		}
	}
	params := writeq.TransitionTicketParams{TenantID: t.ID, ID: tc.row.ID, FromState: from, ToState: to, Rank: rank}
	var waitsOn *ticketCtx
	if mv == domain.MoveBlock {
		if waitsOn, err = blockTicket(ctx, w.Reader, t, tc, body.Block); err != nil {
			return uuid.Nil, err
		}
		kind := domain.BlockKind(body.Block.Kind)
		params.BlockedFrom, params.BlockKind, params.BlockReason, params.BlockExternalRef = &from, &kind, body.Reason, body.Block.ExternalRef
		block := map[string]any{"kind": string(kind)}
		if waitsOn != nil {
			params.BlockTicketID = &waitsOn.row.ID
			block["ticket"] = ticketKey(t, waitsOn.row)
		}
		after["block"] = block
	}
	if _, err := w.TransitionTicket(ctx, params); errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, problem.New(problem.StateConflict, "the ticket's state changed meanwhile; read it again")
	} else if err != nil {
		return uuid.Nil, err
	}
	waits := uuid.Nil
	if waitsOn != nil {
		if err := linkWaitsOn(ctx, w, t, tc, *waitsOn); err != nil {
			return uuid.Nil, err
		}
		waits = waitsOn.row.ID
	}
	if err := rederive(ctx, w, t, tc.row.ID, ticketKey(t, tc.row), before); err != nil {
		return uuid.Nil, err
	}
	if err := refreshProgress(ctx, w, t, tc.row.ParentID); err != nil {
		return uuid.Nil, err
	}
	return waits, rederiveAll(ctx, w, t, deps)
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

// linkWaitsOn records what a block waits on as a blocks link from that
// ticket, when there is none yet (docs/adr/0009 D2).
func linkWaitsOn(ctx context.Context, w *store.Writer, t tenantScope, tc, waitsOn ticketCtx) error {
	e := linkEnds{path: tc, other: waitsOn, typ: domain.LinkBlocks, source: waitsOn.row, target: tc.row}
	_, err := w.GetLink(ctx, readq.GetLinkParams{TenantID: t.ID, Type: e.typ, SourceID: e.source.ID, TargetID: e.target.ID})
	if err == nil || !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	_, err = addLink(ctx, w, t, e, "/block/ticket")
	return err
}
