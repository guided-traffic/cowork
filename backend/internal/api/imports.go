package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/importer"
	"github.com/guided-traffic/cowork/backend/internal/problem"
	"github.com/guided-traffic/cowork/backend/internal/store"
	"github.com/guided-traffic/cowork/backend/internal/store/readq"
	"github.com/guided-traffic/cowork/backend/internal/store/writeq"
)

// The entity of an import job's acts, and the act of its execution
// (docs/adr/0051 D3, docs/adr/0026 D1).
const (
	entityImportJob = "import_job"
	actionImported  = "imported"
	fieldImportJob  = "import_job"
	fieldFileName   = "file"
	importExecuted  = "executed"
	// opCreateImport is the operation whose upload COWORK_MAX_IMPORT_BYTES
	// bounds (limitBody).
	opCreateImport = "createImport"
)

// slot waits for one of a replica's slots within the request's deadline and
// answers its release (docs/adr/0051 D7): the imports' holds the replica to
// one import at a time, a dry run or an execution — an upload is held in
// memory, unpacked, and kept once more compressed, up to
// COWORK_MAX_IMPORT_BYTES each —, the exports' to one export at a time, each
// holding a page of tickets while it streams its archive.
func slot(ctx context.Context, slots chan struct{}) (func(), error) {
	select {
	case slots <- struct{}{}:
		return func() { <-slots }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// importURL is the job's address.
func importURL(t tenantScope, project string, id uuid.UUID) string {
	return fmt.Sprintf("/api/v1/tenants/%s/projects/%s/imports/%s", t.Slug, project, id)
}

// CreateImport reads an upload into a dry run: every file read, the report
// made against the project as it stands, the files kept compressed for the
// execution, nothing imported (docs/adr/0051 D1, D2). A writer's act of the
// project, as creating a ticket is, an agent's included (D6); the tenant's
// role is held to it before the upload is read, the project's in the
// transaction.
func (s *Server) CreateImport(ctx context.Context, req apigen.CreateImportRequestObject) (apigen.CreateImportResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, work); perr != nil {
		return nil, perr
	}
	release, err := slot(ctx, s.imports)
	if err != nil {
		return nil, err
	}
	defer release()
	sources, err := importer.ReadUpload(req.Body, importer.Limits{MaxBytes: s.h.opts.MaxImportBytes, MaxFiles: importer.MaxFiles})
	if err != nil {
		return nil, uploadProblem(err)
	}
	packed, err := importer.Pack(sources)
	if err != nil {
		return nil, err
	}
	upload := importer.Read(sources)
	id, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("make the import job's id: %w", err)
	}
	now := s.h.opts.Now()
	var job readq.GetImportJobRow
	_, err = s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		p, err := importProject(ctx, w.Reader, t, req.Project)
		if err != nil {
			return err
		}
		target, err := s.importTarget(ctx, w.Reader, t, p, upload, nil, nil)
		if err != nil {
			return err
		}
		result := importer.Analyze(upload, target, nil)
		report, err := json.Marshal(result.Report)
		if err != nil {
			return fmt.Errorf("encode the report: %w", err)
		}
		expires := now.Add(store.ImportValidity)
		if err := w.InsertImportJob(ctx, writeq.InsertImportJobParams{ID: id, TenantID: t.ID, ProjectID: p.ID,
			CreatedBy: principal(ctx).PersonID, ExpiresAt: &expires, Report: report, Source: packed}); err != nil {
			return fmt.Errorf("insert the import job: %w", err)
		}
		w.Record(store.Event{EntityType: entityImportJob, EntityID: id, Action: actionCreated,
			After: summaryAct(p.Key, "dry_run", result.Report.Summary)})
		job, err = w.GetImportJob(ctx, readq.GetImportJobParams{TenantID: t.ID, ProjectID: p.ID, ID: id, Now: &now})
		return err
	})
	if err != nil {
		return nil, err
	}
	view, err := importJobView(req.Project, job)
	if err != nil {
		return nil, err
	}
	location := importURL(t, req.Project, id)
	return apigen.CreateImport201JSONResponse{Body: view, Headers: apigen.CreateImport201ResponseHeaders{Location: &location}}, nil
}

