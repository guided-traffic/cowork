package api

import (
	"context"
	"errors"
	"net/http"
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

// The repositories a project owns, bound by their normalised remote identity
// (docs/adr/0066, docs/adr/0006 D3).

const (
	entityRepository = "repository"
	fieldRepository  = "repository"
	fieldPath        = "path"
	fieldRemote      = "remote"
)

// repository is the columns a binding's queries return.
type repository = writeq.InsertRepositoryRow

func repositoryView(r repository) apigen.Repository {
	return apigen.Repository{Id: r.ID, Identity: r.Identity, Path: r.Path, Remote: r.Remote,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
}

// creating is creating a project or binding a repository to one: write scope,
// an administrator always, a member while the tenant lets members create
// projects (docs/adr/0034 D9), and an agent with create-project
// (docs/adr/0043 D4, docs/adr/0066 D7).
func creating(membersCreateProjects bool) auth.Need {
	need := auth.Need{Role: domain.RoleAdmin, Scope: domain.ScopeWrite, Capability: auth.CapCreateProject}
	if membersCreateProjects {
		need.Role = domain.RoleMember
	}
	return need
}

// mayCreateProjects says whether a role lets its person create a project in a
// tenant whose members may, or may not, create them (docs/adr/0034 D9): the
// role creating asks for, without the scope and the capability a request's
// credential has to bring besides — what GET /api/v1/me answers per membership.
func mayCreateProjects(role domain.Role, membersCreateProjects bool) bool {
	return role.AtLeast(creating(membersCreateProjects).Role)
}

// boundRepository is a repository named by a request, normalised.
type boundRepository struct {
	identity, path, remote string
}

// readRepository normalises the remote and the path of a request; the remote
// is kept without its credentials (docs/adr/0066 D1). pointer is where the
// body holds it.
func readRepository(body apigen.RepositoryBind, pointer string) (boundRepository, *problem.Error) {
	identity, err := domain.NormaliseRemote(body.Remote)
	if err != nil {
		return boundRepository{}, problem.Field(pointer+"/remote", err.Error())
	}
	b := boundRepository{identity: identity, remote: domain.SanitiseRemote(body.Remote)}
	if body.Path != nil {
		if b.path, err = domain.NormaliseRepositoryPath(*body.Path); err != nil {
			return boundRepository{}, problem.Field(pointer+"/path", err.Error())
		}
	}
	return b, nil
}

// ListRepositories lists the repositories a project the caller sees owns.
func (s *Server) ListRepositories(ctx context.Context, req apigen.ListRepositoriesRequestObject) (apigen.ListRepositoriesResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, read); perr != nil {
		return nil, perr
	}
	const op = "listRepositories"
	scope := t.ID.String() + "/" + req.Project
	size := s.h.pageSize(req.Params.Limit)
	var rows []readq.ListProjectRepositoriesRow
	err := s.db.InTenant(ctx, t.ID, func(r *store.Reader) error {
		p, err := visibleProject(ctx, r, t, req.Project)
		if err != nil {
			return err
		}
		after, err := s.uuidAfter(op, scope, req.Params.Cursor)
		if err != nil {
			return err
		}
		rows, err = r.ListProjectRepositories(ctx, readq.ListProjectRepositoriesParams{TenantID: t.ID, ProjectID: p.ID,
			After: after, PageSize: limitArg(size)})
		return err
	})
	if err != nil {
		return nil, err
	}
	rows, next := page(s.h, rows, size, op, scope, func(r readq.ListProjectRepositoriesRow) string { return r.ID.String() })
	out := apigen.ListRepositories200JSONResponse{Items: make([]apigen.Repository, 0, len(rows)), NextCursor: nullableString(next)}
	for _, r := range rows {
		out.Items = append(out.Items, apigen.Repository{Id: r.ID, Identity: r.Identity, Path: r.Path, Remote: r.Remote,
			CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt})
	}
	return out, nil
}

// mayBind authorizes binding a repository to a project, or unbinding it: the
// act of creating a project, judged by the person's role in that project.
func mayBind(ctx context.Context, r *store.Reader, t tenantScope, p project) error {
	role, err := projectRole(ctx, r, t, p)
	if err != nil {
		return err
	}
	tenant, err := r.GetTenant(ctx, t.ID)
	if err != nil {
		return err
	}
	if perr := auth.Authorize(principal(ctx), role, creating(tenant.MembersCreateProjects)); perr != nil {
		return perr
	}
	return nil
}

// BindRepository binds a repository to a project, idempotent over its
// identity and path (docs/adr/0066 D5): a binding the project holds already
// answers 200 and keeps the remote as last given; another project's is 409.
func (s *Server) BindRepository(ctx context.Context, req apigen.BindRepositoryRequestObject) (apigen.BindRepositoryResponseObject, error) {
	t := tenantFrom(ctx)
	b, perr := readRepository(*req.Body, "")
	if perr != nil {
		return nil, perr
	}
	ctx, perr = s.keyed(ctx, req.Params.IdempotencyKey, "bindRepository", t.ID.String()+"/"+req.Project, *req.Body)
	if perr != nil {
		return nil, perr
	}
	var (
		out     repository
		created bool
	)
	replay, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		p, err := visibleProject(ctx, w.Reader, t, req.Project)
		if err != nil {
			return err
		}
		if err := mayBind(ctx, w.Reader, t, p); err != nil {
			return err
		}
		if out, created, err = bind(ctx, w, t, p, b); err != nil {
			return err
		}
		// A keyed binding stores its answer: 201 for a new one, 200 for a
		// remote given in another form.
		res, err := stored(repositoryView(out), map[string]string{headerLocation: repositoryURL(t, p.Key, out.ID)})
		if !created {
			res.Status, res.Headers = http.StatusOK, nil
		}
		w.Respond(res)
		return err
	})
	switch {
	case errors.Is(err, store.ErrNoChange):
		return apigen.BindRepository200JSONResponse(repositoryView(out)), nil
	case err != nil:
		return nil, err
	case replay != nil:
		body, err := replayed[apigen.Repository](replay)
		if err != nil {
			return nil, err
		}
		if replay.Status == http.StatusOK {
			return apigen.BindRepository200JSONResponse(body), nil
		}
		return apigen.BindRepository201JSONResponse{Body: body, Headers: apigen.BindRepository201ResponseHeaders{
			Location: header(replay, headerLocation)}}, nil
	case !created:
		return apigen.BindRepository200JSONResponse(repositoryView(out)), nil
	}
	location := repositoryURL(t, req.Project, out.ID)
	return apigen.BindRepository201JSONResponse{Body: repositoryView(out),
		Headers: apigen.BindRepository201ResponseHeaders{Location: &location}}, nil
}

