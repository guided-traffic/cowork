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
// the ticket's side (docs/adr/0012 D1) — inside its team and among the ends
// the caller sees, as before the links crossed teams: deprecated, replaced by
// ListTicketRelations (docs/adr/0046 D7).
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

// noSuchTicket is the one answer for the other end of a link the caller does
// not read, whatever the reason, exactly the answer for a key that names
// nothing (docs/adr/0008 D2, docs/adr/0012 D2): trying keys tells nothing.
func noSuchTicket() *problem.Error { return problem.New(problem.NotFound, "no such ticket") }

// linkEnds is a link the caller sets: the ticket in the path, the other end —
// a ticket they read, of any team — and the link's stored direction. A link
// lives in its source's team, the path's; inside a team relates-to stores the
// smaller id first.
type linkEnds struct {
	path                 ticketCtx
	other                store.Readable
	typ                  domain.LinkType
	source, target       uuid.UUID
	sourceKey, targetKey string
}

// readLinkEnds reads a link the caller sets on the ticket in the path, its
// source: a member's act with write scope on it, in the agent baseline
// (docs/adr/0043 D2), authorized before the other end is looked at; the other
// end — of the team named, the path's for the short form — must be a ticket the
// caller reads, and one they do not read is the 404 of one that does not exist
// (docs/adr/0012 D2 as amended 2026-10-10).
func readLinkEnds(ctx context.Context, r *store.Reader, t tenantScope, project string, number int, typ apigen.LinkType,
	otherTeam, other string) (linkEnds, error) {
	path, err := visibleTicket(ctx, r, t, project, number)
	if err != nil {
		return linkEnds{}, err
	}
	if perr := auth.Authorize(principal(ctx), path.role, work); perr != nil {
		return linkEnds{}, perr
	}
	key, err := domain.ParseTicketKey(other)
	if err != nil || key.Tenant != "" {
		return linkEnds{}, noSuchTicket()
	}
	o, ok, err := r.ReadableTicket(ctx, otherTeam, key.Project, key.Number)
	if err != nil {
		return linkEnds{}, err
	}
	if !ok {
		return linkEnds{}, noSuchTicket()
	}
	if o.ID == path.row.ID {
		return linkEnds{}, &problem.Error{Code: problem.ValidationFailed, Detail: "a ticket does not link to itself",
			Errors: []problem.FieldError{{Pointer: "path:other", Message: "the ticket itself"}}}
	}
	e := linkEnds{path: path, other: o, typ: domain.LinkType(typ), source: path.row.ID, target: o.ID,
		sourceKey: ticketKey(t, path.row), targetKey: o.Head.Key()}
	// relates-to is symmetric and stored once: inside the team the smaller id
	// first; across teams from the end it was made from.
	if e.typ == domain.LinkRelatesTo && !o.Elsewhere && bytes.Compare(e.source[:], e.target[:]) > 0 {
		e.source, e.target, e.sourceKey, e.targetKey = e.target, e.source, e.targetKey, e.sourceKey
	}
	return e, nil
}

// outgoing reports whether the ticket in the path is the link's source.
func (e linkEnds) outgoing() bool { return e.source == e.path.row.ID }

