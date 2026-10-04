package tools

import (
	"context"
	"net/http"
	"time"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
)

// ticketQuery is a filtered list of a project's tickets (docs/adr/0049).
type ticketQuery struct {
	states, types []string
	assignee      string
	assignees     []string
	blocked       *bool
	query         string
	terminal      bool
	limit         int
}

// listTickets reads a project's tickets in the order of its rank.
func listTickets(ctx context.Context, s *Session, tenant, project string, q ticketQuery) ([]apigen.Ticket, error) {
	params := &apigen.ListProjectTicketsParams{Blocked: q.blocked}
	if len(q.states) > 0 {
		params.State = &q.states
	}
	if len(q.types) > 0 {
		params.Type = &q.types
	}
	assignees := q.assignees
	if q.assignee != "" {
		assignees = append(assignees, q.assignee)
	}
	if len(assignees) > 0 {
		params.Assignee = &assignees
	}
	if q.query != "" {
		params.Q = &q.query
	}
	if q.terminal {
		params.IncludeTerminal = &q.terminal
	}
	if q.limit > 0 {
		params.Limit = &q.limit
	}
	res, err := s.API.ListProjectTicketsWithResponse(ctx, tenant, project, params)
	if err := check(res, err, http.StatusOK); err != nil {
		return nil, err
	}
	return res.JSON200.Items, nil
}

// searchTenant reads a tenant's tickets, newest first, by full text and the
// filters of a search.
func searchTenant(ctx context.Context, s *Session, tenant string, q ticketQuery) ([]apigen.Ticket, error) {
	params := &apigen.ListTenantTicketsParams{}
	if len(q.states) > 0 {
		params.State = &q.states
	}
	if len(q.types) > 0 {
		params.Type = &q.types
	}
	if q.assignee != "" {
		params.Assignee = &[]string{q.assignee}
	}
	if q.query != "" {
		params.Q = &q.query
	}
	if q.terminal {
		params.IncludeTerminal = &q.terminal
	}
	if q.limit > 0 {
		params.Limit = &q.limit
	}
	res, err := s.API.ListTenantTicketsWithResponse(ctx, tenant, params)
	if err := check(res, err, http.StatusOK); err != nil {
		return nil, err
	}
	return res.JSON200.Items, nil
}

// listTenantTickets reads a project's tickets changed after a time, newest
// first, done and dropped ones included.
func listTenantTickets(ctx context.Context, s *Session, tenant, project string, after time.Time, limit int) ([]apigen.Ticket, error) {
	terminal := true
	params := &apigen.ListTenantTicketsParams{Project: &[]string{project}, UpdatedAfter: &after,
		IncludeTerminal: &terminal, Limit: &limit}
	res, err := s.API.ListTenantTicketsWithResponse(ctx, tenant, params)
	if err := check(res, err, http.StatusOK); err != nil {
		return nil, err
	}
	return res.JSON200.Items, nil
}

// getTicket reads a ticket and the ETag of its version.
func getTicket(ctx context.Context, s *Session, ref ticketRef) (apigen.Ticket, string, error) {
	res, err := s.API.GetTicketWithResponse(ctx, ref.Tenant, ref.Project, int(ref.Number))
	if err := check(res, err, http.StatusOK); err != nil {
		return apigen.Ticket{}, "", err
	}
	return *res.JSON200, res.HTTPResponse.Header.Get("ETag"), nil
}

// contextDocument reads a ticket's context (docs/adr/0044 D2).
func contextDocument(ctx context.Context, s *Session, ref ticketRef, comments, activity int) (string, error) {
	res, err := s.API.ExportTicketContextWithResponse(ctx, ref.Tenant, ref.Project, int(ref.Number),
		&apigen.ExportTicketContextParams{Comments: &comments, Activity: &activity})
	if err := check(res, err, http.StatusOK); err != nil {
		return "", err
	}
	return string(res.Body), nil
}
