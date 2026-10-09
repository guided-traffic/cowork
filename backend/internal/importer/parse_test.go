package importer

import (
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/domain"
)

// embargoedFixture is how testdata names a file whose path carries the
// embargo's local_ prefix: a local_ file is ignored by many a git
// configuration, and the fixture must be in every clone.
const embargoedFixture = "embargoed-"

// fixturePath is the path a fixture of testdata/tickets has in the upload.
func fixturePath(name string) string {
	dir, base := path.Split(name)
	return "docs/tickets/" + dir + strings.Replace(base, embargoedFixture, "local_", 1)
}

// fixture reads a file of testdata/tickets, the copies of this repository's
// own ticket files and the few made for the shapes it does not hold.
func fixture(t *testing.T, name string) File {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("testdata", "tickets", name))
	require.NoError(t, err)
	return Parse(fixturePath(name), content)
}

func date(s string) *time.Time {
	d, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return &d
}

// docs/adr/0063 Consequences: the four shapes the importer's fixtures hold —
// an open ticket, an archived one with frontmatter, an archived record
// without, and a file that is no ticket.
func TestParseTheFourShapesOfARepository(t *testing.T) {
	open := fixture(t, "026-phase-3-the-owner-and-a-second-person-cannot-yet-run-the-daily-work-in-the-browser.md")
	assert.Empty(t, open.Errors)
	assert.Equal(t, FormatRepository, open.Format)
	assert.EqualValues(t, 26, open.Number)
	assert.Equal(t, domain.StateInProgress, open.State)
	assert.Equal(t, domain.Severity("high"), open.Severity)
	assert.Equal(t, domain.SecurityNone, open.Security)
	assert.Equal(t, domain.UrgencyNext, open.Horizon, "urgency is read as horizon, its comment left out (docs/adr/0044 D3)")
	assert.Equal(t, domain.Effort("L"), open.Effort)
	assert.Equal(t, "docs/planning/project-plan.md phase 3, converted by ADR 0074 D2", open.FiledFrom)
	assert.Equal(t, date("2026-10-03"), open.Opened)
	assert.Equal(t, date("2026-10-03"), open.Decided)
	assert.Nil(t, open.Done)
	assert.True(t, strings.HasPrefix(open.Body, "## Current state\n\nThe family ticket of phase 3"), open.Body)
	assert.False(t, open.Local)

	archived := fixture(t, "archive/004-there-is-no-data-access-layer.md")
	assert.Empty(t, archived.Errors)
	assert.EqualValues(t, 4, archived.Number)
	assert.Equal(t, domain.StateDone, archived.State)
	assert.Equal(t, domain.SecurityHardening, archived.Security)
	assert.True(t, strings.HasPrefix(archived.Threat, "makes a query outside a transaction"))
	assert.Equal(t, "T3", archived.BlockedBy)
	assert.Equal(t, "the phase-2 conversion (T2)", archived.FiledFrom)
	assert.Equal(t, date("2026-10-02"), archived.Done)
	assert.True(t, strings.HasPrefix(archived.Shipped, "sqlc over pgx"))

	record := fixture(t, "archive/007-the-first-analysis-before-one-file-per-ticket.md")
	assert.Empty(t, record.Errors)
	assert.Equal(t, FormatPlain, record.Format)
	assert.EqualValues(t, 7, record.Number)
	assert.Equal(t, "The first analysis, before one file per ticket", record.Title)
	assert.Contains(t, record.Body, "1. The pool is exported")

	notATicket := fixture(t, "README.md")
	assert.Equal(t, skipNotTicketName, notATicket.Skip)
}

// docs/tickets/README.md: the embargo's local_ prefix is read from the name,
// and the questions of a repository's section, which sections follow.
func TestParseAnEmbargoedFileWithItsQuestions(t *testing.T) {
	f := fixture(t, "embargoed-099-an-open-finding-under-embargo.md")
	assert.Empty(t, f.Errors)
	assert.True(t, f.Local)
	assert.EqualValues(t, 99, f.Number)
	assert.Equal(t, domain.SecurityLive, f.Security)
	assert.Equal(t, "human", f.BlockedBy)
	require.Len(t, f.Questions, 1)
	q := f.Questions[0]
	assert.Equal(t, Question{Number: 1, Question: "Who reviews the fix?", Options: "- A person of the team.\n- An outside reviewer.",
		Status: questionOpen, Line: q.Line}, q)
	assert.Equal(t, "## Current state\n\nA finding that is not fixed yet; its file carries the `local_` prefix, as T26's\n"+
		"rules for an embargo say.\n\n## Not verified\n\nNothing.", f.Body,
		"the sections after ## Open questions stay in the body, before the questions grammar v1 writes last")
}

