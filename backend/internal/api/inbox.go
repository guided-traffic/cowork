package api

import (
	"bytes"
	"context"
	"errors"
	"slices"
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
	entityNotification  = "notification"
	entityNotifications = "notifications"
	actionRead          = "read"
)

// markRead is what marking one's own notifications read needs: a write, of
// any member, in the agent baseline — it is the person's own inbox.
var markRead = auth.Need{Role: domain.RoleViewer, Scope: domain.ScopeWrite}

// personTenant is one of the person's tenants a person-level route reads.
type personTenant struct {
	id         uuid.UUID
	slug, name string
	role       domain.Role
}

func (t personTenant) ref() apigen.TeamRef { return apigen.TeamRef{Slug: t.slug, Name: t.name} }

// teamQuery is the team a person-level route is narrowed to: ?team=, or
// ?tenant=, the name it had before (docs/adr/0005 D1), taken until a later
// release removes it (docs/adr/0046 D7). The two are one narrowing, so a
// request that names both is refused at query:tenant whatever the values, as
// a list that names horizon and urgency is.
func teamQuery(team, tenant *string) (*string, *problem.Error) {
	if team != nil && tenant != nil {
		return nil, problem.Field("query:tenant", "tenant is the deprecated name of team: send team alone")
	}
	if team != nil {
		return team, nil
	}
	return tenant, nil
}

// scope is the tenant as the tenant's own routes hold it, for the views they
// share.
func (t personTenant) scope() tenantScope {
	return tenantScope{ID: t.id, Slug: t.slug, Name: t.name, Role: t.role}
}

