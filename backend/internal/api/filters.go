package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	entitySavedFilter = "saved_filter"
	fieldShared       = "shared"
	fieldParameters   = "parameters"
)

// filing a filter is the person's own write: any role of the tenant, with
// write scope. No record lists it among an agent's acts, so it is open to
// agents as the other unlisted acts are (docs/adr/0043).
var filterNeed = auth.Need{Role: domain.RoleViewer, Scope: domain.ScopeWrite}

func filterURL(t tenantScope, id uuid.UUID) string {
	return "/api/v1/tenants/" + t.Slug + "/filters/" + id.String()
}

// ListSavedFilters answers the caller's filters and the tenant's shared ones,
// oldest first, each checked as it is read (docs/adr/0049 D7); a poll that
// finds the page unchanged is a 304 (docs/adr/0054 D7).
func (s *Server) ListSavedFilters(ctx context.Context, req apigen.ListSavedFiltersRequestObject) (apigen.ListSavedFiltersResponseObject, error) {
	t := tenantFrom(ctx)
	p := principal(ctx)
	if perr := auth.Authorize(p, t.Role, read); perr != nil {
		return nil, perr
	}
	const op = "listSavedFilters"
	scope := t.ID.String() + "/" + p.PersonID.String()
	size := s.h.pageSize(req.Params.Limit)
	out := apigen.SavedFilterList{Items: []apigen.SavedFilter{}}
	err := s.db.InTenant(ctx, t.ID, func(r *store.Reader) error {
		after, err := s.uuidAfter(op, scope, req.Params.Cursor)
		if err != nil {
			return err
		}
		rows, err := r.ListSavedFilters(ctx, readq.ListSavedFiltersParams{TenantID: t.ID, UserID: p.PersonID, After: after,
			PageSize: limitArg(size)})
		if err != nil {
			return err
		}
		rows, next := page(s.h, rows, size, op, scope, func(f readq.ListSavedFiltersRow) string { return f.ID.String() })
		out.NextCursor = nullableString(next)
		for _, row := range rows {
			v, err := s.filterView(ctx, r, t, readq.GetSavedFilterRow(row))
			if err != nil {
				return err
			}
			out.Items = append(out.Items, v)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	tag, unchanged := listTag(req.Params.IfNoneMatch, out)
	if unchanged {
		return apigen.ListSavedFilters304Response{Headers: apigen.NotModifiedResponseHeaders{ETag: &tag}}, nil
	}
	return apigen.ListSavedFilters200JSONResponse{Body: out, Headers: apigen.ListSavedFilters200ResponseHeaders{ETag: &tag}}, nil
}

// GetSavedFilter answers one of the caller's filters or a shared one.
func (s *Server) GetSavedFilter(ctx context.Context, req apigen.GetSavedFilterRequestObject) (apigen.GetSavedFilterResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, read); perr != nil {
		return nil, perr
	}
	var (
		v       apigen.SavedFilter
		version int32
	)
	err := s.db.InTenant(ctx, t.ID, func(r *store.Reader) error {
		row, err := visibleFilter(ctx, r, t, req.Filter)
		if err != nil {
			return err
		}
		version = row.Version
		v, err = s.filterView(ctx, r, t, row)
		return err
	})
	if err != nil {
		return nil, err
	}
	return apigen.GetSavedFilter200JSONResponse{Body: v, Headers: apigen.GetSavedFilter200ResponseHeaders{ETag: etag(version)}}, nil
}

// CreateSavedFilter saves a filter of the caller's (docs/adr/0018 D5); its
// parameters are refused as a list refuses them (docs/adr/0049 D4).
func (s *Server) CreateSavedFilter(ctx context.Context, req apigen.CreateSavedFilterRequestObject) (apigen.CreateSavedFilterResponseObject, error) {
	t := tenantFrom(ctx)
	p := principal(ctx)
	if perr := auth.Authorize(p, t.Role, filterNeed); perr != nil {
		return nil, perr
	}
	body := *req.Body
	ctx, perr := s.keyed(ctx, req.Params.IdempotencyKey, "createSavedFilter", t.ID.String(), body)
	if perr != nil {
		return nil, perr
	}
	name, params, perr := s.checkFilter(p.PersonID, body.Name, body.Parameters)
	if perr != nil {
		return nil, perr
	}
	shared := body.Shared != nil && *body.Shared
	var (
		created apigen.SavedFilter
		version int32
	)
	replay, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		id, err := w.InsertSavedFilter(ctx, writeq.InsertSavedFilterParams{TenantID: t.ID, OwnerID: p.PersonID, Name: name,
			Parameters: params, Shared: shared})
		if err != nil {
			return fmt.Errorf("save the filter: %w", err)
		}
		w.Record(store.Event{EntityType: entitySavedFilter, EntityID: id, Action: actionCreated,
			After: map[string]any{fieldName: name, fieldShared: shared, fieldParameters: json.RawMessage(params)}})
		row, err := visibleFilter(ctx, w.Reader, t, id)
		if err != nil {
			return err
		}
		version = row.Version
		if created, err = s.filterView(ctx, w.Reader, t, row); err != nil {
			return err
		}
		res, err := stored(created, map[string]string{headerETag: *etag(version), headerLocation: filterURL(t, id)})
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
		body, err := replayed[apigen.SavedFilter](replay)
		if err != nil {
			return nil, err
		}
		return apigen.CreateSavedFilter201JSONResponse{Body: body, Headers: apigen.CreateSavedFilter201ResponseHeaders{
			ETag: header(replay, headerETag), Location: header(replay, headerLocation)}}, nil
	}
	location := filterURL(t, created.Id)
	return apigen.CreateSavedFilter201JSONResponse{Body: created, Headers: apigen.CreateSavedFilter201ResponseHeaders{
		ETag: etag(version), Location: &location}}, nil
}

// UpdateSavedFilter renames, changes, shares or unshares the caller's filter
// with If-Match (docs/adr/0050 D3); a tenant administrator unshares another
// person's shared filter the same way (docs/adr/0018 D5 as amended
// 2026-10-06).
func (s *Server) UpdateSavedFilter(ctx context.Context, req apigen.UpdateSavedFilterRequestObject) (apigen.UpdateSavedFilterResponseObject, error) {
	t := tenantFrom(ctx)
	p := principal(ctx)
	if perr := auth.Authorize(p, t.Role, filterNeed); perr != nil {
		return nil, perr
	}
	version, perr := ifMatch(req.Params.IfMatch)
	if perr != nil {
		return nil, perr
	}
	var (
		out     apigen.SavedFilter
		current int32
	)
	_, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		row, err := visibleFilter(ctx, w.Reader, t, req.Filter)
		if err != nil {
			return err
		}
		another, perr := mayChangeFilter(p, t.Role, row.OwnerID, req.Body)
		if perr != nil {
			return perr
		}
		if another {
			out, current, err = s.unshareAnothersFilter(ctx, w, t, row, version)
			return err
		}
		ch, err := s.applyFilterPatch(p.PersonID, row, *req.Body)
		if err != nil {
			return err
		}
		if row.Version != version {
			return stale(row.Version, ch.current)
		}
		if len(ch.after) == 0 {
			current = row.Version
			out, err = s.filterView(ctx, w.Reader, t, row)
			if err != nil {
				return err
			}
			return store.ErrNoChange
		}
		_, err = w.UpdateSavedFilter(ctx, writeq.UpdateSavedFilterParams{TenantID: t.ID, ID: row.ID, OwnerID: p.PersonID,
			Version: version, Name: ch.name, Parameters: ch.params, Shared: ch.shared})
		if errors.Is(err, pgx.ErrNoRows) {
			return stale(row.Version, ch.current)
		}
		if err != nil {
			return err
		}
		w.Record(store.Event{EntityType: entitySavedFilter, EntityID: row.ID, Action: actionUpdated, Before: ch.before, After: ch.after})
		if row, err = visibleFilter(ctx, w.Reader, t, row.ID); err != nil {
			return err
		}
		current = row.Version
		out, err = s.filterView(ctx, w.Reader, t, row)
		return err
	})
	if err != nil && !errors.Is(err, store.ErrNoChange) {
		return nil, err
	}
	return apigen.UpdateSavedFilter200JSONResponse{Body: out, Headers: apigen.UpdateSavedFilter200ResponseHeaders{ETag: etag(current)}}, nil
}

