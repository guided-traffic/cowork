//go:build integration

package integration

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api"
	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/test/fixture"
)

// namedFile is a file of an upload or of an archive.
type namedFile struct {
	name string
	body []byte
}

// dirFiles reads the files under root as an upload holds them, under
// prefix, in path order; testdata names a file of the embargo's local_ prefix
// embargoed-, which no ignore rule hides.
func dirFiles(t *testing.T, root, prefix string) []namedFile {
	t.Helper()
	var out []namedFile
	require.NoError(t, filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		body, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		dir, base := path.Split(filepath.ToSlash(rel))
		out = append(out, namedFile{name: prefix + dir + strings.Replace(base, "embargoed-", "local_", 1), body: body})
		return nil
	}))
	return out
}

// tarGzOf packs files into a tar.gz.
func tarGzOf(t *testing.T, files []namedFile) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, f := range files {
		require.NoError(t, tw.WriteHeader(&tar.Header{Name: f.name, Mode: 0o644, Size: int64(len(f.body)), Typeflag: tar.TypeReg}))
		_, err := tw.Write(f.body)
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	return buf.Bytes()
}

// untar reads a tar.gz into its files by path.
func untar(t *testing.T, archive []byte) map[string][]byte {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	require.NoError(t, err)
	tr := tar.NewReader(gz)
	out := map[string][]byte{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return out
		}
		require.NoError(t, err)
		body, err := io.ReadAll(tr)
		require.NoError(t, err)
		out[h.Name] = body
	}
}

// uploadOf is a multipart body of file parts.
func uploadOf(t *testing.T, parts ...namedFile) (string, []byte) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for _, p := range parts {
		w, err := mw.CreateFormFile("file", p.name)
		require.NoError(t, err)
		_, err = w.Write(p.body)
		require.NoError(t, err)
	}
	require.NoError(t, mw.Close())
	return mw.FormDataContentType(), buf.Bytes()
}

// dryRun sends an upload to a project's imports.
func (e ticketEnv) dryRun(t *testing.T, c caller, project string, parts ...namedFile) *apigen.CreateImportResponse {
	t.Helper()
	contentType, body := uploadOf(t, parts...)
	res, err := e.s.client(t, c).CreateImportWithBodyWithResponse(e.ctx, e.SlugA, project, contentType, bytes.NewReader(body))
	require.NoError(t, err)
	return res
}

// execute executes a dry run with its corrections.
func (e ticketEnv) execute(t *testing.T, c caller, project string, id uuid.UUID, corrections ...apigen.ImportCorrection) *apigen.ExecuteImportResponse {
	t.Helper()
	body := apigen.ImportExecution{}
	if len(corrections) > 0 {
		body.Corrections = &corrections
	}
	res, err := e.s.client(t, c).ExecuteImportWithResponse(e.ctx, e.SlugA, project, id, body)
	require.NoError(t, err)
	return res
}

// reported is the file of a report whose path ends in name.
func reported(t *testing.T, job *apigen.ImportJob, name string) apigen.ImportFile {
	t.Helper()
	for _, f := range job.Files {
		if strings.HasSuffix(f.Path, name) {
			return f
		}
	}
	t.Fatalf("no file %s in the report", name)
	return apigen.ImportFile{}
}

const (
	importFixtures   = "../../internal/importer/testdata/tickets"
	fixtureLayer     = "docs/tickets/archive/004-there-is-no-data-access-layer.md"
	fixtureFixed     = "docs/tickets/archive/045-the-login-page-follows-a-return-path-with-a-control-character-to-another-site.md"
	fixtureEmbargoed = "docs/tickets/local_099-an-open-finding-under-embargo.md"
)

