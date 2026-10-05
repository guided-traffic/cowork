package api

import (
	"context"
	"errors"
	"fmt"

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

// The rank of a project's open tickets (docs/adr/0014 D1, D2). Every write
// that hands out a key — a filing, a return from done or dropped, a move —
// takes the project's rank lock before it writes a ticket row, ranks the open
// tickets an earlier release left without a key, and computes its key from
// keys read under the lock; so two writes never compute a key from the same
// neighbours, and a key is never handed out twice. A key is computed over tickets the caller may
// not see, so it is never shown: not on a ticket (ticketView), not in the act,
// not in a cursor (sealPosition).

const (
	actionRanked = "ranked"
	sideAfter    = "after"
	sideBefore   = "before"
)

// rankNeed is a move in the rank: a member's act with write scope; an agent
// needs rank (docs/adr/0043 D4).
var rankNeed = auth.Need{Role: domain.RoleMember, Scope: domain.ScopeWrite, Capability: auth.CapRank}

// rankTarget is where a move puts the ticket: directly after, or directly
// before, the ticket of the project with the number.
type rankTarget struct {
	after  bool
	number int
}

// side names the place in the act and the body.
func (r rankTarget) side() string {
	if r.after {
		return sideAfter
	}
	return sideBefore
}

// pointer is the body field that names the neighbour.
func (r rankTarget) pointer() string { return "/" + r.side() }

// rankTargetOf reads a move's body: exactly one of after and before. The
// document says so as well; the handler does not rely on the validator.
func rankTargetOf(b apigen.TicketRankSet) (rankTarget, *problem.Error) {
	switch {
	case b.After != nil && b.Before == nil:
		return rankTarget{after: true, number: *b.After}, nil
	case b.Before != nil && b.After == nil:
		return rankTarget{number: *b.Before}, nil
	}
	const message = "name exactly one of after and before"
	return rankTarget{}, &problem.Error{Code: problem.ValidationFailed, Detail: "a move names exactly one neighbour",
		Errors: []problem.FieldError{{Pointer: "/" + sideAfter, Message: message}, {Pointer: "/" + sideBefore, Message: message}}}
}

// MoveTicketRank places a ticket directly after or before another open
// ticket of its project (docs/adr/0014 D1, D2): it writes the ticket's key
// and nothing else, and a ticket that already sits there among the tickets the
// caller can see is answered as it is, without an act. No If-Match: a move
// does not overwrite, the last one wins (docs/adr/0050 D4).
func (s *Server) MoveTicketRank(ctx context.Context, req apigen.MoveTicketRankRequestObject) (apigen.MoveTicketRankResponseObject, error) {
	t := tenantFrom(ctx)
	target, perr := rankTargetOf(*req.Body)
	if perr != nil {
		return nil, perr
	}
	var out store.TicketRow
	_, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		tc, err := visibleTicket(ctx, w.Reader, t, req.Project, req.Number)
		if err != nil {
			return err
		}
		if perr := auth.Authorize(principal(ctx), tc.role, rankNeed); perr != nil {
			return perr
		}
		if perr := rankSelf(tc.row.Number, target); perr != nil {
			return perr
		}
		other, err := rankNeighbour(ctx, w.Reader, t, tc.project.ID, target)
		if err != nil {
			return err
		}
		out, err = moveRank(ctx, w, t, tc, other, target)
		return err
	})
	if err != nil && !errors.Is(err, store.ErrNoChange) {
		return nil, err
	}
	return apigen.MoveTicketRank200JSONResponse{Body: ticketView(t, out, s.h.opts.Now()), Headers: apigen.MoveTicketRank200ResponseHeaders{ETag: etag(out.Version)}}, nil
}

// rankSelf refuses the ticket as its own neighbour.
func rankSelf(moved int32, target rankTarget) *problem.Error {
	if int64(target.number) == int64(moved) {
		return problem.Field(target.pointer(), "a ticket is not placed next to itself")
	}
	return nil
}

// rankNeighbour reads the ticket a move or a filing names, in the project,
// through the predicate: one the caller cannot see is answered like one that
// does not exist, as a parent is (docs/adr/0065 D5).
func rankNeighbour(ctx context.Context, r *store.Reader, t tenantScope, projectID uuid.UUID, target rankTarget) (store.TicketRow, error) {
	const message = "no such ticket in the project"
	n, perr := ticketNumber(target.number)
	if perr != nil {
		return store.TicketRow{}, problem.Field(target.pointer(), message)
	}
	row, err := r.GetTicketByNumber(ctx, readq.GetTicketByNumberParams{TenantID: t.ID, ProjectID: projectID, Number: n})
	if errors.Is(err, pgx.ErrNoRows) {
		return store.TicketRow{}, problem.Field(target.pointer(), message)
	}
	return row, err
}

