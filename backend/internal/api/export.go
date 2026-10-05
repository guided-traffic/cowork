package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/markdown"
	"github.com/guided-traffic/cowork/backend/internal/store"
	"github.com/guided-traffic/cowork/backend/internal/store/readq"
)

// markdownResponse answers the export as UTF-8 Markdown; the generated
// response would send the media type without its charset.
type markdownResponse struct {
	body []byte
	etag string
}

func (m markdownResponse) VisitExportTicketResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set(headerETag, m.etag)
	w.Header().Set("Content-Length", strconv.Itoa(len(m.body)))
	w.WriteHeader(http.StatusOK)
	_, err := w.Write(m.body)
	return err
}

// ExportTicket answers the canonical Markdown of a ticket, grammar v1
// (docs/adr/0044 D1); every call is recorded as data leaving the system
// (docs/adr/0044 D5, docs/adr/0026 D5).
func (s *Server) ExportTicket(ctx context.Context, req apigen.ExportTicketRequestObject) (apigen.ExportTicketResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, read); perr != nil {
		return nil, perr
	}
	var doc markdown.Ticket
	var tc ticketCtx
	err := s.db.InTenant(ctx, t.ID, func(r *store.Reader) error {
		var err error
		if tc, err = visibleTicket(ctx, r, t, req.Project, req.Number); err != nil {
			return err
		}
		doc, err = exportDocument(ctx, r, t, tc)
		return err
	})
	if err != nil {
		return nil, err
	}
	_, err = s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		w.Record(store.Event{EntityType: entityTicket, EntityID: tc.row.ID, TicketID: tc.row.ID, TicketKey: doc.Key,
			Action: "exported", After: map[string]any{"format": "markdown v1", fieldVersion: tc.row.Version}})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return markdownResponse{body: markdown.Render(doc), etag: *etag(tc.row.Version)}, nil
}

// exportDocument gathers what the document shows.
func exportDocument(ctx context.Context, r *store.Reader, t tenantScope, tc ticketCtx) (markdown.Ticket, error) {
	row := tc.row
	stages := stagesOf(row)
	doc := markdown.Ticket{
		Key: ticketKey(t, row), Title: row.Title, Type: string(row.Type), State: string(row.State), Severity: string(row.Severity),
		Security: string(row.Security), Threat: deref(row.Threat), Horizon: string(horizonOf(row)), Effort: string(row.Effort),
		ProgressRefinement: stages.Refinement, Progress: stages.Implementation, ProgressReview: stages.Review,
		Opened: row.OpenedAt, Decided: row.DecidedAt, Done: row.DoneAt, Body: row.Body,
	}
	if row.AssigneeName != nil {
		doc.Assignee = *row.AssigneeName
	}
	if row.ParentNumber != nil {
		doc.Parent = domain.FullKey(t.Slug, row.ProjectKey, *row.ParentNumber)
	}
	if err := stateNote(ctx, r, t, row, &doc); err != nil {
		return doc, err
	}
	names, err := r.ExportAttachmentNames(ctx, readq.ExportAttachmentNamesParams{TenantID: t.ID, TicketID: row.ID})
	if err != nil {
		return doc, err
	}
	doc.Attachments = names
	questions, err := r.ExportQuestions(ctx, readq.ExportQuestionsParams{TenantID: t.ID, TicketID: row.ID})
	if err != nil {
		return doc, err
	}
	for _, q := range questions {
		doc.Questions = append(doc.Questions, markdown.Question{Number: int(q.Number), Question: q.Question, Options: q.Options,
			Recommendation: q.Recommendation, Status: q.Status, Answer: deref(q.Answer)})
	}
	return doc, nil
}

// stateNote fills the note of the state the ticket is in: the verification
// note of done, the reason of dropped, the block of blocked
// (docs/adr/0009 D2, D5).
func stateNote(ctx context.Context, r *store.Reader, t tenantScope, row store.TicketRow, doc *markdown.Ticket) error {
	switch row.State {
	case domain.StateBlocked:
		if row.BlockKind != nil {
			doc.BlockedBy = string(*row.BlockKind)
		}
		doc.BlockedReason = deref(row.BlockReason)
		if row.BlockedFrom != nil {
			doc.BlockedFrom = string(*row.BlockedFrom)
		}
		return nil
	case domain.StateDone, domain.StateDropped:
		n, err := r.TransitionNote(ctx, readq.TransitionNoteParams{TenantID: &t.ID, TicketID: &row.ID, State: string(row.State)})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if row.State == domain.StateDone {
			doc.Shipped = n.Note
		} else {
			doc.DroppedReason = n.Reason
		}
	}
	return nil
}
