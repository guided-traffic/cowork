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
	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/problem"
	"github.com/guided-traffic/cowork/backend/internal/store"
	"github.com/guided-traffic/cowork/backend/internal/store/readq"
	"github.com/guided-traffic/cowork/backend/internal/store/writeq"
)

// restricted reports whether the token is restricted to a tenant, and to a
// project of it: such a token is invalid outside them (docs/adr/0035 D3), so
// on the person's own routes it sees its tenant and itself only.
func restricted(p auth.Principal) bool { return p.RestrictedTenantID != uuid.Nil }

// GetMe answers the calling person and their tenants (docs/adr/0023 D2); a
// restricted token sees the membership of its tenant only.
func (s *Server) GetMe(ctx context.Context, _ apigen.GetMeRequestObject) (apigen.GetMeResponseObject, error) {
	p := principal(ctx)
	var out apigen.Me
	err := s.db.Installation(ctx, func(r *store.Reader) error {
		u, err := r.GetUser(ctx, p.PersonID)
		if err != nil {
			return err
		}
		memberships, err := r.ListMembershipsOfUser(ctx, p.PersonID)
		if err != nil {
			return err
		}
		out = apigen.Me{Id: u.ID, Username: nullableOf(u.Username), DisplayName: u.DisplayName, Memberships: []apigen.Membership{}}
		for _, m := range memberships {
			if restricted(p) && m.TenantID != p.RestrictedTenantID {
				continue
			}
			out.Memberships = append(out.Memberships, apigen.Membership{
				Tenant: apigen.TenantRef{Slug: m.Slug, Name: m.Name},
				Role:   apigen.Role(m.Role),
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return apigen.GetMe200JSONResponse(out), nil
}

// ListMyTokens lists the person's tokens, newest first: metadata only
// (docs/adr/0035 D1, D6); a restricted token lists itself only.
func (s *Server) ListMyTokens(ctx context.Context, req apigen.ListMyTokensRequestObject) (apigen.ListMyTokensResponseObject, error) {
	p := principal(ctx)
	const op = "listMyTokens"
	scope := p.PersonID.String()
	var before *uuid.UUID
	if req.Params.Cursor != nil {
		after, perr := s.cursors.decode(op, scope, *req.Params.Cursor)
		if perr != nil {
			return nil, perr
		}
		id, err := uuid.Parse(after)
		if err != nil {
			return nil, problem.New(problem.InvalidCursor, "")
		}
		before = &id
	}
	size := s.h.pageSize(req.Params.Limit)
	var rows []readq.ListTokensOfUserRow
	err := s.db.Installation(ctx, func(r *store.Reader) error {
		var err error
		params := readq.ListTokensOfUserParams{UserID: p.PersonID, Before: before, PageSize: limitArg(size)}
		if restricted(p) {
			params.OnlyID = &p.TokenID
		}
		rows, err = r.ListTokensOfUser(ctx, params)
		return err
	})
	if err != nil {
		return nil, err
	}
	rows, next := page(s.h, rows, size, op, scope, func(t readq.ListTokensOfUserRow) string { return t.ID.String() })
	out := apigen.ListMyTokens200JSONResponse{Items: []apigen.Token{}, NextCursor: nullableString(next)}
	now := s.h.opts.Now()
	for _, t := range rows {
		out.Items = append(out.Items, tokenView(t, now))
	}
	return out, nil
}

func tokenView(t readq.ListTokensOfUserRow, now time.Time) apigen.Token {
	state := apigen.TokenStateActive
	switch {
	case t.RevokedAt != nil:
		state = apigen.TokenStateRevoked
	case !t.ExpiresAt.After(now):
		state = apigen.TokenStateExpired
	}
	caps := make([]apigen.Capability, 0, len(t.Capabilities))
	for _, c := range t.Capabilities {
		caps = append(caps, apigen.Capability(c))
	}
	v := apigen.Token{
		Id:           t.ID,
		Name:         t.Name,
		Scope:        apigen.Scope(t.Scope),
		Agent:        t.Agent,
		Capabilities: caps,
		CreatedAt:    t.CreatedAt,
		ExpiresAt:    t.ExpiresAt,
		State:        state,
	}
	v.RestrictedTenant = nullableOf(t.RestrictedTenantSlug)
	if t.RestrictedProjectID != nil {
		id := *t.RestrictedProjectID
		v.RestrictedProjectId = nullableOf(&id)
	}
	if t.LastUsedOn != nil {
		v.LastUsedOn = nullableOf(&openapi_types.Date{Time: *t.LastUsedOn})
	}
	v.RevokedAt = nullableOf(t.RevokedAt)
	return v
}

// RevokeMyToken revokes one of the person's tokens: immediately, recorded,
// the row kept (docs/adr/0035 D6, D9). A token may always revoke itself;
// another token of the person needs write scope, and an agent may revoke
// only the token it holds (token administration is on the hard-off list,
// docs/adr/0043 D3). A restricted token knows no token but itself.
func (s *Server) RevokeMyToken(ctx context.Context, req apigen.RevokeMyTokenRequestObject) (apigen.RevokeMyTokenResponseObject, error) {
	p := principal(ctx)
	target := req.TokenId
	if target != p.TokenID && restricted(p) {
		return nil, problem.New(problem.NotFound, "no such token")
	}
	if target != p.TokenID {
		if perr := auth.Authorize(p, "", auth.Need{Scope: domain.ScopeWrite, HardOff: auth.HardOffTokens}); perr != nil {
			return nil, perr
		}
	}
	_, err := s.db.Mutate(ctx, uuid.Nil, func(w *store.Writer) error {
		tok, err := w.GetTokenOfUser(ctx, readq.GetTokenOfUserParams{TokenID: target, UserID: p.PersonID})
		if errors.Is(err, pgx.ErrNoRows) {
			return problem.New(problem.NotFound, "no such token")
		}
		if err != nil {
			return err
		}
		if tok.RevokedAt != nil {
			return store.ErrNoChange
		}
		_, err = w.RevokeToken(ctx, writeq.RevokeTokenParams{TokenID: target, UserID: p.PersonID, RevokedBy: &p.PersonID})
		if errors.Is(err, pgx.ErrNoRows) {
			// A simultaneous revocation came first.
			return store.ErrNoChange
		}
		if err != nil {
			return err
		}
		w.Record(store.Event{EntityType: "token", EntityID: target, Action: "revoked"})
		return nil
	})
	if err != nil && !errors.Is(err, store.ErrNoChange) {
		return nil, err
	}
	return apigen.RevokeMyToken204Response{}, nil
}