// docs/adr/0051 D1–D3, D6, docs/adr/0063, docs/adr/0065 D7: a dry run of a
// repository's tickets, its report, who may make and read it, and its
// execution with corrections — every ticket, question and link at once, the
// numbers kept and the sequence past them, every row an act naming the job,
// one event for the streams, and a second execution refused.
func TestImportADryRunAndItsExecution(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	admin := caller{Token: e.tk.AdminA}
	archive := namedFile{name: "tickets.tar.gz", body: tarGzOf(t, dirFiles(t, importFixtures, "docs/tickets/"))}

	assertProblem(t, rawImport(t, e, caller{Token: e.tk.ViewerA}, archive), http.StatusForbidden, "forbidden")
	reader, _, err := f.Token(e.ctx, fixture.TokenSpec{UserID: e.AdminA, Scope: domain.ScopeRead})
	require.NoError(t, err)
	assertProblem(t, rawImport(t, e, caller{Token: reader}, archive), http.StatusForbidden, "insufficient_scope")

	created := e.dryRun(t, admin, "ALPHA", archive)
	require.Equal(t, http.StatusCreated, created.StatusCode(), string(created.Body))
	job := created.JSON201
	assert.Equal(t, fmt.Sprintf("/api/v1/tenants/%s/projects/ALPHA/imports/%s", e.SlugA, job.Id), created.HTTPResponse.Header.Get("Location"))
	assert.Equal(t, apigen.ImportStatusDryRun, job.Status)
	assert.Equal(t, e.AdminA, job.CreatedBy.Id)
	expires, err := job.ExpiresAt.Get()
	require.NoError(t, err)
	assert.WithinDuration(t, job.CreatedAt.Add(24*time.Hour), expires, time.Minute)
	assert.True(t, job.ExecutedAt.IsNull())
	assert.Equal(t, apigen.ImportSummary{Files: 10, Create: 9, Skip: 1, Open: 3, Confidential: 1,
		HighestNumber: nullable.NewNullableWithValue(99)}, job.Summary)
	layer := reported(t, job, fixtureLayer)
	assert.Equal(t, apigen.ImportOutcomeCreate, layer.Outcome)
	assert.Equal(t, e.SlugA+"/ALPHA-4", layer.Key.MustGet())
	assert.True(t, strings.HasPrefix(layer.Note.MustGet(), "sqlc over pgx"), "the shipped line is the done act's note")
	embargoed := reported(t, job, fixtureEmbargoed)
	assert.True(t, embargoed.Confidential)
	assert.Equal(t, apigen.BlockKindHuman, embargoed.BlockCandidate.MustGet())
	missing, err := e.s.client(t, admin).GetTicketWithResponse(e.ctx, e.SlugA, "ALPHA", 4)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, missing.StatusCode(), "a dry run imports nothing")

	read, err := e.s.client(t, caller{Token: e.tk.AdminAWrite}).GetImportWithResponse(e.ctx, e.SlugA, "ALPHA", job.Id)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, read.StatusCode(), string(read.Body))
	assert.JSONEq(t, string(mustJSON(t, job)), string(read.Body), "the job reads back as it was answered")
	agent, err := e.s.client(t, caller{Token: e.tk.AdminAWrite, Agent: "claude-code/opus/s1"}).GetImportWithResponse(e.ctx, e.SlugA, "ALPHA", job.Id)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, agent.StatusCode(), "its maker's agent reads the job")
	another, err := e.s.client(t, caller{Token: e.tk.MemberA}).GetImportWithResponse(e.ctx, e.SlugA, "ALPHA", job.Id)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, another.StatusCode(), "another writer does not read the job: it holds the embargoed file")
	other, err := e.s.client(t, caller{Token: e.tk.MemberB}).GetImportWithResponse(e.ctx, e.SlugA, "ALPHA", job.Id)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, other.StatusCode(), "another tenant's member does not reach the tenant")
	notTheirs := e.execute(t, caller{Token: e.tk.MemberA}, "ALPHA", job.Id)
	assert.Equal(t, http.StatusNotFound, notTheirs.StatusCode(), "nor executes it")

	stream := e.openStream(t, e.s, admin, e.SlugA, "")
	executed := e.execute(t, admin, "ALPHA", job.Id,
		apigen.ImportCorrection{Path: fixtureFixed, Type: ptr(apigen.TicketTypeTask)},
		apigen.ImportCorrection{Path: fixtureEmbargoed, State: ptr(apigen.TicketStateBlocked),
			Block: &apigen.ImportBlockCorrection{Kind: apigen.BlockKindHuman, Reason: "waits on a review"}})
	require.Equal(t, http.StatusOK, executed.StatusCode(), string(executed.Body))
	done := executed.JSON200
	assert.Equal(t, apigen.ImportStatusExecuted, done.Status)
	assert.True(t, done.ExpiresAt.IsNull())
	assert.Equal(t, e.AdminA, done.ExecutedBy.MustGet().Id)
	assert.Equal(t, 9, done.Summary.Created)
	assert.Zero(t, done.Summary.Create)
	assert.Equal(t, apigen.ImportOutcomeCreated, reported(t, done, fixtureLayer).Outcome)
	assert.Equal(t, apigen.TicketTypeTask, reported(t, done, fixtureFixed).Type.MustGet())
	assert.Equal(t, "corrected", reported(t, done, fixtureFixed).TypeReason.MustGet())

	m, ok := stream.next(t, 5*time.Second)
	require.True(t, ok, "the execution is announced")
	assert.Equal(t, "project.changed", m.Event)
	assert.JSONEq(t, fmt.Sprintf(`{"key":"%s/ALPHA","kind":"imported"}`, e.SlugA), m.Data)
	_, more := stream.next(t, 700*time.Millisecond)
	assert.False(t, more, "no event per ticket: the import is announced once")

	again := e.execute(t, admin, "ALPHA", job.Id)
	assertProblemOf(t, again.HTTPResponse, again.Body, http.StatusConflict, "import_executed")

	assertImportedTickets(t, e, f, job.Id)
}

