// Package fixture creates the persons, tenants, memberships, projects and
// tokens a test or a development database needs, before any login exists
// (docs/adr/0038 D6). It writes over the administrative connection, past
// row-level security, and it is never part of the binary: nothing under
// backend/cmd/cowork imports it, which a test asserts.
package fixture

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/domain"
)

// AllCapabilities is the "full" capability set of docs/adr/0043 D4.
var AllCapabilities = []string{"decide", "close", "drop", "rank", "set-horizon", "interest",
	"upload", "create-project", "record-answer"}

// AssistedCapabilities is the "assisted" shortcut of docs/adr/0043 D4.
var AssistedCapabilities = []string{"drop", "set-horizon", "interest", "upload"}

// DB writes fixture rows over an administrative connection to the database
// under test.
type DB struct {
	pool *pgxpool.Pool
}

// Connect opens the administrative connection. The role in adminURL must be
// a superuser or own the tables with row-level security not forced on it; a
// fixture write is not an act and leaves no audit row.
func Connect(ctx context.Context, adminURL string) (*DB, error) {
	pool, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		return nil, fmt.Errorf("open fixture connection: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping fixture connection: %w", err)
	}
	return &DB{pool: pool}, nil
}

// Close closes the connection.
func (f *DB) Close() { f.pool.Close() }

// Exec runs a statement over the administrative connection, for the rare test
// that must arrange a state no route can reach.
func (f *DB) Exec(ctx context.Context, sql string, args ...any) error {
	_, err := f.pool.Exec(ctx, sql, args...)
	return err
}

// QueryRow runs a query over the administrative connection, for tests that
// read what the runtime role may not.
func (f *DB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return f.pool.QueryRow(ctx, sql, args...)
}

// Query runs a query over the administrative connection.
func (f *DB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return f.pool.Query(ctx, sql, args...)
}

// QueryCount runs a counting query over the administrative connection.
func (f *DB) QueryCount(ctx context.Context, sql string, args ...any) (int64, error) {
	var n int64
	err := f.pool.QueryRow(ctx, sql, args...).Scan(&n)
	return n, err
}

// Person creates a person with a local identity, or returns the one with that
// username.
func (f *DB) Person(ctx context.Context, username, displayName string) (uuid.UUID, error) {
	var id uuid.UUID
	err := f.pool.QueryRow(ctx,
		`INSERT INTO users (username, display_name) VALUES ($1, $2)
		 ON CONFLICT (username) DO UPDATE SET display_name = EXCLUDED.display_name
		 RETURNING id`, username, displayName).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("create person %s: %w", username, err)
	}
	return id, nil
}

// Tenant creates a tenant, or returns the one with that slug.
func (f *DB) Tenant(ctx context.Context, slug, name string) (uuid.UUID, error) {
	var id uuid.UUID
	err := f.pool.QueryRow(ctx,
		`INSERT INTO tenants (slug, name) VALUES ($1, $2)
		 ON CONFLICT (slug) DO UPDATE SET name = EXCLUDED.name
		 RETURNING id`, slug, name).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("create tenant %s: %w", slug, err)
	}
	return id, nil
}

// Member grants a person a role in a tenant, as a marked manual grant
// (docs/adr/0030 D3), replacing an earlier grant's role.
func (f *DB) Member(ctx context.Context, tenantID, userID uuid.UUID, role domain.Role) error {
	_, err := f.pool.Exec(ctx,
		`INSERT INTO memberships (tenant_id, user_id, role, source) VALUES ($1, $2, $3, 'grant')
		 ON CONFLICT (tenant_id, user_id, source) DO UPDATE SET role = EXCLUDED.role`,
		tenantID, userID, string(role))
	if err != nil {
		return fmt.Errorf("grant %s: %w", role, err)
	}
	return nil
}

// Account gives a person a local account with the password
// (docs/adr/0033). The account is managed by the tenant — the way the route
// that creates one makes it — or, with uuid.Nil, belongs to the configuration,
// like the local administrator (docs/adr/0032 D1). changeRequired makes the
// password temporary.
func (f *DB) Account(ctx context.Context, userID uuid.UUID, password string, managingTenant uuid.UUID, changeRequired bool) error {
	hash, err := auth.HashPassword(ctx, password)
	if err != nil {
		return err
	}
	origin := "config"
	if managingTenant != uuid.Nil {
		origin = "tenant"
	}
	_, err = f.pool.Exec(ctx,
		`INSERT INTO local_accounts (user_id, password_hash, password_change_required, origin, managing_tenant_id)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (user_id) DO UPDATE SET password_hash = EXCLUDED.password_hash,
		     password_change_required = EXCLUDED.password_change_required`,
		userID, hash, changeRequired, origin, nilIfZero(managingTenant))
	if err != nil {
		return fmt.Errorf("create local account: %w", err)
	}
	return nil
}

// GlobalAdmin makes a person a global administrator (docs/adr/0004 D4).
func (f *DB) GlobalAdmin(ctx context.Context, userID uuid.UUID) error {
	_, err := f.pool.Exec(ctx, `UPDATE users SET global_admin = true WHERE id = $1`, userID)
	return err
}

