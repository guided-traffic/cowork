package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/problem"
	"github.com/guided-traffic/cowork/backend/internal/store"
	"github.com/guided-traffic/cowork/backend/internal/store/readq"
	"github.com/guided-traffic/cowork/backend/internal/store/writeq"
)

const entityTicket = "ticket"

// work is what a member does with a ticket: the member role in its project
// and write scope; in the agent baseline (docs/adr/0043 D2).
var work = auth.Need{Role: domain.RoleMember, Scope: domain.ScopeWrite}

func ticketURL(t tenantScope, project string, number int32) string {
	return projectURL(t, project) + "/tickets/" + strconv.Itoa(int(number))
}

func ticketView(t tenantScope, r store.TicketRow) apigen.Ticket {
	v := apigen.Ticket{
		Id: r.ID, Key: domain.FullKey(t.Slug, r.ProjectKey, r.Number), Project: r.ProjectKey, Number: int(r.Number),
		Type: apigen.TicketType(r.Type), Title: r.Title, Body: r.Body, State: apigen.TicketState(r.State),
		Severity: apigen.Severity(r.Severity), Security: apigen.SecurityClass(r.Security), Threat: nullableOf(r.Threat),
		Urgency: apigen.Urgency(r.UrgencyDerived), UrgencyDerived: apigen.Urgency(r.UrgencyDerived), UrgencyRule: r.UrgencyRule,
		Effort: apigen.Effort(r.Effort), Progress: effectiveProgress(r), ProgressDerived: r.ProgressDerived != nil, Confidential: r.Confidential,
		OpenedAt: r.OpenedAt, DecidedAt: nullableOf(r.DecidedAt), DoneAt: nullableOf(r.DoneAt),
		Version: int(r.Version), CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
		Reporter: personView(r.ReporterID, r.ReporterUsername, r.ReporterName),
		Block:    nullableOf[apigen.Block](nil), UrgencyOverride: nullableOf[apigen.UrgencyOverride](nil),
		Assignee: nullableOf[apigen.Person](nil), Parent: nullableOf[string](nil),
	}
	if r.State == domain.StateBlocked && r.BlockedFrom != nil && r.BlockKind != nil {
		b := apigen.Block{From: apigen.TicketState(*r.BlockedFrom), Kind: apigen.BlockKind(*r.BlockKind),
			ExternalRef: nullableOf(r.BlockExternalRef), Ticket: nullableOf[string](nil)}
		if r.BlockReason != nil {
			b.Reason = *r.BlockReason
		}
		if r.BlockProjectKey != nil && r.BlockNumber != nil {
			key := domain.FullKey(t.Slug, *r.BlockProjectKey, *r.BlockNumber)
			b.Ticket = nullableOf(&key)
		}
		v.Block = nullableOf(&b)
	}
	if r.UrgencyOverride != nil && r.UrgencyOverrideAt != nil {
		o := apigen.UrgencyOverride{Value: apigen.Urgency(*r.UrgencyOverride), At: *r.UrgencyOverrideAt, By: nullableOf[apigen.Person](nil)}
		if r.UrgencyOverrideReason != nil {
			o.Reason = *r.UrgencyOverrideReason
		}
		if r.UrgencyOverrideBy != nil {
			p := apigen.Person{Id: *r.UrgencyOverrideBy, Username: nullableOf[string](nil)}
			o.By = nullableOf(&p)
		}
		v.UrgencyOverride = nullableOf(&o)
		v.Urgency = apigen.Urgency(*r.UrgencyOverride)
	}
	if r.AssigneeID != nil {
		a := personView(*r.AssigneeID, r.AssigneeUsername, r.AssigneeName)
		v.Assignee = nullableOf(&a)
	}
	if r.ParentNumber != nil {
		key := domain.FullKey(t.Slug, r.ProjectKey, *r.ParentNumber)
		v.Parent = nullableOf(&key)
	}
	return v
}

// effectiveProgress is the progress a ticket shows: 100 when done, the
// derived value while it has children, else its own (docs/adr/0017 D3, D5).
func effectiveProgress(r store.TicketRow) int {
	switch {
	case r.State == domain.StateDone:
		return 100
	case r.ProgressDerived != nil:
		return int(*r.ProgressDerived)
	}
	return int(r.Progress)
}

