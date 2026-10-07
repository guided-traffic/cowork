package api

import (
	"context"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/github"
	"github.com/guided-traffic/cowork/backend/internal/markdown"
	"github.com/guided-traffic/cowork/backend/internal/store"
	"github.com/guided-traffic/cowork/backend/internal/store/readq"
)

// The bounds of the context's lists: the links and the prerequisite tree a
// document shows, the attachments it lists, and the comments and acts a
// request may ask for (the document's maximum, docs/adr/0044 D2).
const (
	contextDefault         = 10
	maxContextLinks        = 200
	maxContextNodes        = 200
	maxContextFiles        = 200
	maxContextPullRequests = 200
	maxContextEntries      = 100
)

// contextResponse answers the context as UTF-8 Markdown, without an ETag: it
// is not one entity (docs/adr/0044 Residual risks).
type contextResponse struct{ body []byte }

func (m contextResponse) VisitExportTicketContextResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(m.body)))
	w.WriteHeader(http.StatusOK)
	_, err := w.Write(m.body)
	return err
}

// ExportTicketContext answers a ticket for reading: the canonical document and
// what surrounds it (docs/adr/0044 D2). What the caller cannot see is absent,
// and every call is recorded as data leaving the system (D5).
func (s *Server) ExportTicketContext(ctx context.Context, req apigen.ExportTicketContextRequestObject) (apigen.ExportTicketContextResponseObject, error) {
	t := tenantFrom(ctx)
	p := principal(ctx)
	if perr := auth.Authorize(p, t.Role, read); perr != nil {
		return nil, perr
	}
	comments, acts := countOf(req.Params.Comments), countOf(req.Params.Activity)
	doc := markdown.Context{Exported: s.h.opts.Now(), By: p.DisplayName, Agent: p.Agent}
	if p.TokenID != uuid.Nil {
		doc.Token = &markdown.Token{Name: p.TokenName}
	}
	var tc ticketCtx
	err := s.db.InTenant(ctx, t.ID, func(r *store.Reader) error {
		var err error
		if tc, err = visibleTicket(ctx, r, t, req.Project, req.Number); err != nil {
			return err
		}
		if doc.Ticket, err = exportDocument(ctx, r, t, tc); err != nil {
			return err
		}
		if err := contextLinks(ctx, r, t, tc, &doc); err != nil {
			return err
		}
		if err := contextTree(ctx, r, t, tc, &doc); err != nil {
			return err
		}
		if err := contextAttachments(ctx, r, t, tc, &doc); err != nil {
			return err
		}
		if err := contextPullRequests(ctx, r, t, tc, &doc); err != nil {
			return err
		}
		if err := contextComments(ctx, r, t, tc, comments, &doc); err != nil {
			return err
		}
		return contextActivity(ctx, r, t, tc, acts, &doc)
	})
	if err != nil {
		return nil, err
	}
	_, err = s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		w.Record(store.Event{EntityType: entityTicket, EntityID: tc.row.ID, TicketID: tc.row.ID, TicketKey: doc.Ticket.Key,
			Action: "exported", After: map[string]any{"format": "context v1", fieldVersion: tc.row.Version}})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return contextResponse{body: markdown.RenderContext(doc)}, nil
}

// countOf is a requested number of entries, the default when none is asked,
// held to the document's maximum.
func countOf(n *int) int32 {
	switch {
	case n == nil:
		return contextDefault
	case *n <= 0:
		return 0
	case *n >= maxContextEntries:
		return maxContextEntries
	}
	return int32(*n) // #nosec G115 -- between 1 and 99 here
}

func contextLinks(ctx context.Context, r *store.Reader, t tenantScope, tc ticketCtx, doc *markdown.Context) error {
	rows, err := r.ContextLinks(ctx, readq.ContextLinksParams{TenantID: t.ID, TicketID: tc.row.ID, PageSize: maxContextLinks})
	for _, l := range rows {
		doc.Links = append(doc.Links, markdown.Link{Name: l.Type.Name(l.Outgoing),
			Key: domain.FullKey(t.Slug, l.OtherProjectKey, l.OtherNumber), Title: l.OtherTitle, State: string(l.OtherState),
			Assignee: deref(l.OtherAssigneeName)})
	}
	return err
}

// contextTree reads the prerequisite tree of the route (docs/adr/0012 D6) and
// shows each prerequisite once: a ticket the tree repeats under a second
// ticket it blocks is left out there.
func contextTree(ctx context.Context, r *store.Reader, t tenantScope, tc ticketCtx, doc *markdown.Context) error {
	rows, err := ticketTree(ctx, r, t.ID, tc.row.ID, false, nil, maxContextNodes)
	for _, n := range rows {
		if n.Repeated {
			continue
		}
		v := treeNodeView(t.Slug, n)
		doc.Prerequisites = append(doc.Prerequisites, markdown.Prerequisite{Depth: v.Depth, Key: v.Key, Title: v.Title,
			State: string(v.State), Assignee: deref(n.AssigneeName), Progress: v.Progress})
	}
	return err
}

