//go:build integration

package integration

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api"
	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/test/fixture"
)

// dashboardClock is the server's clock in the dashboard tests: a Wednesday at
// noon, UTC, so the default period is 8 September to 7 October 2026 and the
// last week of throughput is the one of Monday 5 October.
var dashboardClock = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

// dashboardEnv is the world with the server's clock fixed and a project HIDDEN
// in tenant A, restricted, the member not on its list. Every tile is read as
// the administrator, who sees everything, and as the member, who sees neither
// HIDDEN nor a confidential ticket the administrator reported: what the member
// reads must be what the tile would be without those tickets (docs/adr/0034 D4,
// docs/adr/0065 D4).
type dashboardEnv struct {
	ticketEnv
	f      *fixture.DB
	hidden uuid.UUID
}

func newDashboardEnv(t *testing.T) dashboardEnv {
	t.Helper()
	w := newWorld(t)
	s := newAPI(t, func(o *api.Options) { o.Now = func() time.Time { return dashboardClock } })
	e := dashboardEnv{ticketEnv: ticketEnv{world: w, tk: issueTokens(t, w), s: s, ctx: context.Background()}, f: fixtures(t)}
	var err error
	e.hidden, err = e.f.Project(e.ctx, e.A, "HIDDEN", "Hidden")
	require.NoError(t, err)
	e.exec(t, "UPDATE projects SET restricted = true WHERE id = $1", e.hidden)
	return e
}

func (e dashboardEnv) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	require.NoError(t, e.f.Exec(e.ctx, sql, args...))
}

// ago is the clock less n days of 24 hours.
func ago(n float64) time.Time { return dashboardClock.Add(-time.Duration(n * float64(24*time.Hour))) }

// ticket files a task in a project of tenant A, reported by the administrator,
// filed and last updated at the time given.
func (e dashboardEnv) ticket(t *testing.T, project uuid.UUID, title string, opened time.Time) uuid.UUID {
	t.Helper()
	id, _, err := e.f.Ticket(e.ctx, e.A, project, e.AdminA, title)
	require.NoError(t, err)
	e.exec(t, "UPDATE tickets SET opened_at = $2, updated_at = $2 WHERE id = $1", id, opened)
	return id
}

// hide makes a ticket confidential: its reporter, the administrator, still
// sees it; the member does not.
func (e dashboardEnv) hide(t *testing.T, id uuid.UUID) {
	t.Helper()
	e.exec(t, "UPDATE tickets SET confidential = true WHERE id = $1", id)
}

func (e dashboardEnv) done(t *testing.T, id uuid.UUID, at time.Time) {
	t.Helper()
	e.exec(t, "UPDATE tickets SET state = 'done', done_at = $2, done_from = 'in-progress', done_by_hand = true WHERE id = $1", id, at)
}

func (e dashboardEnv) state(t *testing.T, id uuid.UUID, state string) {
	t.Helper()
	e.exec(t, "UPDATE tickets SET state = $2 WHERE id = $1", id, state)
}

// dashboard reads the dashboard of tenant A as c with a raw query string.
func (e dashboardEnv) dashboard(t *testing.T, c caller, query string) apigen.Dashboard {
	t.Helper()
	res := e.s.do(t, c, http.MethodGet, "/api/v1/tenants/"+e.SlugA+"/dashboard"+query, nil)
	require.Equal(t, http.StatusOK, res.StatusCode)
	assert.Regexp(t, `^W/"[0-9a-f]{24}"$`, res.Header.Get("ETag"))
	return decode[apigen.Dashboard](t, res)
}

func (e dashboardEnv) admin() caller  { return caller{Token: e.tk.AdminA} }
func (e dashboardEnv) member() caller { return caller{Token: e.tk.MemberA} }

// stateCounts reads tile 1 as project/state → count.
func stateCounts(d apigen.Dashboard) map[string]int {
	out := map[string]int{}
	for _, c := range d.OpenByState {
		out[c.Project+"/"+string(c.State)] = c.Count
	}
	return out
}

