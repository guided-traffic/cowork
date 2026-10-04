package api

import (
	"bytes"
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

const entityLink = "link"

// ListTicketLinks lists a ticket's links in both directions, each read from
// the ticket's side (docs/adr/0012 D1).
func (s *Server) ListTicketLinks(ctx context.Context, req apigen.ListTicketLinksRequestObject) (apigen.ListTicketLinksResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, read); perr != nil {
		return nil, perr
	}
	const op = "listTicketLinks"
	scope := fmt.Sprintf("%s/%s/%d", t.ID, req.Project, req.Number)
	size := s.h.pageSize(req.Params.Limit)
	var rows []readq.ListTicketLinksRow
	err := s.db.InTenant(ctx, t.ID, func(r *store.Reader) error {
		tc, err := visibleTicket(ctx, r, t, req.Project, req.Number)
		if err != nil {
			return err
		}
		params := readq.ListTicketLinksParams{TenantID: t.ID, TicketID: tc.row.ID, PageSize: limitArg(size)}
		if req.Params.Cursor != nil {
			after, perr := s.cursors.decode(op, scope, *req.Params.Cursor)
			if perr != nil {
				return perr
			}
			id, err := uuid.Parse(after)
			if err != nil {
				return problem.New(problem.InvalidCursor, "the cursor does not belong to this list")
			}
			params.After = &id
		}
		rows, err = r.ListTicketLinks(ctx, params)
		return err
	})
	if err != nil {
		return nil, err
	}
	rows, next := page(s.h, rows, size, op, scope, func(l readq.ListTicketLinksRow) string { return l.ID.String() })
	out := apigen.LinkList{Items: []apigen.Link{}, NextCursor: nullableString(next)}
	for _, l := range rows {
		out.Items = append(out.Items, apigen.Link{
			Id: l.ID, Type: apigen.LinkType(l.Type), Direction: direction(l.Outgoing), Name: l.Type.Name(l.Outgoing),
			Ticket: apigen.TicketRef{Key: domain.FullKey(t.Slug, l.OtherProjectKey, l.OtherNumber), Title: l.OtherTitle,
				State: apigen.TicketState(l.OtherState)},
			CreatedBy: personView(l.CreatedBy, l.CreatedByUsername, l.CreatedByName), CreatedAt: l.CreatedAt,
		})
	}
	tag, unchanged := listTag(req.Params.IfNoneMatch, out)
	if unchanged {
		return apigen.ListTicketLinks304Response{Headers: apigen.NotModifiedResponseHeaders{ETag: &tag}}, nil
	}
	return apigen.ListTicketLinks200JSONResponse{Body: out, Headers: apigen.ListTicketLinks200ResponseHeaders{ETag: &tag}}, nil
}

func direction(outgoing bool) apigen.LinkDirection {
	if outgoing {
		return apigen.LinkDirectionOutgoing
	}
	return apigen.LinkDirectionIncoming
}

// linkEnds is a link request read: the ticket in the path, the other end,
// and the link's stored direction.
type linkEnds struct {
	path, other    ticketCtx
	typ            domain.LinkType
	source, target store.TicketRow
}

// readLinkEnds reads both ends through the predicate — an end the caller
// cannot see is the 404 of one that does not exist — and authorizes the act
// on the ticket in the path: a member's, with write scope, in the agent
// baseline (docs/adr/0043 D2).
func readLinkEnds(ctx context.Context, r *store.Reader, t tenantScope, project string, number int, typ apigen.LinkType, other string) (linkEnds, error) {
	path, err := visibleTicket(ctx, r, t, project, number)
	if err != nil {
		return linkEnds{}, err
	}
	if perr := auth.Authorize(principal(ctx), path.role, work); perr != nil {
		return linkEnds{}, perr
	}
	key, err := domain.ParseTicketKey(other)
	if err != nil {
		return linkEnds{}, problem.New(problem.NotFound, "no such ticket")
	}
	o, err := visibleTicket(ctx, r, t, key.Project, int(key.Number))
	if err != nil {
		return linkEnds{}, err
	}
	if o.row.ID == path.row.ID {
		return linkEnds{}, &problem.Error{Code: problem.ValidationFailed, Detail: "a ticket does not link to itself",
			Errors: []problem.FieldError{{Pointer: "path:other", Message: "the ticket itself"}}}
	}
	e := linkEnds{path: path, other: o, typ: domain.LinkType(typ), source: path.row, target: o.row}
	// relates-to is symmetric and stored once, the smaller id first.
	if e.typ == domain.LinkRelatesTo && bytes.Compare(e.source.ID[:], e.target.ID[:]) > 0 {
		e.source, e.target = e.target, e.source
	}
	return e, nil
}

