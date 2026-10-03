package api

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/problem"
	"github.com/guided-traffic/cowork/backend/internal/store"
	"github.com/guided-traffic/cowork/backend/internal/store/readq"
	"github.com/guided-traffic/cowork/backend/internal/store/writeq"
)

// project is the columns every project query returns.
type project = readq.GetProjectByKeyRow

func projectView(p project) apigen.Project {
	return apigen.Project{
		Id: p.ID, Key: p.Key, Name: p.Name, Description: p.Description, Restricted: p.Restricted,
		WipLimits: wipView(p.WipLimits), ArchivedAt: nullableOf(p.ArchivedAt), Version: int(p.Version),
		CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
	}
}

// wipView decodes the stored WIP limits; the column holds what the API wrote.
func wipView(raw []byte) apigen.WipLimits {
	var w apigen.WipLimits
	_ = json.Unmarshal(raw, &w)
	return w
}

// wipJSON encodes WIP limits for the column; none is an empty object.
func wipJSON(w *apigen.WipLimits) []byte {
	if w == nil {
		return []byte("{}")
	}
	b, _ := json.Marshal(w)
	return b
}

// edit is changing a project's name, description or WIP limits: a member's
// act with write scope, agents included — the open variant of a detail no
// record decides (docs/adr/0043's default of everything reversible).
var edit = auth.Need{Role: domain.RoleMember, Scope: domain.ScopeWrite}

func projectURL(t tenantScope, key string) string {
	return "/api/v1/tenants/" + t.Slug + "/projects/" + key
}

// ListProjects lists the projects the caller can see, by key: a restricted
// project only to its list and the administrators (docs/adr/0034 D3), a
// project-restricted token its own project only (docs/adr/0035 D3).
func (s *Server) ListProjects(ctx context.Context, req apigen.ListProjectsRequestObject) (apigen.ListProjectsResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, read); perr != nil {
		return nil, perr
	}
	const op = "listProjects"
	scope := t.ID.String()
	size := s.h.pageSize(req.Params.Limit)
	params := readq.ListProjectsParams{TenantID: t.ID, PageSize: limitArg(size)}
	if req.Params.IncludeArchived != nil {
		params.IncludeArchived = *req.Params.IncludeArchived
	}
	if req.Params.Cursor != nil {
		after, perr := s.cursors.decode(op, scope, *req.Params.Cursor)
		if perr != nil {
			return nil, perr
		}
		params.After = &after
	}
	var rows []readq.ListProjectsRow
	err := s.db.InTenant(ctx, t.ID, func(r *store.Reader) error {
		var err error
		rows, err = r.ListProjects(ctx, params)
		return err
	})
	if err != nil {
		return nil, err
	}
	rows, next := page(s.h, rows, size, op, scope, func(p readq.ListProjectsRow) string { return p.Key })
	out := apigen.ListProjects200JSONResponse{Items: []apigen.Project{}, NextCursor: nullableString(next)}
	for _, p := range rows {
		out.Items = append(out.Items, projectView(project(p)))
	}
	return out, nil
}

