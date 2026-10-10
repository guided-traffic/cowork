package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/guided-traffic/cowork/backend/internal/domain"
)

// The crossings between teams (docs/adr/0021 D7 as made concrete 2026-10-10):
// what a relation of a ticket reads or writes of a ticket of another team, and
// nothing else, through the SECURITY DEFINER functions of migration 47. Each
// function decides what the caller sees of a ticket of any team
// (ticket_sight) and admits itself to the owner role's policies by the setting
// it sets and restores; no query of the runtime role is widened, and this file
// is the only one that calls them. Their answers are raw rows: sqlc cannot type
// a table function's columns.

// Sight is what a caller sees of a ticket at the end of a relation of a ticket
// they see (docs/adr/0005 D3, docs/adr/0034 D4, docs/adr/0065 D5).
type Sight string

// The three sights of migration 47's ticket_sight.
const (
	// SightSees: the caller reads the ticket.
	SightSees Sight = "sees"
	// SightHead: its team's name, key, title, type and state, nothing more.
	SightHead Sight = "head"
	// SightPlaceholder: its team's name alone, `<team> [Confidential]`.
	SightPlaceholder Sight = "placeholder"
)

// The kinds of a relation.
const (
	RelationParent = "parent"
	RelationChild  = "child"
	RelationLink   = "link"
)

// Head is a ticket at the end of a relation as the caller sees it: its team,
// and unless it is a placeholder its key, title, type and state.
type Head struct {
	TeamSlug, TeamName string
	ProjectKey         string
	Number             int32
	Title              string
	Type               domain.TicketType
	State              domain.TicketState
	Sight              Sight
}

// Placeholder reports whether the caller may not see the ticket at all.
func (h Head) Placeholder() bool { return h.Sight == SightPlaceholder }

// Readable reports whether the caller reads the ticket, which only then is
// theirs to open.
func (h Head) Readable() bool { return h.Sight == SightSees }

// Key is the ticket's canonical key, "" for a placeholder.
func (h Head) Key() string {
	if h.Placeholder() {
		return ""
	}
	return domain.FullKey(h.TeamSlug, h.ProjectKey, h.Number)
}

// Person is a person a crossing names, as the users policy names them to the
// caller: a person who shares no team with the caller has the id alone.
type Person struct {
	ID       uuid.UUID
	Username *string
	Name     *string
}

// RelatedLink is the link of a relation of the kind link.
type RelatedLink struct {
	ID        uuid.UUID
	Type      domain.LinkType
	Outgoing  bool
	CreatedBy Person
	CreatedAt time.Time
}

// Relation is one relation of a ticket the caller sees — its parent, a child
// or a link — and the ticket at its other end as the caller sees it.
type Relation struct {
	Anchor uuid.UUID
	Kind   string
	Link   *RelatedLink
	Head   Head
	// Assignee is the other end's assignee where it is a ticket of the caller's
	// own team they read; nil otherwise, and for one without an assignee.
	Assignee *Person
	// Position orders a ticket's relations of one kind for a page: the link's
	// id, or the other end's. It is never shown.
	Position uuid.UUID
}

// relationRow is a row of relation_heads, in its columns' order.
type relationRow struct {
	AnchorID              uuid.UUID
	Relation              string
	LinkID                *uuid.UUID
	LinkType              *domain.LinkType
	Outgoing              *bool
	LinkCreatedBy         *uuid.UUID
	LinkCreatedByUsername *string
	LinkCreatedByName     *string
	LinkCreatedAt         *time.Time
	FarID                 uuid.UUID
	TeamSlug              string
	TeamName              string
	ProjectKey            *string
	Number                *int32
	Title                 *string
	Type                  *domain.TicketType
	State                 *domain.TicketState
	Sight                 string
	AssigneeID            *uuid.UUID
	AssigneeUsername      *string
	AssigneeName          *string
}

