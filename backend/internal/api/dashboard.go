package api

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/problem"
	"github.com/guided-traffic/cowork/backend/internal/store"
	"github.com/guided-traffic/cowork/backend/internal/store/readq"
)

// The dashboard's fixed definitions (docs/adr/0018 D6, docs/adr/0019 D2): the
// default period, the weeks of throughput, the days of lead time, and the open
// tickets updated last that the page lists beside the tiles. Each tile is
// defined in the API document, components/schemas.yaml#/Dashboard.
const (
	periodDays      = 30
	throughputWeeks = 8
	leadTimeDays    = 30
	recentTickets   = 8
)

// ageBounds are the lower bounds of the age buckets in days of 24 hours; a
// bucket ends where the next begins, and the last has no end.
var ageBounds = [...]int{0, 7, 30, 90, 365}

// severities are the severities in their order (docs/adr/0010 D1).
var severities = [...]apigen.Severity{apigen.SeverityCritical, apigen.SeverityHigh, apigen.SeverityMedium,
	apigen.SeverityLow, apigen.SeverityCosmetic}

// dashboardQuery is a dashboard request as the queries take it: the projects
// named and the ones left out — never nil, the queries read an empty array as
// no filter —, the period as UTC days, and the clock the ages are measured
// against.
type dashboardQuery struct {
	projects, without []string
	from, to          time.Time
	now               time.Time
}

// utcDay is the UTC day t falls on, at midnight.
func utcDay(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// parseDashboardQuery checks the filters (docs/adr/0049 D4): `project` as the
// ticket lists take it, and the period, by default the thirty days that end
// today.
func parseDashboardQuery(now time.Time, p apigen.GetDashboardParams) (dashboardQuery, *problem.Error) {
	q := dashboardQuery{projects: []string{}, without: []string{}, now: now}
	var errs []problem.FieldError
	for _, v := range deref(p.Project) {
		plain, negated := strings.CutPrefix(v, "!")
		switch {
		case !domain.ValidProjectKey(plain):
			errs = append(errs, problem.FieldError{Pointer: "query:project", Message: "not a project key: " + v})
		case negated:
			q.without = append(q.without, plain)
		default:
			q.projects = append(q.projects, plain)
		}
	}
	q.to = utcDay(now)
	if p.To != nil {
		q.to = utcDay(p.To.Time)
	}
	q.from = q.to.AddDate(0, 0, 1-periodDays)
	if p.From != nil {
		q.from = utcDay(p.From.Time)
	}
	if q.from.After(q.to) {
		errs = append(errs, problem.FieldError{Pointer: "query:from", Message: "after to, the period's last day"})
	}
	if len(errs) > 0 {
		return q, &problem.Error{Code: problem.ValidationFailed, Detail: "the dashboard request has values this route does not take", Errors: errs}
	}
	return q, nil
}

// end is the instant the period ends: midnight after its last day.
func (q dashboardQuery) end() time.Time { return q.to.AddDate(0, 0, 1) }

// weeks are the Mondays of throughput's ISO weeks, oldest first; the last is
// the week of the period's last day.
func (q dashboardQuery) weeks() []time.Time {
	last := isoMonday(q.to)
	out := make([]time.Time, throughputWeeks)
	for i := range out {
		out[i] = last.AddDate(0, 0, -7*(throughputWeeks-1-i))
	}
	return out
}

// isoMonday is the Monday of the ISO week a UTC day falls in.
func isoMonday(day time.Time) time.Time {
	sinceMonday := (int(day.Weekday()) + 6) % 7
	return day.AddDate(0, 0, -sinceMonday)
}

// leadTimeFrom is the first day of lead time's thirty days, which end with the
// period's last day.
func (q dashboardQuery) leadTimeFrom() time.Time { return q.to.AddDate(0, 0, 1-leadTimeDays) }

// ageCut is the instant a ticket filed at is exactly days old.
func (q dashboardQuery) ageCut(days int) time.Time {
	return q.now.Add(-time.Duration(days) * 24 * time.Hour)
}

// GetDashboard answers the tenant's dashboard (docs/adr/0018 D6): every tile
// read in one transaction under the visibility predicate, with a weak ETag.
func (s *Server) GetDashboard(ctx context.Context, req apigen.GetDashboardRequestObject) (apigen.GetDashboardResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, read); perr != nil {
		return nil, perr
	}
	q, perr := parseDashboardQuery(s.h.opts.Now(), req.Params)
	if perr != nil {
		return nil, perr
	}
	var rows dashboardRows
	if err := s.db.InTenant(ctx, t.ID, func(r *store.Reader) error { return rows.read(ctx, r, t.ID, q) }); err != nil {
		return nil, err
	}
	out := rows.view(t.Slug, q)
	tag, unchanged := listTag(req.Params.IfNoneMatch, out)
	if unchanged {
		return apigen.GetDashboard304Response{Headers: apigen.NotModifiedResponseHeaders{ETag: &tag}}, nil
	}
	return apigen.GetDashboard200JSONResponse{Body: out, Headers: apigen.GetDashboard200ResponseHeaders{ETag: &tag}}, nil
}