func repositoryURL(t tenantScope, project string, id uuid.UUID) string {
	return projectURL(t, project) + "/repositories/" + id.String()
}

// bind writes a binding of the project, with its act: linked for a new one,
// updated when the project binds the repository already and the remote was
// given in another form. A binding of the project that changes nothing is
// store.ErrNoChange with the binding; another project's is 409.
func bind(ctx context.Context, w *store.Writer, t tenantScope, p project, b boundRepository) (repository, bool, error) {
	cur, err := w.GetRepositoryBinding(ctx, readq.GetRepositoryBindingParams{TenantID: t.ID, Identity: b.identity, Path: b.path})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		row, err := w.InsertRepository(ctx, writeq.InsertRepositoryParams{TenantID: t.ID, ProjectID: p.ID,
			Identity: b.identity, Path: b.path, Remote: b.remote})
		if errors.Is(err, pgx.ErrNoRows) {
			// A simultaneous binding came first; answer as the later request would.
			return bind(ctx, w, t, p, b)
		}
		if err != nil {
			return repository{}, false, err
		}
		w.Record(store.Event{EntityType: entityRepository, EntityID: row.ID, Action: actionLinked,
			After: map[string]any{fieldProject: p.Key, fieldRepository: b.identity, fieldPath: b.path, fieldRemote: b.remote}})
		return row, true, nil
	case err != nil:
		return repository{}, false, err
	case cur.ProjectID != p.ID:
		return repository{}, false, boundElsewhere(ctx, w.Reader, t, cur.ProjectID, "/remote")
	case cur.Remote == b.remote:
		return repository(cur), false, store.ErrNoChange
	}
	row, err := w.UpdateRepositoryRemote(ctx, writeq.UpdateRepositoryRemoteParams{TenantID: t.ID, ID: cur.ID, Remote: b.remote})
	if err != nil {
		return repository{}, false, err
	}
	w.Record(store.Event{EntityType: entityRepository, EntityID: cur.ID, Action: actionUpdated,
		Before: map[string]any{fieldRemote: cur.Remote}, After: map[string]any{fieldRemote: b.remote}})
	return repository(row), false, nil
}

