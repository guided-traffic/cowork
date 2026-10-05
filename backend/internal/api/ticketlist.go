package api

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/problem"
	"github.com/guided-traffic/cowork/backend/internal/store"
	"github.com/guided-traffic/cowork/backend/internal/store/readq"
)

// maxPageDepth caps a numbered page: page × per_page above it is
// page_too_deep (docs/adr/0048 D2).
const maxPageDepth = 10000

// The filter values that are not a vocabulary value (docs/adr/0049 D1, D5).
const (
	filterMe   = "me"
	filterNone = "none"
)

// ticketQuery is what both ticket lists take: the filters of docs/adr/0049
// and the paging of docs/adr/0048.
type ticketQuery struct {
	project, state, typ, severity, security, urgency, effort *[]string
	assignee, reporter, parent, interest                     *[]string
	progressMin, progressMax                                 *int
	openedAfter, openedBefore, updatedAfter, updatedBefore   *time.Time
	doneAfter                                                *time.Time
	q                                                        *string
	includeTerminal, blocked, hasOpenQuestions               *bool
	cursor                                                   *string
	limit, page, perPage                                     *int
}

// ticketListing is a parsed ticket list request.
type ticketListing struct {
	filter  store.TicketFilter
	page    store.TicketPage
	size    int
	parents []parentRef
}

// parentRef is a parent key of the parent filter, resolved inside the
// transaction.
type parentRef struct {
	key     domain.TicketKey
	negated bool
}

// ListProjectTickets lists a project's tickets in its rank (docs/adr/0014 D1).
func (s *Server) ListProjectTickets(ctx context.Context, req apigen.ListProjectTicketsRequestObject) (apigen.ListProjectTicketsResponseObject, error) {
	p := req.Params
	q := ticketQuery{
		state: p.State, typ: p.Type, severity: p.Severity, security: p.Security, urgency: p.Urgency, effort: p.Effort,
		assignee: p.Assignee, reporter: p.Reporter, parent: p.Parent, interest: p.Interest, progressMin: p.ProgressMin,
		progressMax: p.ProgressMax,
		openedAfter: p.OpenedAfter, openedBefore: p.OpenedBefore, updatedAfter: p.UpdatedAfter, updatedBefore: p.UpdatedBefore,
		doneAfter: p.DoneAfter, q: p.Q, includeTerminal: p.IncludeTerminal, blocked: p.Blocked, hasOpenQuestions: p.HasOpenQuestions,
		cursor: p.Cursor, limit: p.Limit, page: p.Page, perPage: (*int)(p.PerPage),
	}
	list, tag, err := s.listTickets(ctx, "listProjectTickets", req.Project, q, store.ByRank)
	if err != nil {
		return nil, err
	}
	if notModified(p.IfNoneMatch, tag) {
		return apigen.ListProjectTickets304Response{Headers: apigen.ListProjectTickets304ResponseHeaders{ETag: &tag}}, nil
	}
	return apigen.ListProjectTickets200JSONResponse{Body: list, Headers: apigen.ListProjectTickets200ResponseHeaders{ETag: &tag}}, nil
}

// ListTenantTickets lists the tenant's tickets across the projects the caller
// can see, newest first.
func (s *Server) ListTenantTickets(ctx context.Context, req apigen.ListTenantTicketsRequestObject) (apigen.ListTenantTicketsResponseObject, error) {
	p := req.Params
	q := ticketQuery{
		project: p.Project, state: p.State, typ: p.Type, severity: p.Severity, security: p.Security, urgency: p.Urgency,
		effort: p.Effort, assignee: p.Assignee, reporter: p.Reporter, parent: p.Parent, interest: p.Interest, progressMin: p.ProgressMin,
		progressMax: p.ProgressMax, openedAfter: p.OpenedAfter, openedBefore: p.OpenedBefore, updatedAfter: p.UpdatedAfter,
		updatedBefore: p.UpdatedBefore, doneAfter: p.DoneAfter, q: p.Q, includeTerminal: p.IncludeTerminal, blocked: p.Blocked,
		hasOpenQuestions: p.HasOpenQuestions, cursor: p.Cursor,
		limit: p.Limit, page: p.Page, perPage: (*int)(p.PerPage),
	}
	list, tag, err := s.listTickets(ctx, "listTenantTickets", "", q, store.NewestFirst)
	if err != nil {
		return nil, err
	}
	if notModified(p.IfNoneMatch, tag) {
		return apigen.ListTenantTickets304Response{Headers: apigen.ListTenantTickets304ResponseHeaders{ETag: &tag}}, nil
	}
	return apigen.ListTenantTickets200JSONResponse{Body: list, Headers: apigen.ListTenantTickets200ResponseHeaders{ETag: &tag}}, nil
}

