package metrics

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// exercise records through every method once, so that every family of the
// backend's own instruments has a series to gather.
func exercise(m *Metrics) {
	ctx, done := m.Request(context.Background(), http.MethodGet)
	SetRoute(ctx, "/api/v1/teams/{team}/projects/{project}/tickets/{number}")
	done(http.StatusOK)
	m.ObservePool(func() PoolStats {
		return PoolStats{Idle: 3, InUse: 1, Max: 4, Acquires: 10, AcquireTime: time.Second, Waits: 2, WaitTime: time.Second, Canceled: 1}
	})
	m.QueryError("unique_violation")
	m.ObserveSchema(func(context.Context) (uint, bool, error) { return 39, false, nil })
	m.JobRun("session-expiry", time.Second, false)
	m.OpenStreams(2)
	m.EventPublished()
	m.SubscriberDropped(DropBehind)
	m.Replay(true)
	m.Act("commented", ActorAgent)
	m.Login(LoginLocal, LoginSuccess)
	m.Lockout()
	m.TokenRefused(TokenExpired)
	m.ObserveConsistency(func(context.Context) (Consistency, error) {
		const tenant = "0199a7c2-1d2e-7f00-8000-0000000000aa"
		return Consistency{Counts: []ConsistencyCounts{{Tenant: tenant, Dangling: 1, Orphans: 2}},
			Exports: []TenantExport{{Tenant: tenant, Since: time.Now().Add(-time.Hour)}}}, nil
	})
}

// teamLabelled are the families that carry a team, by its id: the
// consistency family's — the check's two counts and the age of the last
// export —, and nothing else (docs/adr/0060 D5).
var teamLabelled = map[string]bool{
	"cowork_consistency_dangling_attachments":    true,
	"cowork_consistency_orphaned_objects":        true,
	"cowork_consistency_last_export_age_seconds": true,
}

func gathered(t *testing.T, m *Metrics) []Sample {
	t.Helper()
	samples, err := m.Samples()
	require.NoError(t, err)
	return samples
}

// docs/adr/0060 D4: every instrument is named cowork_<subsystem>_<name>_<unit>,
// in a subsystem of the first release; a counter's name ends in _total.
func TestEveryInstrumentIsNamedByTheRule(t *testing.T) {
	m := New()
	names := m.Names()
	require.NotEmpty(t, names)
	rule := regexp.MustCompile(`^cowork_(http|db|jobs|events|audit|auth|migrations|consistency)_[a-z][a-z_]*[a-z]$`)
	for _, name := range names {
		assert.Regexp(t, rule, name)
	}
	assert.Len(t, names, len(slices.Compact(slices.Sorted(slices.Values(names)))), "no name twice")

	exercise(m)
	for _, s := range gathered(t, m) {
		if strings.HasPrefix(s.Name, "cowork_") && strings.Contains(s.Name, "_total") {
			assert.True(t, strings.HasSuffix(s.Name, "_total"), "%s: a counter's name ends in _total", s.Name)
		}
	}
}

// docs/adr/0060 D5: no label carries a person, a ticket, a key, a token or a
// request id, anywhere; the team label belongs to the consistency family's
// three gauges alone, as the team's id — never its slug —, and they carry
// nothing else but tenant, the same id under the label's name before, for one
// release (docs/adr/0005 D1). Every family of the backend's is gathered, so a
// new instrument cannot slip past this test unexercised.
func TestNoInstrumentCarriesAForbiddenLabel(t *testing.T) {
	m := New()
	exercise(m)
	samples := gathered(t, m)
	families := map[string]bool{}
	for _, s := range samples {
		families[strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(s.Name, "_bucket"), "_count"), "_sum")] = true
		for label, value := range s.Labels {
			assert.NotContains(t, []string{"person", "person_id", "user", "user_id", "ticket", "key", "token", "token_id", "request_id"},
				label, "%s carries the label %s", s.Name, label)
			if label == "team" || label == "tenant" {
				assert.True(t, teamLabelled[s.Name], "%s carries a team", s.Name)
				_, err := uuid.Parse(value)
				assert.NoError(t, err, "%s names a team by %q, which is no id", s.Name, value)
			}
		}
		if teamLabelled[s.Name] {
			assert.Equal(t, []string{"team", "tenant"}, slices.Sorted(maps.Keys(s.Labels)), "%s carries the team and nothing else", s.Name)
			assert.Equal(t, s.Labels["team"], s.Labels["tenant"], "%s: tenant repeats team", s.Name)
		}
	}
	for _, name := range m.Names() {
		assert.True(t, families[name], "%s was not gathered: exercise it in this test", name)
	}
	for name := range teamLabelled {
		assert.True(t, slices.Contains(m.Names(), name), "%s is an instrument of the registry", name)
	}
}

