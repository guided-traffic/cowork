package api

import (
	"bytes"
	"cmp"
	"context"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/problem"
	"github.com/guided-traffic/cowork/backend/internal/richtext"
	"github.com/guided-traffic/cowork/backend/internal/store"
	"github.com/guided-traffic/cowork/backend/internal/store/readq"
)

// The person-level lists of tickets — "next for me", "assigned to me" and the
// open decisions (docs/adr/0018 D3) — are ordered by the score
// (docs/adr/0014 D5): each tenant's part is read in that tenant in the score's
// order after the cursor's position (docs/adr/0021 D5), and the parts are
// merged. The position is the score's key and the ticket's id, which is
// unique across tenants, so every tenant's part resumes at the same place of
// one order; the key's order is the score's at every moment, so a cursor never
// moves by itself. A score is shown on the ticket, so the position is not
// sealed.

// myTicket is a ticket of a person-level list with its tenant and its place in
// its project's rank, the secondary indicator.
type myTicket struct {
	tenant personTenant
	row    store.TicketRow
	place  int32
}

// mine is what a person-level list of tickets reads: its operation, the
// tenant and the project it is narrowed to, and which of the person's tickets
// it holds.
type mine struct {
	op              string
	tenant, project *string
	cursor          *string
	limit           *int
	filter          store.TicketFilter
}

// ListMyNext answers what the person could take up next across their tenants
// (docs/adr/0018 D3 as amended 2026-10-05): the open tickets assigned to them
// or to nobody, in the projects they see; another person's ticket is not
// "for me". A weak ETag answers an unchanged page with 304 (docs/adr/0054 D7).
func (s *Server) ListMyNext(ctx context.Context, req apigen.ListMyNextRequestObject) (apigen.ListMyNextResponseObject, error) {
	p := principal(ctx)
	out, err := s.listMine(ctx, mine{op: "listMyNext", tenant: req.Params.Tenant, project: req.Params.Project,
		cursor: req.Params.Cursor, limit: req.Params.Limit,
		filter: store.TicketFilter{Assignees: store.PersonSet{In: []uuid.UUID{p.PersonID}, None: true}}})
	if err != nil {
		return nil, err
	}
	tag, unchanged := listTag(req.Params.IfNoneMatch, out)
	if unchanged {
		return apigen.ListMyNext304Response{Headers: apigen.NotModifiedResponseHeaders{ETag: &tag}}, nil
	}
	return apigen.ListMyNext200JSONResponse{Body: out, Headers: apigen.ListMyNext200ResponseHeaders{ETag: &tag}}, nil
}

// ListMyAssigned answers the open tickets assigned to the person across their
// tenants (docs/adr/0018 D3), with a weak ETag and 304 as ListMyNext.
func (s *Server) ListMyAssigned(ctx context.Context, req apigen.ListMyAssignedRequestObject) (apigen.ListMyAssignedResponseObject, error) {
	p := principal(ctx)
	out, err := s.listMine(ctx, mine{op: "listMyAssigned", tenant: req.Params.Tenant, cursor: req.Params.Cursor,
		limit: req.Params.Limit, filter: store.TicketFilter{Assignees: store.PersonSet{In: []uuid.UUID{p.PersonID}}}})
	if err != nil {
		return nil, err
	}
	tag, unchanged := listTag(req.Params.IfNoneMatch, out)
	if unchanged {
		return apigen.ListMyAssigned304Response{Headers: apigen.NotModifiedResponseHeaders{ETag: &tag}}, nil
	}
	return apigen.ListMyAssigned200JSONResponse{Body: out, Headers: apigen.ListMyAssigned200ResponseHeaders{ETag: &tag}}, nil
}

// listMine reads a person-level list of tickets: each tenant's part in the
// score's order after the cursor, a page of it at most, with each ticket's
// place; the parts merged in the same order and cut to the page.
func (s *Server) listMine(ctx context.Context, l mine) (apigen.MyTicketList, error) {
	if l.project != nil && l.tenant == nil {
		return apigen.MyTicketList{}, problem.Field("query:project", "a project is named within a tenant: name the tenant as well")
	}
	scope := principal(ctx).PersonID.String() + "/" + deref(l.tenant) + "/" + deref(l.project)
	after := ""
	if l.cursor != nil {
		raw, perr := s.cursors.decode(l.op, scope, *l.cursor)
		if perr != nil {
			return apigen.MyTicketList{}, perr
		}
		if _, _, err := store.ParseScorePosition(raw); err != nil {
			return apigen.MyTicketList{}, invalidCursor()
		}
		after = raw
	}
	tenants, err := s.h.personTenants(ctx, l.tenant)
	if err != nil {
		return apigen.MyTicketList{}, err
	}
	if l.project != nil {
		l.filter.Projects = store.ValueSet{In: []string{*l.project}}
	}
	size := s.h.pageSize(l.limit)
	var rows []myTicket
	for _, t := range tenants {
		part, err := s.mineIn(ctx, t, l.filter, store.TicketPage{Order: store.ByScore, After: after, Limit: size + 1})
		if err != nil {
			return apigen.MyTicketList{}, err
		}
		rows = append(rows, part...)
	}
	slices.SortFunc(rows, func(a, b myTicket) int { return byScore(a.row.ScoreKey, b.row.ScoreKey, a.row.ID, b.row.ID) })
	rows, next := page(s.h, rows, size, l.op, scope, func(m myTicket) string { return store.ScorePosition(m.row.ScoreKey, m.row.ID) })
	now := s.h.opts.Now()
	out := apigen.MyTicketList{Items: make([]apigen.MyTicket, 0, len(rows)), NextCursor: nullableString(next)}
	for _, m := range rows {
		out.Items = append(out.Items, apigen.MyTicket{Tenant: m.tenant.ref(), Ticket: ticketView(m.tenant.scope(), m.row, now),
			Place: int(m.place)})
	}
	return out, nil
}

