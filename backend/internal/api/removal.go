package api

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/problem"
	"github.com/guided-traffic/cowork/backend/internal/store"
	"github.com/guided-traffic/cowork/backend/internal/store/writeq"
)

// A relation is removed by a writer of either end, whatever team the other
// end is in and whatever they read of it (docs/adr/0008 D2,
// docs/adr/0012 D2 as amended by the owner 2026-10-10): a child from its
// parent's side here, a link from its target's side through removeLinkOf.

// actionDetached is the act recorded on a parent when a child leaves it from
// the parent's side, or from the child's side where the parent is of another
// team (docs/adr/0026 D1).
const actionDetached = "detached"

// fieldChild names the child a detached act names.
const fieldChild = "child"

// noSuchLink is the one answer for a link id that names no link of the ticket
// in the path, whatever the reason: none, or one that does not touch it.
func noSuchLink() *problem.Error { return problem.New(problem.NotFound, "no such link") }

// noSuchChild is the one answer for a child's relation id that names no child
// of the ticket in the path, whatever the reason: none, one of another
// ticket, or one this server did not hand out.
func noSuchChild() *problem.Error { return problem.New(problem.NotFound, "no such child") }

// childHandlePrefix starts the plain text of a child's relation id.
const childHandlePrefix = "child/"

// childHandle is the id of a child's relation as /relations answers it: the
// parent and the child sealed (cursorCodec.sealPosition), so that it shows no
// id of a ticket the reader may not see. It names, it does not admit: the
// removal checks the writer and the relation as it stands.
func (s *Server) childHandle(parent, child uuid.UUID) string {
	return s.cursors.sealPosition(childHandlePrefix + parent.String() + "/" + child.String())
}

// openChildHandle is the child a child's relation id names under parent;
// false for one sealed under another ticket, or not sealed by this server.
func (s *Server) openChildHandle(handle string, parent uuid.UUID) (uuid.UUID, bool) {
	plain, ok := s.cursors.openPosition(handle)
	if !ok {
		return uuid.Nil, false
	}
	rest, ok := strings.CutPrefix(plain, childHandlePrefix)
	if !ok {
		return uuid.Nil, false
	}
	p, c, ok := strings.Cut(rest, "/")
	if !ok || p != parent.String() {
		return uuid.Nil, false
	}
	child, err := uuid.Parse(c)
	if err != nil {
		return uuid.Nil, false
	}
	return child, true
}

// RemoveTicketChild detaches a child of any project or team from the ticket
// in the path, its parent, by the id of the child's relation: a write on the
// parent, whatever the caller reads of the child. A relation that is no child
// of this ticket answers exactly as none, 404 "no such child".
func (s *Server) RemoveTicketChild(ctx context.Context, req apigen.RemoveTicketChildRequestObject) (apigen.RemoveTicketChildResponseObject, error) {
	t := tenantFrom(ctx)
	_, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		parent, err := visibleTicket(ctx, w.Reader, t, req.Project, req.Number)
		if err != nil {
			return err
		}
		if perr := auth.Authorize(principal(ctx), parent.role, work); perr != nil {
			return perr
		}
		child, ok := s.openChildHandle(req.Child, parent.row.ID)
		if !ok {
			return noSuchChild()
		}
		return detachChild(ctx, w, t, parent, child)
	})
	if err != nil {
		return nil, err
	}
	return apigen.RemoveTicketChild204Response{}, nil
}