// Tile 1 (docs/adr/0018 D6): the open tickets per project and state — done and
// dropped left out, another tenant's never there, an archived project only when
// the filter names it; a confidential ticket and a restricted project count only
// for whoever sees them, and a key the caller cannot see counts like one that
// names nothing.
func TestDashboardOpenByState(t *testing.T) {
	e := newDashboardEnv(t)
	e.ticket(t, e.ProjectA, "filed", ago(1))
	e.hide(t, e.ticket(t, e.ProjectA, "filed, confidential", ago(1)))
	e.state(t, e.ticket(t, e.ProjectA, "in progress", ago(1)), "in-progress")
	e.state(t, e.ticket(t, e.ProjectA, "review", ago(1)), "review")
	e.done(t, e.ticket(t, e.ProjectA, "done", ago(3)), ago(1))
	e.state(t, e.ticket(t, e.ProjectA, "dropped", ago(1)), "dropped")
	e.ticket(t, e.hidden, "hidden", ago(1))
	gamma, err := e.f.Project(e.ctx, e.A, "GAMMA", "Gamma")
	require.NoError(t, err)
	e.ticket(t, gamma, "archived", ago(1))
	e.exec(t, "UPDATE projects SET archived_at = now() WHERE id = $1", gamma)
	_, _, err = e.f.Ticket(e.ctx, e.B, e.ProjectB, e.MemberB, "another tenant's")
	require.NoError(t, err)

	member := e.dashboard(t, e.member(), "")
	assert.Equal(t, []apigen.DashboardStateCount{
		{Project: "ALPHA", State: apigen.TicketStateFiled, Count: 1},
		{Project: "ALPHA", State: apigen.TicketStateInProgress, Count: 1},
		{Project: "ALPHA", State: apigen.TicketStateReview, Count: 1},
	}, member.OpenByState, "by project, the states in their order")

	admin := e.dashboard(t, e.admin(), "")
	assert.Equal(t, map[string]int{"ALPHA/filed": 2, "ALPHA/in-progress": 1, "ALPHA/review": 1, "HIDDEN/filed": 1}, stateCounts(admin))

	assert.Equal(t, map[string]int{"GAMMA/filed": 1}, stateCounts(e.dashboard(t, e.admin(), "?project=GAMMA")),
		"an archived project counts when the filter names it")
	assert.Equal(t, map[string]int{"HIDDEN/filed": 1}, stateCounts(e.dashboard(t, e.admin(), "?project=!ALPHA")))
	assert.Equal(t, map[string]int{"ALPHA/filed": 2, "ALPHA/in-progress": 1, "ALPHA/review": 1, "HIDDEN/filed": 1},
		stateCounts(e.dashboard(t, e.admin(), "?project=ALPHA&project=HIDDEN")))

	hidden := e.dashboard(t, e.member(), "?project=HIDDEN")
	nothing := e.dashboard(t, e.member(), "?project=NOSUCH")
	assert.Empty(t, hidden.OpenByState)
	assert.Equal(t, nothing, hidden, "a project the member cannot see answers like one that does not exist")
}

// Tile 2: every severity of the open tickets, zero included.
func TestDashboardOpenBySeverity(t *testing.T) {
	e := newDashboardEnv(t)
	set := func(id uuid.UUID, severity string) {
		e.exec(t, "UPDATE tickets SET severity = $2 WHERE id = $1", id, severity)
	}
	critical := e.ticket(t, e.ProjectA, "critical, confidential", ago(1))
	set(critical, "critical")
	e.hide(t, critical)
	set(e.ticket(t, e.ProjectA, "high", ago(1)), "high")
	e.ticket(t, e.ProjectA, "medium", ago(1))
	closed := e.ticket(t, e.ProjectA, "high, done", ago(1))
	set(closed, "high")
	e.done(t, closed, ago(0.5))
	set(e.ticket(t, e.hidden, "low, hidden", ago(1)), "low")

	counts := func(d apigen.Dashboard) []int {
		out := make([]int, 0, len(d.OpenBySeverity))
		for _, s := range d.OpenBySeverity {
			out = append(out, s.Count)
		}
		return out
	}
	member := e.dashboard(t, e.member(), "")
	assert.Equal(t, []apigen.Severity{apigen.SeverityCritical, apigen.SeverityHigh, apigen.SeverityMedium, apigen.SeverityLow,
		apigen.SeverityCosmetic}, []apigen.Severity{member.OpenBySeverity[0].Severity, member.OpenBySeverity[1].Severity,
		member.OpenBySeverity[2].Severity, member.OpenBySeverity[3].Severity, member.OpenBySeverity[4].Severity})
	assert.Equal(t, []int{0, 1, 1, 0, 0}, counts(member))
	assert.Equal(t, []int{1, 1, 1, 1, 0}, counts(e.dashboard(t, e.admin(), "")))
}