// unshareAnothersFilter is a tenant administrator's unshare of another
// person's shared filter at the version the request read: shared and nothing
// else changes, recorded as updated. The filter answered is the one the
// administrator no longer reads.
func (s *Server) unshareAnothersFilter(ctx context.Context, w *store.Writer, t tenantScope, row readq.GetSavedFilterRow,
	version int32) (apigen.SavedFilter, int32, error) {
	current := map[string]any{fieldShared: row.Shared}
	if row.Version != version {
		return apigen.SavedFilter{}, 0, stale(row.Version, current)
	}
	changed, err := w.UnshareAnothersFilter(ctx, row.ID, version)
	if errors.Is(err, pgx.ErrNoRows) {
		return apigen.SavedFilter{}, 0, stale(row.Version, current)
	}
	if err != nil {
		return apigen.SavedFilter{}, 0, err
	}
	w.Record(store.Event{EntityType: entitySavedFilter, EntityID: row.ID, Action: actionUpdated,
		Before: map[string]any{fieldShared: true}, After: map[string]any{fieldShared: false}})
	row.Shared, row.Version, row.UpdatedAt = false, changed.Version, changed.UpdatedAt
	out, err := s.filterView(ctx, w.Reader, t, row)
	return out, changed.Version, err
}

// DeleteSavedFilter removes the caller's filter, or — a tenant
// administrator's act — another person's shared one (docs/adr/0018 D5 as
// amended 2026-10-06).
func (s *Server) DeleteSavedFilter(ctx context.Context, req apigen.DeleteSavedFilterRequestObject) (apigen.DeleteSavedFilterResponseObject, error) {
	t := tenantFrom(ctx)
	p := principal(ctx)
	if perr := auth.Authorize(p, t.Role, filterNeed); perr != nil {
		return nil, perr
	}
	_, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		row, err := visibleFilter(ctx, w.Reader, t, req.Filter)
		if err != nil {
			return err
		}
		another, perr := mayChangeFilter(p, t.Role, row.OwnerID, nil)
		if perr != nil {
			return perr
		}
		var n int64
		if another {
			n, err = w.DeleteSharedSavedFilter(ctx, writeq.DeleteSharedSavedFilterParams{TenantID: t.ID, ID: row.ID})
		} else {
			n, err = w.DeleteSavedFilter(ctx, writeq.DeleteSavedFilterParams{TenantID: t.ID, ID: row.ID, OwnerID: p.PersonID})
		}
		if err != nil {
			return err
		}
		if n == 0 {
			// A concurrent deletion came first, or its owner stopped sharing
			// it: the filter is gone for this request as for any later one.
			return noSuchFilter()
		}
		w.Record(store.Event{EntityType: entitySavedFilter, EntityID: row.ID, Action: actionDeleted,
			Before: map[string]any{fieldName: row.Name, fieldShared: row.Shared}})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return apigen.DeleteSavedFilter204Response{}, nil
}