// moveRank writes a move under the rank lock; the states and keys it decides
// on are read after the lock. A move that changes nothing returns the ticket
// as it stood and ErrNoChange, which rolls back the ranking of unranked
// tickets as well: it writes nothing.
func moveRank(ctx context.Context, w *store.Writer, t tenantScope, tc ticketCtx, other store.TicketRow, target rankTarget) (store.TicketRow, error) {
	if err := lockRank(ctx, w, t, tc.project.ID); err != nil {
		return store.TicketRow{}, err
	}
	cur, err := w.GetTicketByNumber(ctx, readq.GetTicketByNumberParams{TenantID: t.ID, ProjectID: tc.project.ID, Number: tc.row.Number})
	if errors.Is(err, pgx.ErrNoRows) {
		return store.TicketRow{}, problem.New(problem.NotFound, "no such ticket")
	}
	if err != nil {
		return store.TicketRow{}, err
	}
	near, err := w.GetTicketRank(ctx, writeq.GetTicketRankParams{TenantID: t.ID, ID: other.ID})
	if err != nil {
		return store.TicketRow{}, fmt.Errorf("read the neighbour's rank: %w", err)
	}
	if perr := rankStates(cur.State, near.State, target); perr != nil {
		return store.TicketRow{}, perr
	}
	if _, err := rankUnranked(ctx, w, t, tc.project.ID); err != nil {
		return store.TicketRow{}, err
	}
	key, noop, err := placeRank(ctx, w, t, tc, other.ID, target)
	if err != nil {
		return store.TicketRow{}, err
	}
	if noop {
		return cur, store.ErrNoChange
	}
	if _, err := w.MoveTicketRank(ctx, writeq.MoveTicketRankParams{TenantID: t.ID, ID: cur.ID, Rank: key}); errors.Is(err, pgx.ErrNoRows) {
		return store.TicketRow{}, problem.New(problem.StateConflict, "the ticket's state changed meanwhile; read it again")
	} else if err != nil {
		return store.TicketRow{}, err
	}
	// The act names the neighbour, never a key.
	w.Record(store.Event{EntityType: entityTicket, EntityID: cur.ID, TicketID: cur.ID, TicketKey: ticketKey(t, cur),
		Action: actionRanked, After: map[string]any{target.side(): ticketKey(t, other)}, Refs: []uuid.UUID{other.ID}})
	return reread(ctx, w, t, cur.ID)
}

// rankStates refuses a move of, or next to, a done or dropped ticket: it has
// no rank (docs/adr/0014 D1).
func rankStates(moved, neighbour domain.TicketState, target rankTarget) *problem.Error {
	switch {
	case moved.Terminal():
		return &problem.Error{Code: problem.StateConflict, Detail: "a " + string(moved) + " ticket has no rank; reopen it first",
			Errors: []problem.FieldError{{Pointer: "path:number", Message: "the ticket is " + string(moved), Current: string(moved)}}}
	case neighbour.Terminal():
		return &problem.Error{Code: problem.StateConflict, Detail: "a " + string(neighbour) + " ticket has no rank to be placed next to",
			Errors: []problem.FieldError{{Pointer: target.pointer(), Message: "the ticket is " + string(neighbour), Current: string(neighbour)}}}
	}
	return nil
}

// placeRank decides a move on what it reads under the lock (gapBeside). A key
// longer than domain.RankRebalanceLength, or none that fits, means moves have
// worn the gap down: the project's keys are spread again first
// (rebalanceRank), and the move is decided on the gap as it is then.
func placeRank(ctx context.Context, w *store.Writer, t tenantScope, tc ticketCtx, neighbour uuid.UUID, target rankTarget) (string, bool, error) {
	for spread := false; ; spread = true {
		near, seen, far, err := gapBeside(ctx, w, t, tc.project.ID, neighbour, target.after)
		if err != nil {
			return "", false, err
		}
		key, noop, err := planRank(target.after, near, far, seen, tc.row.ID)
		if noop || spread || !crowded(key, err) {
			return key, noop, err
		}
		if err := rebalanceRank(ctx, w, t, tc.project.ID); err != nil {
			return "", false, err
		}
	}
}

