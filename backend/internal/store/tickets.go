package store

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/store/readq"
)

// TicketRow is a ticket as the API shows it: the columns of
// queries/read/tickets.sql, which the list builder selects as well.
type TicketRow = readq.GetTicketByNumberRow

// ticketSelect is the list builder's copy of the ticket queries' column list
// and joins; TestTicketListSelectsWhatTheQueriesSelect holds it equal to
// GetTicketByNumber, so a list row and a single ticket are one type.
const ticketSelect = `SELECT t.id, t.project_id, p.key AS project_key, t.number, t.type, t.title, t.body, t.state,
       t.blocked_from, t.block_kind, t.block_reason, t.block_ticket_id, t.block_external_ref,
       bp.key AS block_project_key, bt.number AS block_number,
       t.severity, t.security, t.threat, t.urgency_derived, t.urgency_rule, t.urgency_override,
       t.urgency_override_reason, t.urgency_override_by, t.urgency_override_at, t.effort, t.progress, t.progress_derived,
       t.parent_id, pt.number AS parent_number,
       t.reporter_id, ru.username AS reporter_username, ru.display_name AS reporter_name,
       t.assignee_id, au.username AS assignee_username, au.display_name AS assignee_name,
       t.confidential, t.rank, t.opened_at, t.decided_at, t.done_at, t.version, t.created_at, t.updated_at`

const ticketFrom = `FROM tickets t
JOIN projects p ON p.tenant_id = t.tenant_id AND p.id = t.project_id
LEFT JOIN users ru ON ru.id = t.reporter_id
LEFT JOIN users au ON au.id = t.assignee_id
LEFT JOIN tickets pt ON pt.tenant_id = t.tenant_id AND pt.id = t.parent_id
     AND app_ticket_visible(pt.project_id, pt.confidential, pt.assignee_id, pt.reporter_id)
LEFT JOIN tickets bt ON bt.tenant_id = t.tenant_id AND bt.id = t.block_ticket_id
     AND app_ticket_visible(bt.project_id, bt.confidential, bt.assignee_id, bt.reporter_id)
LEFT JOIN projects bp ON bp.tenant_id = bt.tenant_id AND bp.id = bt.project_id`

// ValueSet is one repeatable filter parameter: any of In, none of NotIn
// (docs/adr/0049 D1, D2).
type ValueSet struct {
	In    []string
	NotIn []string
}

// Empty reports whether the parameter was not given.
func (s ValueSet) Empty() bool { return len(s.In) == 0 && len(s.NotIn) == 0 }

// PersonSet is a person filter: ids, and none for the column being empty.
type PersonSet struct {
	In      []uuid.UUID
	NotIn   []uuid.UUID
	None    bool
	NotNone bool
}

// TicketFilter is a ticket list's filter, parsed and checked by the API
// (docs/adr/0049).
type TicketFilter struct {
	// ProjectID narrows to one project (the project's list).
	ProjectID uuid.UUID
	// Projects filters a tenant-wide list by project keys.
	Projects   ValueSet
	States     ValueSet
	Types      ValueSet
	Severities ValueSet
	Securities ValueSet
	Urgencies  ValueSet
	Efforts    ValueSet
	Assignees  PersonSet
	Reporters  PersonSet
	// Parents filters by parent ticket id, None for roots.
	Parents       PersonSet
	ProgressMin   *int
	ProgressMax   *int
	OpenedAfter   *time.Time
	OpenedBefore  *time.Time
	UpdatedAfter  *time.Time
	UpdatedBefore *time.Time
	Query         string
	// Blocked filters by whether an open ticket the caller can see blocks
	// the ticket (docs/adr/0049 D1).
	Blocked *bool
	// HasOpenQuestions filters by whether the ticket has an open question.
	HasOpenQuestions *bool
	// Interest filters by stakes: Me for the caller's own (Person), Any
	// for anyone's, each with its negation (docs/adr/0049 D1).
	Interest InterestFilter
	// IncludeTerminal shows done and dropped tickets; without it they show
	// only when States names them.
	IncludeTerminal bool
}

// InterestFilter is the interest parameter: me and any, each set to true
// or, negated, to false.
type InterestFilter struct {
	Person  uuid.UUID
	Me, Any *bool
}

// TicketOrder is a list's fixed sort (docs/adr/0048 D6).
type TicketOrder int

// The two ticket orders.
const (
	// ByRank is a project's list: the ranked tickets by their key, then the
	// unranked — the terminal ones, and the open ones a release before the
	// rank filed — by number (docs/adr/0014 D1).
	ByRank TicketOrder = iota
	// NewestFirst is the tenant-wide list.
	NewestFirst
)

// rankedKey is the key a ticket is listed by in its project's rank: none
// while it is done or dropped. This release takes the key away with the
// state; the previous release leaves it in the column (docs/adr/0028 D3), and
// the list reads it as none.
const rankedKey = "(CASE WHEN t.state IN ('done', 'dropped') THEN NULL ELSE t.rank END)"