// refreshProgress derives the progress of a parent again after a change of
// its children, and of its ancestors as far as the value changes; their
// versions stay (docs/adr/0017 D3, docs/adr/0050 D1).
func refreshProgress(ctx context.Context, w *store.Writer, t tenantScope, parents ...*uuid.UUID) error {
	for _, id := range parents {
		for id != nil {
			next, err := w.RefreshDerivedProgress(ctx, writeq.RefreshDerivedProgressParams{TenantID: t.ID, ID: *id})
			if errors.Is(err, pgx.ErrNoRows) {
				break
			}
			if err != nil {
				return fmt.Errorf("derive the progress: %w", err)
			}
			id = next
		}
	}
	return nil
}

// personView names a person; one the caller cannot see any more (no longer a
// member of the tenant) keeps its id only.
func personView(id uuid.UUID, username, name *string) apigen.Person {
	p := apigen.Person{Id: id, Username: nullableOf(username)}
	if name != nil {
		p.DisplayName = *name
	}
	return p
}

// ticketCtx is a visible ticket with its project and the person's role there.
type ticketCtx struct {
	project project
	role    domain.Role
	row     store.TicketRow
}

// visibleTicket reads a ticket by project key and number through the
// visibility predicate; one the caller cannot see is the same 404 as one that
// does not exist (docs/adr/0065 D5).
func visibleTicket(ctx context.Context, r *store.Reader, t tenantScope, projectKey string, number int) (ticketCtx, error) {
	p, err := visibleProject(ctx, r, t, projectKey)
	if err != nil {
		return ticketCtx{}, err
	}
	role, err := projectRole(ctx, r, t, p)
	if err != nil {
		return ticketCtx{}, err
	}
	n, perr := ticketNumber(number)
	if perr != nil {
		return ticketCtx{}, perr
	}
	row, err := r.GetTicketByNumber(ctx, readq.GetTicketByNumberParams{TenantID: t.ID, ProjectID: p.ID, Number: n})
	if errors.Is(err, pgx.ErrNoRows) {
		return ticketCtx{}, problem.New(problem.NotFound, "no such ticket")
	}
	if err != nil {
		return ticketCtx{}, err
	}
	return ticketCtx{project: p, role: role, row: row}, nil
}

// ticketNumber is a number within the column's range; any other names no
// ticket.
func ticketNumber(n int) (int32, *problem.Error) {
	if n < 1 || n > math.MaxInt32 {
		return 0, problem.New(problem.NotFound, "no such ticket")
	}
	return int32(n), nil
}

// reread returns the ticket as the write in this transaction left it.
func reread(ctx context.Context, w *store.Writer, t tenantScope, id uuid.UUID) (store.TicketRow, error) {
	row, err := w.GetWrittenTicket(ctx, writeq.GetWrittenTicketParams{TenantID: t.ID, ID: id})
	return store.TicketRow(row), err
}

// GetTicket answers one ticket.
func (s *Server) GetTicket(ctx context.Context, req apigen.GetTicketRequestObject) (apigen.GetTicketResponseObject, error) {
	t := tenantFrom(ctx)
	row, err := s.readTicket(ctx, t, req.Project, req.Number)
	if err != nil {
		return nil, err
	}
	return apigen.GetTicket200JSONResponse{Body: ticketView(t, row), Headers: apigen.GetTicket200ResponseHeaders{ETag: etag(row.Version)}}, nil
}

// ResolveTicket answers a ticket by its key under the tenant it names
// (docs/adr/0023 D3): the same body and ETag as GetTicket.
func (s *Server) ResolveTicket(ctx context.Context, req apigen.ResolveTicketRequestObject) (apigen.ResolveTicketResponseObject, error) {
	t := tenantFrom(ctx)
	key, err := domain.ParseTicketKey(req.Key)
	if err != nil || key.Tenant != "" {
		return nil, problem.New(problem.NotFound, "no such ticket")
	}
	row, err := s.readTicket(ctx, t, key.Project, int(key.Number))
	if err != nil {
		return nil, err
	}
	return apigen.ResolveTicket200JSONResponse{Body: ticketView(t, row), Headers: apigen.ResolveTicket200ResponseHeaders{ETag: etag(row.Version)}}, nil
}

func (s *Server) readTicket(ctx context.Context, t tenantScope, projectKey string, number int) (store.TicketRow, error) {
	if perr := auth.Authorize(principal(ctx), t.Role, read); perr != nil {
		return store.TicketRow{}, perr
	}
	var row store.TicketRow
	err := s.db.InTenant(ctx, t.ID, func(r *store.Reader) error {
		tc, err := visibleTicket(ctx, r, t, projectKey, number)
		row = tc.row
		return err
	})
	return row, err
}

