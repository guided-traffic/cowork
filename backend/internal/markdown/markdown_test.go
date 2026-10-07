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
			Horizon: "now", Effort: "M", ProgressRefinement: 100, Progress: 40, Parent: "acme/VKO-3",
			Opened: *at("2026-09-30T22:30:00-02:00"), Decided: at("2026-10-01T09:00:00Z"),
			Assignee:    Person{Name: "Ada Lovelace", Issuer: "https://login.example.com", Subject: "CgNhZGESBWxvY2Fs"},
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
		// The flag travels with the document, so an import sets it again
		// (docs/adr/0065 D7, docs/adr/0044 D1 as amended 2026-10-06).
		"confidential": {
			Key: "acme/VKO-17", Title: "Session fixation on the login", Type: "bug", State: "analysed", Severity: "high",
			Security: "live", Threat: "a person who can set a cookie on the domain takes over a member's session",
			Confidential: true, Horizon: "now", Effort: "S", Opened: *at("2026-10-05T00:00:00Z"),
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

// docs/adr/0044 D1: a person is `Name <identity>`, the way git writes an
// author — local:<username> for a local account, oidc:<issuer>#<subject> for
// a person of the identity provider. The name loses its angle brackets, so
// the first < starts the identity; a person without an identity keeps the
// name alone, and no person writes nothing.
func TestPerson(t *testing.T) {
	for _, c := range []struct {
		in   Person
		want string
	}{
		{Person{Name: "Ada Lovelace", Username: "ada"}, "Ada Lovelace <local:ada>"},
		{Person{Name: "Ada Lovelace", Issuer: "https://login.example.com/dex", Subject: "CgNhZGESBWxvY2Fs"},
			"Ada Lovelace <oidc:https://login.example.com/dex#CgNhZGESBWxvY2Fs>"},
		{Person{Name: "Sam", Issuer: "https://login.example.com", Subject: "a#b>c"},
			"Sam <oidc:https://login.example.com#a#b>c>"},
		{Person{Name: "  <Ada>  the\n<first>  ", Username: "ada"}, "Ada the first <local:ada>"},
		{Person{Name: "<>", Username: "ada"}, "<local:ada>"},
		{Person{Name: "Ada Lovelace", Issuer: "https://login.example.com"}, "Ada Lovelace"},
		{Person{Name: "Ada Lovelace"}, "Ada Lovelace"},
		{Person{}, ""},
	} {
		assert.Equal(t, c.want, person(c.in), "%+v", c.in)
	}
	assert.Contains(t, string(Render(Ticket{Assignee: Person{Name: "Ada Lovelace", Username: "ada"}})),
		"\nassignee: \"Ada Lovelace <local:ada>\"\n", "quoted, since a plain YAML value has no < or :")
	assert.NotContains(t, string(Render(Ticket{})), "assignee:", "an unassigned ticket has no key")
}