// CreateProject creates a project and its ticket counter. A write act, for
// members while the tenant allows it and for administrators always
// (docs/adr/0034 D9); an agent needs create-project (docs/adr/0043 D4).
func (s *Server) CreateProject(ctx context.Context, req apigen.CreateProjectRequestObject) (apigen.CreateProjectResponseObject, error) {
	t := tenantFrom(ctx)
	p := principal(ctx)
	body := *req.Body
	ctx, perr := s.keyed(ctx, req.Params.IdempotencyKey, "createProject", t.ID.String(), body)
	if perr != nil {
		return nil, perr
	}
	var created project
	replay, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		tenant, err := w.GetTenant(ctx, t.ID)
		if err != nil {
			return err
		}
		need := auth.Need{Role: domain.RoleAdmin, Scope: domain.ScopeWrite, Capability: auth.CapCreateProject}
		if tenant.MembersCreateProjects {
			need.Role = domain.RoleMember
		}
		if perr := auth.Authorize(p, t.Role, need); perr != nil {
			return perr
		}
		taken, err := w.ProjectKeyTaken(ctx, readq.ProjectKeyTakenParams{TenantID: t.ID, Key: body.Key})
		if err != nil {
			return err
		}
		if taken {
			return &problem.Error{Code: problem.ProjectKeyTaken, Detail: "the tenant has a project with this key",
				Errors: []problem.FieldError{{Pointer: "/key", Message: "taken"}}}
		}
		description := ""
		if body.Description != nil {
			description = *body.Description
		}
		row, err := w.InsertProject(ctx, writeq.InsertProjectParams{TenantID: t.ID, Key: body.Key, Name: body.Name,
			Description: description, WipLimits: wipJSON(body.WipLimits)})
		if err != nil {
			return err
		}
		if err := w.InsertTicketCounter(ctx, writeq.InsertTicketCounterParams{TenantID: t.ID, ProjectID: row.ID}); err != nil {
			return err
		}
		created = project(row)
		w.Record(store.Event{EntityType: entityProject, EntityID: row.ID, Action: actionCreated,
			After: map[string]any{"key": row.Key, fieldName: row.Name, fieldDescription: row.Description}})
		res, err := stored(projectView(created), map[string]string{
			headerETag: *etag(created.Version), headerLocation: projectURL(t, created.Key)})
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
		body, err := replayed[apigen.Project](replay)
		if err != nil {
			return nil, err
		}
		return apigen.CreateProject201JSONResponse{Body: body, Headers: apigen.CreateProject201ResponseHeaders{
			ETag: header(replay, headerETag), Location: header(replay, headerLocation)}}, nil
	}
	location := projectURL(t, created.Key)
	return apigen.CreateProject201JSONResponse{Body: projectView(created), Headers: apigen.CreateProject201ResponseHeaders{
		ETag: etag(created.Version), Location: &location}}, nil
}

// GetProject answers one project the caller can see.
func (s *Server) GetProject(ctx context.Context, req apigen.GetProjectRequestObject) (apigen.GetProjectResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, read); perr != nil {
		return nil, perr
	}
	var row project
	err := s.db.InTenant(ctx, t.ID, func(r *store.Reader) error {
		var err error
		row, err = visibleProject(ctx, r, t, req.Project)
		return err
	})
	if err != nil {
		return nil, err
	}
	return apigen.GetProject200JSONResponse{Body: projectView(row), Headers: apigen.GetProject200ResponseHeaders{ETag: etag(row.Version)}}, nil
}

// visibleProject reads a project by key through the visibility predicate;
// one the caller cannot see is the same 404 as one that does not exist.
func visibleProject(ctx context.Context, r *store.Reader, t tenantScope, key string) (project, error) {
	row, err := r.GetProjectByKey(ctx, readq.GetProjectByKeyParams{TenantID: t.ID, Key: key})
	if errors.Is(err, pgx.ErrNoRows) {
		return project{}, problem.New(problem.NotFound, "no such project")
	}
	return row, err
}

