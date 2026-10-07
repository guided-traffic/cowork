package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/importer"
	"github.com/guided-traffic/cowork/backend/internal/problem"
	"github.com/guided-traffic/cowork/backend/internal/store"
	"github.com/guided-traffic/cowork/backend/internal/store/readq"
	"github.com/guided-traffic/cowork/backend/internal/store/writeq"
)

// execution writes an import's plan in the execution's transaction
// (docs/adr/0051 D3): every created row an act of the importer naming the job
// (docs/adr/0026 D1). The acts on the tickets it creates are quiet — the job's
// one act tells the streams, as project.changed — and tell nobody's inbox.
type execution struct {
	w      *store.Writer
	t      tenantScope
	p      project
	job    uuid.UUID
	caller auth.Principal
	// ids are the created tickets by number; existing the project's tickets
	// the plan names, numbers by id; files the created tickets' reports, which
	// a link the database refuses adds a warning to.
	ids      map[int32]uuid.UUID
	existing map[uuid.UUID]int32
	files    map[int32]*importer.FileReport
	parents  map[uuid.UUID]bool
	locked   bool
}

// execute writes the plan, the sequence advanced past its highest number,
// and the job's end with the report of what it created.
func (s *Server) execute(ctx context.Context, w *store.Writer, t tenantScope, p project, job uuid.UUID, now time.Time,
	tg importer.Target, result *importer.Result) error {
	ex := &execution{w: w, t: t, p: p, job: job, caller: principal(ctx), ids: map[int32]uuid.UUID{},
		existing: map[uuid.UUID]int32{}, files: map[int32]*importer.FileReport{}, parents: map[uuid.UUID]bool{}}
	for n, id := range tg.Existing {
		ex.existing[id] = n
	}
	for _, f := range result.Report.Files {
		if f.Number != nil && f.Outcome == importer.OutcomeCreate {
			ex.files[*f.Number] = f
		}
	}
	last, err := rankUnranked(ctx, w, t, p.ID)
	if err != nil {
		return err
	}
	for _, pt := range result.Plan.Tickets {
		if last, err = ex.ticket(ctx, pt, last); err != nil {
			return fmt.Errorf("import %s: %w", pt.Path, err)
		}
	}
	for id := range ex.parents {
		if err := refreshProgress(ctx, w, t, &id); err != nil {
			return err
		}
	}
	for _, l := range result.Plan.Links {
		if err := ex.link(ctx, l); err != nil {
			return err
		}
	}
	if result.Plan.Highest > 0 {
		if err := w.AdvanceTicketCounter(ctx, writeq.AdvanceTicketCounterParams{TenantID: t.ID, ProjectID: p.ID, Number: result.Plan.Highest}); err != nil {
			return fmt.Errorf("advance the sequence: %w", err)
		}
	}
	return ex.finish(ctx, now, result)
}

// finish ends the job: executed, the report of what it created, the files
// gone; one act, published as the project's project.changed.
func (ex *execution) finish(ctx context.Context, now time.Time, result *importer.Result) error {
	result.Report.Executed()
	report, err := json.Marshal(result.Report)
	if err != nil {
		return fmt.Errorf("encode the report: %w", err)
	}
	n, err := ex.w.FinishImportJob(ctx, writeq.FinishImportJobParams{TenantID: ex.t.ID, ID: ex.job,
		ExecutedBy: &ex.caller.PersonID, ExecutedAt: &now, Report: report})
	if err != nil {
		return fmt.Errorf("finish the import job: %w", err)
	}
	if n == 0 {
		return problem.New(problem.ImportExecuted, "the dry run was executed already; a dry run is executed at most once")
	}
	ex.w.Record(store.Event{EntityType: entityImportJob, EntityID: ex.job, Action: actionImported,
		After:       summaryAct(ex.p.Key, importExecuted, result.Report.Summary),
		ProjectRank: &store.ProjectChange{ID: ex.p.ID, Key: ex.t.Slug + "/" + ex.p.Key}})
	return nil
}

// ref is the id of a planned ticket: one this execution created, or the
// project's.
func (ex *execution) ref(r importer.Ref) uuid.UUID {
	if r.Number != 0 {
		return ex.ids[r.Number]
	}
	return r.ID
}

func (ex *execution) key(n int32) string { return domain.FullKey(ex.t.Slug, ex.p.Key, n) }

