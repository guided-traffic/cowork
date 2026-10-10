package api

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/oapi-codegen/nullable"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/problem"
	"github.com/guided-traffic/cowork/backend/internal/store"
	"github.com/guided-traffic/cowork/backend/internal/store/readq"
	"github.com/guided-traffic/cowork/backend/internal/store/writeq"
)

// The entity types of the administration's acts besides the membership's.
const (
	entityGroupMapping  = "group_mapping"
	entityProjectAccess = "project_access"
	fieldRole           = "role"
	fieldUser           = "user"
	fieldGroup          = "group"
	fieldRestricted     = "restricted"
	// fieldProject keys the project in the audit rows of an access entry and of
	// a repository binding.
	fieldProject = "project"
)

// adminRead is an administrator's read: the admin role and a token's read
// scope, as the audit record has it (docs/adr/0034 D1, docs/adr/0035 D3).
var adminRead = auth.Need{Role: domain.RoleAdmin, Scope: domain.ScopeRead}

// lastAdmin refuses a change that leaves the tenant without an administrator
// who can log in, mapped or granted (docs/adr/0034 D1; the security review of
// 2026-10-04, m5): the change of an administrator's own grant or of a mapping
// included, and the deactivation of an account the tenant manages
// (DeactivateAccount). A derivation at a login or a refresh is the identity provider's
// truth and is never refused. The caller holds the tenant's lock (m4), so two
// changes cannot each leave the other's administrator as the last one.
func (s *Server) lastAdmin(ctx context.Context, w *store.Writer, tenantID uuid.UUID) error {
	ok, err := w.TenantHasAdmin(ctx, readq.TenantHasAdminParams{TenantID: tenantID, Issuer: s.h.issuer()})
	if err != nil {
		return err
	}
	if !ok {
		return problem.New(problem.LastAdmin, "the change would leave the team without an administrator")
	}
	return nil
}

func memberChange(person uuid.UUID) *store.MembershipChange {
	return &store.MembershipChange{Person: person, Audience: store.AudienceMembers}
}

// memberView is a member as the list shows them: the effective role, every
// source with its own role, the mapping before the grant, whether the person is
// a local account (docs/adr/0030 D4, docs/adr/0034 D7), and — for the
// tenant's administrators only — the address that tells two persons of one
// name apart (the security review of 2026-10-04, item 13).
func memberView(id uuid.UUID, username *string, displayName string, email *string, admin bool, role domain.Role,
	sources, roles []string, local bool) apigen.Member {
	return apigen.Member{
		Person:  apigen.Person{Id: id, Username: nullableOf(username), DisplayName: displayName},
		Email:   addressFor(admin, email),
		Role:    apigen.Role(role),
		Origins: originsOf(sources, roles),
		Local:   local,
	}
}

// addressFor is a person's address as the caller may see it: the tenant's
// administrators do, everyone else sees null.
func addressFor(admin bool, email *string) nullable.Nullable[string] {
	if !admin {
		return nullableOf[string](nil)
	}
	return nullableOf(email)
}

func originsOf(sources, roles []string) []apigen.MembershipOrigin {
	out := make([]apigen.MembershipOrigin, 0, len(sources))
	for i, s := range sources {
		if i < len(roles) {
			out = append(out, apigen.MembershipOrigin{Source: apigen.MembershipSource(s), Role: apigen.Role(roles[i])})
		}
	}
	return out
}

// readMember reads one member for an administrator's answer.
func readMember(ctx context.Context, r *store.Reader, tenantID, person uuid.UUID) (apigen.Member, error) {
	m, err := r.GetMember(ctx, readq.GetMemberParams{TenantID: tenantID, UserID: person})
	if err != nil {
		return apigen.Member{}, err
	}
	return memberView(m.ID, m.Username, m.DisplayName, m.Email, true, m.Role, m.Sources, m.Roles, m.Local), nil
}

func personNotFound(detail string) *problem.Error {
	return &problem.Error{Code: problem.PersonNotFound, Detail: detail}
}