// uploadProblem answers an upload the import refuses: past the limits 413, a
// broken one 400 at /file, a body cut off by its limit or its deadline as
// those answer elsewhere (docs/adr/0039 D2).
func uploadProblem(err error) error {
	var ue *importer.UploadError
	var tooBig *http.MaxBytesError
	switch {
	case errors.As(err, &ue) && ue.TooLarge:
		return problem.New(problem.PayloadTooLarge, ue.Message)
	case errors.As(err, &ue):
		return problem.Field("/file", ue.Message)
	case errors.As(err, &tooBig):
		return tooLarge(tooBig.Limit)
	case errors.Is(err, os.ErrDeadlineExceeded):
		return problem.New(problem.Timeout, "the upload did not arrive within the request timeout")
	}
	return problem.New(problem.ValidationFailed, "the upload is not a readable multipart body")
}

// importProject is the project an import goes into: one the caller sees and
// writes, as creating a ticket needs (docs/adr/0051 D6), not archived — an
// archived project takes no new ticket (docs/adr/0006 D4).
func importProject(ctx context.Context, r *store.Reader, t tenantScope, key string) (project, error) {
	p, err := importWriter(ctx, r, t, key)
	if err != nil {
		return p, err
	}
	if p.ArchivedAt != nil {
		return p, problem.New(problem.ProjectArchived, "the project is archived")
	}
	return p, nil
}

// importWriter is a project the caller sees and writes: its role there, a
// restricted project's lowered to its list's entry, and the token's write
// scope (docs/adr/0034 D3, docs/adr/0035 D3).
func importWriter(ctx context.Context, r *store.Reader, t tenantScope, key string) (project, error) {
	p, err := visibleProject(ctx, r, t, key)
	if err != nil {
		return p, err
	}
	role, err := projectRole(ctx, r, t, p)
	if err != nil {
		return p, err
	}
	if perr := auth.Authorize(principal(ctx), role, work); perr != nil {
		return p, perr
	}
	return p, nil
}

// summaryAct is what the acts on an import job record: the project, the
// status and the counts, never a file's content.
func summaryAct(project, status string, s importer.Summary) map[string]any {
	return map[string]any{"project": project, "status": status, "files": s.Files, "create": s.Create, "created": s.Created,
		"conflict": s.Conflict, "error": s.Error, "skip": s.Skip, "exclude": s.Exclude}
}