// CreateTicket files a ticket (docs/adr/0007, 0010, 0065 D2).
func (s *Server) CreateTicket(ctx context.Context, req apigen.CreateTicketRequestObject) (apigen.CreateTicketResponseObject, error) {
	t := tenantFrom(ctx)
	body := *req.Body
	ctx, perr := keyed(ctx, req.Params.IdempotencyKey, "createTicket", t.ID.String()+"/"+req.Project, body)
	if perr != nil {
		return nil, perr
	}
	if perr := checkThreat(domain.SecurityClass(body.Security), body.Threat); perr != nil {
		return nil, perr
	}
	var created store.TicketRow
	replay, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		p, err := fileableProject(ctx, w.Reader, t, req.Project)
		if err != nil {
			return err
		}
		ins, err := s.newTicket(ctx, w, t, p, body)
		if err != nil {
			return err
		}
		id, err := w.InsertTicket(ctx, ins)
		if err != nil {
			return err
		}
		if err := refreshProgress(ctx, w, t, ins.ParentID); err != nil {
			return err
		}
		key := domain.FullKey(t.Slug, p.Key, ins.Number)
		w.Record(store.Event{EntityType: entityTicket, EntityID: id, TicketID: id, TicketKey: key, Action: actionCreated,
			After: map[string]any{fieldType: body.Type, "title": body.Title, "severity": body.Severity,
				"security": body.Security, "effort": body.Effort, "urgency": ins.UrgencyDerived}})
		if ins.Confidential {
			w.Record(store.Event{EntityType: entityTicket, EntityID: id, TicketID: id, TicketKey: key,
				Action: "confidential_set", Reason: "the security class is " + string(body.Security)})
		}
		if created, err = reread(ctx, w, t, id); err != nil {
			return err
		}
		res, err := stored(ticketView(t, created), map[string]string{
			headerETag: *etag(created.Version), headerLocation: ticketURL(t, p.Key, created.Number)})
		if err != nil {
			return err
		}
		w.Respond(res)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if replay != nil {
		body, err := replayed[apigen.Ticket](replay)
		if err != nil {
			return nil, err
		}
		return apigen.CreateTicket201JSONResponse{Body: body, Headers: apigen.CreateTicket201ResponseHeaders{
			ETag: header(replay, headerETag), Location: header(replay, headerLocation)}}, nil
	}
	location := ticketURL(t, created.ProjectKey, created.Number)
	return apigen.CreateTicket201JSONResponse{Body: ticketView(t, created), Headers: apigen.CreateTicket201ResponseHeaders{
		ETag: etag(created.Version), Location: &location}}, nil
}

// fileableProject reads the project a ticket is filed in: visible, open,
// and the caller a member there with write scope (docs/adr/0043 D2).
func fileableProject(ctx context.Context, r *store.Reader, t tenantScope, key string) (project, error) {
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
	if p.ArchivedAt != nil {
		return p, problem.New(problem.ProjectArchived, "the project is archived")
	}
	return p, nil
}

// newTicket validates a new ticket's references and derives what is derived:
// the number, the urgency (docs/adr/0010 D3) and the confidential flag
// (docs/adr/0065 D2).
func (s *Server) newTicket(ctx context.Context, w *store.Writer, t tenantScope, p project, body apigen.TicketCreate) (writeq.InsertTicketParams, error) {
	ins := writeq.InsertTicketParams{
		TenantID: t.ID, ProjectID: p.ID, Type: domain.TicketType(body.Type), Title: body.Title,
		Severity: domain.Severity(body.Severity), Security: domain.SecurityClass(body.Security), Threat: body.Threat,
		Effort: domain.Effort(body.Effort), ReporterID: principal(ctx).PersonID,
		Confidential: domain.SecurityClass(body.Security).MakesConfidential(),
	}
	if body.Body != nil {
		ins.Body = *body.Body
	}
	ins.UrgencyDerived, ins.UrgencyRule = domain.DeriveUrgency(domain.UrgencyInputs{State: domain.StateFiled})
	if body.Parent != nil {
		parent, perr := resolveParent(ctx, w.Reader, t, p, *body.Parent)
		if perr != nil {
			return ins, perr
		}
		ins.ParentID = &parent
	}
	if body.Assignee != nil {
		if err := checkAssignee(ctx, w.Reader, t, p.ID, *body.Assignee); err != nil {
			return ins, err
		}
		ins.AssigneeID = body.Assignee
	}
	number, err := w.NextTicketNumber(ctx, writeq.NextTicketNumberParams{TenantID: t.ID, ProjectID: p.ID})
	if err != nil {
		return ins, fmt.Errorf("next ticket number: %w", err)
	}
	ins.Number = number
	return ins, nil
}