// ticket creates one ticket with its acts and its questions; an open one takes
// the next place at the bottom of the project's rank, in the plan's order
// (docs/adr/0014 D2). It returns the bottom key after it.
func (ex *execution) ticket(ctx context.Context, pt *importer.PlannedTicket, last string) (string, error) {
	params := ex.ticketParams(pt)
	if !pt.State.Terminal() {
		key, err := domain.RankBetween(last, "")
		if err != nil {
			return last, fmt.Errorf("rank an imported ticket: %w", err)
		}
		params.Rank, last = &key, key
	}
	id, err := ex.w.InsertImportedTicket(ctx, params)
	if err != nil {
		return last, fmt.Errorf("insert the ticket: %w", err)
	}
	ex.ids[pt.Number] = id
	if params.ParentID != nil {
		ex.parents[*params.ParentID] = true
	}
	if err := refreshScore(ctx, ex.w, ex.t, id); err != nil {
		return last, err
	}
	ex.ticketActs(id, pt)
	return last, ex.questions(ctx, id, pt)
}

// ticketParams are the columns of an imported ticket: the source's, the
// importer its reporter, the horizon other than later set by the importer
// (docs/adr/0010 D3), done from in-progress — the one way a ticket closed
// before the stages had (docs/adr/0009 D5) — and the file and job it came
// from (docs/adr/0051 D3).
func (ex *execution) ticketParams(pt *importer.PlannedTicket) writeq.InsertImportedTicketParams {
	path := pt.Path
	params := writeq.InsertImportedTicketParams{TenantID: ex.t.ID, ProjectID: ex.p.ID, Number: pt.Number, Type: pt.Type,
		Title: pt.Title, Body: pt.Body, State: pt.State, Severity: pt.Severity, Security: pt.Security,
		UrgencyDerived: domain.UrgencyDefault, UrgencyRule: domain.UrgencyRuleDefault, Effort: pt.Effort,
		ProgressRefinement: int16(pt.Stages[0]), Progress: int16(pt.Stages[1]), ProgressReview: int16(pt.Stages[2]),
		ReporterID: ex.caller.PersonID, AssigneeID: pt.Assignee, Confidential: pt.Confidential,
		OpenedAt: pt.Opened, DecidedAt: pt.Decided, DoneAt: pt.Done, DoneByHand: pt.DoneByHand,
		ImportedFromFile: &path, ImportedFromJob: &ex.job}
	params.ReporterTokenID, params.ReporterTokenName = actToken(ex.caller)
	if pt.Threat != "" && pt.Security != domain.SecurityNone {
		params.Threat = &pt.Threat
	}
	if pt.Horizon != domain.UrgencyDefault {
		h := pt.Horizon
		params.UrgencyOverride, params.UrgencyOverrideBy = &h, &ex.caller.PersonID
	}
	if pt.Parent != nil {
		id := ex.ref(*pt.Parent)
		params.ParentID = &id
	}
	if b := pt.Block; b != nil {
		kind, from, reason := b.Kind, b.From, b.Reason
		params.BlockKind, params.BlockedFrom, params.BlockReason = &kind, &from, &reason
		if b.WaitsOn != nil {
			id := ex.ref(*b.WaitsOn)
			params.BlockTicketID = &id
		}
	}
	if pt.State == domain.StateDone {
		from := domain.StateInProgress
		params.DoneFrom = &from
	}
	return params
}

// ticketActs records the ticket's creation, its flag where the source set it
// (docs/adr/0065 D7), and the act that holds a done ticket's note or a
// dropped one's reason, which the export reads back (docs/adr/0009 D5,
// docs/adr/0063 D2).
func (ex *execution) ticketActs(id uuid.UUID, pt *importer.PlannedTicket) {
	key := ex.key(pt.Number)
	act := func(e store.Event) {
		e.EntityType, e.EntityID, e.TicketID, e.TicketKey, e.Quiet = entityTicket, id, id, key, true
		ex.w.Record(e)
	}
	act(store.Event{Action: actionCreated, After: map[string]any{fieldType: pt.Type, "title": pt.Title, "severity": pt.Severity,
		"security": pt.Security, "effort": pt.Effort, "urgency": pt.Horizon, fieldState: pt.State,
		fieldImportJob: ex.job, fieldFileName: pt.Path}})
	if pt.Confidential {
		act(store.Event{Action: "confidential_set", Reason: pt.ConfidentialReason})
	}
	switch pt.State {
	case domain.StateDone:
		act(store.Event{Action: actionTransitioned, After: map[string]any{fieldState: pt.State, fieldDoneByHand: pt.DoneByHand,
			fieldImportJob: ex.job}, Note: pt.Note})
	case domain.StateDropped:
		act(store.Event{Action: actionTransitioned, After: map[string]any{fieldState: pt.State, fieldImportJob: ex.job}, Reason: pt.Note})
	}
}

