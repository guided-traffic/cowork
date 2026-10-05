package markdown

import (
	"bytes"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Context is a ticket for reading (docs/adr/0044 D2): the canonical document
// under one first line that says it is no import format, and the read-only
// sections around it in a fixed order. Like Render, it is a pure function of
// its input.
type Context struct {
	Ticket Ticket
	// Exported is when, By who and Agent through which agent the document
	// was made; Agent is empty for a person's request. Token is the token a
	// request came through, as the document names it where no agent made it.
	Exported  time.Time
	By, Agent string
	Token     *Token
	Links     []Link
	// Prerequisites is the tree, depth first.
	Prerequisites []Prerequisite
	// Comments and Activity are nil when the request left their sections
	// out (comments=0, activity=0); otherwise oldest of them first.
	Comments    []Comment
	Activity    []Act
	Attachments []Attachment
}

// Link is a link read from the ticket: its name from this side and the
// other end.
type Link struct {
	Name, Key, Title, State, Assignee string
}

// Prerequisite is a node of the prerequisite tree; Depth 1 blocks the ticket
// itself.
type Prerequisite struct {
	Depth                       int
	Key, Title, State, Assignee string
	Progress                    int
}

// Token is the token an act came through (docs/adr/0036 D6): its name, empty
// where the act did not record one. A nil *Token is a browser session's act.
type Token struct {
	Name string
}

// Comment is one comment of the thread; a withdrawn one has no text.
type Comment struct {
	Author, Agent string
	Token         *Token
	At            time.Time
	Body          string
	Withdrawn     bool
}

// Attachment is an attachment's metadata; its content is never in a
// document (docs/adr/0044 D6).
type Attachment struct {
	Name, Type, URL string
	Size            int64
}

// Act is one entry of the activity list. Before and After are the changed
// fields; Redacted is an act whose payload names a ticket the reader cannot
// see (docs/adr/0065 D4).
type Act struct {
	At                   time.Time
	Actor, Agent, Action string
	Token                *Token
	Before, After        map[string]any
	Reason, Note         string
	Redacted             bool
}

// maxQuoted bounds a reason or a note quoted in an activity line.
const maxQuoted = 200

// RenderContext returns the context document.
func RenderContext(c Context) []byte {
	var b bytes.Buffer
	by := c.By
	if mark := via(c.Agent, c.Token); mark != "" {
		by += " (" + strings.TrimPrefix(mark, " ") + ")"
	}
	fmt.Fprintf(&b, "<!-- cowork: context of %s, exported %s by %s — not an import format -->\n",
		c.Ticket.Key, c.Exported.UTC().Format(time.RFC3339), oneLine(by))
	b.Write(Render(c.Ticket))
	writeLinks(&b, c.Links)
	writePrerequisites(&b, c.Prerequisites)
	if c.Comments != nil {
		writeComments(&b, c.Comments)
	}
	writeAttachments(&b, c.Attachments)
	if c.Activity != nil {
		writeActivity(&b, c.Activity)
	}
	return b.Bytes()
}

func writeLinks(b *bytes.Buffer, links []Link) {
	b.WriteString("\n## Links\n\n")
	if len(links) == 0 {
		b.WriteString("None.\n")
		return
	}
	for _, l := range links {
		fmt.Fprintf(b, "- %s %s — %s (%s, %s)\n", l.Name, l.Key, oneLine(l.Title), l.State, assignee(l.Assignee))
	}
}

func writePrerequisites(b *bytes.Buffer, tree []Prerequisite) {
	b.WriteString("\n## Prerequisites\n\n")
	if len(tree) == 0 {
		b.WriteString("None.\n")
		return
	}
	open := 0
	for _, p := range tree {
		if p.State != "done" && p.State != "dropped" {
			open++
		}
	}
	fmt.Fprintf(b, "%d of %d open.\n\n", open, len(tree))
	for _, p := range tree {
		fmt.Fprintf(b, "%s- %s — %s (%s, %s, %d%%)\n", strings.Repeat("  ", max(p.Depth-1, 0)), p.Key, oneLine(p.Title),
			p.State, assignee(p.Assignee), p.Progress)
	}
}

// writeComments quotes each comment under its author: what other people and
// agents wrote is shown as quoted text, never as part of the document's own
// structure.
func writeComments(b *bytes.Buffer, comments []Comment) {
	b.WriteString("\n## Recent comments\n")
	if len(comments) == 0 {
		b.WriteString("\nNone.\n")
		return
	}
	for _, c := range comments {
		fmt.Fprintf(b, "\n**%s**%s, %s:", oneLine(c.Author), via(c.Agent, c.Token), stamp(c.At))
		if c.Withdrawn {
			b.WriteString(" [withdrawn]\n")
			continue
		}
		b.WriteString("\n\n")
		for _, line := range strings.Split(strings.TrimSpace(c.Body), "\n") {
			b.WriteString(strings.TrimRight("> "+line, " ") + "\n")
		}
	}
}

func writeAttachments(b *bytes.Buffer, attachments []Attachment) {
	b.WriteString("\n## Attachments\n\n")
	if len(attachments) == 0 {
		b.WriteString("None.\n")
		return
	}
	for _, a := range attachments {
		fmt.Fprintf(b, "- %s — %s, %s — %s\n", oneLine(a.Name), a.Type, size(a.Size), a.URL)
	}
}

func writeActivity(b *bytes.Buffer, acts []Act) {
	b.WriteString("\n## Recent activity\n\n")
	if len(acts) == 0 {
		b.WriteString("None.\n")
		return
	}
	for _, a := range acts {
		fmt.Fprintf(b, "- %s — %s%s — %s\n", stamp(a.At), oneLine(a.Actor), via(a.Agent, a.Token), summary(a))
	}
}

// summary is an act in one line: what was done and, unless the act is
// redacted, what it changed and why.
func summary(a Act) string {
	if a.Redacted {
		return a.Action + " (the details name a ticket you cannot see)"
	}
	s := a.Action
	switch {
	case a.Action == "transitioned" && a.Before["state"] != nil && a.After["state"] != nil:
		s += fmt.Sprintf(": %v → %v", a.Before["state"], a.After["state"])
	case (a.Action == "linked" || a.Action == "unlinked") && linkPayload(a) != nil:
		p := linkPayload(a)
		s += fmt.Sprintf(": %v %v %v", p["source"], p["type"], p["target"])
	case a.Action == "updated":
		if fields := changed(a); len(fields) > 0 {
			s += " " + strings.Join(fields, ", ")
		}
	case a.Action == "ranked" && a.After["by"] == "score":
		// The sort of the project's rank by the score (docs/adr/0014 D3).
		s += " by score"
	}
	if a.Reason != "" {
		s += " — reason: " + quoted(a.Reason)
	}
	if a.Note != "" {
		s += " — note: " + quoted(a.Note)
	}
	return s
}

func linkPayload(a Act) map[string]any {
	p := a.After
	if a.Action == "unlinked" {
		p = a.Before
	}
	if p["source"] == nil || p["target"] == nil {
		return nil
	}
	return p
}

// changed are the names of the fields an act changed.
func changed(a Act) []string {
	seen := map[string]bool{}
	for _, m := range []map[string]any{a.Before, a.After} {
		for k := range m {
			seen[k] = true
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func quoted(s string) string {
	s = oneLine(s)
	if utf8.RuneCountInString(s) > maxQuoted {
		s = string([]rune(s)[:maxQuoted]) + "…"
	}
	return strconv.Quote(s)
}

func assignee(name string) string {
	if name == "" {
		return "unassigned"
	}
	return oneLine(name)
}

// via says how an act was made beside its person: by the agent, or through
// the token where no agent made it (docs/adr/0036 D6); nothing for the
// person's own browser session.
func via(agent string, token *Token) string {
	switch {
	case agent != "":
		return " via " + agent
	case token == nil:
		return ""
	case oneLine(token.Name) == "":
		return " through a token"
	default:
		return " through the token " + oneLine(token.Name)
	}
}

func stamp(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04 UTC")
}

// size writes a byte count the way a person reads it.
func size(n int64) string {
	switch {
	case n < 1024:
		return strconv.FormatInt(n, 10) + " B"
	case n < 1024*1024:
		return strconv.FormatFloat(float64(n)/1024, 'f', 1, 64) + " KiB"
	default:
		return strconv.FormatFloat(float64(n)/(1024*1024), 'f', 1, 64) + " MiB"
	}
}