// checkThreat holds threat to the security class (docs/adr/0010 D2).
func checkThreat(security domain.SecurityClass, threat *string) *problem.Error {
	has := threat != nil && strings.TrimSpace(*threat) != ""
	switch {
	case security == domain.SecurityNone && threat != nil:
		return problem.Field("/threat", "a ticket without a security class carries no threat")
	case security != domain.SecurityNone && !has:
		return problem.Field("/threat", "a security class needs the threat it names")
	}
	return nil
}

// resolveParent reads a parent key given inside the project: a ticket of the
// same project the caller can see (docs/adr/0008 D2).
func resolveParent(ctx context.Context, r *store.Reader, t tenantScope, p project, key string) (uuid.UUID, *problem.Error) {
	k, err := domain.ParseTicketKey(key)
	if err == nil {
		k, err = k.InTenant(t.Slug)
	}
	if err != nil {
		return uuid.Nil, problem.Field("/parent", err.Error())
	}
	if k.Project != p.Key {
		return uuid.Nil, problem.Field("/parent", "a parent is a ticket of the same project")
	}
	row, err := r.GetTicketByNumber(ctx, readq.GetTicketByNumberParams{TenantID: t.ID, ProjectID: p.ID, Number: k.Number})
	if err != nil {
		return uuid.Nil, problem.Field("/parent", "no such ticket")
	}
	return row.ID, nil
}

// checkAssignee admits as assignee only a person who can see the project.
func checkAssignee(ctx context.Context, r *store.Reader, t tenantScope, projectID, person uuid.UUID) error {
	visible, err := r.CanSeeProject(ctx, readq.CanSeeProjectParams{TenantID: t.ID, ProjectID: projectID, UserID: person})
	if err != nil {
		return fmt.Errorf("check the assignee: %w", err)
	}
	if !visible {
		return problem.Field("/assignee", "not a member who can see the project")
	}
	return nil
}

// UpdateTicket changes a ticket's fields with If-Match. A live or boundary
// security class sets the confidential flag, which nothing lifts but an
// administrator (docs/adr/0065 D2, D3).
func (s *Server) UpdateTicket(ctx context.Context, req apigen.UpdateTicketRequestObject) (apigen.UpdateTicketResponseObject, error) {
	t := tenantFrom(ctx)
	version, perr := ifMatch(req.Params.IfMatch)
	if perr != nil {
		return nil, perr
	}
	var out store.TicketRow
	_, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		tc, err := visibleTicket(ctx, w.Reader, t, req.Project, req.Number)
		if err != nil {
			return err
		}
		if perr := auth.Authorize(principal(ctx), tc.role, work); perr != nil {
			return perr
		}
		ch, err := applyTicketPatch(ctx, w, t, tc, *req.Body)
		if err != nil {
			return err
		}
		if tc.row.Version != version {
			return stale(tc.row.Version, pick(ch.before, ch.sent))
		}
		if out, err = writeTicketChange(ctx, w, t, tc, ch, req.Body.Comment); errors.Is(err, store.ErrNoChange) {
			out = tc.row
		}
		return err
	})
	if err != nil && !errors.Is(err, store.ErrNoChange) {
		return nil, err
	}
	return apigen.UpdateTicket200JSONResponse{Body: ticketView(t, out), Headers: apigen.UpdateTicket200ResponseHeaders{ETag: etag(out.Version)}}, nil
}

