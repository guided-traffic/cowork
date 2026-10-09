// Package bootstrap keeps what the configuration says an installation starts
// with: the one local administrator and, while no tenant exists, the bootstrap
// tenant (docs/adr/0032 D1–D3, D5–D8). `cowork serve` runs it at every start,
// after the migrations, as the runtime role and under an advisory lock, so
// that "helm install" with a database and a Secret yields an installation a
// person can log in to and use (D8). It is idempotent: a start that finds
// everything as the configuration says changes nothing and records nothing.
package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/store"
	"github.com/guided-traffic/cowork/backend/internal/store/writeq"
)

// jobName is the system actor, system:bootstrap (docs/adr/0032 D6), and the name
// the policies of the tables it writes admit; jobLock is its advisory lock.
const (
	jobName = "bootstrap"
	jobLock = 4

	entityUser       = "user"
	entityTenant     = "tenant"
	entityMembership = "membership"
	originConfig     = "config"

	reasonConfiguration = "configuration"
	actionCreated       = "created"
	fieldGlobalAdmin    = "global_admin"
)

// Params is what the configuration says. Username and Password come together or
// not at all, as do TenantSlug and TenantName, and a tenant needs somebody to
// administer it — the local administrator or the administrator group of the
// identity provider (internal/config checks all three).
type Params struct {
	Username, Password     string
	TenantSlug, TenantName string
	// AdminGroup is COWORK_ADMIN_GROUP: the bootstrap tenant gets a mapping
	// that makes its members administrators (docs/adr/0032 D6).
	AdminGroup string
}