// RelationHeads reads the relations of the given kinds of the caller's
// tickets, each other end as the caller sees it. An id that is no ticket of
// the transaction's team the caller sees has none.
func (r *Reader) RelationHeads(ctx context.Context, anchors []uuid.UUID, kinds ...string) ([]Relation, error) {
	if len(anchors) == 0 {
		return nil, nil
	}
	rows, err := r.tx.Query(ctx, `SELECT anchor_id, relation, link_id, link_type, outgoing, link_created_by,
		link_created_by_username, link_created_by_name, link_created_at, far_id, team_slug, team_name, project_key,
		number, title, type, state, sight, assignee_id, assignee_username, assignee_name
		FROM relation_heads($1::uuid[], $2::text[])`, anchors, kinds)
	if err != nil {
		return nil, fmt.Errorf("read the relations' heads: %w", err)
	}
	raw, err := pgx.CollectRows(rows, pgx.RowToStructByPos[relationRow])
	if err != nil {
		return nil, fmt.Errorf("read the relations' heads: %w", err)
	}
	out := make([]Relation, 0, len(raw))
	for _, row := range raw {
		rel := Relation{Anchor: row.AnchorID, Kind: row.Relation, Position: row.FarID,
			Head: headOf(row.TeamSlug, row.TeamName, row.ProjectKey, row.Number, row.Title, row.Type, row.State, row.Sight)}
		if row.LinkID != nil && row.LinkType != nil && row.Outgoing != nil && row.LinkCreatedBy != nil && row.LinkCreatedAt != nil {
			rel.Link = &RelatedLink{ID: *row.LinkID, Type: *row.LinkType, Outgoing: *row.Outgoing,
				CreatedBy: Person{ID: *row.LinkCreatedBy, Username: row.LinkCreatedByUsername, Name: row.LinkCreatedByName},
				CreatedAt: *row.LinkCreatedAt}
			rel.Position = *row.LinkID
		}
		if row.AssigneeID != nil {
			rel.Assignee = &Person{ID: *row.AssigneeID, Username: row.AssigneeUsername, Name: row.AssigneeName}
		}
		out = append(out, rel)
	}
	return out, nil
}

// ParentHeads reads the parent of each of the caller's tickets as the caller
// sees it; a ticket without a parent, or one the caller does not see, has none
// in the map.
func (r *Reader) ParentHeads(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]Head, error) {
	rels, err := r.RelationHeads(ctx, ids, RelationParent)
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]Head, len(rels))
	for _, rel := range rels {
		out[rel.Anchor] = rel.Head
	}
	return out, nil
}

func headOf(slug, name string, project *string, number *int32, title *string, typ *domain.TicketType,
	state *domain.TicketState, sight string) Head {
	h := Head{TeamSlug: slug, TeamName: name, Sight: Sight(sight)}
	if h.Placeholder() || project == nil || number == nil || title == nil || typ == nil || state == nil {
		h.Sight = SightPlaceholder
		return h
	}
	h.ProjectKey, h.Number, h.Title, h.Type, h.State = *project, *number, *title, *typ, *state
	return h
}

// FarEnd is a ticket of another team a relation's act is recorded at
// (docs/adr/0012 D3, docs/adr/0024 D2): its team, its id and its key. Only the
// crossings of this file make one, so a handler records into no team of its
// choosing.
type FarEnd struct {
	team, ticket uuid.UUID
	key          string
}

// Key is the far end's canonical key.
func (f FarEnd) Key() string { return f.key }

// Readable is a ticket the caller reads, found by its canonical key in any
// team (readable_ticket): its id, its project, its head and, where it is a
// ticket of another team, the far end its acts are recorded at.
type Readable struct {
	ID        uuid.UUID
	TenantID  uuid.UUID
	ProjectID uuid.UUID
	Head      Head
	// Far is the address of the ticket's acts where it is a ticket of another
	// team than the transaction's; Elsewhere says it is.
	Far       FarEnd
	Elsewhere bool
}