// detachChild ends the parent relation of child to parent: a child of the
// team by the runtime role, one of another team through the crossing
// end_relation. The child's parent alone changes, its version stays, as at a
// purge; the act updated is recorded on the child — in its own team's record
// — and detached on the parent, whose progress is derived again.
func detachChild(ctx context.Context, w *store.Writer, t tenantScope, parent ticketCtx, child uuid.UUID) error {
	n, err := w.DetachChildOf(ctx, writeq.DetachChildOfParams{TenantID: t.ID, ID: child, ParentID: parent.row.ID})
	if err != nil {
		return fmt.Errorf("detach the child: %w", err)
	}
	onChild := store.Event{EntityType: entityTicket, EntityID: child, Action: actionUpdated,
		After: map[string]any{fieldParent: nil}, Refs: []uuid.UUID{parent.row.ID}}
	var childKey string
	if n == 1 {
		k, err := w.LinkEndKey(ctx, writeq.LinkEndKeyParams{TenantID: t.ID, ID: child})
		if err != nil {
			return fmt.Errorf("read the detached child: %w", err)
		}
		childKey = domain.FullKey(t.Slug, k.ProjectKey, k.Number)
		onChild.TicketID, onChild.TicketKey = child, childKey
		w.Record(onChild)
	} else {
		far, ok, err := w.EndChildElsewhere(ctx, parent.row.ID, child)
		if err != nil {
			return err
		}
		if !ok {
			return noSuchChild()
		}
		childKey = far.Key()
		if err := w.RecordElsewhere(ctx, far, onChild); err != nil {
			return err
		}
	}
	w.Record(store.Event{EntityType: entityTicket, EntityID: parent.row.ID, TicketID: parent.row.ID,
		TicketKey: ticketKey(t, parent.row), Action: actionDetached, Before: map[string]any{fieldChild: childKey},
		Refs: []uuid.UUID{child}})
	return refreshProgress(ctx, w, &parent.row.ID)
}

// removeLinkOf removes a link of the ticket in the path by its id, whichever
// end the ticket is: one the team keeps by the runtime role, one another team
// keeps with this ticket as its target through the crossing end_relation.
// removed is false for any other id — no link, one that does not touch the
// ticket, one a racing writer removed first —, and nothing is recorded.
func removeLinkOf(ctx context.Context, w *store.Writer, t tenantScope, path ticketCtx, id uuid.UUID) (removed bool, err error) {
	l, err := w.GetLinkByID(ctx, writeq.GetLinkByIDParams{TenantID: t.ID, ID: id})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return removeFarLink(ctx, w, t, path, id)
	case err != nil:
		return false, err
	case l.SourceID != path.row.ID && l.TargetID != path.row.ID:
		return false, nil
	}
	return removeLink(ctx, w, t, path, l)
}

// removeFarLink ends a link another team keeps with the ticket in the path as
// its target (end_relation) and records the act on both tickets, the source's
// in its own team's record; false where id is no such link.
func removeFarLink(ctx context.Context, w *store.Writer, t tenantScope, path ticketCtx, id uuid.UUID) (bool, error) {
	far, typ, ok, err := w.EndLinkElsewhere(ctx, path.row.ID, id)
	if err != nil || !ok {
		return false, err
	}
	pathKey := ticketKey(t, path.row)
	payload := linkPayload(typ, far.Key(), pathKey)
	if err := w.RecordElsewhere(ctx, far, linkAct(actionUnlinked, id, payload, path.row.ID)); err != nil {
		return false, err
	}
	near := linkAct(actionUnlinked, id, payload, far.Ticket())
	near.TicketID, near.TicketKey = path.row.ID, pathKey
	w.Record(near)
	return true, nil
}

// leftParent is a parent of another team a patch takes a ticket away from:
// the act detached is recorded on it in its team's record once the patch is
// written. The zero value records nothing.
type leftParent struct {
	far store.FarEnd
	ok  bool
}

// parentLeftElsewhere is the parent of another team the patch takes the
// ticket away from — cleared, or set to another —, read before the write ends
// the relation; none where the parent stays, there is none, or it is of the
// ticket's own team, whose record holds the child's act.
func parentLeftElsewhere(ctx context.Context, w *store.Writer, tc ticketCtx, next *uuid.UUID) (leftParent, error) {
	old := tc.row.ParentID
	if old == nil || (next != nil && *next == *old) {
		return leftParent{}, nil
	}
	far, err := w.RelationsElsewhere(ctx, tc.row.ID)
	if err != nil {
		return leftParent{}, err
	}
	for _, f := range far {
		if f.Kind == store.RelationParent {
			return leftParent{far: f.Far, ok: true}, nil
		}
	}
	return leftParent{}, nil
}

// record writes the act detached on the parent the ticket left, naming the
// ticket in its refs.
func (p leftParent) record(ctx context.Context, w *store.Writer, t tenantScope, tc ticketCtx) error {
	if !p.ok {
		return nil
	}
	return w.RecordElsewhere(ctx, p.far, store.Event{EntityType: entityTicket, Action: actionDetached,
		Before: map[string]any{fieldChild: ticketKey(t, tc.row)}, Refs: []uuid.UUID{tc.row.ID}})
}