func noSuchFilter() *problem.Error { return problem.New(problem.NotFound, "no such saved filter") }

// visibleFilter reads one of the caller's filters or a shared one; any other
// is the same 404 as one that does not exist.
func visibleFilter(ctx context.Context, r *store.Reader, t tenantScope, id uuid.UUID) (readq.GetSavedFilterRow, error) {
	row, err := r.GetSavedFilter(ctx, readq.GetSavedFilterParams{TenantID: t.ID, ID: id, UserID: principal(ctx).PersonID})
	if errors.Is(err, pgx.ErrNoRows) {
		return row, noSuchFilter()
	}
	return row, err
}

// mayChangeFilter holds a change of a filter the caller can see — their own
// or a shared one — to docs/adr/0018 D5 as amended 2026-10-06: its owner
// changes it; a tenant administrator unshares another person's shared filter,
// a patch of shared false and nothing else, or deletes it, an administration
// act with admin scope that no agent makes (docs/adr/0043 D3). patch is nil
// for a deletion. It reports whether the act is on another person's filter.
func mayChangeFilter(p auth.Principal, role domain.Role, owner uuid.UUID, patch *apigen.SavedFilterPatch) (bool, *problem.Error) {
	if owner == p.PersonID {
		return false, nil
	}
	if role != domain.RoleAdmin {
		return false, problem.New(problem.Forbidden, "only its owner changes a saved filter; a tenant administrator unshares or deletes a shared one")
	}
	if perr := auth.Authorize(p, role, administer); perr != nil {
		return false, perr
	}
	if patch != nil && (patch.Name != nil || patch.Parameters != nil || patch.Shared == nil || *patch.Shared) {
		return false, problem.New(problem.Forbidden, "an administrator unshares another person's filter and changes nothing else of it")
	}
	return true, nil
}