// docs/adr/0060 D5: the route label is the pattern the handler names, never the
// path; a request nobody names is unmatched, a method HTTP does not define is
// other, and a request that wrote nothing was answered 200.
func TestARequestIsRecordedByItsPattern(t *testing.T) {
	m := New()
	ctx, done := m.Request(context.Background(), http.MethodPatch)
	assert.Equal(t, 1.0, Sum(gathered(t, m), "cowork_http_requests_in_flight"), "in flight while it runs")
	SetRoute(ctx, "/api/v1/teams/{team}/projects/{project}/tickets/{number}")
	done(http.StatusConflict)

	_, done = m.Request(context.Background(), "BREW")
	done(0)

	samples := gathered(t, m)
	assert.Equal(t, 0.0, Sum(samples, "cowork_http_requests_in_flight"))
	assert.Equal(t, 1.0, Sum(samples, "cowork_http_requests_total",
		"route", "/api/v1/teams/{team}/projects/{project}/tickets/{number}", "method", "PATCH", "status", "409"))
	assert.Equal(t, 1.0, Sum(samples, "cowork_http_request_duration_seconds_count",
		"route", "/api/v1/teams/{team}/projects/{project}/tickets/{number}", "method", "PATCH"))
	assert.Equal(t, 1.0, Sum(samples, "cowork_http_requests_total", "route", Unmatched, "method", "other", "status", "200"))

	SetRoute(context.Background(), "/nowhere") // outside a record: nothing happens
}

func TestANilRegistryRecordsNothing(t *testing.T) {
	var m *Metrics
	ctx, done := m.Request(context.Background(), http.MethodGet)
	SetRoute(ctx, "/healthz")
	done(http.StatusOK)
	m.ObservePool(func() PoolStats { return PoolStats{} })
	m.ObserveSchema(nil)
	m.QueryError("other")
	m.JobRun("ticket-purge", time.Second, true)
	m.OpenStreams(1)
	m.EventPublished()
	m.SubscriberDropped(DropLimit)
	m.Replay(false)
	m.Act("created", ActorSystem)
	m.Login(LoginOIDC, LoginRefused)
	m.Lockout()
	m.TokenRefused(TokenUnknown)
	m.ObserveConsistency(nil)
	samples, err := m.Samples()
	assert.NoError(t, err)
	assert.Empty(t, samples)
	assert.Empty(t, m.Names())
}

// docs/adr/0060 D6: a job's failures one after the other are what the alert
// counts; a success sets them back.
func TestAJobCountsItsConsecutiveFailures(t *testing.T) {
	m := New()
	m.JobRun("ticket-purge", 2*time.Second, true)
	m.JobRun("ticket-purge", time.Second, true)
	samples := gathered(t, m)
	assert.Equal(t, 2.0, Sum(samples, "cowork_jobs_consecutive_failures", "name", "ticket-purge"))
	assert.Equal(t, 2.0, Sum(samples, "cowork_jobs_failures_total", "name", "ticket-purge"))
	assert.Equal(t, 2.0, Sum(samples, "cowork_jobs_runs_total", "name", "ticket-purge"))
	assert.Equal(t, 3.0, Sum(samples, "cowork_jobs_duration_seconds_sum", "name", "ticket-purge"))

	m.JobRun("ticket-purge", time.Second, false)
	m.JobRun("session-expiry", time.Second, false)
	samples = gathered(t, m)
	assert.Equal(t, 0.0, Sum(samples, "cowork_jobs_consecutive_failures", "name", "ticket-purge"))
	assert.Equal(t, 2.0, Sum(samples, "cowork_jobs_failures_total", "name", "ticket-purge"), "the failures stay counted")
	assert.True(t, Has(samples, "cowork_jobs_failures_total", "name", "session-expiry"), "a job that never failed shows its zero")
}

