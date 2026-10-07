package api

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/importer"
	"github.com/guided-traffic/cowork/backend/internal/markdown"
	"github.com/guided-traffic/cowork/backend/internal/store"
	"github.com/guided-traffic/cowork/backend/internal/store/readq"
)

// exportArchive is an export as the archive holds it (docs/adr/0051 D4): the
// documents, the three manifests.
type exportArchive struct {
	manifest    apigen.ExportManifest
	links       []apigen.ExportLink
	attachments []apigen.ExportAttachment
	docs        []exportDoc
}

// exportDoc is one ticket's document, named by its key.
type exportDoc struct {
	path string
	body []byte
}

// archiveResponse answers an archive: application/gzip, a download named by
// what it holds and the day, its length.
type archiveResponse struct {
	body     []byte
	filename string
}

func (a archiveResponse) write(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+a.filename+`"`)
	w.Header().Set("Content-Length", strconv.Itoa(len(a.body)))
	w.WriteHeader(http.StatusOK)
	_, err := w.Write(a.body)
	return err
}

// VisitExportProjectResponse writes the project's archive.
func (a archiveResponse) VisitExportProjectResponse(w http.ResponseWriter) error { return a.write(w) }

// VisitExportTenantResponse writes the tenant's archive.
func (a archiveResponse) VisitExportTenantResponse(w http.ResponseWriter) error { return a.write(w) }

// ExportProject answers the project's tickets the caller sees as an archive,
// and records the export (docs/adr/0051 D4, docs/adr/0059 D3): whoever reads
// the project exports it (docs/adr/0051 D6).
func (s *Server) ExportProject(ctx context.Context, req apigen.ExportProjectRequestObject) (apigen.ExportProjectResponseObject, error) {
	t := tenantFrom(ctx)
	now := s.h.opts.Now().UTC()
	var p project
	var a exportArchive
	err := s.db.InTenant(ctx, t.ID, func(r *store.Reader) error {
		var err error
		if p, err = visibleProject(ctx, r, t, req.Project); err != nil {
			return err
		}
		role, err := projectRole(ctx, r, t, p)
		if err != nil {
			return err
		}
		if perr := auth.Authorize(principal(ctx), role, read); perr != nil {
			return perr
		}
		return a.gather(ctx, r, t, []exportProject{{ID: p.ID, Key: p.Key, Name: p.Name, Archived: p.ArchivedAt != nil}}, &p.ID, now)
	})
	if err != nil {
		return nil, err
	}
	body, err := a.tarGz(now)
	if err != nil {
		return nil, err
	}
	if err := s.recordExport(ctx, t, entityProject, p.ID, a.manifest); err != nil {
		return nil, err
	}
	return archiveResponse{body: body, filename: fmt.Sprintf("%s-%s-%s.tar.gz", t.Slug, p.Key, now.Format("20060102"))}, nil
}

// ExportTenant answers every project of the tenant the caller sees, archived
// ones included, as one archive — the union of the project exports, each link
// once (docs/adr/0051 D4, the backup's second line of docs/adr/0059 D2).
func (s *Server) ExportTenant(ctx context.Context, _ apigen.ExportTenantRequestObject) (apigen.ExportTenantResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, read); perr != nil {
		return nil, perr
	}
	now := s.h.opts.Now().UTC()
	var a exportArchive
	err := s.db.InTenant(ctx, t.ID, func(r *store.Reader) error {
		rows, err := r.ListProjects(ctx, readq.ListProjectsParams{TenantID: t.ID, IncludeArchived: true, PageSize: math.MaxInt32})
		if err != nil {
			return fmt.Errorf("list the projects: %w", err)
		}
		projects := make([]exportProject, 0, len(rows))
		for _, row := range rows {
			projects = append(projects, exportProject{ID: row.ID, Key: row.Key, Name: row.Name, Archived: row.ArchivedAt != nil})
		}
		return a.gather(ctx, r, t, projects, nil, now)
	})
	if err != nil {
		return nil, err
	}
	body, err := a.tarGz(now)
	if err != nil {
		return nil, err
	}
	if err := s.recordExport(ctx, t, entityTenant, t.ID, a.manifest); err != nil {
		return nil, err
	}
	return archiveResponse{body: body, filename: fmt.Sprintf("%s-%s.tar.gz", t.Slug, now.Format("20060102"))}, nil
}