// assertImportedTickets holds the tickets of the fixtures' execution to what
// the source says.
func assertImportedTickets(t *testing.T, e ticketEnv, f *fixture.DB, job uuid.UUID) {
	t.Helper()
	admin := e.s.client(t, caller{Token: e.tk.AdminA})
	get := func(n int) apigen.Ticket {
		res, err := admin.GetTicketWithResponse(e.ctx, e.SlugA, "ALPHA", n)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, res.StatusCode(), "ALPHA-%d: %s", n, res.Body)
		return *res.JSON200
	}
	layer := get(4)
	assert.Equal(t, apigen.TicketStateDone, layer.State)
	assert.Equal(t, "2026-10-02", layer.OpenedAt.UTC().Format(time.DateOnly))
	assert.Equal(t, "2026-10-02", layer.DoneAt.MustGet().UTC().Format(time.DateOnly))
	assert.True(t, layer.DoneByHand)
	assert.Equal(t, e.AdminA, layer.Reporter.Id, "the importer is the reporter")
	phase := get(26)
	assert.Equal(t, apigen.TicketStateInProgress, phase.State)
	assert.Equal(t, apigen.HorizonNext, phase.Horizon)
	assert.Equal(t, apigen.TicketTypeFeature, phase.Type)
	finding := get(99)
	assert.True(t, finding.Confidential)
	assert.Equal(t, apigen.TicketStateBlocked, finding.State)
	assert.Equal(t, apigen.BlockKindHuman, finding.Block.MustGet().Kind)
	hidden, err := e.s.client(t, caller{Token: e.tk.MemberA}).GetTicketWithResponse(e.ctx, e.SlugA, "ALPHA", 99)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, hidden.StatusCode(), "the embargo's rule set the flag (docs/adr/0065 D7)")

	links, err := admin.ListTicketLinksWithResponse(e.ctx, e.SlugA, "ALPHA", 28, &apigen.ListTicketLinksParams{})
	require.NoError(t, err)
	require.Len(t, links.JSON200.Items, 1)
	assert.Equal(t, e.SlugA+"/ALPHA-26", links.JSON200.Items[0].Ticket.Key)
	assert.Equal(t, apigen.LinkTypeFoundIn, links.JSON200.Items[0].Type)
	questions, err := admin.ListQuestionsWithResponse(e.ctx, e.SlugA, "ALPHA", 22, &apigen.ListQuestionsParams{})
	require.NoError(t, err)
	require.Len(t, questions.JSON200.Items, 6)
	assert.Equal(t, apigen.QuestionStatusAnswered, questions.JSON200.Items[0].Status)

	doc := markdownOf(t, e, 4)
	assert.Contains(t, doc, "shipped: \"sqlc over pgx (readq, writeq)")
	assert.Contains(t, doc, "## Related\n\n")
	assert.Contains(t, doc, "- Filed from: the phase-2 conversion (T2)")
	assert.Contains(t, markdownOf(t, e, 7), "The data-access layer of "+e.SlugA+"/ALPHA-4 settles all three.")
	assert.Contains(t, markdownOf(t, e, 28), `shipped: "imported from archive; the source carried no verification note"`)

	next := e.file(t, caller{Token: e.tk.MemberA}, "ALPHA", task("After the import"))
	assert.EqualValues(t, 100, next.Number, "the sequence advanced past the highest number imported (docs/adr/0007 D6)")

	for _, c := range []struct {
		query string
		want  int64
	}{
		{`SELECT count(*) FROM audit_events WHERE action = 'created' AND entity_type = 'ticket' AND after->>'import_job' = $1`, 9},
		{`SELECT count(*) FROM audit_events WHERE action = 'asked' AND after->>'import_job' = $1`, 9},
		{`SELECT count(*) FROM audit_events WHERE action = 'transitioned' AND after->>'import_job' = $1`, 6},
		{`SELECT count(*) FROM audit_events WHERE action = 'confidential_set' AND ticket_key LIKE '%-99'
		  AND id >= (SELECT id FROM audit_events WHERE after->>'import_job' = $1 ORDER BY id LIMIT 1)`, 1},
		{`SELECT count(*) FROM audit_events WHERE action = 'imported' AND entity_id = $1::uuid AND actor_user_id IS NOT NULL`, 1},
		{`SELECT count(*) FROM tickets WHERE imported_from_job = $1::uuid`, 9},
	} {
		n, err := f.QueryCount(e.ctx, c.query, job.String())
		require.NoError(t, err)
		assert.Equal(t, c.want, n, c.query)
	}
	var file string
	require.NoError(t, f.QueryRow(e.ctx, `SELECT imported_from_file FROM tickets WHERE imported_from_job = $1 AND number = 4`, job).Scan(&file))
	assert.Equal(t, fixtureLayer, file)
}

// rawImport sends an upload as c and returns the response, for the refusals.
func rawImport(t *testing.T, e ticketEnv, c caller, parts ...namedFile) *http.Response {
	t.Helper()
	contentType, body := uploadOf(t, parts...)
	return e.s.do(t, c, http.MethodPost, fmt.Sprintf("/api/v1/tenants/%s/projects/ALPHA/imports", e.SlugA), string(body),
		"Content-Type", contentType)
}

