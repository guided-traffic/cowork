package api

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"strings"
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

// read is what every member of the tenant may do with a read token.
var read = auth.Need{Role: domain.RoleViewer, Scope: domain.ScopeRead}

// administer is an administration act: the admin role, admin scope, never an
// agent (docs/adr/0034 D1, docs/adr/0035 D3, docs/adr/0043 D3).
var administer = auth.Need{Role: domain.RoleAdmin, Scope: domain.ScopeAdmin, HardOff: auth.HardOffAdministration}

// GetTenant answers the tenant and its settings.
func (s *Server) GetTenant(ctx context.Context, _ apigen.GetTenantRequestObject) (apigen.GetTenantResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, read); perr != nil {
		return nil, perr
	}
	var row readq.GetTenantRow
	err := s.db.InTenant(ctx, t.ID, func(r *store.Reader) error {
		var err error
		row, err = r.GetTenant(ctx, t.ID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return apigen.GetTenant200JSONResponse{Body: tenantView(row), Headers: apigen.GetTenant200ResponseHeaders{ETag: etag(row.Version)}}, nil
}

func tenantView(t readq.GetTenantRow) apigen.Tenant {
	v := apigen.Tenant{
		Slug:                  t.Slug,
		Name:                  t.Name,
		Version:               int(t.Version),
		TimeVisibleToMembers:  t.TimeVisibleToMembers,
		MembersCreateProjects: t.MembersCreateProjects,
		CreatedAt:             t.CreatedAt,
		UpdatedAt:             t.UpdatedAt,
	}
	if t.TimeLockedUntil != nil {
		v.TimeLockedUntil = nullableOf(&openapi_types.Date{Time: *t.TimeLockedUntil})
	} else {
		v.TimeLockedUntil = nullableOf[openapi_types.Date](nil)
	}
	return v
}

// tenantSettings is the writable part of a tenant.
type tenantSettings struct {
	Name                  string
	TimeVisibleToMembers  bool
	TimeLockedUntil       *time.Time
	MembersCreateProjects bool
}

func settingsOf(t readq.GetTenantRow) tenantSettings {
	return tenantSettings{t.Name, t.TimeVisibleToMembers, t.TimeLockedUntil, t.MembersCreateProjects}
}

func (s tenantSettings) values() map[string]any {
	var lock any
	if s.TimeLockedUntil != nil {
		lock = s.TimeLockedUntil.Format(time.DateOnly)
	}
	return map[string]any{
		fieldName:                 s.Name,
		"time_visible_to_members": s.TimeVisibleToMembers,
		fieldTimeLockedUntil:      lock,
		"members_create_projects": s.MembersCreateProjects,
	}
}

// apply returns the settings with the patch applied and the names of the
// fields the patch sends.
func (s tenantSettings) apply(p apigen.TenantPatch) (tenantSettings, []string) {
	var sent []string
	if p.Name != nil {
		s.Name, sent = *p.Name, append(sent, fieldName)
	}
	if p.TimeVisibleToMembers != nil {
		s.TimeVisibleToMembers, sent = *p.TimeVisibleToMembers, append(sent, "time_visible_to_members")
	}
	if p.TimeLockedUntil.IsSpecified() {
		sent = append(sent, fieldTimeLockedUntil)
		s.TimeLockedUntil = nil
		if !p.TimeLockedUntil.IsNull() {
			d := p.TimeLockedUntil.MustGet().Time
			s.TimeLockedUntil = &d
		}
	}
	if p.MembersCreateProjects != nil {
		s.MembersCreateProjects, sent = *p.MembersCreateProjects, append(sent, "members_create_projects")
	}
	return s, sent
}

// UpdateTenant changes the tenant's name or settings: an administration act
// with If-Match (docs/adr/0050 D3). Moving the time lock is recorded as
// locked (docs/adr/0026 D1), everything else as updated.
func (s *Server) UpdateTenant(ctx context.Context, req apigen.UpdateTenantRequestObject) (apigen.UpdateTenantResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, administer); perr != nil {
		return nil, perr
	}
	version, perr := ifMatch(req.Params.IfMatch)
	if perr != nil {
		return nil, perr
	}
	var out readq.GetTenantRow
	_, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		cur, err := w.GetTenant(ctx, t.ID)
		if err != nil {
			return err
		}
		before := settingsOf(cur)
		after, sent := before.apply(*req.Body)
		if cur.Version != version {
			return stale(cur.Version, pick(before.values(), sent))
		}
		changedBefore, changedAfter := diff(before.values(), after.values())
		if len(changedAfter) == 0 {
			out = cur
			return store.ErrNoChange
		}
		_, err = w.UpdateTenantSettings(ctx, writeq.UpdateTenantSettingsParams{
			TenantID: t.ID, Version: version, Name: after.Name, TimeVisibleToMembers: after.TimeVisibleToMembers,
			TimeLockedUntil: after.TimeLockedUntil, MembersCreateProjects: after.MembersCreateProjects,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return stale(cur.Version, pick(before.values(), sent))
		}
		if err != nil {
			return err
		}
		recordSettingsChange(w, t.ID, changedBefore, changedAfter)
		out, err = w.GetTenant(ctx, t.ID)
		return err
	})
	if err != nil && !errors.Is(err, store.ErrNoChange) {
		return nil, err
	}
	return apigen.UpdateTenant200JSONResponse{Body: tenantView(out), Headers: apigen.UpdateTenant200ResponseHeaders{ETag: etag(out.Version)}}, nil
}