// existing finds the link the ends describe, made before: in the team, or —
// a relates-to across teams — made from the other end, in its team.
func (e linkEnds) existing(ctx context.Context, r *store.Reader, t tenantScope) (*store.RelatedLink, error) {
	l, err := r.GetLink(ctx, readq.GetLinkParams{TenantID: t.ID, Type: e.typ, SourceID: e.source, TargetID: e.target})
	if err == nil {
		return &store.RelatedLink{ID: l.ID, Type: e.typ, Outgoing: e.outgoing(),
			CreatedBy: store.Person{ID: l.CreatedBy, Username: l.CreatedByUsername, Name: l.CreatedByName}, CreatedAt: l.CreatedAt}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if e.typ != domain.LinkRelatesTo || !e.other.Elsewhere {
		return nil, nil
	}
	rels, err := r.RelationHeads(ctx, []uuid.UUID{e.path.row.ID}, store.RelationLink)
	if err != nil {
		return nil, err
	}
	for _, rel := range rels {
		if rel.Link != nil && rel.Link.Type == domain.LinkRelatesTo && rel.Head.Key() == e.other.Head.Key() {
			return rel.Link, nil
		}
	}
	return nil, nil
}

// view is the link as the path's ticket reads it, its maker named as the
// list names them: the short form's answer.
func (e linkEnds) view(l store.RelatedLink) apigen.Link {
	return apigen.Link{
		Id: l.ID, Type: apigen.LinkType(e.typ), Direction: direction(l.Outgoing), Name: e.typ.Name(l.Outgoing),
		Ticket:    apigen.TicketRef{Key: e.other.Head.Key(), Title: e.other.Head.Title, State: apigen.TicketState(e.other.Head.State)},
		CreatedBy: personView(l.CreatedBy.ID, l.CreatedBy.Username, l.CreatedBy.Name), CreatedAt: l.CreatedAt,
	}
}

// relation is the link as a relation of the path's ticket: the canonical
// form's answer.
func (e linkEnds) relation(l store.RelatedLink) apigen.Relation {
	return relationView(store.Relation{Kind: store.RelationLink, Link: &l, Head: e.other.Head})
}

// linkPayload is what a link's acts record of it.
func linkPayload(typ domain.LinkType, sourceKey, targetKey string) map[string]any {
	return map[string]any{fieldType: string(typ), "source": sourceKey, "target": targetKey}
}

// linkAct is a link's act on one of its tickets, naming the other in Refs: a
// reader of the ticket who cannot see the other reads it without its payload
// (docs/adr/0065 D4), and an end of another team is seen by no reader of the
// act's own team.
func linkAct(action string, id uuid.UUID, payload map[string]any, other uuid.UUID) store.Event {
	ev := store.Event{EntityType: entityLink, EntityID: id, Action: action, Refs: []uuid.UUID{other}}
	if action == actionLinked {
		ev.After = payload
	} else {
		ev.Before = payload
	}
	return ev
}

// record writes the act on both tickets (docs/adr/0012 D3): the other end's
// in its own team's record when it is a ticket of another team.
func (e linkEnds) record(ctx context.Context, w *store.Writer, t tenantScope, action string, id uuid.UUID) error {
	payload := linkPayload(e.typ, e.sourceKey, e.targetKey)
	near := linkAct(action, id, payload, e.other.ID)
	near.TicketID, near.TicketKey = e.path.row.ID, ticketKey(t, e.path.row)
	w.Record(near)
	far := linkAct(action, id, payload, e.path.row.ID)
	if e.other.Elsewhere {
		return w.RecordElsewhere(ctx, e.other.Far, far)
	}
	far.TicketID, far.TicketKey = e.other.ID, e.other.Head.Key()
	w.Record(far)
	return nil
}

// setLinkAttempts bounds how often setLink reads a link a racing writer
// stored and finds it gone again before it inserts its own.
const setLinkAttempts = 3

// setLink makes the link the ends describe, or finds the one made before
// (docs/adr/0045 D1); created says it is new. A writer that raced another to
// the same link — the same pair at once, or a relates-to from both ends at
// once, whichever end stored it — gets the answer for the link that now
// exists, never a refusal: the insert takes the conflict and the link is read
// back.
func setLink(ctx context.Context, w *store.Writer, t tenantScope, e linkEnds, pointer string) (store.RelatedLink, bool, error) {
	for range setLinkAttempts {
		existing, err := e.existing(ctx, w.Reader, t)
		if err != nil {
			return store.RelatedLink{}, false, err
		}
		if existing != nil {
			return *existing, false, nil
		}
		made, created, err := addLink(ctx, w, t, e, pointer)
		if err != nil || created {
			return made, created, err
		}
	}
	// The link a racing writer stored is gone again, its other end with it:
	// the answer of an other end that names nothing.
	return store.RelatedLink{}, false, noSuchTicket()
}

// addLink creates the link the ends describe, with its act on both tickets;
// a blocks link takes the installation's lock of the blocks graph and refuses
// a cycle through any team first (docs/adr/0012 D4). pointer names the other
// end in a refusal. created is false, and nothing is recorded, where an equal
// link stands by now, stored by a writer that raced this one.
func addLink(ctx context.Context, w *store.Writer, t tenantScope, e linkEnds, pointer string) (link store.RelatedLink, created bool, err error) {
	if e.typ == domain.LinkBlocks {
		if err := w.LockGraph(ctx, store.GraphBlocks); err != nil {
			return store.RelatedLink{}, false, err
		}
		cycle, err := w.BlocksReach(ctx, e.target, e.source)
		if err != nil {
			return store.RelatedLink{}, false, err
		}
		if cycle {
			return store.RelatedLink{}, false, &problem.Error{Code: problem.LinkCycle,
				Detail: "the blocked ticket already blocks the other one over blocks links",
				Errors: []problem.FieldError{{Pointer: pointer, Message: "would close a cycle"}}}
		}
	}
	caller := principal(ctx)
	ins, err := w.InsertLink(ctx, writeq.InsertLinkParams{TenantID: t.ID, Type: e.typ, SourceID: e.source,
		TargetID: e.target, CreatedBy: caller.PersonID})
	if errors.Is(err, pgx.ErrNoRows) {
		return store.RelatedLink{}, false, nil
	}
	if err != nil {
		return store.RelatedLink{}, false, fmt.Errorf("insert the link: %w", err)
	}
	if err := e.record(ctx, w, t, actionLinked, ins.ID); err != nil {
		return store.RelatedLink{}, false, err
	}
	// The principal does not carry its person's username; the maker is read
	// back as the list reads them.
	l, err := w.GetLink(ctx, readq.GetLinkParams{TenantID: t.ID, Type: e.typ, SourceID: e.source, TargetID: e.target})
	if err != nil {
		return store.RelatedLink{}, false, fmt.Errorf("read the new link: %w", err)
	}
	return store.RelatedLink{ID: ins.ID, Type: e.typ, Outgoing: e.outgoing(),
		CreatedBy: store.Person{ID: l.CreatedBy, Username: l.CreatedByUsername, Name: l.CreatedByName}, CreatedAt: ins.CreatedAt}, true, nil
}

func ticketKey(t tenantScope, r store.TicketRow) string {
	return domain.FullKey(t.Slug, r.ProjectKey, r.Number)
}

// LinkTickets links the ticket in the path, as the source, to another ticket
// of the team by its short key (docs/adr/0007 D3); an existing link is success
// without a second act (docs/adr/0045 D1).
func (s *Server) LinkTickets(ctx context.Context, req apigen.LinkTicketsRequestObject) (apigen.LinkTicketsResponseObject, error) {
	t := tenantFrom(ctx)
	var (
		out     apigen.Link
		created bool
	)
	_, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		e, err := readLinkEnds(ctx, w.Reader, t, req.Project, req.Number, req.Type, t.Slug, req.Other)
		if err != nil {
			return err
		}
		l, isNew, err := setLink(ctx, w, t, e, "path:other")
		if err != nil {
			return err
		}
		out, created = e.view(l), isNew
		if !isNew {
			return store.ErrNoChange
		}
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

// LinkTicketTo links the ticket in the path, as the source, to a ticket of
// any team by its canonical key (docs/adr/0012 D2 as amended 2026-10-10).
func (s *Server) LinkTicketTo(ctx context.Context, req apigen.LinkTicketToRequestObject) (apigen.LinkTicketToResponseObject, error) {
	t := tenantFrom(ctx)
	var (
		out     apigen.Relation
		created bool
	)
	_, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		e, err := readLinkEnds(ctx, w.Reader, t, req.Project, req.Number, req.Type, req.OtherTeam, req.Other)
		if err != nil {
			return err
		}
		l, isNew, err := setLink(ctx, w, t, e, "path:other")
		if err != nil {
			return err
		}
		out, created = e.relation(l), isNew
		if !isNew {
			return store.ErrNoChange
		}
		return nil
	})
	if err != nil && !errors.Is(err, store.ErrNoChange) {
		return nil, err
	}
	if created {
		return apigen.LinkTicketTo201JSONResponse(out), nil
	}
	return apigen.LinkTicketTo200JSONResponse(out), nil
}

