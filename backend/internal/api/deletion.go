package api

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/problem"
	"github.com/guided-traffic/cowork/backend/internal/storage"
	"github.com/guided-traffic/cowork/backend/internal/store"
	"github.com/guided-traffic/cowork/backend/internal/store/readq"
	"github.com/guided-traffic/cowork/backend/internal/store/writeq"
)

// deletion is what deleting, restoring and purging a ticket need: a tenant
// administrator with admin scope, never an agent (docs/adr/0024 D7,
// docs/adr/0043 D3).
var deletion = auth.Need{Role: domain.RoleAdmin, Scope: domain.ScopeAdmin, HardOff: auth.HardOffDeletion}

// DeleteTicket puts a ticket into its tenant's bin (docs/adr/0024 D1): from
// then on it answers like a missing one everywhere but the bin.
func (s *Server) DeleteTicket(ctx context.Context, req apigen.DeleteTicketRequestObject) (apigen.DeleteTicketResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, deletion); perr != nil {
		return nil, perr
	}
	_, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		tc, err := visibleTicket(ctx, w.Reader, t, req.Project, req.Number)
		if err != nil {
			return err
		}
		_, err = w.MarkTicketDeleted(ctx, writeq.MarkTicketDeletedParams{TenantID: t.ID, ID: tc.row.ID,
			DeletedBy: principal(ctx).PersonID})
		if errors.Is(err, pgx.ErrNoRows) {
			// A concurrent deletion came first: the ticket is gone for this
			// request as for any later one.
			return problem.New(problem.NotFound, "no such ticket")
		}
		if err != nil {
			return err
		}
		if err := refreshProgress(ctx, w, t, tc.row.ParentID); err != nil {
			return err
		}
		w.Record(store.Event{EntityType: entityTicket, EntityID: tc.row.ID, TicketID: tc.row.ID,
			TicketKey: domain.FullKey(t.Slug, tc.project.Key, tc.row.Number), Action: actionDeleted})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return apigen.DeleteTicket204Response{}, nil
}

// ListDeletedTickets answers a page of the tenant's bin, the last deleted
// first (docs/adr/0024 D1): the one list in which a deleted ticket exists. A
// poll that finds it unchanged is a 304 (docs/adr/0054 D7).
func (s *Server) ListDeletedTickets(ctx context.Context, req apigen.ListDeletedTicketsRequestObject) (apigen.ListDeletedTicketsResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, adminRead); perr != nil {
		return nil, perr
	}
	const op = "listDeletedTickets"
	scope := t.ID.String()
	size := s.h.pageSize(req.Params.Limit)
	params := readq.ListDeletedTicketsParams{TenantID: t.ID, PageSize: limitArg(size)}
	if req.Params.Cursor != nil {
		var perr *problem.Error
		if params.BeforeAt, params.BeforeID, perr = s.binPosition(op, scope, *req.Params.Cursor); perr != nil {
			return nil, perr
		}
	}
	var rows []readq.ListDeletedTicketsRow
	err := s.db.InTenant(ctx, t.ID, func(r *store.Reader) error {
		var err error
		rows, err = r.ListDeletedTickets(ctx, params)
		return err
	})
	if err != nil {
		return nil, err
	}
	rows, next := page(s.h, rows, size, op, scope, func(r readq.ListDeletedTicketsRow) string {
		return deletedAt(r).Format(time.RFC3339Nano) + "/" + r.ID.String()
	})
	out := apigen.DeletedTicketList{Items: make([]apigen.DeletedTicket, 0, len(rows)), NextCursor: nullableString(next)}
	for _, r := range rows {
		by := apigen.Person{Id: uuid.Nil, Username: nullableOf[string](nil)}
		if r.DeletedBy != nil {
			by = personView(*r.DeletedBy, r.DeletedByUsername, r.DeletedByName)
		}
		out.Items = append(out.Items, apigen.DeletedTicket{
			Key: domain.FullKey(t.Slug, r.ProjectKey, r.Number), Project: r.ProjectKey, Number: int(r.Number),
			Type: apigen.TicketType(r.Type), Title: r.Title, State: apigen.TicketState(r.State), Confidential: r.Confidential,
			DeletedAt: deletedAt(r), DeletedBy: by, PurgeAt: deletedAt(r).Add(store.PurgeAfter),
		})
	}
	tag, unchanged := listTag(req.Params.IfNoneMatch, out)
	if unchanged {
		return apigen.ListDeletedTickets304Response{Headers: apigen.NotModifiedResponseHeaders{ETag: &tag}}, nil
	}
	return apigen.ListDeletedTickets200JSONResponse{Body: out, Headers: apigen.ListDeletedTickets200ResponseHeaders{ETag: &tag}}, nil
}