// gapBeside reads, under the rank lock, the gap a ticket placed next to the
// neighbour on one side goes into: the neighbour's key, the open ticket the
// caller sees next to it on that side, and the next key of any ticket there.
func gapBeside(ctx context.Context, w *store.Writer, t tenantScope, projectID, neighbour uuid.UUID, after bool) (string, uuid.UUID, string, error) {
	near, err := w.GetTicketRank(ctx, writeq.GetTicketRankParams{TenantID: t.ID, ID: neighbour})
	if err != nil {
		return "", uuid.Nil, "", fmt.Errorf("read the neighbour's rank: %w", err)
	}
	if near.Rank == nil {
		// Done and dropped take no rank lock: the neighbour left the rank
		// after its state was read.
		return "", uuid.Nil, "", problem.New(problem.StateConflict, "the neighbour's state changed meanwhile; read it again")
	}
	seen, far, err := beside(ctx, w, t, projectID, *near.Rank, after)
	return *near.Rank, seen, far, err
}

// crowded reports whether a key computed for a place asks for the project's
// keys to be spread first: longer than domain.RankRebalanceLength, or no key
// fits the column at all.
func crowded(key string, err error) bool {
	return errors.Is(err, domain.ErrRankTooLong) || (err == nil && len(key) > domain.RankRebalanceLength)
}

// rebalanceRank spreads the keys of the project's open tickets evenly again,
// in their order (domain.RankSpread, docs/adr/0014 Consequences), every
// ticket of the project counted, those the caller cannot see included, so
// each keeps its place; a key a release before the rank left on a done or
// dropped ticket is taken away with them. Maintenance, not a move: no act and
// no version. The caller holds the rank lock and has ranked the unranked.
func rebalanceRank(ctx context.Context, w *store.Writer, t tenantScope, projectID uuid.UUID) error {
	rows, err := w.ListRankKeys(ctx, writeq.ListRankKeysParams{TenantID: t.ID, ProjectID: projectID})
	if err != nil {
		return fmt.Errorf("read the rank keys: %w", err)
	}
	held := make([]uuid.UUID, 0, len(rows))
	open := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		held = append(held, r.ID)
		if !r.State.Terminal() {
			open = append(open, r.ID)
		}
	}
	return writeRanks(ctx, w, t, held, open, domain.RankSpread(len(open)), false)
}

// beside reads what lies next to a key on one side of the project's rank:
// the first open ticket the caller can see, uuid.Nil at an end, and the first
// key of any ticket, whoever holds it and whatever its state, "" at an end.
func beside(ctx context.Context, w *store.Writer, t tenantScope, projectID uuid.UUID, key string, after bool) (uuid.UUID, string, error) {
	var (
		seen            uuid.UUID
		far             string
		errSeen, errFar error
	)
	if after {
		seen, errSeen = orEnd(w.NextSeenRankedTicket(ctx, writeq.NextSeenRankedTicketParams{TenantID: t.ID, ProjectID: projectID, After: key}))
		far, errFar = orEnd(w.NextRankedTicket(ctx, writeq.NextRankedTicketParams{TenantID: t.ID, ProjectID: projectID, After: key}))
	} else {
		seen, errSeen = orEnd(w.PreviousSeenRankedTicket(ctx, writeq.PreviousSeenRankedTicketParams{TenantID: t.ID, ProjectID: projectID, Before: key}))
		far, errFar = orEnd(w.PreviousRankedTicket(ctx, writeq.PreviousRankedTicketParams{TenantID: t.ID, ProjectID: projectID, Before: key}))
	}
	if err := errors.Join(errSeen, errFar); err != nil {
		return uuid.Nil, "", fmt.Errorf("read beside the neighbour: %w", err)
	}
	return seen, far, nil
}

// orEnd is a query's answer, the zero value where the rank ends.
func orEnd[T any](v T, err error) (T, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		var end T
		return end, nil
	}
	return v, err
}

// planRank decides a move. seen is the ticket the caller can see next to the
// neighbour on the side the moved ticket goes to, uuid.Nil at an end: when it
// is the moved one, the ticket already sits there for the caller, and the move
// is none, whatever sits between unseen — so whether it writes tells nothing
// of a hidden ticket. Otherwise the key lies strictly between the neighbour's
// and far, the next key on that side over every ticket ("" at an end), so it
// never equals or passes a key the caller cannot see.
func planRank(after bool, neighbour, far string, seen, moved uuid.UUID) (key string, noop bool, err error) {
	if seen == moved {
		return "", true, nil
	}
	key, err = between(after, neighbour, far)
	return key, false, err
}

// between is a key strictly between the neighbour's key and far, the next key
// on the side after names ("" at an end).
func between(after bool, neighbour, far string) (string, error) {
	var (
		key string
		err error
	)
	if after {
		key, err = domain.RankBetween(neighbour, far)
	} else {
		key, err = domain.RankBetween(far, neighbour)
	}
	if err != nil {
		return "", fmt.Errorf("place the rank: %w", err)
	}
	return key, nil
}