// docs/adr/0011 D4: a repository's answered questions — the recommendation
// inside the options text — and an open one.
func TestParseTheQuestionsOfARepository(t *testing.T) {
	f := fixture(t, "archive/022-six-agent-gates-are-built-open-and-need-a-review-after-experience.md")
	assert.Empty(t, f.Errors)
	require.Len(t, f.Questions, 6)
	for i, q := range f.Questions {
		assert.EqualValues(t, i+1, q.Number)
		assert.Equal(t, questionAnswered, q.Status, "Q%d", q.Number)
		assert.Empty(t, q.Recommendation, "a repository writes the recommendation into the options")
	}
	assert.Equal(t, "Is removing an open `blocks` prerequisite an agent act?", f.Questions[0].Question)
	assert.True(t, strings.HasPrefix(f.Questions[0].Options, "- **(a) Keep it allowed:**"), f.Questions[0].Options)
	assert.True(t, strings.HasPrefix(f.Questions[0].Answer, "(a) — keep it allowed."), f.Questions[0].Answer)
	assert.NotContains(t, f.Body, "### Q1")

	open := fixture(t, "050-cowork-mcp-has-not-run-in-a-live-claude-code-session.md")
	require.Len(t, open.Questions, 1)
	assert.Equal(t, questionOpen, open.Questions[0].Status)
}

// docs/adr/0065 D7: a boundary finding whose shipped line names the fix, and
// a dropped ticket with its reason.
func TestParseAFixedFindingAndADroppedTicket(t *testing.T) {
	fixed := fixture(t, "archive/045-the-login-page-follows-a-return-path-with-a-control-character-to-another-site.md")
	assert.Empty(t, fixed.Errors)
	assert.Equal(t, domain.SecurityBoundary, fixed.Security)
	assert.True(t, strings.HasPrefix(fixed.Shipped, "0.3.0"))

	dropped := fixture(t, "archive/051-the-chart-offers-only-an-ingress-and-no-gateway-api-route.md")
	assert.Empty(t, dropped.Errors)
	assert.Equal(t, domain.StateDropped, dropped.State)
	assert.Equal(t, domain.UrgencyIcebox, dropped.Horizon)
	assert.True(t, strings.HasPrefix(dropped.DroppedReason, "the owner's answer (a)"))
}

// docs/adr/0051 D2, docs/adr/0010 D5, docs/adr/0044 D3: every class of error
// a file can have, each with its field and, where it has one, its line.
func TestParseNamesEveryErrorOfAFile(t *testing.T) {
	const head = "---\nid: T5\ntitle: Five\nstate: filed\nseverity: low\nsecurity: none\neffort: S\nopened: 2026-10-01\n"
	for _, c := range []struct {
		name, content, field, message string
		line                          int
	}{
		{"out of vocabulary", strings.Replace(head, "severity: low", "severity: urgent", 1) + "---\n", keySeverity, `severity: "urgent" is outside the vocabulary`, 5},
		{"a state outside the vocabulary", strings.Replace(head, "state: filed", "state: waiting", 1) + "---\n", keyState, `state: "waiting" is outside`, 4},
		// The YAML parser names the line its context starts on.
		{"malformed frontmatter", head + "{broken\n---\n", fieldFile, "the frontmatter is not YAML", 9},
		{"a key twice", head + "title: Again\n---\n", fieldFile, "names title twice, also on line 3", 9},
		{"no closing line", head, fieldFile, "has no closing --- line", 1},
		{"no mapping", "---\n- one\n- two\n---\n", fieldFile, "is not a list of key: value lines", 2},
		{"urgency and horizon disagree", head + "urgency: now\nhorizon: later\n---\n", keyUrgency, "urgency now and horizon later disagree", 9},
		{"no title", strings.Replace(head, "title: Five\n", "", 1) + "---\n", keyTitle, "names no title", 0},
		{"an empty state", strings.Replace(head, "state: filed", "state:", 1) + "---\n", keyState, "names no state", 4},
		{"a class without its threat", strings.Replace(head, "security: none", "security: live", 1) + "---\n", keyThreat, "security live needs the threat it names", 0},
		{"a threat without a class", head + "threat: something\n---\n", keyThreat, "carries no threat", 9},
		{"no date", strings.Replace(head, "opened: 2026-10-01", "opened: yesterday", 1) + "---\n", keyOpened, `opened: "yesterday" is not a date`, 8},
		{"a stage off the steps", head + "progress: 42\n---\n", "progress", `progress: "42" is not 0 to 100 in steps of five`, 9},
		{"an id the name contradicts", strings.Replace(head, "id: T5", "id: T6", 1) + "---\n", keyID, "names the number 6, the file name 5", 2},
		{"an id that is none", strings.Replace(head, "id: T5", "id: five", 1) + "---\n", keyID, "is not T and a number", 2},
		{"a list where a value belongs", head + "shipped:\n  - a\n---\n", keyShipped, "takes one value", 9},
		{"a question twice", head + "---\n\n## Open questions\n\n### Q1: One?\n\n**Answer:** _open_\n\n### Q1: Again?\n\n**Answer:** _open_\n", "Q1", "Q1 appears twice, also on line 13", 17},
		{"a question without its text", head + "---\n\n## Open questions\n\n### Q1:\n\n**Answer:** _open_\n", "Q1", "Q1 has no question", 13},
		{"no UTF-8", "---\nid: T5\ntitle: \xff\n---\n", fieldFile, "not UTF-8", 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := Parse("docs/tickets/005-five.md", []byte(c.content))
			require.NotEmpty(t, f.Errors, "%+v", f)
			found := false
			for _, e := range f.Errors {
				if e.Field == c.field && strings.Contains(e.Message, c.message) {
					found = true
					assert.Equal(t, c.line, e.Line, e.Message)
				}
			}
			assert.True(t, found, "no error %q on %s in %+v", c.message, c.field, f.Errors)
		})
	}
}