// The pool's statistics are read at the scrape: the connections by state, the
// most there may be, the acquires and the waits with their time.
func TestThePoolIsReadAtTheScrape(t *testing.T) {
	m := New()
	assert.False(t, Has(gathered(t, m), "cowork_db_pool_connections"), "no pool observed, no series")
	stats := PoolStats{Idle: 2, InUse: 5, Constructing: 1, Max: 8, Acquires: 40, AcquireTime: 2 * time.Second,
		Waits: 3, WaitTime: 1500 * time.Millisecond, Canceled: 1}
	m.ObservePool(func() PoolStats { return stats })
	samples := gathered(t, m)
	assert.Equal(t, 2.0, Sum(samples, "cowork_db_pool_connections", "state", "idle"))
	assert.Equal(t, 5.0, Sum(samples, "cowork_db_pool_connections", "state", "in_use"))
	assert.Equal(t, 1.0, Sum(samples, "cowork_db_pool_connections", "state", "constructing"))
	assert.Equal(t, 8.0, Sum(samples, "cowork_db_pool_max_connections"))
	assert.Equal(t, 40.0, Sum(samples, "cowork_db_pool_acquire_duration_seconds_count"))
	assert.Equal(t, 2.0, Sum(samples, "cowork_db_pool_acquire_duration_seconds_sum"))
	assert.Equal(t, 3.0, Sum(samples, "cowork_db_pool_wait_duration_seconds_count"))
	assert.Equal(t, 1.5, Sum(samples, "cowork_db_pool_wait_duration_seconds_sum"))
	assert.Equal(t, 1.0, Sum(samples, "cowork_db_pool_canceled_acquires_total"))

	stats.InUse = 8
	assert.Equal(t, 8.0, Sum(gathered(t, m), "cowork_db_pool_connections", "state", "in_use"), "every scrape reads anew")
}

// docs/adr/0060 D4, D6: the schema's version and dirty flag are read at a
// scrape, at most once every ten seconds; a read that fails leaves both out.
func TestTheSchemaIsReadAtMostEveryTenSeconds(t *testing.T) {
	m := New()
	clock := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	m.schema.now = func() time.Time { return clock }
	reads := 0
	state := struct {
		version uint
		dirty   bool
		err     error
	}{version: 39}
	m.ObserveSchema(func(ctx context.Context) (uint, bool, error) {
		reads++
		_, ok := ctx.Deadline()
		assert.True(t, ok, "the read is bounded")
		return state.version, state.dirty, state.err
	})

	samples := gathered(t, m)
	assert.Equal(t, 39.0, Sum(samples, "cowork_migrations_schema_version"))
	assert.Equal(t, 0.0, Sum(samples, "cowork_migrations_schema_dirty"))
	assert.True(t, Has(samples, "cowork_migrations_schema_dirty"))

	state.version, state.dirty = 40, true
	clock = clock.Add(9 * time.Second)
	gathered(t, m)
	assert.Equal(t, 1, reads, "a scrape within ten seconds reuses the read")

	clock = clock.Add(time.Second)
	samples = gathered(t, m)
	assert.Equal(t, 2, reads)
	assert.Equal(t, 40.0, Sum(samples, "cowork_migrations_schema_version"))
	assert.Equal(t, 1.0, Sum(samples, "cowork_migrations_schema_dirty"), "a migration that failed halfway shows while the backend serves")

	state.err = errors.New("the database does not answer")
	clock = clock.Add(10 * time.Second)
	samples = gathered(t, m)
	assert.False(t, Has(samples, "cowork_migrations_schema_version"), "a failed read shows nothing it does not know")
	assert.False(t, Has(samples, "cowork_migrations_schema_dirty"))
	clock = clock.Add(time.Second)
	gathered(t, m)
	assert.Equal(t, 3, reads, "a failed read is not repeated at every scrape either")
}

