package api

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/problem"
	"github.com/guided-traffic/cowork/backend/internal/store"
	"github.com/guided-traffic/cowork/backend/internal/store/readq"
	"github.com/guided-traffic/cowork/backend/internal/store/writeq"
)

const (
	entityTimeEntry = "time_entry"
	dayLayout       = "2006-01-02"
	groupByTicket   = "ticket"
)

// booking is every time-entry write: a member's act with write scope, and
// never an agent's (docs/adr/0017 D6, docs/adr/0043 D3).
var booking = auth.Need{Role: domain.RoleMember, Scope: domain.ScopeWrite, HardOff: auth.HardOffBookingTime}

// timeEntry is the columns every time-entry query returns.
type timeEntry = readq.GetTimeEntryRow

func timeView(ticketKey string, e timeEntry) apigen.TimeEntry {
	return apigen.TimeEntry{
		Id: e.ID, Ticket: ticketKey, Person: personView(e.PersonID, e.PersonUsername, e.PersonName),
		Author: personView(e.AuthorID, e.AuthorUsername, e.AuthorName), Minutes: int(e.Minutes),
		Day: openapi_types.Date{Time: e.Day}, Note: e.Note, Voided: e.VoidedAt != nil, VoidedAt: nullableOf(e.VoidedAt),
		Edited: e.Edited, Version: int(e.Version), CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt,
	}
}

// visibleTimeEntry reads an entry through its ticket's predicate and the
// time visibility of docs/adr/0034 D5.
func visibleTimeEntry(ctx context.Context, r *store.Reader, t tenantScope, project string, number int, id uuid.UUID) (ticketCtx, timeEntry, error) {
	tc, err := visibleTicket(ctx, r, t, project, number)
	if err != nil {
		return tc, timeEntry{}, err
	}
	e, err := r.GetTimeEntry(ctx, readq.GetTimeEntryParams{TenantID: t.ID, TicketID: tc.row.ID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return tc, e, problem.New(problem.NotFound, "no such time entry")
	}
	return tc, e, err
}

// checkUnlocked refuses a write on a day on or before the closed period; the
// lock is read FOR SHARE (docs/adr/0017 D8).
func checkUnlocked(ctx context.Context, w *store.Writer, t tenantScope, days ...time.Time) error {
	locked, err := w.TimeLockedUntil(ctx, t.ID)
	if err != nil {
		return fmt.Errorf("read the time lock: %w", err)
	}
	if locked == nil {
		return nil
	}
	for _, d := range days {
		if !d.After(*locked) {
			return &problem.Error{Code: problem.PeriodLocked, Detail: "time is closed up to " + locked.Format(dayLayout),
				Errors: []problem.FieldError{{Pointer: "/day", Message: "on or before the closed period", Current: locked.Format(dayLayout)}}}
		}
	}
	return nil
}

// ListTicketTime lists a ticket's visible entries and their sum.
func (s *Server) ListTicketTime(ctx context.Context, req apigen.ListTicketTimeRequestObject) (apigen.ListTicketTimeResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, read); perr != nil {
		return nil, perr
	}
	const op = "listTicketTime"
	scope := fmt.Sprintf("%s/%s/%d", t.ID, req.Project, req.Number)
	size := s.h.pageSize(req.Params.Limit)
	var rows []readq.ListTicketTimeRow
	var sum int64
	var key string
	err := s.db.InTenant(ctx, t.ID, func(r *store.Reader) error {
		tc, err := visibleTicket(ctx, r, t, req.Project, req.Number)
		if err != nil {
			return err
		}
		key = ticketKey(t, tc.row)
		after, err := s.uuidAfter(op, scope, req.Params.Cursor)
		if err != nil {
			return err
		}
		if rows, err = r.ListTicketTime(ctx, readq.ListTicketTimeParams{TenantID: t.ID, TicketID: tc.row.ID, After: after, PageSize: limitArg(size)}); err != nil {
			return err
		}
		sum, err = r.SumTicketTime(ctx, readq.SumTicketTimeParams{TenantID: t.ID, TicketID: tc.row.ID})
		return err
	})
	if err != nil {
		return nil, err
	}
	rows, next := page(s.h, rows, size, op, scope, func(e readq.ListTicketTimeRow) string { return e.ID.String() })
	total := int(sum)
	out := apigen.ListTicketTime200JSONResponse{Items: make([]apigen.TimeEntry, 0, len(rows)), NextCursor: nullableString(next), TotalMinutes: &total}
	for _, e := range rows {
		out.Items = append(out.Items, timeView(key, timeEntry(e)))
	}
	return out, nil
}

