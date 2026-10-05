package api

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/problem"
	"github.com/guided-traffic/cowork/backend/internal/store/readq"
)

// dashboardNow is a Wednesday afternoon, UTC.
var dashboardNow = time.Date(2026, 10, 7, 15, 30, 0, 0, time.UTC)

func utc(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

func dateParam(t time.Time) *apigen.FromDay { return &apigen.FromDay{Time: t} }

func mustQuery(t *testing.T, p apigen.GetDashboardParams) dashboardQuery {
	t.Helper()
	q, perr := parseDashboardQuery(dashboardNow, p)
	require.Nil(t, perr)
	return q
}

// docs/adr/0018 D6: without a period the dashboard reads the thirty days that
// end today, UTC, and without a project every project, as empty arrays.
func TestTheDashboardsDefaults(t *testing.T) {
	q := mustQuery(t, apigen.GetDashboardParams{})
	assert.Equal(t, utc(2026, 10, 7), q.to)
	assert.Equal(t, utc(2026, 9, 8), q.from, "thirty days, both ends inclusive")
	assert.Equal(t, utc(2026, 10, 8), q.end())
	assert.NotNil(t, q.projects, "an empty array, which the queries read as no filter; NULL would match nothing")
	assert.NotNil(t, q.without)
	assert.Empty(t, q.projects)

	late := time.Date(2026, 10, 7, 23, 30, 0, 0, time.FixedZone("UTC-2", -2*3600))
	q, perr := parseDashboardQuery(late, apigen.GetDashboardParams{})
	require.Nil(t, perr)
	assert.Equal(t, utc(2026, 10, 8), q.to, "today is the UTC day (docs/adr/0055 D3)")
}

// docs/adr/0049 D2, D6: the dashboard's project filter is the ticket lists':
// repeatable, a ! leaves a project out, a value that is no key is refused.
func TestTheDashboardsProjectFilter(t *testing.T) {
	q := mustQuery(t, apigen.GetDashboardParams{Project: &apigen.FilterProject{"ALPHA", "!BETA", "GAMMA"}})
	assert.Equal(t, []string{"ALPHA", "GAMMA"}, q.projects)
	assert.Equal(t, []string{"BETA"}, q.without)

	_, perr := parseDashboardQuery(dashboardNow, apigen.GetDashboardParams{Project: &apigen.FilterProject{"alpha", "!"}})
	require.NotNil(t, perr)
	assert.Equal(t, problem.ValidationFailed, perr.Code)
	require.Len(t, perr.Errors, 2)
	assert.Equal(t, "query:project", perr.Errors[0].Pointer)
}

// The period is the request's, both days inclusive; a first day after the
// last is refused.
func TestTheDashboardsPeriod(t *testing.T) {
	q := mustQuery(t, apigen.GetDashboardParams{From: dateParam(utc(2026, 9, 1)), To: dateParam(utc(2026, 9, 30))})
	assert.Equal(t, utc(2026, 9, 1), q.from)
	assert.Equal(t, utc(2026, 9, 30), q.to)

	q = mustQuery(t, apigen.GetDashboardParams{To: dateParam(utc(2026, 9, 30))})
	assert.Equal(t, utc(2026, 9, 1), q.from, "thirty days that end with the last day named")

	q = mustQuery(t, apigen.GetDashboardParams{From: dateParam(utc(2026, 10, 7)), To: dateParam(utc(2026, 10, 7))})
	assert.Equal(t, q.from, q.to, "a period of one day")

	_, perr := parseDashboardQuery(dashboardNow, apigen.GetDashboardParams{From: dateParam(utc(2026, 10, 8)), To: dateParam(utc(2026, 10, 7))})
	require.NotNil(t, perr)
	assert.Equal(t, problem.ValidationFailed, perr.Code)
	assert.Equal(t, "query:from", perr.Errors[0].Pointer)
}

// Tile 1: a row per project and state, as the query counted them.
func TestTheOpenByStateTile(t *testing.T) {
	got := stateCounts([]readq.DashboardOpenByStateRow{
		{ProjectKey: "ALPHA", State: domain.StateFiled, Tickets: 2},
		{ProjectKey: "ALPHA", State: domain.StateBlocked, Tickets: 1},
		{ProjectKey: "BETA", State: domain.StateReview, Tickets: 4},
	})
	assert.Equal(t, []apigen.DashboardStateCount{
		{Project: "ALPHA", State: apigen.TicketStateFiled, Count: 2},
		{Project: "ALPHA", State: apigen.TicketStateBlocked, Count: 1},
		{Project: "BETA", State: apigen.TicketStateReview, Count: 4},
	}, got)
	assert.NotNil(t, stateCounts(nil), "an empty array, never null")
}

// Tile 2: every severity in its order, zero included.
func TestTheOpenBySeverityTile(t *testing.T) {
	got := severityCounts([]readq.DashboardOpenBySeverityRow{
		{Severity: "low", Tickets: 3}, {Severity: "critical", Tickets: 1},
	})
	assert.Equal(t, []apigen.DashboardSeverityCount{
		{Severity: apigen.SeverityCritical, Count: 1},
		{Severity: apigen.SeverityHigh, Count: 0},
		{Severity: apigen.SeverityMedium, Count: 0},
		{Severity: apigen.SeverityLow, Count: 3},
		{Severity: apigen.SeverityCosmetic, Count: 0},
	}, got)
}

// Tile 3: live, then boundary, each with its count and its oldest, null
// where there is none.
func TestTheSecurityTile(t *testing.T) {
	filed := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	got := securityTile("acme", []readq.DashboardSecurityRow{
		{Security: domain.SecurityBoundary, Tickets: 3, ProjectKey: "ALPHA", Number: 7, Title: "a hole", OpenedAt: filed},
	})
	require.Len(t, got, 2)
	assert.Equal(t, apigen.SecurityClassLive, got[0].Class)
	assert.Zero(t, got[0].Count)
	assert.True(t, got[0].Oldest.IsNull())
	assert.Equal(t, apigen.SecurityClassBoundary, got[1].Class)
	assert.Equal(t, 3, got[1].Count)
	assert.Equal(t, apigen.DashboardTicket{Key: "acme/ALPHA-7", Title: "a hole", Since: filed}, got[1].Oldest.MustGet())
}

// Tile 4: the count and the ticket blocked longest with its kind, or null.
func TestTheBlockedTile(t *testing.T) {
	none := blockedTile("acme", nil)
	assert.Zero(t, none.Count)
	assert.True(t, none.Oldest.IsNull())

	since := time.Date(2026, 9, 20, 8, 0, 0, 0, time.UTC)
	kind := domain.BlockHuman
	got := blockedTile("acme", []readq.DashboardBlockedRow{
		{Tickets: 2, ProjectKey: "ALPHA", Number: 3, Title: "waits", BlockKind: &kind, Since: since},
	})
	assert.Equal(t, 2, got.Count)
	assert.Equal(t, apigen.DashboardBlock{Key: "acme/ALPHA-3", Title: "waits", Since: since, Kind: apigen.BlockKindHuman},
		got.Oldest.MustGet())
}

// Tile 5: five buckets in their order, each with its bounds in days, the last
// open-ended; the cuts are the request's clock less whole days of 24 hours.
func TestTheAgeTile(t *testing.T) {
	got := ageTile(readq.DashboardAgeRow{Under7: 1, Under30: 2, Under90: 3, Under365: 4, Older: 5})
	require.Len(t, got, 5)
	bounds := [][2]int{{0, 7}, {7, 30}, {30, 90}, {90, 365}}
	for i, b := range bounds {
		assert.Equal(t, b[0], got[i].FromDays)
		assert.Equal(t, b[1], got[i].ToDays.MustGet())
		assert.Equal(t, i+1, got[i].Count)
	}
	assert.Equal(t, 365, got[4].FromDays)
	assert.True(t, got[4].ToDays.IsNull())
	assert.Equal(t, 5, got[4].Count)

	q := mustQuery(t, apigen.GetDashboardParams{})
	assert.Equal(t, dashboardNow.Add(-7*24*time.Hour), q.ageCut(7))
	assert.Equal(t, dashboardNow.Add(-365*24*time.Hour), q.ageCut(365))
}

// Tile 6: the eight ISO weeks that end with the one holding the period's
// last day, oldest first, every week present, the last cut at that day.
func TestTheThroughputTile(t *testing.T) {
	q := mustQuery(t, apigen.GetDashboardParams{To: dateParam(utc(2026, 10, 7))})
	weeks := q.weeks()
	require.Len(t, weeks, 8)
	assert.Equal(t, utc(2026, 10, 5), weeks[7], "the Monday of the week of Wednesday 7 October")
	assert.Equal(t, utc(2026, 8, 17), weeks[0])

	got := throughputTile(q, []readq.DashboardThroughputRow{
		{Week: utc(2026, 8, 17), Tickets: 2}, {Week: utc(2026, 10, 5), Tickets: 1},
	})
	require.Len(t, got, 8)
	assert.Equal(t, apigen.DashboardWeek{Week: "2026-W34", From: day(utc(2026, 8, 17)), To: day(utc(2026, 8, 23)), Done: 2}, got[0])
	assert.Zero(t, got[1].Done, "a week without a ticket is there with none")
	assert.Equal(t, apigen.DashboardWeek{Week: "2026-W41", From: day(utc(2026, 10, 5)), To: day(utc(2026, 10, 7)), Done: 1}, got[7],
		"the last week ends with the period")
}

// A Sunday is the last day of its ISO week, a Monday the first; the week's
// name is its ISO year's, which differs from the calendar's at a new year.
func TestTheISOWeeks(t *testing.T) {
	assert.Equal(t, utc(2026, 10, 5), isoMonday(utc(2026, 10, 11)), "Sunday")
	assert.Equal(t, utc(2026, 10, 5), isoMonday(utc(2026, 10, 5)), "Monday")
	q := mustQuery(t, apigen.GetDashboardParams{To: dateParam(utc(2027, 1, 2))})
	got := throughputTile(q, nil)
	assert.Equal(t, "2026-W53", got[7].Week, "Saturday 2 January 2027 is in the 53rd week of 2026")
	assert.Equal(t, day(utc(2026, 12, 28)), got[7].From)
	assert.Equal(t, day(utc(2027, 1, 2)), got[7].To)
}

// Tile 7: the thirty days that end with the period's last day, the median in
// whole seconds, null while no ticket was done in them.
func TestTheLeadTimeTile(t *testing.T) {
	q := mustQuery(t, apigen.GetDashboardParams{To: dateParam(utc(2026, 9, 30))})
	assert.Equal(t, utc(2026, 9, 1), q.leadTimeFrom())

	none := leadTimeTile(q, readq.DashboardLeadTimeRow{})
	assert.Zero(t, none.Done)
	assert.True(t, none.MedianSeconds.IsNull())
	assert.Equal(t, day(utc(2026, 9, 1)), none.From)
	assert.Equal(t, day(utc(2026, 9, 30)), none.To)

	got := leadTimeTile(q, readq.DashboardLeadTimeRow{Tickets: 4, MedianSeconds: 86400.5})
	assert.Equal(t, 4, got.Done)
	assert.Equal(t, 86401, got.MedianSeconds.MustGet(), "rounded")
}

// Tile 8: the open questions' count and the one asked first, or null.
func TestTheDecisionsTile(t *testing.T) {
	none := decisionsTile("acme", nil)
	assert.Zero(t, none.Count)
	assert.True(t, none.Oldest.IsNull())

	asked := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	got := decisionsTile("acme", []readq.DashboardDecisionsRow{
		{Questions: 5, ProjectKey: "ALPHA", TicketNumber: 9, TicketTitle: "pick one", QuestionNumber: 2, Question: "A or B?", CreatedAt: asked},
	})
	assert.Equal(t, 5, got.Count)
	assert.Equal(t, apigen.DashboardQuestion{Key: "acme/ALPHA-9", Title: "pick one", Number: 2, Question: "A or B?", Since: asked},
		got.Oldest.MustGet())
}

// Tile 9: the minutes per project that has any, and their sum.
func TestTheTimeTile(t *testing.T) {
	got := timeTile([]readq.DashboardTimeRow{
		{ProjectKey: "ALPHA", ProjectName: "Alpha", Minutes: 90}, {ProjectKey: "BETA", ProjectName: "Beta", Minutes: 30},
	})
	assert.Equal(t, 120, got.TotalMinutes)
	assert.Equal(t, []apigen.DashboardProjectTime{{Project: "ALPHA", Name: "Alpha", Minutes: 90}, {Project: "BETA", Name: "Beta", Minutes: 30}},
		got.Projects)
	empty := timeTile(nil)
	assert.Zero(t, empty.TotalMinutes)
	assert.NotNil(t, empty.Projects)
}

// Beside the tiles: the open tickets updated last, by their canonical keys.
func TestTheRecentList(t *testing.T) {
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	got := recentList("acme", []readq.DashboardRecentRow{
		{ProjectKey: "ALPHA", Number: 4, Type: domain.TypeBug, Title: "broken", State: domain.StateInProgress, UpdatedAt: at},
	})
	assert.Equal(t, []apigen.DashboardRecent{{Key: "acme/ALPHA-4", Type: apigen.TicketTypeBug, Title: "broken",
		State: apigen.TicketStateInProgress, UpdatedAt: at}}, got)
}

// The whole answer carries the period the tiles were read for.
func TestTheDashboardView(t *testing.T) {
	q := mustQuery(t, apigen.GetDashboardParams{})
	got := dashboardRows{}.view("acme", q)
	assert.Equal(t, apigen.DashboardPeriod{From: day(utc(2026, 9, 8)), To: day(utc(2026, 10, 7))}, got.Period)
	assert.Len(t, got.OpenBySeverity, 5)
	assert.Len(t, got.Security, 2)
	assert.Len(t, got.Age, 5)
	assert.Len(t, got.Throughput, 8)
	assert.NotNil(t, got.OpenByState)
	assert.NotNil(t, got.Recent)
}
