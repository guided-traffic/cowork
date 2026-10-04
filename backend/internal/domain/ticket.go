package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// The ticket vocabularies (docs/adr/0008 D1, 0009 D1, D2, 0010 D1). The
// database enums of the same names are mapped onto these types.
type (
	// TicketType is what kind of work a ticket is.
	TicketType string
	// TicketState is where a ticket stands.
	TicketState string
	// BlockKind is what a blocked ticket waits on.
	BlockKind string
	// Severity is the impact if the ticket is never done.
	Severity string
	// SecurityClass is the threat-model class of the tickets page.
	SecurityClass string
	// Urgency is when the ticket matters, derived by a rule set.
	Urgency string
	// Effort is a size, not a time.
	Effort string
)

// The ticket states.
const (
	StateFiled      TicketState = "filed"
	StateAnalysed   TicketState = "analysed"
	StateDecided    TicketState = "decided"
	StateInProgress TicketState = "in-progress"
	// StateReview is the check of the work before it ends (docs/adr/0009 D1).
	StateReview  TicketState = "review"
	StateBlocked TicketState = "blocked"
	StateDone    TicketState = "done"
	StateDropped TicketState = "dropped"
)

// Terminal reports whether s is done or dropped (docs/adr/0009 D1).
func (s TicketState) Terminal() bool { return s == StateDone || s == StateDropped }

// The block kinds.
const (
	BlockDecision BlockKind = "decision"
	BlockHuman    BlockKind = "human"
	BlockProduct  BlockKind = "product"
	BlockRelease  BlockKind = "release"
	BlockExternal BlockKind = "external"
	BlockTicket   BlockKind = "ticket"
)

// The ticket types.
const (
	TypeTask     TicketType = "task"
	TypeBug      TicketType = "bug"
	TypeFeature  TicketType = "feature"
	TypeDecision TicketType = "decision"
	TypeQuestion TicketType = "question"
)

// The security classes; live and boundary set a ticket confidential
// (docs/adr/0065 D2).
const (
	SecurityLive      SecurityClass = "live"
	SecurityBoundary  SecurityClass = "boundary"
	SecurityHardening SecurityClass = "hardening"
	SecurityNone      SecurityClass = "none"
)

// MakesConfidential reports whether the class sets the confidential flag.
func (c SecurityClass) MakesConfidential() bool {
	return c == SecurityLive || c == SecurityBoundary
}

// The urgencies.
const (
	UrgencyNow     Urgency = "now"
	UrgencyRelease Urgency = "release"
	UrgencyNext    Urgency = "next"
	UrgencyLater   Urgency = "later"
	UrgencyIcebox  Urgency = "icebox"
)

// LinkType is a directed link between two tickets of a tenant
// (docs/adr/0012 D1).
type LinkType string

// The link types.
const (
	LinkBlocks     LinkType = "blocks"
	LinkRelatesTo  LinkType = "relates-to"
	LinkDuplicates LinkType = "duplicates"
	LinkFoundIn    LinkType = "found-in"
)

// Name reads a link from one of its ends: the source sees the type, the
// target its reverse view; relates-to reads the same both ways.
func (l LinkType) Name(outgoing bool) string {
	switch {
	case l == LinkRelatesTo:
		return "relates to"
	case outgoing && l == LinkFoundIn:
		return "found in"
	case outgoing:
		return string(l)
	case l == LinkBlocks:
		return "blocked by"
	case l == LinkDuplicates:
		return "duplicated by"
	default:
		return "found here"
	}
}

// The urgency is the ticket's horizon, a planning category a person or an
// agent sets in whatever state the ticket is; nothing derives it (docs/adr/0010
// D3 as amended 2026-10-04). Rule set v2 has one row: every ticket derives
// later, and the horizon set on it is what the API calls its override.
const (
	UrgencyDefault     = UrgencyLater
	UrgencyRuleDefault = "v2:default"
)

// Ticket keys (docs/adr/0007): <tenant-slug>/<PROJECT-KEY>-<number> in full,
// <PROJECT-KEY>-<number> short. The project key has no hyphen, so the last
// hyphen ends it.
var (
	slugPattern       = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,62}$`)
	projectKeyPattern = regexp.MustCompile(`^[A-Z][A-Z0-9]{1,9}$`)
)

// ValidProjectKey reports whether s is a project key: upper case, no hyphen.
func ValidProjectKey(s string) bool { return projectKeyPattern.MatchString(s) }

// ValidTenantSlug reports whether s is a tenant's slug (docs/adr/0005 D4).
func ValidTenantSlug(s string) bool { return slugPattern.MatchString(s) }

// TicketKey is a parsed ticket key.
type TicketKey struct {
	// Tenant is the slug; empty for a short key.
	Tenant  string
	Project string
	// Number is positive and within the column's integer range.
	Number int32
}

// FullKey returns the canonical form, which cowork writes everywhere
// (docs/adr/0007 D2).
func FullKey(tenant, project string, number int32) string {
	return tenant + "/" + ShortKey(project, number)
}

// ShortKey returns PROJECT-number.
func ShortKey(project string, number int32) string {
	return project + "-" + strconv.FormatInt(int64(number), 10)
}

// ErrBadKey is a key that does not follow the grammar.
var ErrBadKey = errors.New("not a ticket key: <tenant>/<PROJECT>-<number> or <PROJECT>-<number>")

// ParseTicketKey reads a full or a short key. Which forms an input may use is
// the caller's to decide: a short key only where the tenant is fixed by the
// context (docs/adr/0007 D3).
func ParseTicketKey(s string) (TicketKey, error) {
	var k TicketKey
	rest := s
	if tenant, after, ok := strings.Cut(s, "/"); ok {
		if !slugPattern.MatchString(tenant) {
			return k, ErrBadKey
		}
		k.Tenant, rest = tenant, after
	}
	i := strings.LastIndex(rest, "-")
	if i < 0 {
		return k, ErrBadKey
	}
	k.Project = rest[:i]
	if !projectKeyPattern.MatchString(k.Project) {
		return k, ErrBadKey
	}
	digits := rest[i+1:]
	n, err := strconv.ParseInt(digits, 10, 32)
	if err != nil || n <= 0 || strconv.FormatInt(n, 10) != digits {
		return k, ErrBadKey
	}
	k.Number = int32(n)
	return k, nil
}

// InTenant resolves a key given inside a tenant: a short key belongs to the
// tenant, a full one must name it.
func (k TicketKey) InTenant(slug string) (TicketKey, error) {
	if k.Tenant != "" && k.Tenant != slug {
		return k, fmt.Errorf("the key names the tenant %q, not %q", k.Tenant, slug)
	}
	k.Tenant = slug
	return k, nil
}

// ValidProgress reports whether p is a progress value: 0 to 100 in steps of
// five (docs/adr/0017 D2).
func ValidProgress(p int) bool { return p >= 0 && p <= 100 && p%5 == 0 }