// GetTimeEntry answers one entry.
func (s *Server) GetTimeEntry(ctx context.Context, req apigen.GetTimeEntryRequestObject) (apigen.GetTimeEntryResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, read); perr != nil {
		return nil, perr
	}
	var tc ticketCtx
	var e timeEntry
	err := s.db.InTenant(ctx, t.ID, func(r *store.Reader) error {
		var err error
		tc, e, err = visibleTimeEntry(ctx, r, t, req.Project, req.Number, req.Entry)
		return err
	})
	if err != nil {
		return nil, err
	}
	return apigen.GetTimeEntry200JSONResponse{Body: timeView(ticketKey(t, tc.row), e), Headers: apigen.GetTimeEntry200ResponseHeaders{ETag: etag(e.Version)}}, nil
}

// BookTime books the caller's own time on a ticket (docs/adr/0017 D6).
func (s *Server) BookTime(ctx context.Context, req apigen.BookTimeRequestObject) (apigen.BookTimeResponseObject, error) {
	t := tenantFrom(ctx)
	body := *req.Body
	// An act no agent can be given is refused before the key an agent's POST
	// would otherwise need.
	if perr := auth.Authorize(principal(ctx), t.Role, booking); perr != nil {
		return nil, perr
	}
	ctx, perr := keyed(ctx, req.Params.IdempotencyKey, "bookTime", fmt.Sprintf("%s/%s/%d", t.ID, req.Project, req.Number), body)
	if perr != nil {
		return nil, perr
	}
	var booked timeEntry
	var key, location string
	replay, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		tc, err := visibleTicket(ctx, w.Reader, t, req.Project, req.Number)
		if err != nil {
			return err
		}
		p := principal(ctx)
		if perr := auth.Authorize(p, tc.role, booking); perr != nil {
			return perr
		}
		if err := checkUnlocked(ctx, w, t, body.Day.Time); err != nil {
			return err
		}
		id, err := w.InsertTimeEntry(ctx, writeq.InsertTimeEntryParams{TenantID: t.ID, TicketID: tc.row.ID, PersonID: p.PersonID,
			AuthorID: p.PersonID, Minutes: clamp32(body.Minutes), Day: body.Day.Time, Note: deref(body.Note)})
		if err != nil {
			return fmt.Errorf("book the time: %w", err)
		}
		key = ticketKey(t, tc.row)
		w.Record(store.Event{EntityType: entityTimeEntry, EntityID: id, TicketID: tc.row.ID, TicketKey: key, Action: "booked",
			After: map[string]any{fieldMinutes: body.Minutes, fieldDay: body.Day.Format(dayLayout), fieldNote: deref(body.Note)}})
		if booked, err = w.GetTimeEntry(ctx, readq.GetTimeEntryParams{TenantID: t.ID, TicketID: tc.row.ID, ID: id}); err != nil {
			return err
		}
		location = ticketURL(t, tc.project.Key, tc.row.Number) + "/time-entries/" + id.String()
		res, err := stored(timeView(key, booked), map[string]string{headerETag: *etag(booked.Version), headerLocation: location})
		if err != nil {
			return err
		}
		w.Respond(res)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if replay != nil {
		body, err := replayed[apigen.TimeEntry](replay)
		if err != nil {
			return nil, err
		}
		return apigen.BookTime201JSONResponse{Body: body, Headers: apigen.BookTime201ResponseHeaders{
			ETag: header(replay, headerETag), Location: header(replay, headerLocation)}}, nil
	}
	return apigen.BookTime201JSONResponse{Body: timeView(key, booked), Headers: apigen.BookTime201ResponseHeaders{
		ETag: etag(booked.Version), Location: &location}}, nil
}