// Tile 3: the open live and boundary findings, each with the oldest by filing
// named — never one the caller cannot see.
func TestDashboardSecurity(t *testing.T) {
	e := newDashboardEnv(t)
	finding := func(project uuid.UUID, title, class string, opened time.Time) uuid.UUID {
		id := e.ticket(t, project, title, opened)
		e.exec(t, "UPDATE tickets SET security = $2, threat = 'a principal reads what it should not' WHERE id = $1", id, class)
		return id
	}
	e.hide(t, finding(e.ProjectA, "live, confidential", "live", ago(10)))
	finding(e.ProjectA, "live, open to members", "live", ago(5))
	e.done(t, finding(e.ProjectA, "live, done", "live", ago(30)), ago(1))
	finding(e.hidden, "boundary, hidden", "boundary", ago(20))
	finding(e.ProjectA, "boundary", "boundary", ago(3))
	finding(e.ProjectA, "hardening", "hardening", ago(40))

	oldest := func(s apigen.DashboardSecurity) string { return s.Oldest.MustGet().Title }
	member := e.dashboard(t, e.member(), "")
	require.Len(t, member.Security, 2)
	assert.Equal(t, apigen.SecurityClassLive, member.Security[0].Class)
	assert.Equal(t, 1, member.Security[0].Count)
	assert.Equal(t, "live, open to members", oldest(member.Security[0]))
	assert.Equal(t, e.SlugA+"/ALPHA-2", member.Security[0].Oldest.MustGet().Key)
	assert.True(t, ago(5).Equal(member.Security[0].Oldest.MustGet().Since), "since its filing")
	assert.Equal(t, apigen.SecurityClassBoundary, member.Security[1].Class)
	assert.Equal(t, 1, member.Security[1].Count)
	assert.Equal(t, "boundary", oldest(member.Security[1]))

	admin := e.dashboard(t, e.admin(), "")
	assert.Equal(t, 2, admin.Security[0].Count)
	assert.Equal(t, "live, confidential", oldest(admin.Security[0]))
	assert.Equal(t, 2, admin.Security[1].Count)
	assert.Equal(t, "boundary, hidden", oldest(admin.Security[1]))

	none := e.dashboard(t, e.member(), "?project=HIDDEN")
	assert.Zero(t, none.Security[1].Count)
	assert.True(t, none.Security[1].Oldest.IsNull())
}

// Tile 4: the blocked tickets and the one blocked longest with its kind —
// blocked since its latest act into blocked, or its last update without one.
func TestDashboardBlocked(t *testing.T) {
	e := newDashboardEnv(t)
	block := func(project uuid.UUID, title, kind string) uuid.UUID {
		id := e.ticket(t, project, title, ago(60))
		e.exec(t, "UPDATE tickets SET state = 'blocked', blocked_from = 'filed', block_kind = $2, block_reason = 'it waits' WHERE id = $1",
			id, kind)
		return id
	}
	act := func(id uuid.UUID, state string, at time.Time) {
		e.exec(t, `INSERT INTO audit_events (tenant_id, actor_user_id, entity_type, entity_id, ticket_id, action, before, after, created_at)
			VALUES ($1, $2, 'ticket', $3, $3, 'transitioned', '{"state":"filed"}', jsonb_build_object('state', $4::text), $5)`,
			e.A, e.AdminA, id, state, at)
	}
	again := block(e.ProjectA, "blocked again", "human")
	act(again, "blocked", ago(30))
	act(again, "filed", ago(20))
	act(again, "blocked", ago(4))
	unrecorded := block(e.ProjectA, "blocked without an act", "product")
	e.exec(t, "UPDATE tickets SET updated_at = $2 WHERE id = $1", unrecorded, ago(6))
	confidential := block(e.ProjectA, "blocked, confidential", "decision")
	act(confidential, "blocked", ago(9))
	e.hide(t, confidential)
	hidden := block(e.hidden, "blocked, hidden", "external")
	act(hidden, "blocked", ago(12))
	e.ticket(t, e.ProjectA, "not blocked", ago(90))

	member := e.dashboard(t, e.member(), "")
	assert.Equal(t, 2, member.Blocked.Count)
	oldest := member.Blocked.Oldest.MustGet()
	assert.Equal(t, "blocked without an act", oldest.Title, "the latest act into blocked counts, not the first")
	assert.Equal(t, apigen.BlockKindProduct, oldest.Kind)
	assert.True(t, ago(6).Equal(oldest.Since), "its last update stands in for the act")

	admin := e.dashboard(t, e.admin(), "")
	assert.Equal(t, 4, admin.Blocked.Count)
	assert.Equal(t, "blocked, hidden", admin.Blocked.Oldest.MustGet().Title)
	assert.Equal(t, apigen.BlockKindExternal, admin.Blocked.Oldest.MustGet().Kind)
	assert.True(t, ago(12).Equal(admin.Blocked.Oldest.MustGet().Since))

	onlyConfidential := e.dashboard(t, e.admin(), "?project=ALPHA")
	assert.Equal(t, "blocked, confidential", onlyConfidential.Blocked.Oldest.MustGet().Title)
	none := e.dashboard(t, e.member(), "?project=HIDDEN")
	assert.Zero(t, none.Blocked.Count)
	assert.True(t, none.Blocked.Oldest.IsNull())
}

