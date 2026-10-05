package api

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/problem"
	"github.com/guided-traffic/cowork/backend/internal/store"
	"github.com/guided-traffic/cowork/backend/internal/store/readq"
	"github.com/guided-traffic/cowork/backend/internal/store/writeq"
)

// originTenant is the origin of a local account a tenant's administrator
// created (docs/adr/0033 D1); the other is the configuration's.
const originTenant = "tenant"

// An account is a person across the whole installation: its password and its
// sessions are not a tenant's. A tenant therefore manages the accounts its own
// administrators created and no others, and an account it does not manage — one
// of another tenant, a global administrator, the local administrator — is
// "no such account", which the tenant cannot tell from a username nobody has.
// Row-level security says the same in the policies of the tables
// (migration 15); the handlers state it first.

// accountView is the account as the administrators of its tenant see it.
func accountView(id uuid.UUID, username *string, displayName string, role string, changeRequired, locked bool,
	deactivatedAt *time.Time, createdAt time.Time) apigen.Account {
	v := apigen.Account{Id: id, Username: deref(username), DisplayName: displayName,
		PasswordChangeRequired: changeRequired, Locked: locked, CreatedAt: createdAt}
	v.DeactivatedAt = nullableOf(deactivatedAt)
	if role == "" {
		v.Role = nullableOf[apigen.Role](nil)
	} else {
		r := apigen.Role(role)
		v.Role = nullableOf(&r)
	}
	return v
}

// ListAccounts lists the local accounts the tenant manages, by person id
// (docs/adr/0033 D1, D5).
func (s *Server) ListAccounts(ctx context.Context, req apigen.ListAccountsRequestObject) (apigen.ListAccountsResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, administer); perr != nil {
		return nil, perr
	}
	const op = "listAccounts"
	scope := t.ID.String()
	after, perr := s.uuidCursor(op, scope, req.Params.Cursor)
	if perr != nil {
		return nil, perr
	}
	size := s.h.pageSize(req.Params.Limit)
	var rows []readq.ListManagedAccountsRow
	err := s.db.InTenant(ctx, t.ID, func(r *store.Reader) error {
		var err error
		rows, err = r.ListManagedAccounts(ctx, readq.ListManagedAccountsParams{
			TenantID: t.ID, LockSince: s.h.opts.Now().Add(-store.LoginWindow), After: after, PageSize: limitArg(size)})
		return err
	})
	if err != nil {
		return nil, err
	}
	rows, next := page(s.h, rows, size, op, scope, func(a readq.ListManagedAccountsRow) string { return a.ID.String() })
	out := apigen.ListAccounts200JSONResponse{Items: []apigen.Account{}, NextCursor: nullableString(next)}
	for _, a := range rows {
		out.Items = append(out.Items, accountView(a.ID, a.Username, a.DisplayName, a.Role, a.PasswordChangeRequired, a.Locked,
			a.DeactivatedAt, a.CreatedAt))
	}
	return out, nil
}

// CreateAccount creates a local account with a temporary password, a marked
// grant in the tenant and this tenant as its manager (docs/adr/0033 D1, D4,
// docs/adr/0030 D3). The creation is the gate for local accounts and a
// recorded act; the password is in no row, no log and no error text.
func (s *Server) CreateAccount(ctx context.Context, req apigen.CreateAccountRequestObject) (apigen.CreateAccountResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, administer); perr != nil {
		return nil, perr
	}
	body := *req.Body
	displayName := strings.TrimSpace(body.DisplayName)
	if displayName == "" {
		return nil, problem.Field("/display_name", "must not be blank")
	}
	if perr := s.checkTemporaryPassword("/temporary_password", body.TemporaryPassword); perr != nil {
		return nil, perr
	}
	hash, err := auth.HashPassword(ctx, body.TemporaryPassword)
	if err != nil {
		return nil, err
	}
	ctx, perr := s.keyed(ctx, req.Params.IdempotencyKey, "createAccount", t.ID.String(), body)
	if perr != nil {
		return nil, perr
	}
	account := newAccount{username: body.Username, displayName: displayName, hash: hash, role: domain.Role(body.Role)}
	if account.userID, err = uuid.NewV7(); err != nil {
		return nil, err
	}
	if account.grantID, err = uuid.NewV7(); err != nil {
		return nil, err
	}
	var view apigen.Account
	replay, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		if err := account.insert(ctx, w, t); err != nil {
			return err
		}
		view = accountView(account.userID, &account.username, displayName, string(account.role), true, false, nil, s.h.opts.Now().UTC())
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
		replayedView, err := replayed[apigen.Account](replay)
		if err != nil {
			return nil, err
		}
		return apigen.CreateAccount201JSONResponse(replayedView), nil
	}
	return apigen.CreateAccount201JSONResponse(view), nil
}

