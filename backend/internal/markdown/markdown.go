// Package markdown renders the canonical Markdown of a ticket, grammar v1
// (docs/adr/0044 D1, docs/adr/0011 D4): the frontmatter rendered from the
// columns, the body, then the open questions. It is a pure function of its
// input, so the same ticket renders the same bytes.
package markdown

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Ticket is everything the document shows. Empty strings and nil times are
// absent keys.
type Ticket struct {
	Key, Title, Type, State, Severity, Security, Threat, Horizon, Effort string
	// The three progress stages (docs/adr/0017 D2): Progress is the
	// implementation stage and keeps its key.
	ProgressRefinement, Progress, ProgressReview int
	// Assignee is the person the ticket is assigned to; Parent the full key.
	Assignee      Person
	Parent        string
	Opened        time.Time
	Decided, Done *time.Time
	// Shipped is the verification note of the done act; DroppedReason the
	// reason of the dropped act.
	Shipped, DroppedReason string
	// BlockedBy is the block's kind, BlockedReason its text, BlockedFrom the
	// state it came from.
	BlockedBy, BlockedReason, BlockedFrom string
	Attachments                           []string
	Body                                  string
	Questions                             []Question
}

// Person is a person as the document writes them (docs/adr/0044 D1): the
// display name for a reader and the identity for an importer. Username names
// a local account (docs/adr/0033 D2); Issuer and Subject a person of the
// identity provider (docs/adr/0029 D5).
type Person struct {
	Name, Username, Issuer, Subject string
}

// Question is one open question, rendered as ### Q<n>.
type Question struct {
	Number                            int
	Question, Options, Recommendation string
	// Status is open, answered or withdrawn; Answer is set when answered.
	Status, Answer string
}

// Render returns the document.
func Render(t Ticket) []byte {
	var b bytes.Buffer
	b.WriteString("---\n")
	raw := func(key, value string) {
		if value != "" {
			b.WriteString(key + ": " + value + "\n")
		}
	}
	field := func(key, value string) {
		if value != "" {
			raw(key, scalar(value))
		}
	}
	for _, kv := range [][2]string{
		{"key", t.Key}, {"title", t.Title}, {"type", t.Type}, {"state", t.State}, {"severity", t.Severity},
		{"security", t.Security},
	} {
		field(kv[0], kv[1])
	}
	if t.Security != "none" {
		field("threat", t.Threat)
	}
	field("horizon", t.Horizon)
	field("effort", t.Effort)
	raw("progress-refinement", strconv.Itoa(t.ProgressRefinement))
	raw("progress", strconv.Itoa(t.Progress))
	raw("progress-review", strconv.Itoa(t.ProgressReview))
	field("assignee", person(t.Assignee))
	field("parent", t.Parent)
	raw("opened", day(&t.Opened))
	raw("decided", day(t.Decided))
	raw("done", day(t.Done))
	field("shipped", t.Shipped)
	field("dropped-reason", t.DroppedReason)
	field("blocked-by", t.BlockedBy)
	field("blocked-reason", t.BlockedReason)
	field("blocked-from", t.BlockedFrom)
	if len(t.Attachments) > 0 {
		b.WriteString("attachments:\n")
		for _, a := range t.Attachments {
			b.WriteString("  - " + scalar(a) + "\n")
		}
	}
	b.WriteString("---\n")
	if body := strings.TrimSpace(t.Body); body != "" {
		b.WriteString("\n" + body + "\n")
	}
	b.WriteString("\n## Open questions\n")
	for _, q := range t.Questions {
		b.WriteString("\n### Q" + strconv.Itoa(q.Number) + ": " + oneLine(q.Question) + "\n")
		if o := strings.TrimSpace(q.Options); o != "" {
			b.WriteString("\n" + o + "\n")
		}
		if r := strings.TrimSpace(q.Recommendation); r != "" {
			b.WriteString("\n**Recommendation:** " + r + "\n")
		}
		b.WriteString("\n**Answer:** " + answer(q) + "\n")
	}
	return b.Bytes()
}

// person writes a person the way git writes an author, `Name <identity>`:
// local:<username> for a local account, oidc:<issuer>#<subject> for a person
// of the identity provider — an issuer has no fragment, so the first # ends
// it. The name loses its angle brackets, as git's does, so the first < starts
// the identity. A person with neither identity, whom no route makes, is
// written by name alone.
func person(p Person) string {
	name := oneLine(strings.NewReplacer("<", "", ">", "").Replace(p.Name))
	var id string
	switch {
	case p.Username != "":
		id = "local:" + p.Username
	case p.Issuer != "" && p.Subject != "":
		id = "oidc:" + p.Issuer + "#" + p.Subject
	default:
		return name
	}
	return strings.TrimSpace(name + " <" + id + ">")
}

func answer(q Question) string {
	switch q.Status {
	case "answered":
		return strings.TrimSpace(q.Answer)
	case "withdrawn":
		return "_withdrawn_"
	}
	return "_open_"
}

func day(t *time.Time) string {
	if t == nil || t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.DateOnly)
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// plain is a value YAML reads back as the same string without quotes.
var (
	plain    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 _./()+-]*$`)
	reserved = regexp.MustCompile(`^(?i:true|false|yes|no|on|off|null|~|[0-9][0-9._:+-]*)$`)
)

// scalar writes a value plain when YAML reads it back as the same string,
// else double-quoted with JSON's escapes, which YAML reads the same.
func scalar(s string) string {
	if plain.MatchString(s) && !reserved.MatchString(s) && !strings.HasSuffix(s, " ") {
		return s
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimSuffix(b.String(), "\n")
}