// dashboardRows are the tiles as the queries answer them.
type dashboardRows struct {
	byState    []readq.DashboardOpenByStateRow
	bySeverity []readq.DashboardOpenBySeverityRow
	security   []readq.DashboardSecurityRow
	blocked    []readq.DashboardBlockedRow
	age        readq.DashboardAgeRow
	throughput []readq.DashboardThroughputRow
	leadTime   readq.DashboardLeadTimeRow
	decisions  []readq.DashboardDecisionsRow
	time       []readq.DashboardTimeRow
	recent     []readq.DashboardRecentRow
}

// read runs the queries of every tile in the caller's transaction.
func (d *dashboardRows) read(ctx context.Context, r *store.Reader, tenant uuid.UUID, q dashboardQuery) error {
	in, out := q.projects, q.without
	var err error
	if d.byState, err = r.DashboardOpenByState(ctx, readq.DashboardOpenByStateParams{TenantID: tenant, Projects: in, WithoutProjects: out}); err != nil {
		return fmt.Errorf("read the open tickets by state: %w", err)
	}
	if d.bySeverity, err = r.DashboardOpenBySeverity(ctx, readq.DashboardOpenBySeverityParams{TenantID: tenant, Projects: in, WithoutProjects: out}); err != nil {
		return fmt.Errorf("read the open tickets by severity: %w", err)
	}
	if d.security, err = r.DashboardSecurity(ctx, readq.DashboardSecurityParams{TenantID: tenant, Projects: in, WithoutProjects: out}); err != nil {
		return fmt.Errorf("read the open security findings: %w", err)
	}
	if d.blocked, err = r.DashboardBlocked(ctx, readq.DashboardBlockedParams{TenantID: tenant, Projects: in, WithoutProjects: out}); err != nil {
		return fmt.Errorf("read the blocked tickets: %w", err)
	}
	if d.age, err = r.DashboardAge(ctx, readq.DashboardAgeParams{Cut7: q.ageCut(ageBounds[1]), Cut30: q.ageCut(ageBounds[2]),
		Cut90: q.ageCut(ageBounds[3]), Cut365: q.ageCut(ageBounds[4]), TenantID: tenant, Projects: in, WithoutProjects: out}); err != nil {
		return fmt.Errorf("read the age of the open tickets: %w", err)
	}
	if d.throughput, err = r.DashboardThroughput(ctx, readq.DashboardThroughputParams{TenantID: tenant, Since: q.weeks()[0], Until: q.end(),
		Projects: in, WithoutProjects: out}); err != nil {
		return fmt.Errorf("read the throughput: %w", err)
	}
	if d.leadTime, err = r.DashboardLeadTime(ctx, readq.DashboardLeadTimeParams{TenantID: tenant, Since: q.leadTimeFrom(), Until: q.end(),
		Projects: in, WithoutProjects: out}); err != nil {
		return fmt.Errorf("read the lead time: %w", err)
	}
	if d.decisions, err = r.DashboardDecisions(ctx, readq.DashboardDecisionsParams{TenantID: tenant, Projects: in, WithoutProjects: out}); err != nil {
		return fmt.Errorf("read the open decisions: %w", err)
	}
	if d.time, err = r.DashboardTime(ctx, readq.DashboardTimeParams{TenantID: tenant, FromDay: q.from, ToDay: q.to, Projects: in, WithoutProjects: out}); err != nil {
		return fmt.Errorf("read the time booked: %w", err)
	}
	if d.recent, err = r.DashboardRecent(ctx, readq.DashboardRecentParams{TenantID: tenant, Projects: in, WithoutProjects: out, PageSize: recentTickets}); err != nil {
		return fmt.Errorf("read the tickets updated last: %w", err)
	}
	return nil
}