// writeTicketChange writes a patch with its acts, its explaining comment and
// its effects; a patch that changes nothing is ErrNoChange.
func writeTicketChange(ctx context.Context, w *store.Writer, t tenantScope, tc ticketCtx, ch ticketChange, comment *string) (store.TicketRow, error) {
	changedBefore, changedAfter := diffDeep(ch.before, ch.after)
	if len(changedAfter) == 0 {
		return store.TicketRow{}, store.ErrNoChange
	}
	// An open ticket turning into a decision, or out of one, is an input of
	// the tickets it blocks (docs/adr/0010 D3).
	var deps []dependent
	if (tc.row.Type == domain.TypeDecision) != (ch.params.Type == domain.TypeDecision) && !tc.row.State.Terminal() {
		var err error
		if deps, err = dependentsOf(ctx, w, t, tc.row.ID); err != nil {
			return store.TicketRow{}, err
		}
	}
	if _, err := w.UpdateTicketFields(ctx, ch.params); errors.Is(err, pgx.ErrNoRows) {
		return store.TicketRow{}, stale(tc.row.Version, pick(ch.before, ch.sent))
	} else if err != nil {
		return store.TicketRow{}, err
	}
	explainedBy, err := explain(ctx, w, t, tc, comment)
	if err != nil {
		return store.TicketRow{}, err
	}
	recordTicketChange(w, t, tc, changedBefore, changedAfter, ch, explainedBy)
	if err := rederiveAll(ctx, w, t, deps); err != nil {
		return store.TicketRow{}, err
	}
	if changes(changedAfter, fieldParent, "effort", "progress") {
		if err := refreshProgress(ctx, w, t, tc.row.ParentID, ch.params.ParentID); err != nil {
			return store.TicketRow{}, err
		}
	}
	return reread(ctx, w, t, tc.row.ID)
}

// changes reports whether any of the fields changed.
func changes(changed map[string]any, fields ...string) bool {
	for _, f := range fields {
		if _, ok := changed[f]; ok {
			return true
		}
	}
	return false
}

// ticketChange is a patch applied to a ticket: the update and the values of
// the fields before and after, keyed as the request names them.
type ticketChange struct {
	params             writeq.UpdateTicketFieldsParams
	before, after      map[string]any
	sent               []string
	becameConfidential bool
}

func applyTicketPatch(ctx context.Context, w *store.Writer, t tenantScope, tc ticketCtx, p apigen.TicketPatch) (ticketChange, error) {
	row := tc.row
	ch := ticketChange{params: writeq.UpdateTicketFieldsParams{
		TenantID: t.ID, ID: row.ID, Version: row.Version, Type: row.Type, Title: row.Title, Severity: row.Severity,
		Security: row.Security, Threat: row.Threat, Effort: row.Effort, ParentID: row.ParentID,
		AssigneeID: row.AssigneeID, Progress: row.Progress, Confidential: row.Confidential,
	}}
	ch.before = ticketFields(ch.params)
	applyScalars(p, &ch)
	if perr := checkThreat(ch.params.Security, ch.params.Threat); perr != nil {
		return ch, perr
	}
	// A change to live or boundary sets the flag; a class that merely stays
	// live does not set again what an administrator lifted (docs/adr/0065 D2, D3).
	if ch.params.Security != row.Security && ch.params.Security.MakesConfidential() && !row.Confidential {
		ch.params.Confidential, ch.becameConfidential = true, true
	}
	if err := applyRelations(ctx, w, t, tc, p, &ch); err != nil {
		return ch, err
	}
	if p.Progress != nil {
		if perr := applyProgress(row, *p.Progress, &ch); perr != nil {
			return ch, perr
		}
	}
	ch.after = ticketFields(ch.params)
	return ch, nil
}

// applyScalars applies the plain fields of a patch.
func applyScalars(p apigen.TicketPatch, ch *ticketChange) {
	if p.Type != nil {
		ch.params.Type, ch.sent = domain.TicketType(*p.Type), append(ch.sent, fieldType)
	}
	if p.Title != nil {
		ch.params.Title, ch.sent = *p.Title, append(ch.sent, "title")
	}
	if p.Severity != nil {
		ch.params.Severity, ch.sent = domain.Severity(*p.Severity), append(ch.sent, "severity")
	}
	if p.Effort != nil {
		ch.params.Effort, ch.sent = domain.Effort(*p.Effort), append(ch.sent, "effort")
	}
	if p.Security != nil {
		ch.params.Security, ch.sent = domain.SecurityClass(*p.Security), append(ch.sent, "security")
	}
	if p.Threat.IsSpecified() {
		ch.sent = append(ch.sent, "threat")
		ch.params.Threat = nil
		if !p.Threat.IsNull() {
			v := p.Threat.MustGet()
			ch.params.Threat = &v
		}
	}
}