// ReadableTicket finds the ticket a canonical key names, any team of the
// installation, when the caller reads it (docs/adr/0008 D2, docs/adr/0012 D2).
// A key the caller does not read — no such team, project or number, deleted,
// confidential and not admitted, of a team they hold no role in, of a project
// restricted from them, outside a token's restriction — is false, exactly as
// one that does not exist.
func (r *Reader) ReadableTicket(ctx context.Context, teamSlug, projectKey string, number int32) (Readable, bool, error) {
	var (
		rd    Readable
		slug  string
		name  string
		key   string
		num   int32
		title string
		typ   domain.TicketType
		state domain.TicketState
	)
	err := r.tx.QueryRow(ctx, `SELECT id, tenant_id, team_slug, team_name, project_id, project_key, number, title, type, state
		FROM readable_ticket($1, $2, $3)`, teamSlug, projectKey, number).
		Scan(&rd.ID, &rd.TenantID, &slug, &name, &rd.ProjectID, &key, &num, &title, &typ, &state)
	if errors.Is(err, pgx.ErrNoRows) {
		return Readable{}, false, nil
	}
	if err != nil {
		return Readable{}, false, fmt.Errorf("find a readable ticket: %w", err)
	}
	rd.Head = Head{TeamSlug: slug, TeamName: name, ProjectKey: key, Number: num, Title: title, Type: typ, State: state, Sight: SightSees}
	if rd.TenantID != r.TenantID {
		rd.Far, rd.Elsewhere = FarEnd{team: rd.TenantID, ticket: rd.ID, key: rd.Head.Key()}, true
	}
	return rd, true, nil
}

// TreeHead is a node of the prerequisite tree across teams, or of its upward
// reading (prerequisite_heads).
type TreeHead struct {
	Depth       int32
	Path        []uuid.UUID
	Repeated    bool
	Head        Head
	BlockedFrom *domain.TicketState
	// Own is a ticket of the caller's own team they read: only then are its
	// assignee and its progress shown.
	Own                       bool
	Assignee                  *Person
	Progress, ProgressDerived *int16
	Refinement, RefinementDer *int16
	Review, ReviewDerived     *int16
	OpenCount                 int32
}

type treeRow struct {
	Depth                     int32
	Path                      []uuid.UUID
	Repeated                  bool
	TeamSlug, TeamName        string
	ProjectKey                *string
	Number                    *int32
	Title                     *string
	Type                      *domain.TicketType
	State                     *domain.TicketState
	BlockedFrom               *domain.TicketState
	Sight                     string
	Own                       bool
	AssigneeID                *uuid.UUID
	AssigneeUsername          *string
	AssigneeName              *string
	Progress, ProgressDerived *int16
	Refinement, RefinementDer *int16
	Review, ReviewDerived     *int16
	OpenCount                 int32
}

// PrerequisiteHeads reads a page of the prerequisite tree of a ticket of the
// caller's team, or with up of its dependents, across teams (docs/adr/0012 D6
// as amended 2026-10-10): the walk goes on only from a ticket the caller reads,
// a head and a placeholder are leaves, after the node at the path after (nil:
// from the start).
func (r *Reader) PrerequisiteHeads(ctx context.Context, root uuid.UUID, up bool, maxDepth int32, after []uuid.UUID,
	limit int32) ([]TreeHead, error) {
	rows, err := r.tx.Query(ctx, `SELECT depth, path, repeated, team_slug, team_name, project_key, number, title, type,
		state, blocked_from, sight, own, assignee_id, assignee_username, assignee_name, progress, progress_derived,
		progress_refinement, progress_refinement_derived, progress_review, progress_review_derived, open_count
		FROM prerequisite_heads($1, $2, $3, $4::uuid[], $5)`, root, up, maxDepth, after, limit)
	if err != nil {
		return nil, fmt.Errorf("read the prerequisite tree: %w", err)
	}
	raw, err := pgx.CollectRows(rows, pgx.RowToStructByPos[treeRow])
	if err != nil {
		return nil, fmt.Errorf("read the prerequisite tree: %w", err)
	}
	out := make([]TreeHead, 0, len(raw))
	for _, n := range raw {
		h := TreeHead{Depth: n.Depth, Path: n.Path, Repeated: n.Repeated, BlockedFrom: n.BlockedFrom, Own: n.Own,
			Head:     headOf(n.TeamSlug, n.TeamName, n.ProjectKey, n.Number, n.Title, n.Type, n.State, n.Sight),
			Progress: n.Progress, ProgressDerived: n.ProgressDerived, Refinement: n.Refinement,
			RefinementDer: n.RefinementDer, Review: n.Review, ReviewDerived: n.ReviewDerived, OpenCount: n.OpenCount}
		if n.Own && n.AssigneeID != nil {
			h.Assignee = &Person{ID: *n.AssigneeID, Username: n.AssigneeUsername, Name: n.AssigneeName}
		}
		out = append(out, h)
	}
	return out, nil
}