// AddMember grants a role to a person who exists, looked up by e-mail address
// or username (docs/adr/0030 D3): a marked grant beside a mapped membership,
// if any, whose higher role applies (D4). A browser session only: a grant made
// with a leaked token would outlive the token's revocation
// (docs/adr/0035 D5).
func (s *Server) AddMember(ctx context.Context, req apigen.AddMemberRequestObject) (apigen.AddMemberResponseObject, error) {
	t, p := tenantFrom(ctx), principal(ctx)
	if perr := auth.Authorize(p, t.Role, administer); perr != nil {
		return nil, perr
	}
	body := *req.Body
	person, err := s.findPerson(ctx, t, body.Person)
	if err != nil {
		return nil, err
	}
	ctx, perr := s.keyed(ctx, req.Params.IdempotencyKey, "addMember", t.ID.String(), body)
	if perr != nil {
		return nil, perr
	}
	grantID, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	var view apigen.Member
	replay, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		var err error
		if view, err = insertGrant(ctx, w, t, grantID, person, domain.Role(body.Role)); err != nil {
			return err
		}
		res, err := stored(view, nil)
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
		if view, err = replayed[apigen.Member](replay); err != nil {
			return nil, err
		}
	}
	return apigen.AddMember201JSONResponse(view), nil
}

// findPerson looks up the person an administrator grants a role to, by
// e-mail address — one the issuer marked verified, or, with
// COWORK_OIDC_EMAIL_TRUSTED, one it said nothing about — or username
// (docs/adr/0030 D3). A username is taken with or
// without the local: that names the identity of a local account
// (docs/adr/0032 D1), and normalised as the local login normalises it.
func (s *Server) findPerson(ctx context.Context, t tenantScope, given string) (uuid.UUID, error) {
	key := strings.TrimSpace(given)
	if key == "" {
		return uuid.Nil, problem.Field("/person", "must not be blank")
	}
	if rest, prefixed := strings.CutPrefix(key, "local:"); prefixed || !strings.Contains(key, "@") {
		if key = auth.NormaliseUsername(rest); key == "" {
			return uuid.Nil, personNotFound("no active person has this e-mail address or username")
		}
	}
	person, match, err := s.db.FindPerson(ctx, store.PersonLookup{TenantID: t.ID, Key: key, Issuer: s.h.issuer(),
		EmailTrusted: s.h.opts.OIDC.EmailTrusted})
	switch {
	case err != nil:
		return uuid.Nil, err
	case match == store.PersonNone:
		return uuid.Nil, personNotFound("no active person has this e-mail address or username")
	case match == store.PersonAmbiguous:
		return uuid.Nil, problem.New(problem.PersonAmbiguous, "several persons have this e-mail address")
	}
	return person, nil
}

// insertGrant makes a person's marked grant in the tenant and records it, or
// answers that they hold one.
func insertGrant(ctx context.Context, w *store.Writer, t tenantScope, id, person uuid.UUID, role domain.Role) (apigen.Member, error) {
	if _, err := w.GetGrant(ctx, readq.GetGrantParams{TenantID: t.ID, UserID: person}); err == nil {
		return apigen.Member{}, grantExists()
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return apigen.Member{}, err
	}
	if err := w.InsertGrant(ctx, writeq.InsertGrantParams{ID: id, TenantID: t.ID, UserID: person, Role: role}); err != nil {
		if isUnique(err, "memberships_tenant_id_user_id_source_key") {
			return apigen.Member{}, grantExists()
		}
		return apigen.Member{}, err
	}
	w.Record(store.Event{EntityType: entityMembership, EntityID: id, Action: actionCreated,
		After: map[string]any{fieldUser: person, fieldRole: role, fieldSource: sourceGrant}, Membership: memberChange(person)})
	return readMember(ctx, w.Reader, t.ID, person)
}

func grantExists() *problem.Error {
	return problem.New(problem.GrantExists, "the person holds a grant in this team already; change it with PUT …/grant")
}

// SetMemberGrant creates a member's grant or changes its role
// (docs/adr/0030 D3); the mapped membership is never touched. Repeating it
// changes nothing (docs/adr/0045 D1). A global administrator who does not hold
// admin in the tenant sets their own grant here (grantSelf), and nobody else's.
func (s *Server) SetMemberGrant(ctx context.Context, req apigen.SetMemberGrantRequestObject) (apigen.SetMemberGrantResponseObject, error) {
	t, p := tenantFrom(ctx), principal(ctx)
	person, role := req.PersonId, domain.Role(req.Body.Role)
	if ownGrant(p, t, person) {
		return s.grantSelf(ctx, t, person, role)
	}
	if perr := auth.Authorize(p, t.Role, administer); perr != nil {
		return nil, perr
	}
	var view apigen.Member
	_, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		var err error
		view, err = s.setGrant(ctx, w, t, person, role)
		return err
	})
	if err != nil && !errors.Is(err, store.ErrNoChange) {
		return nil, err
	}
	return apigen.SetMemberGrant200JSONResponse(view), nil
}

