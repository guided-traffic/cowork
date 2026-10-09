package mcpcli

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/tools"
)

// importUsage is the import subcommand's usage line.
const importUsage = "Usage: cowork-mcp import <tenant>/<PROJECT> <path> [--dry-run]\n"

// flagDryRun stops the import after its dry run's report.
const flagDryRun = "--dry-run"

// importProject imports a repository's ticket files into a project through
// the generated client (docs/adr/0070 D2, docs/adr/0051): a directory is
// packed as a tar.gz, an archive or a Markdown file is sent as it is; the dry
// run's report is printed and, without --dry-run, the dry run executed and
// its report printed — every file's outcome, key, warnings, errors and why a
// file was left out, as plain text an agent reads (docs/adr/0051 D2, D6).
func importProject(ctx context.Context, e Env, args []string, dryRun bool) int {
	tenant, project, ok := strings.Cut(args[0], "/")
	if !ok || !domain.ValidTenantSlug(tenant) || !domain.ValidProjectKey(project) {
		fmt.Fprintf(e.Stderr, "cowork-mcp: %q names no project, <tenant>/<PROJECT> such as acme/VKO\n\n%s", args[0], importUsage)
		return exitUsage
	}
	name, body, err := uploadOf(args[1])
	if err != nil {
		fmt.Fprintf(e.Stderr, "cowork-mcp: %v\n", err)
		return exitError
	}
	cfg, err := loadConfig(e.Lookup)
	if err != nil {
		fmt.Fprintf(e.Stderr, "cowork-mcp: %v\n", err)
		return exitError
	}
	c := connect(e, cfg, "cowork-mcp", "", "import")
	job, err := createImport(ctx, c, tenant, project, name, body)
	if err != nil {
		fmt.Fprintf(e.Stderr, "cowork-mcp: the dry run into %s failed: %v\n", args[0], err)
		return exitError
	}
	writeImport(e.Stdout, tenant, job)
	if dryRun {
		fmt.Fprintf(e.Stdout, "\nNothing is imported yet: run the command again without %s, or execute this dry run with POST /api/v1/tenants/%s/projects/%s/imports/%s/execution, within its day.\n",
			flagDryRun, tenant, project, job.Id)
		return exitOK
	}
	rctx, cancel := context.WithTimeout(ctx, requestBudget)
	defer cancel()
	res, err := c.session.API.ExecuteImportWithResponse(rctx, tenant, project, job.Id, apigen.ImportExecution{})
	switch {
	case err != nil:
		fmt.Fprintf(e.Stderr, "cowork-mcp: %s cannot be reached: %v\n", cfg.url, err)
		return exitError
	case res.StatusCode() != http.StatusOK:
		api := &tools.APIError{Status: res.StatusCode(), Problem: res.ApplicationproblemJSONDefault, Body: string(res.Body)}
		fmt.Fprintf(e.Stderr, "cowork-mcp: the execution of the dry run %s failed: %v\n", job.Id, api)
		return exitError
	}
	fmt.Fprintln(e.Stdout)
	writeImport(e.Stdout, tenant, res.JSON200)
	fmt.Fprintf(e.Stdout, "\nThe importer sets no parent a file does not name: make a family's children its children with PATCH /api/v1/tenants/%s/projects/%s/tickets/<n> and {\"parent\": \"<key>\"}, where they belong together.\n",
		tenant, project)
	return exitOK
}

// createImport sends the upload as the dry run's one part named file.
func createImport(ctx context.Context, c *client, tenant, project, name string, body []byte) (*apigen.ImportJob, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile("file", name)
	if err != nil {
		return nil, fmt.Errorf("pack the upload: %w", err)
	}
	if _, err := part.Write(body); err != nil {
		return nil, fmt.Errorf("pack the upload: %w", err)
	}
	if err := mw.Close(); err != nil {
		return nil, fmt.Errorf("pack the upload: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, requestBudget)
	defer cancel()
	res, err := c.session.API.CreateImportWithBodyWithResponse(ctx, tenant, project, mw.FormDataContentType(), &buf)
	switch {
	case err != nil:
		return nil, fmt.Errorf("%s cannot be reached: %w", c.session.Installation, err)
	case res.StatusCode() != http.StatusCreated:
		return nil, &tools.APIError{Status: res.StatusCode(), Problem: res.ApplicationproblemJSONDefault, Body: string(res.Body)}
	}
	return res.JSON201, nil
}

// uploadOf is what the import sends of a path: a directory packed as a
// tar.gz of the files the import reads, each named by its path under the
// directory as it was given; a file as it is — a tar.gz or a zip, which the
// server knows by its bytes, or one Markdown file.
func uploadOf(path string) (string, []byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", nil, fmt.Errorf("%s cannot be read: %w", path, err)
	}
	if !info.IsDir() {
		body, err := os.ReadFile(path)
		if err != nil {
			return "", nil, fmt.Errorf("%s cannot be read: %w", path, err)
		}
		return filepath.Base(path), body, nil
	}
	body, err := packDir(path)
	if err != nil {
		return "", nil, err
	}
	return filepath.Base(filepath.Clean(path)) + ".tar.gz", body, nil
}

// packDir packs the files under dir the import reads as a tar.gz: the
// Markdown files and an export's manifest.json and links.json
// (docs/adr/0063 D5), so nothing else of a working copy leaves the machine.
// A relative dir names them as the repository does, docs/tickets/…; another,
// by its base name. A link and anything else that is no regular file is left
// out.
func packDir(dir string) ([]byte, error) {
	prefix := filepath.ToSlash(filepath.Clean(dir))
	if filepath.IsAbs(dir) || prefix == ".." || strings.HasPrefix(prefix, "../") {
		prefix = filepath.Base(filepath.Clean(dir))
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() || !importRead(d.Name()) {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)
		if prefix != "." && prefix != "" {
			name = prefix + "/" + name
		}
		return packFile(tw, p, name)
	})
	if err != nil {
		return nil, fmt.Errorf("pack %s: %w", dir, err)
	}
	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("pack %s: %w", dir, err)
	}
	if err := gz.Close(); err != nil {
		return nil, fmt.Errorf("pack %s: %w", dir, err)
	}
	return buf.Bytes(), nil
}