func recordSettingsChange(w *store.Writer, tenantID uuid.UUID, before, after map[string]any) {
	if _, moved := after[fieldTimeLockedUntil]; moved {
		w.Record(store.Event{EntityType: entityTenant, EntityID: tenantID, Action: "locked",
			Before: map[string]any{fieldTimeLockedUntil: before[fieldTimeLockedUntil]},
			After:  map[string]any{fieldTimeLockedUntil: after[fieldTimeLockedUntil]}})
		delete(before, fieldTimeLockedUntil)
		delete(after, fieldTimeLockedUntil)
	}
	if len(after) > 0 {
		w.Record(store.Event{EntityType: entityTenant, EntityID: tenantID, Action: actionUpdated, Before: before, After: after})
	}
}

// diffDeep is diff for values that do not compare with ==, such as structs
// holding pointers: they compare by their JSON.
func diffDeep(before, after map[string]any) (map[string]any, map[string]any) {
	b, a := map[string]any{}, map[string]any{}
	for k, v := range after {
		bj, _ := json.Marshal(before[k])
		aj, _ := json.Marshal(v)
		if string(bj) != string(aj) {
			b[k], a[k] = before[k], v
		}
	}
	return b, a
}

// diff returns the fields whose values differ, before and after.
func diff(before, after map[string]any) (map[string]any, map[string]any) {
	b, a := map[string]any{}, map[string]any{}
	for k, v := range after {
		if before[k] != v {
			b[k], a[k] = before[k], v
		}
	}
	return b, a
}

func pick(values map[string]any, keys []string) map[string]any {
	out := map[string]any{}
	for _, k := range keys {
		out[k] = values[k]
	}
	return out
}

// ListMembers lists the tenant's members with their roles and where each comes
// from (docs/adr/0034 D7, docs/adr/0030 D4).
func (s *Server) ListMembers(ctx context.Context, req apigen.ListMembersRequestObject) (apigen.ListMembersResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, read); perr != nil {
		return nil, perr
	}
	const op = "listMembers"
	scope := t.ID.String()
	after, perr := s.uuidCursor(op, scope, req.Params.Cursor)
	if perr != nil {
		return nil, perr
	}
	size := s.h.pageSize(req.Params.Limit)
	var rows []readq.ListMembersRow
	err := s.db.InTenant(ctx, t.ID, func(r *store.Reader) error {
		var err error
		rows, err = r.ListMembers(ctx, readq.ListMembersParams{TenantID: t.ID, After: after, PageSize: limitArg(size)})
		return err
	})
	if err != nil {
		return nil, err
	}
	rows, next := page(s.h, rows, size, op, scope, func(m readq.ListMembersRow) string { return m.ID.String() })
	out := apigen.ListMembers200JSONResponse{Items: []apigen.Member{}, NextCursor: nullableString(next)}
	admin := t.Role == domain.RoleAdmin
	for _, m := range rows {
		out.Items = append(out.Items, memberView(m.ID, m.Username, m.DisplayName, m.Email, admin, m.Role, m.Sources, m.Roles, m.Local))
	}
	return out, nil
}

