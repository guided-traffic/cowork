package importer

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/markdown"
)

// fixtures is testdata/tickets as an upload of docs/tickets/, in path order.
func fixtures(t *testing.T) []Source {
	t.Helper()
	root := filepath.Join("testdata", "tickets")
	var out []Source
	require.NoError(t, filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		content, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out = append(out, Source{Path: fixturePath(filepath.ToSlash(rel)), Content: content})
		return nil
	}))
	return out
}

func target() Target {
	return Target{Tenant: "acme", Project: "COW", Taken: map[int32]bool{}, Purged: map[int32]bool{},
		Existing: map[int32]uuid.UUID{}, Persons: map[string]Person{}, Assignees: map[uuid.UUID]Person{}}
}

func file(t *testing.T, r Result, name string) *FileReport {
	t.Helper()
	for _, f := range r.Report.Files {
		if strings.HasSuffix(f.Path, name) {
			return f
		}
	}
	t.Fatalf("no file %s in the report", name)
	return nil
}

func planned(t *testing.T, r Result, n int32) *PlannedTicket {
	t.Helper()
	for _, p := range r.Plan.Tickets {
		if p.Number == n {
			return p
		}
	}
	t.Fatalf("no planned ticket %d", n)
	return nil
}

func warned(f *FileReport, part string) bool {
	return slices.ContainsFunc(f.Warnings, func(m MessageReport) bool { return strings.Contains(m.Message, part) })
}