// importRead reports whether the import reads a file of this name.
func importRead(name string) bool {
	return strings.HasSuffix(strings.ToLower(name), ".md") || name == "manifest.json" || name == "links.json"
}

// packFile writes one file into the archive under its name.
func packFile(tw *tar.Writer, path, name string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: info.Size(), ModTime: info.ModTime(), Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	if _, err := io.Copy(tw, f); err != nil {
		return err
	}
	return nil
}

// writeImport prints a job's report: the summary, then every file of the
// upload with its outcome, key, title, why it was skipped, excluded or left
// out, and its warnings and errors with their fields and lines.
func writeImport(w io.Writer, tenant string, job *apigen.ImportJob) {
	bw := bufio.NewWriter(w)
	defer func() { _ = bw.Flush() }()
	s := job.Summary
	key := tenant + "/" + job.Project
	if job.Status == apigen.ImportStatusExecuted {
		fmt.Fprintf(bw, "Imported into %s (job %s): %d tickets created (%d open, %d confidential); %d conflicts and %d files with errors left out; %d skipped, %d excluded",
			key, job.Id, s.Created, s.Open, s.Confidential, s.Conflict, s.Error, s.Skip, s.Exclude)
	} else {
		expires := ""
		if at, err := job.ExpiresAt.Get(); err == nil {
			expires = ", valid until " + at.UTC().Format(time.DateTime) + " UTC"
		}
		fmt.Fprintf(bw, "Dry run into %s (job %s%s): %d tickets to create (%d open, %d confidential); %d conflicts and %d files with errors to leave out; %d skipped, %d excluded",
			key, job.Id, expires, s.Create, s.Open, s.Confidential, s.Conflict, s.Error, s.Skip, s.Exclude)
	}
	if n, err := s.HighestNumber.Get(); err == nil {
		fmt.Fprintf(bw, "; the highest number %d", n)
	}
	fmt.Fprintf(bw, ". %d files:\n", s.Files)
	for _, f := range job.Files {
		writeImportFile(bw, f)
	}
}

// writeImportFile prints one file of a report.
func writeImportFile(w io.Writer, f apigen.ImportFile) {
	fmt.Fprintf(w, "\n%s: %s", f.Path, f.Outcome)
	if k, err := f.Key.Get(); err == nil {
		fmt.Fprintf(w, " %s", k)
	}
	if title, err := f.Title.Get(); err == nil {
		fmt.Fprintf(w, " %q", title)
	}
	var kind []string
	if t, err := f.Type.Get(); err == nil {
		kind = append(kind, string(t))
	}
	if st, err := f.State.Get(); err == nil {
		kind = append(kind, string(st))
	}
	if f.Confidential {
		kind = append(kind, "confidential")
	}
	if len(kind) > 0 {
		fmt.Fprintf(w, " (%s)", strings.Join(kind, ", "))
	}
	fmt.Fprintln(w)
	if reason, err := f.Reason.Get(); err == nil {
		fmt.Fprintf(w, "  why: %s\n", reason)
	}
	if parent, err := f.Parent.Get(); err == nil {
		fmt.Fprintf(w, "  parent: %s\n", parent)
	}
	for _, l := range f.Links {
		fmt.Fprintf(w, "  link: %s %s %s (from %s)\n", l.Direction, l.Type, l.Key, l.Source)
	}
	for _, m := range f.Errors {
		fmt.Fprintf(w, "  error%s: %s\n", where(m), m.Message)
	}
	for _, m := range f.Warnings {
		fmt.Fprintf(w, "  warning%s: %s\n", where(m), m.Message)
	}
}

// where is a message's field and line, as " (field, line n)".
func where(m apigen.ImportMessage) string {
	var at []string
	if field, err := m.Field.Get(); err == nil {
		at = append(at, field)
	}
	if line, err := m.Line.Get(); err == nil {
		at = append(at, fmt.Sprintf("line %d", line))
	}
	if len(at) == 0 {
		return ""
	}
	return " (" + strings.Join(at, ", ") + ")"
}

// withoutFlag takes every occurrence of flag out of args and reports whether
// there was one.
func withoutFlag(args []string, flag string) ([]string, bool) {
	out := make([]string, 0, len(args))
	set := false
	for _, a := range args {
		if a == flag {
			set = true
			continue
		}
		out = append(out, a)
	}
	return out, set
}
