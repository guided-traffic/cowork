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
		w.Record(store.Event{EntityType: entityToken, EntityID: target, Action: "revoked"})
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
	ctx, perr = keyed(ctx, req.Params.IdempotencyKey, "createMyToken", p.PersonID.String(), body)
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
	caps := make([]apigen.Capability, 0, len(t.capabilities))
	for _, c := range t.capabilities {
		caps = append(caps, apigen.Capability(c))
	}
	v := apigen.TokenCreated{Id: id, Name: t.name, Scope: apigen.Scope(t.scope), Agent: t.agent, Capabilities: caps,
		CreatedAt: createdAt, ExpiresAt: t.expiresAt, State: apigen.TokenStateActive}
	v.RestrictedTenant = nullableOf(t.tenantSlug)
	v.RestrictedProjectId = nullableOf(t.projectID)
	v.RevokedAt = nullableOf[time.Time](nil)
	return v
}

// tokenSpec validates a creation request against the token rules and the
// person's own reach (docs/adr/0035 D3, D4, docs/adr/0036 D5, docs/adr/0043 D4):
// an agent token has at most write scope and carries the capabilities, all of
// them unless the request names some; a plain token carries none; the lifetime
// is the default, shortened to the maximum; and a restriction names a tenant the
// person belongs to and a project of it the person sees — a tenant or a project
// the person cannot reach is "no such", whichever it is.
func (s *Server) tokenSpec(ctx context.Context, p auth.Principal, body apigen.CreateMyTokenJSONRequestBody) (tokenSpec, *problem.Error) {
	spec := tokenSpec{name: strings.TrimSpace(body.Name), scope: domain.Scope(body.Scope), agent: body.Agent != nil && *body.Agent}
	if spec.name == "" {
		return spec, problem.Field("/name", "must not be blank")
	}
	named := []string{}
	if body.Capabilities != nil {
		for _, c := range *body.Capabilities {
			named = append(named, string(c))
		}
	}
	switch {
	case spec.agent && spec.scope == domain.ScopeAdmin:
		return spec, problem.Field("/scope", "an agent token has at most write scope (docs/adr/0036 D5)")
	case !spec.agent && len(named) > 0:
		return spec, problem.Field("/capabilities", "only an agent token carries capabilities")
	case body.Project != nil && body.Tenant == nil:
		return spec, problem.Field("/project", "a project restriction needs the tenant restriction")
	}
	switch {
	case !spec.agent:
		spec.capabilities = []string{}
	case body.Capabilities != nil:
		// A list is the capabilities, also an empty one: the nine switches all
		// off leave the baseline (docs/adr/0043 D4). Only a list left out is
		// every capability.
		spec.capabilities = named
	default:
		spec.capabilities = slices.Clone(auth.AllCapabilities)
	}
	lifetime := s.h.opts.TokenDefaultLifetime
	if body.LifetimeDays != nil {
		lifetime = time.Duration(*body.LifetimeDays) * 24 * time.Hour
	}
	spec.expiresAt = s.h.opts.Now().UTC().Add(min(lifetime, s.h.opts.TokenMaxLifetime))
	if body.Tenant == nil {
		return spec, nil
	}
	return spec, s.restrictTo(ctx, p, &spec, *body.Tenant, body.Project)
}

// restrictTo resolves the tenant and project of a restriction through the
// person's own reach: their membership, and the project predicate of the
// tenant (docs/adr/0034 D3).
func (s *Server) restrictTo(ctx context.Context, p auth.Principal, spec *tokenSpec, slug string, project *string) *problem.Error {
	var tenant readq.GetTenantForPersonRow
	err := s.db.Installation(ctx, func(r *store.Reader) error {
		var err error
		tenant, err = r.GetTenantForPerson(ctx, readq.GetTenantForPersonParams{Slug: slug, UserID: p.PersonID})
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return problem.Field("/tenant", "no such tenant")
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
// a wrong one counts towards the account's lockout, so a stolen session cannot
// guess it — the new one meets the length policy and differs, every other
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
	matched, err := s.passwordFits(ctx, body.CurrentPassword, stored.PasswordHash)
	if err != nil {
		return nil, err
	}
	acc := store.LoginAccount{Found: true, UserID: p.PersonID, Hash: stored.PasswordHash, Deactivated: person.DeactivatedAt != nil}
	outcome, err := s.db.RecordLoginAttempt(ctx, s.attempt(ctx, *person.Username, acc, matched, s.h.addressHash(clientFrom(ctx).Client), "password change"))
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
