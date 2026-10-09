package mcpcli

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/tools"
)

// ticketsDir writes a repository's docs/tickets/ under root: two ticket
// files, one in archive/, a link that is no regular file, and a file the
// import does not read.
func ticketsDir(t *testing.T, root string) string {
	t.Helper()
	dir := filepath.Join(root, "docs", "tickets")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "archive"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "001-a.md"), []byte("---\nid: T1\n---\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "archive", "002-b.md"), []byte("---\nid: T2\n---\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte("SECRET=1\n"), 0o600))
	require.NoError(t, os.Symlink("001-a.md", filepath.Join(dir, "link.md")))
	return dir
}

// entriesOf are the names a tar.gz holds, in its order.
func entriesOf(t *testing.T, r io.Reader) []string {
	t.Helper()
	gz, err := gzip.NewReader(r)
	require.NoError(t, err)
	tr := tar.NewReader(gz)
	var names []string
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return names
		}
		require.NoError(t, err)
		names = append(names, h.Name)
	}
}

// importJob is a job as the API answers it, with the files of a report.
func importJob(status apigen.ImportStatus, summary apigen.ImportSummary, files ...apigen.ImportFile) apigen.ImportJob {
	job := apigen.ImportJob{Id: uuid.MustParse("0199a3c2-1d2e-7f00-8000-0000000000aa"), Project: "COW", Status: status,
		CreatedBy: apigen.Person{Id: uuid.New(), DisplayName: "Ada"}, CreatedAt: time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC),
		Summary: summary, Files: files}
	if status == apigen.ImportStatusDryRun {
		job.ExpiresAt.Set(time.Date(2026, 10, 10, 8, 0, 0, 0, time.UTC))
		job.ExecutedBy.SetNull()
		job.ExecutedAt.SetNull()
	} else {
		job.ExpiresAt.SetNull()
		job.ExecutedBy.Set(job.CreatedBy)
		job.ExecutedAt.Set(job.CreatedAt)
	}
	return job
}

// reportFile is a file of a report with its outcome and what the report
// says of it, null where it says nothing, as the API answers it.
func reportFile(path string, outcome apigen.ImportOutcome, edit func(*apigen.ImportFile)) apigen.ImportFile {
	f := apigen.ImportFile{Path: path, Outcome: outcome, Questions: []apigen.ImportQuestion{}, Links: []apigen.ImportLink{},
		Attachments: []string{}, Warnings: []apigen.ImportMessage{}, Errors: []apigen.ImportMessage{}}
	for _, n := range []interface{ SetNull() }{&f.Reason, &f.Key, &f.Conflict, &f.Title, &f.TypeReason, &f.Parent, &f.Note,
		&f.ConfidentialReason, &f.Number, &f.Format, &f.Type, &f.State, &f.BlockCandidate, &f.Block, &f.Columns, &f.Assignee,
		&f.Correction} {
		n.SetNull()
	}
	if edit != nil {
		edit(&f)
	}
	return f
}

// message is a warning or an error of a report; "" and 0 are null.
func message(field string, line int, text string) apigen.ImportMessage {
	m := apigen.ImportMessage{Message: text}
	if field == "" {
		m.Field.SetNull()
	} else {
		m.Field.Set(field)
	}
	if line == 0 {
		m.Line.SetNull()
	} else {
		m.Line.Set(line)
	}
	return m
}

// docs/adr/0070 D2, D4: the import's arguments — a project, a path, and
// --dry-run anywhere — and its refusals before anything is asked.
func TestTheImportCommandLine(t *testing.T) {
	dir := ticketsDir(t, t.TempDir())
	code, _, stderr := run(t, Env{}, "import", "acme/COW")
	assert.Equal(t, 2, code, "import takes a project and a path")
	assert.Contains(t, stderr, `unknown command "import acme/COW"`)
	code, _, stderr = run(t, Env{}, "import", "acme-COW", dir)
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr, `"acme-COW" names no project`)
	code, _, stderr = run(t, Env{}, "import", "acme/COW", filepath.Join(dir, "none"))
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "cannot be read")
	code, _, stderr = run(t, Env{}, "import", "--dry-run", "acme/COW", dir)
	assert.Equal(t, 1, code, "import without configuration ends before it asks")
	assert.Contains(t, stderr, "COWORK_URL is not set")
	code, _, stderr = run(t, Env{}, "import", "acme/COW", dir, "--json")
	assert.Equal(t, 2, code, "import takes no --json")
	assert.Contains(t, stderr, "unknown command")
}