func (e linkEnds) view(t tenantScope, id uuid.UUID, by apigen.Person, at store.TicketRow) apigen.Link {
	outgoing := e.source.ID == e.path.row.ID
	return apigen.Link{
		Id: id, Type: apigen.LinkType(e.typ), Direction: direction(outgoing), Name: e.typ.Name(outgoing),
		Ticket:    apigen.TicketRef{Key: domain.FullKey(t.Slug, at.ProjectKey, at.Number), Title: at.Title, State: apigen.TicketState(at.State)},
		CreatedBy: by,
	}
}

// record writes the act on both tickets (docs/adr/0012 D3).
func (e linkEnds) record(w *store.Writer, t tenantScope, action string, id uuid.UUID) {
	payload := map[string]any{fieldType: string(e.typ),
		"source": domain.FullKey(t.Slug, e.source.ProjectKey, e.source.Number),
		"target": domain.FullKey(t.Slug, e.target.ProjectKey, e.target.Number)}
	for _, ends := range [][2]store.TicketRow{{e.source, e.target}, {e.target, e.source}} {
		end, other := ends[0], ends[1]
		ev := store.Event{EntityType: entityLink, EntityID: id, TicketID: end.ID,
			TicketKey: domain.FullKey(t.Slug, end.ProjectKey, end.Number), Action: action, Refs: []uuid.UUID{other.ID}}
		if action == actionLinked {
			ev.After = payload
		} else {
			ev.Before = payload
		}
		w.Record(ev)
	}
}

// LinkTickets links the ticket in the path, as the source, to another ticket
// of the tenant; an existing link is success without a second act
// (docs/adr/0045 D1).
func (s *Server) LinkTickets(ctx context.Context, req apigen.LinkTicketsRequestObject) (apigen.LinkTicketsResponseObject, error) {
	t := tenantFrom(ctx)
	var out apigen.Link
	created := false
	_, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		e, err := readLinkEnds(ctx, w.Reader, t, req.Project, req.Number, req.Type, req.Other)
		if err != nil {
			return err
		}
		key := readq.GetLinkParams{TenantID: t.ID, Type: e.typ, SourceID: e.source.ID, TargetID: e.target.ID}
		existing, err := w.GetLink(ctx, key)
		if err == nil {
			out = e.view(t, existing.ID, personView(existing.CreatedBy, existing.CreatedByUsername, existing.CreatedByName), e.other.row)
			out.CreatedAt = existing.CreatedAt
			return store.ErrNoChange
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		var before domain.UrgencyInputs
		if e.typ == domain.LinkBlocks {
			if before, err = urgencyInputs(ctx, w.Reader, t, e.target.ID); err != nil {
				return err
			}
		}
		me := principal(ctx)
		ins, err := addLink(ctx, w, t, e, "path:other")
		if err != nil {
			return err
		}
		if e.typ == domain.LinkBlocks {
			if err := rederive(ctx, w, t, e.target.ID, before); err != nil {
				return err
			}
		}
		out = e.view(t, ins.ID, apigen.Person{Id: me.PersonID, DisplayName: me.DisplayName, Username: nullableString(nil)}, e.other.row)
		out.CreatedAt, created = ins.CreatedAt, true
		return nil
	})
	if err != nil && !errors.Is(err, store.ErrNoChange) {
		return nil, err
	}
	if created {
		return apigen.LinkTickets201JSONResponse(out), nil
	}
	return apigen.LinkTickets200JSONResponse(out), nil
}

// addLink creates the link the ends describe, with its act on both tickets;
// a blocks link takes the tenant's lock and refuses a cycle first
// (docs/adr/0012 D4). pointer names the other end in a refusal.
func addLink(ctx context.Context, w *store.Writer, t tenantScope, e linkEnds, pointer string) (writeq.InsertLinkRow, error) {
	if e.typ == domain.LinkBlocks {
		if err := w.LockBlocks(ctx); err != nil {
			return writeq.InsertLinkRow{}, err
		}
		cycle, err := w.BlocksPathExists(ctx, readq.BlocksPathExistsParams{TenantID: t.ID, FromID: e.target.ID, ToID: e.source.ID})
		if err != nil {
			return writeq.InsertLinkRow{}, fmt.Errorf("walk the blocks graph: %w", err)
		}
		if cycle {
			return writeq.InsertLinkRow{}, &problem.Error{Code: problem.LinkCycle,
				Detail: "the blocked ticket already blocks the other one over blocks links",
				Errors: []problem.FieldError{{Pointer: pointer, Message: "would close a cycle"}}}
		}
	}
	ins, err := w.InsertLink(ctx, writeq.InsertLinkParams{TenantID: t.ID, Type: e.typ, SourceID: e.source.ID,
		TargetID: e.target.ID, CreatedBy: principal(ctx).PersonID})
	if err != nil {
		return ins, fmt.Errorf("insert the link: %w", err)
	}
	e.record(w, t, actionLinked, ins.ID)
	return ins, nil
}