// GetImport answers a job with its report; a dry run past its day is gone
// (docs/adr/0051 D7). A writer's of the project, an agent's included (D6),
// and the job's: the policies of import_jobs hold it to the person who made
// it and the tenant's administrators, and another's is 404.
func (s *Server) GetImport(ctx context.Context, req apigen.GetImportRequestObject) (apigen.GetImportResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, work); perr != nil {
		return nil, perr
	}
	now := s.h.opts.Now()
	var job readq.GetImportJobRow
	err := s.db.InTenant(ctx, t.ID, func(r *store.Reader) error {
		p, err := importWriter(ctx, r, t, req.Project)
		if err != nil {
			return err
		}
		job, err = r.GetImportJob(ctx, readq.GetImportJobParams{TenantID: t.ID, ProjectID: p.ID, ID: req.Import, Now: &now})
		if errors.Is(err, pgx.ErrNoRows) {
			return problem.New(problem.NotFound, "no such import job")
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	view, err := importJobView(req.Project, job)
	if err != nil {
		return nil, err
	}
	return apigen.GetImport200JSONResponse(view), nil
}

// importJobView answers a job: its row, and its report as stored, which is the
// document's ImportSummary and ImportFile field by field.
func importJobView(projectKey string, j readq.GetImportJobRow) (apigen.ImportJob, error) {
	var report struct {
		Summary apigen.ImportSummary `json:"summary"`
		Files   []apigen.ImportFile  `json:"files"`
	}
	if err := json.Unmarshal(j.Report, &report); err != nil {
		return apigen.ImportJob{}, fmt.Errorf("decode the report: %w", err)
	}
	if report.Files == nil {
		report.Files = []apigen.ImportFile{}
	}
	v := apigen.ImportJob{Id: j.ID, Project: projectKey, Status: apigen.ImportStatus(j.Status),
		CreatedBy: personView(j.CreatedBy, j.CreatedByUsername, j.CreatedByName), CreatedAt: j.CreatedAt,
		ExpiresAt: nullableOf(j.ExpiresAt), ExecutedAt: nullableOf(j.ExecutedAt), Summary: report.Summary, Files: report.Files}
	if j.ExecutedBy != nil {
		v.ExecutedBy.Set(personView(*j.ExecutedBy, j.ExecutedByUsername, j.ExecutedByName))
	} else {
		v.ExecutedBy.SetNull()
	}
	return v, nil
}

// importTarget reads what the analysis needs of the project and its members
// (importer.Needs): the numbers the project holds or a purged ticket held, the
// tickets the references name, and the members the assignees name — each
// one who can see the project, as an assignee must (docs/adr/0065 D9). A
// corrected assignee who is none is refused at its correction. named are the
// members the dry run assigned, for an execution; an agent's request names
// its person, which a confidential ticket may be assigned to alone.
func (s *Server) importTarget(ctx context.Context, r *store.Reader, t tenantScope, p project, u *importer.Upload,
	corrections []importer.Correction, named map[string]uuid.UUID) (importer.Target, error) {
	issuer := ""
	if s.h.opts.OIDC.Provider != nil {
		issuer = s.h.opts.OIDC.Provider.Issuer()
	}
	tg := importer.Target{Tenant: t.Slug, Project: p.Key, Issuer: issuer, Taken: map[int32]bool{}, Purged: map[int32]bool{},
		Existing: map[int32]uuid.UUID{}, Persons: map[string]importer.Person{}, Assignees: map[uuid.UUID]importer.Person{},
		Named: named}
	if caller := principal(ctx); caller.IsAgent() {
		tg.Agent = &caller.PersonID
	}
	needs := u.Needs(issuer)
	if err := importNumbers(ctx, r, t, p, needs, &tg); err != nil {
		return tg, err
	}
	if err := importPersons(ctx, r, t, p, needs, &tg); err != nil {
		return tg, err
	}
	return tg, correctedAssignees(ctx, r, t, p, corrections, &tg)
}

// importNumbers reads the numbers the project holds, those purged tickets
// held, and the project's tickets the references name.
func importNumbers(ctx context.Context, r *store.Reader, t tenantScope, p project, needs importer.Needs, tg *importer.Target) error {
	taken, err := r.ImportNumbersTaken(ctx, readq.ImportNumbersTakenParams{TenantID: t.ID, ProjectID: p.ID, Numbers: nonNilNumbers(needs.Numbers)})
	if err != nil {
		return fmt.Errorf("read the numbers the project holds: %w", err)
	}
	for _, n := range taken {
		tg.Taken[n] = true
	}
	keys := make([]string, 0, len(needs.Numbers))
	for _, n := range needs.Numbers {
		keys = append(keys, domain.FullKey(t.Slug, p.Key, n))
	}
	purged, err := r.ImportPurgedKeys(ctx, readq.ImportPurgedKeysParams{TenantID: &t.ID, Keys: keys})
	if err != nil {
		return fmt.Errorf("read the purged numbers: %w", err)
	}
	for _, key := range purged {
		if k, err := domain.ParseTicketKey(key); err == nil {
			tg.Purged[k.Number] = true
		}
	}
	existing, err := r.ImportReferencedTickets(ctx, readq.ImportReferencedTicketsParams{TenantID: t.ID, ProjectID: p.ID,
		Numbers: nonNilNumbers(needs.Referenced)})
	if err != nil {
		return fmt.Errorf("read the referenced tickets: %w", err)
	}
	for _, e := range existing {
		tg.Existing[e.Number] = e.ID
	}
	return nil
}

func nonNilNumbers(n []int32) []int32 {
	if n == nil {
		return []int32{}
	}
	return n
}

// importPersons reads the members the assignees name by identity and keeps
// those who can see the project.
func importPersons(ctx context.Context, r *store.Reader, t tenantScope, p project, needs importer.Needs, tg *importer.Target) error {
	if len(needs.Usernames) == 0 && len(needs.Subjects) == 0 {
		return nil
	}
	rows, err := r.ImportPersons(ctx, readq.ImportPersonsParams{TenantID: t.ID, Usernames: append([]string{}, needs.Usernames...),
		Issuer: tg.Issuer, Subjects: append([]string{}, needs.Subjects...)})
	if err != nil {
		return fmt.Errorf("read the assignees: %w", err)
	}
	for _, row := range rows {
		visible, err := r.CanSeeProject(ctx, readq.CanSeeProjectParams{TenantID: t.ID, ProjectID: p.ID, UserID: row.ID})
		if err != nil {
			return fmt.Errorf("check an assignee: %w", err)
		}
		if visible {
			tg.Persons[importer.IdentityOf(deref(row.Username), deref(row.OidcIssuer), deref(row.OidcSubject))] =
				importer.Person{ID: row.ID, Username: row.Username, Name: row.DisplayName}
		}
	}
	return nil
}

// correctedAssignees holds each assignee a correction names to a member who
// can see the project.
func correctedAssignees(ctx context.Context, r *store.Reader, t tenantScope, p project, corrections []importer.Correction, tg *importer.Target) error {
	var errs []problem.FieldError
	for i, c := range corrections {
		if c.Assignee == nil {
			continue
		}
		pointer := fmt.Sprintf("/corrections/%d/assignee", i)
		member, err := r.GetMember(ctx, readq.GetMemberParams{TenantID: t.ID, UserID: *c.Assignee})
		if errors.Is(err, pgx.ErrNoRows) {
			errs = append(errs, problem.FieldError{Pointer: pointer, Message: "not a member who can see the project"})
			continue
		}
		if err != nil {
			return fmt.Errorf("read a corrected assignee: %w", err)
		}
		visible, err := r.CanSeeProject(ctx, readq.CanSeeProjectParams{TenantID: t.ID, ProjectID: p.ID, UserID: *c.Assignee})
		if err != nil {
			return fmt.Errorf("check a corrected assignee: %w", err)
		}
		if !visible {
			errs = append(errs, problem.FieldError{Pointer: pointer, Message: "not a member who can see the project"})
			continue
		}
		tg.Assignees[member.ID] = importer.Person{ID: member.ID, Username: member.Username, Name: member.DisplayName}
	}
	if len(errs) > 0 {
		return &problem.Error{Code: problem.ValidationFailed, Detail: "a corrected assignee cannot be assigned", Errors: errs}
	}
	return nil
}

// ExecuteImport executes a dry run with its corrections in one transaction:
// every ticket, question and link of the files it can import, the sequence
// advanced, or nothing (docs/adr/0051 D3); a file with an error or a conflict
// is left out, and the report says why (D2). The project's rank lock is taken
// first: filings wait, and the numbers the analysis checks cannot be taken
// meanwhile. The same writers as the dry run, of their own dry runs (D6).
func (s *Server) ExecuteImport(ctx context.Context, req apigen.ExecuteImportRequestObject) (apigen.ExecuteImportResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, work); perr != nil {
		return nil, perr
	}
	corrections := correctionsOf(req.Body)
	release, err := slot(ctx, s.imports)
	if err != nil {
		return nil, err
	}
	defer release()
	now := s.h.opts.Now()
	var job readq.GetImportJobRow
	_, err = s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		p, err := importProject(ctx, w.Reader, t, req.Project)
		if err != nil {
			return err
		}
		upload, named, err := lockedDryRun(ctx, w, t, p, req.Import, now)
		if err != nil {
			return err
		}
		if perr := checkCorrections(upload, corrections); perr != nil {
			return perr
		}
		if err := lockRank(ctx, w, t, p.ID); err != nil {
			return err
		}
		target, err := s.importTarget(ctx, w.Reader, t, p, upload, corrections, named)
		if err != nil {
			return err
		}
		result := importer.Analyze(upload, target, corrections)
		if err := s.execute(ctx, w, t, p, req.Import, now, target, &result); err != nil {
			return err
		}
		job, err = w.GetImportJob(ctx, readq.GetImportJobParams{TenantID: t.ID, ProjectID: p.ID, ID: req.Import, Now: &now})
		return err
	})
	if err != nil {
		return nil, err
	}
	view, err := importJobView(req.Project, job)
	if err != nil {
		return nil, err
	}
	return apigen.ExecuteImport200JSONResponse(view), nil
}

