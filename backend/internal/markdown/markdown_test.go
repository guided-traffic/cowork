package markdown

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var update = flag.Bool("update", false, "rewrite the golden files")

func at(s string) *time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return &t
}

// The v1 grammar against golden files (docs/adr/0044 D1, docs/adr/0011 D4);
// `go test ./internal/markdown -update` rewrites them after a deliberate
// change.
func TestRender(t *testing.T) {
	for name, tk := range map[string]Ticket{
		"every-key": {
			Key: "acme/VKO-12", Title: "Export drops attachments: zip is empty", Type: "bug", State: "in-progress",
			Severity: "high", Security: "hardening", Threat: "a crafted name could escape the archive",
			Horizon: "now", Effort: "M", ProgressRefinement: 100, Progress: 40, Assignee: "Ada Lovelace", Parent: "acme/VKO-3",
			Opened: *at("2026-09-30T22:30:00-02:00"), Decided: at("2026-10-01T09:00:00Z"),
			Attachments: []string{"trace.txt", "screen \"1\".png"},
			Body:        "## Current state\n\nThe zip has no files.\n",
		},
		"blocked": {
			Key: "acme/VKO-13", Title: "Wait for the release", Type: "task", State: "blocked", Severity: "low",
			Security: "none", Horizon: "release", Effort: "S", ProgressRefinement: 100, Opened: *at("2026-10-01T00:00:00Z"),
			BlockedBy: "release", BlockedReason: "needs 2.0 out", BlockedFrom: "review",
		},
		"done": {
			Key: "acme/VKO-14", Title: "Fix it", Type: "bug", State: "done", Severity: "medium", Security: "none",
			Horizon: "later", Effort: "XS", ProgressRefinement: 100, Progress: 100, ProgressReview: 100, Opened: *at("2026-10-01T00:00:00Z"),
			Decided: at("2026-10-01T10:00:00Z"), Done: at("2026-10-02T10:00:00Z"),
			Shipped: "make test-integration passed against PostgreSQL 18.6",
		},
		"dropped": {
			Key: "acme/VKO-15", Title: "No", Type: "feature", State: "dropped", Severity: "cosmetic", Security: "none",
			Horizon: "later", Effort: "L", Opened: *at("2026-10-01T00:00:00Z"), DroppedReason: "superseded by VKO-16",
		},
		"questions": {
			Key: "acme/VKO-16", Title: "Café: naïve résumé", Type: "decision", State: "analysed", Severity: "medium",
			Security: "none", Horizon: "icebox", Effort: "S", Opened: *at("2026-10-01T00:00:00Z"),
			Body: "Context first.\n\n## Open questions\n\nThe body's own heading stays above ours.\n",
			Questions: []Question{
				{Number: 1, Question: "Postgres\nor SQLite?", Options: "- A: Postgres\n- B: SQLite", Recommendation: "A",
					Status: "answered", Answer: "Postgres, for the row-level security."},
				{Number: 2, Question: "Which port?", Status: "withdrawn"},
				{Number: 3, Question: "Who reviews?", Status: "open"},
			},
		},
	} {
		got := Render(tk)
		path := filepath.Join("testdata", name+".md")
		if *update {
			require.NoError(t, os.WriteFile(path, got, 0o600))
		}
		want, err := os.ReadFile(path)
		require.NoError(t, err, "run with -update to write %s", path)
		assert.Equal(t, string(want), string(got), name)
	}
}

func TestScalar(t *testing.T) {
	for in, want := range map[string]string{
		"plain words":   "plain words",
		"acme/VKO-12":   "acme/VKO-12",
		"true":          `"true"`,
		"12":            `"12"`,
		"a: b":          `"a: b"`,
		"# not comment": `"# not comment"`,
		"Café":          `"Café"`,
		"line\nbreak":   `"line\nbreak"`,
		"<b>&":          `"<b>&"`,
	} {
		assert.Equal(t, want, scalar(in), in)
	}
}