// docs/adr/0051 D2, docs/adr/0063 D2–D5, docs/adr/0065 D7: a dry run of the
// fixtures — every file reported with its outcome, the archive imported as
// done or dropped with its note, a record without frontmatter as a done task,
// the references resolved inside the upload or to the project, the rest kept
// as text under ## Related, and the embargo's rule applied.
func TestAnalyzeADryRunOfARepositorysTickets(t *testing.T) {
	tg := target()
	three := uuid.Must(uuid.NewV7())
	tg.Existing[3] = three
	r := Analyze(Read(fixtures(t)), tg, nil)

	assert.Equal(t, Summary{Files: 10, Create: 9, Skip: 1, Open: 3, Confidential: 1, HighestNumber: ptr(int32(99))}, r.Report.Summary)
	assert.Empty(t, r.Blocking())
	readme := file(t, r, "tickets/README.md")
	assert.Equal(t, OutcomeSkip, readme.Outcome)
	assert.Equal(t, skipNotTicketName, *readme.Reason)

	phase := file(t, r, "026-phase-3-the-owner-and-a-second-person-cannot-yet-run-the-daily-work-in-the-browser.md")
	assert.Equal(t, OutcomeCreate, phase.Outcome)
	assert.Equal(t, "acme/COW-26", *phase.Key)
	assert.Equal(t, domain.TypeFeature, *phase.Type)
	assert.Equal(t, `detected: the title says "cannot"`, *phase.TypeReason)
	assert.Equal(t, domain.UrgencyNext, *phase.Columns.Horizon)
	assert.ElementsMatch(t, []LinkReport{
		{Type: domain.LinkFoundIn, Direction: directionIncoming, Key: "acme/COW-28", Source: sourceFiledFrom},
		{Type: domain.LinkFoundIn, Direction: directionIncoming, Key: "acme/COW-99", Source: sourceFiledFrom},
	}, phase.Links)
	assert.True(t, warned(phase, "filed-from names an event"))
	assert.True(t, strings.HasSuffix(planned(t, r, 26).Body, "## Related\n\n- Filed from: docs/planning/project-plan.md phase 3, converted by ADR 0074 D2"))

	layer := file(t, r, "archive/004-there-is-no-data-access-layer.md")
	assert.Equal(t, domain.StateDone, *layer.State)
	assert.True(t, strings.HasPrefix(*layer.Note, "sqlc over pgx"))
	assert.Equal(t, []LinkReport{{Type: domain.LinkBlocks, Direction: directionIncoming, Key: "acme/COW-3", Source: sourceBlockedBy}}, layer.Links,
		"T3 is not in the upload and is a ticket of the project (docs/adr/0051 D9)")
	assert.Contains(t, planned(t, r, 4).Body, "- Filed from: the phase-2 conversion (T2)", "a mention of a ticket the import does not create stays")
	assert.Equal(t, "2026-10-02", *layer.Columns.Done)
	assert.True(t, planned(t, r, 4).DoneByHand, "a repository names no stages: done by hand (docs/adr/0009 D5)")

	record := file(t, r, "archive/007-the-first-analysis-before-one-file-per-ticket.md")
	assert.Equal(t, FormatPlain, *record.Format)
	assert.Equal(t, domain.TypeTask, *record.Type)
	assert.Equal(t, domain.StateDone, *record.State)
	assert.Equal(t, NoteDone, *record.Note)
	assert.Equal(t, domain.Severity("low"), *record.Columns.Severity)
	assert.True(t, warned(record, "a file without frontmatter"))
	assert.Contains(t, planned(t, r, 7).Body, "The data-access layer of acme/COW-4 settles all three.", "T4 is created: the mention is its key")

	frontend := file(t, r, "archive/028-the-frontend-has-no-component-library-no-theme-no-client-and-no-live-updates.md")
	assert.Equal(t, NoteDone, *frontend.Note, "no shipped line: the import note (docs/adr/0063 D2)")
	assert.True(t, warned(frontend, "done without a shipped line"))

	fixed := file(t, r, "archive/045-the-login-page-follows-a-return-path-with-a-control-character-to-another-site.md")
	assert.False(t, fixed.Confidential)
	assert.Contains(t, *fixed.ConfidentialReason, "with a shipped line that names the fix")
	assert.Equal(t, domain.TypeBug, *fixed.Type)

	dropped := file(t, r, "archive/051-the-chart-offers-only-an-ingress-and-no-gateway-api-route.md")
	assert.Equal(t, domain.StateDropped, *dropped.State)
	assert.True(t, strings.HasPrefix(*dropped.Note, "the owner's answer (a)"))

	finding := file(t, r, "local_099-an-open-finding-under-embargo.md")
	assert.True(t, finding.Confidential)
	assert.Contains(t, *finding.ConfidentialReason, "local_ prefix")
	assert.Equal(t, domain.BlockHuman, *finding.BlockCandidate, "blocked is never inferred, the kind is a candidate (docs/adr/0009)")
	assert.Equal(t, domain.StateAnalysed, *finding.State)
	assert.Contains(t, planned(t, r, 99).Body, "## Not verified\n\nNothing.\n\n## Related\n\n- Blocked by: human (blocked-by of docs/tickets/local_099-an-open-finding-under-embargo.md)")
	assert.Equal(t, []QuestionReport{{Number: 1, Question: "Who reviews the fix?", Status: questionOpen}}, finding.Questions)

	gates := file(t, r, "archive/022-six-agent-gates-are-built-open-and-need-a-review-after-experience.md")
	assert.Len(t, gates.Questions, 6)

	numbers := make([]int32, 0, len(r.Plan.Tickets))
	for _, p := range r.Plan.Tickets {
		numbers = append(numbers, p.Number)
	}
	assert.Equal(t, []int32{4, 7, 22, 26, 28, 45, 50, 51, 99}, numbers)
	assert.ElementsMatch(t, []PlannedLink{
		{Type: domain.LinkBlocks, Source: Ref{ID: three}, Target: Ref{Number: 4}},
		{Type: domain.LinkFoundIn, Source: Ref{Number: 28}, Target: Ref{Number: 26}},
		{Type: domain.LinkFoundIn, Source: Ref{Number: 99}, Target: Ref{Number: 26}},
	}, r.Plan.Links)
	assert.EqualValues(t, 99, r.Plan.Highest)

	stored, err := json.Marshal(r.Report)
	require.NoError(t, err)
	var back Report
	require.NoError(t, json.Unmarshal(stored, &back))
	assert.Equal(t, r.Report, back, "the report is stored as JSON and read back the same")
}