// UpdateProject changes a project's name, description or WIP limits, with
// If-Match (docs/adr/0050 D3).
func (s *Server) UpdateProject(ctx context.Context, req apigen.UpdateProjectRequestObject) (apigen.UpdateProjectResponseObject, error) {
	t := tenantFrom(ctx)
	version, perr := ifMatch(req.Params.IfMatch)
	if perr != nil {
		return nil, perr
	}
	var out project
	_, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		cur, err := visibleProject(ctx, w.Reader, t, req.Project)
		if err != nil {
			return err
		}
		role, err := projectRole(ctx, w.Reader, t, cur)
		if err != nil {
			return err
		}
		if perr := auth.Authorize(principal(ctx), role, edit); perr != nil {
			return perr
		}
		before := map[string]any{fieldName: cur.Name, fieldDescription: cur.Description, fieldWipLimits: wipView(cur.WipLimits)}
		after := map[string]any{fieldName: cur.Name, fieldDescription: cur.Description, fieldWipLimits: wipView(cur.WipLimits)}
		var sent []string
		if req.Body.Name != nil {
			after[fieldName], sent = *req.Body.Name, append(sent, fieldName)
		}
		if req.Body.Description != nil {
			after[fieldDescription], sent = *req.Body.Description, append(sent, fieldDescription)
		}
		if req.Body.WipLimits != nil {
			after[fieldWipLimits], sent = *req.Body.WipLimits, append(sent, fieldWipLimits)
		}
		if cur.Version != version {
			return stale(cur.Version, pick(before, sent))
		}
		changedBefore, changedAfter := diffDeep(before, after)
		if len(changedAfter) == 0 {
			out = cur
			return store.ErrNoChange
		}
		row, err := w.UpdateProject(ctx, writeq.UpdateProjectParams{TenantID: t.ID, ID: cur.ID, Version: version,
			Name: after[fieldName].(string), Description: after[fieldDescription].(string),
			WipLimits: wipJSON(ptrTo(after[fieldWipLimits].(apigen.WipLimits)))})
		if errors.Is(err, pgx.ErrNoRows) {
			return stale(cur.Version, pick(before, sent))
		}
		if err != nil {
			return err
		}
		out = project(row)
		w.Record(store.Event{EntityType: entityProject, EntityID: cur.ID, Action: actionUpdated, Before: changedBefore, After: changedAfter})
		return nil
	})
	if err != nil && !errors.Is(err, store.ErrNoChange) {
		return nil, err
	}
	return apigen.UpdateProject200JSONResponse{Body: projectView(out), Headers: apigen.UpdateProject200ResponseHeaders{ETag: etag(out.Version)}}, nil
}

// ArchiveProject archives a project: an administration act, idempotent
// (docs/adr/0006 D4, docs/adr/0045 D1).
func (s *Server) ArchiveProject(ctx context.Context, req apigen.ArchiveProjectRequestObject) (apigen.ArchiveProjectResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, administer); perr != nil {
		return nil, perr
	}
	var out project
	_, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		cur, err := visibleProject(ctx, w.Reader, t, req.Project)
		if err != nil {
			return err
		}
		if cur.ArchivedAt != nil {
			out = cur
			return store.ErrNoChange
		}
		row, err := w.ArchiveProject(ctx, writeq.ArchiveProjectParams{TenantID: t.ID, ID: cur.ID})
		if errors.Is(err, pgx.ErrNoRows) {
			// A simultaneous archiving came first.
			if out, err = visibleProject(ctx, w.Reader, t, req.Project); err != nil {
				return err
			}
			return store.ErrNoChange
		}
		if err != nil {
			return err
		}
		out = project(row)
		w.Record(store.Event{EntityType: entityProject, EntityID: cur.ID, Action: "archived"})
		return nil
	})
	if err != nil && !errors.Is(err, store.ErrNoChange) {
		return nil, err
	}
	return apigen.ArchiveProject200JSONResponse{Body: projectView(out), Headers: apigen.ArchiveProject200ResponseHeaders{ETag: etag(out.Version)}}, nil
}

// projectRole is the person's role in a project: the tenant role, lowered to
// the entry on a restricted project's list (docs/adr/0034 D3). Tenant
// administrators keep theirs.
func projectRole(ctx context.Context, r *store.Reader, t tenantScope, p project) (domain.Role, error) {
	if !p.Restricted || t.Role == domain.RoleAdmin {
		return t.Role, nil
	}
	entry, err := r.GetProjectAccessRole(ctx, readq.GetProjectAccessRoleParams{TenantID: t.ID, ProjectID: p.ID, UserID: principal(ctx).PersonID})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", problem.New(problem.NotFound, "no such project")
	}
	if err != nil {
		return "", err
	}
	return t.Role.Min(entry), nil
}

func ptrTo[T any](v T) *T { return &v }