// Position is the cursor position after r in the order: the id (NewestFirst),
// or the key and the number, "<key>.<number>", the key empty for an unranked
// ticket, a done or dropped one included whatever its column holds (ByRank).
func (o TicketOrder) Position(r TicketRow) string {
	if o == NewestFirst {
		return r.ID.String()
	}
	var key string
	if r.Rank != nil && !r.State.Terminal() {
		key = *r.Rank
	}
	return key + "." + strconv.Itoa(int(r.Number))
}

// TicketPage selects a page: after a cursor's position with a limit, or a
// numbered page (docs/adr/0048 D1, D2).
type TicketPage struct {
	Order TicketOrder
	// After is the cursor's position (TicketOrder.Position); empty for the
	// first page.
	After string
	Limit int
	// Page and PerPage select a numbered page; Page 0 means cursor mode.
	Page    int
	PerPage int
}

// TicketList is a page of tickets; Total is set for a numbered page.
type TicketList struct {
	Rows  []TicketRow
	Total int64
}

// ListTickets renders a ticket list under the tenant and visibility
// predicates (docs/adr/0021 D4, docs/adr/0034 D4, docs/adr/0065 D4): the one
// place in the data layer that builds SQL at run time (docs/adr/0027 D4).
func (r *Reader) ListTickets(ctx context.Context, f TicketFilter, page TicketPage) (TicketList, error) {
	b := &queryBuilder{}
	b.where("t.tenant_id = " + b.arg(r.TenantID))
	b.where("app_ticket_visible(t.project_id, t.confidential, t.assignee_id, t.reporter_id)")
	b.filter(f)

	var out TicketList
	if page.Page > 0 {
		var err error
		if out.Total, err = r.countTickets(ctx, b); err != nil {
			return out, err
		}
	}
	sql := ticketSelect + "\n" + ticketFrom + "\nWHERE " + strings.Join(b.conds, "\n  AND ")
	switch {
	case page.Page > 0:
		sql += "\n" + orderBy(page.Order) + fmt.Sprintf("\nLIMIT %d OFFSET %d", page.PerPage, (page.Page-1)*page.PerPage)
	default:
		if page.After != "" {
			cond, err := b.after(page.Order, page.After)
			if err != nil {
				return out, err
			}
			sql += "\n  AND " + cond
		}
		sql += "\n" + orderBy(page.Order) + fmt.Sprintf("\nLIMIT %d", page.Limit)
	}
	rows, err := r.tx.Query(ctx, sql, b.args...)
	if err != nil {
		return out, fmt.Errorf("list tickets: %w", err)
	}
	out.Rows, err = pgx.CollectRows(rows, pgx.RowToStructByPos[TicketRow])
	if err != nil {
		return out, fmt.Errorf("list tickets: %w", err)
	}
	return out, nil
}

func (r *Reader) countTickets(ctx context.Context, b *queryBuilder) (int64, error) {
	var n int64
	sql := "SELECT count(*) FROM tickets t JOIN projects p ON p.tenant_id = t.tenant_id AND p.id = t.project_id\nWHERE " +
		strings.Join(b.conds, "\n  AND ")
	if err := r.tx.QueryRow(ctx, sql, b.args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("count tickets: %w", err)
	}
	return n, nil
}

func orderBy(o TicketOrder) string {
	if o == NewestFirst {
		return "ORDER BY t.id DESC"
	}
	return "ORDER BY " + rankedKey + " NULLS LAST, t.number"
}

// queryBuilder collects conditions and their positional arguments; values
// never enter the SQL text.
type queryBuilder struct {
	conds []string
	args  []any
}

func (b *queryBuilder) arg(v any) string {
	b.args = append(b.args, v)
	return "$" + strconv.Itoa(len(b.args))
}

func (b *queryBuilder) where(cond string) { b.conds = append(b.conds, cond) }

// after is the cursor's condition: the position the previous page ended at.
func (b *queryBuilder) after(o TicketOrder, after string) (string, error) {
	if o == NewestFirst {
		id, err := uuid.Parse(after)
		if err != nil {
			return "", fmt.Errorf("list tickets: bad cursor position: %w", err)
		}
		return "t.id < " + b.arg(id), nil
	}
	key, number, _ := strings.Cut(after, ".")
	n, err := strconv.Atoi(number)
	if err != nil {
		return "", fmt.Errorf("list tickets: bad cursor position: %w", err)
	}
	if key == "" {
		return "(" + rankedKey + " IS NULL AND t.number > " + b.arg(n) + ")", nil
	}
	if !domain.ValidRank(key) {
		return "", fmt.Errorf("list tickets: bad cursor position: %w", domain.ErrRankKey)
	}
	k := b.arg(key)
	return "(" + rankedKey + " > " + k + " OR (" + rankedKey + " = " + k + " AND t.number > " + b.arg(n) + ") OR " +
		rankedKey + " IS NULL)", nil
}