// docs/adr/0064 D3, docs/adr/0007 D4: a number the project holds is a
// conflict naming its key, one a purged ticket held as well; a number the
// upload brings twice is an error of both files; a reference to a file the
// import does not create resolves to nothing.
func TestAnalyzeConflictsDuplicatesAndPurgedNumbers(t *testing.T) {
	tg := target()
	tg.Taken[26] = true
	tg.Purged[50] = true
	sources := append(fixtures(t), Source{Path: "other/045-again.md", Content: []byte("# again\n")})
	r := Analyze(Read(sources), tg, nil)

	phase := file(t, r, "026-phase-3-the-owner-and-a-second-person-cannot-yet-run-the-daily-work-in-the-browser.md")
	assert.Equal(t, OutcomeConflict, phase.Outcome)
	assert.Equal(t, "acme/COW-26", *phase.Conflict)
	purged := file(t, r, "050-cowork-mcp-has-not-run-in-a-live-claude-code-session.md")
	assert.Equal(t, OutcomeConflict, purged.Outcome)
	assert.True(t, warned(purged, "was purged; its number is not handed out again"))
	for _, p := range []string{"archive/045-the-login-page-follows-a-return-path-with-a-control-character-to-another-site.md", "other/045-again.md"} {
		f := file(t, r, p)
		assert.Equal(t, OutcomeError, f.Outcome, p)
		assert.Contains(t, f.Errors[0].Message, "the upload brings the number 45 twice", p)
	}
	frontend := file(t, r, "archive/028-the-frontend-has-no-component-library-no-theme-no-client-and-no-live-updates.md")
	assert.Empty(t, frontend.Links)
	assert.True(t, warned(frontend, "T26 is in the upload, but docs/tickets/026-"), "%+v", frontend.Warnings)
	assert.Contains(t, planned(t, r, 28).Body, "- Found in T26, which is not in this import (filed-from of docs/tickets/archive/028-")
	assert.Len(t, r.Blocking(), 4)
	assert.Equal(t, 2, r.Report.Summary.Conflict)
	assert.Equal(t, 2, r.Report.Summary.Error)
}

// docs/adr/0051 D2, docs/adr/0063 D1, docs/adr/0008 D5: the corrections — a
// file left out, a type, a state with its block, an assignee — and what an
// exclusion does to a reference.
func TestAnalyzeAppliesTheCorrections(t *testing.T) {
	tg := target()
	tg.Taken[26] = true
	ada := Person{ID: uuid.Must(uuid.NewV7()), Name: "Ada", Username: ptr("ada")}
	tg.Assignees[ada.ID] = ada
	twentySix := uuid.Must(uuid.NewV7())
	tg.Existing[26] = twentySix
	u := Read(fixtures(t))
	corrections := []Correction{
		{Path: "docs/tickets/026-phase-3-the-owner-and-a-second-person-cannot-yet-run-the-daily-work-in-the-browser.md", Exclude: true},
		{Path: "docs/tickets/archive/045-the-login-page-follows-a-return-path-with-a-control-character-to-another-site.md", Type: domain.TypeTask},
		{Path: "docs/tickets/local_099-an-open-finding-under-embargo.md", State: domain.StateBlocked,
			Block: &BlockCorrection{Kind: domain.BlockHuman, Reason: "waits on a review"}, AssigneeSet: true, Assignee: &ada.ID},
		{Path: "docs/tickets/050-cowork-mcp-has-not-run-in-a-live-claude-code-session.md", State: domain.StateDropped},
	}
	require.Empty(t, u.Check(corrections))
	r := Analyze(u, tg, corrections)
	assert.Empty(t, r.Blocking(), "the conflict of 026 is excluded")

	phase := file(t, r, "026-phase-3-the-owner-and-a-second-person-cannot-yet-run-the-daily-work-in-the-browser.md")
	assert.Equal(t, OutcomeExclude, phase.Outcome)
	assert.Equal(t, "excluded by the correction", *phase.Reason)
	assert.Equal(t, &corrections[0], phase.Correction)
	frontend := file(t, r, "archive/028-the-frontend-has-no-component-library-no-theme-no-client-and-no-live-updates.md")
	assert.Equal(t, []LinkReport{{Type: domain.LinkFoundIn, Direction: directionOutgoing, Key: "acme/COW-26", Source: sourceFiledFrom}}, frontend.Links,
		"an excluded file's number resolves to the project's ticket of that number")

	fixed := file(t, r, "archive/045-the-login-page-follows-a-return-path-with-a-control-character-to-another-site.md")
	assert.Equal(t, domain.TypeTask, *fixed.Type)
	assert.Equal(t, "corrected", *fixed.TypeReason)

	finding := file(t, r, "local_099-an-open-finding-under-embargo.md")
	assert.Equal(t, domain.StateBlocked, *finding.State)
	assert.Equal(t, &BlockReport{Kind: domain.BlockHuman, Reason: "waits on a review", From: domain.StateAnalysed}, finding.Block)
	assert.Equal(t, &AssigneeReport{Source: "", Person: &ada}, finding.Assignee)
	assert.Equal(t, &ada.ID, planned(t, r, 99).Assignee)
	assert.NotContains(t, planned(t, r, 99).Body, "Blocked by: human", "a state blocked reads blocked-by as its block")

	session := file(t, r, "050-cowork-mcp-has-not-run-in-a-live-claude-code-session.md")
	assert.Equal(t, domain.StateDropped, *session.State)
	assert.Equal(t, ReasonDropped, *session.Note)
}