// checkFilter holds a filter's name and parameters to what a list takes, with
// every refused value at /parameters/<name> (docs/adr/0049 D4), and returns
// the name trimmed and the parameters as stored.
func (s *Server) checkFilter(me uuid.UUID, name string, params apigen.SavedFilterParameters) (string, []byte, *problem.Error) {
	name = strings.TrimSpace(name)
	var errs []problem.FieldError
	if name == "" {
		errs = append(errs, problem.FieldError{Pointer: "/name", Message: "a saved filter needs a name"})
	}
	for _, e := range s.parseFilters(me, filterQuery(params), &ticketListing{}) {
		e.Pointer = "/parameters/" + strings.TrimPrefix(e.Pointer, "query:")
		errs = append(errs, e)
	}
	if len(errs) > 0 {
		return "", nil, &problem.Error{Code: problem.ValidationFailed, Detail: "the filter has values the ticket lists do not take", Errors: errs}
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return "", nil, problem.New(problem.ValidationFailed, "the parameters do not encode")
	}
	return name, raw, nil
}

// filterQuery is a saved filter's parameters as a list's query.
func filterQuery(p apigen.SavedFilterParameters) ticketQuery {
	return ticketQuery{project: p.Project, state: p.State, typ: p.Type, severity: p.Severity, security: p.Security,
		horizon: p.Horizon, effort: p.Effort, assignee: p.Assignee, reporter: p.Reporter, parent: p.Parent,
		interest: p.Interest, progressMin: p.ProgressMin, progressMax: p.ProgressMax, openedAfter: p.OpenedAfter,
		openedBefore: p.OpenedBefore, updatedAfter: p.UpdatedAfter, updatedBefore: p.UpdatedBefore, doneAfter: p.DoneAfter,
		q: p.Q, includeTerminal: p.IncludeTerminal, blocked: p.Blocked, hasOpenQuestions: p.HasOpenQuestions}
}

// filterChange is a patch applied to a filter: the values it ends with, the
// fields that changed before and after, and the current values a 412 names.
type filterChange struct {
	name          string
	params        []byte
	shared        bool
	before, after map[string]any
	current       map[string]any
}

func (s *Server) applyFilterPatch(me uuid.UUID, row readq.GetSavedFilterRow, p apigen.SavedFilterPatch) (filterChange, error) {
	ch := filterChange{name: row.Name, params: row.Parameters, shared: row.Shared,
		before: map[string]any{}, after: map[string]any{}, current: map[string]any{}}
	if p.Name != nil || p.Parameters != nil {
		name := row.Name
		if p.Name != nil {
			name = *p.Name
		}
		// Parameters left out are left as they are: a value that no longer holds
		// stays a warning (docs/adr/0049 D7) and does not refuse a rename.
		checkedName, raw := strings.TrimSpace(name), row.Parameters
		if p.Parameters != nil {
			var perr *problem.Error
			if checkedName, raw, perr = s.checkFilter(me, name, *p.Parameters); perr != nil {
				return ch, perr
			}
		} else if checkedName == "" {
			return ch, problem.Field("/name", "a saved filter needs a name")
		}
		if checkedName != row.Name {
			ch.before[fieldName], ch.after[fieldName], ch.current[fieldName] = row.Name, checkedName, row.Name
			ch.name = checkedName
		}
		if p.Parameters != nil && !jsonEqual(raw, row.Parameters) {
			ch.before[fieldParameters], ch.after[fieldParameters] = json.RawMessage(row.Parameters), json.RawMessage(raw)
			ch.current[fieldParameters] = json.RawMessage(row.Parameters)
			ch.params = raw
		}
	}
	if p.Shared != nil && *p.Shared != row.Shared {
		ch.before[fieldShared], ch.after[fieldShared], ch.current[fieldShared] = row.Shared, *p.Shared, row.Shared
		ch.shared = *p.Shared
	}
	return ch, nil
}