// Tile 5: the open tickets by their age at the request in five buckets; a
// ticket exactly seven days old is in the second.
func TestDashboardAge(t *testing.T) {
	e := newDashboardEnv(t)
	for _, days := range []float64{1, 7, 10, 45, 100, 400} {
		e.ticket(t, e.ProjectA, "open", ago(days))
	}
	e.hide(t, e.ticket(t, e.ProjectA, "confidential", ago(2)))
	e.ticket(t, e.hidden, "hidden", ago(500))
	e.done(t, e.ticket(t, e.ProjectA, "done", ago(3)), ago(1))

	counts := func(d apigen.Dashboard) []int {
		out := make([]int, 0, len(d.Age))
		for _, b := range d.Age {
			out = append(out, b.Count)
		}
		return out
	}
	member := e.dashboard(t, e.member(), "")
	assert.Equal(t, []int{1, 2, 1, 1, 1}, counts(member))
	assert.Equal(t, 0, member.Age[0].FromDays)
	assert.Equal(t, 7, member.Age[0].ToDays.MustGet())
	assert.True(t, member.Age[4].ToDays.IsNull())
	assert.Equal(t, []int{2, 2, 1, 1, 2}, counts(e.dashboard(t, e.admin(), "")))
}

// Tile 6: done per ISO week for the eight weeks that end with the one of the
// period's last day — a Monday at midnight starts its week, the last week ends
// with the period, a ticket reopened counts nowhere.
func TestDashboardThroughput(t *testing.T) {
	e := newDashboardEnv(t)
	doneAt := func(project uuid.UUID, title string, at time.Time) uuid.UUID {
		id := e.ticket(t, project, title, at.Add(-48*time.Hour))
		e.done(t, id, at)
		return id
	}
	doneAt(e.ProjectA, "Tuesday of the last week", time.Date(2026, 10, 6, 10, 0, 0, 0, time.UTC))
	doneAt(e.ProjectA, "Monday at midnight", time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC))
	doneAt(e.ProjectA, "Sunday before", time.Date(2026, 10, 4, 23, 59, 59, 0, time.UTC))
	doneAt(e.ProjectA, "the first week's Monday", time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC))
	doneAt(e.ProjectA, "before the first week", time.Date(2026, 8, 16, 23, 0, 0, 0, time.UTC))
	e.hide(t, doneAt(e.ProjectA, "confidential", time.Date(2026, 9, 15, 9, 0, 0, 0, time.UTC)))
	doneAt(e.hidden, "hidden", time.Date(2026, 9, 16, 9, 0, 0, 0, time.UTC))
	reopened := doneAt(e.ProjectA, "reopened", time.Date(2026, 9, 30, 9, 0, 0, 0, time.UTC))
	e.exec(t, "UPDATE tickets SET state = 'in-progress', done_at = NULL, done_from = NULL, done_by_hand = false WHERE id = $1", reopened)
	doneAt(e.ProjectA, "after the period", time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC))

	done := func(d apigen.Dashboard) map[string]int {
		out := map[string]int{}
		for _, w := range d.Throughput {
			if w.Done > 0 {
				out[w.Week] = w.Done
			}
		}
		return out
	}
	member := e.dashboard(t, e.member(), "")
	require.Len(t, member.Throughput, 8)
	assert.Equal(t, "2026-W34", member.Throughput[0].Week)
	assert.Equal(t, "2026-10-05", member.Throughput[7].From.String())
	assert.Equal(t, "2026-10-07", member.Throughput[7].To.String(), "the last week ends with the period")
	assert.Equal(t, map[string]int{"2026-W34": 1, "2026-W40": 2, "2026-W41": 2}, done(member))
	assert.Equal(t, map[string]int{"2026-W34": 1, "2026-W38": 2, "2026-W40": 2, "2026-W41": 2}, done(e.dashboard(t, e.admin(), "")))

	september := e.dashboard(t, e.member(), "?to=2026-09-30")
	assert.Equal(t, "2026-W40", september.Throughput[7].Week)
	assert.Equal(t, "2026-09-30", september.Throughput[7].To.String())
	assert.Equal(t, 0, september.Throughput[7].Done, "a ticket done after the period's last day counts nowhere, though its week is there")
}