// The rules of the API document's ImportCorrection, each refused at its
// pointer.
func TestCheckRefusesWhatACorrectionCannotSay(t *testing.T) {
	u := Read(fixtures(t))
	const finding = "docs/tickets/local_099-an-open-finding-under-embargo.md"
	const done = "docs/tickets/archive/004-there-is-no-data-access-layer.md"
	human := &BlockCorrection{Kind: domain.BlockHuman, Reason: "why"}
	for _, c := range []struct {
		name    string
		in      []Correction
		pointer string
		message string
	}{
		{"no such file", []Correction{{Path: "docs/tickets/999-none.md"}}, "/corrections/0/path", "names no file of the upload"},
		{"twice", []Correction{{Path: finding}, {Path: finding, Type: domain.TypeBug}}, "/corrections/1/path", "a second correction"},
		{"a skipped file", []Correction{{Path: "docs/tickets/README.md", Exclude: true}}, "/corrections/0/path", "no ticket file"},
		{"exclude and more", []Correction{{Path: finding, Exclude: true, Type: domain.TypeBug}}, "/corrections/0/exclude", "no other correction"},
		{"a block without blocked", []Correction{{Path: finding, State: domain.StateFiled, Block: human}}, "/corrections/0/block", "with the state blocked only"},
		{"blocked without a block", []Correction{{Path: finding, State: domain.StateBlocked}}, "/corrections/0/block", "needs its block"},
		{"a block on a ticket", []Correction{{Path: finding, State: domain.StateBlocked,
			Block: &BlockCorrection{Kind: domain.BlockTicket, Reason: "r"}}}, "/corrections/0/block/kind", "a blocks link"},
		{"from a terminal state", []Correction{{Path: done, State: domain.StateBlocked, Block: human}}, "/corrections/0/block/from", "is no state a block comes from"},
		{"from outside the states", []Correction{{Path: finding, State: domain.StateBlocked,
			Block: &BlockCorrection{Kind: domain.BlockHuman, Reason: "r", From: domain.StateDone}}}, "/corrections/0/block/from", "a block comes from"},
	} {
		t.Run(c.name, func(t *testing.T) {
			errs := u.Check(c.in)
			require.Len(t, errs, 1)
			assert.Equal(t, c.pointer, errs[0].Pointer)
			assert.Contains(t, errs[0].Message, c.message)
		})
	}
	assert.Empty(t, u.Check([]Correction{{Path: done, State: domain.StateBlocked,
		Block: &BlockCorrection{Kind: domain.BlockRelease, Reason: "r", From: domain.StateInProgress}}}))
}