// mayChangeTime holds a correction or a voiding to the entry's author and
// to a live entry.
func mayChangeTime(p auth.Principal, tc ticketCtx, e timeEntry) *problem.Error {
	if perr := auth.Authorize(p, tc.role, booking); perr != nil {
		return perr
	}
	if e.AuthorID != p.PersonID {
		return problem.New(problem.Forbidden, "only the author changes a time entry")
	}
	return nil
}

// EditTimeEntry corrects an entry and keeps its previous values
// (docs/adr/0017 D7); the old and the new day must both lie after the lock.
func (s *Server) EditTimeEntry(ctx context.Context, req apigen.EditTimeEntryRequestObject) (apigen.EditTimeEntryResponseObject, error) {
	t := tenantFrom(ctx)
	version, perr := ifMatch(req.Params.IfMatch)
	if perr != nil {
		return nil, perr
	}
	var key string
	var out timeEntry
	_, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		tc, e, err := visibleTimeEntry(ctx, w.Reader, t, req.Project, req.Number, req.Entry)
		if err != nil {
			return err
		}
		key = ticketKey(t, tc.row)
		p := principal(ctx)
		if perr := mayChangeTime(p, tc, e); perr != nil {
			return perr
		}
		if e.VoidedAt != nil {
			return problem.New(problem.StateConflict, "the time entry is voided")
		}
		before := map[string]any{fieldMinutes: int(e.Minutes), fieldDay: e.Day.Format(dayLayout), fieldNote: e.Note}
		up := timePatch(e, version, *req.Body)
		up.TenantID = t.ID
		if e.Version != version {
			return stale(e.Version, before)
		}
		changedBefore, changedAfter := diffDeep(before, map[string]any{fieldMinutes: int(up.Minutes), fieldDay: up.Day.Format(dayLayout), fieldNote: up.Note})
		if len(changedAfter) == 0 {
			out = e
			return store.ErrNoChange
		}
		if err := checkUnlocked(ctx, w, t, e.Day, up.Day); err != nil {
			return err
		}
		if err := w.InsertTimeEntryRevision(ctx, writeq.InsertTimeEntryRevisionParams{TenantID: t.ID, EntryID: e.ID, Minutes: e.Minutes,
			Day: e.Day, Note: e.Note, EditedBy: p.PersonID}); err != nil {
			return fmt.Errorf("keep the previous values: %w", err)
		}
		if _, err := w.UpdateTimeEntry(ctx, up); errors.Is(err, pgx.ErrNoRows) {
			return stale(e.Version, before)
		} else if err != nil {
			return err
		}
		w.Record(store.Event{EntityType: entityTimeEntry, EntityID: e.ID, TicketID: tc.row.ID, TicketKey: key, Action: actionEdited,
			Before: changedBefore, After: changedAfter})
		out, err = w.GetTimeEntry(ctx, readq.GetTimeEntryParams{TenantID: t.ID, TicketID: tc.row.ID, ID: e.ID})
		return err
	})
	if err != nil && !errors.Is(err, store.ErrNoChange) {
		return nil, err
	}
	return apigen.EditTimeEntry200JSONResponse{Body: timeView(key, out), Headers: apigen.EditTimeEntry200ResponseHeaders{ETag: etag(out.Version)}}, nil
}

// timePatch applies a correction to an entry's values.
func timePatch(e timeEntry, version int32, body apigen.TimeEntryPatch) writeq.UpdateTimeEntryParams {
	up := writeq.UpdateTimeEntryParams{TenantID: uuid.Nil, ID: e.ID, Version: version, Minutes: e.Minutes, Day: e.Day, Note: e.Note}
	if body.Minutes != nil {
		up.Minutes = clamp32(*body.Minutes)
	}
	if body.Day != nil {
		up.Day = body.Day.Time
	}
	if body.Note != nil {
		up.Note = *body.Note
	}
	return up
}

