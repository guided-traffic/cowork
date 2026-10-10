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

// crossingFunctions are the SECURITY DEFINER functions of migration 47 that
// read or write a ticket of another team (docs/adr/0021 D7 as made concrete
// 2026-10-10), and crossingPolicies how many policies admit them.
var crossingFunctions = []string{
	"relation_heads", "readable_ticket", "prerequisite_heads", "open_prerequisite_count", "open_prerequisite_targets",
	"open_prerequisite_heads", "parent_chain_reaches", "blocks_reach", "refresh_derived", "relations_elsewhere",
	"end_relations_elsewhere", "end_team_relations", "end_relation",
}

const crossingPolicies = 8

// crossingPolicyQuery reads every crossing policy and whether it names the
// owner of the tickets table alone; crossingFunctionQuery every crossing
// function and whether that owner owns it and it runs with the owner's rights.
// Both read catalogs any role may read.
const (
	crossingPolicyQuery = `
SELECT p.polname,
       p.polroles = ARRAY[(SELECT c.relowner FROM pg_class c WHERE c.oid = to_regclass('public.tickets'))]::oid[]
FROM pg_policy p
WHERE p.polname LIKE '%\_crossing\_%'`
	crossingFunctionQuery = `
SELECT f.proname,
       f.proowner = (SELECT c.relowner FROM pg_class c WHERE c.oid = to_regclass('public.tickets')) AND f.prosecdef
FROM pg_proc f
WHERE f.pronamespace = 'public'::regnamespace AND f.proname = ANY ($1::text[])`
)

// CheckCrossing refuses a database whose crossings between teams are not the
// owner role's alone: every crossing policy must name the owner of the tables
// and nobody else, and every crossing function must be that owner's and run
// with its rights. A change of ownership past the migrations — REASSIGN OWNED,
// an ALTER … OWNER — would leave policies that admit no function, so the heads
// would be absent and the cycle walks would see one team, a cycle across teams
// undetected. `cowork serve` calls it before it listens.
func (db *DB) CheckCrossing(ctx context.Context) error {
	var problems []error
	policies, err := crossingCatalog(ctx, db, crossingPolicyQuery, "policies",
		func(name string) error {
			return fmt.Errorf("the policy %s names another role than the owner of the tables", name)
		})
	if err != nil {
		return err
	}
	for _, p := range policies {
		problems = append(problems, p.problem)
	}
	if len(policies) != crossingPolicies {
		problems = append(problems, fmt.Errorf("%d crossing policies, %d expected", len(policies), crossingPolicies))
	}
	functions, err := crossingCatalog(ctx, db, crossingFunctionQuery, "functions",
		func(name string) error {
			return fmt.Errorf("the function %s is not the owner's running with its rights", name)
		},
		crossingFunctions)
	if err != nil {
		return err
	}
	found := map[string]bool{}
	for _, f := range functions {
		found[f.name] = true
		problems = append(problems, f.problem)
	}
	for _, f := range crossingFunctions {
		if !found[f] {
			problems = append(problems, fmt.Errorf("the function %s is missing", f))
		}
	}
	if err := errors.Join(problems...); err != nil {
		return fmt.Errorf("refusing a database whose crossings between teams are not the owner role's alone (docs/adr/0021 D7): %w", err)
	}
	return nil
}

// catalogEntry is a crossing policy or function as the catalog reads it, and
// the problem with it, nil for none.
type catalogEntry struct {
	name    string
	problem error
}

// crossingCatalog reads the name and the owner's check of every row a catalog
// query answers.
func crossingCatalog(ctx context.Context, db *DB, query, what string, bad func(string) error, args ...any) ([]catalogEntry, error) {
	rows, err := db.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("read the crossing %s: %w", what, err)
	}
	defer rows.Close()
	var out []catalogEntry
	for rows.Next() {
		var (
			name string
			good bool
		)
		if err := rows.Scan(&name, &good); err != nil {
			return nil, fmt.Errorf("read the crossing %s: %w", what, err)
		}
		e := catalogEntry{name: name}
		if !good {
			e.problem = bad(name)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read the crossing %s: %w", what, err)
	}
	return out, nil
}