// UnlinkTickets removes a link of the ticket in the path to a ticket of its
// team by the short key; one that does not exist is gone as well.
func (s *Server) UnlinkTickets(ctx context.Context, req apigen.UnlinkTicketsRequestObject) (apigen.UnlinkTicketsResponseObject, error) {
	t := tenantFrom(ctx)
	if err := s.unlinkByKey(ctx, t, req.Project, req.Number, req.Type, t.Slug, req.Other); err != nil {
		return nil, err
	}
	return apigen.UnlinkTickets204Response{}, nil
}

// UnlinkTicketFrom removes a link of the ticket in the path to a ticket of any
// team by its canonical key; one that does not exist is gone as well.
func (s *Server) UnlinkTicketFrom(ctx context.Context, req apigen.UnlinkTicketFromRequestObject) (apigen.UnlinkTicketFromResponseObject, error) {
	t := tenantFrom(ctx)
	if err := s.unlinkByKey(ctx, t, req.Project, req.Number, req.Type, req.OtherTeam, req.Other); err != nil {
		return nil, err
	}
	return apigen.UnlinkTicketFrom204Response{}, nil
}

// RemoveTicketLink removes a link of the ticket in the path, its source, by
// the link's id: the way to remove a link whose other end the caller may not
// see (docs/adr/0065 D5). A link the path's ticket is not the source of, or no
// link, removes nothing and answers the same.
func (s *Server) RemoveTicketLink(ctx context.Context, req apigen.RemoveTicketLinkRequestObject) (apigen.RemoveTicketLinkResponseObject, error) {
	t := tenantFrom(ctx)
	_, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		path, err := visibleTicket(ctx, w.Reader, t, req.Project, req.Number)
		if err != nil {
			return err
		}
		if perr := auth.Authorize(principal(ctx), path.role, work); perr != nil {
			return perr
		}
		return removeLink(ctx, w, t, path, req.Link)
	})
	if err != nil && !errors.Is(err, store.ErrNoChange) {
		return nil, err
	}
	return apigen.RemoveTicketLink204Response{}, nil
}