// view is the answer of the tiles' rows.
func (d dashboardRows) view(slug string, q dashboardQuery) apigen.Dashboard {
	return apigen.Dashboard{
		Period:         apigen.DashboardPeriod{From: day(q.from), To: day(q.to)},
		OpenByState:    stateCounts(d.byState),
		OpenBySeverity: severityCounts(d.bySeverity),
		Security:       securityTile(slug, d.security),
		Blocked:        blockedTile(slug, d.blocked),
		Age:            ageTile(d.age),
		Throughput:     throughputTile(q, d.throughput),
		LeadTime:       leadTimeTile(q, d.leadTime),
		Decisions:      decisionsTile(slug, d.decisions),
		Time:           timeTile(d.time),
		Recent:         recentList(slug, d.recent),
	}
}

func day(t time.Time) openapi_types.Date { return openapi_types.Date{Time: t} }

// stateCounts is tile 1: a row per project and open state that has a ticket.
func stateCounts(rows []readq.DashboardOpenByStateRow) []apigen.DashboardStateCount {
	out := make([]apigen.DashboardStateCount, 0, len(rows))
	for _, r := range rows {
		out = append(out, apigen.DashboardStateCount{Project: r.ProjectKey, State: apigen.TicketState(r.State), Count: int(r.Tickets)})
	}
	return out
}

// severityCounts is tile 2: every severity in its order, zero included.
func severityCounts(rows []readq.DashboardOpenBySeverityRow) []apigen.DashboardSeverityCount {
	counted := map[apigen.Severity]int{}
	for _, r := range rows {
		counted[apigen.Severity(r.Severity)] = int(r.Tickets)
	}
	out := make([]apigen.DashboardSeverityCount, 0, len(severities))
	for _, s := range severities {
		out = append(out, apigen.DashboardSeverityCount{Severity: s, Count: counted[s]})
	}
	return out
}

// securityTile is tile 3: live, then boundary, each with its oldest or null.
func securityTile(slug string, rows []readq.DashboardSecurityRow) []apigen.DashboardSecurity {
	out := []apigen.DashboardSecurity{
		{Class: apigen.SecurityClassLive, Oldest: nullable.NewNullNullable[apigen.DashboardTicket]()},
		{Class: apigen.SecurityClassBoundary, Oldest: nullable.NewNullNullable[apigen.DashboardTicket]()},
	}
	for _, r := range rows {
		for i := range out {
			if string(out[i].Class) == string(r.Security) {
				out[i].Count = int(r.Tickets)
				out[i].Oldest = nullable.NewNullableWithValue(apigen.DashboardTicket{
					Key: domain.FullKey(slug, r.ProjectKey, r.Number), Title: r.Title, Since: r.OpenedAt})
			}
		}
	}
	return out
}

// blockedTile is tile 4: the count and the ticket blocked longest, or null.
func blockedTile(slug string, rows []readq.DashboardBlockedRow) apigen.DashboardBlocked {
	out := apigen.DashboardBlocked{Oldest: nullable.NewNullNullable[apigen.DashboardBlock]()}
	if len(rows) == 0 {
		return out
	}
	r := rows[0]
	out.Count = int(r.Tickets)
	out.Oldest = nullable.NewNullableWithValue(apigen.DashboardBlock{Key: domain.FullKey(slug, r.ProjectKey, r.Number),
		Title: r.Title, Since: r.Since, Kind: apigen.BlockKind(deref(r.BlockKind))})
	return out
}