// assertProblemOf holds a response the generated client has read already to
// a problem with its code.
func assertProblemOf(t *testing.T, res *http.Response, body []byte, status int, code string) {
	t.Helper()
	require.Equal(t, status, res.StatusCode, string(body))
	assert.Equal(t, "application/problem+json; charset=utf-8", res.Header.Get("Content-Type"))
	var p map[string]any
	require.NoError(t, json.Unmarshal(body, &p))
	assert.Equal(t, code, p["code"], string(body))
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

// markdownOf is a ticket of ALPHA as its …/markdown document, as an
// administrator reads it.
func markdownOf(t *testing.T, e ticketEnv, n int) string {
	t.Helper()
	res := e.s.do(t, caller{Token: e.tk.AdminA}, http.MethodGet, fmt.Sprintf("%s/%d/markdown", e.projectTickets("ALPHA"), n), nil)
	require.Equal(t, http.StatusOK, res.StatusCode)
	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	return string(body)
}

// ticketFile is a repository ticket file of the given number and frontmatter
// lines besides the required ones, uploaded as a part of its own: a part's
// file name is a base name, which is the file's path in the report.
func ticketFile(n int, extra string) namedFile {
	return namedFile{name: fmt.Sprintf("%03d-ticket-%d.md", n, n), body: []byte(fmt.Sprintf(
		"---\nid: T%d\ntitle: ticket %d\nstate: filed\nseverity: low\nsecurity: none\nurgency: later\neffort: S\nopened: 2026-10-01\n%s---\n\n## Current state\n\nText.\n",
		n, n, extra))}
}

// docs/adr/0051 D2, docs/adr/0064 D3: the execution imports every file it can
// and leaves out each with a conflict or an error, the report saying why; an
// exclusion makes the file's number name the project's ticket; the project as
// it stands at the execution decides, a ticket filed after the dry run
// included; a correction that breaks a rule is refused at its pointer.
func TestImportLeavesOutWhatItCannotImport(t *testing.T) {
	e := newTicketEnv(t)
	admin := caller{Token: e.tk.AdminA}
	member := caller{Token: e.tk.MemberA}
	e.file(t, member, "ALPHA", task("Already here"))

	created := e.dryRun(t, admin, "ALPHA", ticketFile(1, ""), ticketFile(2, "blocked-by: T1\n"),
		namedFile{name: "004-broken.md", body: []byte("---\nid: T4\ntitle: broken\nstate: filed\nseverity: urgent\nsecurity: none\neffort: S\n---\n")})
	require.Equal(t, http.StatusCreated, created.StatusCode(), string(created.Body))
	job := created.JSON201
	conflict := reported(t, job, "001-ticket-1.md")
	assert.Equal(t, apigen.ImportOutcomeConflict, conflict.Outcome)
	assert.Equal(t, e.SlugA+"/ALPHA-1", conflict.Conflict.MustGet())
	assert.Contains(t, conflict.Reason.MustGet(), "left out of the import")

	executed := e.execute(t, admin, "ALPHA", job.Id)
	require.Equal(t, http.StatusOK, executed.StatusCode(), string(executed.Body))
	done := executed.JSON200
	assert.Equal(t, 1, done.Summary.Created)
	assert.Equal(t, 1, done.Summary.Conflict)
	assert.Equal(t, 1, done.Summary.Error)
	assert.Equal(t, "left out of the import: the project holds its number as "+e.SlugA+"/ALPHA-1 (docs/adr/0064 D3)",
		reported(t, done, "001-ticket-1.md").Reason.MustGet())
	broken := reported(t, done, "004-broken.md")
	assert.Equal(t, apigen.ImportOutcomeError, broken.Outcome)
	assert.Contains(t, broken.Reason.MustGet(), "left out of the import: the file has an error")
	assert.Equal(t, apigen.ImportOutcomeCreated, reported(t, done, "002-ticket-2.md").Outcome)
	assert.Contains(t, markdownOf(t, e, 2), "- Blocked by T1, which is not in this import",
		"a reference to a file left out is no link to the project's ticket of its number, a line instead")
	none, err := e.s.client(t, admin).GetTicketWithResponse(e.ctx, e.SlugA, "ALPHA", 4)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, none.StatusCode(), "the file with an error is not imported")
	again := e.execute(t, admin, "ALPHA", job.Id)
	assertProblemOf(t, again.HTTPResponse, again.Body, http.StatusConflict, "import_executed")

	excluded := e.dryRun(t, admin, "ALPHA", ticketFile(1, ""), ticketFile(5, "blocked-by: T1\n"))
	require.Equal(t, http.StatusCreated, excluded.StatusCode(), string(excluded.Body))
	bad := e.execute(t, admin, "ALPHA", excluded.JSON201.Id, apigen.ImportCorrection{Path: "999-none.md", Exclude: ptr(true)})
	assertProblemOf(t, bad.HTTPResponse, bad.Body, http.StatusBadRequest, "validation_failed")
	assigned := e.execute(t, admin, "ALPHA", excluded.JSON201.Id, apigen.ImportCorrection{Path: "005-ticket-5.md",
		Assignee: nullable.NewNullableWithValue(e.MemberB)})
	assertProblemOf(t, assigned.HTTPResponse, assigned.Body, http.StatusBadRequest, "validation_failed")
	executed = e.execute(t, admin, "ALPHA", excluded.JSON201.Id, apigen.ImportCorrection{Path: "001-ticket-1.md", Exclude: ptr(true)},
		apigen.ImportCorrection{Path: "005-ticket-5.md", Assignee: nullable.NewNullableWithValue(e.MemberA)})
	require.Equal(t, http.StatusOK, executed.StatusCode(), string(executed.Body))
	assert.Equal(t, apigen.ImportOutcomeExclude, reported(t, executed.JSON200, "001-ticket-1.md").Outcome)
	links, err := e.s.client(t, admin).ListTicketLinksWithResponse(e.ctx, e.SlugA, "ALPHA", 5, &apigen.ListTicketLinksParams{})
	require.NoError(t, err)
	require.Len(t, links.JSON200.Items, 1, "the excluded file's number names the project's ticket")
	assert.Equal(t, e.SlugA+"/ALPHA-1", links.JSON200.Items[0].Ticket.Key)
	five, err := e.s.client(t, admin).GetTicketWithResponse(e.ctx, e.SlugA, "ALPHA", 5)
	require.NoError(t, err)
	assert.Equal(t, e.MemberA, five.JSON200.Assignee.MustGet().Id)

	later := e.dryRun(t, admin, "ALPHA", ticketFile(6, ""))
	require.Equal(t, http.StatusCreated, later.StatusCode(), string(later.Body))
	assert.Equal(t, apigen.ImportOutcomeCreate, reported(t, later.JSON201, "006-ticket-6.md").Outcome)
	filed := e.file(t, member, "ALPHA", task("Filed meanwhile"))
	require.EqualValues(t, 6, filed.Number)
	meanwhile := e.execute(t, admin, "ALPHA", later.JSON201.Id)
	require.Equal(t, http.StatusOK, meanwhile.StatusCode(), string(meanwhile.Body))
	assert.Equal(t, apigen.ImportOutcomeConflict, reported(t, meanwhile.JSON200, "006-ticket-6.md").Outcome)
	assert.Zero(t, meanwhile.JSON200.Summary.Created)
	sixth := e.get(t, admin, "ALPHA", 6)
	require.Equal(t, http.StatusOK, sixth.StatusCode())
	assert.Equal(t, "Filed meanwhile", sixth.JSON200.Title, "the ticket filed meanwhile stays as it is")
}