// OpenPrerequisite is an open direct prerequisite of a ticket whose state the
// caller reads (open_prerequisite_heads): what refuses done, and what an
// override names in its act (docs/adr/0012 D7).
type OpenPrerequisite struct {
	ID   uuid.UUID
	Head Head
}

// OpenPrerequisiteHeads reads the open direct prerequisites of a ticket of the
// caller's team, of any team, whose state the caller reads in a head; a
// placeholder neither shows nor refuses.
func (r *Reader) OpenPrerequisiteHeads(ctx context.Context, ticket uuid.UUID) ([]OpenPrerequisite, error) {
	rows, err := r.tx.Query(ctx, `SELECT far_id, team_slug, team_name, project_key, number, title, type, state, sight
		FROM open_prerequisite_heads($1)`, ticket)
	if err != nil {
		return nil, fmt.Errorf("read the open prerequisites: %w", err)
	}
	type row struct {
		FarID              uuid.UUID
		TeamSlug, TeamName string
		ProjectKey         *string
		Number             *int32
		Title              *string
		Type               *domain.TicketType
		State              *domain.TicketState
		Sight              string
	}
	raw, err := pgx.CollectRows(rows, pgx.RowToStructByPos[row])
	if err != nil {
		return nil, fmt.Errorf("read the open prerequisites: %w", err)
	}
	out := make([]OpenPrerequisite, 0, len(raw))
	for _, p := range raw {
		out = append(out, OpenPrerequisite{ID: p.FarID,
			Head: headOf(p.TeamSlug, p.TeamName, p.ProjectKey, p.Number, p.Title, p.Type, p.State, p.Sight)})
	}
	return out, nil
}

// ParentChainReaches reports whether ticket, a ticket of the transaction's
// team, is candidate or one of its ancestors, across teams: the parent cycle
// refusal (docs/adr/0008 D2). The caller holds the parents' graph lock, so the
// walk sees every edge committed before it.
func (w *Writer) ParentChainReaches(ctx context.Context, candidate, ticket uuid.UUID) (bool, error) {
	if w.graphs&(1<<GraphParents) == 0 {
		return false, errors.New("store: the parent walk runs under the parents' graph lock")
	}
	var reached bool
	if err := w.tx.QueryRow(ctx, "SELECT parent_chain_reaches($1, $2)", candidate, ticket).Scan(&reached); err != nil {
		return false, fmt.Errorf("walk the parent chain: %w", err)
	}
	return reached, nil
}

// BlocksReach reports whether to, a ticket of the transaction's team, is
// reachable from from over blocks links, across teams: adding the link to
// blocks from would close a cycle (docs/adr/0012 D4). The caller holds the
// blocks' graph lock.
func (w *Writer) BlocksReach(ctx context.Context, from, to uuid.UUID) (bool, error) {
	if w.graphs&(1<<GraphBlocks) == 0 {
		return false, errors.New("store: the blocks walk runs under the blocks' graph lock")
	}
	var reached bool
	if err := w.tx.QueryRow(ctx, "SELECT blocks_reach($1, $2)", from, to).Scan(&reached); err != nil {
		return false, fmt.Errorf("walk the blocks graph: %w", err)
	}
	return reached, nil
}