// mineIn reads one tenant's part of a person-level list of tickets and the
// place of each in its project's rank, in one transaction of that tenant.
func (s *Server) mineIn(ctx context.Context, t personTenant, filter store.TicketFilter, at store.TicketPage) ([]myTicket, error) {
	var out []myTicket
	err := s.db.InTenant(ctx, t.id, func(r *store.Reader) error {
		list, err := r.ListTickets(ctx, filter, at)
		if err != nil || len(list.Rows) == 0 {
			return err
		}
		ids := make([]uuid.UUID, 0, len(list.Rows))
		for _, row := range list.Rows {
			ids = append(ids, row.ID)
		}
		places, err := r.ListRankPlaces(ctx, readq.ListRankPlacesParams{TenantID: t.id, Ids: ids})
		if err != nil {
			return err
		}
		place := make(map[uuid.UUID]int32, len(places))
		for _, p := range places {
			place[p.ID] = p.Place
		}
		for _, row := range list.Rows {
			out = append(out, myTicket{tenant: t, row: row, place: place[row.ID]})
		}
		return nil
	})
	return out, err
}

// byScore is the order of the person-level lists: the score's key, highest
// first, then the ticket's id.
func byScore(a, b float64, idA, idB uuid.UUID) int {
	if c := cmp.Compare(b, a); c != 0 {
		return c
	}
	return bytes.Compare(idA[:], idB[:])
}

// decision is an open question of a person-level list with its tenant and
// the images its ticket's texts may show.
type decision struct {
	tenant personTenant
	row    readq.ListOpenDecisionsRow
	images richtext.Images
}

// decisionPosition is where the open decisions resume: the score's key of the
// question's ticket, the ticket's id and the question's number.
func decisionPosition(d decision) string {
	return store.ScorePosition(d.row.TicketScoreKey, d.row.TicketID) + "/" + strconv.Itoa(int(d.row.Number))
}

// decisionAfter reads a decisionPosition into the query's parameters; false
// for anything decisionPosition does not write.
func decisionAfter(raw string, params *readq.ListOpenDecisionsParams) bool {
	parts := strings.Split(raw, "/")
	if len(parts) != 3 {
		return false
	}
	key, id, err := store.ParseScorePosition(parts[0] + "/" + parts[1])
	n, errNumber := strconv.ParseInt(parts[2], 10, 32)
	if err != nil || errNumber != nil {
		return false
	}
	params.HasAfter, params.AfterKey, params.AfterTicket, params.AfterQuestion = true, key, id, int32(n)
	return true
}

// ListMyDecisions answers the open decisions of the person across their
// tenants (docs/adr/0018 D3): the open questions asked of them and those open
// in the tenant, each tenant's part read in that tenant (docs/adr/0021 D5),
// ordered by the score of their ticket, then the ticket and the question.
func (s *Server) ListMyDecisions(ctx context.Context, req apigen.ListMyDecisionsRequestObject) (apigen.ListMyDecisionsResponseObject, error) {
	p := principal(ctx)
	const op = "listMyDecisions"
	scope := p.PersonID.String() + "/" + deref(req.Params.Tenant)
	var after readq.ListOpenDecisionsParams
	if req.Params.Cursor != nil {
		raw, perr := s.cursors.decode(op, scope, *req.Params.Cursor)
		if perr != nil {
			return nil, perr
		}
		if !decisionAfter(raw, &after) {
			return nil, invalidCursor()
		}
	}
	tenants, err := s.h.personTenants(ctx, req.Params.Tenant)
	if err != nil {
		return nil, err
	}
	size := s.h.pageSize(req.Params.Limit)
	var rows []decision
	for _, t := range tenants {
		params := after
		params.TenantID, params.UserID, params.PageSize = t.id, p.PersonID, limitArg(size)
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
	slices.SortFunc(rows, func(a, b decision) int {
		if c := byScore(a.row.TicketScoreKey, b.row.TicketScoreKey, a.row.TicketID, b.row.TicketID); c != 0 {
			return c
		}
		return cmp.Compare(a.row.Number, b.row.Number)
	})
	rows, next := page(s.h, rows, size, op, scope, decisionPosition)
	out := apigen.DecisionList{Items: make([]apigen.Decision, 0, len(rows)), NextCursor: nullableString(next)}
	for _, d := range rows {
		out.Items = append(out.Items, decisionView(d))
	}
	tag, unchanged := listTag(req.Params.IfNoneMatch, out)
	if unchanged {
		return apigen.ListMyDecisions304Response{Headers: apigen.NotModifiedResponseHeaders{ETag: &tag}}, nil
	}
	return apigen.ListMyDecisions200JSONResponse{Body: out, Headers: apigen.ListMyDecisions200ResponseHeaders{ETag: &tag}}, nil
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