// uuidCursor decodes the cursor of a list ordered by id.
func (s *Server) uuidCursor(op, scope string, cursor *string) (*uuid.UUID, *problem.Error) {
	if cursor == nil {
		return nil, nil
	}
	v, perr := s.cursors.decode(op, scope, *cursor)
	if perr != nil {
		return nil, perr
	}
	id, err := uuid.Parse(v)
	if err != nil {
		return nil, problem.New(problem.InvalidCursor, "")
	}
	return &id, nil
}

// ListAudit lists the tenant's audit record for its administrators, newest
// first, as JSON or CSV (docs/adr/0026 D6).
func (s *Server) ListAudit(ctx context.Context, req apigen.ListAuditRequestObject) (apigen.ListAuditResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, auth.Need{Role: domain.RoleAdmin, Scope: domain.ScopeRead}); perr != nil {
		return nil, perr
	}
	const op = "listAudit"
	scope := t.ID.String()
	before, perr := s.uuidCursor(op, scope, req.Params.Cursor)
	if perr != nil {
		return nil, perr
	}
	size := s.h.pageSize(req.Params.Limit)
	params := readq.ListAuditForTenantParams{
		TenantID: &t.ID, Actor: req.Params.Actor, Token: req.Params.Token, EntityType: req.Params.EntityType,
		FromTime: req.Params.From, ToTime: req.Params.To, Before: before, PageSize: limitArg(size),
		Actions: []string{},
	}
	if req.Params.Action != nil {
		for _, a := range *req.Params.Action {
			params.Actions = append(params.Actions, string(a))
		}
	}
	var rows []readq.ListAuditForTenantRow
	err := s.db.InTenant(ctx, t.ID, func(r *store.Reader) error {
		var err error
		rows, err = r.ListAuditForTenant(ctx, params)
		return err
	})
	if err != nil {
		return nil, err
	}
	rows, next := page(s.h, rows, size, op, scope, func(a readq.ListAuditForTenantRow) string { return a.ID.String() })
	if wantsCSV(ctx) {
		body := auditCSV(rows)
		return apigen.ListAudit200TextcsvResponse{Body: bytes.NewReader(body), ContentLength: int64(len(body))}, nil
	}
	out := apigen.ListAudit200JSONResponse{Items: []apigen.AuditEvent{}, NextCursor: nullableString(next)}
	for _, a := range rows {
		out.Items = append(out.Items, auditView(a))
	}
	return out, nil
}

func auditView(a readq.ListAuditForTenantRow) apigen.AuditEvent {
	v := apigen.AuditEvent{
		Id: a.ID, CreatedAt: a.CreatedAt, EntityType: a.EntityType, Action: apigen.AuditAction(a.Action),
		Agent: nullableOf(a.Agent), TokenId: nullableOf(a.TokenID), EntityId: nullableOf(a.EntityID),
		TicketKey: nullableOf(a.TicketKey), Reason: nullableOf(a.Reason), Note: nullableOf(a.Note),
		RequestId: nullableOf(a.RequestID), IdempotencyKey: nullableOf(a.IdempotencyKey),
	}
	if a.AgentCapabilities != nil {
		v.AgentCapabilities = nullableOf(&a.AgentCapabilities)
	} else {
		v.AgentCapabilities = nullableOf[[]string](nil)
	}
	v.Actor.System = nullableOf(a.ActorSystem)
	v.Actor.Person = nullableOf[apigen.Person](nil)
	if a.ActorUserID != nil {
		person := apigen.Person{Id: *a.ActorUserID, Username: nullableOf(a.ActorUsername)}
		if a.ActorDisplayName != nil {
			person.DisplayName = *a.ActorDisplayName
		}
		v.Actor.Person = nullableOf(&person)
	}
	if a.Before != nil {
		v.Before = jsonValue(a.Before)
	}
	if a.After != nil {
		v.After = jsonValue(a.After)
	}
	return v
}

