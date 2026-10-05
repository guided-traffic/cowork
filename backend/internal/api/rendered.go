package api

import (
	"context"

	"github.com/google/uuid"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/richtext"
	"github.com/guided-traffic/cowork/backend/internal/store"
	"github.com/guided-traffic/cowork/backend/internal/store/readq"
)

// ticketAt is where a ticket's paths start: its project's key and its number.
type ticketAt struct {
	project string
	number  int32
}

// attachmentContentURL is the path an attachment's bytes are delivered at,
// through the backend only (docs/adr/0016 D4).
func attachmentContentURL(t tenantScope, at ticketAt, id uuid.UUID) string {
	return ticketURL(t, at.project, at.number) + "/attachments/" + id.String() + "/content"
}

// imagesOf reads, for each of the tickets, the attachments its rendered texts
// may show as images: the raster ones, delivered inline (docs/adr/0016 D5, D7),
// each with the path of its bytes. Read through the tickets' predicate.
func imagesOf(ctx context.Context, r *store.Reader, t tenantScope, tickets map[uuid.UUID]ticketAt) (map[uuid.UUID]richtext.Images, error) {
	out := map[uuid.UUID]richtext.Images{}
	if len(tickets) == 0 {
		return out, nil
	}
	ids := make([]uuid.UUID, 0, len(tickets))
	for id := range tickets {
		ids = append(ids, id)
	}
	rows, err := r.ListTicketImages(ctx, readq.ListTicketImagesParams{TenantID: t.ID, TicketIds: ids})
	if err != nil {
		return nil, err
	}
	for _, a := range rows {
		if !domain.InlineAttachment(a.ContentType) {
			continue
		}
		if out[a.TicketID] == nil {
			out[a.TicketID] = richtext.Images{}
		}
		out[a.TicketID][a.ID] = attachmentContentURL(t, tickets[a.TicketID], a.ID)
	}
	return out, nil
}

// ticketImages are the images the rendered texts of one ticket may show.
func ticketImages(ctx context.Context, r *store.Reader, t tenantScope, tc ticketCtx) (richtext.Images, error) {
	all, err := imagesOf(ctx, r, t, map[uuid.UUID]ticketAt{tc.row.ID: {project: tc.project.Key, number: tc.row.Number}})
	return all[tc.row.ID], err
}

// GetTicketBody answers the ticket's body as written and rendered: HTML
// sanitised on the server (docs/adr/0011 D6), on a route of its own so that the
// lists render nothing.
func (s *Server) GetTicketBody(ctx context.Context, req apigen.GetTicketBodyRequestObject) (apigen.GetTicketBodyResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, read); perr != nil {
		return nil, perr
	}
	var (
		row    store.TicketRow
		images richtext.Images
	)
	err := s.db.InTenant(ctx, t.ID, func(r *store.Reader) error {
		tc, err := visibleTicket(ctx, r, t, req.Project, req.Number)
		if err != nil {
			return err
		}
		row = tc.row
		images, err = ticketImages(ctx, r, t, tc)
		return err
	})
	if err != nil {
		return nil, err
	}
	return apigen.GetTicketBody200JSONResponse{
		Body:    apigen.TicketBody{Body: row.Body, BodyHtml: richtext.HTML(row.Body, images), Version: int(row.Version)},
		Headers: apigen.GetTicketBody200ResponseHeaders{ETag: etag(row.Version)},
	}, nil
}

// renderedComment is a comment as the API answers it: with its body rendered
// beside the Markdown, none once it is withdrawn.
func renderedComment(c comment, images richtext.Images) apigen.Comment {
	v := commentView(c)
	if !v.Withdrawn {
		html := richtext.HTML(c.Body, images)
		v.BodyHtml = nullableString(&html)
	}
	return v
}
