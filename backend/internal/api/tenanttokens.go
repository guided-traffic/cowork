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

// ListTeamTokens lists the tokens that can act in the team for its
// administrators (docs/adr/0035 D5 as amended 2026-10-05): every token of a
// member that is unrestricted or restricted to this team, newest first, by
// cursor or numbered pages (docs/adr/0048 D2). Metadata only; a token
// restricted to another team is not in it.
func (s *Server) ListTeamTokens(ctx context.Context, req apigen.ListTeamTokensRequestObject) (apigen.ListTeamTokensResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, adminRead); perr != nil {
		return nil, perr
	}
	// The operation's name before the rename binds the cursor, so that a cursor
	// pages on across replicas of both releases during a rollout (docs/adr/0028 D4).
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
	out := apigen.MemberTokenList{Items: make([]apigen.MemberToken, 0, len(rows)), NextCursor: nullableString(next)}
	out.Total, out.Page, out.PerPage = lp.numbers(total)
	now := s.h.opts.Now()
	for _, row := range rows {
		out.Items = append(out.Items, memberTokenView(t, row, now))
	}
	// A page the client holds unchanged is a 304 (docs/adr/0054 D7); the tag
	// is the caller's page, so the state of a token that expired since moves it.
	tag, unchanged := listTag(req.Params.IfNoneMatch, out)
	if unchanged {
		return apigen.ListTeamTokens304Response{Headers: apigen.NotModifiedResponseHeaders{ETag: &tag}}, nil
	}
	return apigen.ListTeamTokens200JSONResponse{Body: out, Headers: apigen.ListTeamTokens200ResponseHeaders{ETag: &tag}}, nil
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
	v := apigen.MemberToken{
		Id: row.ID, Name: row.Name, Person: personView(row.UserID, row.Username, &row.DisplayName),
		Scope: apigen.Scope(row.Scope), Agent: row.Agent, Capabilities: capabilitiesView(row.Capabilities), CreatedAt: row.CreatedAt,
		ExpiresAt: row.ExpiresAt, State: state, RevokedAt: nullableOf(row.RevokedAt),
		RestrictedTeam: nullableOf[string](nil), RestrictedProject: nullableOf(row.RestrictedProjectKey),
		LastUsedOn: nullableOf[openapi_types.Date](nil),
	}
	if row.RestrictedTenantID != nil {
		v.RestrictedTeam = nullableOf(&t.Slug)
	}
	v.RestrictedTenant = v.RestrictedTeam //nolint:staticcheck // SA1019: deprecated in the document, answered beside restricted_team until a later release removes it
	if row.LastUsedOn != nil {
		v.LastUsedOn = nullableOf(&openapi_types.Date{Time: *row.LastUsedOn})
	}
	return v
}

// RevokeTeamToken revokes a member's token that can act in the team: an
// administrator's act, immediate and recorded in the team's audit
// (docs/adr/0035 D5, D6, D9). An unrestricted token ends in every team of
// its person. A token the team's list does not show is "no such", whether or
// not it exists; revoking a revoked one changes nothing.
func (s *Server) RevokeTeamToken(ctx context.Context, req apigen.RevokeTeamTokenRequestObject) (apigen.RevokeTeamTokenResponseObject, error) {
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
		w.Record(store.Event{EntityType: entityToken, EntityID: tok.ID, Action: actionRevoked,
			After: map[string]any{fieldUser: tok.UserID, fieldName: tok.Name, "unrestricted": tok.RestrictedTenantID == nil}})
		return nil
	})
	if err != nil && !errors.Is(err, store.ErrNoChange) {
		return nil, err
	}
	return apigen.RevokeTeamToken204Response{}, nil
}