// RefreshDerived derives the progress of the given parents again after a
// change of their children, and of their ancestors, across teams
// (docs/adr/0017 D3 as amended 2026-10-10): the one path every refresh takes,
// a same-team change included, since a parent counts its children of every
// team. No version moves and no act is recorded (docs/adr/0050 D1). A parent
// of another team gets its three derived columns and nothing else — its own
// stages, done by hand and updated_at are its own team's — and is told on its
// team's streams when they changed. Nil ids are passed over.
func (w *Writer) RefreshDerived(ctx context.Context, parents ...*uuid.UUID) error {
	ids := make([]uuid.UUID, 0, len(parents))
	for _, p := range parents {
		if p != nil {
			ids = append(ids, *p)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	if _, err := w.tx.Exec(ctx, "SELECT refresh_derived($1::uuid[])", ids); err != nil {
		return fmt.Errorf("derive the progress: %w", err)
	}
	return nil
}

// FarRelation is a relation of a ticket of the transaction's team whose other
// end is a ticket of another team (relations_elsewhere): where its acts are
// recorded, and that ticket's head as somebody outside its team reads it,
// which such an act names it by.
type FarRelation struct {
	Kind     string
	LinkID   *uuid.UUID
	LinkType *domain.LinkType
	Outgoing *bool
	Far      FarEnd
	// Outsider is the far end's head as a person who holds no role in its team
	// reads it: a placeholder where it is confidential.
	Outsider Head
}

// RelationsElsewhere reads every relation of a ticket of the transaction's
// team whose other end is a ticket of another team, whatever the caller sees
// of that ticket: the addresses of the acts a link, a settled prerequisite or
// a purge records there. None of it is shown to the caller.
func (r *Reader) RelationsElsewhere(ctx context.Context, ticket uuid.UUID) ([]FarRelation, error) {
	rows, err := r.tx.Query(ctx, `SELECT relation, link_id, link_type, outgoing, far_tenant, far_id, far_key,
		far_confidential, far_team_name, far_title, far_type, far_state
		FROM relations_elsewhere($1)`, ticket)
	if err != nil {
		return nil, fmt.Errorf("read the relations into other teams: %w", err)
	}
	type row struct {
		Relation        string
		LinkID          *uuid.UUID
		LinkType        *domain.LinkType
		Outgoing        *bool
		FarTenant       uuid.UUID
		FarID           uuid.UUID
		FarKey          string
		FarConfidential bool
		FarTeamName     string
		FarTitle        string
		FarType         domain.TicketType
		FarState        domain.TicketState
	}
	raw, err := pgx.CollectRows(rows, pgx.RowToStructByPos[row])
	if err != nil {
		return nil, fmt.Errorf("read the relations into other teams: %w", err)
	}
	out := make([]FarRelation, 0, len(raw))
	for _, f := range raw {
		key, perr := domain.ParseTicketKey(f.FarKey)
		if perr != nil {
			return nil, fmt.Errorf("read the relations into other teams: %w", perr)
		}
		outsider := Head{TeamSlug: key.Tenant, TeamName: f.FarTeamName, Sight: SightPlaceholder}
		if !f.FarConfidential {
			outsider = Head{TeamSlug: key.Tenant, TeamName: f.FarTeamName, ProjectKey: key.Project, Number: key.Number,
				Title: f.FarTitle, Type: f.FarType, State: f.FarState, Sight: SightHead}
		}
		out = append(out, FarRelation{Kind: f.Relation, LinkID: f.LinkID, LinkType: f.LinkType, Outgoing: f.Outgoing,
			Far: FarEnd{team: f.FarTenant, ticket: f.FarID, key: f.FarKey}, Outsider: outsider})
	}
	return out, nil
}

// RecordElsewhere writes acts on a ticket of another team in that team's
// record, in the transaction (docs/adr/0012 D3, docs/adr/0024 D2): the audit
// rows, the notifications they tell and their publication on that team's
// streams, as the caller's acts — each act's ticket is the far end. The
// transaction is bound to the far team for those statements alone and to its
// own team again after them. The far ticket is held against a deletion of its
// row until the transaction ends, so a purge of it waits for these acts and
// empties them with the rest; one a purge took away before records nothing. A
// FarEnd comes only from a crossing of this file.
func (w *Writer) RecordElsewhere(ctx context.Context, far FarEnd, events ...Event) (err error) {
	if far.team == uuid.Nil || far.ticket == uuid.Nil || far.team == w.TenantID {
		return errors.New("store: an act elsewhere is recorded at a ticket of another team")
	}
	if len(events) == 0 {
		return nil
	}
	if _, err := w.tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", far.team.String()); err != nil {
		return fmt.Errorf("bind the transaction to the other team: %w", err)
	}
	defer func() {
		if err != nil {
			return
		}
		if _, err = w.tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", uuidText(w.TenantID)); err != nil {
			err = fmt.Errorf("bind the transaction to its team again: %w", err)
		}
	}()
	held, err := w.holdFarTicket(ctx, far)
	if err != nil || !held {
		return err
	}
	kept := w.events
	w.events = make([]Event, 0, len(events))
	for _, e := range events {
		e.TicketID, e.TicketKey = far.ticket, far.key
		if e.EntityID == uuid.Nil && e.EntityType == entityTicket {
			e.EntityID = far.ticket
		}
		w.events = append(w.events, e)
	}
	err = w.writeEvents(ctx, far.team, w.caller, Idempotency{}, false)
	w.flushed = w.flushed || err == nil
	w.events = kept
	return err
}

