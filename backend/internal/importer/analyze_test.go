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

// docs/adr/0064 D3, docs/adr/0007 D4, docs/adr/0051 D2: a number the project
// holds is a conflict naming its key, and one a purged ticket held is given
// back with a warning; a number the upload brings twice is an error of both
// files; each is left out of the plan, the report saying why, and a
// reference to a file the import does not create resolves to nothing.
func TestAnalyzeConflictsDuplicatesAndPurgedNumbers(t *testing.T) {
	tg := target()
	tg.Taken[26] = true
	tg.Purged[50] = true
	sources := append(fixtures(t), Source{Path: "other/045-again.md", Content: []byte("# again\n")})
	r := Analyze(Read(sources), tg, nil)

	phase := file(t, r, "026-phase-3-the-owner-and-a-second-person-cannot-yet-run-the-daily-work-in-the-browser.md")
	assert.Equal(t, OutcomeConflict, phase.Outcome)
	assert.Equal(t, "acme/COW-26", *phase.Conflict)
	assert.Equal(t, "left out of the import: the project holds its number as acme/COW-26 (docs/adr/0064 D3)", *phase.Reason)
	purged := file(t, r, "050-cowork-mcp-has-not-run-in-a-live-claude-code-session.md")
	assert.Equal(t, OutcomeCreate, purged.Outcome)
	assert.Nil(t, purged.Conflict)
	assert.True(t, warned(purged, "acme/COW-50 was purged; the import gives its number back"))
	for _, p := range []string{"archive/045-the-login-page-follows-a-return-path-with-a-control-character-to-another-site.md", "other/045-again.md"} {
		f := file(t, r, p)
		assert.Equal(t, OutcomeError, f.Outcome, p)
		assert.Contains(t, f.Errors[0].Message, "the upload brings the number 45 twice", p)
		assert.Contains(t, *f.Reason, "left out of the import: the file has an error", p)
	}
	frontend := file(t, r, "archive/028-the-frontend-has-no-component-library-no-theme-no-client-and-no-live-updates.md")
	assert.Empty(t, frontend.Links)
	assert.True(t, warned(frontend, "T26 is in the upload, but docs/tickets/026-"), "%+v", frontend.Warnings)
	assert.Contains(t, planned(t, r, 28).Body, "- Found in T26, which is not in this import (filed-from of docs/tickets/archive/028-")
	assert.Equal(t, []int32{4, 7, 22, 28, 50, 51, 99}, plannedNumbers(r), "the plan leaves the conflict and the errors out")
	assert.Equal(t, 1, r.Report.Summary.Conflict)
	assert.Equal(t, 2, r.Report.Summary.Error)
	assert.Equal(t, 7, r.Report.Summary.Create)
}