// personTenants are the tenants a person-level route reads, in the order of
// their slugs (docs/adr/0023 D2): the person's, a restricted token's own only
// (docs/adr/0035 D3), and of those the one narrow names. A narrow that names
// none of them answers like the boundary, whether or not the tenant exists
// (D5).
func (h *handler) personTenants(ctx context.Context, narrow *string) ([]personTenant, error) {
	p := principal(ctx)
	var out []personTenant
	err := h.opts.DB.Installation(ctx, func(r *store.Reader) error {
		rows, err := r.ListMembershipsOfUser(ctx, p.PersonID)
		for _, m := range rows {
			if (restricted(p) && m.TenantID != p.RestrictedTenantID) || (narrow != nil && m.Slug != *narrow) {
				continue
			}
			out = append(out, personTenant{id: m.TenantID, slug: m.Slug, name: m.Name, role: m.Role})
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	if narrow != nil && len(out) == 0 {
		return nil, problem.New(problem.NotFound, "no such team")
	}
	slices.SortFunc(out, func(a, b personTenant) int { return strings.Compare(a.slug, b.slug) })
	return out, nil
}

// unread counts the person's unread notifications in the tenants, one read
// per tenant (docs/adr/0021 D5), over what the inbox shows.
func (h *handler) unread(ctx context.Context, tenants []personTenant) (int, error) {
	p := principal(ctx)
	total := 0
	for _, t := range tenants {
		err := h.opts.DB.InTenant(ctx, t.id, func(r *store.Reader) error {
			n, err := r.CountUnread(ctx, readq.CountUnreadParams{TenantID: t.id, UserID: p.PersonID})
			total += int(n)
			return err
		})
		if err != nil {
			return 0, err
		}
	}
	return total, nil
}

// inboxRow is a notification with the tenant it was read in.
type inboxRow struct {
	tenant personTenant
	row    readq.ListInboxRow
}

// ListMyInbox answers the person's notifications across their tenants,
// newest first, and the unread count (docs/adr/0020 D1): each tenant's page
// read in that tenant, the pages merged by id, which orders by time across
// tenants (docs/adr/0022 D5).
func (s *Server) ListMyInbox(ctx context.Context, req apigen.ListMyInboxRequestObject) (apigen.ListMyInboxResponseObject, error) {
	p := principal(ctx)
	const op = "listMyInbox"
	narrow, perr := teamQuery(req.Params.Team, req.Params.Tenant) //nolint:staticcheck // SA1019: deprecated in the document, taken as team until a later release removes it
	if perr != nil {
		return nil, perr
	}
	scope := p.PersonID.String() + "/" + deref(narrow)
	before, err := s.uuidAfter(op, scope, req.Params.Cursor)
	if err != nil {
		return nil, err
	}
	tenants, err := s.h.personTenants(ctx, narrow)
	if err != nil {
		return nil, err
	}
	size := s.h.pageSize(req.Params.Limit)
	var rows []inboxRow
	visible := map[uuid.UUID]bool{}
	unread := 0
	for _, t := range tenants {
		err := s.db.InTenant(ctx, t.id, func(r *store.Reader) error {
			page, err := r.ListInbox(ctx, readq.ListInboxParams{TenantID: t.id, UserID: p.PersonID, Before: before, PageSize: limitArg(size)})
			if err != nil {
				return err
			}
			n, err := r.CountUnread(ctx, readq.CountUnreadParams{TenantID: t.id, UserID: p.PersonID})
			if err != nil {
				return err
			}
			unread += int(n)
			var refs []uuid.UUID
			for _, row := range page {
				rows = append(rows, inboxRow{tenant: t, row: row})
				refs = append(refs, row.Refs...)
			}
			if len(refs) == 0 {
				return nil
			}
			ids, err := r.VisibleTickets(ctx, readq.VisibleTicketsParams{TenantID: t.id, Ids: refs})
			for _, id := range ids {
				visible[id] = true
			}
			return err
		})
		if err != nil {
			return nil, err
		}
	}
	slices.SortFunc(rows, func(a, b inboxRow) int { return bytes.Compare(b.row.ID[:], a.row.ID[:]) })
	rows, next := page(s.h, rows, size, op, scope, func(r inboxRow) string { return r.row.ID.String() })
	out := apigen.InboxList{Items: make([]apigen.InboxEntry, 0, len(rows)), NextCursor: nullableString(next), Unread: unread}
	for _, r := range rows {
		out.Items = append(out.Items, inboxEntryView(r, visible))
	}
	tag, unchanged := listTag(req.Params.IfNoneMatch, out)
	if unchanged {
		return apigen.ListMyInbox304Response{Headers: apigen.NotModifiedResponseHeaders{ETag: &tag}}, nil
	}
	return apigen.ListMyInbox200JSONResponse{Body: out, Headers: apigen.ListMyInbox200ResponseHeaders{ETag: &tag}}, nil
}

// inboxEntryView renders a notification from its act (docs/adr/0020 D3): the
// tickets as they are now, and the act as the ticket's activity shows it,
// without its payload where it names a ticket the person cannot see.
func inboxEntryView(r inboxRow, visible map[uuid.UUID]bool) apigen.InboxEntry {
	n, slug := r.row, r.tenant.slug
	act := activityView(readq.ListTicketActivityRow{
		ID: n.ActID, ActorUserID: n.ActorUserID, ActorUsername: n.ActorUsername, ActorName: n.ActorName,
		ActorSystem: n.ActorSystem, Agent: n.Agent, TokenID: n.TokenID, TokenName: n.TokenName, EntityType: n.EntityType,
		EntityID: n.EntityID, Action: n.Action, Before: n.Before, After: n.After, Reason: n.ActReason, Note: n.ActNote,
		ExplainedByCommentID: n.ExplainedByCommentID, Refs: n.Refs, CreatedAt: n.ActAt,
	}, visible)
	v := apigen.InboxEntry{
		Id: n.ID, Team: r.tenant.ref(), Reason: apigen.InboxReason(n.Reason), Act: act,
		Tenant:  r.tenant.ref(), //nolint:staticcheck // SA1019: deprecated in the document, answered beside team until a later release removes it
		Ticket:  apigen.TicketRef{Key: domain.FullKey(slug, n.ProjectKey, n.Number), Title: n.Title, State: apigen.TicketState(n.State)},
		Blocker: nullableOf[apigen.TicketRef](nil), Withdrawn: n.Withdrawn, Read: n.ReadAt != nil, CreatedAt: n.CreatedAt,
	}
	// A prerequisite of another team is named by the act's refs alone; the
	// blocked ticket's relations show it by its head (docs/adr/0012 D5).
	if n.Reason == store.NoticeBlockerClosed && n.Action != actionPrerequisiteSettled {
		v.Blocker = nullableOf(&apigen.TicketRef{Key: domain.FullKey(slug, n.ActProjectKey, n.ActNumber), Title: n.ActTitle,
			State: apigen.TicketState(n.ActState)})
	}
	return v
}

// MarkNotificationRead marks one of the person's notifications read, in the
// tenant it belongs to — found by one read per tenant, never one query across
// them (docs/adr/0021 D5). One already read changes nothing.
func (s *Server) MarkNotificationRead(ctx context.Context, req apigen.MarkNotificationReadRequestObject) (apigen.MarkNotificationReadResponseObject, error) {
	p := principal(ctx)
	if perr := auth.Authorize(p, domain.RoleViewer, markRead); perr != nil {
		return nil, perr
	}
	tenants, err := s.h.personTenants(ctx, nil)
	if err != nil {
		return nil, err
	}
	var (
		home        *personTenant
		alreadyRead bool
	)
	for i, t := range tenants {
		err := s.db.InTenant(ctx, t.id, func(r *store.Reader) error {
			var err error
			alreadyRead, err = r.FindNotification(ctx, readq.FindNotificationParams{TenantID: t.id, UserID: p.PersonID, ID: req.Notification})
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			home = &tenants[i]
			return err
		})
		if err != nil {
			return nil, err
		}
		if home != nil {
			break
		}
	}
	if home == nil {
		return nil, problem.New(problem.NotFound, "no such notification")
	}
	if !alreadyRead {
		_, err := s.db.Mutate(ctx, home.id, func(w *store.Writer) error {
			n, err := w.MarkNotificationRead(ctx, writeq.MarkNotificationReadParams{TenantID: home.id, UserID: p.PersonID,
				ID: req.Notification, Now: s.h.opts.Now()})
			if err != nil {
				return err
			}
			if n == 0 {
				return store.ErrNoChange
			}
			w.Record(store.Event{EntityType: entityNotification, EntityID: req.Notification, Action: actionRead, InboxOf: p.PersonID})
			return nil
		})
		if err != nil && !errors.Is(err, store.ErrNoChange) {
			return nil, err
		}
	}
	unread, err := s.h.unread(ctx, tenants)
	if err != nil {
		return nil, err
	}
	return apigen.MarkNotificationRead200JSONResponse{Unread: unread}, nil
}

// MarkMyInboxRead marks every unread notification of the person up to and
// including the one given read, tenant by tenant (docs/adr/0020 D6): one act
// per tenant where something changed.
func (s *Server) MarkMyInboxRead(ctx context.Context, req apigen.MarkMyInboxReadRequestObject) (apigen.MarkMyInboxReadResponseObject, error) {
	p := principal(ctx)
	if perr := auth.Authorize(p, domain.RoleViewer, markRead); perr != nil {
		return nil, perr
	}
	narrow, perr := teamQuery(req.Params.Team, req.Params.Tenant) //nolint:staticcheck // SA1019: deprecated in the document, taken as team until a later release removes it
	if perr != nil {
		return nil, perr
	}
	tenants, err := s.h.personTenants(ctx, narrow)
	if err != nil {
		return nil, err
	}
	through := req.Body.Through
	for _, t := range tenants {
		_, err := s.db.Mutate(ctx, t.id, func(w *store.Writer) error {
			n, err := w.MarkInboxRead(ctx, writeq.MarkInboxReadParams{TenantID: t.id, UserID: p.PersonID, Through: through,
				Now: s.h.opts.Now()})
			if err != nil {
				return err
			}
			if n == 0 {
				return store.ErrNoChange
			}
			w.Record(store.Event{EntityType: entityNotifications, Action: actionRead,
				After: map[string]any{"read": n, "through": through}, InboxOf: p.PersonID})
			return nil
		})
		if err != nil && !errors.Is(err, store.ErrNoChange) {
			return nil, err
		}
	}
	unread, err := s.h.unread(ctx, tenants)
	if err != nil {
		return nil, err
	}
	return apigen.MarkMyInboxRead200JSONResponse{Unread: unread}, nil
}
