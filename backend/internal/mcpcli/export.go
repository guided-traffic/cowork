package mcpcli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/tools"
)

// exportUsage is the export subcommand's usage line.
const exportUsage = "Usage: cowork-mcp export <tenant>/<PROJECT> <dir>\n"

// exportProject fetches a project's export through the generated client and
// unpacks it into an empty or a new directory, never overwriting a file
// (docs/adr/0070 D2, D5): the documents named by key, `<tenant>/<PROJECT>-<n>.md`,
// and the three manifests (docs/adr/0051 D4). The export is recorded on the
// installation as every export is (docs/adr/0059 D3).
func exportProject(ctx context.Context, e Env, args []string) int {
	tenant, project, ok := strings.Cut(args[0], "/")
	if !ok || !domain.ValidTenantSlug(tenant) || !domain.ValidProjectKey(project) {
		fmt.Fprintf(e.Stderr, "cowork-mcp: %q names no project, <tenant>/<PROJECT> such as acme/VKO\n\n%s", args[0], exportUsage)
		return exitUsage
	}
	dir := args[1]
	if err := emptyTarget(dir); err != nil {
		fmt.Fprintf(e.Stderr, "cowork-mcp: %v\n", err)
		return exitError
	}
	cfg, err := loadConfig(e.Lookup)
	if err != nil {
		fmt.Fprintf(e.Stderr, "cowork-mcp: %v\n", err)
		return exitError
	}
	ctx, cancel := context.WithTimeout(ctx, requestBudget)
	defer cancel()
	c := connect(e, cfg, "cowork-mcp", "", "export")
	res, err := c.session.API.ExportProjectWithResponse(ctx, tenant, project)
	switch {
	case err != nil:
		fmt.Fprintf(e.Stderr, "cowork-mcp: %s cannot be reached: %v\n", cfg.url, err)
		return exitError
	case res.StatusCode() != 200:
		api := &tools.APIError{Status: res.StatusCode(), Problem: res.ApplicationproblemJSONDefault, Body: string(res.Body)}
		fmt.Fprintf(e.Stderr, "cowork-mcp: the export of %s failed: %v\n", args[0], api)
		return exitError
	}
	manifest, err := unpack(res.Body, dir, tenant, project)
	if err != nil {
		fmt.Fprintf(e.Stderr, "cowork-mcp: %v\n", err)
		return exitError
	}
	fmt.Fprintf(e.Stdout, "Exported %s into %s: %d tickets as Markdown, the links and the attachments as manifests — the bytes of the attachments stay on the installation.\n",
		args[0], dir, manifest.Tickets)
	if manifest.ConfidentialNotIncluded > 0 {
		fmt.Fprintf(e.Stdout, "%d confidential tickets are not included: the token's person cannot read them.\n", manifest.ConfidentialNotIncluded)
	}
	return exitOK
}

// emptyTarget accepts a directory that does not exist yet, or one that is
// empty: an export never overwrites (docs/adr/0070 D5).
func emptyTarget(dir string) error {
	info, err := os.Stat(dir)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return nil
	case err != nil:
		return fmt.Errorf("the directory %s cannot be read: %w", dir, err)
	case !info.IsDir():
		return fmt.Errorf("%s is no directory", dir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("the directory %s cannot be read: %w", dir, err)
	}
	if len(entries) > 0 {
		return fmt.Errorf("%s is not empty: an export never overwrites, give it an empty or a new directory", dir)
	}
	return nil
}

// exportManifest is the manifest at the root of an export, and
// exportManifests are its three manifests (docs/adr/0051 D4).
const exportManifest = "manifest.json"

var exportManifests = map[string]bool{exportManifest: true, "links.json": true, "attachments.json": true}

// unpack writes the files of the export of tenant/project under dir, through
// a root opened at dir, which no name or link reaches out of: only the names
// such an export holds, regular files only, each created anew — a file that
// exists is an error, never overwritten. On POSIX systems the directories
// are the person's alone, 0700, and the files 0600, since an export may hold
// confidential tickets; Windows applies no such mode, and the files take the
// access list of the directory they are written to.
func unpack(archive []byte, dir, tenant, project string) (apigen.ExportManifest, error) {
	var manifest apigen.ExportManifest
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return manifest, fmt.Errorf("the export is no tar.gz: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return manifest, fmt.Errorf("create %s: %w", dir, err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return manifest, fmt.Errorf("open %s: %w", dir, err)
	}
	defer func() { _ = root.Close() }()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return manifest, nil
		}
		if err != nil {
			return manifest, fmt.Errorf("read the export: %w", err)
		}
		if err := exportEntry(h, tenant, project); err != nil {
			return manifest, err
		}
		body, err := writeNew(root, h.Name, tr)
		if err != nil {
			return manifest, err
		}
		if h.Name == exportManifest {
			if err := json.Unmarshal(body, &manifest); err != nil {
				return manifest, fmt.Errorf("read the export's manifest: %w", err)
			}
		}
	}
}

// exportEntry accepts an entry of the archive that the export of
// tenant/project holds (docs/adr/0051 D4): a regular file, named as a valid
// fs path, that is one of the three manifests or a ticket's document
// <tenant>/<PROJECT>-<n>.md of the project. Every other entry is refused,
// whatever separators, steps or volume its name holds.
func exportEntry(h *tar.Header, tenant, project string) error {
	switch {
	case h.Typeflag != tar.TypeReg:
		return fmt.Errorf("the export holds %q, which is no regular file", h.Name)
	case !fs.ValidPath(h.Name) || !exportName(h.Name, tenant, project):
		return fmt.Errorf("the export holds %q, a name no export of %s/%s holds", h.Name, tenant, project)
	}
	return nil
}

// exportName reports whether name is one of the manifests, or the document of
// a ticket of tenant/project, its number written as the key writes it.
func exportName(name, tenant, project string) bool {
	if exportManifests[name] {
		return true
	}
	dir, file, ok := strings.Cut(name, "/")
	stem, document := strings.CutSuffix(file, ".md")
	if !ok || !document || dir != tenant {
		return false
	}
	key, err := domain.ParseTicketKey(stem)
	return err == nil && key.Tenant == "" && key.Project == project
}

// writeNew creates the file of an entry inside the root, never over one that
// exists, and returns what it wrote.
func writeNew(root *os.Root, name string, r io.Reader) ([]byte, error) {
	file := filepath.FromSlash(name)
	if dir := filepath.Dir(file); dir != "." {
		if err := root.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("create %s: %w", dir, err)
		}
	}
	body, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read the export: %w", err)
	}
	f, err := root.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf("write %s: %w", file, err)
	}
	if _, err := f.Write(body); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("write %s: %w", file, err)
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("write %s: %w", file, err)
	}
	return body, nil
}