// questions creates the ticket's open questions with their numbers, each
// asked by the importer (docs/adr/0011 D2, D4).
func (ex *execution) questions(ctx context.Context, ticket uuid.UUID, pt *importer.PlannedTicket) error {
	for _, q := range pt.Questions {
		params := writeq.InsertImportedQuestionParams{TenantID: ex.t.ID, TicketID: ticket, Number: q.Number, Question: q.Question,
			Options: q.Options, Recommendation: q.Recommendation, Status: q.Status, AskedBy: ex.caller.PersonID}
		if q.Status == "answered" {
			params.Answer = &q.Answer
		}
		id, err := ex.w.InsertImportedQuestion(ctx, params)
		if err != nil {
			return fmt.Errorf("insert Q%d: %w", q.Number, err)
		}
		ex.w.Record(store.Event{EntityType: entityQuestion, EntityID: id, TicketID: ticket, TicketKey: ex.key(pt.Number),
			Action: "asked", After: map[string]any{"number": q.Number, "question": q.Question, "status": q.Status,
				fieldImportJob: ex.job}, Quiet: true})
	}
	return nil
}

// link creates one link, with its act on both tickets; a blocks link the
// tenant's graph would close a cycle with — through the project's tickets,
// which the analysis does not walk — is omitted, and the report says so
// (docs/adr/0012 D4).
func (ex *execution) link(ctx context.Context, l importer.PlannedLink) error {
	source, target := ex.ref(l.Source), ex.ref(l.Target)
	if l.Type == domain.LinkBlocks {
		if !ex.locked {
			if err := ex.w.LockBlocks(ctx); err != nil {
				return err
			}
			ex.locked = true
		}
		cycle, err := ex.w.BlocksPathExists(ctx, readq.BlocksPathExistsParams{TenantID: ex.t.ID, FromID: target, ToID: source})
		if err != nil {
			return fmt.Errorf("walk the blocks graph: %w", err)
		}
		if cycle {
			ex.omitted(l)
			return nil
		}
	}
	if l.Type == domain.LinkRelatesTo && bytes.Compare(source[:], target[:]) > 0 {
		source, target = target, source
		l.Source, l.Target = l.Target, l.Source
	}
	ins, err := ex.w.InsertLink(ctx, writeq.InsertLinkParams{TenantID: ex.t.ID, Type: l.Type, SourceID: source, TargetID: target,
		CreatedBy: ex.caller.PersonID})
	if err != nil {
		return fmt.Errorf("insert the link: %w", err)
	}
	return ex.linkActs(l, ins.ID, source, target)
}

// linkActs records the link on both its tickets (docs/adr/0012 D3): quiet on
// a ticket the import creates, published on the project's.
func (ex *execution) linkActs(l importer.PlannedLink, id, source, target uuid.UUID) error {
	sourceKey, targetKey := ex.refKey(l.Source), ex.refKey(l.Target)
	if sourceKey == "" || targetKey == "" {
		return errNoTicket
	}
	payload := map[string]any{fieldType: string(l.Type), "source": sourceKey, "target": targetKey, fieldImportJob: ex.job}
	for _, end := range []struct {
		ticket, other uuid.UUID
		key           string
		created       bool
	}{{source, target, sourceKey, l.Source.Number != 0}, {target, source, targetKey, l.Target.Number != 0}} {
		ex.w.Record(store.Event{EntityType: entityLink, EntityID: id, TicketID: end.ticket, TicketKey: end.key,
			Action: actionLinked, After: payload, Refs: []uuid.UUID{end.other}, Quiet: end.created})
	}
	return nil
}

// refKey is the key of a planned ticket: one the execution creates, or the
// project's by its number; "" for neither.
func (ex *execution) refKey(r importer.Ref) string {
	if r.Number != 0 {
		return ex.key(r.Number)
	}
	if n, ok := ex.existing[r.ID]; ok {
		return ex.key(n)
	}
	return ""
}

// omitted reports a link the database's walk refused at the files the import
// creates.
func (ex *execution) omitted(l importer.PlannedLink) {
	for _, end := range []importer.Ref{l.Source, l.Target} {
		if f, ok := ex.files[end.Number]; ok && end.Number != 0 {
			field := "links"
			f.Warnings = append(f.Warnings, importer.MessageReport{Field: &field,
				Message: "a blocks link the execution found would close a cycle through the project's tickets is omitted (docs/adr/0012 D4)"})
		}
	}
}

// errNoTicket is a link's end the execution cannot name.
var errNoTicket = errors.New("a planned link names no ticket")