// applyProgress sets the progress of an open ticket (docs/adr/0017 D2).
func applyProgress(row store.TicketRow, v int, ch *ticketChange) *problem.Error {
	if row.State.Terminal() {
		return &problem.Error{Code: problem.StateConflict, Detail: "a " + string(row.State) + " ticket's progress does not change",
			Errors: []problem.FieldError{{Pointer: "/progress", Message: "the ticket is " + string(row.State), Current: row.Progress}}}
	}
	if row.ProgressDerived != nil {
		return &problem.Error{Code: problem.StateConflict, Detail: "the progress is derived from the ticket's children",
			Errors: []problem.FieldError{{Pointer: "/progress", Message: "derived", Current: int(*row.ProgressDerived)}}}
	}
	if v < 0 || v > 100 || v%5 != 0 {
		return problem.Field("/progress", "0 to 100 in steps of five")
	}
	ch.params.Progress, ch.sent = int16(v), append(ch.sent, "progress")
	return nil
}

// applyRelations applies the parent and the assignee of a patch.
func applyRelations(ctx context.Context, w *store.Writer, t tenantScope, tc ticketCtx, p apigen.TicketPatch, ch *ticketChange) error {
	if p.Parent.IsSpecified() {
		ch.sent = append(ch.sent, fieldParent)
		ch.params.ParentID = nil
		if !p.Parent.IsNull() {
			parent, perr := resolveParent(ctx, w.Reader, t, tc.project, p.Parent.MustGet())
			if perr != nil {
				return perr
			}
			if err := w.LockParents(ctx, tc.project.ID); err != nil {
				return err
			}
			cycle, err := w.ParentChainContains(ctx, readq.ParentChainContainsParams{TenantID: t.ID, CandidateParentID: parent, TicketID: tc.row.ID})
			if err != nil {
				return fmt.Errorf("walk the parent chain: %w", err)
			}
			if cycle {
				return &problem.Error{Code: problem.ParentCycle, Detail: "the parent is the ticket itself or one of its descendants",
					Errors: []problem.FieldError{{Pointer: "/parent", Message: "would make a cycle"}}}
			}
			ch.params.ParentID = &parent
		}
	}
	if p.Assignee.IsSpecified() {
		ch.sent = append(ch.sent, fieldAssignee)
		ch.params.AssigneeID = nil
		if !p.Assignee.IsNull() {
			person := p.Assignee.MustGet()
			if err := checkAssignee(ctx, w.Reader, t, tc.project.ID, person); err != nil {
				return err
			}
			ch.params.AssigneeID = &person
		}
	}
	return nil
}

// ticketFields are a ticket's patchable fields, keyed as the request names
// them; the parent and the assignee by id.
func ticketFields(p writeq.UpdateTicketFieldsParams) map[string]any {
	var threat, parent, assignee any
	if p.Threat != nil {
		threat = *p.Threat
	}
	if p.ParentID != nil {
		parent = p.ParentID.String()
	}
	if p.AssigneeID != nil {
		assignee = p.AssigneeID.String()
	}
	return map[string]any{
		fieldType: string(p.Type), "title": p.Title, "severity": string(p.Severity), "security": string(p.Security),
		"threat": threat, "effort": string(p.Effort), fieldParent: parent, fieldAssignee: assignee, "progress": int(p.Progress),
	}
}

// recordTicketChange records a patch: the assignment as its own act
// (docs/adr/0026 D1), the other fields as updated, and the flag set.
func recordTicketChange(w *store.Writer, t tenantScope, tc ticketCtx, before, after map[string]any, ch ticketChange, explainedBy uuid.UUID) {
	key := domain.FullKey(t.Slug, tc.project.Key, tc.row.Number)
	ev := func(action string, b, a any) store.Event {
		return store.Event{EntityType: entityTicket, EntityID: tc.row.ID, TicketID: tc.row.ID, TicketKey: key, Action: action,
			Before: b, After: a, ExplainedBy: explainedBy}
	}
	if _, ok := after[fieldAssignee]; ok {
		w.Record(ev("assigned", map[string]any{fieldAssignee: before[fieldAssignee]}, map[string]any{fieldAssignee: after[fieldAssignee]}))
		delete(before, fieldAssignee)
		delete(after, fieldAssignee)
	}
	if len(after) > 0 {
		e := ev(actionUpdated, before, after)
		if _, ok := after[fieldParent]; ok {
			for _, p := range []*uuid.UUID{tc.row.ParentID, ch.params.ParentID} {
				if p != nil {
					e.Refs = append(e.Refs, *p)
				}
			}
		}
		w.Record(e)
	}
	if ch.becameConfidential {
		e := ev("confidential_set", nil, nil)
		e.Reason = "the security class is " + string(ch.params.Security)
		w.Record(e)
	}
}