func (b *queryBuilder) filter(f TicketFilter) {
	if f.ProjectID != uuid.Nil {
		b.where("t.project_id = " + b.arg(f.ProjectID))
	}
	b.textSet("p.key", f.Projects)
	b.textSet("t.state::text", f.States)
	b.textSet("t.type::text", f.Types)
	b.textSet("t.severity::text", f.Severities)
	b.textSet("t.security::text", f.Securities)
	b.textSet("coalesce(t.urgency_override, t.urgency_derived)::text", f.Urgencies)
	b.textSet("t.effort::text", f.Efforts)
	b.personSet("t.assignee_id", f.Assignees)
	b.personSet("t.reporter_id", f.Reporters)
	b.personSet("t.parent_id", f.Parents)
	if !f.IncludeTerminal {
		var hidden []string
		for _, s := range []domain.TicketState{domain.StateDone, domain.StateDropped} {
			if !slices.Contains(f.States.In, string(s)) {
				hidden = append(hidden, string(s))
			}
		}
		if len(hidden) > 0 {
			b.where("NOT (t.state::text = ANY (" + b.arg(hidden) + "::text[]))")
		}
	}
	if f.ProgressMin != nil {
		b.where(effectiveProgress + " >= " + b.arg(*f.ProgressMin))
	}
	if f.ProgressMax != nil {
		b.where(effectiveProgress + " <= " + b.arg(*f.ProgressMax))
	}
	for _, c := range []struct {
		expr string
		at   *time.Time
	}{
		{"t.opened_at > ", f.OpenedAfter}, {"t.opened_at < ", f.OpenedBefore},
		{"t.updated_at > ", f.UpdatedAfter}, {"t.updated_at < ", f.UpdatedBefore},
	} {
		if c.at != nil {
			b.where(c.expr + b.arg(*c.at))
		}
	}
	if f.Query != "" {
		b.where("t.search @@ plainto_tsquery('cowork_simple', " + b.arg(f.Query) + ")")
	}
	b.exists(openBlocker, f.Blocked)
	b.exists(openQuestion, f.HasOpenQuestions)
	b.exists(anyInterest, f.Interest.Any)
	if f.Interest.Me != nil {
		b.exists(`EXISTS (SELECT 1 FROM ticket_interest mi
        WHERE mi.tenant_id = t.tenant_id AND mi.ticket_id = t.id AND mi.user_id = `+b.arg(f.Interest.Person)+`)`, f.Interest.Me)
	}
}

// anyInterest holds when anyone holds a stake in t.
const anyInterest = `EXISTS (SELECT 1 FROM ticket_interest ai WHERE ai.tenant_id = t.tenant_id AND ai.ticket_id = t.id)`

// exists adds cond, or its negation, when the filter is set.
func (b *queryBuilder) exists(cond string, set *bool) {
	if set == nil {
		return
	}
	if !*set {
		cond = "NOT " + cond
	}
	b.where(cond)
}

// effectiveProgress is the progress a ticket shows: 100 when done, the
// derived value while it has children, else its own (docs/adr/0017 D3, D5).
const effectiveProgress = "CASE WHEN t.state = 'done' THEN 100 ELSE coalesce(t.progress_derived, t.progress) END"

// openQuestion holds when t has an open question; a question is visible
// with its ticket.
const openQuestion = `EXISTS (SELECT 1 FROM questions oq
        WHERE oq.tenant_id = t.tenant_id AND oq.ticket_id = t.id AND oq.status = 'open')`

// openBlocker holds when an open ticket the caller can see blocks t.
const openBlocker = `EXISTS (SELECT 1 FROM ticket_links bl
        JOIN tickets bs ON bs.tenant_id = bl.tenant_id AND bs.id = bl.source_id
        WHERE bl.tenant_id = t.tenant_id AND bl.target_id = t.id AND bl.type = 'blocks'
          AND bs.state NOT IN ('done', 'dropped')
          AND app_ticket_visible(bs.project_id, bs.confidential, bs.assignee_id, bs.reporter_id))`

func (b *queryBuilder) textSet(expr string, s ValueSet) {
	if len(s.In) > 0 {
		b.where(expr + " = ANY (" + b.arg(s.In) + "::text[])")
	}
	if len(s.NotIn) > 0 {
		b.where("NOT (" + expr + " = ANY (" + b.arg(s.NotIn) + "::text[]))")
	}
}

func (b *queryBuilder) personSet(column string, s PersonSet) {
	var anyOf []string
	if len(s.In) > 0 {
		anyOf = append(anyOf, column+" = ANY ("+b.arg(s.In)+"::uuid[])")
	}
	if s.None {
		anyOf = append(anyOf, column+" IS NULL")
	}
	if len(anyOf) > 0 {
		b.where("(" + strings.Join(anyOf, " OR ") + ")")
	}
	if len(s.NotIn) > 0 {
		b.where("(" + column + " IS NULL OR NOT (" + column + " = ANY (" + b.arg(s.NotIn) + "::uuid[])))")
	}
	if s.NotNone {
		b.where(column + " IS NOT NULL")
	}
}