// exportUpload renders tickets of acme/VKO as an export would hold them, with
// its links manifest.
func exportUpload(t *testing.T, tickets []markdown.Ticket, links []manifestLink) []Source {
	t.Helper()
	out := make([]Source, 0, len(tickets)+2)
	manifest, err := json.Marshal(map[string]any{"format": ExportFormat, "tenant": "acme", "projects": []map[string]any{{"key": "VKO"}}})
	require.NoError(t, err)
	out = append(out, Source{Path: ManifestFile, Content: manifest})
	raw, err := json.Marshal(links)
	require.NoError(t, err)
	out = append(out, Source{Path: LinksFile, Content: raw})
	for _, tk := range tickets {
		out = append(out, Source{Path: tk.Key + ".md", Content: markdown.Render(tk)})
	}
	return out
}

func vko(n int, title string, change func(*markdown.Ticket)) markdown.Ticket {
	tk := markdown.Ticket{Key: domain.FullKey("acme", "VKO", int32(n)), Title: title, Type: "task", State: "filed",
		Severity: "low", Security: "none", Horizon: "later", Effort: "S"}
	if change != nil {
		change(&tk)
	}
	return tk
}

// docs/adr/0051 D4, D9, docs/adr/0044 D1: an export read back — its links
// made inside the upload and to the project, one to another project
// reported and omitted, a block on a ticket resolved through the links, a
// parent, a cycle of blocks omitted, a loop of parents refused, the
// confidential flag kept, an assignee resolved by identity only.
func TestAnalyzeAnExport(t *testing.T) {
	grace := Person{ID: uuid.Must(uuid.NewV7()), Name: "Grace", Username: ptr("grace")}
	tg := target()
	tg.Project = "NEW"
	tg.Issuer = "https://id.example.com"
	tg.Persons[IdentityOf("grace", "", "")] = grace
	existing := uuid.Must(uuid.NewV7())
	tg.Existing[9] = existing
	tickets := []markdown.Ticket{
		vko(1, "Parent", func(tk *markdown.Ticket) { tk.Assignee = markdown.Person{Name: "Grace", Username: "grace"} }),
		vko(2, "Child", func(tk *markdown.Ticket) { tk.Parent = "acme/VKO-1" }),
		vko(3, "Waits", func(tk *markdown.Ticket) {
			tk.State, tk.BlockedBy, tk.BlockedReason, tk.BlockedFrom = "blocked", "ticket", "needs VKO-1", "in-progress"
		}),
		vko(4, "Secret", func(tk *markdown.Ticket) {
			tk.Security, tk.Threat, tk.Confidential = "hardening", "a hand-set flag", true
			tk.Assignee = markdown.Person{Name: "Mallory", Issuer: "https://other.example.com", Subject: "m"}
		}),
		vko(5, "Loop one", func(tk *markdown.Ticket) { tk.Parent = "acme/VKO-6" }),
		vko(6, "Loop two", func(tk *markdown.Ticket) { tk.Parent = "acme/VKO-5" }),
	}
	links := []manifestLink{
		{Source: "acme/VKO-1", Type: "blocks", Target: "acme/VKO-3"},
		{Source: "acme/VKO-3", Type: "blocks", Target: "acme/VKO-1"},
		{Source: "acme/VKO-2", Type: "relates-to", Target: "acme/VKO-9"},
		{Source: "acme/VKO-2", Type: "relates-to", Target: "acme/OTHER-1"},
		{Source: "acme/VKO-1", Type: "duplicates", Target: "acme/VKO-1"},
		{Source: "acme/OTHER-2", Type: "blocks", Target: "acme/OTHER-3"},
	}
	r := Analyze(Read(exportUpload(t, tickets, links)), tg, nil)

	assert.Equal(t, OutcomeSkip, file(t, r, ManifestFile).Outcome)
	assert.Equal(t, "the manifest of an export of acme/VKO (docs/adr/0051 D4)", *file(t, r, ManifestFile).Reason)
	assert.Contains(t, *file(t, r, LinksFile).Reason, "6 links")

	parent := file(t, r, "acme/VKO-1.md")
	assert.Equal(t, FormatExport, *parent.Format)
	assert.Equal(t, "acme/NEW-1", *parent.Key)
	assert.Equal(t, "named by the file", *parent.TypeReason)
	assert.Equal(t, &grace, parent.Assignee.Person)
	assert.True(t, warned(parent, "a link from the ticket to itself"))
	assert.True(t, warned(file(t, r, "acme/VKO-3.md"), "it would close a cycle"))

	child := file(t, r, "acme/VKO-2.md")
	assert.Equal(t, "acme/NEW-1", *child.Parent)
	assert.ElementsMatch(t, []LinkReport{{Type: domain.LinkRelatesTo, Direction: directionOutgoing, Key: "acme/NEW-9", Source: sourceLinks}}, child.Links)
	assert.True(t, warned(child, "acme/OTHER-1 is a ticket of another project"))

	waits := file(t, r, "acme/VKO-3.md")
	assert.Equal(t, &BlockReport{Kind: domain.BlockTicket, Reason: "needs VKO-1", From: domain.StateInProgress, Ticket: ptr("acme/NEW-1")}, waits.Block)

	secret := file(t, r, "acme/VKO-4.md")
	assert.True(t, secret.Confidential)
	assert.Equal(t, "the export marks it confidential", *secret.ConfidentialReason)
	assert.Nil(t, secret.Assignee.Person)
	assert.True(t, warned(secret, "of another issuer"))

	for _, p := range []string{"acme/VKO-5.md", "acme/VKO-6.md"} {
		assert.Equal(t, OutcomeError, file(t, r, p).Outcome, p)
	}
	numbers := make([]int32, 0, len(r.Plan.Tickets))
	for _, p := range r.Plan.Tickets {
		numbers = append(numbers, p.Number)
	}
	assert.Equal(t, []int32{1, 2, 3, 4}, numbers, "parents first, by number")
	assert.ElementsMatch(t, []PlannedLink{
		{Type: domain.LinkBlocks, Source: Ref{Number: 1}, Target: Ref{Number: 3}},
		{Type: domain.LinkRelatesTo, Source: Ref{Number: 2}, Target: Ref{ID: existing}},
	}, r.Plan.Links, "the block's link and the links.json one are the same link, made once")
}