// listTickets answers a page of tickets and its weak ETag. projectKey is
// empty for the tenant-wide list.
func (s *Server) listTickets(ctx context.Context, op, projectKey string, q ticketQuery, order store.TicketOrder) (apigen.TicketList, string, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, read); perr != nil {
		return apigen.TicketList{}, "", perr
	}
	scope := ticketListScope(t, projectKey, order)
	l, perr := s.parseTicketQuery(ctx, q, op, scope, order)
	if perr != nil {
		return apigen.TicketList{}, "", perr
	}
	var list store.TicketList
	err := s.db.InTenant(ctx, t.ID, func(r *store.Reader) error {
		if projectKey != "" {
			p, err := visibleProject(ctx, r, t, projectKey)
			if err != nil {
				return err
			}
			l.filter.ProjectID = p.ID
		}
		if err := resolveParents(ctx, r, t, &l); err != nil {
			return err
		}
		var err error
		list, err = r.ListTickets(ctx, l.filter, l.page)
		return err
	})
	if err != nil {
		return apigen.TicketList{}, "", err
	}
	out := apigen.TicketList{Items: []apigen.Ticket{}}
	rows := list.Rows
	if l.page.Page > 0 {
		total := int(list.Total)
		out.Page, out.PerPage, out.Total = &l.page.Page, &l.page.PerPage, &total
		out.NextCursor = nullableString(nil)
	} else {
		var next *string
		rows, next = page(s.h, rows, l.size, op, scope, s.position(order))
		out.NextCursor = nullableString(next)
	}
	for _, r := range rows {
		out.Items = append(out.Items, ticketView(t, r))
	}
	return out, weakETag(out), nil
}

// position writes a row's cursor position. The rank order's is sealed: a key
// is computed over tickets the caller may not see (docs/adr/0014 D2,
// docs/adr/0048 D1), and a readable cursor would show it.
func (s *Server) position(order store.TicketOrder) func(store.TicketRow) string {
	if order != store.ByRank {
		return order.Position
	}
	return func(r store.TicketRow) string { return s.cursors.sealPosition(order.Position(r)) }
}

// ticketListScope is what a ticket list's cursor is bound to besides its
// operation: the tenant, the project, and the rank order of a project's list,
// so a cursor of the number order it had before the rank is invalid_cursor
// rather than a position read in another order (docs/adr/0048 D5).
func ticketListScope(t tenantScope, projectKey string, order store.TicketOrder) string {
	scope := t.ID.String() + "/" + projectKey
	if order == store.ByRank {
		scope += "/rank"
	}
	return scope
}

// parseTicketQuery checks the filters and the paging; every refused
// parameter is named (docs/adr/0049 D4).
func (s *Server) parseTicketQuery(ctx context.Context, q ticketQuery, op, scope string, order store.TicketOrder) (ticketListing, *problem.Error) {
	l := ticketListing{page: store.TicketPage{Order: order}}
	errs := s.parseFilters(principal(ctx).PersonID, q, &l)
	errs = append(errs, pagingConflicts(q)...)
	if len(errs) > 0 {
		return l, &problem.Error{Code: problem.ValidationFailed, Detail: "the list request has values this route does not take", Errors: errs}
	}
	return l, s.paging(q, op, scope, &l)
}