// boundElsewhere is the 409 of a repository another project of the tenant
// binds: it names the project only when the caller sees it, so the answer
// tells no more than that the binding exists, as a taken project key does.
// pointer is where the request holds the remote.
func boundElsewhere(ctx context.Context, r *store.Reader, t tenantScope, projectID uuid.UUID, pointer string) error {
	e := &problem.Error{Code: problem.RepositoryBound, Detail: "another project of the team binds this repository",
		Errors: []problem.FieldError{{Pointer: pointer, Message: "bound to another project"}}}
	other, err := r.GetVisibleProjectByID(ctx, readq.GetVisibleProjectByIDParams{TenantID: t.ID, ID: projectID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return e
	case err != nil:
		return err
	}
	e.Detail = "the project " + t.Slug + "/" + other.Key + " binds this repository"
	return e
}

// bindingProject is the project of the tenant that binds a repository, for a
// creation that is idempotent over the remote (docs/adr/0066 D5): found is
// false when none does, and a project the caller cannot see is the 409.
func bindingProject(ctx context.Context, r *store.Reader, t tenantScope, b boundRepository) (project, bool, error) {
	cur, err := r.GetRepositoryBinding(ctx, readq.GetRepositoryBindingParams{TenantID: t.ID, Identity: b.identity, Path: b.path})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return project{}, false, nil
	case err != nil:
		return project{}, false, err
	}
	row, err := r.GetVisibleProjectByID(ctx, readq.GetVisibleProjectByIDParams{TenantID: t.ID, ID: cur.ProjectID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return project{}, false, boundElsewhere(ctx, r, t, cur.ProjectID, "/repository/remote")
	case err != nil:
		return project{}, false, err
	}
	return project(row), true, nil
}

// UnbindRepository removes a binding of the project, by the people who may
// bind; idempotent (docs/adr/0045 D1).
func (s *Server) UnbindRepository(ctx context.Context, req apigen.UnbindRepositoryRequestObject) (apigen.UnbindRepositoryResponseObject, error) {
	t := tenantFrom(ctx)
	_, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		p, err := visibleProject(ctx, w.Reader, t, req.Project)
		if err != nil {
			return err
		}
		if err := mayBind(ctx, w.Reader, t, p); err != nil {
			return err
		}
		row, err := w.DeleteRepository(ctx, writeq.DeleteRepositoryParams{TenantID: t.ID, ProjectID: p.ID, ID: req.Repository})
		if errors.Is(err, pgx.ErrNoRows) {
			return store.ErrNoChange
		}
		if err != nil {
			return err
		}
		w.Record(store.Event{EntityType: entityRepository, EntityID: row.ID, Action: actionUnlinked,
			Before: map[string]any{fieldProject: p.Key, fieldRepository: row.Identity, fieldPath: row.Path, fieldRemote: row.Remote}})
		return nil
	})
	if err != nil && !errors.Is(err, store.ErrNoChange) {
		return nil, err
	}
	return apigen.UnbindRepository204Response{}, nil
}

// lookupTenant is a tenant the lookup searches: one of the person's, and for
// a token restricted to a tenant that one only (docs/adr/0035 D3).
type lookupTenant struct {
	id         uuid.UUID
	slug, name string
	role       domain.Role
}

// foundBinding is a binding the lookup found, with its tenant.
type foundBinding struct {
	tenant lookupTenant
	row    readq.FindRepositoryBindingsRow
}