// recordExport records the export as data that left the system, one act on
// what was exported (docs/adr/0059 D3, docs/adr/0026 D5); never published.
func (s *Server) recordExport(ctx context.Context, t tenantScope, entity string, id uuid.UUID, m apigen.ExportManifest) error {
	_, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		w.Record(store.Event{EntityType: entity, EntityID: id, Action: "exported", After: map[string]any{
			"format": string(m.Format), "tickets": m.Tickets, "projects": len(m.Projects),
			"confidential_not_included": m.ConfidentialNotIncluded}})
		return nil
	})
	return err
}

// exportProject is a project an export holds.
type exportProject struct {
	ID       uuid.UUID
	Key      string
	Name     string
	Archived bool
}

// gather reads what the archive holds, in one read transaction: every ticket
// of the projects the caller sees as its …/markdown document, the links, the
// attachments' metadata, the count of the confidential tickets left out, and
// the exporter. project narrows the manifests to one project; nil is the
// tenant's.
func (a *exportArchive) gather(ctx context.Context, r *store.Reader, t tenantScope, projects []exportProject, projectID *uuid.UUID, now time.Time) error {
	exporter, err := exporterOf(ctx, r)
	if err != nil {
		return err
	}
	a.manifest = apigen.ExportManifest{Format: apigen.ExportManifestFormat(importer.ExportFormat), Tenant: t.Slug,
		ExportedAt: now, ExportedBy: exporter, Projects: []apigen.ExportManifestProject{}}
	counts, err := a.documents(ctx, r, t, projectID)
	if err != nil {
		return err
	}
	hidden, err := r.ExportHiddenConfidential(ctx, readq.ExportHiddenConfidentialParams{TenantID: t.ID, ProjectID: projectID})
	if err != nil {
		return fmt.Errorf("count the confidential tickets left out: %w", err)
	}
	left := map[uuid.UUID]int{}
	for _, h := range hidden {
		left[h.ProjectID] = int(h.Hidden)
	}
	for _, p := range projects {
		a.manifest.Projects = append(a.manifest.Projects, apigen.ExportManifestProject{Key: p.Key, Name: p.Name, Archived: p.Archived,
			Tickets: counts[p.Key], ConfidentialNotIncluded: left[p.ID]})
		a.manifest.Tickets += counts[p.Key]
		a.manifest.ConfidentialNotIncluded += left[p.ID]
	}
	if err := a.readLinks(ctx, r, t, projectID); err != nil {
		return err
	}
	return a.readAttachments(ctx, r, t, projectID)
}

// exporterOf is the caller as grammar v1 writes a person (docs/adr/0044 D1).
func exporterOf(ctx context.Context, r *store.Reader) (string, error) {
	p := principal(ctx)
	person := markdown.Person{Name: p.DisplayName}
	id, err := r.ExportPerson(ctx, p.PersonID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("read the exporter: %w", err)
	}
	person.Username, person.Issuer, person.Subject = deref(id.Username), deref(id.OidcIssuer), deref(id.OidcSubject)
	return person.String(), nil
}