// VoidTimeEntry voids an entry: kept, excluded from every sum
// (docs/adr/0017 D7); voiding a voided entry changes nothing.
func (s *Server) VoidTimeEntry(ctx context.Context, req apigen.VoidTimeEntryRequestObject) (apigen.VoidTimeEntryResponseObject, error) {
	t := tenantFrom(ctx)
	var key string
	var out timeEntry
	_, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		tc, e, err := visibleTimeEntry(ctx, w.Reader, t, req.Project, req.Number, req.Entry)
		if err != nil {
			return err
		}
		key = ticketKey(t, tc.row)
		p := principal(ctx)
		if perr := mayChangeTime(p, tc, e); perr != nil {
			return perr
		}
		if e.VoidedAt != nil {
			out = e
			return store.ErrNoChange
		}
		if err := checkUnlocked(ctx, w, t, e.Day); err != nil {
			return err
		}
		if err := w.VoidTimeEntry(ctx, writeq.VoidTimeEntryParams{TenantID: t.ID, ID: e.ID, VoidedBy: &p.PersonID}); err != nil {
			return fmt.Errorf("void the time entry: %w", err)
		}
		w.Record(store.Event{EntityType: entityTimeEntry, EntityID: e.ID, TicketID: tc.row.ID, TicketKey: key, Action: "voided"})
		out, err = w.GetTimeEntry(ctx, readq.GetTimeEntryParams{TenantID: t.ID, TicketID: tc.row.ID, ID: e.ID})
		return err
	})
	if err != nil && !errors.Is(err, store.ErrNoChange) {
		return nil, err
	}
	return apigen.VoidTimeEntry200JSONResponse{Body: timeView(key, out), Headers: apigen.VoidTimeEntry200ResponseHeaders{ETag: etag(out.Version)}}, nil
}

// ListTimeEntryRevisions lists an entry's previous values, oldest first.
func (s *Server) ListTimeEntryRevisions(ctx context.Context, req apigen.ListTimeEntryRevisionsRequestObject) (apigen.ListTimeEntryRevisionsResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, read); perr != nil {
		return nil, perr
	}
	const op = "listTimeEntryRevisions"
	scope := fmt.Sprintf("%s/%s", t.ID, req.Entry)
	size := s.h.pageSize(req.Params.Limit)
	var rows []readq.ListTimeEntryRevisionsRow
	err := s.db.InTenant(ctx, t.ID, func(r *store.Reader) error {
		if _, _, err := visibleTimeEntry(ctx, r, t, req.Project, req.Number, req.Entry); err != nil {
			return err
		}
		after, err := s.uuidAfter(op, scope, req.Params.Cursor)
		if err != nil {
			return err
		}
		rows, err = r.ListTimeEntryRevisions(ctx, readq.ListTimeEntryRevisionsParams{TenantID: t.ID, EntryID: req.Entry, After: after, PageSize: limitArg(size)})
		return err
	})
	if err != nil {
		return nil, err
	}
	rows, next := page(s.h, rows, size, op, scope, func(r readq.ListTimeEntryRevisionsRow) string { return r.ID.String() })
	out := apigen.ListTimeEntryRevisions200JSONResponse{Items: make([]apigen.TimeEntryRevision, 0, len(rows)), NextCursor: nullableString(next)}
	for _, r := range rows {
		out.Items = append(out.Items, apigen.TimeEntryRevision{Minutes: int(r.Minutes), Day: openapi_types.Date{Time: r.Day}, Note: r.Note,
			EditedBy: personView(r.EditedBy, r.EditedByUsername, r.EditedByName), At: r.CreatedAt})
	}
	return out, nil
}

// timeFilter is the period, project, ticket and person filter of the
// tenant's time lists.
type timeFilter struct {
	from, to *time.Time
	project  *string
	number   *int32
	person   *uuid.UUID
}

func parseTimeFilter(p auth.Principal, from, to *openapi_types.Date, project, ticket, person *string) (timeFilter, *problem.Error) {
	var f timeFilter
	if from != nil {
		f.from = &from.Time
	}
	if to != nil {
		f.to = &to.Time
	}
	f.project = project
	if ticket != nil {
		k, err := domain.ParseTicketKey(*ticket)
		if err != nil {
			return f, problem.Field("query:ticket", err.Error())
		}
		f.number = &k.Number
		if f.project != nil && *f.project != k.Project {
			return f, problem.Field("query:ticket", "the ticket is not of the project the filter names")
		}
		f.project = &k.Project
	}
	if person != nil {
		id := p.PersonID
		if *person != filterMe {
			parsed, err := uuid.Parse(*person)
			if err != nil {
				return f, problem.Field("query:person", "not a person id or me")
			}
			id = parsed
		}
		f.person = &id
	}
	return f, nil
}