// ageTile is tile 5: the five buckets in their order.
func ageTile(r readq.DashboardAgeRow) []apigen.DashboardAgeBucket {
	counts := [len(ageBounds)]int64{r.Under7, r.Under30, r.Under90, r.Under365, r.Older}
	out := make([]apigen.DashboardAgeBucket, len(ageBounds))
	for i, from := range ageBounds {
		out[i] = apigen.DashboardAgeBucket{FromDays: from, Count: int(counts[i]), ToDays: nullable.NewNullNullable[int]()}
		if i+1 < len(ageBounds) {
			out[i].ToDays = nullable.NewNullableWithValue(ageBounds[i+1])
		}
	}
	return out
}

// throughputTile is tile 6: every week, oldest first, the last cut at the
// period's last day.
func throughputTile(q dashboardQuery, rows []readq.DashboardThroughputRow) []apigen.DashboardWeek {
	done := map[time.Time]int{}
	for _, r := range rows {
		done[utcDay(r.Week)] = int(r.Tickets)
	}
	weeks := q.weeks()
	out := make([]apigen.DashboardWeek, 0, len(weeks))
	for _, monday := range weeks {
		sunday := monday.AddDate(0, 0, 6)
		if sunday.After(q.to) {
			sunday = q.to
		}
		year, week := monday.ISOWeek()
		out = append(out, apigen.DashboardWeek{Week: fmt.Sprintf("%04d-W%02d", year, week), From: day(monday), To: day(sunday),
			Done: done[monday]})
	}
	return out
}

// leadTimeTile is tile 7: the median in whole seconds, null without a ticket.
func leadTimeTile(q dashboardQuery, r readq.DashboardLeadTimeRow) apigen.DashboardLeadTime {
	out := apigen.DashboardLeadTime{From: day(q.leadTimeFrom()), To: day(q.to), Done: int(r.Tickets),
		MedianSeconds: nullable.NewNullNullable[int]()}
	if r.Tickets > 0 {
		out.MedianSeconds = nullable.NewNullableWithValue(int(math.Round(r.MedianSeconds)))
	}
	return out
}

// decisionsTile is tile 8: the count and the question asked first, or null.
func decisionsTile(slug string, rows []readq.DashboardDecisionsRow) apigen.DashboardDecisions {
	out := apigen.DashboardDecisions{Oldest: nullable.NewNullNullable[apigen.DashboardQuestion]()}
	if len(rows) == 0 {
		return out
	}
	r := rows[0]
	out.Count = int(r.Questions)
	out.Oldest = nullable.NewNullableWithValue(apigen.DashboardQuestion{Key: domain.FullKey(slug, r.ProjectKey, r.TicketNumber),
		Title: r.TicketTitle, Number: int(r.QuestionNumber), Question: r.Question, Since: r.CreatedAt})
	return out
}

// timeTile is tile 9: the minutes per project that has any, and their sum.
func timeTile(rows []readq.DashboardTimeRow) apigen.DashboardTime {
	out := apigen.DashboardTime{Projects: make([]apigen.DashboardProjectTime, 0, len(rows))}
	for _, r := range rows {
		out.Projects = append(out.Projects, apigen.DashboardProjectTime{Project: r.ProjectKey, Name: r.ProjectName, Minutes: int(r.Minutes)})
		out.TotalMinutes += int(r.Minutes)
	}
	return out
}

// recentList is the open tickets updated last, beside the tiles.
func recentList(slug string, rows []readq.DashboardRecentRow) []apigen.DashboardRecent {
	out := make([]apigen.DashboardRecent, 0, len(rows))
	for _, r := range rows {
		out = append(out, apigen.DashboardRecent{Key: domain.FullKey(slug, r.ProjectKey, r.Number), Type: apigen.TicketType(r.Type),
			Title: r.Title, State: apigen.TicketState(r.State), UpdatedAt: r.UpdatedAt})
	}
	return out
}