// parseFilters checks the filters of docs/adr/0049 D1, D2 into l, me standing
// for the person of the caller (D5), and returns every refused value, its
// pointer query:<name> — the lists and a saved filter alike (D6, D7).
func (s *Server) parseFilters(me uuid.UUID, q ticketQuery, l *ticketListing) []problem.FieldError {
	var errs []problem.FieldError
	vocab := func(name string, values *[]string, valid func(string) bool, set *store.ValueSet) {
		for _, v := range deref(values) {
			plain, negated := strings.CutPrefix(v, "!")
			if !valid(plain) {
				errs = append(errs, problem.FieldError{Pointer: "query:" + name, Message: "not a value of " + name + ": " + v})
				continue
			}
			if negated {
				set.NotIn = append(set.NotIn, plain)
			} else {
				set.In = append(set.In, plain)
			}
		}
	}
	f := &l.filter
	vocab("project", q.project, domain.ValidProjectKey, &f.Projects)
	vocab("state", q.state, func(v string) bool { return apigen.TicketState(v).Valid() }, &f.States)
	vocab("type", q.typ, func(v string) bool { return apigen.TicketType(v).Valid() }, &f.Types)
	vocab("severity", q.severity, func(v string) bool { return apigen.Severity(v).Valid() }, &f.Severities)
	vocab("security", q.security, func(v string) bool { return apigen.SecurityClass(v).Valid() }, &f.Securities)
	vocab("urgency", q.urgency, func(v string) bool { return apigen.Urgency(v).Valid() }, &f.Urgencies)
	vocab("effort", q.effort, func(v string) bool { return apigen.Effort(v).Valid() }, &f.Efforts)
	errs = append(errs, persons(fieldAssignee, q.assignee, me, true, &f.Assignees)...)
	errs = append(errs, persons("reporter", q.reporter, me, false, &f.Reporters)...)
	errs = append(errs, l.parentRefs(q.parent)...)
	errs = append(errs, interestFilter(q.interest, me, &f.Interest)...)
	f.ProgressMin, f.ProgressMax = q.progressMin, q.progressMax
	f.OpenedAfter, f.OpenedBefore, f.UpdatedAfter, f.UpdatedBefore = q.openedAfter, q.openedBefore, q.updatedAfter, q.updatedBefore
	f.DoneAfter = q.doneAfter
	f.IncludeTerminal = q.includeTerminal != nil && *q.includeTerminal
	f.Blocked, f.HasOpenQuestions = q.blocked, q.hasOpenQuestions
	if q.q != nil {
		if s.h.opts.MaxQueryLength > 0 && utf8.RuneCountInString(*q.q) > s.h.opts.MaxQueryLength {
			errs = append(errs, problem.FieldError{Pointer: fieldQuery,
				Message: "longer than " + strconv.Itoa(s.h.opts.MaxQueryLength) + " characters"})
		}
		f.Query = *q.q
	}
	return errs
}

// persons parses a person filter: an id, me, and — where the column may be
// empty — none, each negatable.
func persons(name string, values *[]string, me uuid.UUID, noneAllowed bool, set *store.PersonSet) []problem.FieldError {
	var errs []problem.FieldError
	for _, v := range deref(values) {
		plain, negated := strings.CutPrefix(v, "!")
		switch {
		case plain == filterNone && noneAllowed:
			if negated {
				set.NotNone = true
			} else {
				set.None = true
			}
			continue
		case plain == filterMe:
			plain = me.String()
		}
		id, err := uuid.Parse(plain)
		if err != nil {
			errs = append(errs, problem.FieldError{Pointer: "query:" + name, Message: "not a person id, me or none: " + v})
			continue
		}
		if negated {
			set.NotIn = append(set.NotIn, id)
		} else {
			set.In = append(set.In, id)
		}
	}
	return errs
}

// interestFilter parses the interest filter: me or any, each negatable.
func interestFilter(values *[]string, me uuid.UUID, f *store.InterestFilter) []problem.FieldError {
	var errs []problem.FieldError
	f.Person = me
	for _, v := range deref(values) {
		plain, negated := strings.CutPrefix(v, "!")
		set := !negated
		switch plain {
		case filterMe:
			f.Me = &set
		case "any":
			f.Any = &set
		default:
			errs = append(errs, problem.FieldError{Pointer: "query:interest", Message: "not me or any: " + v})
		}
	}
	return errs
}