// LookupRepository finds the project a repository is bound to across the
// person's tenants, or proposes one (docs/adr/0066 D2): the remotes are tried
// in the order given, the first with a binding that covers the path decides,
// and several bindings are reported as the data error they are (D6).
func (s *Server) LookupRepository(ctx context.Context, req apigen.LookupRepositoryRequestObject) (apigen.LookupRepositoryResponseObject, error) {
	path := ""
	if req.Params.Path != nil {
		var err error
		if path, err = domain.NormaliseRepositoryPath(*req.Params.Path); err != nil {
			return nil, problem.Field("query:path", err.Error())
		}
	}
	out := apigen.LookupRepository200JSONResponse{Status: apigen.RepositoryLookupStatusUnbound,
		Remotes: []apigen.LookupRemote{}, Bindings: []apigen.RepositoryBindingRef{},
		Proposal: nullableOf[apigen.RepositoryProposal](nil), ProposalUnavailable: nullableString(nil)}
	var identities []string
	remoteOf := map[string]string{}
	for _, raw := range req.Params.Remote {
		shown := domain.SanitiseRemote(raw)
		identity, err := domain.NormaliseRemote(raw)
		if err != nil {
			out.Remotes = append(out.Remotes, apigen.LookupRemote{Remote: shown, Identity: nullableString(nil)})
			continue
		}
		out.Remotes = append(out.Remotes, apigen.LookupRemote{Remote: shown, Identity: nullableOf(&identity)})
		if _, seen := remoteOf[identity]; !seen {
			identities = append(identities, identity)
			remoteOf[identity] = shown
		}
	}
	if len(identities) == 0 {
		out.ProposalUnavailable = nullableOf(ptrTo("no remote names a host, so there is no identity to bind; a .cowork.yaml binds such a repository"))
		return out, nil
	}
	tenants, err := s.lookupTenants(ctx)
	if err != nil {
		return nil, err
	}
	found, err := s.findBindings(ctx, tenants, identities)
	if err != nil {
		return nil, err
	}
	if chosen := chooseBindings(found, identities, path); len(chosen) > 0 {
		out.Status = apigen.RepositoryLookupStatusBound
		if len(chosen) > 1 {
			out.Status = apigen.RepositoryLookupStatusAmbiguous
		}
		for _, b := range chosen {
			team := apigen.TeamRef{Slug: b.tenant.slug, Name: b.tenant.name}
			out.Bindings = append(out.Bindings, apigen.RepositoryBindingRef{
				Team:     team,
				Tenant:   team, //nolint:staticcheck // SA1019: deprecated in the document, answered beside team until a later release removes it
				Project:  apigen.ProjectRef{Key: b.row.ProjectKey, Name: b.row.ProjectName},
				Identity: b.row.Identity, Path: b.row.Path, Remote: b.row.Remote, Archived: b.row.ArchivedAt != nil,
			})
		}
		return out, nil
	}
	proposal, why, err := s.propose(ctx, tenants, identities[0], remoteOf[identities[0]])
	if err != nil {
		return nil, err
	}
	if proposal == nil {
		out.ProposalUnavailable = nullableOf(&why)
		return out, nil
	}
	out.Proposal = nullableOf(proposal)
	return out, nil
}

// lookupTenants are the person's tenants the request reaches.
func (s *Server) lookupTenants(ctx context.Context) ([]lookupTenant, error) {
	p := principal(ctx)
	var out []lookupTenant
	err := s.db.Installation(ctx, func(r *store.Reader) error {
		rows, err := r.ListMembershipsOfUser(ctx, p.PersonID)
		if err != nil {
			return err
		}
		for _, m := range rows {
			if restricted(p) && m.TenantID != p.RestrictedTenantID {
				continue
			}
			out = append(out, lookupTenant{id: m.TenantID, slug: m.Slug, name: m.Name, role: m.Role})
		}
		return nil
	})
	return out, err
}

// findBindings reads the bindings of the identities in every tenant, one
// transaction per tenant, each bound to its tenant (docs/adr/0021 D5).
func (s *Server) findBindings(ctx context.Context, tenants []lookupTenant, identities []string) ([]foundBinding, error) {
	var found []foundBinding
	for _, t := range tenants {
		err := s.db.InTenant(ctx, t.id, func(r *store.Reader) error {
			rows, err := r.FindRepositoryBindings(ctx, readq.FindRepositoryBindingsParams{TenantID: t.id, Identities: identities})
			for _, row := range rows {
				found = append(found, foundBinding{tenant: t, row: row})
			}
			return err
		})
		if err != nil {
			return nil, err
		}
	}
	return found, nil
}

// chooseBindings returns the bindings that decide: those of the first
// identity, in the order given, with a binding whose sub-directory covers
// the path, and of those the ones with the most specific sub-directory.
func chooseBindings(found []foundBinding, identities []string, path string) []foundBinding {
	for _, identity := range identities {
		depth := -1
		for _, b := range found {
			if b.row.Identity == identity && domain.PathCovers(b.row.Path, path) {
				depth = max(depth, len(b.row.Path))
			}
		}
		if depth < 0 {
			continue
		}
		chosen := make([]foundBinding, 0, len(found))
		for _, b := range found {
			if b.row.Identity == identity && domain.PathCovers(b.row.Path, path) && len(b.row.Path) == depth {
				chosen = append(chosen, b)
			}
		}
		return chosen
	}
	return nil
}