// Project creates a project with its ticket counter.
func (f *DB) Project(ctx context.Context, tenantID uuid.UUID, key, name string) (uuid.UUID, error) {
	var id uuid.UUID
	err := f.pool.QueryRow(ctx,
		`WITH p AS (INSERT INTO projects (tenant_id, key, name) VALUES ($1, $2, $3) RETURNING id, tenant_id)
		 INSERT INTO ticket_counters (tenant_id, project_id) SELECT tenant_id, id FROM p RETURNING project_id`,
		tenantID, key, name).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("create project %s: %w", key, err)
	}
	return id, nil
}

// Ticket files a plain task as the reporter, with the project's next number,
// and returns its id and number.
func (f *DB) Ticket(ctx context.Context, tenantID, projectID, reporter uuid.UUID, title string) (uuid.UUID, int, error) {
	var id uuid.UUID
	var number int
	err := f.pool.QueryRow(ctx,
		`WITH n AS (UPDATE ticket_counters SET last_number = last_number + 1
		            WHERE tenant_id = $1 AND project_id = $2 RETURNING last_number)
		 INSERT INTO tickets (tenant_id, project_id, number, type, title, severity, security, urgency_derived,
		                      urgency_rule, effort, reporter_id)
		 SELECT $1, $2, n.last_number, 'task', $4, 'medium', 'none', 'later', 'v2:default', 'S', $3 FROM n
		 RETURNING id, number`,
		tenantID, projectID, reporter, title).Scan(&id, &number)
	if err != nil {
		return uuid.Nil, 0, fmt.Errorf("file ticket %q: %w", title, err)
	}
	return id, number, nil
}

// TokenSpec describes a token to create. Zero values are the common case: a
// write token without restriction that expires in ninety days.
type TokenSpec struct {
	UserID       uuid.UUID
	Name         string
	Scope        domain.Scope
	Agent        bool
	Capabilities []string
	TenantID     uuid.UUID
	ProjectID    uuid.UUID
	ExpiresAt    time.Time
	Revoked      bool
}

// Token creates a token and returns its plaintext, which exists nowhere else.
func (f *DB) Token(ctx context.Context, spec TokenSpec) (plaintext string, id uuid.UUID, err error) {
	if spec.UserID == uuid.Nil {
		return "", uuid.Nil, errors.New("a token needs its person")
	}
	if spec.Name == "" {
		spec.Name = "fixture"
	}
	if spec.Scope == "" {
		spec.Scope = domain.ScopeWrite
	}
	if spec.ExpiresAt.IsZero() {
		spec.ExpiresAt = time.Now().Add(90 * 24 * time.Hour)
	}
	if spec.Agent && spec.Capabilities == nil {
		spec.Capabilities = AllCapabilities
	}
	if !spec.Agent {
		spec.Capabilities = nil
	}
	plaintext, hash, err := auth.GenerateToken()
	if err != nil {
		return "", uuid.Nil, err
	}
	createdAt := time.Now()
	if !spec.ExpiresAt.After(createdAt) {
		// An expired token: created before its expiry, as the table demands.
		createdAt = spec.ExpiresAt.Add(-time.Hour)
	}
	err = f.pool.QueryRow(ctx,
		`INSERT INTO tokens (user_id, name, token_hash, scope, restricted_tenant_id, restricted_project_id,
		                     agent, capabilities, created_at, expires_at, revoked_at, revoked_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
		         CASE WHEN $11 THEN now() END, CASE WHEN $11 THEN $1::uuid END)
		 RETURNING id`,
		spec.UserID, spec.Name, hash[:], string(spec.Scope), nilIfZero(spec.TenantID), nilIfZero(spec.ProjectID),
		spec.Agent, nonNil(spec.Capabilities), createdAt, spec.ExpiresAt, spec.Revoked).Scan(&id)
	if err != nil {
		return "", uuid.Nil, fmt.Errorf("create token: %w", err)
	}
	return plaintext, id, nil
}

// Session creates a browser session of the person, as a local login makes
// one, live for twelve hours from now, and returns its cookie value, which
// exists nowhere else (docs/adr/0031 D1).
func (f *DB) Session(ctx context.Context, userID uuid.UUID) (string, error) {
	value, hash, err := auth.GenerateSession()
	if err != nil {
		return "", err
	}
	err = f.pool.QueryRow(ctx,
		`INSERT INTO sessions (user_id, token_hash, created_at, last_seen_at, expires_at)
		 VALUES ($1, $2, now(), now(), now() + interval '12 hours') RETURNING id`,
		userID, hash[:]).Scan(new(uuid.UUID))
	if err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}
	return value, nil
}

// TokenHash is the stored hash of a plaintext token, for tests that look a
// token up directly.
func TokenHash(plaintext string) [sha256.Size]byte { return auth.HashToken(plaintext) }

func nilIfZero(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return slices.Clone(s)
}
