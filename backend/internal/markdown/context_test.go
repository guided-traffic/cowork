package markdown

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The context document against golden files (docs/adr/0044 D2): the first
// line, the canonical document, then the sections in their fixed order;
// `go test ./internal/markdown -update` rewrites them.
func TestRenderContext(t *testing.T) {
	ticket := Ticket{
		Key: "acme/VKO-12", Title: "Export drops attachments", Type: "bug", State: "in-progress", Severity: "high",
		Security: "none", Horizon: "now", Effort: "M", ProgressRefinement: 100, Progress: 40,
		Opened: *at("2026-10-01T08:00:00Z"), Decided: at("2026-10-02T09:00:00Z"),
		Assignee:    Person{Name: "Ada Lovelace", Username: "ada"},
		Attachments: []string{"trace.txt"},
		Body:        "## Current state\n\nThe zip has no files.\n",
		Questions:   []Question{{Number: 1, Question: "Zip or tar?", Options: "- A: zip\n- B: tar", Recommendation: "A", Status: "open"}},
	}
	for name, c := range map[string]Context{
		"context-full": {
			Ticket: ticket, Exported: *at("2026-10-04T09:12:00Z"), By: "Ada Lovelace", Agent: "claude-code/unknown/7f3a",
			Links: []Link{
				{Name: "blocked by", Key: "acme/VKO-3", Title: "Fix the writer", State: "in-progress", Assignee: "Sam"},
				{Name: "relates to", Key: "acme/OPS-1", Title: "Backups", State: "filed"},
			},
			Prerequisites: []Prerequisite{
				{Depth: 1, Key: "acme/VKO-3", Title: "Fix the writer", State: "in-progress", Assignee: "Sam", Progress: 40},
				{Depth: 2, Key: "acme/VKO-1", Title: "Pick a library", State: "done", Progress: 100},
			},
			Comments: []Comment{
				{Author: "Sam", At: *at("2026-10-03T10:00:00Z"), Body: "Seen it twice.\n\n## Links\nnot a section"},
				{Author: "Ada Lovelace", Agent: "claude-code/unknown/7f3a", At: *at("2026-10-04T08:00:00Z"), Withdrawn: true},
			},
			Attachments: []Attachment{{Name: "trace.txt", Type: "text/plain; charset=utf-8", Size: 1536,
				URL: "/api/v1/tenants/acme/projects/VKO/tickets/12/attachments/0199a3c2-1d2e-7f00-8000-000000000009/content"}},
			Activity: []Act{
				{At: *at("2026-10-02T09:00:00Z"), Actor: "Ada Lovelace", Action: "transitioned",
					Before: map[string]any{"state": "analysed"}, After: map[string]any{"state": "decided"}},
				{At: *at("2026-10-03T10:00:00Z"), Actor: "Sam", Action: "linked",
					After: map[string]any{"type": "blocks", "source": "acme/VKO-3", "target": "acme/VKO-12"}},
				{At: *at("2026-10-03T11:00:00Z"), Actor: "Sam", Action: "updated", Before: map[string]any{"effort": "S", "title": "x"},
					After: map[string]any{"effort": "M", "title": "y"}, Reason: "bigger than \"it looked\"\nat first"},
				// The acts on the horizon, recorded as overridden (docs/adr/0010 D1).
				{At: *at("2026-10-03T12:00:00Z"), Actor: "Ada Lovelace", Agent: "claude-code/unknown/7f3a", Action: "overridden",
					Before: map[string]any{"urgency_override": "next"}, After: map[string]any{"urgency_override": nil},
					Reason: "not in this release"},
				{At: *at("2026-10-03T13:00:00Z"), Actor: "Sam", Action: "overridden",
					Before: map[string]any{"urgency_override": nil}, After: map[string]any{"urgency_override": "now"},
					Reason: "a customer is down"},
				{At: *at("2026-10-04T08:00:00Z"), Actor: "Ada Lovelace", Agent: "claude-code/unknown/7f3a", Action: "linked", Redacted: true},
			},
		},
		"context-quiet": {
			Ticket: Ticket{Key: "acme/VKO-13", Title: "Nothing around it", Type: "task", State: "filed", Severity: "low",
				Security: "none", Horizon: "later", Effort: "S", Opened: *at("2026-10-01T00:00:00Z")},
			Exported: *at("2026-10-04T09:12:00Z"), By: "Sam",
			Comments: []Comment{}, Activity: nil,
		},
	} {
		got := RenderContext(c)
		path := filepath.Join("testdata", name+".md")
		if *update {
			require.NoError(t, os.WriteFile(path, got, 0o600))
		}
		want, err := os.ReadFile(path)
		require.NoError(t, err, "run with -update to write %s", path)
		assert.Equal(t, string(want), string(got), name)
	}
}

