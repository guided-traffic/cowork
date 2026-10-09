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
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/oapi-codegen/nullable"

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

// ticketView is a ticket as the API shows it at the moment now, which its
// score's age is counted to (docs/adr/0014 D4). The rank key is not shown: it
// is computed over tickets the caller may not see (docs/adr/0014 D2), and the
// list's order is what the caller reads of the rank.
func ticketView(t tenantScope, r store.TicketRow, now time.Time) apigen.Ticket {
	stages := stagesOf(r)
	score, version := scoreView(r, now)
	v := apigen.Ticket{
		Id: r.ID, Key: domain.FullKey(t.Slug, r.ProjectKey, r.Number), Project: r.ProjectKey, Number: int(r.Number),
		Type: apigen.TicketType(r.Type), Title: r.Title, Body: r.Body, State: apigen.TicketState(r.State),
		Severity: apigen.Severity(r.Severity), Security: apigen.SecurityClass(r.Security), Threat: nullableOf(r.Threat),
		Horizon: apigen.Horizon(horizonOf(r)), HorizonSet: horizonSetView(r),
		Effort: apigen.Effort(r.Effort), Progress: stages.Implementation, ProgressRefinement: stages.Refinement,
		ProgressReview: stages.Review, ProgressDerived: hasChildren(r), Confidential: r.Confidential,
		OpenedAt: r.OpenedAt, DecidedAt: nullableOf(r.DecidedAt), DoneAt: nullableOf(r.DoneAt),
		DoneFrom: nullableOf[apigen.TicketState](nil), DoneByHand: doneByHand(r),
		OpenPrerequisites: int(r.OpenPrerequisites), Score: score, ScoreVersion: version,
		Version: int(r.Version), CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
		Reporter: personView(r.ReporterID, r.ReporterUsername, r.ReporterName), ReporterAgent: nullableOf(r.ReporterAgent),
		ReporterToken: tokenMarkView(r.ReporterTokenID, r.ReporterTokenName), Block: nullableOf[apigen.Block](nil),
		Assignee: nullableOf[apigen.Person](nil), Parent: nullableOf[string](nil),
	}
	if r.State == domain.StateDone {
		from := apigen.TicketState(origin(r))
		v.DoneFrom = nullableOf(&from)
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

// horizonSetView is the horizon a person or an agent set on the ticket, null
// where none is set (docs/adr/0010 D3); the columns keep the name urgency
// override (docs/adr/0010 D1). Its person is named as the reporter is.
func horizonSetView(r store.TicketRow) nullable.Nullable[apigen.HorizonSet] {
	if r.UrgencyOverride == nil || r.UrgencyOverrideAt == nil {
		return nullableOf[apigen.HorizonSet](nil)
	}
	set := apigen.HorizonSet{Value: apigen.Horizon(*r.UrgencyOverride), At: *r.UrgencyOverrideAt,
		By: nullableOf[apigen.Person](nil), Reason: nullableOf(r.UrgencyOverrideReason)}
	if r.UrgencyOverrideBy != nil {
		p := personView(*r.UrgencyOverrideBy, r.UrgencyOverrideByUsername, r.UrgencyOverrideByName)
		set.By = nullableOf(&p)
	}
	return nullableOf(&set)
}

// hasChildren reports whether a ticket's stages are derived from children:
// the derivation keeps progress_derived set while there are any
// (docs/adr/0017 D3) — this release and the previous one alike.
func hasChildren(r store.TicketRow) bool { return r.ProgressDerived != nil }

// stagesOf is the three progress stages a ticket shows: each derived from the
// children while there are any, else its own (docs/adr/0017 D2, D3). Done
// leaves them as they are (D5). The derived refinement and review count only
// while progress_derived says there are children: the previous release, run
// over this schema in a rollback (docs/adr/0028 D4), derives the
// implementation stage alone, and when a parent's last child leaves it clears
// progress_derived and leaves the other two as they were.
func stagesOf(r store.TicketRow) domain.Stages {
	if !hasChildren(r) {
		return domain.Stages{Refinement: int(r.ProgressRefinement), Implementation: int(r.Progress), Review: int(r.ProgressReview)}
	}
	return domain.Stages{
		Refinement:     int(derivedOr(r.ProgressRefinementDerived, r.ProgressRefinement)),
		Implementation: int(*r.ProgressDerived),
		Review:         int(derivedOr(r.ProgressReviewDerived, r.ProgressReview)),
	}
}

// doneByHand reports whether a done ticket is done by hand: every done but the
// one by its stages (docs/adr/0009 D5). The column says what was set by hand;
// a parent, and a leaf whose stages are short of full, are done by hand
// whatever it holds.
func doneByHand(r store.TicketRow) bool {
	return r.State == domain.StateDone && !domain.DoneByStages(r.DoneByHand, hasChildren(r), stagesOf(r))
}

func derivedOr(derived *int16, own int16) int16 {
	if derived != nil {
		return *derived
	}
	return own
}

// refreshProgress derives the stages of a parent again after a change of its
// children, and of its ancestors as far as a value changes; their versions
// stay (docs/adr/0017 D3, docs/adr/0050 D1).
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

// actAgent is the agent mark a row records of an agent's act; nil for a
// person's own (docs/adr/0036 D6).
func actAgent(p auth.Principal) *string {
	if !p.IsAgent() {
		return nil
	}
	agent := p.Agent
	return &agent
}

// actToken is the token a request came through, as a row records it beside
// the agent mark: its id and its name, both nil for a browser session's act
// (docs/adr/0036 D6). The name is copied, because a reader of the act may not
// read the token's row.
func actToken(p auth.Principal) (id *uuid.UUID, name *string) {
	if p.TokenID == uuid.Nil {
		return nil, nil
	}
	tokenID, tokenName := p.TokenID, p.TokenName
	return &tokenID, &tokenName
}

// tokenMarkView is the token an act came through, as the API shows it; null
// for a browser session's act. Never the token's secret or hash: the row
// holds neither.
func tokenMarkView(id *uuid.UUID, name *string) nullable.Nullable[apigen.TokenMark] {
	if id == nil {
		return nullable.NewNullNullable[apigen.TokenMark]()
	}
	return nullable.NewNullableWithValue(apigen.TokenMark{Id: *id, Name: nullableOf(name)})
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
	return apigen.GetTicket200JSONResponse{Body: ticketView(t, row, s.h.opts.Now()), Headers: apigen.GetTicket200ResponseHeaders{ETag: etag(row.Version)}}, nil
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
	return apigen.ResolveTicket200JSONResponse{Body: ticketView(t, row, s.h.opts.Now()), Headers: apigen.ResolveTicket200ResponseHeaders{ETag: etag(row.Version)}}, nil
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

// CreateTicket files a ticket (docs/adr/0007, 0010, 0065 D2), into its
// horizon at its place (docs/adr/0010 D3, docs/adr/0014 D2).
func (s *Server) CreateTicket(ctx context.Context, req apigen.CreateTicketRequestObject) (apigen.CreateTicketResponseObject, error) {
	t := tenantFrom(ctx)
	body := *req.Body
	ctx, perr := s.keyed(ctx, req.Params.IdempotencyKey, "createTicket", t.ID.String()+"/"+req.Project, body)
	if perr != nil {
		return nil, perr
	}
	if perr := checkThreat(domain.SecurityClass(body.Security), body.Threat); perr != nil {
		return nil, perr
	}
	f, perr := filingOf(body)
	if perr != nil {
		return nil, perr
	}
	var created store.TicketRow
	replay, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		p, err := fileableProject(ctx, w.Reader, t, req.Project, f.capabilities()...)
		if err != nil {
			return err
		}
		ins, near, err := s.newTicket(ctx, w, t, p, body, f)
		if err != nil {
			return err
		}
		id, err := insertTicket(ctx, w, t, ins)
		if err != nil {
			return err
		}
		key := domain.FullKey(t.Slug, p.Key, ins.Number)
		after := map[string]any{fieldType: body.Type, fieldTitle: body.Title, fieldSeverity: body.Severity,
			fieldSecurity: body.Security, fieldEffort: body.Effort, "urgency": f.horizon}
		if near != nil {
			after[f.place.side()] = ticketKey(t, *near)
		}
		w.Record(store.Event{EntityType: entityTicket, EntityID: id, TicketID: id, TicketKey: key, Action: actionCreated, After: after,
			Notices: told(store.NoticeAssigned, ins.AssigneeID)})
		if ins.Confidential {
			w.Record(store.Event{EntityType: entityTicket, EntityID: id, TicketID: id, TicketKey: key,
				Action: actionConfidentialSet, Reason: "the security class is " + string(body.Security)})
		}
		if created, err = reread(ctx, w, t, id); err != nil {
			return err
		}
		res, err := stored(ticketView(t, created, s.h.opts.Now()), map[string]string{
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
	return apigen.CreateTicket201JSONResponse{Body: ticketView(t, created, s.h.opts.Now()), Headers: apigen.CreateTicket201ResponseHeaders{
		ETag: etag(created.Version), Location: &location}}, nil
}

// insertTicket writes a filing: the ticket, its score (docs/adr/0014 D4) and
// the stages its parent derives from it (docs/adr/0017 D3).
func insertTicket(ctx context.Context, w *store.Writer, t tenantScope, ins writeq.InsertTicketParams) (uuid.UUID, error) {
	id, err := w.InsertTicket(ctx, ins)
	if err != nil {
		return uuid.Nil, err
	}
	if err := refreshScore(ctx, w, t, id); err != nil {
		return uuid.Nil, err
	}
	return id, refreshProgress(ctx, w, t, ins.ParentID)
}

// filing is what a filing decides beyond the ticket's fields: the horizon
// it is filed into and the ticket it is placed next to, nil at the bottom.
type filing struct {
	horizon domain.Urgency
	place   *rankTarget
}

// filingOf reads a filing's horizon, later when left out (docs/adr/0010 D3),
// and its place, at most one neighbour (docs/adr/0014 D2).
func filingOf(b apigen.TicketCreate) (filing, *problem.Error) {
	f := filing{horizon: domain.UrgencyDefault}
	if b.Horizon != nil {
		f.horizon = domain.Urgency(*b.Horizon)
	}
	switch {
	case b.After != nil && b.Before != nil:
		return f, problem.Field("/before", "a filing names at most one neighbour")
	case b.After != nil:
		f.place = &rankTarget{after: true, number: *b.After}
	case b.Before != nil:
		f.place = &rankTarget{number: *b.Before}
	}
	return f, nil
}

// capabilities are what an agent's filing needs beyond the baseline:
// set-horizon for a horizon other than later, rank for a place
// (docs/adr/0043 D4).
func (f filing) capabilities() []string {
	var caps []string
	if f.horizon != domain.UrgencyDefault {
		caps = append(caps, auth.CapSetHorizon)
	}
	if f.place != nil {
		caps = append(caps, auth.CapRank)
	}
	return caps
}

// fileableProject reads the project a ticket is filed in: visible, open,
// and the caller a member there with write scope (docs/adr/0043 D2) and, for
// an agent, every capability named.
func fileableProject(ctx context.Context, r *store.Reader, t tenantScope, key string, capabilities ...string) (project, error) {
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
	for _, c := range capabilities {
		need := work
		need.Capability = c
		if perr := auth.Authorize(principal(ctx), role, need); perr != nil {
			return p, perr
		}
	}
	if p.ArchivedAt != nil {
		return p, problem.New(problem.ProjectArchived, "the project is archived")
	}
	return p, nil
}

// newTicket validates a new ticket's references and derives what is derived:
// the number, the rank at the filing's place or the bottom of the project
// (docs/adr/0014 D2), the horizon (docs/adr/0010 D3) and the confidential
// flag (docs/adr/0065 D2). It returns the ticket placed next to, nil at the
// bottom. The number's counter row is the project's rank lock as well, taken
// before any ticket row is written.
func (s *Server) newTicket(ctx context.Context, w *store.Writer, t tenantScope, p project, body apigen.TicketCreate, f filing) (writeq.InsertTicketParams, *store.TicketRow, error) {
	caller := principal(ctx)
	ins := writeq.InsertTicketParams{
		TenantID: t.ID, ProjectID: p.ID, Type: domain.TicketType(body.Type), Title: body.Title,
		Severity: domain.Severity(body.Severity), Security: domain.SecurityClass(body.Security), Threat: body.Threat,
		Effort: domain.Effort(body.Effort), ReporterID: caller.PersonID, ReporterAgent: actAgent(caller),
		Confidential: domain.SecurityClass(body.Security).MakesConfidential(),
	}
	ins.ReporterTokenID, ins.ReporterTokenName = actToken(caller)
	if body.Body != nil {
		ins.Body = *body.Body
	}
	ins.UrgencyDerived, ins.UrgencyRule = domain.UrgencyDefault, domain.UrgencyRuleDefault
	if f.horizon != domain.UrgencyDefault {
		horizon := f.horizon
		ins.UrgencyOverride, ins.UrgencyOverrideBy = &horizon, &caller.PersonID
	}
	if body.Parent != nil {
		parent, perr := resolveParent(ctx, w.Reader, t, p, *body.Parent)
		if perr != nil {
			return ins, nil, perr
		}
		ins.ParentID = &parent
	}
	if body.Assignee != nil {
		if err := checkAssignee(ctx, w.Reader, t, p.ID, *body.Assignee); err != nil {
			return ins, nil, err
		}
		if perr := mayAssign(caller, ins.Confidential, nil, *body.Assignee); perr != nil {
			return ins, nil, perr
		}
		ins.AssigneeID = body.Assignee
	}
	number, err := w.NextTicketNumber(ctx, writeq.NextTicketNumberParams{TenantID: t.ID, ProjectID: p.ID})
	if err != nil {
		return ins, nil, fmt.Errorf("next ticket number: %w", err)
	}
	ins.Number = number
	if f.place == nil {
		ins.Rank, err = rankAtBottom(ctx, w, t, p.ID)
		return ins, nil, err
	}
	near, key, err := rankBeside(ctx, w, t, p.ID, *f.place, f.horizon)
	ins.Rank = key
	return ins, &near, err
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

// mayAssign holds an assignment to the hard-off rule of docs/adr/0043 D3:
// the assignee of a confidential ticket is admitted to it (docs/adr/0065 D9),
// so an agent assigns one only to its own person — assigning nobody admits
// nobody and never reaches here. A person's token is held the same way, by the
// rule of docs/adr/0035 D5: admitting another person to a confidential ticket
// gives access that would outlive a leaked token's revocation, so it takes a
// browser session. confidential is the flag as the write leaves it; current
// is the assignee before the write, nil on a filing, and an assignee the write
// leaves as it was admits nobody new.
func mayAssign(p auth.Principal, confidential bool, current *uuid.UUID, assignee uuid.UUID) *problem.Error {
	if !confidential || assignee == p.PersonID || (current != nil && *current == assignee) {
		return nil
	}
	if perr := auth.Authorize(p, "", auth.Need{HardOff: auth.HardOffConfidentialAssignee}); perr != nil {
		return perr
	}
	if !p.Session {
		return problem.New(problem.SessionRequired,
			"assigning a confidential ticket to another person admits them to it, which takes a browser session; a token assigns it only to its own person")
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
		if perr := stageInputs(principal(ctx), tc, ch.effect, *req.Body); perr != nil {
			return perr
		}
		if out, err = writeTicketChange(ctx, w, t, tc, ch, *req.Body); errors.Is(err, store.ErrNoChange) {
			out = tc.row
		}
		return err
	})
	if err != nil && !errors.Is(err, store.ErrNoChange) {
		return nil, err
	}
	return apigen.UpdateTicket200JSONResponse{Body: ticketView(t, out, s.h.opts.Now()), Headers: apigen.UpdateTicket200ResponseHeaders{ETag: etag(out.Version)}}, nil
}

// writeTicketChange writes a patch with its acts, its explaining comment and
// its effects — the done act or the reopen its stages make among them; a
// patch that changes nothing is ErrNoChange.
func writeTicketChange(ctx context.Context, w *store.Writer, t tenantScope, tc ticketCtx, ch ticketChange, body apigen.TicketPatch) (store.TicketRow, error) {
	changedBefore, changedAfter := diffDeep(ch.before, ch.after)
	if len(changedAfter) == 0 {
		return store.TicketRow{}, store.ErrNoChange
	}
	sm, err := planStageMove(ctx, w, t, tc, ch, body)
	if err != nil {
		return store.TicketRow{}, err
	}
	if _, err := w.UpdateTicketFields(ctx, ch.params); errors.Is(err, pgx.ErrNoRows) {
		return store.TicketRow{}, stale(tc.row.Version, pick(ch.before, ch.sent))
	} else if err != nil {
		return store.TicketRow{}, err
	}
	explainedBy, err := explain(ctx, w, t, tc, body.Comment)
	if err != nil {
		return store.TicketRow{}, err
	}
	recordTicketChange(w, t, tc, changedBefore, changedAfter, ch, explainedBy)
	if sm != nil {
		if err := sm.write(ctx, w, t, tc, ch.params, body, explainedBy); err != nil {
			return store.TicketRow{}, err
		}
	}
	if sm != nil || changes(changedAfter, fieldParent, "effort", fieldProgress, fieldRefinement, fieldReview) {
		if err := refreshProgress(ctx, w, t, tc.row.ParentID, ch.params.ParentID); err != nil {
			return store.TicketRow{}, err
		}
	}
	if changes(changedAfter, "severity") {
		if err := refreshScore(ctx, w, t, tc.row.ID); err != nil {
			return store.TicketRow{}, err
		}
	}
	return reread(ctx, w, t, tc.row.ID)
}

// stageMove is the state change a patch's stages make: the done act, or the
// reopen of a ticket done by its stages (docs/adr/0009 D5).
type stageMove struct {
	change stateChange
	after  map[string]any
	refs   []uuid.UUID
}

// planStageMove prepares what the patch's stages do before any ticket row is
// written: the done act is refused over open prerequisites unless a person
// overrides (docs/adr/0012 D7); a reopen takes its key at the bottom of the
// rank under the project's lock (docs/adr/0014 D2). nil when the stages move
// no state.
func planStageMove(ctx context.Context, w *store.Writer, t tenantScope, tc ticketCtx, ch ticketChange, body apigen.TicketPatch) (*stageMove, error) {
	switch ch.effect {
	case domain.StagesComplete:
		m := &stageMove{change: stateChange{from: tc.row.State, to: domain.StateDone},
			after: map[string]any{fieldState: string(domain.StateDone), fieldDoneByHand: false}}
		var ev store.Event
		if err := closeOver(ctx, w.Reader, t, tc, overrides(body.OverridePrerequisites), lastStageSent(ch.sent), m.after, &ev); err != nil {
			return nil, err
		}
		m.refs = ev.Refs
		return m, nil
	case domain.StagesReopen:
		key, err := rankAtBottom(ctx, w, t, tc.project.ID)
		if err != nil {
			return nil, err
		}
		to := origin(tc.row)
		return &stageMove{change: stateChange{from: domain.StateDone, to: to, rank: &key},
			after: map[string]any{fieldState: string(to)}}, nil
	}
	return nil, nil
}

// lastStageSent is the pointer of the last stage a patch sent: the field a
// refusal of its done act names.
func lastStageSent(sent []string) string {
	pointer := "/" + fieldReview
	for _, f := range sent {
		if f == fieldRefinement || f == fieldProgress || f == fieldReview {
			pointer = "/" + f
		}
	}
	return pointer
}

// write moves the state after the fields are written — the version raised
// once, by the fields — and records the transition beside the field act, with
// the note and the reason (docs/adr/0009 D5, D6). The move reads the type and
// the parent the patch gave the ticket.
func (m *stageMove) write(ctx context.Context, w *store.Writer, t tenantScope, tc ticketCtx, params writeq.UpdateTicketFieldsParams, body apigen.TicketPatch, explainedBy uuid.UUID) error {
	moved := tc
	moved.row.Type, moved.row.ParentID = params.Type, params.ParentID
	if _, err := move(ctx, w, t, moved, m.change, m.after); err != nil {
		return err
	}
	w.Record(store.Event{EntityType: entityTicket, EntityID: tc.row.ID, TicketID: tc.row.ID, TicketKey: ticketKey(t, tc.row),
		Action: actionTransitioned, Before: map[string]any{fieldState: string(m.change.from)}, After: m.after,
		Reason: deref(body.Reason), Note: deref(body.Note), ExplainedBy: explainedBy, Refs: m.refs,
		Notices: stateNotices(m.change.to)})
	return nil
}

// stageInputs holds a patch to what its stages do (docs/adr/0009 D5,
// docs/adr/0017 D4, docs/adr/0043 D4): the done act needs the verification
// note, of an agent close and a ticket in in-progress or review, and an
// override of the prerequisites a person with a reason; the reopen of a ticket
// done by its stages needs a reason; a change that moves no state takes no
// note, no override and no reason.
func stageInputs(p auth.Principal, tc ticketCtx, effect domain.StageEffect, body apigen.TicketPatch) *problem.Error {
	override := overrides(body.OverridePrerequisites)
	switch effect {
	case domain.StagesComplete:
		if perr := mayClose(p, tc.role, tc.row.State, override); perr != nil {
			return perr
		}
		switch {
		case blank(body.Note):
			return problem.Field("/note", "this change brings the last progress stage to 100 and completes the ticket: "+
				"it needs the verification note — what was run, against what, with what result")
		case override && blank(body.Reason):
			return problem.Field("/reason", "overriding the open prerequisites needs a reason")
		}
	case domain.StagesReopen:
		switch {
		case blank(body.Reason):
			return problem.Field("/reason", "this change lowers a stage of a ticket done by its stages and reopens it: it needs a reason")
		case body.Note != nil:
			return problem.Field("/note", "only the change that completes the ticket takes a verification note")
		case override:
			return problem.Field("/override_prerequisites", "only the change that completes the ticket is refused by prerequisites")
		}
	default:
		switch {
		case body.Note != nil:
			return problem.Field("/note", "only the change that completes the ticket takes a verification note")
		case override:
			return problem.Field("/override_prerequisites", "only the change that completes the ticket is refused by prerequisites")
		case body.Reason != nil:
			return problem.Field("/reason", "a reason goes with a change that reopens the ticket or overrides its prerequisites")
		}
	}
	return nil
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

// ticketChange is a patch applied to a ticket: the update, the values of the
// fields before and after, keyed as the request names them, and what its
// stages do to the state.
type ticketChange struct {
	params             writeq.UpdateTicketFieldsParams
	before, after      map[string]any
	sent               []string
	becameConfidential bool
	effect             domain.StageEffect
}

// The patch's names of the three progress stages (docs/adr/0017 D2).
const (
	fieldProgress   = "progress"
	fieldRefinement = "progress_refinement"
	fieldReview     = "progress_review"
)

func applyTicketPatch(ctx context.Context, w *store.Writer, t tenantScope, tc ticketCtx, p apigen.TicketPatch) (ticketChange, error) {
	row := tc.row
	ch := ticketChange{params: writeq.UpdateTicketFieldsParams{
		TenantID: t.ID, ID: row.ID, Version: row.Version, Type: row.Type, Title: row.Title, Severity: row.Severity,
		Security: row.Security, Threat: row.Threat, Effort: row.Effort, ParentID: row.ParentID,
		AssigneeID: row.AssigneeID, Progress: row.Progress, ProgressRefinement: row.ProgressRefinement,
		ProgressReview: row.ProgressReview, Confidential: row.Confidential,
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
	if perr := applyStages(row, p, &ch); perr != nil {
		return ch, perr
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

// applyStages sets the progress stages a patch sends, in every state but
// dropped, on a ticket without children (docs/adr/0017 D2, D3), and decides
// what they do to its state (docs/adr/0009 D5).
func applyStages(row store.TicketRow, p apigen.TicketPatch, ch *ticketChange) *problem.Error {
	shown := stagesOf(row)
	for _, s := range []struct {
		field   string
		value   *int
		dst     *int16
		current int
	}{
		{fieldRefinement, p.ProgressRefinement, &ch.params.ProgressRefinement, shown.Refinement},
		{fieldProgress, p.Progress, &ch.params.Progress, shown.Implementation},
		{fieldReview, p.ProgressReview, &ch.params.ProgressReview, shown.Review},
	} {
		if s.value == nil {
			continue
		}
		switch {
		case row.State == domain.StateDropped:
			return &problem.Error{Code: problem.StateConflict, Detail: "a dropped ticket's progress does not change",
				Errors: []problem.FieldError{{Pointer: "/" + s.field, Message: "the ticket is dropped", Current: s.current}}}
		case hasChildren(row):
			return &problem.Error{Code: problem.StateConflict, Detail: "the progress is derived from the ticket's children",
				Errors: []problem.FieldError{{Pointer: "/" + s.field, Message: "derived", Current: s.current}}}
		}
		v := *s.value
		if v < 0 || v > 100 || !domain.ValidProgress(v) {
			return problem.Field("/"+s.field, "0 to 100 in steps of five")
		}
		*s.dst, ch.sent = int16(v), append(ch.sent, s.field)
	}
	if !hasChildren(row) {
		after := domain.Stages{Refinement: int(ch.params.ProgressRefinement), Implementation: int(ch.params.Progress),
			Review: int(ch.params.ProgressReview)}
		ch.effect = domain.EffectOfStages(row.State, doneByHand(row), shown, after)
	}
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
			if perr := mayAssign(principal(ctx), ch.params.Confidential, tc.row.AssigneeID, person); perr != nil {
				return perr
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
		fieldType: string(p.Type), "title": p.Title, fieldSeverity: string(p.Severity), "security": string(p.Security),
		"threat": threat, fieldEffort: string(p.Effort), fieldParent: parent, fieldAssignee: assignee, fieldProgress: int(p.Progress),
		fieldRefinement: int(p.ProgressRefinement), fieldReview: int(p.ProgressReview),
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
		e := ev("assigned", map[string]any{fieldAssignee: before[fieldAssignee]}, map[string]any{fieldAssignee: after[fieldAssignee]})
		e.Notices = told(store.NoticeAssigned, ch.params.AssigneeID)
		w.Record(e)
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
		e := ev(actionConfidentialSet, nil, nil)
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
	return apigen.ReplaceTicketBody200JSONResponse{Body: ticketView(t, out, s.h.opts.Now()), Headers: apigen.ReplaceTicketBody200ResponseHeaders{ETag: etag(out.Version)}}, nil
}

// SetHorizon sets the ticket's horizon, which holds until a person or an
// agent sets another (docs/adr/0010 D3); later clears the horizon set, since
// later is where a ticket nobody placed stands. The reason is optional for a
// person and required of an agent, which needs set-horizon (docs/adr/0043 D4).
func (s *Server) SetHorizon(ctx context.Context, req apigen.SetHorizonRequestObject) (apigen.SetHorizonResponseObject, error) {
	t := tenantFrom(ctx)
	hw := horizonWrite{reason: req.Body.Reason}
	if value := domain.Urgency(req.Body.Value); value != domain.UrgencyDefault {
		hw.value = &value
	}
	out, err := s.setOverride(ctx, t, req.Project, req.Number, req.Params.IfMatch, hw)
	if err != nil {
		return nil, err
	}
	return apigen.SetHorizon200JSONResponse{Body: ticketView(t, out, s.h.opts.Now()), Headers: apigen.SetHorizon200ResponseHeaders{ETag: etag(out.Version)}}, nil
}

// horizonWrite is a write of a ticket's set horizon, which the columns keep
// under the name urgency override (docs/adr/0010 D1): value nil clears it.
type horizonWrite struct {
	value  *domain.Urgency
	reason *string
}

// current is what a 412 names of the ticket: its horizon and its set horizon.
func (hw horizonWrite) current(r store.TicketRow) map[string]any {
	cur := map[string]any{fieldHorizon: horizonOf(r), fieldHorizonSet: nil}
	if set, err := horizonSetView(r).Get(); err == nil {
		cur[fieldHorizonSet] = set
	}
	return cur
}

func (s *Server) setOverride(ctx context.Context, t tenantScope, projectKey string, number int, ifm *string, hw horizonWrite) (store.TicketRow, error) {
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
		if perr := horizonInputs(principal(ctx), tc.role, hw); perr != nil {
			return perr
		}
		if tc.row.Version != version {
			return stale(tc.row.Version, hw.current(tc.row))
		}
		if hw.value == nil && tc.row.UrgencyOverride == nil {
			out = tc.row
			return store.ErrNoChange
		}
		// A reason is kept with a horizon set, never without one
		// (migration 19); the act records it either way.
		var by *uuid.UUID
		kept := hw.reason
		if hw.value != nil {
			person := principal(ctx).PersonID
			by = &person
		} else {
			kept = nil
		}
		if _, err := w.SetUrgencyOverride(ctx, writeq.SetUrgencyOverrideParams{TenantID: t.ID, ID: tc.row.ID, Version: version,
			UrgencyOverride: hw.value, Reason: kept, OverrideBy: by}); errors.Is(err, pgx.ErrNoRows) {
			return stale(tc.row.Version, hw.current(tc.row))
		} else if err != nil {
			return err
		}
		if err := refreshScore(ctx, w, t, tc.row.ID); err != nil {
			return err
		}
		e := store.Event{EntityType: entityTicket, EntityID: tc.row.ID, TicketID: tc.row.ID,
			TicketKey: domain.FullKey(t.Slug, tc.project.Key, tc.row.Number), Action: actionOverridden,
			Before: map[string]any{fieldUrgencyOverride: tc.row.UrgencyOverride}, After: map[string]any{fieldUrgencyOverride: hw.value}}
		if hw.reason != nil {
			e.Reason = *hw.reason
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

// horizonInputs holds a write of the set horizon to its rules: a member's act
// with write scope; an agent needs set-horizon, and a reason for every
// horizon it sets, later included (docs/adr/0010 D3, docs/adr/0043 D4).
func horizonInputs(p auth.Principal, role domain.Role, hw horizonWrite) *problem.Error {
	need := work
	need.Capability = auth.CapSetHorizon
	if perr := auth.Authorize(p, role, need); perr != nil {
		return perr
	}
	if p.IsAgent() && blank(hw.reason) {
		return problem.Field("/reason", "an agent sets a horizon with a reason")
	}
	return nil
}

// SetConfidential sets or lifts the confidential flag: a tenant
// administrator's act, never an agent's; lifting needs a reason
// (docs/adr/0065 D2, D3, D6) and a browser session — it shows the ticket to
// every member, which would outlive a leaked token's revocation, while setting
// the flag only takes sight away and stays open to a token (docs/adr/0035 D5).
func (s *Server) SetConfidential(ctx context.Context, req apigen.SetConfidentialRequestObject) (apigen.SetConfidentialResponseObject, error) {
	t, p := tenantFrom(ctx), principal(ctx)
	if perr := auth.Authorize(p, t.Role, auth.Need{Role: domain.RoleAdmin, Scope: domain.ScopeAdmin, HardOff: auth.HardOffConfidential}); perr != nil {
		return nil, perr
	}
	if !req.Body.Confidential && !p.Session {
		return nil, problem.New(problem.SessionRequired, "lifting the confidential flag takes a browser session; a token may only set it")
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
		action := actionConfidentialSet
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
	return apigen.SetConfidential200JSONResponse{Body: ticketView(t, out, s.h.opts.Now()), Headers: apigen.SetConfidential200ResponseHeaders{ETag: etag(out.Version)}}, nil
}

// weakETag is a list page's validator for its caller: a hash of what it
// answers (docs/adr/0054 D7).
func weakETag(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return `W/"` + hex.EncodeToString(sum[:12]) + `"`
}

// listTag is a list page's weak ETag, and whether the client holds the page
// already: its If-None-Match names the tag (docs/adr/0054 D7).
func listTag(ifNoneMatch *string, page any) (string, bool) {
	tag := weakETag(page)
	return tag, notModified(ifNoneMatch, tag)
}

// notModified compares an If-None-Match header with a weak ETag.
func notModified(ifNoneMatch *string, etag string) bool {
	if ifNoneMatch == nil {
		return false
	}
	return slices.Contains(strings.Split(strings.ReplaceAll(*ifNoneMatch, " ", ""), ","), etag)
}

// told is the notice of an act that names one person (docs/adr/0020 D2) — the
// assignee, the person asked —, none where it names nobody.
func told(reason string, person *uuid.UUID) []store.Notice {
	if person == nil {
		return nil
	}
	return []store.Notice{{Reason: reason, People: []uuid.UUID{*person}}}
}