// docs/adr/0059 D4, docs/adr/0060 D4, D5: the consistency check's counts are
// read from the database at a scrape — the same on every replica, whichever
// ran the check —, at most once a minute; every tenant with a result has its
// two series, by its id; a read that fails leaves the family out and is not
// repeated at every scrape either.
func TestTheConsistencyCountsAreReadAtMostOnceAMinute(t *testing.T) {
	m := New()
	clock := time.Date(2026, 10, 6, 3, 0, 0, 0, time.UTC)
	m.consistency.now = func() time.Time { return clock }
	assert.False(t, Has(gathered(t, m), "cowork_consistency_dangling_attachments"), "nothing observed, no series")

	const a, b = "0199a7c2-1d2e-7f00-8000-0000000000aa", "0199a7c2-1d2e-7f00-8000-0000000000bb"
	reads := 0
	state := struct {
		counts []ConsistencyCounts
		err    error
	}{counts: []ConsistencyCounts{{Tenant: a, Dangling: 3, Orphans: 2}, {Tenant: b}}}
	m.ObserveConsistency(func(ctx context.Context) (Consistency, error) {
		reads++
		_, ok := ctx.Deadline()
		assert.True(t, ok, "the read is bounded")
		return Consistency{Counts: state.counts}, state.err
	})

	samples := gathered(t, m)
	assert.Equal(t, 3.0, Sum(samples, "cowork_consistency_dangling_attachments", "team", a))
	assert.Equal(t, 2.0, Sum(samples, "cowork_consistency_orphaned_objects", "team", a))
	assert.True(t, Has(samples, "cowork_consistency_dangling_attachments", "team", b), "a checked tenant shows its zero")
	assert.Equal(t, 0.0, Sum(samples, "cowork_consistency_orphaned_objects", "team", b))

	state.counts = []ConsistencyCounts{{Tenant: a, Dangling: 1}}
	clock = clock.Add(59 * time.Second)
	gathered(t, m)
	assert.Equal(t, 1, reads, "a scrape within the minute reuses the read")

	clock = clock.Add(time.Second)
	samples = gathered(t, m)
	assert.Equal(t, 2, reads)
	assert.Equal(t, 1.0, Sum(samples, "cowork_consistency_dangling_attachments", "team", a), "an acceptance shows at the next read")
	assert.Equal(t, 0.0, Sum(samples, "cowork_consistency_orphaned_objects", "team", a), "a removal shows at the next read")
	assert.False(t, Has(samples, "cowork_consistency_dangling_attachments", "team", b), "a tenant without a result has no series")

	state.err = errors.New("the database does not answer")
	clock = clock.Add(time.Minute)
	samples = gathered(t, m)
	assert.False(t, Has(samples, "cowork_consistency_dangling_attachments"), "a failed read shows nothing it does not know")
	assert.False(t, Has(samples, "cowork_consistency_orphaned_objects"))
	clock = clock.Add(time.Second)
	gathered(t, m)
	assert.Equal(t, 3, reads, "a failed read is not repeated at every scrape either")
}

// docs/adr/0059 D2, docs/adr/0060 D4, D6: every tenant has the age of its last
// export — of a project or of the whole —, read with the counts at most once a
// minute and counted at every scrape from the time read, so that it grows
// between two reads and the alert on it needs no read to fire; an export shows
// at the next read; a time ahead of the replica's clock is no negative age; a
// failed read leaves the age out with the counts.
func TestTheLastExportsAgeIsCountedAtEveryScrape(t *testing.T) {
	m := New()
	clock := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	m.consistency.now = func() time.Time { return clock }
	const a, b = "0199a7c2-1d2e-7f00-8000-0000000000aa", "0199a7c2-1d2e-7f00-8000-0000000000bb"
	reads := 0
	state := struct {
		exports []TenantExport
		err     error
	}{exports: []TenantExport{{Tenant: a, Since: clock.Add(-8 * 24 * time.Hour)}, {Tenant: b, Since: clock.Add(-time.Hour)}}}
	m.ObserveConsistency(func(context.Context) (Consistency, error) {
		reads++
		return Consistency{Exports: state.exports}, state.err
	})

	samples := gathered(t, m)
	assert.Equal(t, (8 * 24 * time.Hour).Seconds(), Sum(samples, "cowork_consistency_last_export_age_seconds", "team", a))
	assert.Equal(t, time.Hour.Seconds(), Sum(samples, "cowork_consistency_last_export_age_seconds", "team", b))
	assert.False(t, Has(samples, "cowork_consistency_dangling_attachments"), "a tenant without a check has an age and no counts")

	state.exports = []TenantExport{{Tenant: a, Since: clock.Add(30 * time.Second)}, {Tenant: b, Since: clock.Add(-time.Hour)}}
	clock = clock.Add(30 * time.Second)
	samples = gathered(t, m)
	assert.Equal(t, 1, reads, "a scrape within the minute reuses the read")
	assert.Equal(t, (8*24*time.Hour + 30*time.Second).Seconds(), Sum(samples, "cowork_consistency_last_export_age_seconds", "team", a),
		"the age grows between two reads")

	clock = clock.Add(30 * time.Second)
	samples = gathered(t, m)
	assert.Equal(t, 2, reads)
	assert.Equal(t, 30.0, Sum(samples, "cowork_consistency_last_export_age_seconds", "team", a), "an export shows at the next read")

	state.exports = []TenantExport{{Tenant: a, Since: clock.Add(time.Hour)}}
	clock = clock.Add(time.Minute)
	samples = gathered(t, m)
	assert.Equal(t, 0.0, Sum(samples, "cowork_consistency_last_export_age_seconds", "team", a), "a clock ahead is no negative age")
	assert.True(t, Has(samples, "cowork_consistency_last_export_age_seconds", "team", a))

	state.err = errors.New("the database does not answer")
	clock = clock.Add(time.Minute)
	assert.False(t, Has(gathered(t, m), "cowork_consistency_last_export_age_seconds"), "a failed read shows nothing it does not know")
}