// setGrant creates a member's grant or changes its role under the tenant's
// lock, and answers the member as the grant leaves them — with
// store.ErrNoChange where they hold the role already.
func (s *Server) setGrant(ctx context.Context, w *store.Writer, t tenantScope, person uuid.UUID, role domain.Role) (apigen.Member, error) {
	if err := w.LockTenant(ctx); err != nil {
		return apigen.Member{}, err
	}
	member, err := w.IsMember(ctx, readq.IsMemberParams{TenantID: t.ID, UserID: person})
	if err != nil {
		return apigen.Member{}, err
	}
	if !member {
		return apigen.Member{}, personNotFound("the person is not a member of the team; add them by e-mail address or username")
	}
	cur, err := w.GetGrant(ctx, readq.GetGrantParams{TenantID: t.ID, UserID: person})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		id, err := uuid.NewV7()
		if err != nil {
			return apigen.Member{}, err
		}
		if err := w.InsertGrant(ctx, writeq.InsertGrantParams{ID: id, TenantID: t.ID, UserID: person, Role: role}); err != nil {
			return apigen.Member{}, err
		}
		w.Record(store.Event{EntityType: entityMembership, EntityID: id, Action: actionCreated,
			After: map[string]any{fieldUser: person, fieldRole: role, fieldSource: sourceGrant}, Membership: memberChange(person)})
	case err != nil:
		return apigen.Member{}, err
	case cur.Role == role:
		view, err := readMember(ctx, w.Reader, t.ID, person)
		if err != nil {
			return apigen.Member{}, err
		}
		return view, store.ErrNoChange
	default:
		if err := w.SetGrantRole(ctx, writeq.SetGrantRoleParams{Role: role, TenantID: t.ID, UserID: person}); err != nil {
			return apigen.Member{}, err
		}
		w.Record(store.Event{EntityType: entityMembership, EntityID: cur.ID, Action: actionUpdated,
			Before: map[string]any{fieldRole: cur.Role}, After: map[string]any{fieldRole: role}, Membership: memberChange(person)})
	}
	if err := s.lastAdmin(ctx, w, t.ID); err != nil {
		return apigen.Member{}, err
	}
	return readMember(ctx, w.Reader, t.ID, person)
}

// ownGrant says whether a request sets its own person's grant as a global
// administrator who does not hold admin in the tenant (docs/adr/0034 D2): one
// the boundary admitted without a role, or one who holds a lower role. The
// document takes a session for setMemberGrant and the pipeline refuses an
// agent's request; this holds the path to both again.
func ownGrant(p auth.Principal, t tenantScope, person uuid.UUID) bool {
	return person == p.PersonID && p.GlobalAdmin && p.Session && !p.IsAgent() && t.Role != domain.RoleAdmin
}

// grantSelf is a global administrator's grant of a role to themselves in a
// tenant in which they do not hold admin (docs/adr/0034 D2): a marked grant
// like any other, made — or its role changed — under the tenant's lock,
// recorded in the tenant with the administrator as its actor and announced to
// its members. It takes no administrator away, so it meets no last_admin; it is
// how a tenant left without an administrator who can log in gets one again. The
// same role is no change (docs/adr/0045 D1).
func (s *Server) grantSelf(ctx context.Context, t tenantScope, person uuid.UUID, role domain.Role) (apigen.SetMemberGrantResponseObject, error) {
	var view apigen.Member
	_, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		if err := w.LockTenant(ctx); err != nil {
			return err
		}
		var err error
		view, err = s.setOwnGrant(ctx, w, t, person, role)
		return err
	})
	if err != nil && !errors.Is(err, store.ErrNoChange) {
		return nil, err
	}
	return apigen.SetMemberGrant200JSONResponse(view), nil
}

