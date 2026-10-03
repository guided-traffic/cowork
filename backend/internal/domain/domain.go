// Package domain holds the vocabularies of cowork and the rules that need no
// database: roles, scopes, keys, states. The database enums of the same
// names are mapped onto these types by sqlc (backend/sqlc.yaml), so a value
// read from a row and a value parsed from a request are one type.
package domain

// Role is a person's role in a tenant (docs/adr/0034 D1). The roles are
// ordered; a check is role.AtLeast(required).
type Role string

// The tenant roles, lowest first.
const (
	RoleViewer Role = "viewer"
	RoleMember Role = "member"
	RoleAdmin  Role = "admin"
)

func (r Role) rank() int {
	switch r {
	case RoleViewer:
		return 1
	case RoleMember:
		return 2
	case RoleAdmin:
		return 3
	default:
		return 0
	}
}

// AtLeast reports whether r is required or higher. An unknown role is below
// every role.
func (r Role) AtLeast(required Role) bool {
	return r.rank() > 0 && r.rank() >= required.rank()
}

// Min returns the lower of two roles; an unknown role is the lowest.
func (r Role) Min(other Role) Role {
	if other.rank() < r.rank() {
		return other
	}
	return r
}

// Valid reports whether r is one of the three roles.
func (r Role) Valid() bool { return r.rank() > 0 }

// Scope is a personal access token's scope (docs/adr/0035 D3), ordered like
// the roles it stands for: read, then what a member may write, then what an
// administrator may.
type Scope string

// The token scopes, lowest first.
const (
	ScopeRead  Scope = "read"
	ScopeWrite Scope = "write"
	ScopeAdmin Scope = "admin"
)

func (s Scope) rank() int {
	switch s {
	case ScopeRead:
		return 1
	case ScopeWrite:
		return 2
	case ScopeAdmin:
		return 3
	default:
		return 0
	}
}

// AtLeast reports whether s is required or higher.
func (s Scope) AtLeast(required Scope) bool {
	return s.rank() > 0 && s.rank() >= required.rank()
}

// Valid reports whether s is one of the three scopes.
func (s Scope) Valid() bool { return s.rank() > 0 }
