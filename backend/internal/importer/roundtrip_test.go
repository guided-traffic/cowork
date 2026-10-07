package importer

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/markdown"
)

// toTicket is the document a parsed export file renders again.
func toTicket(f File) markdown.Ticket {
	tk := markdown.Ticket{
		Key: domain.FullKey(f.Key.Tenant, f.Key.Project, f.Key.Number), Title: f.Title, Type: string(f.Type),
		State: string(f.State), Severity: string(f.Severity), Security: string(f.Security), Threat: f.Threat,
		Confidential: f.Confidential, Horizon: string(f.Horizon), Effort: string(f.Effort),
		ProgressRefinement: f.Stages[0], Progress: f.Stages[1], ProgressReview: f.Stages[2],
		Parent: f.Parent, Decided: f.Decided, Done: f.Done, Shipped: f.Shipped, DroppedReason: f.DroppedReason,
		BlockedBy: f.BlockedBy, BlockedReason: f.BlockedReason, BlockedFrom: f.BlockedFrom,
		Attachments: f.Attachments, Body: f.Body,
	}
	if f.Opened != nil {
		tk.Opened = *f.Opened
	}
	if m := personForm.FindStringSubmatch(f.Assignee); m != nil {
		id, _ := parseIdentity(f.Assignee)
		tk.Assignee = markdown.Person{Name: m[1], Username: id.username, Issuer: id.issuer, Subject: id.subject}
	}
	for _, q := range f.Questions {
		tk.Questions = append(tk.Questions, markdown.Question{Number: int(q.Number), Question: q.Question, Options: q.Options,
			Recommendation: q.Recommendation, Status: q.Status, Answer: q.Answer})
	}
	return tk
}

var keyLine = regexp.MustCompile(`(?m)^key: (\S+)$`)

// docs/adr/0044 D1, D3: what markdown.Render writes is what the importer
// reads back — every golden file of grammar v1 parses without a message and
// renders again to the same bytes; a /context document is refused.
func TestParseReadsTheGoldenFilesOfGrammarV1(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "markdown", "testdata", "*.md"))
	require.NoError(t, err)
	require.NotEmpty(t, paths)
	for _, p := range paths {
		content, err := os.ReadFile(p)
		require.NoError(t, err)
		key := keyLine.FindSubmatch(content)
		require.NotNil(t, key, p)
		f := Parse(string(key[1])+".md", content)
		if strings.HasPrefix(filepath.Base(p), "context-") {
			require.Len(t, f.Errors, 1, p)
			assert.Contains(t, f.Errors[0].Message, "a /context document is no import format", p)
			continue
		}
		assert.Empty(t, f.Errors, p)
		if filepath.Base(p) == "questions.md" {
			// The importer reports a body with a heading of the section's name
			// (docs/adr/0011 Residual risks) and keeps it in the body.
			require.Len(t, f.Warnings, 1, p)
			assert.Contains(t, f.Warnings[0].Message, "the body keeps a ## Open questions heading of its own")
		} else {
			assert.Empty(t, f.Warnings, p)
		}
		assert.Equal(t, FormatExport, f.Format, p)
		assert.Equal(t, string(content), string(markdown.Render(toTicket(f))), p)
	}
}

// docs/adr/0051 D5 in the small: a ticket rendered, read back and rendered
// again is the same document, whatever its values hold — quotes, colons,
// a body with headings and fenced code of its own, every answer form.
func TestRenderParseRenderIsTheSameDocument(t *testing.T) {
	opened := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	decided := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	for name, tk := range map[string]markdown.Ticket{
		"quoted values": {Key: "acme/VKO-1", Title: `"Quoted": no, yes # not a comment`, Type: "task", State: "filed",
			Severity: "low", Security: "boundary", Threat: "a member: reads #1's \\ files", Confidential: true,
			Horizon: "later", Effort: "XS", Opened: opened, Attachments: []string{"a: b.txt", "- dash.png", "#hash"}},
		"a local assignee and a parent": {Key: "acme/VKO-2", Title: "Child", Type: "feature", State: "decided",
			Severity: "medium", Security: "none", Horizon: "next", Effort: "M", ProgressRefinement: 100, Opened: opened,
			Decided: &decided, Parent: "acme/VKO-1", Assignee: markdown.Person{Name: "Grace Hopper", Username: "grace"}},
		"a provider assignee": {Key: "acme/VKO-3", Title: "Ada's", Type: "bug", State: "review", Severity: "critical",
			Security: "none", Horizon: "now", Effort: "L", ProgressRefinement: 100, Progress: 100, ProgressReview: 55,
			Opened: opened, Assignee: markdown.Person{Name: "Ada <Lovelace>", Issuer: "https://id.example.com/realm", Subject: "x#y"}},
		"blocked on a ticket": {Key: "acme/VKO-4", Title: "Waits", Type: "task", State: "blocked", Severity: "low",
			Security: "none", Horizon: "later", Effort: "S", Opened: opened, BlockedBy: "ticket", BlockedReason: "needs: VKO-1",
			BlockedFrom: "in-progress"},
		"a body with its own structure": {Key: "acme/VKO-5", Title: "Structure", Type: "question", State: "analysed",
			Severity: "low", Security: "none", Horizon: "icebox", Effort: "S", Opened: opened,
			Body: "## Current state\n\n```markdown\n## Open questions\n\n### Q1: in a fence?\n**Answer:** no\n```\n\n## Not verified\n\n---\n\nA rule above.",
			Questions: []markdown.Question{
				{Number: 1, Question: "Which?", Options: "- **(a)** one\n- **(b)** two\n\n```\ncode\n```", Recommendation: "**(a)**, because\n\nit is first",
					Status: "answered", Answer: "(a)\n\nRecorded in ADR 0001."},
				{Number: 3, Question: "Gone?", Options: "An option.", Status: "withdrawn"},
				{Number: 7, Question: "Open?", Status: "open"},
			}},
		"done with a note": {Key: "acme/VKO-6", Title: "Done", Type: "bug", State: "done", Severity: "high", Security: "live",
			Threat: "t", Horizon: "later", Effort: "S", ProgressRefinement: 100, Progress: 100, ProgressReview: 100,
			Opened: opened, Decided: &decided, Done: &decided, Shipped: "0.3.0 — fixed: \"quoted\""},
		"dropped with a reason": {Key: "acme/VKO-7", Title: "Dropped", Type: "decision", State: "dropped", Severity: "cosmetic",
			Security: "hardening", Threat: "h", Horizon: "release", Effort: "XS", Opened: opened, DroppedReason: "the owner's (a)"},
	} {
		t.Run(name, func(t *testing.T) {
			doc := markdown.Render(tk)
			f := Parse(tk.Key+".md", doc)
			require.Empty(t, f.Errors, string(doc))
			assert.Empty(t, f.Warnings, string(doc))
			assert.Equal(t, string(doc), string(markdown.Render(toTicket(f))))
		})
	}
}