// setOwnGrant makes the global administrator's grant or changes its role, with
// the tenant's lock held. A grant of admin that another administrator gave them
// after the boundary read their role is lowered only as any administrator's
// own grant is: held to last_admin.
func (s *Server) setOwnGrant(ctx context.Context, w *store.Writer, t tenantScope, person uuid.UUID, role domain.Role) (apigen.Member, error) {
	cur, err := w.GetGrant(ctx, readq.GetGrantParams{TenantID: t.ID, UserID: person})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		id, err := uuid.NewV7()
		if err != nil {
			return apigen.Member{}, err
		}
		return insertGrant(ctx, w, t, id, person, role)
	case err != nil:
		return apigen.Member{}, err
	case cur.Role == role:
		view, err := readMember(ctx, w.Reader, t.ID, person)
		if err != nil {
			return apigen.Member{}, err
		}
		return view, store.ErrNoChange
	}
	if err := w.SetGrantRole(ctx, writeq.SetGrantRoleParams{Role: role, TenantID: t.ID, UserID: person}); err != nil {
		return apigen.Member{}, err
	}
	w.Record(store.Event{EntityType: entityMembership, EntityID: cur.ID, Action: actionUpdated,
		Before: map[string]any{fieldRole: cur.Role}, After: map[string]any{fieldRole: role}, Membership: memberChange(person)})
	if cur.Role == domain.RoleAdmin {
		if err := s.lastAdmin(ctx, w, t.ID); err != nil {
			return apigen.Member{}, err
		}
	}
	return readMember(ctx, w.Reader, t.ID, person)
}

// RemoveMemberGrant removes a member's grant; the mapped membership, if any,
// stays (docs/adr/0030 D3). An administrator's token may: it only takes access
// away.
func (s *Server) RemoveMemberGrant(ctx context.Context, req apigen.RemoveMemberGrantRequestObject) (apigen.RemoveMemberGrantResponseObject, error) {
	t, p := tenantFrom(ctx), principal(ctx)
	if perr := auth.Authorize(p, t.Role, administer); perr != nil {
		return nil, perr
	}
	person := req.PersonId
	_, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		if err := w.LockTenant(ctx); err != nil {
			return err
		}
		row, err := w.DeleteGrant(ctx, writeq.DeleteGrantParams{TenantID: t.ID, UserID: person})
		if errors.Is(err, pgx.ErrNoRows) {
			return store.ErrNoChange
		}
		if err != nil {
			return err
		}
		w.Record(store.Event{EntityType: entityMembership, EntityID: row.ID, Action: actionDeleted,
			Before: map[string]any{fieldUser: person, fieldRole: row.Role, fieldSource: sourceGrant}, Membership: memberChange(person)})
		return s.lastAdmin(ctx, w, t.ID)
	})
	if err != nil && !errors.Is(err, store.ErrNoChange) {
		return nil, err
	}
	return apigen.RemoveMemberGrant204Response{}, nil
}

// groupMappingView is a mapping, and whether the calling person's groups — as
// of their last login or refresh — hold its group.
func groupMappingView(id uuid.UUID, group string, role domain.Role, version int32, created, updated time.Time, mine []string) apigen.GroupMapping {
	includes := slices.Contains(mine, group)
	return apigen.GroupMapping{Id: id, Group: group, Role: apigen.Role(role), Version: int(version),
		CreatedAt: created, UpdatedAt: updated, IncludesCaller: includes}
}

// callerGroups reads the calling person's groups snapshot, which they may read
// of themselves.
func callerGroups(ctx context.Context, r *store.Reader) ([]string, error) {
	u, err := r.GetUser(ctx, principal(ctx).PersonID)
	if err != nil {
		return nil, err
	}
	return u.OidcGroups, nil
}

// ListGroupMappings lists the tenant's group mappings by group
// (docs/adr/0030 D2, D7), to its administrators and to a global administrator
// who oversees it (docs/adr/0034 D2).
func (s *Server) ListGroupMappings(ctx context.Context, req apigen.ListGroupMappingsRequestObject) (apigen.ListGroupMappingsResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := administrationRead(principal(ctx), t, adminRead); perr != nil {
		return nil, perr
	}
	const op = "listGroupMappings"
	scope := t.ID.String()
	params := readq.ListGroupMappingsParams{TenantID: t.ID, PageSize: limitArg(s.h.pageSize(req.Params.Limit))}
	if req.Params.Cursor != nil {
		after, perr := s.cursors.decode(op, scope, *req.Params.Cursor)
		if perr != nil {
			return nil, perr
		}
		params.After = &after
	}
	var (
		rows []readq.ListGroupMappingsRow
		mine []string
	)
	err := s.db.InTenant(ctx, t.ID, func(r *store.Reader) error {
		var err error
		if rows, err = r.ListGroupMappings(ctx, params); err != nil {
			return err
		}
		mine, err = callerGroups(ctx, r)
		return err
	})
	if err != nil {
		return nil, err
	}
	rows, next := page(s.h, rows, s.h.pageSize(req.Params.Limit), op, scope, func(m readq.ListGroupMappingsRow) string { return m.GroupName })
	out := apigen.GroupMappingList{Items: []apigen.GroupMapping{}, NextCursor: nullableString(next)}
	for _, m := range rows {
		out.Items = append(out.Items, groupMappingView(m.ID, m.GroupName, m.Role, m.Version, m.CreatedAt, m.UpdatedAt, mine))
	}
	tag, unchanged := listTag(req.Params.IfNoneMatch, out)
	if unchanged {
		return apigen.ListGroupMappings304Response{Headers: apigen.NotModifiedResponseHeaders{ETag: &tag}}, nil
	}
	return apigen.ListGroupMappings200JSONResponse{Body: out, Headers: apigen.ListGroupMappings200ResponseHeaders{ETag: &tag}}, nil
}