// newAccount is what creating a local account writes: the person, the account,
// the grant.
type newAccount struct {
	userID, grantID uuid.UUID
	username        string
	displayName     string
	hash            string
	role            domain.Role
}

// insert writes the account's rows and the acts that record them. A username
// that exists is 409 — the installation has one namespace, so it also tells an
// administrator that another tenant has the name.
func (a newAccount) insert(ctx context.Context, w *store.Writer, t tenantScope) error {
	username, managing := a.username, t.ID
	if err := w.InsertUser(ctx, writeq.InsertUserParams{ID: a.userID, Username: &username, DisplayName: a.displayName}); err != nil {
		if isUnique(err, "users_username_key") {
			return &problem.Error{Code: problem.UsernameTaken, Detail: "the installation has a person with this username",
				Errors: []problem.FieldError{{Pointer: "/username", Message: messageTaken}}}
		}
		return err
	}
	if err := w.InsertLocalAccount(ctx, writeq.InsertLocalAccountParams{UserID: a.userID, PasswordHash: a.hash,
		PasswordChangeRequired: true, Origin: originTenant, ManagingTenantID: &managing}); err != nil {
		return err
	}
	if err := w.InsertGrant(ctx, writeq.InsertGrantParams{ID: a.grantID, TenantID: t.ID, UserID: a.userID, Role: a.role}); err != nil {
		return err
	}
	// Failures someone caused against this name before the account existed are
	// not the account's.
	if _, err := w.DeleteFailedLoginAttempts(ctx, username); err != nil {
		return err
	}
	if _, err := w.DeleteLoginLock(ctx, username); err != nil {
		return err
	}
	w.Record(store.Event{EntityType: entityUser, EntityID: a.userID, Action: actionCreated, After: map[string]any{
		"username": username, fieldName: a.displayName, "origin": originTenant}})
	w.Record(store.Event{EntityType: entityMembership, EntityID: a.grantID, Action: actionCreated, After: map[string]any{
		"user": a.userID, "role": a.role, fieldSource: sourceGrant}})
	return nil
}

func (s *Server) checkTemporaryPassword(pointer, password string) *problem.Error {
	if err := auth.CheckPassword(password, s.h.opts.PasswordMinLength); err != nil {
		return problem.Field(pointer, strings.TrimPrefix(err.Error(), auth.ErrPasswordLength.Error()+": "))
	}
	return nil
}

// isUnique reports a violation of the named unique constraint.
func isUnique(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == constraint
}

// managedAccount finds the account a tenant manages by username, or answers
// "no such account".
func managedAccount(ctx context.Context, w *store.Writer, t tenantScope, username string) (readq.GetManagedAccountRow, error) {
	row, err := w.GetManagedAccount(ctx, readq.GetManagedAccountParams{Username: &username, TenantID: &t.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return row, problem.New(problem.NotFound, "no such account")
	}
	return row, err
}

// notOwn refuses an administrator's act on their own account where it would
// get round a check the account's own route makes: a password reset would
// change the password without the current one a stolen session lacks, an unlock
// would lift the lock on guessing, a deactivation would lock the administrator
// out for good.
func notOwn(p auth.Principal, target uuid.UUID, route string) *problem.Error {
	if p.PersonID != target {
		return nil
	}
	return problem.New(problem.Forbidden, "not for your own account: "+route)
}

// ResetAccountPassword sets a new temporary password; its person changes it at
// the next login, and every session of the account ends (docs/adr/0033 D5,
// docs/adr/0031 D4).
func (s *Server) ResetAccountPassword(ctx context.Context, req apigen.ResetAccountPasswordRequestObject) (apigen.ResetAccountPasswordResponseObject, error) {
	t, p := tenantFrom(ctx), principal(ctx)
	if perr := auth.Authorize(p, t.Role, administer); perr != nil {
		return nil, perr
	}
	if perr := s.checkTemporaryPassword("/temporary_password", req.Body.TemporaryPassword); perr != nil {
		return nil, perr
	}
	hash, err := auth.HashPassword(ctx, req.Body.TemporaryPassword)
	if err != nil {
		return nil, err
	}
	_, err = s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		target, err := managedAccount(ctx, w, t, req.Username)
		if err != nil {
			return err
		}
		if perr := notOwn(p, target.ID, "change your password with PUT /api/v1/me/password"); perr != nil {
			return perr
		}
		if _, err := w.SetAccountPassword(ctx, writeq.SetAccountPasswordParams{PasswordHash: hash, PasswordChangeRequired: true, UserID: target.ID}); err != nil {
			return err
		}
		ended, err := w.DeleteSessionsOfUser(ctx, target.ID)
		if err != nil {
			return err
		}
		w.Record(store.Event{EntityType: entityUser, EntityID: target.ID, Action: "password_reset",
			After: map[string]any{"password_change_required": true, fieldSessionsEnded: ended}})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return apigen.ResetAccountPassword204Response{}, nil
}

// UnlockAccount forgets the failures and the lock of the account's username
// (docs/adr/0033 D6).
func (s *Server) UnlockAccount(ctx context.Context, req apigen.UnlockAccountRequestObject) (apigen.UnlockAccountResponseObject, error) {
	t, p := tenantFrom(ctx), principal(ctx)
	if perr := auth.Authorize(p, t.Role, administer); perr != nil {
		return nil, perr
	}
	_, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		target, err := managedAccount(ctx, w, t, req.Username)
		if err != nil {
			return err
		}
		if perr := notOwn(p, target.ID, "ask another administrator"); perr != nil {
			return perr
		}
		failures, err := w.DeleteFailedLoginAttempts(ctx, req.Username)
		if err != nil {
			return err
		}
		locks, err := w.DeleteLoginLock(ctx, req.Username)
		if err != nil {
			return err
		}
		if failures+locks == 0 {
			return store.ErrNoChange
		}
		w.Record(store.Event{EntityType: entityUser, EntityID: target.ID, Action: "unlocked"})
		return nil
	})
	if err != nil && !errors.Is(err, store.ErrNoChange) {
		return nil, err
	}
	return apigen.UnlockAccount204Response{}, nil
}