// Sync makes the database say what Params says, once the advisory lock is its.
// Another replica that holds the lock is working on the same thing, so Sync
// waits for it and then finds nothing left to do.
func Sync(ctx context.Context, db *store.DB, p Params, logger *slog.Logger) error {
	deadline := time.Now().Add(2 * time.Minute)
	for {
		ran, err := db.RunJob(ctx, jobName, jobLock, func(w *store.Writer) error {
			return syncer{p: p, logger: logger}.run(ctx, w)
		})
		if err != nil {
			return fmt.Errorf("synchronise the configured administrator: %w", err)
		}
		if ran {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("another replica has held the bootstrap lock for two minutes")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

type syncer struct {
	p      Params
	logger *slog.Logger
}

func (s syncer) run(ctx context.Context, w *store.Writer) error {
	accounts, err := w.ListConfigAccounts(ctx)
	if err != nil {
		return fmt.Errorf("list the accounts the configuration keeps: %w", err)
	}
	// The configuration keeps one account (D4): another one it kept — the
	// variables are unset, or name another username — is deactivated, never
	// deleted (D2).
	for _, a := range accounts {
		if a.DeactivatedAt == nil && (s.p.Username == "" || deref(a.Username) != s.p.Username) {
			if err := s.deactivate(ctx, w, a.ID, deref(a.Username)); err != nil {
				return err
			}
		}
	}
	adminID := uuid.Nil
	if s.p.Username != "" {
		if adminID, err = s.keepAdministrator(ctx, w); err != nil {
			return err
		}
	}
	return s.keepTenant(ctx, w, adminID)
}

func (s syncer) deactivate(ctx context.Context, w *store.Writer, id uuid.UUID, username string) error {
	if _, err := w.DeactivateUser(ctx, id); err != nil {
		return fmt.Errorf("deactivate the account: %w", err)
	}
	revoked, err := w.RevokeTokensOfUser(ctx, writeq.RevokeTokensOfUserParams{UserID: id})
	if err != nil {
		return fmt.Errorf("revoke its tokens: %w", err)
	}
	ended, err := w.DeleteSessionsOfUser(ctx, id)
	if err != nil {
		return fmt.Errorf("end its sessions: %w", err)
	}
	w.Record(store.Event{EntityType: entityUser, EntityID: id, Action: "deactivated", Reason: reasonConfiguration,
		After: map[string]any{"tokens_revoked": revoked, "sessions_ended": ended}})
	s.logger.Info("the local administrator is deactivated: the configuration no longer names it", "username", username)
	return nil
}

// keepAdministrator creates or updates the account of COWORK_LOCAL_ADMIN_USERNAME
// (docs/adr/0032 D1, D2): a global administrator and a full account, with the
// configured password. A password that differs from the stored hash is
// re-hashed and every session of the account ends; so does the take-over of an
// account a tenant's administrator made under that name, which loses its
// tokens as well — the person behind the name is the operator's now.
func (s syncer) keepAdministrator(ctx context.Context, w *store.Writer) (uuid.UUID, error) {
	username := s.p.Username
	row, err := w.GetUserByUsername(ctx, &username)
	if errors.Is(err, pgx.ErrNoRows) {
		return s.createAdministrator(ctx, w)
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("read the account: %w", err)
	}
	if row.AccountOrigin == nil {
		return s.addAccount(ctx, w, row.ID)
	}
	changed := false
	if row.DeactivatedAt != nil {
		if _, err := w.ReactivateUser(ctx, row.ID); err != nil {
			return uuid.Nil, fmt.Errorf("reactivate the account: %w", err)
		}
		w.Record(store.Event{EntityType: entityUser, EntityID: row.ID, Action: "reactivated", Reason: reasonConfiguration})
		changed = true
	}
	if !row.GlobalAdmin {
		if _, err := w.SetGlobalAdmin(ctx, row.ID); err != nil {
			return uuid.Nil, fmt.Errorf("make the account a global administrator: %w", err)
		}
		w.Record(store.Event{EntityType: entityUser, EntityID: row.ID, Action: "updated", Reason: reasonConfiguration,
			After: map[string]any{fieldGlobalAdmin: true}})
		changed = true
	}
	takeOver := *row.AccountOrigin != originConfig
	matches, verr := auth.VerifyPassword(ctx, s.p.Password, deref(row.AccountPasswordHash))
	if verr != nil && ctx.Err() != nil {
		return uuid.Nil, verr
	}
	if takeOver || !matches {
		if err := s.setPassword(ctx, w, row.ID, takeOver); err != nil {
			return uuid.Nil, err
		}
		changed = true
	}
	if changed {
		s.logger.Info("the local administrator is in step with the configuration", "username", username, "taken_over", takeOver)
	}
	return row.ID, nil
}

func (s syncer) createAdministrator(ctx context.Context, w *store.Writer) (uuid.UUID, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, err
	}
	username := s.p.Username
	if err := w.InsertUser(ctx, writeq.InsertUserParams{ID: id, Username: &username, DisplayName: username, GlobalAdmin: true}); err != nil {
		return uuid.Nil, fmt.Errorf("create the account: %w", err)
	}
	if err := s.insertAccount(ctx, w, id); err != nil {
		return uuid.Nil, err
	}
	w.Record(store.Event{EntityType: entityUser, EntityID: id, Action: actionCreated, Reason: reasonConfiguration,
		After: map[string]any{"username": username, fieldGlobalAdmin: true, "origin": originConfig}})
	s.logger.Info("the local administrator is created", "username", username)
	return id, nil
}

// addAccount gives a person who exists without a local account — an identity
// the fixture or a later provider made — the configured account.
func (s syncer) addAccount(ctx context.Context, w *store.Writer, id uuid.UUID) (uuid.UUID, error) {
	if err := s.insertAccount(ctx, w, id); err != nil {
		return uuid.Nil, err
	}
	if _, err := w.ReactivateUser(ctx, id); err != nil {
		return uuid.Nil, fmt.Errorf("reactivate the person: %w", err)
	}
	if _, err := w.SetGlobalAdmin(ctx, id); err != nil {
		return uuid.Nil, fmt.Errorf("make the person a global administrator: %w", err)
	}
	w.Record(store.Event{EntityType: entityUser, EntityID: id, Action: "updated", Reason: reasonConfiguration,
		After: map[string]any{"local_account": true, fieldGlobalAdmin: true, "origin": originConfig}})
	s.logger.Info("the local administrator is attached to an existing person", "username", s.p.Username)
	return id, nil
}

func (s syncer) insertAccount(ctx context.Context, w *store.Writer, id uuid.UUID) error {
	hash, err := auth.HashPassword(ctx, s.p.Password)
	if err != nil {
		return err
	}
	if err := w.InsertLocalAccount(ctx, writeq.InsertLocalAccountParams{UserID: id, PasswordHash: hash, Origin: originConfig}); err != nil {
		return fmt.Errorf("create the local account: %w", err)
	}
	return nil
}

// setPassword stores the configured password, ends every session of the
// account, revokes every token of it, and forgets the failures that may have
// locked it. Rotating the Secret is the recovery of a leaked password or a
// locked administrator (docs/adr/0033 D6, D7): a session or a token that was
// made with the old password does not outlive it.
func (s syncer) setPassword(ctx context.Context, w *store.Writer, id uuid.UUID, takeOver bool) error {
	hash, err := auth.HashPassword(ctx, s.p.Password)
	if err != nil {
		return err
	}
	if takeOver {
		_, err = w.TakeOverAccount(ctx, writeq.TakeOverAccountParams{PasswordHash: hash, UserID: id})
	} else {
		_, err = w.SetAccountPassword(ctx, writeq.SetAccountPasswordParams{PasswordHash: hash, UserID: id})
	}
	if err != nil {
		return fmt.Errorf("store the configured password: %w", err)
	}
	ended, err := w.DeleteSessionsOfUser(ctx, id)
	if err != nil {
		return fmt.Errorf("end the sessions: %w", err)
	}
	revoked, err := w.RevokeTokensOfUser(ctx, writeq.RevokeTokensOfUserParams{UserID: id})
	if err != nil {
		return fmt.Errorf("revoke the tokens: %w", err)
	}
	after := map[string]any{"sessions_ended": ended, "tokens_revoked": revoked}
	if takeOver {
		after["taken_over"] = true
	}
	if _, err := w.DeleteFailedLoginAttempts(ctx, s.p.Username); err != nil {
		return fmt.Errorf("forget the failed logins: %w", err)
	}
	if _, err := w.DeleteLoginLock(ctx, s.p.Username); err != nil {
		return fmt.Errorf("forget the lock: %w", err)
	}
	w.Record(store.Event{EntityType: entityUser, EntityID: id, Action: "password_changed", Reason: reasonConfiguration, After: after})
	return nil
}

// keepTenant creates the bootstrap tenant when none exists, gives the local
// administrator — when one is configured — a marked grant as its
// administrator, and maps the administrator group — when one is configured —
// to its admin role (docs/adr/0032 D6, D7, docs/adr/0030 D2). When a tenant
// exists the variables do nothing, whatever they say. The members of the
// group get their membership at their next login or groups refresh.
func (s syncer) keepTenant(ctx context.Context, w *store.Writer, adminID uuid.UUID) error {
	if s.p.TenantSlug == "" {
		return nil
	}
	exists, err := w.TenantsExist(ctx)
	if err != nil {
		return fmt.Errorf("ask whether a tenant exists: %w", err)
	}
	if exists {
		return nil
	}
	id, err := uuid.NewV7()
	if err != nil {
		return err
	}
	grantID, err := uuid.NewV7()
	if err != nil {
		return err
	}
	if err := w.InsertTenant(ctx, writeq.InsertTenantParams{ID: id, Slug: s.p.TenantSlug, Name: s.p.TenantName}); err != nil {
		return fmt.Errorf("create the bootstrap tenant: %w", err)
	}
	w.Record(store.Event{EntityType: entityTenant, EntityID: id, Action: actionCreated,
		After: map[string]any{"slug": s.p.TenantSlug, "name": s.p.TenantName}})
	if adminID != uuid.Nil {
		if err := w.InsertGrant(ctx, writeq.InsertGrantParams{ID: grantID, TenantID: id, UserID: adminID, Role: domain.RoleAdmin}); err != nil {
			return fmt.Errorf("grant the administrator the bootstrap tenant: %w", err)
		}
		w.Record(store.Event{EntityType: entityMembership, EntityID: grantID, Action: actionCreated,
			After: map[string]any{"tenant": s.p.TenantSlug, "user": adminID, "role": domain.RoleAdmin, "source": "grant"}})
	}
	if s.p.AdminGroup != "" {
		mappingID, err := uuid.NewV7()
		if err != nil {
			return err
		}
		if err := w.InsertGroupMapping(ctx, writeq.InsertGroupMappingParams{ID: mappingID, TenantID: id, GroupName: s.p.AdminGroup,
			Role: domain.RoleAdmin}); err != nil {
			return fmt.Errorf("map the administrator group to the bootstrap tenant: %w", err)
		}
		w.Record(store.Event{EntityType: "group_mapping", EntityID: mappingID, Action: actionCreated,
			After: map[string]any{"tenant": s.p.TenantSlug, "group": s.p.AdminGroup, "role": domain.RoleAdmin}})
	}
	s.logger.Info("the bootstrap tenant is created", "slug", s.p.TenantSlug, "administrator_group", s.p.AdminGroup != "")
	return nil
}

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}
