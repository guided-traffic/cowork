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

// tenantScope is the tenant a request under /tenants/{tenant} acts in, with
// the person's role there.
type tenantScope struct {
	ID   uuid.UUID
	Slug string
	Name string
	Role domain.Role
}

type tenantKey struct{}

func withTenant(ctx context.Context, t tenantScope) context.Context {
	return context.WithValue(ctx, tenantKey{}, t)
}

// tenantFrom returns the tenant the boundary admitted the request to; every
// handler under /tenants/{tenant} has one.
func tenantFrom(ctx context.Context) tenantScope {
	t, _ := ctx.Value(tenantKey{}).(tenantScope)
	return t
}

// tenantWideForProjectTokens are the operations under a tenant that a token
// restricted to one project may call: lists the data layer narrows to its
// project. Every other tenant-level route is outside the project, where the
// token is invalid (docs/adr/0035 D3).
var tenantWideForProjectTokens = map[string]bool{
	"listProjects":      true,
	"listTenantTickets": true,
	"resolveTicket":     true,
	opStreamEvents:      true,
}

// boundary admits a request to a tenant before any handler runs
// (docs/adr/0023 D5): the person must be a member, and the token must not be
// restricted elsewhere. Every refusal is the same 404 as an unknown slug, so
// the answer does not tell whether the tenant exists (docs/adr/0047 D5).
func (h *handler) boundary(ctx context.Context, slug, path, operationID string) (tenantScope, *problem.Error) {
	refused := problem.New(problem.NotFound, "no such tenant")
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
		h.logger.Error("tenant boundary failed", "request_id", requestid.From(ctx), "error", err)
		return tenantScope{}, problem.New(problem.Internal, "internal error")
	}
	if !found {
		return tenantScope{}, refused
	}
	if p.RestrictedTenantID != uuid.Nil && p.RestrictedTenantID != scope.ID {
		return tenantScope{}, refused
	}
	if p.RestrictedProjectID != uuid.Nil && !strings.Contains(path, "{project}") && !tenantWideForProjectTokens[operationID] {
		return tenantScope{}, refused
	}
	return scope, nil
}
