package api

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/problem"
	"github.com/guided-traffic/cowork/backend/internal/store"
	"github.com/guided-traffic/cowork/backend/internal/store/readq"
)

// GetMyToken answers the token the request presents and what it makes of the
// request (docs/adr/0043 D6, docs/adr/0070 D2): the token as the list shows
// it, its project restriction by key among it, and the request's agent mark
// and capabilities. A browser session presents no token.
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
	keys, err := s.projectKeys(ctx, p.PersonID, []readq.ListTokensOfUserRow{tok})
	if err != nil {
		return nil, err
	}
	listed := tokenView(tok, s.h.opts.Now(), keys)
	out := apigen.GetMyToken200JSONResponse{
		Id: listed.Id, Name: listed.Name, Scope: listed.Scope, Agent: listed.Agent, Capabilities: listed.Capabilities,
		RestrictedTenant: listed.RestrictedTenant, RestrictedProject: listed.RestrictedProject,
		RestrictedProjectId: listed.RestrictedProjectId, //nolint:staticcheck // SA1019: deprecated in the document, kept in /api/v1 for the clients that read it
		CreatedAt:           listed.CreatedAt, ExpiresAt: listed.ExpiresAt, LastUsedOn: listed.LastUsedOn,
		RevokedAt: listed.RevokedAt, State: listed.State,
		Request: apigen.RequestMark{Agent: p.IsAgent(), AgentMark: nullableString(nil), Capabilities: []apigen.Capability{}},
	}
	if p.IsAgent() {
		out.Request.AgentMark = nullableOf(&p.Agent)
		out.Request.Capabilities = requestCapabilities(p.Capabilities)
	}
	return out, nil
}

// capabilitiesView is a capability set as the API answers it, under this
// release's names: a set stored before with override-urgency shows
// set-horizon (docs/adr/0043 D4 as amended 2026-10-05).
func capabilitiesView(caps []string) []apigen.Capability {
	canonical := auth.Canonical(caps)
	out := make([]apigen.Capability, 0, len(canonical))
	for _, c := range canonical {
		out = append(out, apigen.Capability(c))
	}
	return out
}

// requestCapabilities is the set an agent's request holds as /me/token
// answers it: set-horizon is followed by override-urgency, its name before,
// which a cowork-mcp of the release before looks for in this list to describe
// its tools — taking the name away would tell its model it lacks an act the
// API grants (docs/adr/0046 D7). A later release drops the old name here.
func requestCapabilities(caps []string) []apigen.Capability {
	out := []apigen.Capability{}
	for _, c := range capabilitiesView(caps) {
		out = append(out, c)
		if c == apigen.CapabilitySetHorizon {
			out = append(out, apigen.CapabilityOverrideUrgency)
		}
	}
	return out
}