// unlinkByKey removes a link of the ticket in the path, its source — inside
// the team a relates-to from either end —, found among the path's relations by
// the other end's key: a write on the path's ticket alone, whatever the caller
// reads of the other end (docs/adr/0012 D2). A key that names no such link
// removes nothing and answers the same.
func (s *Server) unlinkByKey(ctx context.Context, t tenantScope, project string, number int, typ apigen.LinkType,
	otherTeam, other string) error {
	_, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		path, err := visibleTicket(ctx, w.Reader, t, project, number)
		if err != nil {
			return err
		}
		if perr := auth.Authorize(principal(ctx), path.role, work); perr != nil {
			return perr
		}
		key, err := domain.ParseTicketKey(other)
		if err != nil || key.Tenant != "" {
			return store.ErrNoChange
		}
		want := domain.FullKey(otherTeam, key.Project, key.Number)
		rels, err := w.RelationHeads(ctx, []uuid.UUID{path.row.ID}, store.RelationLink)
		if err != nil {
			return err
		}
		for _, rel := range rels {
			if rel.Link == nil || rel.Link.Type != domain.LinkType(typ) || rel.Head.Key() != want {
				continue
			}
			if rel.Link.Outgoing || (rel.Link.Type == domain.LinkRelatesTo && rel.Head.TeamSlug == t.Slug) {
				return removeLink(ctx, w, t, path, rel.Link.ID)
			}
		}
		return store.ErrNoChange
	})
	if err != nil && !errors.Is(err, store.ErrNoChange) {
		return err
	}
	return nil
}

// removeLink removes a link of the team whose source is the path's ticket —
// or, a relates-to inside the team, whose target it is — and records the act
// on both tickets, the other end's in its own team's record
// (docs/adr/0012 D3). Any other link is ErrNoChange.
func removeLink(ctx context.Context, w *store.Writer, t tenantScope, path ticketCtx, id uuid.UUID) error {
	l, err := w.GetLinkByID(ctx, writeq.GetLinkByIDParams{TenantID: t.ID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return store.ErrNoChange
	}
	if err != nil {
		return err
	}
	otherID := l.TargetID
	switch {
	case l.SourceID == path.row.ID:
	case l.TargetID == path.row.ID && l.Type == domain.LinkRelatesTo:
		otherID = l.SourceID
	default:
		return store.ErrNoChange
	}
	far, elsewhere, err := linkFarEnd(ctx, w, path.row.ID, id)
	if err != nil {
		return err
	}
	otherKey := ""
	if elsewhere {
		otherKey = far.Key()
	} else {
		k, err := w.LinkEndKey(ctx, writeq.LinkEndKeyParams{TenantID: t.ID, ID: otherID})
		if err != nil {
			return fmt.Errorf("read the other end of the link: %w", err)
		}
		otherKey = domain.FullKey(t.Slug, k.ProjectKey, k.Number)
	}
	if _, err := w.DeleteLinkByID(ctx, writeq.DeleteLinkByIDParams{TenantID: t.ID, ID: id}); err != nil {
		return err
	}
	sourceKey, targetKey := ticketKey(t, path.row), otherKey
	if l.SourceID != path.row.ID {
		sourceKey, targetKey = otherKey, sourceKey
	}
	payload := linkPayload(l.Type, sourceKey, targetKey)
	near := linkAct(actionUnlinked, id, payload, otherID)
	near.TicketID, near.TicketKey = path.row.ID, ticketKey(t, path.row)
	w.Record(near)
	other := linkAct(actionUnlinked, id, payload, path.row.ID)
	if elsewhere {
		return w.RecordElsewhere(ctx, far, other)
	}
	other.TicketID, other.TicketKey = otherID, otherKey
	w.Record(other)
	return nil
}

// linkFarEnd is where the act of a link of the ticket is recorded when its
// other end is a ticket of another team; false when it is the team's own.
func linkFarEnd(ctx context.Context, w *store.Writer, ticket, link uuid.UUID) (store.FarEnd, bool, error) {
	far, err := w.RelationsElsewhere(ctx, ticket)
	if err != nil {
		return store.FarEnd{}, false, err
	}
	for _, f := range far {
		if f.LinkID != nil && *f.LinkID == link {
			return f.Far, true, nil
		}
	}
	return store.FarEnd{}, false, nil
}