// holdFarTicket locks the far ticket's row FOR KEY SHARE in the far team's
// own context, the transaction bound to it: a purge's deletion of the row
// waits for the transaction, and an update of the ticket does not. False
// where the row is gone — purged since the crossing named it.
func (w *Writer) holdFarTicket(ctx context.Context, far FarEnd) (bool, error) {
	var id uuid.UUID
	err := w.tx.QueryRow(ctx, "SELECT id FROM tickets WHERE tenant_id = $1 AND id = $2 FOR KEY SHARE", far.team, far.ticket).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("hold the ticket of the other team: %w", err)
	}
	return true, nil
}

// farEndOf reads one row of end_relations_elsewhere or end_team_relations.
type farEndRow struct {
	FarTenant uuid.UUID
	FarID     uuid.UUID
	FarKey    string
	Relation  string
	LinkID    *uuid.UUID
	LinkType  *domain.LinkType
	Outgoing  *bool
}

// endRelationsElsewhere ends a purged ticket's relations into other teams
// (end_relations_elsewhere) and records each change in the record of the team
// it changes (docs/adr/0024 D2 as made concrete 2026-10-10): a child there
// becomes a root, `updated`, its version unchanged; a link to or from it goes,
// `unlinked`. Inside the purge, after namePurge.
func (w *Writer) endRelationsElsewhere(ctx context.Context, ticketID uuid.UUID, key string) (int64, error) {
	rows, err := w.tx.Query(ctx, `SELECT far_tenant, far_id, far_key, relation, link_id, link_type, outgoing
		FROM end_relations_elsewhere($1)`, ticketID)
	if err != nil {
		return 0, fmt.Errorf("end the relations into other teams: %w", err)
	}
	ends, err := pgx.CollectRows(rows, pgx.RowToStructByPos[farEndRow])
	if err != nil {
		return 0, fmt.Errorf("end the relations into other teams: %w", err)
	}
	for _, e := range ends {
		far := FarEnd{team: e.FarTenant, ticket: e.FarID, key: e.FarKey}
		ev := endedRelation(e, ticketID, key, "the parent was purged", "the other ticket was purged")
		if err := w.RecordElsewhere(ctx, far, ev); err != nil {
			return 0, err
		}
	}
	return int64(len(ends)), nil
}