// The closed sets of labels exist at zero from the start, so a rate over them
// — the alert on the drops among them — is defined before the first event.
func TestTheClosedSetsStartAtZero(t *testing.T) {
	samples := gathered(t, New())
	for _, reason := range []StreamDrop{DropBehind, DropLimit, DropResync} {
		assert.True(t, Has(samples, "cowork_events_subscribers_dropped_total", "reason", string(reason)), reason)
	}
	assert.True(t, Has(samples, "cowork_events_replays_total", "outcome", "miss"))
	assert.True(t, Has(samples, "cowork_auth_logins_total", "method", "oidc", "outcome", "refused"))
	assert.True(t, Has(samples, "cowork_auth_token_refusals_total", "reason", "session_only"))
	assert.True(t, Has(samples, "cowork_auth_lockouts_total"))
	assert.True(t, Has(samples, "cowork_events_open_streams"))
	assert.True(t, Has(samples, "cowork_http_requests_in_flight"))
	assert.True(t, Has(samples, "go_goroutines"), "the Go runtime collector")
}

// docs/adr/0060 D1: the scrape is Prometheus text.
func TestTheHandlerAnswersPrometheusText(t *testing.T) {
	m := New()
	m.Act("created", ActorPerson)
	rec := httptest.NewRecorder()
	m.Handler(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/plain")
	body, err := io.ReadAll(rec.Body)
	require.NoError(t, err)
	assert.Contains(t, string(body), `cowork_audit_acts_total{action="created",actor="person"} 1`)
	assert.Contains(t, string(body), "# TYPE cowork_http_requests_in_flight gauge")
}

// docs/adr/0060 D3, Consequences: the dashboard names only instruments that
// exist, so a renamed one breaks this test and not a panel; every query is
// narrowed to the dashboard's namespace.
func TestTheDashboardNamesOnlyInstrumentsThatExist(t *testing.T) {
	m := New()
	exercise(m)
	samples, err := m.Samples()
	if err != nil {
		t.Logf("a collector failed on this platform, the rest is checked: %v", err)
	}
	known := map[string]bool{}
	for _, s := range samples {
		known[s.Name] = true
	}
	metric := regexp.MustCompile(`\b(?:cowork|go|process)_[a-z0-9_]+\b`)
	queries := DashboardQueries()
	require.NotEmpty(t, queries)
	for _, q := range queries {
		names := metric.FindAllString(q, -1)
		assert.NotEmpty(t, names, "%s names no instrument", q)
		for _, name := range names {
			assert.True(t, known[name], "the dashboard names %s, which no instrument answers: %s", name, q)
		}
		assert.Contains(t, q, selector, "%s is not narrowed to the namespace", q)
	}

	raw, err := Dashboard()
	require.NoError(t, err)
	var board map[string]any
	require.NoError(t, json.Unmarshal(raw, &board))
	assert.Equal(t, "cowork-backend", board["uid"])
	assert.NotEmpty(t, board["panels"])
}

// Only this package imports the client library: the rest of the backend
// records through its typed methods (docs/adr/0060 D1), the integration tier
// included.
func TestOnlyThisPackageImportsTheClientLibrary(t *testing.T) {
	const self = "github.com/guided-traffic/cowork/backend/internal/metrics"
	out, err := exec.Command("go", "list", "-tags", "integration",
		"-f", `{{.ImportPath}} {{join .Imports " "}} {{join .TestImports " "}} {{join .XTestImports " "}}`,
		"github.com/guided-traffic/cowork/backend/...").Output()
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	require.Greater(t, len(lines), 10)
	for _, line := range lines {
		fields := strings.Fields(line)
		if fields[0] == self {
			continue
		}
		for _, imported := range fields[1:] {
			assert.False(t, strings.HasPrefix(imported, "github.com/prometheus/"), "%s imports %s", fields[0], imported)
		}
	}
}