// docs/adr/0044 D3, docs/adr/0063 D5: a name that is no ticket file's is
// skipped, and so is a /context document, whatever its name; an export's
// name reads its number, and an unknown key is a warning, its value not read.
func TestParseNamesAndUnknownKeys(t *testing.T) {
	for _, name := range []string{"README.md", "notes.md", "docs/tickets/12-short.md", "acme/vko-12.md", "._001-apple.md"} {
		assert.Equal(t, skipNotTicketName, Parse(name, []byte("---\n---\n")).Skip, name)
	}
	context := Parse("acme/VKO-5.md", []byte("\xef\xbb\xbf<!-- cowork: context of acme/VKO-5, exported 2026-10-04T09:12:00Z by Ada — not an import format -->\n"+
		"---\nkey: acme/VKO-5\n---\n\n## Links\n\nNone.\n"))
	assert.Equal(t, skipContext, context.Skip)
	assert.Empty(t, context.Errors)
	f := Parse("acme/VKO-12.md", []byte("---\nkey: acme/VKO-12\ntitle: T\ntype: bug\nstate: filed\nseverity: low\n"+
		"security: none\nhorizon: now\neffort: S\nopened: 2026-10-01\nsprint: 7\n---\n"))
	assert.Empty(t, f.Errors)
	assert.Equal(t, FormatExport, f.Format)
	assert.EqualValues(t, 12, f.Number)
	assert.Equal(t, domain.TicketKey{Tenant: "acme", Project: "VKO", Number: 12}, f.Key)
	require.Len(t, f.Warnings, 1)
	assert.Equal(t, Message{Field: "sprint", Line: 11, Message: "the key sprint is not read"}, f.Warnings[0])

	other := Parse("acme/VKO-12.md", []byte("---\nkey: acme/VKO-13\n---\n"))
	assert.True(t, other.failed(keyKey), "the key's number and the name's disagree: %+v", other.Errors)
}

// docs/adr/0011 D4: the last ## Open questions heading outside fenced code
// holds the questions; one inside a fence, or an earlier one, is the body's.
func TestParseFindsTheQuestionsOutsideFencedCode(t *testing.T) {
	text := "---\nid: T5\ntitle: Five\nstate: filed\nseverity: low\nsecurity: none\neffort: S\nopened: 2026-10-01\n---\n\n" +
		"## Current state\n\n```markdown\n## Open questions\n\n### Q9: In a fence?\n```\n\n" +
		"## Open questions\n\nText before the first one.\n\n### Q2: Real?\n\nOptions.\n\n**Recommendation:** yes\n\n**Answer:** _withdrawn_\n\n## Related\n\n- T4\n"
	f := Parse("docs/tickets/005-five.md", []byte(text))
	assert.Empty(t, f.Errors)
	require.Len(t, f.Questions, 1)
	q := f.Questions[0]
	assert.EqualValues(t, 2, q.Number)
	assert.Equal(t, "Text before the first one.\n\nOptions.", q.Options, "a preamble joins the first question's options")
	assert.Equal(t, "yes", q.Recommendation)
	assert.Equal(t, questionWithdrawn, q.Status)
	assert.Equal(t, "## Current state\n\n```markdown\n## Open questions\n\n### Q9: In a fence?\n```\n\n## Related\n\n- T4", f.Body)
	require.Len(t, f.Warnings, 1)
	assert.Equal(t, Message{Field: fieldBody, Line: 20, Message: "text before the first question joins Q2's options"}, f.Warnings[0])
}