// Tile 7: the median from filing to done of the tickets done in the thirty
// days that end with the period's last day; the median of an even count is
// the mean of the middle two.
func TestDashboardLeadTime(t *testing.T) {
	e := newDashboardEnv(t)
	measured := func(project uuid.UUID, title string, done time.Time, days float64) uuid.UUID {
		id := e.ticket(t, project, title, done.Add(-time.Duration(days*float64(24*time.Hour))))
		e.done(t, id, done)
		return id
	}
	windowStart := time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)
	measured(e.ProjectA, "one day, at the window's start", windowStart, 1)
	measured(e.ProjectA, "three days", ago(2), 3)
	measured(e.ProjectA, "ten days", ago(1), 10)
	measured(e.ProjectA, "before the window", windowStart.Add(-time.Second), 100)
	e.hide(t, measured(e.ProjectA, "thirty days, confidential", ago(3), 30))
	measured(e.hidden, "forty days, hidden", ago(4), 40)
	e.ticket(t, e.ProjectA, "open", ago(50))

	member := e.dashboard(t, e.member(), "")
	assert.Equal(t, "2026-09-08", member.LeadTime.From.String())
	assert.Equal(t, "2026-10-07", member.LeadTime.To.String())
	assert.Equal(t, 3, member.LeadTime.Done)
	assert.Equal(t, 3*86400, member.LeadTime.MedianSeconds.MustGet())

	admin := e.dashboard(t, e.admin(), "")
	assert.Equal(t, 5, admin.LeadTime.Done)
	assert.Equal(t, 10*86400, admin.LeadTime.MedianSeconds.MustGet())

	even := e.dashboard(t, e.admin(), "?project=!HIDDEN")
	assert.Equal(t, 4, even.LeadTime.Done)
	assert.Equal(t, int(6.5*86400), even.LeadTime.MedianSeconds.MustGet())

	none := e.dashboard(t, e.member(), "?project=HIDDEN")
	assert.Zero(t, none.LeadTime.Done)
	assert.True(t, none.LeadTime.MedianSeconds.IsNull())
}

// Tile 8: the open questions, whomever asked and whatever the ticket's state,
// and the one asked first; an answered or withdrawn one is no open decision.
func TestDashboardDecisions(t *testing.T) {
	e := newDashboardEnv(t)
	number := 0
	ask := func(ticket uuid.UUID, text string, at time.Time, extra string, args ...any) {
		number++
		e.exec(t, "INSERT INTO questions (tenant_id, ticket_id, number, question, asked_by, created_at"+extra,
			append([]any{e.A, ticket, number, text, e.MemberA, at}, args...)...)
	}
	open := ") VALUES ($1, $2, $3, $4, $5, $6)"
	alpha := e.ticket(t, e.ProjectA, "a decision", ago(40))
	ask(alpha, "open, the member's oldest", ago(5), open)
	ask(alpha, "answered", ago(20), ", status, answer, answered_by, answered_at) VALUES ($1, $2, $3, $4, $5, $6, 'answered', 'yes', $5, $6)")
	ask(alpha, "withdrawn", ago(25), ", status, withdrawn_by, withdrawn_at) VALUES ($1, $2, $3, $4, $5, $6, 'withdrawn', $5, $6)")
	closed := e.ticket(t, e.ProjectA, "done with a question open", ago(40))
	e.done(t, closed, ago(2))
	ask(closed, "open on a done ticket", ago(3), open)
	confidential := e.ticket(t, e.ProjectA, "confidential", ago(40))
	e.hide(t, confidential)
	ask(confidential, "open, confidential", ago(10), open)
	ask(e.ticket(t, e.hidden, "hidden", ago(40)), "open, hidden", ago(15), open)

	member := e.dashboard(t, e.member(), "")
	assert.Equal(t, 2, member.Decisions.Count)
	oldest := member.Decisions.Oldest.MustGet()
	assert.Equal(t, apigen.DashboardQuestion{Key: e.SlugA + "/ALPHA-1", Title: "a decision", Number: 1,
		Question: "open, the member's oldest", Since: oldest.Since}, oldest)
	assert.True(t, ago(5).Equal(oldest.Since))

	admin := e.dashboard(t, e.admin(), "")
	assert.Equal(t, 4, admin.Decisions.Count)
	assert.Equal(t, "open, hidden", admin.Decisions.Oldest.MustGet().Question)

	none := e.dashboard(t, e.member(), "?project=HIDDEN")
	assert.Zero(t, none.Decisions.Count)
	assert.True(t, none.Decisions.Oldest.IsNull())
}