// docs/adr/0070 D2, docs/adr/0051 D2, D6: a directory is packed with its
// regular files under the path it was given, the dry run's report printed
// and executed, the executed report printed — every file's outcome, key,
// reason, warnings and errors —; --dry-run stops after the report.
func TestTheImportPacksADirectoryAndPrintsTheReports(t *testing.T) {
	root := t.TempDir()
	ticketsDir(t, root)
	t.Chdir(root)
	dry := importJob(apigen.ImportStatusDryRun, apigen.ImportSummary{Files: 3, Create: 1, Conflict: 1, Skip: 1, Open: 1,
		HighestNumber: nullable.NewNullableWithValue(1)},
		reportFile("docs/tickets/001-a.md", apigen.ImportOutcomeCreate, func(f *apigen.ImportFile) {
			f.Key, f.Title = nullable.NewNullableWithValue("acme/COW-1"), nullable.NewNullableWithValue("a ticket")
			f.Type, f.State = nullable.NewNullableWithValue(apigen.TicketTypeTask), nullable.NewNullableWithValue(apigen.TicketStateFiled)
			f.Warnings = []apigen.ImportMessage{message("opened", 0, "the file names no opened date")}
		}),
		reportFile("docs/tickets/archive/002-b.md", apigen.ImportOutcomeConflict, func(f *apigen.ImportFile) {
			f.Key = nullable.NewNullableWithValue("acme/COW-2")
			f.Reason = nullable.NewNullableWithValue("left out of the import: the project holds its number as acme/COW-2 (docs/adr/0064 D3)")
		}),
		reportFile("docs/tickets/link.md", apigen.ImportOutcomeSkip, func(f *apigen.ImportFile) {
			f.Reason = nullable.NewNullableWithValue("not a regular file")
			f.Errors = []apigen.ImportMessage{message("", 3, "an example error")}
		}))
	done := importJob(apigen.ImportStatusExecuted, apigen.ImportSummary{Files: 3, Created: 1, Conflict: 1, Skip: 1, Open: 1,
		HighestNumber: nullable.NewNullableWithValue(1)}, slices.Clone(dry.Files)...)
	done.Files[0].Outcome = apigen.ImportOutcomeCreated

	var uploaded []string
	var name string
	executions := 0
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/tenants/acme/projects/COW/imports", func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.Header.Get("X-Cowork-Agent"), "cowork-mcp/unknown/", "the binary's requests are an agent's")
		mr, err := r.MultipartReader()
		require.NoError(t, err)
		part, err := mr.NextPart()
		require.NoError(t, err)
		assert.Equal(t, "file", part.FormName())
		name = part.FileName()
		uploaded = entriesOf(t, part)
		_, err = mr.NextPart()
		assert.ErrorIs(t, err, io.EOF, "one part")
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Location", "/api/v1/tenants/acme/projects/COW/imports/"+dry.Id.String())
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(dry)
	})
	mux.HandleFunc("POST /api/v1/tenants/acme/projects/COW/imports/{import}/execution", func(w http.ResponseWriter, r *http.Request) {
		executions++
		assert.Equal(t, dry.Id.String(), r.PathValue("import"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(done)
	})
	env := Env{Lookup: configured, Doer: tools.HandlerDoer{Handler: mux}}

	code, stdout, stderr := run(t, env, "import", "acme/COW", "docs/tickets", "--dry-run")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, "tickets.tar.gz", name)
	assert.Equal(t, []string{"docs/tickets/001-a.md", "docs/tickets/archive/002-b.md"}, uploaded,
		"the files the import reads under the path as it was given, the link and the rest left out")
	assert.Zero(t, executions, "--dry-run stops after the report")
	assert.Contains(t, stdout, "Dry run into acme/COW (job 0199a3c2-1d2e-7f00-8000-0000000000aa, valid until 2026-10-10 08:00:00 UTC): 1 tickets to create (1 open, 0 confidential); 1 conflicts and 0 files with errors to leave out; 1 skipped, 0 excluded; the highest number 1. 3 files:")
	assert.Contains(t, stdout, "docs/tickets/001-a.md: create acme/COW-1 \"a ticket\" (task, filed)\n  warning (opened): the file names no opened date\n")
	assert.Contains(t, stdout, "docs/tickets/archive/002-b.md: conflict acme/COW-2\n  why: left out of the import: the project holds its number as acme/COW-2")
	assert.Contains(t, stdout, "docs/tickets/link.md: skip\n  why: not a regular file\n  error (line 3): an example error\n")
	assert.Contains(t, stdout, "Nothing is imported yet: run the command again without --dry-run")

	code, stdout, stderr = run(t, env, "import", "acme/COW", filepath.Join(root, "docs", "tickets"))
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, []string{"tickets/001-a.md", "tickets/archive/002-b.md"}, uploaded, "an absolute path names the files by its base")
	assert.Equal(t, 1, executions)
	assert.Contains(t, stdout, "Imported into acme/COW (job 0199a3c2-1d2e-7f00-8000-0000000000aa): 1 tickets created (1 open, 0 confidential); 1 conflicts and 0 files with errors left out; 1 skipped, 0 excluded")
	assert.Contains(t, stdout, "docs/tickets/001-a.md: created acme/COW-1")
	assert.Contains(t, stdout, "The importer sets no parent a file does not name")
}

// An archive and a Markdown file go as they are; the installation's refusal
// is the command's error.
func TestTheImportSendsAFileAsItIsAndReportsARefusal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "001-a.md")
	require.NoError(t, os.WriteFile(path, []byte("---\nid: T1\n---\n"), 0o600))
	var got []byte
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/tenants/acme/projects/COW/imports", func(w http.ResponseWriter, r *http.Request) {
		mr, err := r.MultipartReader()
		require.NoError(t, err)
		part, err := mr.NextPart()
		require.NoError(t, err)
		assert.Equal(t, "001-a.md", part.FileName())
		got, _ = io.ReadAll(part)
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(problemBody{Status: 403, Code: "forbidden", Title: "Forbidden", Type: "t", Detail: "the act needs the member role"})
	})
	code, _, stderr := run(t, Env{Lookup: configured, Doer: tools.HandlerDoer{Handler: mux}}, "import", "acme/COW", path)
	assert.Equal(t, 1, code)
	assert.Equal(t, "---\nid: T1\n---\n", string(got))
	assert.Contains(t, stderr, "the dry run into acme/COW failed")
	assert.Contains(t, stderr, "403 forbidden")
}