// endedRelation is the act recorded on a ticket of another team when its
// relation to the ticket nearID ends: a child's parent cleared, `updated` and
// no refs — the ticket it names is gone from every reader's sight —, or a
// link's removal, `unlinked`, read from that ticket's side.
func endedRelation(e farEndRow, nearID uuid.UUID, nearKey, parentReason, linkReason string) Event {
	if e.Relation == RelationChild {
		return Event{EntityType: entityTicket, EntityID: e.FarID, Action: actionUpdated,
			Before: map[string]any{"parent": nearID.String()}, After: map[string]any{"parent": nil}, Reason: parentReason}
	}
	payload := map[string]any{"type": "", fieldSource: e.FarKey, fieldTarget: nearKey}
	if e.LinkType != nil {
		payload["type"] = string(*e.LinkType)
	}
	// Outgoing is read from the far end: true where it is the link's source.
	if e.Outgoing != nil && !*e.Outgoing {
		payload[fieldSource], payload[fieldTarget] = nearKey, e.FarKey
	}
	ev := Event{EntityType: "link", Action: "unlinked", Before: payload, Reason: linkReason, Refs: []uuid.UUID{nearID}}
	if e.LinkID != nil {
		ev.EntityID = *e.LinkID
	}
	return ev
}

// The fields of a link's act: its source's and its target's keys.
const (
	fieldSource = "source"
	fieldTarget = "target"
)

// jobTeamDeletion names the end of a team's relations in app.job, which
// end_team_relations demands.
const jobTeamDeletion = "team-deletion"

// teamDeletionLock is the job lock key of EndTeamRelations.
const teamDeletionLock = 10

// EndTeamRelations ends every relation between a team and the other teams of
// the installation — what the deletion of a team does first
// (docs/adr/0024 D6 as made concrete 2026-10-10) — and records each change in
// the record of the other team it changes, as system:team-deletion: a child
// there becomes a root, `updated`; a link goes, `unlinked`; a parent there
// counts the team's tickets no more and derives its progress again. No route
// calls it while the deletion of a team is not built. ran is false when the
// job's lock is held elsewhere.
func (db *DB) EndTeamRelations(ctx context.Context, team uuid.UUID) (ran bool, err error) {
	return db.RunJob(ctx, jobTeamDeletion, teamDeletionLock, func(w *Writer) error {
		rows, err := w.tx.Query(ctx, `SELECT far_tenant, far_id, far_key, relation, link_id, link_type, outgoing,
			near_id, near_key FROM end_team_relations($1)`, team)
		if err != nil {
			return fmt.Errorf("end the team's relations: %w", err)
		}
		type row struct {
			farEndRow
			NearID  uuid.UUID
			NearKey string
		}
		ends, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (row, error) {
			var e row
			err := r.Scan(&e.FarTenant, &e.FarID, &e.FarKey, &e.Relation, &e.LinkID, &e.LinkType, &e.Outgoing,
				&e.NearID, &e.NearKey)
			return e, err
		})
		if err != nil {
			return fmt.Errorf("end the team's relations: %w", err)
		}
		var parents []*uuid.UUID
		for _, e := range ends {
			if e.Relation == RelationParent {
				id := e.FarID
				parents = append(parents, &id)
				continue
			}
			ev := endedRelation(e.farEndRow, e.NearID, e.NearKey, "the parent's team was deleted",
				"the other ticket's team was deleted")
			if err := w.RecordElsewhere(ctx, FarEnd{team: e.FarTenant, ticket: e.FarID, key: e.FarKey}, ev); err != nil {
				return err
			}
		}
		return w.RefreshDerived(ctx, parents...)
	})
}