// parentRefs parses the parent filter: a ticket key, or none for roots, each
// negatable.
func (l *ticketListing) parentRefs(values *[]string) []problem.FieldError {
	var errs []problem.FieldError
	for _, v := range deref(values) {
		plain, negated := strings.CutPrefix(v, "!")
		if plain == filterNone {
			if negated {
				l.filter.Parents.NotNone = true
			} else {
				l.filter.Parents.None = true
			}
			continue
		}
		key, err := domain.ParseTicketKey(plain)
		if err != nil {
			errs = append(errs, problem.FieldError{Pointer: "query:parent", Message: "not a ticket key or none: " + v})
			continue
		}
		l.parents = append(l.parents, parentRef{key: key, negated: negated})
	}
	return errs
}

// resolveParents turns the parent keys into ids under the visibility
// predicate. A key that names no ticket the caller can see matches nothing,
// the same for one that does not exist (docs/adr/0065 D5).
func resolveParents(ctx context.Context, r *store.Reader, t tenantScope, l *ticketListing) error {
	for _, ref := range l.parents {
		id := uuid.Nil
		if k, err := ref.key.InTenant(t.Slug); err == nil {
			found, err := ticketIDByKey(ctx, r, t, k)
			if err != nil {
				return err
			}
			id = found
		}
		if ref.negated {
			l.filter.Parents.NotIn = append(l.filter.Parents.NotIn, id)
		} else {
			l.filter.Parents.In = append(l.filter.Parents.In, id)
		}
	}
	return nil
}

// ticketIDByKey finds a visible ticket by its key inside the tenant; uuid.Nil
// when there is none.
func ticketIDByKey(ctx context.Context, r *store.Reader, t tenantScope, k domain.TicketKey) (uuid.UUID, error) {
	p, err := visibleProject(ctx, r, t, k.Project)
	if err != nil {
		var perr *problem.Error
		if errors.As(err, &perr) {
			return uuid.Nil, nil
		}
		return uuid.Nil, err
	}
	row, err := r.GetTicketByNumber(ctx, readq.GetTicketByNumberParams{TenantID: t.ID, ProjectID: p.ID, Number: k.Number})
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, nil
	}
	if err != nil {
		return uuid.Nil, err
	}
	return row.ID, nil
}

// pagingConflicts refuses parameters of the two paging modes mixed
// (docs/adr/0048 D1, D2).
func pagingConflicts(q ticketQuery) []problem.FieldError {
	var errs []problem.FieldError
	if q.page != nil && q.cursor != nil {
		errs = append(errs, problem.FieldError{Pointer: "query:cursor", Message: "a numbered page takes no cursor"})
	}
	if q.page != nil && q.limit != nil {
		errs = append(errs, problem.FieldError{Pointer: "query:limit", Message: "a numbered page takes per_page, not limit"})
	}
	if q.page == nil && q.perPage != nil {
		errs = append(errs, problem.FieldError{Pointer: "query:per_page", Message: "per_page goes with page"})
	}
	return errs
}

// paging sets the page: numbered with page, else after the cursor.
func (s *Server) paging(q ticketQuery, op, scope string, l *ticketListing) *problem.Error {
	if q.page != nil {
		perPage := defaultPageSize
		if q.perPage != nil {
			perPage = *q.perPage
		}
		perPage = s.h.pageSize(&perPage)
		if *q.page > maxPageDepth/perPage {
			return &problem.Error{Code: problem.PageTooDeep,
				Detail: "pages end at row " + strconv.Itoa(maxPageDepth) + "; narrow the list with a filter or follow the cursor",
				Errors: []problem.FieldError{{Pointer: "query:page", Message: "too deep"}}}
		}
		l.page.Page, l.page.PerPage = *q.page, perPage
		return nil
	}
	l.size = s.h.pageSize(q.limit)
	l.page.Limit = int(limitArg(l.size))
	if q.cursor != nil {
		after, perr := s.cursors.decode(op, scope, *q.cursor)
		if perr != nil {
			return perr
		}
		if l.page.Order == store.ByRank {
			var ok bool
			if after, ok = s.cursors.openPosition(after); !ok {
				return invalidCursor()
			}
		}
		l.page.After = after
	}
	return nil
}