// DeactivateAccount deactivates the account's person: no login, every token
// revoked, every session ended (docs/adr/0024 D5). The person, their
// memberships and everything they did stay. A deactivated person counts for no
// tenant as an administrator, so the deactivation is a change of who
// administers the managing tenant: it takes the tenant's lock first and is
// refused with last_admin when it would leave the tenant without an
// administrator who can log in (docs/adr/0034 D1) — two administrators who
// deactivate each other at once are decided one after the other. The other
// tenants the person administers are not asked: their rows are outside this
// tenant's transaction (docs/security/local-accounts.md H-32).
func (s *Server) DeactivateAccount(ctx context.Context, req apigen.DeactivateAccountRequestObject) (apigen.DeactivateAccountResponseObject, error) {
	t, p := tenantFrom(ctx), principal(ctx)
	if perr := auth.Authorize(p, t.Role, administer); perr != nil {
		return nil, perr
	}
	_, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		if err := w.LockTenant(ctx); err != nil {
			return err
		}
		target, err := managedAccount(ctx, w, t, req.Username)
		if err != nil {
			return err
		}
		if perr := notOwn(p, target.ID, "ask another administrator"); perr != nil {
			return perr
		}
		changed, err := w.DeactivateUser(ctx, target.ID)
		if err != nil {
			return err
		}
		if changed == 0 {
			return store.ErrNoChange
		}
		if err := s.lastAdmin(ctx, w, t.ID); err != nil {
			return err
		}
		revoked, err := w.RevokeTokensOfUser(ctx, writeq.RevokeTokensOfUserParams{UserID: target.ID, RevokedBy: &p.PersonID})
		if err != nil {
			return err
		}
		ended, err := w.DeleteSessionsOfUser(ctx, target.ID)
		if err != nil {
			return err
		}
		w.Record(store.Event{EntityType: entityUser, EntityID: target.ID, Action: "deactivated",
			After: map[string]any{"tokens_revoked": revoked, fieldSessionsEnded: ended}})
		return nil
	})
	if err != nil && !errors.Is(err, store.ErrNoChange) {
		return nil, err
	}
	return apigen.DeactivateAccount204Response{}, nil
}

// EndAccountSessions ends every session of the account, at once
// (docs/adr/0031 D4). It is allowed for the administrator's own account too: it
// is the one way an administrator signs themselves out everywhere.
func (s *Server) EndAccountSessions(ctx context.Context, req apigen.EndAccountSessionsRequestObject) (apigen.EndAccountSessionsResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, administer); perr != nil {
		return nil, perr
	}
	_, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		target, err := managedAccount(ctx, w, t, req.Username)
		if err != nil {
			return err
		}
		ended, err := w.DeleteSessionsOfUser(ctx, target.ID)
		if err != nil {
			return err
		}
		if ended == 0 {
			return store.ErrNoChange
		}
		w.Record(store.Event{EntityType: entityUser, EntityID: target.ID, Action: actionRevoked,
			After: map[string]any{fieldSessionsEnded: ended}})
		return nil
	})
	if err != nil && !errors.Is(err, store.ErrNoChange) {
		return nil, err
	}
	return apigen.EndAccountSessions204Response{}, nil
}