func ticketKey(t tenantScope, r store.TicketRow) string {
	return domain.FullKey(t.Slug, r.ProjectKey, r.Number)
}

// UnlinkTickets removes a link; one that does not exist is gone as well.
func (s *Server) UnlinkTickets(ctx context.Context, req apigen.UnlinkTicketsRequestObject) (apigen.UnlinkTicketsResponseObject, error) {
	t := tenantFrom(ctx)
	_, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		e, err := readLinkEnds(ctx, w.Reader, t, req.Project, req.Number, req.Type, req.Other)
		if err != nil {
			return err
		}
		existing, err := w.GetLink(ctx, readq.GetLinkParams{TenantID: t.ID, Type: e.typ, SourceID: e.source.ID, TargetID: e.target.ID})
		if errors.Is(err, pgx.ErrNoRows) {
			return store.ErrNoChange
		}
		if err != nil {
			return err
		}
		var before domain.UrgencyInputs
		if e.typ == domain.LinkBlocks {
			if before, err = urgencyInputs(ctx, w.Reader, t, e.target.ID); err != nil {
				return err
			}
		}
		if _, err := w.DeleteLink(ctx, writeq.DeleteLinkParams{TenantID: t.ID, Type: e.typ, SourceID: e.source.ID, TargetID: e.target.ID}); err != nil {
			return err
		}
		e.record(w, t, actionUnlinked, existing.ID)
		if e.typ == domain.LinkBlocks {
			return rederive(ctx, w, t, e.target.ID, before)
		}
		return nil
	})
	if err != nil && !errors.Is(err, store.ErrNoChange) {
		return nil, err
	}
	return apigen.UnlinkTickets204Response{}, nil
}

// urgencyInputs reads the facts rule set v1 derives from.
func urgencyInputs(ctx context.Context, r *store.Reader, t tenantScope, id uuid.UUID) (domain.UrgencyInputs, error) {
	row, err := r.GetUrgencyInputs(ctx, readq.GetUrgencyInputsParams{TenantID: t.ID, ID: id})
	if err != nil {
		return domain.UrgencyInputs{}, fmt.Errorf("read the urgency inputs: %w", err)
	}
	in := domain.UrgencyInputs{State: row.State, OpenDecisionBlocker: row.OpenDecisionBlocker}
	if row.BlockKind != nil {
		in.BlockKind = *row.BlockKind
	}
	return in, nil
}

// rederive applies rule set v1 again when one of its inputs changed. A
// standing override stays — it holds until a person or an agent withdraws it
// or sets another — and the new derived value and its rule show beside it
// (docs/adr/0010 D3). The derivation is no act of its own: the act that
// changed the input is recorded.
func rederive(ctx context.Context, w *store.Writer, t tenantScope, id uuid.UUID, before domain.UrgencyInputs) error {
	after, err := urgencyInputs(ctx, w.Reader, t, id)
	if err != nil {
		return err
	}
	if after.Normalized() == before.Normalized() {
		return nil
	}
	u, rule := domain.DeriveUrgency(after)
	if err := w.RederiveUrgency(ctx, writeq.RederiveUrgencyParams{TenantID: t.ID, ID: id, UrgencyDerived: u, UrgencyRule: rule}); err != nil {
		return fmt.Errorf("derive the urgency again: %w", err)
	}
	return nil
}

// dependent is a ticket whose urgency derivation reads another ticket, with
// its inputs before a change of that ticket.
type dependent struct {
	id     uuid.UUID
	before domain.UrgencyInputs
}

// dependentsOf reads the tickets a ticket blocks and their inputs, before a
// change of the ticket that may be one of their inputs: a decision opening
// or settling (docs/adr/0010 D3).
func dependentsOf(ctx context.Context, w *store.Writer, t tenantScope, id uuid.UUID) ([]dependent, error) {
	rows, err := w.ListBlockedTickets(ctx, readq.ListBlockedTicketsParams{TenantID: t.ID, TicketID: id})
	if err != nil {
		return nil, fmt.Errorf("list the blocked tickets: %w", err)
	}
	out := make([]dependent, 0, len(rows))
	for _, id := range rows {
		in, err := urgencyInputs(ctx, w.Reader, t, id)
		if err != nil {
			return nil, err
		}
		out = append(out, dependent{id: id, before: in})
	}
	return out, nil
}

// rederiveAll derives the dependents' urgency again after the change.
func rederiveAll(ctx context.Context, w *store.Writer, t tenantScope, deps []dependent) error {
	for _, d := range deps {
		if err := rederive(ctx, w, t, d.id, d.before); err != nil {
			return err
		}
	}
	return nil
}