// docs/adr/0051 D6, docs/adr/0043 D2, D3: a writer of the project imports, as
// creating a ticket needs, an agent of theirs too — its dry run, its
// execution, its report —, and the agent sets a parent afterwards as its
// baseline allows; the agent assigns a confidential ticket to nobody but its
// person; a viewer does not import.
func TestAWriterAndTheirAgentImport(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	agent := caller{Token: e.tk.AgentA}
	var adminName string
	require.NoError(t, f.QueryRow(e.ctx, `SELECT username FROM users WHERE id = $1`, e.AdminA).Scan(&adminName))
	parentFile := ticketFile(1, "")
	child := ticketFile(2, "")
	embargoed := namedFile{name: "local_003-a-finding.md", body: []byte("---\nid: T3\ntitle: a finding\nstate: filed\n" +
		"severity: high\nsecurity: boundary\nthreat: a reader learns too much\neffort: S\nopened: 2026-10-01\n" +
		"assignee: Admin <local:" + adminName + ">\n---\n")}

	assertProblem(t, rawImport(t, e, caller{Token: e.tk.ViewerA}, parentFile), http.StatusForbidden, "forbidden")
	created := e.dryRun(t, agent, "ALPHA", parentFile, child, embargoed)
	require.Equal(t, http.StatusCreated, created.StatusCode(), string(created.Body))
	job := created.JSON201
	assert.Equal(t, e.MemberA, job.CreatedBy.Id)
	finding := reported(t, job, "local_003-a-finding.md")
	assert.True(t, finding.Confidential)
	assert.True(t, finding.Assignee.MustGet().Person.IsNull(), "an agent admits nobody to a confidential ticket")
	read, err := e.s.client(t, caller{Token: e.tk.MemberA}).GetImportWithResponse(e.ctx, e.SlugA, "ALPHA", job.Id)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, read.StatusCode(), "the job is its person's")
	executed := e.execute(t, agent, "ALPHA", job.Id)
	require.Equal(t, http.StatusOK, executed.StatusCode(), string(executed.Body))
	assert.Equal(t, 3, executed.JSON200.Summary.Created)
	three := e.get(t, caller{Token: e.tk.MemberA}, "ALPHA", 3)
	require.Equal(t, http.StatusOK, three.StatusCode())
	assert.True(t, three.JSON200.Assignee.IsNull())
	assert.Equal(t, e.MemberA, three.JSON200.Reporter.Id, "the agent's person is the reporter")

	second := e.get(t, agent, "ALPHA", 2)
	require.Equal(t, http.StatusOK, second.StatusCode())
	parented := e.patch(t, agent, *second.JSON200, apigen.TicketPatch{Parent: nullable.NewNullableWithValue(e.SlugA + "/ALPHA-1")})
	require.Equal(t, http.StatusOK, parented.StatusCode(), string(parented.Body))
	assert.Equal(t, e.SlugA+"/ALPHA-1", parented.JSON200.Parent.MustGet(), "the agent sets the parent after the import")
}

