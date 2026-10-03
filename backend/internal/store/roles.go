package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// roleFacts are the attributes that decide whether a role could see past
// row-level security (docs/adr/0021 D2).
type roleFacts struct {
	superuser     bool
	bypassRLS     bool
	ownsRelations bool
	memberOfOwner bool
}

func (f roleFacts) check(role string) error {
	var problems []error
	if f.superuser {
		problems = append(problems, fmt.Errorf("the runtime role %q is a superuser", role))
	}
	if f.bypassRLS {
		problems = append(problems, fmt.Errorf("the runtime role %q has BYPASSRLS", role))
	}
	if f.ownsRelations {
		problems = append(problems, fmt.Errorf("the runtime role %q owns relations of the schema; the owner role must own them", role))
	}
	if f.memberOfOwner {
		problems = append(problems, fmt.Errorf("the runtime role %q is a member of the owner role", role))
	}
	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("refusing a runtime role that could bypass row-level security (docs/adr/0021 D2): %w", errors.Join(problems...))
}

// The two queries read the facts of a role as any role may: pg_roles and
// pg_class are readable by everyone. "Member of the owner" is asked against
// the owner of the tenants table, the first table of the schema, and is false
// while the table does not exist. namedRoleQuery takes the role as $1;
// currentRoleQuery reads the connection's own role.
const namedRoleQuery = `
SELECT r.rolsuper,
       r.rolbypassrls,
       EXISTS (SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
               WHERE n.nspname = 'public' AND c.relowner = r.oid),
       COALESCE((SELECT pg_has_role(r.oid, c.relowner, 'MEMBER') FROM pg_class c
                 WHERE c.oid = to_regclass('public.tenants') AND c.relowner <> r.oid), false)
FROM pg_roles r
WHERE r.rolname = $1`

const currentRoleQuery = `
SELECT current_user,
       r.rolsuper,
       r.rolbypassrls,
       EXISTS (SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
               WHERE n.nspname = 'public' AND c.relowner = r.oid),
       COALESCE((SELECT pg_has_role(r.oid, c.relowner, 'MEMBER') FROM pg_class c
                 WHERE c.oid = to_regclass('public.tenants') AND c.relowner <> r.oid), false)
FROM pg_roles r
WHERE r.rolname = current_user`

// checkRoleAsOwner checks the named runtime role from the owner's connection
// before and after the migration run. Before the run the schema may not exist
// yet, so ownership can only fail after it.
func checkRoleAsOwner(ctx context.Context, db *sql.DB, role string, afterMigration bool) error {
	var f roleFacts
	err := db.QueryRowContext(ctx, namedRoleQuery, role).
		Scan(&f.superuser, &f.bypassRLS, &f.ownsRelations, &f.memberOfOwner)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("the runtime role %q does not exist; the operator creates it (docs/adr/0058 D5)", role)
	}
	if err != nil {
		return fmt.Errorf("read the runtime role: %w", err)
	}
	if !afterMigration {
		f.ownsRelations = false
	}
	return f.check(role)
}

// CheckRuntimeRole refuses a connection whose role could bypass row-level
// security: a superuser, a role with BYPASSRLS, a role that owns relations of
// the schema, or a member of the owner role (docs/adr/0021 D2). `cowork serve`
// calls it before it listens.
func (db *DB) CheckRuntimeRole(ctx context.Context) error {
	var (
		f    roleFacts
		role string
	)
	err := db.pool.QueryRow(ctx, currentRoleQuery).
		Scan(&role, &f.superuser, &f.bypassRLS, &f.ownsRelations, &f.memberOfOwner)
	if err != nil {
		return fmt.Errorf("read the runtime role: %w", err)
	}
	return f.check(role)
}