// maxKeyCandidates bounds the search for a free key.
const maxKeyCandidates = 99

// propose builds the proposal for an unbound repository (docs/adr/0066 D2,
// D3): the tenants the caller may create a project in, narrowed to the one
// the person has, or to those that bind repositories under the same remote
// owner; a key free in each; the repository's name. No tenant is a reason.
func (s *Server) propose(ctx context.Context, tenants []lookupTenant, identity, remote string) (*apigen.RepositoryProposal, string, error) {
	if principal(ctx).RestrictedProjectID != uuid.Nil {
		return nil, "this token is restricted to one project and cannot create another", nil
	}
	creatable, underOwner, refusal, err := s.creatableTenants(ctx, tenants, domain.RemoteOwner(identity)+"/")
	if err != nil {
		return nil, "", err
	}
	proposal := &apigen.RepositoryProposal{Identity: identity, Remote: remote, Name: domain.RepositoryName(identity),
		Team: nullableString(nil), Teams: []apigen.ProposalTeam{}}
	offered := creatable
	switch {
	case len(creatable) == 0 && refusal != "":
		return nil, "you may create a project in none of your teams: " + refusal, nil
	case len(creatable) == 0:
		return nil, "you belong to no team this request reaches", nil
	case len(creatable) == 1:
		proposal.Reason = apigen.RepositoryProposalReasonOnlyTenant
	case len(underOwner) == 1:
		proposal.Reason, offered = apigen.RepositoryProposalReasonRemoteOwner, underOwner
	case len(underOwner) > 1:
		proposal.Reason, offered = apigen.RepositoryProposalReasonChoose, underOwner
	default:
		proposal.Reason = apigen.RepositoryProposalReasonChoose
	}
	base := domain.ProposeProjectKey(proposal.Name)
	for _, t := range offered {
		key, err := s.freeKey(ctx, t, base)
		if err != nil {
			return nil, "", err
		}
		proposal.Teams = append(proposal.Teams, apigen.ProposalTeam{Slug: t.slug, Name: t.name, Key: key})
	}
	if len(offered) == 1 {
		proposal.Team = nullableOf(&offered[0].slug)
	}
	proposal.Tenant, proposal.Tenants = proposal.Team, proposal.Teams //nolint:staticcheck // SA1019: deprecated in the document, answered beside team and teams until a later release removes them
	return proposal, "", nil
}

// creatableTenants are the tenants the caller may create a project in, and
// of them those that bind a repository under the owner; refusal is why the
// last tenant refused.
func (s *Server) creatableTenants(ctx context.Context, tenants []lookupTenant, owner string) (creatable, underOwner []lookupTenant, refusal string, err error) {
	for _, t := range tenants {
		var tenant readq.GetTenantRow
		var found bool
		err := s.db.InTenant(ctx, t.id, func(r *store.Reader) error {
			var err error
			if tenant, err = r.GetTenant(ctx, t.id); err != nil {
				return err
			}
			found, err = r.RepositoriesUnderOwner(ctx, readq.RepositoriesUnderOwnerParams{TenantID: t.id, Prefix: owner})
			return err
		})
		if err != nil {
			return nil, nil, "", err
		}
		if perr := auth.Authorize(principal(ctx), t.role, creating(tenant.MembersCreateProjects)); perr != nil {
			refusal = perr.Detail
			continue
		}
		creatable = append(creatable, t)
		if found {
			underOwner = append(underOwner, t)
		}
	}
	return creatable, underOwner, refusal, nil
}

// freeKey is the first candidate of the base key that the tenant has not
// taken (docs/adr/0066 D2: a number appended on collision).
func (s *Server) freeKey(ctx context.Context, t lookupTenant, base string) (string, error) {
	var key string
	err := s.db.InTenant(ctx, t.id, func(r *store.Reader) error {
		for n := 1; n <= maxKeyCandidates; n++ {
			candidate := domain.KeyCandidate(base, n)
			taken, err := r.ProjectKeyTaken(ctx, readq.ProjectKeyTakenParams{TenantID: t.id, Key: candidate})
			if err != nil {
				return err
			}
			if !taken {
				key = candidate
				return nil
			}
		}
		key = strings.ToUpper(base)
		return nil
	})
	return key, err
}