// ReplaceTicketBody replaces the body as a whole with If-Match; the act keeps
// the previous and the new body (docs/adr/0011 D1).
func (s *Server) ReplaceTicketBody(ctx context.Context, req apigen.ReplaceTicketBodyRequestObject) (apigen.ReplaceTicketBodyResponseObject, error) {
	t := tenantFrom(ctx)
	version, perr := ifMatch(req.Params.IfMatch)
	if perr != nil {
		return nil, perr
	}
	var out store.TicketRow
	_, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		tc, err := visibleTicket(ctx, w.Reader, t, req.Project, req.Number)
		if err != nil {
			return err
		}
		if perr := auth.Authorize(principal(ctx), tc.role, work); perr != nil {
			return perr
		}
		if tc.row.Version != version {
			return stale(tc.row.Version, map[string]any{fieldBody: tc.row.Body})
		}
		if tc.row.Body == req.Body.Body {
			out = tc.row
			return store.ErrNoChange
		}
		if _, err := w.UpdateTicketBody(ctx, writeq.UpdateTicketBodyParams{TenantID: t.ID, ID: tc.row.ID, Version: version, Body: req.Body.Body}); errors.Is(err, pgx.ErrNoRows) {
			return stale(tc.row.Version, map[string]any{fieldBody: tc.row.Body})
		} else if err != nil {
			return err
		}
		explainedBy, err := explain(ctx, w, t, tc, req.Body.Comment)
		if err != nil {
			return err
		}
		w.Record(store.Event{EntityType: entityTicket, EntityID: tc.row.ID, TicketID: tc.row.ID,
			TicketKey: domain.FullKey(t.Slug, tc.project.Key, tc.row.Number), Action: actionUpdated,
			Before: map[string]any{fieldBody: tc.row.Body}, After: map[string]any{fieldBody: req.Body.Body}, ExplainedBy: explainedBy})
		out, err = reread(ctx, w, t, tc.row.ID)
		return err
	})
	if err != nil && !errors.Is(err, store.ErrNoChange) {
		return nil, err
	}
	return apigen.ReplaceTicketBody200JSONResponse{Body: ticketView(t, out), Headers: apigen.ReplaceTicketBody200ResponseHeaders{ETag: etag(out.Version)}}, nil
}

// OverrideUrgency sets a reasoned override; an agent needs override-urgency
// (docs/adr/0010 D3, docs/adr/0043 D4).
func (s *Server) OverrideUrgency(ctx context.Context, req apigen.OverrideUrgencyRequestObject) (apigen.OverrideUrgencyResponseObject, error) {
	t := tenantFrom(ctx)
	value := domain.Urgency(req.Body.Value)
	out, err := s.setOverride(ctx, t, req.Project, req.Number, req.Params.IfMatch, &value, &req.Body.Reason)
	if err != nil {
		return nil, err
	}
	return apigen.OverrideUrgency200JSONResponse{Body: ticketView(t, out), Headers: apigen.OverrideUrgency200ResponseHeaders{ETag: etag(out.Version)}}, nil
}

// WithdrawUrgencyOverride drops the override; the derived urgency holds again.
func (s *Server) WithdrawUrgencyOverride(ctx context.Context, req apigen.WithdrawUrgencyOverrideRequestObject) (apigen.WithdrawUrgencyOverrideResponseObject, error) {
	t := tenantFrom(ctx)
	out, err := s.setOverride(ctx, t, req.Project, req.Number, req.Params.IfMatch, nil, nil)
	if err != nil {
		return nil, err
	}
	return apigen.WithdrawUrgencyOverride200JSONResponse{Body: ticketView(t, out), Headers: apigen.WithdrawUrgencyOverride200ResponseHeaders{ETag: etag(out.Version)}}, nil
}