// lockedDryRun locks the job and reads its files back, with the members its
// report assigned by the files' paths: a second execution waits for the
// first and finds it executed (docs/adr/0051 D3), and a dry run past its day
// is gone (D7).
func lockedDryRun(ctx context.Context, w *store.Writer, t tenantScope, p project, id uuid.UUID, now time.Time) (*importer.Upload, map[string]uuid.UUID, error) {
	job, err := w.LockImportJob(ctx, writeq.LockImportJobParams{TenantID: t.ID, ProjectID: p.ID, ID: id})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, nil, problem.New(problem.NotFound, "no such import job")
	case err != nil:
		return nil, nil, err
	case job.Status == importExecuted:
		return nil, nil, problem.New(problem.ImportExecuted, "the dry run was executed already; a dry run is executed at most once")
	case job.ExpiresAt == nil || !job.ExpiresAt.After(now):
		return nil, nil, problem.New(problem.NotFound, "no such import job")
	}
	sources, err := importer.Unpack(job.Source)
	if err != nil {
		return nil, nil, err
	}
	var report importer.Report
	if err := json.Unmarshal(job.Report, &report); err != nil {
		return nil, nil, fmt.Errorf("decode the dry run's report: %w", err)
	}
	named := map[string]uuid.UUID{}
	for _, f := range report.Files {
		if f.Assignee != nil && f.Assignee.Person != nil {
			named[f.Path] = f.Assignee.Person.ID
		}
	}
	return importer.Read(sources), named, nil
}