// readGroupMapping reads one mapping of the tenant for an answer, and its
// version for the ETag.
func readGroupMapping(ctx context.Context, r *store.Reader, tenantID, id uuid.UUID) (apigen.GroupMapping, int32, error) {
	m, err := r.GetGroupMapping(ctx, readq.GetGroupMappingParams{TenantID: tenantID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return apigen.GroupMapping{}, 0, problem.New(problem.NotFound, "no such group mapping")
	}
	if err != nil {
		return apigen.GroupMapping{}, 0, err
	}
	mine, err := callerGroups(ctx, r)
	if err != nil {
		return apigen.GroupMapping{}, 0, err
	}
	return groupMappingView(m.ID, m.GroupName, m.Role, m.Version, m.CreatedAt, m.UpdatedAt, mine), m.Version, nil
}

func mappingChange(id uuid.UUID) *store.MembershipChange {
	return &store.MembershipChange{Mapping: id, Audience: store.AudienceAdmins}
}

func mappingURL(t tenantScope, id uuid.UUID) string {
	return teamFamily + "/" + t.Slug + "/group-mappings/" + id.String()
}

// mapsGroups authorizes the making of a mapping and the change of its role:
// an administrator of the tenant, who must be a global administrator as well.
// Every tenant shares the identity provider's one namespace of groups, and a
// mapping admits everyone in its group at once (docs/adr/0030 D7). Removing
// one only takes access away and stays with the tenant's administrators.
func mapsGroups(p auth.Principal, role domain.Role, act string) *problem.Error {
	if perr := auth.Authorize(p, role, administer); perr != nil {
		return perr
	}
	if !p.GlobalAdmin {
		return problem.New(problem.Forbidden, act+" needs a global administrator who administers the team")
	}
	return nil
}

// CreateGroupMapping maps a group to a role in the tenant (docs/adr/0030 D2,
// D7) and derives at once the memberships of every person whose groups hold
// it. A browser session of a global administrator who administers the tenant
// only (docs/adr/0035 D5, docs/adr/0030 D7).
func (s *Server) CreateGroupMapping(ctx context.Context, req apigen.CreateGroupMappingRequestObject) (apigen.CreateGroupMappingResponseObject, error) {
	t, p := tenantFrom(ctx), principal(ctx)
	if perr := mapsGroups(p, t.Role, "mapping a group"); perr != nil {
		return nil, perr
	}
	body := *req.Body
	ctx, perr := s.keyed(ctx, req.Params.IdempotencyKey, "createGroupMapping", t.ID.String(), body)
	if perr != nil {
		return nil, perr
	}
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	role := domain.Role(body.Role)
	var (
		view    apigen.GroupMapping
		version int32
	)
	replay, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		if err := w.LockTenant(ctx); err != nil {
			return err
		}
		taken, err := w.GroupMappingExists(ctx, readq.GroupMappingExistsParams{TenantID: t.ID, GroupName: body.Group})
		if err != nil {
			return err
		}
		if taken {
			return mappingExists()
		}
		if err := w.InsertGroupMapping(ctx, writeq.InsertGroupMappingParams{ID: id, TenantID: t.ID, GroupName: body.Group,
			Role: role, CreatedBy: &p.PersonID}); err != nil {
			if isUnique(err, "group_mappings_tenant_id_group_name_key") {
				return mappingExists()
			}
			return err
		}
		w.Record(store.Event{EntityType: entityGroupMapping, EntityID: id, Action: actionCreated,
			After: map[string]any{fieldGroup: body.Group, fieldRole: role}, Membership: mappingChange(id)})
		if err := w.RederiveGroup(ctx, body.Group, s.h.issuer()); err != nil {
			return err
		}
		if view, version, err = readGroupMapping(ctx, w.Reader, t.ID, id); err != nil {
			return err
		}
		res, err := stored(view, map[string]string{headerETag: *etag(version), headerLocation: mappingURL(t, id)})
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
		if view, err = replayed[apigen.GroupMapping](replay); err != nil {
			return nil, err
		}
		return apigen.CreateGroupMapping201JSONResponse{Body: view, Headers: apigen.CreateGroupMapping201ResponseHeaders{
			ETag: header(replay, headerETag), Location: header(replay, headerLocation)}}, nil
	}
	location := mappingURL(t, view.Id)
	return apigen.CreateGroupMapping201JSONResponse{Body: view, Headers: apigen.CreateGroupMapping201ResponseHeaders{
		ETag: etag(version), Location: &location}}, nil
}