// ListTenantTime lists the tenant's visible entries, newest first: cursor
// pages, or numbered pages with a total (docs/adr/0048 D2); CSV on request.
func (s *Server) ListTenantTime(ctx context.Context, req apigen.ListTenantTimeRequestObject) (apigen.ListTenantTimeResponseObject, error) {
	t := tenantFrom(ctx)
	p := principal(ctx)
	if perr := auth.Authorize(p, t.Role, read); perr != nil {
		return nil, perr
	}
	q := req.Params
	f, perr := parseTimeFilter(p, q.From, q.To, q.Project, q.Ticket, q.Person)
	if perr != nil {
		return nil, perr
	}
	const op = "listTenantTime"
	scope := t.ID.String()
	params := readq.ListTenantTimeParams{TenantID: t.ID, FromDay: f.from, ToDay: f.to, ProjectKey: f.project, TicketNumber: f.number,
		PersonID: f.person, IncludeVoided: q.IncludeVoided != nil && *q.IncludeVoided}
	numbered, perPage, size, perr := s.timePaging(q, &params)
	if perr != nil {
		return nil, perr
	}
	if !numbered && q.Cursor != nil {
		before, err := s.uuidAfter(op, scope, q.Cursor)
		if err != nil {
			return nil, err
		}
		params.Before = before
	}
	var rows []readq.ListTenantTimeRow
	var total int64
	err := s.db.InTenant(ctx, t.ID, func(r *store.Reader) error {
		var err error
		if rows, err = r.ListTenantTime(ctx, params); err != nil || !numbered {
			return err
		}
		total, err = r.CountTenantTime(ctx, readq.CountTenantTimeParams{TenantID: t.ID, FromDay: f.from, ToDay: f.to, ProjectKey: f.project,
			TicketNumber: f.number, PersonID: f.person, IncludeVoided: params.IncludeVoided})
		return err
	})
	if err != nil {
		return nil, err
	}
	out := apigen.TimeEntryList{Items: []apigen.TimeEntry{}, NextCursor: nullableString(nil)}
	if numbered {
		n := int(total)
		out.Total, out.Page, out.PerPage = &n, q.Page, &perPage
	} else {
		var next *string
		rows, next = page(s.h, rows, size, op, scope, func(e readq.ListTenantTimeRow) string { return e.ID.String() })
		out.NextCursor = nullableString(next)
	}
	for _, e := range rows {
		out.Items = append(out.Items, timeView(domain.FullKey(t.Slug, e.ProjectKey, e.TicketNumber), tenantTimeEntry(e)))
	}
	if wantsCSV(ctx) {
		body := timeCSV(out.Items)
		return apigen.ListTenantTime200TextcsvResponse{Body: bytes.NewReader(body), ContentLength: int64(len(body))}, nil
	}
	return apigen.ListTenantTime200JSONResponse(out), nil
}

// timePaging sets the page of the tenant's time list.
func (s *Server) timePaging(q apigen.ListTenantTimeParams, params *readq.ListTenantTimeParams) (numbered bool, perPage, size int, perr *problem.Error) {
	switch {
	case q.Page != nil && (q.Cursor != nil || q.Limit != nil):
		return false, 0, 0, problem.Field("query:page", "a numbered page takes per_page, neither cursor nor limit")
	case q.Page == nil && q.PerPage != nil:
		return false, 0, 0, problem.Field("query:per_page", "per_page goes with page")
	case q.Page != nil:
		perPage = defaultPageSize
		if q.PerPage != nil {
			perPage = int(*q.PerPage)
		}
		perPage = s.h.pageSize(&perPage)
		if *q.Page > maxPageDepth/perPage {
			return false, 0, 0, &problem.Error{Code: problem.PageTooDeep, Detail: "pages end at row " + strconv.Itoa(maxPageDepth) + "; narrow the period",
				Errors: []problem.FieldError{{Pointer: "query:page", Message: "too deep"}}}
		}
		params.PageSize, params.PageOffset = clamp32(perPage), clamp32((*q.Page-1)*perPage)
		return true, perPage, perPage, nil
	}
	size = s.h.pageSize(q.Limit)
	params.PageSize = limitArg(size)
	return false, 0, size, nil
}