// plannedNumbers are the numbers of the tickets the plan creates, in its
// order.
func plannedNumbers(r Result) []int32 {
	numbers := make([]int32, 0, len(r.Plan.Tickets))
	for _, p := range r.Plan.Tickets {
		numbers = append(numbers, p.Number)
	}
	return numbers
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
	assert.Zero(t, r.Report.Summary.Conflict, "the conflict of 026 is excluded")

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

// docs/adr/0051 D7: the execution writes no text longer than the API takes —
// a body of 200,000 characters, a question of 2,000, its options and its
// answer of 100,000 each and its recommendation of 10,000, counted in
// characters as the execution writes them, with the keys the import puts in
// place of a mention and the lines it adds; a threat, a block's reason and a
// dropped ticket's reason of 2,000, a done ticket's note of 10,000 —; a longer
// one is an error of its file, which the execution leaves out.
func TestTheImportHoldsTheTextsToTheLengthsOfTheAPI(t *testing.T) {
	src := func(name, front, text string) Source {
		return Source{Path: "docs/tickets/" + name, Content: []byte("---\nid: T" + strings.TrimLeft(name[:3], "0") +
			"\ntitle: x\nseverity: low\neffort: S\nopened: 2026-10-07\n" + front + "---\n\n" + text + "\n")}
	}
	const filed = "state: filed\nsecurity: none\n"
	question := func(q, options, recommendation, answer string) string {
		return "## Open questions\n\n### Q1: " + q + "\n\n" + options + "\n\n**Recommendation:** " + recommendation + "\n\n**Answer:** " + answer
	}
	long := func(n int) string { return strings.Repeat("ä", n) }
	r := Analyze(Read([]Source{
		src("001-a.md", filed, long(maxBody)),
		src("002-b.md", filed, long(maxBody+1)),
		src("003-c.md", filed, question(long(maxQuestionLength), long(maxQuestionText), long(maxRecommendation), long(maxQuestionText))),
		src("004-d.md", filed, question("which?", long(maxQuestionText+1), "a", "a")),
		src("005-e.md", filed, question("which?", "a", "a", long(maxQuestionText+1))),
		src("006-f.md", filed, long(maxBody-9)+" T1 T1 T1"),
		src("007-g.md", filed+"blocked-by: T8\n", long(maxBody-30)),
		src("008-h.md", filed, question(long(maxQuestionLength-10)+" T1 T1 T1", "a", "a", "a")),
		src("009-i.md", filed, question("which?", "a", long(maxRecommendation+1), "a")),
		src("010-j.md", "state: filed\nsecurity: hardening\nthreat: "+long(maxThreat+1)+"\n", ""),
		src("011-k.md", "state: blocked\nsecurity: none\nblocked-by: human\nblocked-from: filed\nblocked-reason: "+long(maxReason+1)+"\n", ""),
		src("012-l.md", "state: done\nsecurity: none\nshipped: "+long(maxNote+1)+"\n", ""),
		src("013-m.md", "state: dropped\nsecurity: none\ndropped-reason: "+long(maxReason+1)+"\n", ""),
		src("014-n.md", "state: done\nsecurity: hardening\nthreat: "+long(maxThreat)+"\nshipped: "+long(maxNote)+"\n", ""),
	}), target(), nil)

	for _, name := range []string{"001-a.md", "003-c.md", "014-n.md"} {
		assert.Equal(t, OutcomeCreate, file(t, r, name).Outcome, "%s is at the bounds", name)
	}
	for name, want := range map[string]MessageReport{
		"002-b.md": {Field: ptr(fieldBody), Message: "the body as the import writes it has 200001 characters, more than the 200000 a ticket's body holds"},
		"004-d.md": {Field: ptr("Q1"), Line: ptr(13), Message: "Q1's options have 100001 characters, more than the 100000 a question's options hold"},
		"005-e.md": {Field: ptr("Q1"), Line: ptr(13), Message: "Q1's answer has 100001 characters, more than the 100000 an answer holds"},
		"006-f.md": {Field: ptr(fieldBody), Message: "the body as the import writes it has 200024 characters, more than the 200000 a ticket's body holds"},
		// The blocks link from 008 becomes a line under ## Related once 008
		// is left out, which takes 007 past the bound.
		"007-g.md": {Field: ptr(fieldBody), Message: "the body as the import writes it has 200066 characters, more than the 200000 a ticket's body holds"},
		"008-h.md": {Field: ptr("Q1"), Line: ptr(13), Message: "Q1 as the import writes it has 2023 characters, more than the 2000 a question holds"},
		"009-i.md": {Field: ptr("Q1"), Line: ptr(13), Message: "Q1's recommendation has 10001 characters, more than the 10000 a recommendation holds"},
		"010-j.md": {Field: ptr(keyThreat), Line: ptr(9), Message: "the threat has 2001 characters, more than the 2000 a threat holds"},
		"011-k.md": {Field: ptr(keyBlockedReason), Line: ptr(11), Message: "the block's reason has 2001 characters, more than the 2000 a reason holds"},
		"012-l.md": {Field: ptr(keyShipped), Line: ptr(9), Message: "the shipped line has 10001 characters, more than the 10000 a verification note holds"},
		"013-m.md": {Field: ptr(keyDroppedReason), Line: ptr(9), Message: "the dropped-reason has 2001 characters, more than the 2000 a reason holds"},
	} {
		f := file(t, r, name)
		assert.Equal(t, OutcomeError, f.Outcome, name)
		assert.Equal(t, []MessageReport{want}, f.Errors, name)
	}
	assert.Equal(t, []int32{1, 3, 14}, plannedNumbers(r), "the execution leaves out every file past a bound")
	assert.Empty(t, r.Plan.Links)
}

// The execution assigns by a file's identity only the member its dry run
// named, and an import through a token assigns a confidential ticket to the
// token's own person or to nobody (docs/adr/0043 D3, docs/adr/0065 D9) — the
// file's assignee and a correction's alike; nothing of it refuses a file.
func TestAnalyzeAssignsWhomTheDryRunNamedAndAnAgentMay(t *testing.T) {
	ada := Person{ID: uuid.Must(uuid.NewV7()), Name: "Ada", Username: ptr("ada")}
	bob := Person{ID: uuid.Must(uuid.NewV7()), Name: "Bob", Username: ptr("bob")}
	src := func(name, assignee string) Source {
		return Source{Path: "docs/tickets/" + name, Content: []byte("---\nid: T" + strings.TrimLeft(strings.TrimPrefix(name, "local_")[:3], "0") +
			"\ntitle: x\nstate: filed\nseverity: low\nsecurity: none\neffort: S\nopened: 2026-10-07\nassignee: " + assignee + "\n---\n")}
	}
	u := Read([]Source{src("001-a.md", "Ada <local:ada>"), src("local_002-b.md", "Ada <local:ada>"),
		src("local_003-c.md", "Bob <local:bob>"), src("004-d.md", "Bob <local:bob>")})
	assigned := func(r Result, n int32) *uuid.UUID { return planned(t, r, n).Assignee }

	tg := target()
	tg.Persons[IdentityOf("ada", "", "")] = ada
	tg.Persons[IdentityOf("bob", "", "")] = bob
	dry := Analyze(u, tg, nil)
	assert.Equal(t, &ada.ID, assigned(dry, 1), "a dry run assigns whom the identity names")

	tg.Named = map[string]uuid.UUID{"docs/tickets/001-a.md": ada.ID, "docs/tickets/local_002-b.md": ada.ID,
		"docs/tickets/local_003-c.md": ada.ID}
	r := Analyze(Read(u.sources), tg, nil)
	assert.Equal(t, &ada.ID, assigned(r, 1), "the member the dry run named")
	assert.Nil(t, assigned(r, 3), "another member than the dry run named")
	assert.True(t, warned(file(t, r, "local_003-c.md"), "whom the dry run did not name"))
	assert.Nil(t, assigned(r, 4), "a member the dry run did not name")
	assert.Equal(t, OutcomeCreate, file(t, r, "004-d.md").Outcome)

	tg.Named, tg.TokenPerson = nil, &bob.ID
	tg.Assignees[ada.ID] = ada
	corrections := []Correction{{Path: "docs/tickets/local_003-c.md", AssigneeSet: true, Assignee: &ada.ID}}
	r = Analyze(Read(u.sources), tg, corrections)
	assert.Equal(t, &ada.ID, assigned(r, 1), "a token assigns a ticket that is not confidential to anybody")
	assert.Nil(t, assigned(r, 2), "a token assigns a confidential ticket to nobody but its person")
	assert.Nil(t, file(t, r, "local_002-b.md").Assignee.Person)
	assert.True(t, warned(file(t, r, "local_002-b.md"), "assigns a confidential ticket to the token's own person or to nobody"))
	assert.Nil(t, assigned(r, 3), "a correction as well")
	r = Analyze(Read(u.sources), tg, nil)
	assert.Equal(t, &bob.ID, assigned(r, 3), "its own person")
	assert.Equal(t, 4, r.Report.Summary.Create)
}