func mappingExists() *problem.Error {
	return &problem.Error{Code: problem.MappingExists, Detail: "the team maps this group already",
		Errors: []problem.FieldError{{Pointer: "/group", Message: messageTaken}}}
}

// UpdateGroupMapping changes a mapping's role, with If-Match
// (docs/adr/0050 D3), and re-derives the memberships it gives at once. Like
// its making, a global administrator's who administers the tenant
// (docs/adr/0030 D7).
func (s *Server) UpdateGroupMapping(ctx context.Context, req apigen.UpdateGroupMappingRequestObject) (apigen.UpdateGroupMappingResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := mapsGroups(principal(ctx), t.Role, "changing a group mapping"); perr != nil {
		return nil, perr
	}
	version, perr := ifMatch(req.Params.IfMatch)
	if perr != nil {
		return nil, perr
	}
	role := domain.Role(req.Body.Role)
	var (
		view    apigen.GroupMapping
		current int32
	)
	_, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		if err := w.LockTenant(ctx); err != nil {
			return err
		}
		cur, err := w.GetGroupMapping(ctx, readq.GetGroupMappingParams{TenantID: t.ID, ID: req.MappingId})
		if errors.Is(err, pgx.ErrNoRows) {
			return problem.New(problem.NotFound, "no such group mapping")
		}
		if err != nil {
			return err
		}
		if cur.Version != version {
			return stale(cur.Version, map[string]any{fieldRole: cur.Role})
		}
		if cur.Role == role {
			if view, current, err = readGroupMapping(ctx, w.Reader, t.ID, cur.ID); err != nil {
				return err
			}
			return store.ErrNoChange
		}
		if _, err := w.UpdateGroupMappingRole(ctx, writeq.UpdateGroupMappingRoleParams{Role: role, TenantID: t.ID, ID: cur.ID,
			Version: version}); errors.Is(err, pgx.ErrNoRows) {
			return stale(cur.Version, map[string]any{fieldRole: cur.Role})
		} else if err != nil {
			return err
		}
		w.Record(store.Event{EntityType: entityGroupMapping, EntityID: cur.ID, Action: actionUpdated,
			Before: map[string]any{fieldRole: cur.Role}, After: map[string]any{fieldRole: role}, Membership: mappingChange(cur.ID)})
		if err := w.RederiveGroup(ctx, cur.GroupName, s.h.issuer()); err != nil {
			return err
		}
		if err := s.lastAdmin(ctx, w, t.ID); err != nil {
			return err
		}
		view, current, err = readGroupMapping(ctx, w.Reader, t.ID, cur.ID)
		return err
	})
	if err != nil && !errors.Is(err, store.ErrNoChange) {
		return nil, err
	}
	return apigen.UpdateGroupMapping200JSONResponse{Body: view, Headers: apigen.UpdateGroupMapping200ResponseHeaders{
		ETag: etag(current)}}, nil
}

// DeleteGroupMapping removes a mapping; the memberships it derived go at once,
// or fall to the person's other mapped groups, and grants stay
// (docs/adr/0030 D3). Any administrator of the tenant may, a token of one too:
// it only takes access away (docs/adr/0030 D7).
func (s *Server) DeleteGroupMapping(ctx context.Context, req apigen.DeleteGroupMappingRequestObject) (apigen.DeleteGroupMappingResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, administer); perr != nil {
		return nil, perr
	}
	_, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		if err := w.LockTenant(ctx); err != nil {
			return err
		}
		row, err := w.DeleteGroupMapping(ctx, writeq.DeleteGroupMappingParams{TenantID: t.ID, ID: req.MappingId})
		if errors.Is(err, pgx.ErrNoRows) {
			return store.ErrNoChange
		}
		if err != nil {
			return err
		}
		w.Record(store.Event{EntityType: entityGroupMapping, EntityID: req.MappingId, Action: actionDeleted,
			Before: map[string]any{fieldGroup: row.GroupName, fieldRole: row.Role}, Membership: mappingChange(req.MappingId)})
		if err := w.RederiveGroup(ctx, row.GroupName, s.h.issuer()); err != nil {
			return err
		}
		return s.lastAdmin(ctx, w, t.ID)
	})
	if err != nil && !errors.Is(err, store.ErrNoChange) {
		return nil, err
	}
	return apigen.DeleteGroupMapping204Response{}, nil
}