// auditCSV renders audit rows as CSV; a cell that a spreadsheet would read
// as a formula is prefixed with an apostrophe.
func auditCSV(rows []readq.ListAuditForTenantRow) []byte {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{"id", "created_at", "actor_user_id", "actor_system", "agent", "token_id", "entity_type",
		"entity_id", "ticket_key", "action", "reason", fieldNote, "request_id", "before", "after"})
	for _, a := range rows {
		_ = w.Write(neutralise([]string{
			a.ID.String(), a.CreatedAt.UTC().Format(time.RFC3339Nano), uuidString(a.ActorUserID), deref(a.ActorSystem),
			deref(a.Agent), uuidString(a.TokenID), a.EntityType, uuidString(a.EntityID), deref(a.TicketKey), a.Action,
			deref(a.Reason), deref(a.Note), uuidString(a.RequestID), string(a.Before), string(a.After),
		}))
	}
	w.Flush()
	return buf.Bytes()
}

func neutralise(cells []string) []string {
	for i, c := range cells {
		if c != "" && strings.ContainsRune("=+-@\t\r", rune(c[0])) {
			cells[i] = "'" + c
		}
	}
	return cells
}

// deref returns what p points at, or the zero value.
func deref[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}

func uuidString(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

// CreateTenant creates a tenant and makes the global administrator who asks its
// first administrator, by a marked grant, in the same transaction and recorded
// with it (docs/adr/0005 D5, docs/adr/0032 D7): a tenant without an
// administrator cannot come to exist. The pipeline has refused a token; the
// person must be a global administrator, who has no other role anywhere
// (docs/adr/0034 D2). Both acts are installation-level rows, as ADR 0026 D1
// says of a tenant's creation.
func (s *Server) CreateTenant(ctx context.Context, req apigen.CreateTenantRequestObject) (apigen.CreateTenantResponseObject, error) {
	p := principal(ctx)
	if !p.GlobalAdmin {
		return nil, problem.New(problem.Forbidden, "creating a tenant needs a global administrator")
	}
	body := *req.Body
	name := strings.TrimSpace(body.Name)
	if name == "" {
		return nil, problem.Field("/name", "must not be blank")
	}
	ctx, perr := s.keyed(ctx, req.Params.IdempotencyKey, "createTenant", p.PersonID.String(), body)
	if perr != nil {
		return nil, perr
	}
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	grantID, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	var created readq.GetTenantRow
	replay, err := s.db.Mutate(ctx, uuid.Nil, func(w *store.Writer) error {
		if err := w.InsertTenant(ctx, writeq.InsertTenantParams{ID: id, Slug: body.Slug, Name: name}); err != nil {
			if isUnique(err, "tenants_slug_key") {
				return &problem.Error{Code: problem.TenantSlugTaken, Detail: "the installation has a tenant with this slug",
					Errors: []problem.FieldError{{Pointer: "/slug", Message: messageTaken}}}
			}
			return err
		}
		if err := w.InsertGrant(ctx, writeq.InsertGrantParams{ID: grantID, TenantID: id, UserID: p.PersonID, Role: domain.RoleAdmin}); err != nil {
			return err
		}
		w.Record(store.Event{EntityType: entityTenant, EntityID: id, Action: actionCreated,
			After: map[string]any{"slug": body.Slug, fieldName: name}})
		w.Record(store.Event{EntityType: entityMembership, EntityID: grantID, Action: actionCreated,
			After: map[string]any{"tenant": body.Slug, "user": p.PersonID, "role": domain.RoleAdmin, fieldSource: sourceGrant}})
		var err error
		if created, err = w.GetTenant(ctx, id); err != nil {
			return err
		}
		res, err := stored(tenantView(created), map[string]string{headerETag: *etag(created.Version), headerLocation: tenantURL(created.Slug)})
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
		replayedView, err := replayed[apigen.Tenant](replay)
		if err != nil {
			return nil, err
		}
		return apigen.CreateTenant201JSONResponse{Body: replayedView, Headers: apigen.CreateTenant201ResponseHeaders{
			ETag: header(replay, headerETag), Location: header(replay, headerLocation)}}, nil
	}
	location := tenantURL(created.Slug)
	return apigen.CreateTenant201JSONResponse{Body: tenantView(created), Headers: apigen.CreateTenant201ResponseHeaders{
		ETag: etag(created.Version), Location: &location}}, nil
}

func tenantURL(slug string) string { return "/api/v1/tenants/" + slug }
