package api

import (
	"context"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/richtext"
	"github.com/guided-traffic/cowork/backend/internal/store"
	"github.com/guided-traffic/cowork/backend/internal/store/readq"
)

// listPosition is where a person-level list resumes. The lists are ordered by
// the tenant's slug, the project's key and the project's rank until the score
// exists (docs/adr/0014 D5): the position is the tenant's slug, the project's
// key and the place in the rank (store.RankPosition) — sealed in the cursor,
// because a rank key is computed over tickets the caller may not see (D2) —
// and for the decisions the question's number.
type listPosition struct {
	slug, project, rank string
	question            int32
}

// encode writes the cursor value of a position.
func (s *Server) encodePosition(p listPosition, withQuestion bool) string {
	v := p.slug + "/" + p.project + "/" + s.cursors.sealPosition(p.rank)
	if withQuestion {
		v += "/" + strconv.Itoa(int(p.question))
	}
	return v
}

// decodePosition reads a person-level list's cursor; nil without one.
func (s *Server) decodePosition(op, scope string, cursor *string, withQuestion bool) (*listPosition, error) {
	if cursor == nil {
		return nil, nil
	}
	raw, perr := s.cursors.decode(op, scope, *cursor)
	if perr != nil {
		return nil, perr
	}
	parts := strings.Split(raw, "/")
	want := 3
	if withQuestion {
		want = 4
	}
	if len(parts) != want {
		return nil, invalidCursor()
	}
	rank, ok := s.cursors.openPosition(parts[2])
	if !ok {
		return nil, invalidCursor()
	}
	pos := &listPosition{slug: parts[0], project: parts[1], rank: rank}
	if withQuestion {
		n, err := strconv.ParseInt(parts[3], 10, 32)
		if err != nil {
			return nil, invalidCursor()
		}
		pos.question = int32(n)
	}
	return pos, nil
}

// resumes says whether a tenant's part of a person-level list is read, and
// from where: a tenant before the cursor's has been read, the cursor's
// resumes after its position, and one after it starts at its top.
func (p *listPosition) resumes(t personTenant) (read bool, here bool) {
	switch {
	case p == nil:
		return true, false
	case t.slug < p.slug:
		return false, false
	case t.slug == p.slug:
		return true, true
	}
	return true, false
}

// myTicket is a ticket of a person-level list with its tenant.
type myTicket struct {
	tenant personTenant
	row    store.TicketRow
}

// ListMyAssigned answers the open tickets assigned to the person across their
// tenants (docs/adr/0018 D3): each tenant's part read in that tenant under the
// tenant's predicates (docs/adr/0021 D5), in the order of listPosition.
func (s *Server) ListMyAssigned(ctx context.Context, req apigen.ListMyAssignedRequestObject) (apigen.ListMyAssignedResponseObject, error) {
	p := principal(ctx)
	const op = "listMyAssigned"
	scope := p.PersonID.String() + "/" + deref(req.Params.Tenant)
	after, err := s.decodePosition(op, scope, req.Params.Cursor, false)
	if err != nil {
		return nil, err
	}
	tenants, err := s.h.personTenants(ctx, req.Params.Tenant)
	if err != nil {
		return nil, err
	}
	size := s.h.pageSize(req.Params.Limit)
	filter := store.TicketFilter{Assignees: store.PersonSet{In: []uuid.UUID{p.PersonID}}}
	var rows []myTicket
	for _, t := range tenants {
		read, here := after.resumes(t)
		if !read || len(rows) > size {
			continue
		}
		at := store.TicketPage{Order: store.ByProjectRank, Limit: size + 1 - len(rows)}
		if here {
			at.After = after.project + "/" + after.rank
		}
		err := s.db.InTenant(ctx, t.id, func(r *store.Reader) error {
			list, err := r.ListTickets(ctx, filter, at)
			for _, row := range list.Rows {
				rows = append(rows, myTicket{tenant: t, row: row})
			}
			return err
		})
		if err != nil {
			return nil, err
		}
	}
	rows, next := page(s.h, rows, size, op, scope, func(m myTicket) string {
		return s.encodePosition(listPosition{slug: m.tenant.slug, project: m.row.ProjectKey,
			rank: store.RankPosition(m.row.Rank, m.row.State, m.row.Number)}, false)
	})
	out := apigen.ListMyAssigned200JSONResponse{Items: make([]apigen.MyTicket, 0, len(rows)), NextCursor: nullableString(next)}
	for _, m := range rows {
		out.Items = append(out.Items, apigen.MyTicket{Tenant: m.tenant.ref(), Ticket: ticketView(m.tenant.scope(), m.row)})
	}
	return out, nil
}