// docs/adr/0007 D4 as amended: a number a purged ticket held is imported, and
// the report says what named it before names the new ticket; a /context
// document is skipped with its reason.
func TestImportGivesAPurgedNumberBack(t *testing.T) {
	e := newTicketEnv(t)
	admin := caller{Token: e.tk.AdminA}
	gone := e.file(t, admin, "ALPHA", task("Purged"))
	e.send(t, admin, http.StatusNoContent, http.MethodDelete, ticketPath(e.SlugA, "ALPHA", gone.Number), nil)
	require.NoError(t, fixtures(t).Exec(e.ctx, `UPDATE tickets SET deleted_at = now() - interval '31 days' WHERE id = $1`, gone.Id))
	_, err := openRuntime(t).PurgeDeletedTickets(e.ctx, time.Now())
	require.NoError(t, err)

	context := namedFile{name: "ALPHA-9.md", body: []byte("<!-- cowork: context of " + e.SlugA + "/ALPHA-9, exported 2026-10-04T09:12:00Z by Ada -->\n" +
		"---\nkey: " + e.SlugA + "/ALPHA-9\n---\n\n## Links\n\nNone.\n")}
	created := e.dryRun(t, admin, "ALPHA", ticketFile(gone.Number, ""), context)
	require.Equal(t, http.StatusCreated, created.StatusCode(), string(created.Body))
	back := reported(t, created.JSON201, fmt.Sprintf("%03d-ticket-%d.md", gone.Number, gone.Number))
	assert.Equal(t, apigen.ImportOutcomeCreate, back.Outcome)
	assert.Contains(t, back.Warnings[0].Message, "was purged; the import gives its number back")
	skipped := reported(t, created.JSON201, "ALPHA-9.md")
	assert.Equal(t, apigen.ImportOutcomeSkip, skipped.Outcome)
	assert.Contains(t, skipped.Reason.MustGet(), "a /context document")
	executed := e.execute(t, admin, "ALPHA", created.JSON201.Id)
	require.Equal(t, http.StatusOK, executed.StatusCode(), string(executed.Body))
	again := e.get(t, admin, "ALPHA", gone.Number)
	require.Equal(t, http.StatusOK, again.StatusCode())
	assert.Equal(t, fmt.Sprintf("ticket %d", gone.Number), again.JSON200.Title)
}

// An execution assigns the member its dry run named and nobody else: a
// person who became a member between the dry run and the execution is not
// assigned — not admitted to the confidential ticket the report showed
// unassigned —, and the report says so.
func TestTheExecutionAssignsWhomTheDryRunNamed(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	admin := caller{Token: e.tk.AdminA}
	late, err := f.Person(e.ctx, uniqueSlug("late"), "Late")
	require.NoError(t, err)
	var username string
	require.NoError(t, f.QueryRow(e.ctx, `SELECT username FROM users WHERE id = $1`, late).Scan(&username))
	finding := namedFile{name: "local_007-a-finding.md", body: []byte("---\nid: T7\ntitle: a finding\nstate: filed\n" +
		"severity: high\nsecurity: live\nthreat: a reader learns too much\neffort: S\nopened: 2026-10-01\n" +
		"assignee: Late <local:" + username + ">\n---\n")}

	created := e.dryRun(t, admin, "ALPHA", finding)
	require.Equal(t, http.StatusCreated, created.StatusCode(), string(created.Body))
	assert.True(t, reported(t, created.JSON201, "local_007-a-finding.md").Assignee.MustGet().Person.IsNull(),
		"the dry run names nobody: the person is no member")
	require.NoError(t, f.Member(e.ctx, e.A, late, domain.RoleMember))

	executed := e.execute(t, admin, "ALPHA", created.JSON201.Id)
	require.Equal(t, http.StatusOK, executed.StatusCode(), string(executed.Body))
	file := reported(t, executed.JSON200, "local_007-a-finding.md")
	assert.True(t, file.Assignee.MustGet().Person.IsNull())
	assert.Contains(t, string(mustJSON(t, file.Warnings)), "whom the dry run did not name")
	seven := e.get(t, admin, "ALPHA", 7)
	require.Equal(t, http.StatusOK, seven.StatusCode())
	assert.True(t, seven.JSON200.Confidential)
	assert.True(t, seven.JSON200.Assignee.IsNull(), "the member added in between is not admitted")
}