func contextAttachments(ctx context.Context, r *store.Reader, t tenantScope, tc ticketCtx, doc *markdown.Context) error {
	rows, err := r.ListAttachments(ctx, readq.ListAttachmentsParams{TenantID: t.ID, TicketID: tc.row.ID, PageSize: maxContextFiles})
	for _, a := range rows {
		v := attachmentView(t, tc, attachment(a))
		doc.Attachments = append(doc.Attachments, markdown.Attachment{Name: v.FileName, Type: string(v.ContentType),
			Size: v.Size, URL: v.ContentUrl})
	}
	return err
}

// contextPullRequests reads what GitHub's webhook linked to the ticket
// (docs/adr/0071 D6), oldest link first, under the ticket's predicate.
func contextPullRequests(ctx context.Context, r *store.Reader, t tenantScope, tc ticketCtx, doc *markdown.Context) error {
	rows, err := r.ListTicketPullRequests(ctx, readq.ListTicketPullRequestsParams{TenantID: t.ID, TicketID: tc.row.ID,
		PageSize: maxContextPullRequests})
	for _, p := range rows {
		pr := markdown.PullRequest{Repository: p.Repository, Title: p.Title, State: p.State, Author: deref(p.Author),
			URL: p.Url, From: foundInWord(p.Kind, p.FoundIn), MergedAt: p.MergedAt, SHA: deref(p.Sha)}
		if p.Number != nil {
			pr.Number = int(*p.Number)
		}
		doc.PullRequests = append(doc.PullRequests, pr)
	}
	return err
}

// foundInTitle is where a reader finds a pull request's short key: its title,
// which the stored found_in calls the subject as it does a commit's.
const foundInTitle = "title"

// foundInWord says where a key was read as a reader names it: the title of a
// pull request, the subject of a commit.
func foundInWord(kind, foundIn string) string {
	if foundIn == github.FoundInSubject && kind == entityPullRequest {
		return foundInTitle
	}
	return foundIn
}

// contextComments reads the last n comments and shows them oldest first.
func contextComments(ctx context.Context, r *store.Reader, t tenantScope, tc ticketCtx, n int32, doc *markdown.Context) error {
	if n == 0 {
		return nil
	}
	rows, err := r.ListComments(ctx, readq.ListCommentsParams{TenantID: t.ID, TicketID: tc.row.ID, Descending: true,
		PageSize: n})
	doc.Comments = make([]markdown.Comment, 0, len(rows))
	for i := len(rows) - 1; i >= 0; i-- {
		c := commentView(comment(rows[i]))
		mc := markdown.Comment{Author: c.Author.DisplayName, Token: contextToken(c.Token), At: c.CreatedAt, Withdrawn: c.Withdrawn}
		if agent, err := c.Agent.Get(); err == nil {
			mc.Agent = agent
		}
		if body, err := c.Body.Get(); err == nil {
			mc.Body = body
		}
		doc.Comments = append(doc.Comments, mc)
	}
	return err
}

// contextActivity reads the last n acts and shows them oldest first; an act
// that names a ticket the caller cannot see shows without its payload.
func contextActivity(ctx context.Context, r *store.Reader, t tenantScope, tc ticketCtx, n int32, doc *markdown.Context) error {
	if n == 0 {
		return nil
	}
	rows, err := r.ListTicketActivity(ctx, readq.ListTicketActivityParams{TenantID: &t.ID, TicketID: &tc.row.ID,
		Descending: true, PageSize: n})
	if err != nil {
		return err
	}
	visible := map[uuid.UUID]bool{}
	var refs []uuid.UUID
	for _, a := range rows {
		refs = append(refs, a.Refs...)
	}
	if len(refs) > 0 {
		ids, err := r.VisibleTickets(ctx, readq.VisibleTicketsParams{TenantID: t.ID, Ids: refs})
		if err != nil {
			return err
		}
		for _, id := range ids {
			visible[id] = true
		}
	}
	doc.Activity = make([]markdown.Act, 0, len(rows))
	for i := len(rows) - 1; i >= 0; i-- {
		v := activityView(rows[i], visible)
		act := markdown.Act{At: v.At, Action: string(v.Action), Token: contextToken(v.Token), Redacted: v.Redacted}
		if actor, err := v.Actor.Get(); err == nil {
			act.Actor = actor.DisplayName
		} else if system, err := v.ActorSystem.Get(); err == nil {
			act.Actor = system
		}
		if agent, err := v.Agent.Get(); err == nil {
			act.Agent = agent
		}
		act.Before, act.After = payload(v.Before), payload(v.After)
		if reason, err := v.Reason.Get(); err == nil {
			act.Reason = reason
		}
		if note, err := v.Note.Get(); err == nil {
			act.Note = note
		}
		doc.Activity = append(doc.Activity, act)
	}
	return nil
}

// contextToken is the token an act came through as the context names it: by
// its name, which an act recorded before the name was kept lacks; nil for a
// browser session's act (docs/adr/0036 D6).
func contextToken(v nullable.Nullable[apigen.TokenMark]) *markdown.Token {
	tok, err := v.Get()
	if err != nil {
		return nil
	}
	name, _ := tok.Name.Get()
	return &markdown.Token{Name: name}
}

// payload is an act's changed fields; none, or a redacted payload, is nil.
func payload(v interface {
	Get() (map[string]any, error)
}) map[string]any {
	m, err := v.Get()
	if err != nil {
		return nil
	}
	return m
}