// Tile 9: the minutes booked on the days of the period per project, voided
// entries left out, each entry under the visibility of time and of its ticket.
func TestDashboardTime(t *testing.T) {
	e := newDashboardEnv(t)
	alpha := e.ticket(t, e.ProjectA, "worked on", ago(60))
	confidential := e.ticket(t, e.ProjectA, "confidential", ago(60))
	e.hide(t, confidential)
	hidden := e.ticket(t, e.hidden, "hidden", ago(60))
	book := func(ticket, person uuid.UUID, minutes int, day string) uuid.UUID {
		var id uuid.UUID
		require.NoError(t, e.f.QueryRow(e.ctx, `INSERT INTO time_entries (tenant_id, ticket_id, person_id, author_id, minutes, day)
			VALUES ($1, $2, $3, $3, $4, $5) RETURNING id`, e.A, ticket, person, minutes, day).Scan(&id))
		return id
	}
	book(alpha, e.MemberA, 60, "2026-10-01")
	book(alpha, e.MemberA, 10, "2026-09-08")
	book(alpha, e.MemberA, 5, "2026-10-07")
	book(alpha, e.AdminA, 30, "2026-10-02")
	voided := book(alpha, e.AdminA, 45, "2026-10-02")
	e.exec(t, "UPDATE time_entries SET voided_by = $2, voided_at = now() WHERE id = $1", voided, e.AdminA)
	book(alpha, e.AdminA, 90, "2026-09-07")
	book(confidential, e.AdminA, 20, "2026-10-03")
	book(hidden, e.AdminA, 50, "2026-10-03")

	member := e.dashboard(t, e.member(), "")
	assert.Equal(t, apigen.DashboardTime{TotalMinutes: 75, Projects: []apigen.DashboardProjectTime{{Project: "ALPHA", Name: "Alpha", Minutes: 75}}},
		member.Time, "the member's own: the tenant shows nobody else's time to members")

	e.exec(t, "UPDATE tenants SET time_visible_to_members = true WHERE id = $1", e.A)
	member = e.dashboard(t, e.member(), "")
	assert.Equal(t, apigen.DashboardTime{TotalMinutes: 105, Projects: []apigen.DashboardProjectTime{{Project: "ALPHA", Name: "Alpha", Minutes: 105}}},
		member.Time, "everyone's on the tickets the member sees")

	admin := e.dashboard(t, e.admin(), "")
	assert.Equal(t, apigen.DashboardTime{TotalMinutes: 175, Projects: []apigen.DashboardProjectTime{
		{Project: "ALPHA", Name: "Alpha", Minutes: 125}, {Project: "HIDDEN", Name: "Hidden", Minutes: 50}}}, admin.Time)

	september := e.dashboard(t, e.admin(), "?from=2026-09-01&to=2026-09-30")
	assert.Equal(t, 100, september.Time.TotalMinutes, "the period's days, both ends inclusive")
}