// decision is an open question of a person-level list with its tenant and
// the images its ticket's texts may show.
type decision struct {
	tenant personTenant
	row    readq.ListOpenDecisionsRow
	images richtext.Images
}

// ListMyDecisions answers the open decisions of the person across their
// tenants (docs/adr/0018 D3): the open questions asked of them and those open
// in the tenant, each tenant's part read in that tenant (docs/adr/0021 D5), in
// the order of listPosition.
func (s *Server) ListMyDecisions(ctx context.Context, req apigen.ListMyDecisionsRequestObject) (apigen.ListMyDecisionsResponseObject, error) {
	p := principal(ctx)
	const op = "listMyDecisions"
	scope := p.PersonID.String() + "/" + deref(req.Params.Tenant)
	after, err := s.decodePosition(op, scope, req.Params.Cursor, true)
	if err != nil {
		return nil, err
	}
	tenants, err := s.h.personTenants(ctx, req.Params.Tenant)
	if err != nil {
		return nil, err
	}
	size := s.h.pageSize(req.Params.Limit)
	var rows []decision
	for _, t := range tenants {
		read, here := after.resumes(t)
		if !read || len(rows) > size {
			continue
		}
		params := readq.ListOpenDecisionsParams{TenantID: t.id, UserID: p.PersonID, PageSize: int32(size + 1 - len(rows))} // #nosec G115 -- size is clamped to the page size, far below int32's range
		if here {
			rank, number, _ := strings.Cut(after.rank, ".")
			n, err := strconv.ParseInt(number, 10, 32)
			if err != nil {
				return nil, invalidCursor()
			}
			params.HasAfter, params.AfterProject, params.AfterNumber, params.AfterQuestion = true, after.project, int32(n), after.question
			if rank != "" {
				params.AfterRank = &rank
			}
		}
		err := s.db.InTenant(ctx, t.id, func(r *store.Reader) error {
			list, err := r.ListOpenDecisions(ctx, params)
			if err != nil {
				return err
			}
			tickets := map[uuid.UUID]ticketAt{}
			for _, row := range list {
				tickets[row.TicketID] = ticketAt{project: row.ProjectKey, number: row.TicketNumber}
			}
			images, err := imagesOf(ctx, r, t.scope(), tickets)
			for _, row := range list {
				rows = append(rows, decision{tenant: t, row: row, images: images[row.TicketID]})
			}
			return err
		})
		if err != nil {
			return nil, err
		}
	}
	rows, next := page(s.h, rows, size, op, scope, func(d decision) string {
		return s.encodePosition(listPosition{slug: d.tenant.slug, project: d.row.ProjectKey,
			rank: store.RankPosition(d.row.TicketRank, d.row.TicketState, d.row.TicketNumber), question: d.row.Number}, true)
	})
	out := apigen.ListMyDecisions200JSONResponse{Items: make([]apigen.Decision, 0, len(rows)), NextCursor: nullableString(next)}
	for _, d := range rows {
		out.Items = append(out.Items, decisionView(d))
	}
	return out, nil
}

func decisionView(d decision) apigen.Decision {
	q := d.row
	return apigen.Decision{
		Tenant: d.tenant.ref(),
		Ticket: apigen.TicketRef{Key: domain.FullKey(d.tenant.slug, q.ProjectKey, q.TicketNumber), Title: q.TicketTitle,
			State: apigen.TicketState(q.TicketState)},
		Question: questionView(question{
			ID: q.ID, Number: q.Number, Question: q.Question, Options: q.Options, Recommendation: q.Recommendation,
			Answer: q.Answer, Status: q.Status, AskedBy: q.AskedBy, AskedByUsername: q.AskedByUsername,
			AskedByName: q.AskedByName, AskedByAgent: q.AskedByAgent, AskedByTokenID: q.AskedByTokenID,
			AskedByTokenName: q.AskedByTokenName, AskedOf: q.AskedOf, AskedOfUsername: q.AskedOfUsername,
			AskedOfName: q.AskedOfName, AnsweredBy: q.AnsweredBy, AnsweredByUsername: q.AnsweredByUsername,
			AnsweredByName: q.AnsweredByName, AnsweredAt: q.AnsweredAt, RecordedByAgent: q.RecordedByAgent,
			AnsweredByTokenID: q.AnsweredByTokenID, AnsweredByTokenName: q.AnsweredByTokenName,
			WithdrawnAt: q.WithdrawnAt, Version: q.Version, CreatedAt: q.CreatedAt, UpdatedAt: q.UpdatedAt,
		}, d.images),
	}
}