func deletedAt(r readq.ListDeletedTicketsRow) time.Time {
	if r.DeletedAt == nil {
		return time.Time{}
	}
	return *r.DeletedAt
}

// binPosition reads the bin's cursor: the deletion time and the id of the
// last ticket of the previous page.
func (s *Server) binPosition(op, scope, cursor string) (*time.Time, *uuid.UUID, *problem.Error) {
	pos, perr := s.cursors.decode(op, scope, cursor)
	if perr != nil {
		return nil, nil, perr
	}
	at, id, ok := strings.Cut(pos, "/")
	when, err1 := time.Parse(time.RFC3339Nano, at)
	after, err2 := uuid.Parse(id)
	if !ok || err1 != nil || err2 != nil {
		return nil, nil, invalidCursor()
	}
	return &when, &after, nil
}

// RestoreTicket brings a ticket back from the bin as it was (docs/adr/0024 D1).
func (s *Server) RestoreTicket(ctx context.Context, req apigen.RestoreTicketRequestObject) (apigen.RestoreTicketResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, deletion); perr != nil {
		return nil, perr
	}
	var out store.TicketRow
	_, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		d, err := deletedTicket(ctx, w.Reader, t, req.Key)
		if err != nil {
			return err
		}
		if _, err := w.RestoreTicket(ctx, writeq.RestoreTicketParams{TenantID: t.ID, ID: d.ID}); errors.Is(err, pgx.ErrNoRows) {
			return problem.New(problem.NotFound, "no such deleted ticket")
		} else if err != nil {
			return err
		}
		if err := refreshProgress(ctx, w, t, d.ParentID); err != nil {
			return err
		}
		w.Record(store.Event{EntityType: entityTicket, EntityID: d.ID, TicketID: d.ID,
			TicketKey: domain.FullKey(t.Slug, d.ProjectKey, d.Number), Action: "restored"})
		out, err = reread(ctx, w, t, d.ID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return apigen.RestoreTicket200JSONResponse{Body: ticketView(t, out), Headers: apigen.RestoreTicket200ResponseHeaders{ETag: etag(out.Version)}}, nil
}

// PurgeTicket removes a deleted ticket for good before its thirty days have
// passed (docs/adr/0024 D2), and its attachment objects once that committed.
func (s *Server) PurgeTicket(ctx context.Context, req apigen.PurgeTicketRequestObject) (apigen.PurgeTicketResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, deletion); perr != nil {
		return nil, perr
	}
	var purged store.Purged
	_, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		d, err := deletedTicket(ctx, w.Reader, t, req.Key)
		if err != nil {
			return err
		}
		purged, err = w.PurgeTicket(ctx, t.Slug, d.ID)
		if errors.Is(err, store.ErrNotFound) {
			return problem.New(problem.NotFound, "no such deleted ticket")
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	RemovePurgedObjects(context.WithoutCancel(ctx), s.storage, s.h.logger, []store.Purged{purged})
	return apigen.PurgeTicket204Response{}, nil
}

// deletedTicket reads a ticket of the bin by its short key: deleted, and one
// the caller can see. Any other key — a ticket that is not deleted included —
// is 404.
func deletedTicket(ctx context.Context, r *store.Reader, t tenantScope, raw string) (readq.GetDeletedTicketRow, error) {
	key, err := domain.ParseTicketKey(raw)
	if err != nil || key.Tenant != "" {
		return readq.GetDeletedTicketRow{}, problem.New(problem.NotFound, "no such deleted ticket")
	}
	row, err := r.GetDeletedTicket(ctx, readq.GetDeletedTicketParams{TenantID: t.ID, ProjectKey: key.Project, Number: key.Number})
	if errors.Is(err, pgx.ErrNoRows) {
		return row, problem.New(problem.NotFound, "no such deleted ticket")
	}
	return row, err
}

// RemovePurgedObjects removes the attachment objects of purged tickets, whose
// rows are gone: after the commit, because a rollback would otherwise leave
// rows that name missing bytes (docs/adr/0024 D2). A failure leaves an object
// that no row names and is logged with its key; without object storage the
// objects stay where an earlier configuration put them.
func RemovePurgedObjects(ctx context.Context, objects *storage.Client, logger *slog.Logger, purged []store.Purged) {
	for _, p := range purged {
		if len(p.Attachments) == 0 {
			continue
		}
		if objects == nil {
			logger.Warn("a purged ticket had attachments, and no object storage is configured to remove them from",
				"ticket", p.Key, "attachments", len(p.Attachments))
			continue
		}
		for _, id := range p.Attachments {
			if err := objects.Delete(ctx, storage.Key(p.TenantID, id)); err != nil {
				logger.Error("an attachment object of a purged ticket could not be removed", "ticket", p.Key,
					"object", storage.Key(p.TenantID, id), "error", err)
			}
		}
	}
}