// lockRank takes the project's rank lock: its counter row, which a filing
// locks with its number anyway (LockProjectRank).
func lockRank(ctx context.Context, w *store.Writer, t tenantScope, projectID uuid.UUID) error {
	if err := w.LockProjectRank(ctx, writeq.LockProjectRankParams{TenantID: t.ID, ProjectID: projectID}); err != nil {
		return fmt.Errorf("take the rank lock: %w", err)
	}
	return nil
}

// rankUnranked gives the project's open tickets without a key — filed, or
// reopened, by a release before the rank (docs/adr/0028 D3) — keys at the
// bottom in number order, where the list showed them; it is no move, so no
// act and no version. It returns the bottom key after them, "" for an empty
// rank. The caller holds the rank lock.
func rankUnranked(ctx context.Context, w *store.Writer, t tenantScope, projectID uuid.UUID) (string, error) {
	ids, err := w.ListUnrankedTickets(ctx, writeq.ListUnrankedTicketsParams{TenantID: t.ID, ProjectID: projectID})
	if err != nil {
		return "", fmt.Errorf("read the unranked tickets: %w", err)
	}
	last, err := w.LastRank(ctx, writeq.LastRankParams{TenantID: t.ID, ProjectID: projectID})
	if err != nil {
		return "", fmt.Errorf("read the bottom of the rank: %w", err)
	}
	for _, id := range ids {
		if last, err = domain.RankBetween(last, ""); err != nil {
			return "", fmt.Errorf("rank an unranked ticket: %w", err)
		}
		if err := w.RankUnrankedTicket(ctx, writeq.RankUnrankedTicketParams{TenantID: t.ID, ID: id, Rank: last}); err != nil {
			return "", fmt.Errorf("rank an unranked ticket: %w", err)
		}
	}
	return last, nil
}

// rankBeside is the key of a filing placed directly after or before an open
// ticket of the project in the horizon it is filed into (docs/adr/0014 D2 as
// amended 2026-10-04), under the rank lock: strictly between that ticket's key
// and the next key on that side over every ticket, as a move's. It returns
// the ticket named, for the act.
func rankBeside(ctx context.Context, w *store.Writer, t tenantScope, projectID uuid.UUID, target rankTarget, horizon domain.Urgency) (store.TicketRow, string, error) {
	if err := lockRank(ctx, w, t, projectID); err != nil {
		return store.TicketRow{}, "", err
	}
	if _, err := rankUnranked(ctx, w, t, projectID); err != nil {
		return store.TicketRow{}, "", err
	}
	other, err := rankNeighbour(ctx, w.Reader, t, projectID, target)
	if err != nil {
		return store.TicketRow{}, "", err
	}
	if perr := rankStates(domain.StateFiled, other.State, target); perr != nil {
		return other, "", perr
	}
	if u := horizonOf(other); u != horizon {
		return other, "", problem.Field(target.pointer(),
			fmt.Sprintf("%s stands in the horizon %s, not in %s", ticketKey(t, other), u, horizon))
	}
	for spread := false; ; spread = true {
		near, _, far, err := gapBeside(ctx, w, t, projectID, other.ID, target.after)
		if err != nil {
			return other, "", err
		}
		key, err := between(target.after, near, far)
		if spread || !crowded(key, err) {
			return other, key, err
		}
		if err := rebalanceRank(ctx, w, t, projectID); err != nil {
			return other, "", err
		}
	}
}

// horizonOf is the horizon a ticket stands in: the one set on it, else the
// derived one (docs/adr/0010 D3).
func horizonOf(r store.TicketRow) domain.Urgency {
	if r.UrgencyOverride != nil {
		return *r.UrgencyOverride
	}
	return r.UrgencyDerived
}

// rankAtBottom is the key of a ticket that joins its project's rank at the
// bottom — a filing, a reopen — under the rank lock and below the tickets
// that had none (docs/adr/0014 D2).
func rankAtBottom(ctx context.Context, w *store.Writer, t tenantScope, projectID uuid.UUID) (string, error) {
	if err := lockRank(ctx, w, t, projectID); err != nil {
		return "", err
	}
	last, err := rankUnranked(ctx, w, t, projectID)
	if err != nil {
		return "", err
	}
	key, err := domain.RankBetween(last, "")
	if err != nil {
		return "", fmt.Errorf("rank at the bottom: %w", err)
	}
	return key, nil
}