// docs/adr/0051 D7, docs/adr/0039 D2: the upload's bound, a broken upload,
// an archived project, and a dry run that expires after twenty-four hours —
// its read and its execution then 404, the expiry job deleting it with its
// files, an executed job kept.
func TestImportBoundsAndExpiry(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	admin := caller{Token: e.tk.AdminA}
	small := newAPI(t, func(o *api.Options) { o.MaxImportBytes = 1024 })
	contentType, body := uploadOf(t, namedFile{name: "001-a.md", body: bytes.Repeat([]byte("x"), 4096)})
	res := small.do(t, admin, http.MethodPost, fmt.Sprintf("/api/v1/tenants/%s/projects/ALPHA/imports", e.SlugA), string(body),
		"Content-Type", contentType)
	assertProblem(t, res, http.StatusRequestEntityTooLarge, "payload_too_large")
	broken := rawImport(t, e, admin, namedFile{name: "t.tgz", body: append([]byte{0x1f, 0x8b}, bytes.Repeat([]byte("x"), 64)...)})
	assertProblem(t, broken, http.StatusBadRequest, "validation_failed")

	created := e.dryRun(t, admin, "ALPHA", ticketFile(1, ""))
	require.Equal(t, http.StatusCreated, created.StatusCode(), string(created.Body))
	id := created.JSON201.Id
	tomorrow := newAPI(t, func(o *api.Options) { o.Now = func() time.Time { return time.Now().Add(25 * time.Hour) } })
	gone, err := tomorrow.client(t, admin).GetImportWithResponse(e.ctx, e.SlugA, "ALPHA", id)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, gone.StatusCode())
	late, err := tomorrow.client(t, admin).ExecuteImportWithResponse(e.ctx, e.SlugA, "ALPHA", id, apigen.ImportExecution{})
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, late.StatusCode())

	kept := e.dryRun(t, admin, "ALPHA", ticketFile(2, ""))
	require.Equal(t, http.StatusCreated, kept.StatusCode(), string(kept.Body))
	require.Equal(t, http.StatusOK, e.execute(t, admin, "ALPHA", kept.JSON201.Id).StatusCode())
	removed, err := openRuntime(t).ExpireImportJobs(e.ctx, time.Now().Add(25*time.Hour))
	require.NoError(t, err)
	assert.GreaterOrEqual(t, removed, int64(1))
	n, err := f.QueryCount(e.ctx, `SELECT count(*) FROM import_jobs WHERE id = $1`, id)
	require.NoError(t, err)
	assert.Zero(t, n, "the expired dry run is deleted with its files")
	n, err = f.QueryCount(e.ctx, `SELECT count(*) FROM import_jobs WHERE id = $1 AND status = 'executed' AND source IS NULL`, kept.JSON201.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n, "an executed job stays, without its files")
	n, err = f.QueryCount(e.ctx, `SELECT count(*) FROM audit_events WHERE actor_system = 'system:import-expiry' AND action = 'expired'`)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, n, int64(1))

	archive, err := e.s.client(t, admin).ArchiveProjectWithResponse(e.ctx, e.SlugA, "ALPHA")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, archive.StatusCode(), string(archive.Body))
	assertProblem(t, rawImport(t, e, admin, ticketFile(3, "")), http.StatusConflict, "project_archived")
}

// docs/adr/0021 D6, docs/adr/0051 D6, D7: the policies of migration 45 hold an
// import job to the person who made it and the tenant's administrators —
// another member reads, changes and makes none, even through a query that
// names no person —, and its deletion to the expiry job, which no
// administrator is.
func TestTheImportJobPoliciesAdmitItsMakerAndTheAdministrators(t *testing.T) {
	e := newTicketEnv(t)
	created := e.dryRun(t, caller{Token: e.tk.MemberA}, "ALPHA", ticketFile(1, ""))
	require.Equal(t, http.StatusCreated, created.StatusCode(), string(created.Body))

	n, err := e.runAs(t, e.A, e.MemberA, uuid.Nil, `SELECT id FROM import_jobs`)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n, "its maker reads the job")
	n, err = e.runAs(t, e.A, e.AdminA, uuid.Nil, `SELECT id FROM import_jobs`)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n, "the tenant's administrator reads the job")
	n, err = e.runAs(t, e.A, e.ViewerA, uuid.Nil, `SELECT id FROM import_jobs`)
	require.NoError(t, err)
	assert.Zero(t, n, "another member of the tenant reads no job")
	n, err = e.runAs(t, e.A, e.ViewerA, uuid.Nil, `UPDATE import_jobs SET report = '{}'`)
	require.NoError(t, err)
	assert.Zero(t, n, "another member of the tenant changes no job")
	_, err = e.runAs(t, e.A, e.ViewerA, uuid.Nil, fmt.Sprintf(`INSERT INTO import_jobs
		(tenant_id, project_id, created_by, expires_at, report, source)
		VALUES ('%s', '%s', '%s', now() + interval '1 day', '{}', '\x')`, e.A, e.ProjectA, e.MemberA))
	assert.ErrorContains(t, err, "row-level security", "a member of the tenant makes no job in another's name")
	n, err = e.runAs(t, e.A, e.AdminA, uuid.Nil, `DELETE FROM import_jobs`)
	require.NoError(t, err)
	assert.Zero(t, n, "an administrator deletes no job: the expiry job does")
}