// SetProjectRestriction restricts a project to its access list or opens it
// (docs/adr/0034 D3), with If-Match: the restriction is a project setting
// (docs/adr/0050 D3). A browser session only: opening a project with a leaked
// token would outlive the token's revocation (docs/adr/0035 D5).
func (s *Server) SetProjectRestriction(ctx context.Context, req apigen.SetProjectRestrictionRequestObject) (apigen.SetProjectRestrictionResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, administer); perr != nil {
		return nil, perr
	}
	version, perr := ifMatch(req.Params.IfMatch)
	if perr != nil {
		return nil, perr
	}
	want := req.Body.Restricted
	var out project
	_, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		cur, err := visibleProject(ctx, w.Reader, t, req.Project)
		if err != nil {
			return err
		}
		if cur.Version != version {
			return stale(cur.Version, map[string]any{fieldRestricted: cur.Restricted})
		}
		if cur.Restricted == want {
			out = cur
			return store.ErrNoChange
		}
		row, err := w.SetProjectRestricted(ctx, writeq.SetProjectRestrictedParams{Restricted: want, TenantID: t.ID, ID: cur.ID, Version: version})
		if errors.Is(err, pgx.ErrNoRows) {
			return stale(cur.Version, map[string]any{fieldRestricted: cur.Restricted})
		}
		if err != nil {
			return err
		}
		out = project(row)
		w.Record(store.Event{EntityType: entityProject, EntityID: cur.ID, Action: actionUpdated,
			Before: map[string]any{fieldRestricted: cur.Restricted}, After: map[string]any{fieldRestricted: want},
			Membership: &store.MembershipChange{Project: cur.ID, Audience: store.AudienceMembers}})
		return nil
	})
	if err != nil && !errors.Is(err, store.ErrNoChange) {
		return nil, err
	}
	return apigen.SetProjectRestriction200JSONResponse{Body: projectView(out), Headers: apigen.SetProjectRestriction200ResponseHeaders{
		ETag: etag(out.Version)}}, nil
}

// ListProjectAccess lists a project's access list by person
// (docs/adr/0034 D3).
func (s *Server) ListProjectAccess(ctx context.Context, req apigen.ListProjectAccessRequestObject) (apigen.ListProjectAccessResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, adminRead); perr != nil {
		return nil, perr
	}
	const op = "listProjectAccess"
	size := s.h.pageSize(req.Params.Limit)
	var rows []readq.ListProjectAccessRow
	scope := t.ID.String() + "/" + req.Project
	after, perr := s.uuidCursor(op, scope, req.Params.Cursor)
	if perr != nil {
		return nil, perr
	}
	err := s.db.InTenant(ctx, t.ID, func(r *store.Reader) error {
		proj, err := visibleProject(ctx, r, t, req.Project)
		if err != nil {
			return err
		}
		rows, err = r.ListProjectAccess(ctx, readq.ListProjectAccessParams{TenantID: t.ID, ProjectID: proj.ID, After: after,
			PageSize: limitArg(size)})
		return err
	})
	if err != nil {
		return nil, err
	}
	rows, next := page(s.h, rows, size, op, scope, func(a readq.ListProjectAccessRow) string { return a.UserID.String() })
	out := apigen.ProjectAccessList{Items: []apigen.ProjectAccessEntry{}, NextCursor: nullableString(next)}
	for _, a := range rows {
		out.Items = append(out.Items, apigen.ProjectAccessEntry{
			Person:    apigen.Person{Id: a.UserID, Username: nullableOf(a.Username), DisplayName: a.DisplayName},
			Email:     addressFor(true, a.Email),
			Role:      apigen.ProjectAccessRole(a.Role),
			CreatedAt: a.CreatedAt,
		})
	}
	tag, unchanged := listTag(req.Params.IfNoneMatch, out)
	if unchanged {
		return apigen.ListProjectAccess304Response{Headers: apigen.NotModifiedResponseHeaders{ETag: &tag}}, nil
	}
	return apigen.ListProjectAccess200JSONResponse{Body: out, Headers: apigen.ListProjectAccess200ResponseHeaders{ETag: &tag}}, nil
}

