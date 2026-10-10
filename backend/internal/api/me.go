package api

import (
	"context"
	"errors"
	"slices"
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
		out = apigen.Me{Id: u.ID, Username: nullableOf(u.Username), DisplayName: u.DisplayName, GlobalAdmin: u.GlobalAdmin,
			Local: u.Local, PasswordChangeRequired: u.PasswordChangeRequired, Memberships: []apigen.Membership{}}
		for _, m := range memberships {
			if restricted(p) && m.TenantID != p.RestrictedTenantID {
				continue
			}
			team := apigen.TeamRef{Slug: m.Slug, Name: m.Name}
			out.Memberships = append(out.Memberships, apigen.Membership{
				Team:    team,
				Tenant:  team, //nolint:staticcheck // SA1019: deprecated in the document, answered beside team until a later release removes it
				Role:    apigen.Role(m.Role),
				Origins: originsOf(m.Sources, m.Roles),
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return apigen.GetMe200JSONResponse(out), nil
}

// ListMyTokens lists the person's tokens, newest first, by cursor or numbered
// pages (docs/adr/0048 D2): metadata only (docs/adr/0035 D1, D6); a
// restricted token lists itself only.
func (s *Server) ListMyTokens(ctx context.Context, req apigen.ListMyTokensRequestObject) (apigen.ListMyTokensResponseObject, error) {
	p := principal(ctx)
	const op = "listMyTokens"
	scope := p.PersonID.String()
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
	params := readq.ListTokensOfUserParams{UserID: p.PersonID, Before: before, PageSize: lp.limit(), PageOffset: lp.offset()}
	if restricted(p) {
		params.OnlyID = &p.TokenID
	}
	var rows []readq.ListTokensOfUserRow
	var total int64
	err := s.db.Installation(ctx, func(r *store.Reader) error {
		var err error
		if rows, err = r.ListTokensOfUser(ctx, params); err != nil || !lp.numbered {
			return err
		}
		total, err = r.CountTokensOfUser(ctx, readq.CountTokensOfUserParams{UserID: p.PersonID, OnlyID: params.OnlyID})
		return err
	})
	if err != nil {
		return nil, err
	}
	var next *string
	if !lp.numbered {
		rows, next = page(s.h, rows, lp.size, op, scope, func(t readq.ListTokensOfUserRow) string { return t.ID.String() })
	}
	keys, err := s.projectKeys(ctx, p.PersonID, rows)
	if err != nil {
		return nil, err
	}
	out := apigen.ListMyTokens200JSONResponse{Items: []apigen.Token{}, NextCursor: nullableString(next)}
	out.Total, out.Page, out.PerPage = lp.numbers(total)
	now := s.h.opts.Now()
	for _, t := range rows {
		out.Items = append(out.Items, tokenView(t, now, keys))
	}
	return out, nil
}

// projectKeys names the projects the tokens are restricted to by their keys
// (docs/adr/0035 D3), each tenant read in its own transaction under the
// project predicate: a project the person no longer sees, or of a tenant they
// no longer belong to, has no key, and the token reaches nothing.
func (s *Server) projectKeys(ctx context.Context, person uuid.UUID, tokens []readq.ListTokensOfUserRow) (map[uuid.UUID]string, error) {
	byTenant := map[uuid.UUID][]uuid.UUID{}
	for _, t := range tokens {
		if t.RestrictedTenantID != nil && t.RestrictedProjectID != nil {
			byTenant[*t.RestrictedTenantID] = append(byTenant[*t.RestrictedTenantID], *t.RestrictedProjectID)
		}
	}
	keys := map[uuid.UUID]string{}
	if len(byTenant) == 0 {
		return keys, nil
	}
	var memberships []readq.ListMembershipsOfUserRow
	err := s.db.Installation(ctx, func(r *store.Reader) error {
		var err error
		memberships, err = r.ListMembershipsOfUser(ctx, person)
		return err
	})
	if err != nil {
		return nil, err
	}
	for _, m := range memberships {
		ids, ok := byTenant[m.TenantID]
		if !ok {
			continue
		}
		err := s.db.InTenant(ctx, m.TenantID, func(r *store.Reader) error {
			rows, err := r.ListVisibleProjectKeys(ctx, readq.ListVisibleProjectKeysParams{TenantID: m.TenantID, Ids: ids})
			for _, row := range rows {
				keys[row.ID] = row.Key
			}
			return err
		})
		if err != nil {
			return nil, err
		}
	}
	return keys, nil
}

// tokenView is a token as the list shows it; keys names the projects of the
// restrictions the person still sees (projectKeys).
func tokenView(t readq.ListTokensOfUserRow, now time.Time, keys map[uuid.UUID]string) apigen.Token {
	state := apigen.TokenStateActive
	switch {
	case t.RevokedAt != nil:
		state = apigen.TokenStateRevoked
	case !t.ExpiresAt.After(now):
		state = apigen.TokenStateExpired
	}
	v := apigen.Token{
		Id:           t.ID,
		Name:         t.Name,
		Scope:        apigen.Scope(t.Scope),
		Agent:        t.Agent,
		Capabilities: capabilitiesView(t.Capabilities),
		CreatedAt:    t.CreatedAt,
		ExpiresAt:    t.ExpiresAt,
		State:        state,
	}
	v.RestrictedTeam = nullableOf(t.RestrictedTenantSlug)
	v.RestrictedTenant = nullableOf(t.RestrictedTenantSlug) //nolint:staticcheck // SA1019: deprecated in the document, answered beside restricted_team until a later release removes it
	v.RestrictedProject = nullableOf[string](nil)
	if t.RestrictedProjectID != nil {
		id := *t.RestrictedProjectID
		v.RestrictedProjectId = nullableOf(&id) //nolint:staticcheck // SA1019: deprecated in the document, kept in /api/v1 for the clients that read it
		if key, ok := keys[id]; ok {
			v.RestrictedProject = nullableOf(&key)
		}
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
		w.Record(store.Event{EntityType: entityToken, EntityID: target, Action: actionRevoked})
		return nil
	})
	if err != nil && !errors.Is(err, store.ErrNoChange) {
		return nil, err
	}
	return apigen.RevokeMyToken204Response{}, nil
}

// CreateMyToken creates a personal access token for the calling person, who is
// in a browser session: the pipeline has already refused a token (docs/adr/0035
// D5). The plaintext is in this answer and nowhere else — the database keeps
// its SHA-256 — and a replay for an Idempotency-Key answers without it
// (docs/adr/0045 D6).
func (s *Server) CreateMyToken(ctx context.Context, req apigen.CreateMyTokenRequestObject) (apigen.CreateMyTokenResponseObject, error) {
	p := principal(ctx)
	body := *req.Body
	spec, perr := s.tokenSpec(ctx, p, body)
	if perr != nil {
		return nil, perr
	}
	ctx, perr = s.keyed(ctx, req.Params.IdempotencyKey, "createMyToken", p.PersonID.String(), body)
	if perr != nil {
		return nil, perr
	}
	plaintext, hash, err := auth.GenerateToken()
	if err != nil {
		return nil, err
	}
	var view apigen.TokenCreated
	replay, err := s.db.Mutate(ctx, uuid.Nil, func(w *store.Writer) error {
		row, err := w.InsertToken(ctx, writeq.InsertTokenParams{
			UserID: p.PersonID, Name: spec.name, TokenHash: hash[:], Scope: spec.scope,
			RestrictedTenantID: spec.tenantID, RestrictedProjectID: spec.projectID,
			Agent: spec.agent, Capabilities: spec.capabilities, ExpiresAt: spec.expiresAt,
		})
		if err != nil {
			return err
		}
		view = spec.view(row.ID, row.CreatedAt)
		w.Record(store.Event{EntityType: entityToken, EntityID: row.ID, Action: actionCreated, After: map[string]any{
			fieldName: spec.name, "scope": spec.scope, "agent": spec.agent, "capabilities": spec.capabilities,
			"restricted_tenant": spec.tenantSlug, "restricted_project": spec.projectKey, "expires_at": spec.expiresAt,
		}})
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
		body, err := replayed[apigen.TokenCreated](replay)
		if err != nil {
			return nil, err
		}
		return apigen.CreateMyToken201JSONResponse(body), nil
	}
	view.Token = &plaintext
	return apigen.CreateMyToken201JSONResponse(view), nil
}

// tokenSpec is a validated request to create a token, with its restriction
// resolved to ids.
type tokenSpec struct {
	name         string
	scope        domain.Scope
	agent        bool
	capabilities []string
	tenantID     *uuid.UUID
	tenantSlug   *string
	projectID    *uuid.UUID
	projectKey   *string
	expiresAt    time.Time
}

func (t tokenSpec) view(id uuid.UUID, createdAt time.Time) apigen.TokenCreated {
	v := apigen.TokenCreated{Id: id, Name: t.name, Scope: apigen.Scope(t.scope), Agent: t.agent, Capabilities: capabilitiesView(t.capabilities),
		CreatedAt: createdAt, ExpiresAt: t.expiresAt, State: apigen.TokenStateActive}
	v.RestrictedTeam = nullableOf(t.tenantSlug)
	v.RestrictedTenant = nullableOf(t.tenantSlug) //nolint:staticcheck // SA1019: deprecated in the document, answered beside restricted_team until a later release removes it
	v.RestrictedProject = nullableOf(t.projectKey)
	v.RestrictedProjectId = nullableOf(t.projectID) //nolint:staticcheck // SA1019: deprecated in the document, kept in /api/v1 for the clients that read it
	v.RevokedAt = nullableOf[time.Time](nil)
	return v
}

// teamRestriction is the team a token is to be restricted to, by the pointer
// of the property that named it: team, or tenant, the name it had before
// (docs/adr/0005 D1), taken until a later release removes it
// (docs/adr/0046 D7). The two are one restriction, so a request that names two
// different slugs is refused at /team rather than one of them picked.
func teamRestriction(body apigen.CreateMyTokenJSONRequestBody) (*string, string, *problem.Error) {
	old := body.Tenant //nolint:staticcheck // SA1019: deprecated in the document, taken as team until a later release removes it
	switch {
	case body.Team != nil && old != nil && *body.Team != *old:
		return nil, "", problem.Field("/team", "tenant is the deprecated name of team: send team alone, or the same slug in both")
	case body.Team != nil:
		return body.Team, "/team", nil
	case old != nil:
		return old, "/tenant", nil
	}
	return nil, "", nil
}

// tokenCapabilities are the capabilities a creation request names, each once,
// and those the token carries (docs/adr/0043 D4): none for a plain token; for
// an agent token the list it names — an empty one too: the nine switches all
// off leave the baseline —, and every capability where it names no list.
func tokenCapabilities(agent bool, requested *[]apigen.Capability) (named, carried []string) {
	named = []string{}
	if requested != nil {
		for _, c := range *requested {
			named = append(named, string(c))
		}
		named = auth.Canonical(named)
	}
	switch {
	case !agent:
		return named, []string{}
	case requested != nil:
		return named, named
	}
	return named, slices.Clone(auth.AllCapabilities)
}

// tokenSpec validates a creation request against the token rules and the
// person's own reach (docs/adr/0035 D3, D4, docs/adr/0036 D5, docs/adr/0043 D4):
// an agent token has at most write scope and carries the capabilities, all of
// them unless the request names some; a plain token carries none; the lifetime
// is the default, shortened to the maximum; and a restriction names a team the
// person belongs to and a project of it the person sees — a team or a project
// the person cannot reach is "no such", whichever it is.
func (s *Server) tokenSpec(ctx context.Context, p auth.Principal, body apigen.CreateMyTokenJSONRequestBody) (tokenSpec, *problem.Error) {
	spec := tokenSpec{name: strings.TrimSpace(body.Name), scope: domain.Scope(body.Scope), agent: body.Agent != nil && *body.Agent}
	if spec.name == "" {
		return spec, problem.Field("/name", "must not be blank")
	}
	team, pointer, perr := teamRestriction(body)
	if perr != nil {
		return spec, perr
	}
	named, carried := tokenCapabilities(spec.agent, body.Capabilities)
	switch {
	case spec.agent && spec.scope == domain.ScopeAdmin:
		return spec, problem.Field("/scope", "an agent token has at most write scope (docs/adr/0036 D5)")
	case !spec.agent && len(named) > 0:
		return spec, problem.Field("/capabilities", "only an agent token carries capabilities")
	case body.Project != nil && team == nil:
		return spec, problem.Field("/project", "a project restriction needs the team restriction")
	}
	spec.capabilities = carried
	lifetime := s.h.opts.TokenDefaultLifetime
	if body.LifetimeDays != nil {
		lifetime = time.Duration(*body.LifetimeDays) * 24 * time.Hour
	}
	spec.expiresAt = s.h.opts.Now().UTC().Add(min(lifetime, s.h.opts.TokenMaxLifetime))
	if team == nil {
		return spec, nil
	}
	return spec, s.restrictTo(ctx, p, &spec, *team, pointer, body.Project)
}

// restrictTo resolves the team and project of a restriction through the
// person's own reach: their membership, and the project predicate of the
// team (docs/adr/0034 D3). pointer is the property that named the team.
func (s *Server) restrictTo(ctx context.Context, p auth.Principal, spec *tokenSpec, slug, pointer string, project *string) *problem.Error {
	var tenant readq.GetTenantForPersonRow
	err := s.db.Installation(ctx, func(r *store.Reader) error {
		var err error
		tenant, err = r.GetTenantForPerson(ctx, readq.GetTenantForPersonParams{Slug: slug, UserID: p.PersonID})
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return problem.Field(pointer, "no such team")
	}
	if err != nil {
		return problem.New(problem.Internal, "internal error")
	}
	spec.tenantID, spec.tenantSlug = &tenant.ID, &slug
	if project == nil {
		return nil
	}
	var row readq.GetProjectByKeyRow
	err = s.db.InTenant(ctx, tenant.ID, func(r *store.Reader) error {
		var err error
		row, err = r.GetProjectByKey(ctx, readq.GetProjectByKeyParams{TenantID: tenant.ID, Key: *project})
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return problem.Field("/project", "no such project")
	}
	if err != nil {
		return problem.New(problem.Internal, "internal error")
	}
	spec.projectID, spec.projectKey = &row.ID, project
	return nil
}

// ChangeMyPassword changes the password of the calling person's local account
// (docs/adr/0033 D3, D4). The current password is verified like a login's —
// held to the address throttle before it is hashed, and a wrong one counts
// towards the account's lockout, so a stolen session cannot guess it at the
// hash's speed — the new one meets the length policy and differs, every other
// session of the account ends, and a temporary password stops being one. The
// password of the local administrator is the configuration's, and is not
// changed here.
func (s *Server) ChangeMyPassword(ctx context.Context, req apigen.ChangeMyPasswordRequestObject) (apigen.ChangeMyPasswordResponseObject, error) {
	p := principal(ctx)
	body := *req.Body
	person, stored, perr, err := s.ownAccount(ctx, p.PersonID)
	if perr != nil || err != nil {
		return nil, firstOf(perr, err)
	}
	if err := auth.CheckPassword(body.NewPassword, s.h.opts.PasswordMinLength); err != nil {
		return nil, problem.Field("/new_password", strings.TrimPrefix(err.Error(), auth.ErrPasswordLength.Error()+": "))
	}
	address := s.h.addressHash(clientFrom(ctx).Client)
	reserved, err := s.reserveAttempt(ctx, *person.Username, address, s.h.opts.Now(), attemptPasswordChange)
	if err != nil {
		return nil, err
	}
	matched, err := s.passwordFits(ctx, body.CurrentPassword, stored.PasswordHash)
	if err != nil {
		return nil, err
	}
	acc := store.LoginAccount{Found: true, UserID: p.PersonID, Hash: stored.PasswordHash, Deactivated: person.DeactivatedAt != nil}
	attempt := s.attempt(ctx, *person.Username, acc, matched, address, attemptPasswordChange)
	attempt.Reserved = reserved
	outcome, err := s.db.RecordLoginAttempt(ctx, attempt)
	if err != nil {
		return nil, err
	}
	if outcome != store.LoginSucceeded {
		return nil, problem.Field("/current_password", "the current password is wrong")
	}
	if body.NewPassword == body.CurrentPassword {
		return nil, problem.Field("/new_password", "must differ from the current password")
	}
	hash, err := auth.HashPassword(ctx, body.NewPassword)
	if err != nil {
		return nil, err
	}
	_, err = s.db.Mutate(ctx, uuid.Nil, func(w *store.Writer) error {
		if _, err := w.SetAccountPassword(ctx, writeq.SetAccountPasswordParams{PasswordHash: hash, UserID: p.PersonID}); err != nil {
			return err
		}
		ended, err := w.DeleteOtherSessionsOfUser(ctx, writeq.DeleteOtherSessionsOfUserParams{UserID: p.PersonID, KeepHash: p.SessionHash})
		if err != nil {
			return err
		}
		w.Record(store.Event{EntityType: entityUser, EntityID: p.PersonID, Action: "password_changed",
			After: map[string]any{fieldSessionsEnded: ended}})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return apigen.ChangeMyPassword204Response{}, nil
}

// ownAccount reads the calling person and their local account, and refuses a
// person without one and the account the configuration keeps. The first error
// is a problem to answer, the second a failure.
func (s *Server) ownAccount(ctx context.Context, personID uuid.UUID) (readq.GetUserRow, readq.GetOwnLocalAccountRow, *problem.Error, error) {
	var (
		person  readq.GetUserRow
		stored  readq.GetOwnLocalAccountRow
		haveOwn = true
	)
	err := s.db.Installation(ctx, func(r *store.Reader) error {
		var err error
		if person, err = r.GetUser(ctx, personID); err != nil {
			return err
		}
		stored, err = r.GetOwnLocalAccount(ctx, personID)
		if errors.Is(err, pgx.ErrNoRows) {
			haveOwn = false
			return nil
		}
		return err
	})
	switch {
	case err != nil:
		return person, stored, nil, err
	case !haveOwn || person.Username == nil:
		return person, stored, problem.New(problem.Forbidden, "this person has no local account, so no password to change"), nil
	case stored.Origin == "config":
		return person, stored, problem.New(problem.Forbidden, "the password of this account is set by the configuration, COWORK_LOCAL_ADMIN_PASSWORD"), nil
	}
	return person, stored, nil, nil
}

// firstOf returns the problem when there is one, else the failure.
func firstOf(perr *problem.Error, err error) error {
	if perr != nil {
		return perr
	}
	return err
}
