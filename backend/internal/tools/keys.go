package tools

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/guided-traffic/cowork/backend/internal/domain"
)

// ticketRef is a ticket a tool names, its team resolved; Tenant holds the
// team's slug, as domain.TicketKey does.
type ticketRef struct {
	Tenant, Project string
	Number          int32
}

// Full is the canonical key, team/PROJECT-n (docs/adr/0007 D2).
func (r ticketRef) Full() string { return domain.FullKey(r.Tenant, r.Project, r.Number) }

// Short is PROJECT-n.
func (r ticketRef) Short() string { return domain.ShortKey(r.Project, r.Number) }

// resolveKey reads a ticket key: a full one, or a short one in the team the
// session is bound to (docs/adr/0007 D3).
func (s *Session) resolveKey(raw string) (ticketRef, error) {
	raw = strings.TrimSpace(raw)
	k, err := domain.ParseTicketKey(raw)
	if err != nil {
		return ticketRef{}, usage("%q is not a ticket key: write team/PROJECT-n, or PROJECT-n in a session bound to a project", raw)
	}
	if k.Tenant == "" {
		b := s.Binding()
		if b == nil {
			return ticketRef{}, usage("%q is a short key, and this session is bound to no project: write the full key, team/%s", raw, raw)
		}
		k.Tenant = b.Team
	}
	return ticketRef{Tenant: k.Tenant, Project: k.Project, Number: k.Number}, nil
}

// resolveProject reads a project: team/KEY, a key in the bound team, or the
// bound project when none is named.
func (s *Session) resolveProject(raw string) (team, project string, err error) {
	raw = strings.TrimSpace(raw)
	b := s.Binding()
	switch {
	case raw == "" && b != nil && b.Project != "":
		return b.Team, b.Project, nil
	case raw == "" && b != nil:
		return "", "", usage("name the project: its key in the team %s, or team/KEY", b.Team)
	case raw == "":
		return "", "", usage("this session is bound to no project: name one, team/KEY")
	}
	t, p, full := strings.Cut(raw, "/")
	if !full {
		if b == nil {
			return "", "", usage("%q needs its team, team/%s: this session is bound to no project", raw, raw)
		}
		t, p = b.Team, raw
	}
	if !domain.ValidTenantSlug(t) || !domain.ValidProjectKey(p) {
		return "", "", usage("%q is not a project: write team/KEY, the key upper case", raw)
	}
	return t, p, nil
}

// commitType is the conventional commit type a ticket's work proposes
// (docs/adr/0068 D3): a bug is fixed, a decision and a question end in
// documentation, everything else is a feature.
func commitType(ticketType string) string {
	switch ticketType {
	case string(domain.TypeBug):
		return "fix"
	case string(domain.TypeDecision), string(domain.TypeQuestion):
		return "docs"
	default:
		return "feat"
	}
}

// branchSlug is a title as a branch's tail: its lower-case ASCII words,
// without articles, five at most and forty characters at most; a word with
// another letter is left out rather than mangled.
func branchSlug(title string) string {
	words := strings.FieldsFunc(strings.ToLower(title), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	var kept []string
	n := 0
	for _, w := range words {
		if w != asciiOnly(w) || w == "a" || w == "an" || w == "the" {
			continue
		}
		if len(kept) == 5 || n+len(w)+len(kept) > 40 {
			break
		}
		kept, n = append(kept, w), n+len(w)
	}
	return strings.Join(kept, "-")
}

func asciiOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// commitLines are the strings a commit for the ticket carries, ready to copy
// (docs/adr/0068 D1–D4): the short key at the end of the subject, the full
// key in a trailer, and the default branch the person may replace.
func commitLines(r ticketRef, ticketType, title string) string {
	branch := commitType(ticketType) + "/" + r.Short()
	if slug := branchSlug(title); slug != "" {
		branch += "-" + slug
	}
	return fmt.Sprintf("Commits for %s: the subject ends with `(%s)`, the body ends with the trailer `Cowork-Ticket: %s`; "+
		"the branch is `%s` unless the person names another.", r.Full(), r.Short(), r.Full(), branch)
}