// documents renders every ticket the caller sees, done and dropped ones
// included, deleted ones not, each exactly as …/markdown answers it; it
// returns how many each project holds.
func (a *exportArchive) documents(ctx context.Context, r *store.Reader, t tenantScope, projectID *uuid.UUID) (map[string]int, error) {
	f := store.TicketFilter{IncludeTerminal: true}
	if projectID != nil {
		f.ProjectID = *projectID
	}
	list, err := r.ListTickets(ctx, f, store.TicketPage{Order: store.NewestFirst, Limit: math.MaxInt32})
	if err != nil {
		return nil, err
	}
	rows := list.Rows
	slices.SortFunc(rows, func(x, y store.TicketRow) int {
		if c := strings.Compare(x.ProjectKey, y.ProjectKey); c != 0 {
			return c
		}
		return int(x.Number) - int(y.Number)
	})
	counts := map[string]int{}
	for _, row := range rows {
		doc, err := exportDocument(ctx, r, t, ticketCtx{row: row})
		if err != nil {
			return nil, err
		}
		a.docs = append(a.docs, exportDoc{path: ticketKey(t, row) + ".md", body: markdown.Render(doc)})
		counts[row.ProjectKey]++
	}
	return counts, nil
}

// readLinks reads the links manifest: every link whose two ends the caller
// sees, once (docs/adr/0051 D4).
func (a *exportArchive) readLinks(ctx context.Context, r *store.Reader, t tenantScope, projectID *uuid.UUID) error {
	rows, err := r.ExportLinks(ctx, readq.ExportLinksParams{TenantID: t.ID, ProjectID: projectID})
	if err != nil {
		return fmt.Errorf("read the links: %w", err)
	}
	a.links = make([]apigen.ExportLink, 0, len(rows))
	for _, l := range rows {
		a.links = append(a.links, apigen.ExportLink{Source: domain.FullKey(t.Slug, l.SourceProject, l.SourceNumber),
			Type: apigen.LinkType(l.Type), Target: domain.FullKey(t.Slug, l.TargetProject, l.TargetNumber)})
	}
	return nil
}

// readAttachments reads the attachments manifest: names, types, sizes and the
// paths of the bytes, never the bytes (docs/adr/0016 D5).
func (a *exportArchive) readAttachments(ctx context.Context, r *store.Reader, t tenantScope, projectID *uuid.UUID) error {
	rows, err := r.ExportAttachments(ctx, readq.ExportAttachmentsParams{TenantID: t.ID, ProjectID: projectID})
	if err != nil {
		return fmt.Errorf("read the attachments: %w", err)
	}
	a.attachments = make([]apigen.ExportAttachment, 0, len(rows))
	for _, at := range rows {
		a.attachments = append(a.attachments, apigen.ExportAttachment{Ticket: domain.FullKey(t.Slug, at.ProjectKey, at.Number),
			Name: at.FileName, Type: at.ContentType, Size: at.Size,
			Url: attachmentContentURL(t, ticketAt{project: at.ProjectKey, number: at.Number}, at.ID)})
	}
	return nil
}

// tarGz writes the archive: the three manifests at its root, then the
// documents by key, every entry of the export's time.
func (a *exportArchive) tarGz(now time.Time) ([]byte, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	files := []exportDoc{}
	for _, m := range []struct {
		name string
		v    any
	}{{importer.ManifestFile, a.manifest}, {importer.LinksFile, a.links}, {importer.AttachmentsFile, a.attachments}} {
		body, err := json.MarshalIndent(m.v, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("encode %s: %w", m.name, err)
		}
		files = append(files, exportDoc{path: m.name, body: append(body, '\n')})
	}
	for _, f := range append(files, a.docs...) {
		h := &tar.Header{Name: f.path, Mode: 0o644, Size: int64(len(f.body)), ModTime: now, Typeflag: tar.TypeReg, Format: tar.FormatPAX}
		if err := tw.WriteHeader(h); err != nil {
			return nil, fmt.Errorf("write the archive: %w", err)
		}
		if _, err := tw.Write(f.body); err != nil {
			return nil, fmt.Errorf("write the archive: %w", err)
		}
	}
	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("write the archive: %w", err)
	}
	if err := gz.Close(); err != nil {
		return nil, fmt.Errorf("write the archive: %w", err)
	}
	return buf.Bytes(), nil
}
