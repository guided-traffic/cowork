package api

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/problem"
	"github.com/guided-traffic/cowork/backend/internal/store"
	"github.com/guided-traffic/cowork/backend/internal/store/readq"
	"github.com/guided-traffic/cowork/backend/internal/store/writeq"
)

// ListTenantTokens lists the tokens that can act in the tenant for its
// administrators (docs/adr/0035 D5 as amended 2026-10-05): every token of a
// member that is unrestricted or restricted to this tenant, newest first, by
// cursor or numbered pages (docs/adr/0048 D2). Metadata only; a token
// restricted to another tenant is not in it.
func (s *Server) ListTenantTokens(ctx context.Context, req apigen.ListTenantTokensRequestObject) (apigen.ListTenantTokensResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, adminRead); perr != nil {
		return nil, perr
	}
	const op = "listTenantTokens"
	scope := t.ID.String()
	q := req.Params
	lp, perr := s.h.tablePage(q.Cursor, q.Limit, q.Page, (*int)(q.PerPage))
	if perr != nil {
		return nil, perr
	}
	var before *uuid.UUID
	if !lp.numbered {
		if before, perr = s.uuidCursor(op, scope, q.Cursor); perr != nil {
			return nil, perr
		}
	}
	var rows []readq.ListTenantTokensRow
	var total int64
	err := s.db.InTenant(ctx, t.ID, func(r *store.Reader) error {
		var err error
		rows, err = r.ListTenantTokens(ctx, readq.ListTenantTokensParams{TenantID: t.ID, Before: before,
			PageSize: lp.limit(), PageOffset: lp.offset()})
		if err != nil || !lp.numbered {
			return err
		}
		total, err = r.CountTenantTokens(ctx, t.ID)
		return err
	})
	if err != nil {
		return nil, err
	}
	var next *string
	if !lp.numbered {
		rows, next = page(s.h, rows, lp.size, op, scope, func(row readq.ListTenantTokensRow) string { return row.ID.String() })
	}
	out := apigen.ListTenantTokens200JSONResponse{Items: make([]apigen.MemberToken, 0, len(rows)), NextCursor: nullableString(next)}
	out.Total, out.Page, out.PerPage = lp.numbers(total)
	now := s.h.opts.Now()
	for _, row := range rows {
		out.Items = append(out.Items, memberTokenView(t, row, now))
	}
	return out, nil
}

// memberTokenView is a token as the tenant's administrators see it: its
// person and its metadata; restricted_tenant names this tenant or nothing.
func memberTokenView(t tenantScope, row readq.ListTenantTokensRow, now time.Time) apigen.MemberToken {
	state := apigen.TokenStateActive
	switch {
	case row.RevokedAt != nil:
		state = apigen.TokenStateRevoked
	case !row.ExpiresAt.After(now):
		state = apigen.TokenStateExpired
	}
	caps := make([]apigen.Capability, 0, len(row.Capabilities))
	for _, c := range row.Capabilities {
		caps = append(caps, apigen.Capability(c))
	}
	v := apigen.MemberToken{
		Id: row.ID, Name: row.Name, Person: personView(row.UserID, row.Username, &row.DisplayName),
		Scope: apigen.Scope(row.Scope), Agent: row.Agent, Capabilities: caps, CreatedAt: row.CreatedAt,
		ExpiresAt: row.ExpiresAt, State: state, RevokedAt: nullableOf(row.RevokedAt),
		RestrictedTenant: nullableOf[string](nil), RestrictedProject: nullableOf(row.RestrictedProjectKey),
		LastUsedOn: nullableOf[openapi_types.Date](nil),
	}
	if row.RestrictedTenantID != nil {
		v.RestrictedTenant = nullableOf(&t.Slug)
	}
	if row.LastUsedOn != nil {
		v.LastUsedOn = nullableOf(&openapi_types.Date{Time: *row.LastUsedOn})
	}
	return v
}

// RevokeTenantToken revokes a member's token that can act in the tenant: an
// administrator's act, immediate and recorded in the tenant's audit
// (docs/adr/0035 D5, D6, D9). An unrestricted token ends in every tenant of
// its person. A token the tenant's list does not show is "no such", whether or
// not it exists; revoking a revoked one changes nothing.
func (s *Server) RevokeTenantToken(ctx context.Context, req apigen.RevokeTenantTokenRequestObject) (apigen.RevokeTenantTokenResponseObject, error) {
	t, p := tenantFrom(ctx), principal(ctx)
	if perr := auth.Authorize(p, t.Role, administer); perr != nil {
		return nil, perr
	}
	_, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		tok, err := w.GetTenantToken(ctx, readq.GetTenantTokenParams{TokenID: req.TokenId, TenantID: t.ID})
		if errors.Is(err, pgx.ErrNoRows) {
			return problem.New(problem.NotFound, "no such token")
		}
		if err != nil {
			return err
		}
		if tok.RevokedAt != nil {
			return store.ErrNoChange
		}
		_, err = w.RevokeTenantToken(ctx, writeq.RevokeTenantTokenParams{TokenID: tok.ID, TenantID: t.ID, RevokedBy: &p.PersonID})
		if errors.Is(err, pgx.ErrNoRows) {
			// A simultaneous revocation came first.
			return store.ErrNoChange
		}
		if err != nil {
			return err
		}
		w.Record(store.Event{EntityType: entityToken, EntityID: tok.ID, Action: "revoked",
			After: map[string]any{fieldUser: tok.UserID, fieldName: tok.Name, "unrestricted": tok.RestrictedTenantID == nil}})
		return nil
	})
	if err != nil && !errors.Is(err, store.ErrNoChange) {
		return nil, err
	}
	return apigen.RevokeTenantToken204Response{}, nil
}
