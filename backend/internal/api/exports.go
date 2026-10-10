package api

import (
	"archive/tar"
	"cmp"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/importer"
	"github.com/guided-traffic/cowork/backend/internal/markdown"
	"github.com/guided-traffic/cowork/backend/internal/requestid"
	"github.com/guided-traffic/cowork/backend/internal/store"
	"github.com/guided-traffic/cowork/backend/internal/store/readq"
)

// exportPage is how many tickets an export reads at once: one page of bodies
// is what an export holds, never its archive (docs/adr/0051 D7).
const exportPage = 50

// exportArchive is what an export's archive holds besides the documents
// (docs/adr/0051 D4): the three manifests, read before the archive starts.
type exportArchive struct {
	manifest    apigen.ExportManifest
	links       []apigen.ExportLink
	attachments []apigen.ExportAttachment
}

// exportStream answers an archive as it writes it (docs/adr/0051 D4, D7). Who
// may export what is decided before the answer starts; the replica's export
// slot, the snapshot, the act and the archive when the answer is written.
type exportStream struct {
	s        *Server
	ctx      context.Context
	t        tenantScope
	projects []exportProject
	// project narrows the manifests to the project's export; nil is the
	// tenant's.
	project  *uuid.UUID
	entity   string
	entityID uuid.UUID
	now      time.Time
	filename string
}

// VisitExportProjectResponse writes the project's archive.
func (x exportStream) VisitExportProjectResponse(w http.ResponseWriter) error { return x.write(w) }

// VisitExportTeamResponse writes the team's archive.
func (x exportStream) VisitExportTeamResponse(w http.ResponseWriter) error { return x.write(w) }

// write waits for the replica's export slot, reads one snapshot, records the
// export and streams the archive: application/gzip, a download named by what
// it holds and the day. Until the answer starts, a failure is a problem like
// any other; after it, the answer is cut off, so that no reader takes a
// truncated archive for a whole one.
func (x exportStream) write(w http.ResponseWriter) error {
	release, err := slot(x.ctx, x.s.exports)
	if err != nil {
		return err
	}
	defer release()
	started := false
	err = x.s.db.InTenantSnapshot(x.ctx, x.t.ID, func(r *store.Reader) error {
		var a exportArchive
		if err := a.gather(x.ctx, r, x.t, x.projects, x.project, x.now); err != nil {
			return err
		}
		if err := x.s.recordExport(x.ctx, x.t, x.entity, x.entityID, a.manifest); err != nil {
			return err
		}
		w.Header().Set("Content-Type", "application/gzip")
		w.Header().Set("Content-Disposition", `attachment; filename="`+x.filename+`"`)
		w.WriteHeader(http.StatusOK)
		started = true
		return a.stream(x.ctx, r, x.t, w, x.projects, x.now)
	})
	if err != nil && started {
		x.s.h.logger.Error("the export ended before its archive", "request_id", requestid.From(x.ctx), "error", err)
		panic(http.ErrAbortHandler)
	}
	return err
}

// ExportProject answers the project's tickets the caller sees as an archive,
// and records the export (docs/adr/0051 D4, docs/adr/0059 D3): whoever reads
// the project exports it (docs/adr/0051 D6).
func (s *Server) ExportProject(ctx context.Context, req apigen.ExportProjectRequestObject) (apigen.ExportProjectResponseObject, error) {
	t := tenantFrom(ctx)
	now := s.h.opts.Now().UTC()
	var p project
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
		return nil
	})
	if err != nil {
		return nil, err
	}
	return exportStream{s: s, ctx: ctx, t: t, now: now, project: &p.ID, entity: entityProject, entityID: p.ID,
		projects: []exportProject{{ID: p.ID, Key: p.Key, Name: p.Name, Archived: p.ArchivedAt != nil}},
		filename: fmt.Sprintf("%s-%s-%s.tar.gz", t.Slug, p.Key, now.Format("20060102"))}, nil
}

