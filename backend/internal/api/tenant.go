package api

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/problem"
	"github.com/guided-traffic/cowork/backend/internal/requestid"
	"github.com/guided-traffic/cowork/backend/internal/store"
	"github.com/guided-traffic/cowork/backend/internal/store/readq"
)

// tenantScope is the tenant a request under /teams/{team} acts in, with
// the person's role there. Oversight marks a global administrator who holds no
// role in the tenant (docs/adr/0034 D2): Role is empty, so every need of
// auth.Authorize refuses them, and the boundary admitted them to the
// operations of oversight only.
type tenantScope struct {
	ID        uuid.UUID
	Slug      string
	Name      string
	Role      domain.Role
	Oversight bool
}

type tenantKey struct{}

func withTenant(ctx context.Context, t tenantScope) context.Context {
	return context.WithValue(ctx, tenantKey{}, t)
}

// tenantFrom returns the tenant the boundary admitted the request to; every
// handler under /teams/{team} has one.
func tenantFrom(ctx context.Context) tenantScope {
	t, _ := ctx.Value(tenantKey{}).(tenantScope)
	return t
}

// tenantWideForProjectTokens are the operations under a tenant that a token
// restricted to one project may call: lists the data layer narrows to its
// project. Every other tenant-level route is outside the project, where the
// token is invalid (docs/adr/0035 D3).
var tenantWideForProjectTokens = map[string]bool{
	"listProjects":    true,
	"listTeamTickets": true,
	"searchTeam":      true,
	"resolveTicket":   true,
	opStreamEvents:    true,
}

// oversight are the operations under a tenant that a global administrator
// reaches without a role in it (docs/adr/0034 D2): its administration — the
// tenant and its settings, the members, the group mappings — and the grant of
// a role to themselves, which SetMemberGrant holds to their own person. Nothing
// of the tenant's work: no project, ticket, time entry, attachment, event or
// audit row, and no other act.
var oversight = map[string]bool{
	"getTeam":           true,
	"listMembers":       true,
	"listGroupMappings": true,
	"setMemberGrant":    true,
}

// oversees says whether the request may reach a tenant in which its person
// holds no role: a global administrator's, in a browser session no agent
// marks, on an operation of oversight. A token keeps the reach of its
// person's memberships, so a leaked one of a global administrator gains
// nothing by it, and an agent — the chat in the UI among them — none either.
func oversees(p auth.Principal, operationID string) bool {
	return p.GlobalAdmin && p.Session && !p.IsAgent() && oversight[operationID]
}

// boundary admits a request to a tenant before any handler runs
// (docs/adr/0023 D5): the person must be a member, and the token must not be
// restricted elsewhere — or the person is a global administrator who oversees
// the tenant (oversees). Every refusal is the same 404 as an unknown slug, so
// the answer does not tell whether the tenant exists (docs/adr/0047 D5).
func (h *handler) boundary(ctx context.Context, slug, path, operationID string) (tenantScope, *problem.Error) {
	refused := problem.New(problem.NotFound, "no such team")
	p, ok := auth.PrincipalFrom(ctx)
	if !ok {
		return tenantScope{}, refused
	}
	var (
		scope tenantScope
		found bool
	)
	err := h.opts.DB.Installation(ctx, func(r *store.Reader) error {
		row, err := r.GetTenantForPerson(ctx, readq.GetTenantForPersonParams{Slug: slug, UserID: p.PersonID})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		scope, found = tenantScope{ID: row.ID, Slug: row.Slug, Name: row.Name, Role: row.Role}, true
		return nil
	})
	if err != nil {
		h.logger.Error("team boundary failed", "request_id", requestid.From(ctx), "error", err)
		return tenantScope{}, problem.New(problem.Internal, "internal error")
	}
	if !found {
		return h.overseen(ctx, p, slug, operationID)
	}
	if p.RestrictedTenantID != uuid.Nil && p.RestrictedTenantID != scope.ID {
		return tenantScope{}, refused
	}
	if p.RestrictedProjectID != uuid.Nil && !strings.Contains(path, "{project}") && !tenantWideForProjectTokens[operationID] {
		return tenantScope{}, refused
	}
	return scope, nil
}

// overseen admits a global administrator to a tenant in which they hold no
// role, for an operation of oversight (docs/adr/0034 D2), or refuses like an
// unknown slug. The tenants policy shows a global administrator every tenant
// (migration 26).
func (h *handler) overseen(ctx context.Context, p auth.Principal, slug, operationID string) (tenantScope, *problem.Error) {
	refused := problem.New(problem.NotFound, "no such team")
	if !oversees(p, operationID) {
		return tenantScope{}, refused
	}
	var (
		row   readq.GetTenantBySlugRow
		found bool
	)
	err := h.opts.DB.Installation(ctx, func(r *store.Reader) error {
		var err error
		row, err = r.GetTenantBySlug(ctx, slug)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		found = err == nil
		return err
	})
	if err != nil {
		h.logger.Error("team boundary failed", "request_id", requestid.From(ctx), "error", err)
		return tenantScope{}, problem.New(problem.Internal, "internal error")
	}
	if !found {
		return tenantScope{}, refused
	}
	return tenantScope{ID: row.ID, Slug: row.Slug, Name: row.Name, Oversight: true}, nil
}

// administrationRead authorizes a read of the tenant's administration — the
// tenant, its members, its group mappings: by the caller's role and need, or
// for a global administrator the boundary admitted without a role, by that
// admission alone (docs/adr/0034 D2).
func administrationRead(p auth.Principal, t tenantScope, need auth.Need) *problem.Error {
	if t.Oversight {
		return nil
	}
	return auth.Authorize(p, t.Role, need)
}