// A blocks cycle among the files of a repository and a block on a ticket
// nothing resolves: the one omitted with a warning, the other an error.
func TestAnalyzeRefusesWhatABlockCannotBe(t *testing.T) {
	src := func(name, front string) Source {
		return Source{Path: "docs/tickets/" + name, Content: []byte("---\n" + front +
			"title: x\nseverity: low\nsecurity: none\neffort: S\nopened: 2026-10-01\n---\n")}
	}
	r := Analyze(Read([]Source{
		src("001-a.md", "id: T1\nstate: filed\nblocked-by: T2\n"),
		src("002-b.md", "id: T2\nstate: filed\nblocked-by: T1\n"),
		src("003-c.md", "id: T3\nstate: blocked\nblocked-by: T9\nblocked-reason: waits\nblocked-from: filed\n"),
		src("004-d.md", "id: T4\nstate: blocked\nblocked-by: human\n"),
	}), target(), nil)
	assert.Len(t, r.Plan.Links, 1, "the second link of the cycle is omitted")
	assert.True(t, warned(file(t, r, "002-b.md"), "would close a cycle"))
	c := file(t, r, "003-c.md")
	assert.Equal(t, OutcomeError, c.Outcome)
	assert.Contains(t, c.Errors[0].Message, "T9 is neither in the upload nor a ticket of the project")
	d := file(t, r, "004-d.md")
	assert.Equal(t, OutcomeError, d.Outcome)
	assert.Contains(t, d.Errors[0].Message, "needs its blocked-reason")
}