// correctionsOf reads the request's corrections.
func correctionsOf(body *apigen.ExecuteImportJSONRequestBody) []importer.Correction {
	if body == nil || body.Corrections == nil {
		return nil
	}
	out := make([]importer.Correction, 0, len(*body.Corrections))
	for _, c := range *body.Corrections {
		ic := importer.Correction{Path: c.Path, Exclude: c.Exclude != nil && *c.Exclude}
		if c.Type != nil {
			ic.Type = domain.TicketType(*c.Type)
		}
		if c.State != nil {
			ic.State = domain.TicketState(*c.State)
		}
		if b := c.Block; b != nil {
			ic.Block = &importer.BlockCorrection{Kind: domain.BlockKind(b.Kind), Reason: b.Reason}
			if b.From != nil {
				ic.Block.From = domain.TicketState(*b.From)
			}
		}
		if c.Assignee.IsSpecified() {
			ic.AssigneeSet = true
			if !c.Assignee.IsNull() {
				id := c.Assignee.MustGet()
				ic.Assignee = &id
			}
		}
		out = append(out, ic)
	}
	return out
}

// checkCorrections refuses corrections that break the rules, each at its
// pointer.
func checkCorrections(u *importer.Upload, corrections []importer.Correction) *problem.Error {
	errs := u.Check(corrections)
	if len(errs) == 0 {
		return nil
	}
	fields := make([]problem.FieldError, 0, len(errs))
	for _, e := range errs {
		fields = append(fields, problem.FieldError{Pointer: e.Pointer, Message: e.Message})
	}
	return &problem.Error{Code: problem.ValidationFailed, Detail: "a correction cannot be applied", Errors: fields}
}
