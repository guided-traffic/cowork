package api

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/problem"
	"github.com/guided-traffic/cowork/backend/internal/store"
	"github.com/guided-traffic/cowork/backend/internal/store/readq"
)

// GetMyToken answers the token the request presents and what it makes of the
// request (docs/adr/0043 D6, docs/adr/0070 D2): the token as the list shows
// it, the key of its project restriction, and the request's agent mark and
// capabilities. A browser session presents no token.
func (s *Server) GetMyToken(ctx context.Context, _ apigen.GetMyTokenRequestObject) (apigen.GetMyTokenResponseObject, error) {
	p := principal(ctx)
	if p.TokenID == uuid.Nil {
		return nil, problem.New(problem.NotFound, "this request presents no token: it is a browser session's")
	}
	var tok readq.ListTokensOfUserRow
	err := s.db.Installation(ctx, func(r *store.Reader) error {
		rows, err := r.ListTokensOfUser(ctx, readq.ListTokensOfUserParams{UserID: p.PersonID, OnlyID: &p.TokenID, PageSize: 1})
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return pgx.ErrNoRows
		}
		tok = rows[0]
		return nil
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, problem.New(problem.NotFound, "no such token")
	}
	if err != nil {
		return nil, err
	}
	listed := tokenView(tok, s.h.opts.Now())
	out := apigen.GetMyToken200JSONResponse{
		Id: listed.Id, Name: listed.Name, Scope: listed.Scope, Agent: listed.Agent, Capabilities: listed.Capabilities,
		RestrictedTenant: listed.RestrictedTenant, RestrictedProjectId: listed.RestrictedProjectId,
		CreatedAt: listed.CreatedAt, ExpiresAt: listed.ExpiresAt, LastUsedOn: listed.LastUsedOn,
		RevokedAt: listed.RevokedAt, State: listed.State, RestrictedProject: nullableString(nil),
		Request: apigen.RequestMark{Agent: p.IsAgent(), AgentMark: nullableString(nil), Capabilities: []apigen.Capability{}},
	}
	if p.IsAgent() {
		out.Request.AgentMark = nullableOf(&p.Agent)
		for _, c := range p.Capabilities {
			out.Request.Capabilities = append(out.Request.Capabilities, apigen.Capability(c))
		}
	}
	if p.RestrictedProjectID != uuid.Nil {
		err := s.db.InTenant(ctx, p.RestrictedTenantID, func(r *store.Reader) error {
			row, err := r.GetVisibleProjectByID(ctx, readq.GetVisibleProjectByIDParams{TenantID: p.RestrictedTenantID, ID: p.RestrictedProjectID})
			if err == nil {
				out.RestrictedProject = nullableOf(&row.Key)
			}
			if errors.Is(err, pgx.ErrNoRows) {
				// The person no longer sees the project; the token reaches nothing.
				return nil
			}
			return err
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