// The context starts with a line that is not frontmatter, so a reader of the
// canonical grammar never takes it for an importable file (docs/adr/0044 D3),
// and the canonical document follows unchanged.
func TestContextCarriesTheCanonicalDocument(t *testing.T) {
	tk := Ticket{Key: "acme/VKO-14", Title: "t", Type: "task", State: "filed", Severity: "low", Security: "none",
		Horizon: "later", Effort: "S", Opened: *at("2026-10-01T00:00:00Z")}
	got := string(RenderContext(Context{Ticket: tk, Exported: *at("2026-10-04T00:00:00Z"), By: "Sam"}))
	first, rest, _ := strings.Cut(got, "\n")
	assert.True(t, strings.HasPrefix(first, "<!-- cowork: context of acme/VKO-14, exported 2026-10-04T00:00:00Z by Sam — not an import format -->"))
	assert.True(t, strings.HasPrefix(rest, string(Render(tk))))
	assert.NotContains(t, got, "## Recent comments", "comments=0 leaves the section out")
	assert.NotContains(t, got, "## Recent activity", "activity=0 leaves the section out")
}

// A person's act through a token is named by the token where the context
// names an agent's act by the agent (docs/adr/0036 D6): the request's own
// line, a comment and an act; a token whose name the act did not record is
// "a token". Everything else reads as before; the other golden files hold
// that.
func TestRenderContextNamesTheTokenOfAPersonsAct(t *testing.T) {
	c := Context{
		Ticket: Ticket{Key: "acme/VKO-15", Title: "Marked", Type: "task", State: "filed", Severity: "low",
			Security: "none", Horizon: "later", Effort: "S", Opened: *at("2026-10-01T00:00:00Z")},
		Exported: *at("2026-10-04T09:12:00Z"), By: "Sam", Token: &Token{Name: "ci-script"},
		Comments: []Comment{
			{Author: "Sam", Token: &Token{Name: "ci-script"}, At: *at("2026-10-03T10:00:00Z"), Body: "Built."},
			{Author: "Ada Lovelace", Agent: "claude-code/opus/7f3a", Token: &Token{Name: "claude-laptop"},
				At: *at("2026-10-03T11:00:00Z"), Body: "Checked."},
			{Author: "Ada Lovelace", At: *at("2026-10-03T12:00:00Z"), Body: "Agreed."},
		},
		Activity: []Act{
			{At: *at("2026-10-03T10:00:00Z"), Actor: "Sam", Token: &Token{Name: "ci-script"}, Action: "commented"},
			{At: *at("2026-10-03T10:30:00Z"), Actor: "Sam", Token: &Token{}, Action: "updated",
				Before: map[string]any{"title": "x"}, After: map[string]any{"title": "Marked"}},
			{At: *at("2026-10-03T11:00:00Z"), Actor: "Ada Lovelace", Agent: "claude-code/opus/7f3a",
				Token: &Token{Name: "claude-laptop"}, Action: "commented"},
			{At: *at("2026-10-03T12:00:00Z"), Actor: "Ada Lovelace", Action: "commented"},
		},
	}
	got := RenderContext(c)
	path := filepath.Join("testdata", "context-token.md")
	if *update {
		require.NoError(t, os.WriteFile(path, got, 0o600))
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err, "run with -update to write %s", path)
	assert.Equal(t, string(want), string(got))
}

// docs/adr/0014 D3: the sort of a project's rank by the score is told apart
// from a move in the rank.
func TestSummaryOfASortByScore(t *testing.T) {
	assert.Equal(t, "ranked by score", summary(Act{Action: "ranked", After: map[string]any{"by": "score", "moved": 3}}))
	assert.Equal(t, "ranked", summary(Act{Action: "ranked", After: map[string]any{"after": "acme/COW-2"}}))
	assert.Equal(t, "ranked (the details name a ticket you cannot see)",
		summary(Act{Action: "ranked", After: map[string]any{"by": "score"}, Redacted: true}))
}

func TestSize(t *testing.T) {
	assert.Equal(t, "512 B", size(512))
	assert.Equal(t, "1.5 KiB", size(1536))
	assert.Equal(t, "2.0 MiB", size(2<<20))
}