// clamp32 converts a count the document or the depth cap bounds; a value
// outside int32 is clamped, never wrapped.
func clamp32(n int) int32 {
	switch {
	case n < 0:
		return 0
	case n > math.MaxInt32:
		return math.MaxInt32
	}
	return int32(n)
}

func tenantTimeEntry(e readq.ListTenantTimeRow) timeEntry {
	return timeEntry{ID: e.ID, PersonID: e.PersonID, PersonUsername: e.PersonUsername, PersonName: e.PersonName,
		AuthorID: e.AuthorID, AuthorUsername: e.AuthorUsername, AuthorName: e.AuthorName, Minutes: e.Minutes, Day: e.Day,
		Note: e.Note, VoidedAt: e.VoidedAt, Edited: e.Edited, Version: e.Version, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt}
}

// timeCSV writes entries as CSV: days as YYYY-MM-DD, times as RFC 3339,
// minutes as integers, formula cells neutralised.
func timeCSV(items []apigen.TimeEntry) []byte {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{"id", groupByTicket, fieldDay, fieldMinutes, "person_id", "person", fieldNote, "voided_at", "created_at"})
	for _, e := range items {
		voided := ""
		if e.VoidedAt.IsSpecified() && !e.VoidedAt.IsNull() {
			voided = e.VoidedAt.MustGet().UTC().Format(time.RFC3339)
		}
		_ = w.Write(neutralise([]string{e.Id.String(), e.Ticket, e.Day.Format(dayLayout), strconv.Itoa(e.Minutes),
			e.Person.Id.String(), e.Person.DisplayName, e.Note, voided, e.CreatedAt.UTC().Format(time.RFC3339)}))
	}
	w.Flush()
	return buf.Bytes()
}

// TimeReport sums the visible, not voided minutes per ticket, project,
// person or for the tenant over a period (docs/adr/0017 D10).
func (s *Server) TimeReport(ctx context.Context, req apigen.TimeReportRequestObject) (apigen.TimeReportResponseObject, error) {
	t := tenantFrom(ctx)
	p := principal(ctx)
	if perr := auth.Authorize(p, t.Role, read); perr != nil {
		return nil, perr
	}
	q := req.Params
	f, perr := parseTimeFilter(p, q.From, q.To, q.Project, nil, q.Person)
	if perr != nil {
		return nil, perr
	}
	groupBy := groupByTicket
	if q.GroupBy != nil {
		groupBy = string(*q.GroupBy)
	}
	var rows []readq.TimeReportRow
	err := s.db.InTenant(ctx, t.ID, func(r *store.Reader) error {
		var err error
		rows, err = r.TimeReport(ctx, readq.TimeReportParams{GroupBy: groupBy, TenantID: t.ID, FromDay: f.from, ToDay: f.to,
			ProjectKey: f.project, PersonID: f.person})
		return err
	})
	if err != nil {
		return nil, err
	}
	out := apigen.TimeReport{GroupBy: apigen.TimeReportGroupBy(groupBy), Items: []struct {
		Key     string `json:"key"`
		Label   string `json:"label"`
		Minutes int    `json:"minutes"`
	}{}}
	var buf bytes.Buffer
	cw := csv.NewWriter(&buf)
	_ = cw.Write([]string{groupBy, "label", fieldMinutes})
	for _, r := range rows {
		key := r.GroupKey
		if groupBy == groupByTicket {
			key = t.Slug + "/" + key
		}
		out.Items = append(out.Items, struct {
			Key     string `json:"key"`
			Label   string `json:"label"`
			Minutes int    `json:"minutes"`
		}{Key: key, Label: r.Label, Minutes: int(r.Minutes)})
		out.TotalMinutes += int(r.Minutes)
		_ = cw.Write(neutralise([]string{key, r.Label, strconv.FormatInt(r.Minutes, 10)}))
	}
	if wantsCSV(ctx) {
		cw.Flush()
		body := buf.Bytes()
		return apigen.TimeReport200TextcsvResponse{Body: bytes.NewReader(body), ContentLength: int64(len(body))}, nil
	}
	return apigen.TimeReport200JSONResponse(out), nil
}
