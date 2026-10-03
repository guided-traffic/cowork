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
	Key, Title, Type, State, Severity, Security, Threat, Urgency, Effort string
	Progress                                                             int
	// Assignee is the display name; Parent the full key.
	Assignee, Parent string
	Opened           time.Time
	Decided, Done    *time.Time
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
	field("urgency", t.Urgency)
	field("effort", t.Effort)
	raw("progress", strconv.Itoa(t.Progress))
	field("assignee", t.Assignee)
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