func accessChange(person, projectID uuid.UUID) *store.MembershipChange {
	return &store.MembershipChange{Person: person, Project: projectID, Audience: store.AudienceAdminsAndPerson}
}

// SetProjectAccess puts a member of the tenant on a project's access list or
// changes their entry (docs/adr/0034 D3). A browser session only: an entry
// made with a leaked token would keep its person in a restricted project after
// the token's revocation (docs/adr/0035 D5).
func (s *Server) SetProjectAccess(ctx context.Context, req apigen.SetProjectAccessRequestObject) (apigen.SetProjectAccessResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, administer); perr != nil {
		return nil, perr
	}
	person, role := req.PersonId, domain.Role(req.Body.Role)
	var out apigen.ProjectAccessEntry
	_, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		proj, err := visibleProject(ctx, w.Reader, t, req.Project)
		if err != nil {
			return err
		}
		m, err := w.GetMember(ctx, readq.GetMemberParams{TenantID: t.ID, UserID: person})
		if errors.Is(err, pgx.ErrNoRows) {
			return personNotFound("the person is not a member of the team")
		}
		if err != nil {
			return err
		}
		out = apigen.ProjectAccessEntry{Person: apigen.Person{Id: m.ID, Username: nullableOf(m.Username), DisplayName: m.DisplayName},
			Email: addressFor(true, m.Email), Role: apigen.ProjectAccessRole(role)}
		key := readq.GetProjectAccessEntryParams{TenantID: t.ID, ProjectID: proj.ID, UserID: person}
		cur, err := w.GetProjectAccessEntry(ctx, key)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			row, err := w.InsertProjectAccess(ctx, writeq.InsertProjectAccessParams{TenantID: t.ID, ProjectID: proj.ID, UserID: person, Role: role})
			if err != nil {
				return err
			}
			out.CreatedAt = row.CreatedAt
			w.Record(store.Event{EntityType: entityProjectAccess, EntityID: row.ID, Action: actionCreated,
				After: map[string]any{fieldProject: proj.Key, fieldUser: person, fieldRole: role}, Membership: accessChange(person, proj.ID)})
		case err != nil:
			return err
		case cur.Role == role:
			out.CreatedAt = cur.CreatedAt
			return store.ErrNoChange
		default:
			row, err := w.SetProjectAccessRole(ctx, writeq.SetProjectAccessRoleParams{Role: role, TenantID: t.ID, ProjectID: proj.ID, UserID: person})
			if err != nil {
				return err
			}
			out.CreatedAt = row.CreatedAt
			w.Record(store.Event{EntityType: entityProjectAccess, EntityID: row.ID, Action: actionUpdated,
				Before: map[string]any{fieldRole: cur.Role}, After: map[string]any{fieldRole: role}, Membership: accessChange(person, proj.ID)})
		}
		return nil
	})
	if err != nil && !errors.Is(err, store.ErrNoChange) {
		return nil, err
	}
	return apigen.SetProjectAccess200JSONResponse(out), nil
}

// RemoveProjectAccess takes a person off a project's access list
// (docs/adr/0034 D3). An administrator's token may: it only takes access
// away.
func (s *Server) RemoveProjectAccess(ctx context.Context, req apigen.RemoveProjectAccessRequestObject) (apigen.RemoveProjectAccessResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, administer); perr != nil {
		return nil, perr
	}
	person := req.PersonId
	_, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		proj, err := visibleProject(ctx, w.Reader, t, req.Project)
		if err != nil {
			return err
		}
		row, err := w.DeleteProjectAccess(ctx, writeq.DeleteProjectAccessParams{TenantID: t.ID, ProjectID: proj.ID, UserID: person})
		if errors.Is(err, pgx.ErrNoRows) {
			return store.ErrNoChange
		}
		if err != nil {
			return err
		}
		w.Record(store.Event{EntityType: entityProjectAccess, EntityID: row.ID, Action: actionDeleted,
			Before: map[string]any{fieldProject: proj.Key, fieldUser: person, fieldRole: row.Role}, Membership: accessChange(person, proj.ID)})
		return nil
	})
	if err != nil && !errors.Is(err, store.ErrNoChange) {
		return nil, err
	}
	return apigen.RemoveProjectAccess204Response{}, nil
}