func (s *Server) setOverride(ctx context.Context, t tenantScope, projectKey string, number int, ifm *string, value *domain.Urgency, reason *string) (store.TicketRow, error) {
	version, perr := ifMatch(ifm)
	if perr != nil {
		return store.TicketRow{}, perr
	}
	var out store.TicketRow
	_, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		tc, err := visibleTicket(ctx, w.Reader, t, projectKey, number)
		if err != nil {
			return err
		}
		need := work
		need.Capability = auth.CapOverrideUrgency
		if perr := auth.Authorize(principal(ctx), tc.role, need); perr != nil {
			return perr
		}
		cur := map[string]any{fieldUrgencyOverride: tc.row.UrgencyOverride, "urgency_override_reason": tc.row.UrgencyOverrideReason}
		if tc.row.Version != version {
			return stale(tc.row.Version, cur)
		}
		if value == nil && tc.row.UrgencyOverride == nil {
			out = tc.row
			return store.ErrNoChange
		}
		var by *uuid.UUID
		if value != nil {
			person := principal(ctx).PersonID
			by = &person
		}
		if _, err := w.SetUrgencyOverride(ctx, writeq.SetUrgencyOverrideParams{TenantID: t.ID, ID: tc.row.ID, Version: version,
			UrgencyOverride: value, Reason: reason, OverrideBy: by}); errors.Is(err, pgx.ErrNoRows) {
			return stale(tc.row.Version, cur)
		} else if err != nil {
			return err
		}
		e := store.Event{EntityType: entityTicket, EntityID: tc.row.ID, TicketID: tc.row.ID,
			TicketKey: domain.FullKey(t.Slug, tc.project.Key, tc.row.Number), Action: actionOverridden,
			Before: map[string]any{fieldUrgencyOverride: tc.row.UrgencyOverride}, After: map[string]any{fieldUrgencyOverride: value}}
		if reason != nil {
			e.Reason = *reason
		}
		w.Record(e)
		out, err = reread(ctx, w, t, tc.row.ID)
		return err
	})
	if err != nil && !errors.Is(err, store.ErrNoChange) {
		return store.TicketRow{}, err
	}
	return out, nil
}

// SetConfidential sets or lifts the confidential flag: a tenant
// administrator's act, never an agent's; lifting needs a reason
// (docs/adr/0065 D2, D3, D6).
func (s *Server) SetConfidential(ctx context.Context, req apigen.SetConfidentialRequestObject) (apigen.SetConfidentialResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, auth.Need{Role: domain.RoleAdmin, Scope: domain.ScopeAdmin, HardOff: auth.HardOffConfidential}); perr != nil {
		return nil, perr
	}
	version, perr := ifMatch(req.Params.IfMatch)
	if perr != nil {
		return nil, perr
	}
	if !req.Body.Confidential && (req.Body.Reason == nil || strings.TrimSpace(*req.Body.Reason) == "") {
		return nil, problem.Field("/reason", "lifting the flag needs a reason")
	}
	var out store.TicketRow
	_, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		tc, err := visibleTicket(ctx, w.Reader, t, req.Project, req.Number)
		if err != nil {
			return err
		}
		if tc.row.Version != version {
			return stale(tc.row.Version, map[string]any{"confidential": tc.row.Confidential})
		}
		if tc.row.Confidential == req.Body.Confidential {
			out = tc.row
			return store.ErrNoChange
		}
		if _, err := w.SetConfidential(ctx, writeq.SetConfidentialParams{TenantID: t.ID, ID: tc.row.ID, Version: version, Confidential: req.Body.Confidential}); errors.Is(err, pgx.ErrNoRows) {
			return stale(tc.row.Version, map[string]any{"confidential": tc.row.Confidential})
		} else if err != nil {
			return err
		}
		action := "confidential_set"
		if !req.Body.Confidential {
			action = "confidential_lifted"
		}
		e := store.Event{EntityType: entityTicket, EntityID: tc.row.ID, TicketID: tc.row.ID,
			TicketKey: domain.FullKey(t.Slug, tc.project.Key, tc.row.Number), Action: action}
		if req.Body.Reason != nil {
			e.Reason = *req.Body.Reason
		}
		w.Record(e)
		out, err = reread(ctx, w, t, tc.row.ID)
		return err
	})
	if err != nil && !errors.Is(err, store.ErrNoChange) {
		return nil, err
	}
	return apigen.SetConfidential200JSONResponse{Body: ticketView(t, out), Headers: apigen.SetConfidential200ResponseHeaders{ETag: etag(out.Version)}}, nil
}

// weakETag is a list page's validator for its caller: a hash of what it
// answers (docs/adr/0054 D7).
func weakETag(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return `W/"` + hex.EncodeToString(sum[:12]) + `"`
}

// notModified compares an If-None-Match header with a weak ETag.
func notModified(ifNoneMatch *string, etag string) bool {
	if ifNoneMatch == nil {
		return false
	}
	return slices.Contains(strings.Split(strings.ReplaceAll(*ifNoneMatch, " ", ""), ","), etag)
}