// docs/adr/0024 D2, docs/adr/0051 D3: the purge of a ticket an import created
// — by the job, which is no administrator — takes its file out of the job's
// report, which would keep the text the purge removes; the other files stay,
// and the summary still counts it.
func TestThePurgeTakesAnImportedTicketOutOfItsReport(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	admin := caller{Token: e.tk.AdminA}
	created := e.dryRun(t, admin, "ALPHA", ticketFile(1, ""), ticketFile(2, ""))
	require.Equal(t, http.StatusCreated, created.StatusCode(), string(created.Body))
	id := created.JSON201.Id
	require.Equal(t, http.StatusOK, e.execute(t, admin, "ALPHA", id).StatusCode())
	e.send(t, admin, http.StatusNoContent, http.MethodDelete, ticketPath(e.SlugA, "ALPHA", 1), nil)
	require.NoError(t, f.Exec(e.ctx, `UPDATE tickets SET deleted_at = now() - interval '31 days'
		WHERE tenant_id = $1 AND imported_from_job = $2 AND number = 1`, e.A, id))

	_, err := openRuntime(t).PurgeDeletedTickets(e.ctx, time.Now())
	require.NoError(t, err)
	read, err := e.s.client(t, admin).GetImportWithResponse(e.ctx, e.SlugA, "ALPHA", id)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, read.StatusCode(), string(read.Body))
	require.Len(t, read.JSON200.Files, 1)
	assert.Equal(t, e.SlugA+"/ALPHA-2", read.JSON200.Files[0].Key.MustGet())
	assert.Equal(t, 2, read.JSON200.Summary.Created, "the summary still counts the purged ticket")
	assert.Equal(t, 1, scalar[int](t, `SELECT count(*) FROM audit_events WHERE tenant_id = $1 AND action = 'purged'
		AND ticket_key = $2 AND (after ->> 'import_report')::int = 1`, e.A, e.SlugA+"/ALPHA-1"),
		"the purge's act counts the report it changed")
}

// stateLine is a ticket file's frontmatter state, as the grep of
// docs/tickets/README.md reads it.
var stateLine = regexp.MustCompile(`(?m)^state:[ \t]*([a-z-]+)`)

// docs/adr/0051 D2, D3, docs/adr/0063: the phase's verification in the
// small — a dry run over this repository's whole docs/tickets/, the archive
// included, reads every ticket file without an error and skips only what no
// ticket is; after the execution the project's open tickets are as many as
// the source's `state:` lines that are not done or dropped.
func TestImportThisRepositorysTickets(t *testing.T) {
	e := newTicketEnv(t)
	admin := caller{Token: e.tk.AdminA}
	files := dirFiles(t, filepath.Join("..", "..", "..", "docs", "tickets"), "docs/tickets/")
	open := 0
	for _, f := range files {
		base := path.Base(f.name)
		if !regexp.MustCompile(`^(local_)?[0-9].*\.md$`).MatchString(base) {
			continue
		}
		if m := stateLine.FindSubmatch(f.body); m != nil && string(m[1]) != "done" && string(m[1]) != "dropped" {
			open++
		}
	}
	require.Positive(t, open)

	created := e.dryRun(t, admin, "ALPHA", namedFile{name: "tickets.tar.gz", body: tarGzOf(t, files)})
	require.Equal(t, http.StatusCreated, created.StatusCode(), string(created.Body))
	job := created.JSON201
	for _, f := range job.Files {
		switch f.Outcome {
		case apigen.ImportOutcomeCreate:
		case apigen.ImportOutcomeSkip:
			reason := f.Reason.MustGet()
			assert.True(t, strings.HasPrefix(reason, "not a ticket file") || strings.HasPrefix(reason, "not a Markdown file"),
				"%s is skipped for no reason of docs/adr/0063 D5: %s", f.Path, reason)
		default:
			t.Errorf("%s: %s %+v", f.Path, f.Outcome, f.Errors)
		}
	}
	assert.Zero(t, job.Summary.Error)
	assert.Equal(t, open, job.Summary.Open)

	executed := e.execute(t, admin, "ALPHA", job.Id)
	require.Equal(t, http.StatusOK, executed.StatusCode(), string(executed.Body))
	list, err := e.s.client(t, admin).ListProjectTicketsWithResponse(e.ctx, e.SlugA, "ALPHA",
		&apigen.ListProjectTicketsParams{Page: ptr(1), PerPage: ptr(apigen.ListProjectTicketsParamsPerPage(100))})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, list.StatusCode(), string(list.Body))
	require.NotNil(t, list.JSON200.Total)
	assert.EqualValues(t, open, *list.JSON200.Total, "the open tickets after the import are the source's open state lines")
}