// jsonEqual compares two JSON documents by their content.
func jsonEqual(a, b []byte) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	ja, _ := json.Marshal(x)
	jb, _ := json.Marshal(y)
	return string(ja) == string(jb)
}

// filterView is a saved filter as its reader sees it. Its parameters are
// checked against the current vocabularies, and a value that no longer holds
// is a warning, not an error (docs/adr/0049 D7). Another person's filter that
// names a project or a ticket the reader cannot see — or one that no longer
// exists — is redacted: no parameters and no warnings, as an act that names a
// hidden ticket is shown without its payload (docs/adr/0065 D5). Its owner
// reads it whole, with a warning for each such value.
func (s *Server) filterView(ctx context.Context, r *store.Reader, t tenantScope, row readq.GetSavedFilterRow) (apigen.SavedFilter, error) {
	me := principal(ctx).PersonID
	v := apigen.SavedFilter{Id: row.ID, Name: row.Name, Owner: personView(row.OwnerID, row.OwnerUsername, row.OwnerName),
		Shared: row.Shared, Version: int(row.Version), CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		Warnings: []apigen.SavedFilterWarning{}}
	if err := json.Unmarshal(row.Parameters, &v.Parameters); err != nil {
		return v, fmt.Errorf("read the saved parameters: %w", err)
	}
	var l ticketListing
	for _, e := range s.parseFilters(me, filterQuery(v.Parameters), &l) {
		v.Warnings = append(v.Warnings, apigen.SavedFilterWarning{Parameter: strings.TrimPrefix(e.Pointer, "query:"), Message: e.Message})
	}
	hidden, err := hiddenNames(ctx, r, t, l)
	if err != nil {
		return v, err
	}
	if len(hidden) > 0 && row.OwnerID != me {
		return apigen.SavedFilter{Id: v.Id, Name: v.Name, Owner: v.Owner, Shared: v.Shared, Redacted: true, Version: v.Version,
			CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt, Warnings: []apigen.SavedFilterWarning{}}, nil
	}
	v.Warnings = append(v.Warnings, hidden...)
	return v, nil
}

// hiddenNames are the projects and the parent tickets a filter names that the
// reader cannot see or that do not exist, each a warning.
func hiddenNames(ctx context.Context, r *store.Reader, t tenantScope, l ticketListing) ([]apigen.SavedFilterWarning, error) {
	var out []apigen.SavedFilterWarning
	for _, key := range append(append([]string{}, l.filter.Projects.In...), l.filter.Projects.NotIn...) {
		if _, err := visibleProject(ctx, r, t, key); err != nil {
			var perr *problem.Error
			if !errors.As(err, &perr) {
				return nil, err
			}
			out = append(out, apigen.SavedFilterWarning{Parameter: "project", Message: "names no project you can see: " + key})
		}
	}
	for _, ref := range l.parents {
		id := uuid.Nil
		if k, err := ref.key.InTenant(t.Slug); err == nil {
			found, err := ticketIDByKey(ctx, r, t, k)
			if err != nil {
				return nil, err
			}
			id = found
		}
		if id == uuid.Nil {
			out = append(out, apigen.SavedFilterWarning{Parameter: "parent", Message: "names no ticket you can see: " + domain.ShortKey(ref.key.Project, ref.key.Number)})
		}
	}
	return out, nil
}