// ExportTeam answers every project of the team the caller sees, archived
// ones included, as one archive — the union of the project exports, each link
// once (docs/adr/0051 D4, the backup's second line of docs/adr/0059 D2).
func (s *Server) ExportTeam(ctx context.Context, _ apigen.ExportTeamRequestObject) (apigen.ExportTeamResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, read); perr != nil {
		return nil, perr
	}
	now := s.h.opts.Now().UTC()
	var projects []exportProject
	err := s.db.InTenant(ctx, t.ID, func(r *store.Reader) error {
		rows, err := r.ListProjects(ctx, readq.ListProjectsParams{TenantID: t.ID, IncludeArchived: true, PageSize: math.MaxInt32})
		if err != nil {
			return fmt.Errorf("list the projects: %w", err)
		}
		projects = make([]exportProject, 0, len(rows))
		for _, row := range rows {
			projects = append(projects, exportProject{ID: row.ID, Key: row.Key, Name: row.Name, Archived: row.ArchivedAt != nil})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return exportStream{s: s, ctx: ctx, t: t, now: now, projects: projects, entity: entityTenant, entityID: t.ID,
		filename: fmt.Sprintf("%s-%s.tar.gz", t.Slug, now.Format("20060102"))}, nil
}

// recordExport records the export as data that left the system, one act on
// what was exported (docs/adr/0059 D3, docs/adr/0026 D5); never published.
func (s *Server) recordExport(ctx context.Context, t tenantScope, entity string, id uuid.UUID, m apigen.ExportManifest) error {
	_, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		w.Record(store.Event{EntityType: entity, EntityID: id, Action: actionExported, After: map[string]any{
			fieldFormat: string(m.Format), "tickets": m.Tickets, "projects": len(m.Projects),
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

// gather reads what the archive holds besides the documents, in the export's
// snapshot: the count of the documents of each project and of the
// confidential tickets it leaves out, the links, the attachments' metadata
// and the exporter. projectID narrows the manifests to one project; nil is
// the tenant's.
func (a *exportArchive) gather(ctx context.Context, r *store.Reader, t tenantScope, projects []exportProject, projectID *uuid.UUID, now time.Time) error {
	exporter, err := exporterOf(ctx, r)
	if err != nil {
		return err
	}
	a.manifest = apigen.ExportManifest{Format: apigen.ExportManifestFormat(importer.ExportFormat), Team: t.Slug,
		Tenant:     t.Slug, //nolint:staticcheck // SA1019: deprecated in the document, written beside team for the importers of the release before
		ExportedAt: now, ExportedBy: exporter, Projects: []apigen.ExportManifestProject{}}
	hidden, err := r.ExportHiddenConfidential(ctx, readq.ExportHiddenConfidentialParams{TenantID: t.ID, ProjectID: projectID})
	if err != nil {
		return fmt.Errorf("count the confidential tickets left out: %w", err)
	}
	left := map[uuid.UUID]int{}
	for _, h := range hidden {
		left[h.ProjectID] = int(h.Hidden)
	}
	for _, p := range projects {
		n, err := r.CountTickets(ctx, exportFilter(p))
		if err != nil {
			return err
		}
		a.manifest.Projects = append(a.manifest.Projects, apigen.ExportManifestProject{Key: p.Key, Name: p.Name, Archived: p.Archived,
			Tickets: int(n), ConfidentialNotIncluded: left[p.ID]})
		a.manifest.Tickets += int(n)
		a.manifest.ConfidentialNotIncluded += left[p.ID]
	}
	if err := a.readLinks(ctx, r, t, projectID); err != nil {
		return err
	}
	if err := a.readLinksElsewhere(ctx, r, t, projects); err != nil {
		return err
	}
	return a.readAttachments(ctx, r, t, projectID)
}

// exportAnchors is how many tickets one read of their links into other teams
// names at most.
const exportAnchors = 500

// readLinksElsewhere adds the links between the exported tickets and tickets
// of other teams to the links manifest, in the export's snapshot: each by the
// keys of its two ends and its type, the other end's key the only thing of it
// the archive holds — never its head's text —, and none whose other end the
// caller may not see (docs/adr/0051 D9 as made concrete 2026-10-10). A link
// has one exported end, so each is listed once.
func (a *exportArchive) readLinksElsewhere(ctx context.Context, r *store.Reader, t tenantScope, projects []exportProject) error {
	var (
		ids  []uuid.UUID
		keys = map[uuid.UUID]string{}
	)
	for _, p := range projects {
		after := ""
		for {
			list, err := r.ListTickets(ctx, exportFilter(p), store.TicketPage{Order: store.ByNumber, After: after, Limit: exportPage})
			if err != nil {
				return err
			}
			for _, row := range list.Rows {
				ids, keys[row.ID] = append(ids, row.ID), ticketKey(t, row)
			}
			if len(list.Rows) < exportPage {
				break
			}
			after = store.ByNumber.Position(list.Rows[len(list.Rows)-1])
		}
	}
	var elsewhere []apigen.ExportLink
	for start := 0; start < len(ids); start += exportAnchors {
		rels, err := r.RelationHeads(ctx, ids[start:min(start+exportAnchors, len(ids))], store.RelationLink)
		if err != nil {
			return err
		}
		for _, rel := range rels {
			if rel.Head.TeamSlug == t.Slug || rel.Head.Placeholder() || rel.Link == nil {
				continue
			}
			l := apigen.ExportLink{Source: keys[rel.Anchor], Type: apigen.LinkType(rel.Link.Type), Target: rel.Head.Key()}
			if !rel.Link.Outgoing {
				l.Source, l.Target = l.Target, l.Source
			}
			elsewhere = append(elsewhere, l)
		}
	}
	slices.SortFunc(elsewhere, func(x, y apigen.ExportLink) int {
		return cmp.Or(strings.Compare(x.Source, y.Source), strings.Compare(string(x.Type), string(y.Type)),
			strings.Compare(x.Target, y.Target))
	})
	a.links = append(a.links, elsewhere...)
	return nil
}

// exportFilter is every ticket of the project the caller sees, done and
// dropped ones included, deleted ones not.
func exportFilter(p exportProject) store.TicketFilter {
	return store.TicketFilter{ProjectID: p.ID, IncludeTerminal: true}
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

// documents writes the project's documents by number, each exactly as
// …/markdown answers it, reading a page of tickets at a time.
func documents(ctx context.Context, r *store.Reader, t tenantScope, tw *tar.Writer, p exportProject, now time.Time) error {
	after := ""
	for {
		list, err := r.ListTickets(ctx, exportFilter(p), store.TicketPage{Order: store.ByNumber, After: after, Limit: exportPage})
		if err != nil {
			return err
		}
		shows, err := showingAll(ctx, r, list.Rows)
		if err != nil {
			return err
		}
		for _, st := range shows {
			doc, err := exportDocument(ctx, r, t, st)
			if err != nil {
				return err
			}
			if err := writeEntry(tw, ticketKey(t, st.row)+".md", markdown.Render(doc), now); err != nil {
				return err
			}
		}
		if len(list.Rows) < exportPage {
			return nil
		}
		after = store.ByNumber.Position(list.Rows[len(list.Rows)-1])
	}
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

// stream writes the archive to w as it goes: the three manifests at its root,
// then the documents by project key and number, every entry of the export's
// time.
func (a *exportArchive) stream(ctx context.Context, r *store.Reader, t tenantScope, w io.Writer, projects []exportProject, now time.Time) error {
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	for _, m := range []struct {
		name string
		v    any
	}{{importer.ManifestFile, a.manifest}, {importer.LinksFile, a.links}, {importer.AttachmentsFile, a.attachments}} {
		body, err := json.MarshalIndent(m.v, "", "  ")
		if err != nil {
			return fmt.Errorf("encode %s: %w", m.name, err)
		}
		if err := writeEntry(tw, m.name, append(body, '\n'), now); err != nil {
			return err
		}
	}
	byKey := slices.Clone(projects)
	slices.SortFunc(byKey, func(x, y exportProject) int { return strings.Compare(x.Key, y.Key) })
	for _, p := range byKey {
		if err := documents(ctx, r, t, tw, p, now); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return fmt.Errorf("write the archive: %w", err)
	}
	if err := gz.Close(); err != nil {
		return fmt.Errorf("write the archive: %w", err)
	}
	return nil
}

// writeEntry writes one file of the archive.
func writeEntry(tw *tar.Writer, name string, body []byte, now time.Time) error {
	h := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), ModTime: now, Typeflag: tar.TypeReg, Format: tar.FormatPAX}
	if err := tw.WriteHeader(h); err != nil {
		return fmt.Errorf("write the archive: %w", err)
	}
	if _, err := tw.Write(body); err != nil {
		return fmt.Errorf("write the archive: %w", err)
	}
	return nil
}