// Beside the tiles: the eight open tickets updated last, newest first.
func TestDashboardRecent(t *testing.T) {
	e := newDashboardEnv(t)
	for i := range 9 {
		e.ticket(t, e.ProjectA, "open "+string(rune('a'+i)), ago(float64(20-i)))
	}
	e.hide(t, e.ticket(t, e.ProjectA, "confidential", ago(0.5)))
	e.ticket(t, e.hidden, "hidden", ago(0.4))
	e.done(t, e.ticket(t, e.ProjectA, "done", ago(0.1)), ago(0.1))

	titles := func(d apigen.Dashboard) []string {
		out := make([]string, 0, len(d.Recent))
		for _, r := range d.Recent {
			out = append(out, r.Title)
		}
		return out
	}
	member := e.dashboard(t, e.member(), "")
	assert.Equal(t, []string{"open i", "open h", "open g", "open f", "open e", "open d", "open c", "open b"}, titles(member))
	assert.Equal(t, apigen.TicketTypeTask, member.Recent[0].Type)
	assert.Equal(t, apigen.TicketStateFiled, member.Recent[0].State)
	assert.Equal(t, e.SlugA+"/ALPHA-9", member.Recent[0].Key)
	assert.Equal(t, []string{"hidden", "confidential", "open i", "open h", "open g", "open f", "open e", "open d"},
		titles(e.dashboard(t, e.admin(), "")))
}

// The route: the default period from the server's clock, the refusals of the
// filters, the weak ETag and its 304, who may read it.
func TestDashboardRoute(t *testing.T) {
	e := newDashboardEnv(t)
	e.ticket(t, e.ProjectA, "open", ago(1))
	path := "/api/v1/tenants/" + e.SlugA + "/dashboard"

	d := e.dashboard(t, caller{Token: e.tk.ViewerA}, "")
	assert.Equal(t, "2026-09-08", d.Period.From.String(), "the thirty days that end today")
	assert.Equal(t, "2026-10-07", d.Period.To.String())
	d = e.dashboard(t, e.member(), "?from=2026-09-01&to=2026-09-30")
	assert.Equal(t, "2026-09-01", d.Period.From.String())
	assert.Equal(t, "2026-09-30", d.Period.To.String())

	body := assertProblem(t, e.s.do(t, e.member(), http.MethodGet, path+"?from=2026-10-08&to=2026-10-07", nil),
		http.StatusBadRequest, "validation_failed")
	assert.Equal(t, "query:from", body["errors"].([]any)[0].(map[string]any)["pointer"])
	assertProblem(t, e.s.do(t, e.member(), http.MethodGet, path+"?project=alpha", nil), http.StatusBadRequest, "validation_failed")
	assertProblem(t, e.s.do(t, e.member(), http.MethodGet, path+"?state=filed", nil), http.StatusBadRequest, "validation_failed")

	first := e.s.do(t, e.member(), http.MethodGet, path, nil)
	require.Equal(t, http.StatusOK, first.StatusCode)
	tag := first.Header.Get("ETag")
	same := e.s.do(t, e.member(), http.MethodGet, path, nil, "If-None-Match", tag)
	assert.Equal(t, http.StatusNotModified, same.StatusCode)
	assert.Equal(t, tag, same.Header.Get("ETag"))
	e.ticket(t, e.hidden, "hidden from the member", ago(1))
	assert.Equal(t, http.StatusNotModified, e.s.do(t, e.member(), http.MethodGet, path, nil, "If-None-Match", tag).StatusCode,
		"a ticket the member cannot see changes nothing the member reads")
	e.ticket(t, e.ProjectA, "another", ago(1))
	changed := e.s.do(t, e.member(), http.MethodGet, path, nil, "If-None-Match", tag)
	assert.Equal(t, http.StatusOK, changed.StatusCode)
	assert.NotEqual(t, tag, changed.Header.Get("ETag"))

	assertProblem(t, e.s.do(t, caller{Token: e.tk.MemberB}, http.MethodGet, path, nil), http.StatusNotFound, "not_found")
	restricted, _, err := e.f.Token(e.ctx, fixture.TokenSpec{UserID: e.MemberA, TenantID: e.A, ProjectID: e.ProjectA})
	require.NoError(t, err)
	assertProblem(t, e.s.do(t, caller{Token: restricted}, http.MethodGet, path, nil), http.StatusNotFound, "not_found")
	agent := e.dashboard(t, caller{Token: e.tk.AssistedAgentA, Agent: "claude-code/opus/1"}, "")
	assert.NotEmpty(t, agent.OpenByState, "an agent reads it like its person")
}
