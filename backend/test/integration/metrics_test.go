//go:build integration

package integration

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/metrics"
	"github.com/guided-traffic/cowork/backend/internal/store"
	"github.com/guided-traffic/cowork/backend/test/fixture"
)

// docs/adr/0060 D1, D4, D5, D6: `cowork serve`, built and run as the image
// runs it against a database of its own, answers a scrape on
// COWORK_METRICS_ADDR — a second listener, no authentication — after a few
// API requests: the routes by their pattern, the pool, the schema state read
// while it serves, the jobs, the acts by their actor, the refused token, the Go
// runtime; no forbidden label anywhere. The API's listener has no /metrics, the
// metrics listener nothing else, and SIGTERM ends both. With the variable empty
// the listener is off.
func TestServeAnswersAScrapeOnItsMetricsListener(t *testing.T) {
	ctx := context.Background()
	iso := newIsolated(t)
	bin := filepath.Join(t.TempDir(), "cowork")
	out, err := exec.Command("go", "build", "-o", bin, "github.com/guided-traffic/cowork/backend/cmd/cowork").CombinedOutput()
	require.NoError(t, err, "go build: %s", out)

	person, err := iso.F.Person(ctx, "mia", "Mia")
	require.NoError(t, err)
	tenant, err := iso.F.Tenant(ctx, "metrics", "Metrics")
	require.NoError(t, err)
	require.NoError(t, iso.F.Member(ctx, tenant, person, domain.RoleMember))
	project, err := iso.F.Project(ctx, tenant, "OBS", "Observability")
	require.NoError(t, err)
	_, number, err := iso.F.Ticket(ctx, tenant, project, person, "Scrape it")
	require.NoError(t, err)
	own, _, err := iso.F.Token(ctx, fixture.TokenSpec{UserID: person, Scope: domain.ScopeWrite})
	require.NoError(t, err)
	agent, _, err := iso.F.Token(ctx, fixture.TokenSpec{UserID: person, Scope: domain.ScopeWrite, Agent: true})
	require.NoError(t, err)

	apiAddr, metricsAddr := freeAddress(t), freeAddress(t)
	server := startServe(t, bin, iso.RuntimeURL, apiAddr, metricsAddr)

	ticket := fmt.Sprintf("http://%s/api/v1/tenants/metrics/projects/OBS/tickets/%d", apiAddr, number)
	assert.Equal(t, http.StatusOK, send(t, http.MethodGet, ticket, own, ""))
	assert.Equal(t, http.StatusCreated, send(t, http.MethodPost, ticket+"/comments", own, `{"body":"by hand"}`))
	assert.Equal(t, http.StatusCreated, send(t, http.MethodPost, ticket+"/comments", agent, `{"body":"by an agent"}`,
		"Idempotency-Key", uuid.NewString()))
	assert.Equal(t, http.StatusUnauthorized, send(t, http.MethodGet, ticket, "cwk_"+strings.Repeat("A", 43), ""))
	assert.Equal(t, http.StatusNotFound, send(t, http.MethodGet, "http://"+apiAddr+"/metrics", "", ""), "the API's listener has no metrics")

	require.Eventually(t, func() bool {
		samples, err := scrapeOnce(metricsAddr)
		return err == nil && metrics.Sum(samples, "cowork_jobs_runs_total", "name", "ticket-purge") >= 1
	}, 30*time.Second, 200*time.Millisecond, "the jobs ran at start")
	samples := scrape(t, metricsAddr)

	for _, c := range []struct{ route, method, status string }{
		{"/api/v1/tenants/{tenant}/projects/{project}/tickets/{number}", "GET", "200"},
		{"/api/v1/tenants/{tenant}/projects/{project}/tickets/{number}/comments", "POST", "201"},
		{"/api/v1/tenants/{tenant}/projects/{project}/tickets/{number}", "GET", "401"},
		{"/healthz", "GET", "200"},
		{metrics.Unmatched, "GET", "404"},
	} {
		assert.GreaterOrEqual(t, metrics.Sum(samples, "cowork_http_requests_total", "route", c.route, "method", c.method, "status", c.status), 1.0, c)
	}
	assert.True(t, metrics.Has(samples, "cowork_http_request_duration_seconds_bucket", "route", "/api/v1/tenants/{tenant}/projects/{project}/tickets/{number}", "le", "+Inf"))
	assert.Equal(t, 1.0, metrics.Sum(samples, "cowork_audit_acts_total", "action", "commented", "actor", "person"))
	assert.Equal(t, 1.0, metrics.Sum(samples, "cowork_audit_acts_total", "action", "commented", "actor", "agent"))
	assert.Equal(t, 1.0, metrics.Sum(samples, "cowork_auth_token_refusals_total", "reason", "unknown"))
	assert.Greater(t, metrics.Sum(samples, "cowork_db_pool_max_connections"), 0.0)
	assert.True(t, metrics.Has(samples, "cowork_db_pool_connections", "state", "idle"))
	assert.Greater(t, metrics.Sum(samples, "cowork_db_pool_acquire_duration_seconds_count"), 0.0)
	assert.Equal(t, float64(iso.version(t)), metrics.Sum(samples, "cowork_migrations_schema_version"))
	assert.True(t, metrics.Has(samples, "cowork_migrations_schema_dirty"))
	assert.Equal(t, 0.0, metrics.Sum(samples, "cowork_migrations_schema_dirty"))
	for _, job := range []string{"bootstrap", "idempotency-expiry", "session-expiry", "login-expiry", "notification-expiry", "ticket-purge"} {
		assert.GreaterOrEqual(t, metrics.Sum(samples, "cowork_jobs_runs_total", "name", job), 1.0, job)
		assert.Equal(t, 0.0, metrics.Sum(samples, "cowork_jobs_consecutive_failures", "name", job), job)
	}
	assert.True(t, metrics.Has(samples, "cowork_events_open_streams"))
	assert.True(t, metrics.Has(samples, "go_goroutines"))
	for _, s := range samples {
		for label, value := range s.Labels {
			assert.NotContains(t, []string{"person", "person_id", "user", "user_id", "ticket", "key", "token", "token_id", "request_id"},
				label, "%s carries the label %s", s.Name, label)
			if label == "tenant" {
				assert.True(t, strings.HasPrefix(s.Name, "cowork_consistency_"), "%s carries a tenant", s.Name)
			}
			if label == "route" {
				for _, instance := range []string{"metrics", "OBS", "/" + strconv.Itoa(number)} {
					assert.NotContains(t, value, instance, "a route label holds what the client sent")
				}
			}
		}
	}
	assert.Equal(t, http.StatusNotFound, send(t, http.MethodGet, "http://"+metricsAddr+"/healthz", "", ""), "the metrics listener serves the metrics alone")
	assert.Equal(t, http.StatusNotFound, send(t, http.MethodGet, "http://"+metricsAddr+"/api/v1/version", "", ""))

	// A migration of a newer release that fails halfway beside the running
	// server shows at the next read, at most ten seconds later (D6).
	require.NoError(t, iso.F.Exec(ctx, "UPDATE schema_migrations SET dirty = true"))
	assert.Eventually(t, func() bool {
		samples, err := scrapeOnce(metricsAddr)
		return err == nil && metrics.Sum(samples, "cowork_migrations_schema_dirty") == 1
	}, 20*time.Second, 500*time.Millisecond, "the dirty flag is read while the server serves")
	require.NoError(t, iso.F.Exec(ctx, "UPDATE schema_migrations SET dirty = false"))

	server.stop(t)
	assert.Contains(t, server.log.text(), "metrics listening")
	for _, addr := range []string{apiAddr, metricsAddr} {
		_, err := net.DialTimeout("tcp", addr, time.Second)
		assert.Error(t, err, "%s is closed after SIGTERM", addr)
	}

	// An empty COWORK_METRICS_ADDR switches the listener off.
	apiAddr = freeAddress(t)
	off := startServe(t, bin, iso.RuntimeURL, apiAddr, "")
	assert.Equal(t, http.StatusOK, send(t, http.MethodGet, "http://"+apiAddr+"/healthz", "", ""))
	off.stop(t)
	assert.Contains(t, off.log.text(), "the metrics listener is off")
	assert.NotContains(t, off.log.text(), "metrics listening")
}

// served is a `cowork serve` process of a test. err is the process's end,
// written before exited closes.
type served struct {
	cmd    *exec.Cmd
	exited chan struct{}
	err    error
	log    *recordingLogger
}

// startServe runs `cowork serve` as the chart's container runs it — migrated
// beforehand, no owner credential — on apiAddr and metricsAddr, and waits for
// its health.
func startServe(t *testing.T, bin, runtimeURL, apiAddr, metricsAddr string) *served {
	t.Helper()
	s := &served{cmd: exec.Command(bin, "serve"), exited: make(chan struct{}), log: &recordingLogger{}}
	s.cmd.Env = []string{
		"COWORK_DATABASE_URL=" + runtimeURL,
		"COWORK_MIGRATE_ON_START=false",
		"COWORK_SESSION_KEY=" + base64.StdEncoding.EncodeToString(testSessionKey),
		"COWORK_LISTEN_ADDR=" + apiAddr,
		"COWORK_METRICS_ADDR=" + metricsAddr,
		"COWORK_LOG_FORMAT=text",
	}
	s.cmd.Stdout, s.cmd.Stderr = s.log, s.log
	require.NoError(t, s.cmd.Start())
	go func() {
		s.err = s.cmd.Wait()
		close(s.exited)
	}()
	t.Cleanup(func() {
		select {
		case <-s.exited:
		default:
			_ = s.cmd.Process.Kill()
			<-s.exited
		}
	})
	for deadline := time.Now().Add(30 * time.Second); ; time.Sleep(100 * time.Millisecond) {
		select {
		case <-s.exited:
			t.Fatalf("cowork serve ended before it served: %v\n%s", s.err, s.log.text())
		default:
		}
		if res, err := plain.Get("http://" + apiAddr + "/healthz"); err == nil {
			res.Body.Close()
			if res.StatusCode == http.StatusOK {
				return s
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("cowork serve does not answer its health:\n%s", s.log.text())
		}
	}
}

// stop sends SIGTERM and expects the process to end cleanly within the
// shutdown timeout.
func (s *served) stop(t *testing.T) {
	t.Helper()
	require.NoError(t, s.cmd.Process.Signal(syscall.SIGTERM))
	select {
	case <-s.exited:
		require.NoError(t, s.err, "cowork serve did not end cleanly:\n%s", s.log.text())
	case <-time.After(20 * time.Second):
		t.Fatalf("cowork serve did not end after SIGTERM:\n%s", s.log.text())
	}
}

func freeAddress(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())
	return addr
}

// send makes one request with an optional token and JSON body and returns
// its status.
func send(t *testing.T, method, url, token, body string, headers ...string) int {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	require.NoError(t, err)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	res, err := plain.Do(req)
	require.NoError(t, err)
	_, _ = io.Copy(io.Discard, res.Body)
	res.Body.Close()
	return res.StatusCode
}

var (
	sampleLine = regexp.MustCompile(`^([a-zA-Z_:][a-zA-Z0-9_:]*)(?:\{(.*)\})? (\S+)$`)
	labelPair  = regexp.MustCompile(`([a-zA-Z_][a-zA-Z0-9_]*)="((?:[^"\\]|\\.)*)"`)
	// plain keeps no connection open and opens none ahead: the server's
	// shutdown waits up to five seconds for a connection that was opened and
	// never used, which a keep-alive client's spare dial is.
	plain = &http.Client{Transport: &http.Transport{DisableKeepAlives: true}, Timeout: 10 * time.Second}
)

// scrape reads the metrics listener as Prometheus does and parses the text
// format's samples.
func scrape(t *testing.T, addr string) []metrics.Sample {
	t.Helper()
	samples, err := scrapeOnce(addr)
	require.NoError(t, err)
	require.NotEmpty(t, samples)
	return samples
}

// scrapeOnce is scrape for a condition that polls: it fails nothing.
func scrapeOnce(addr string) ([]metrics.Sample, error) {
	res, err := plain.Get("http://" + addr + "/metrics")
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/plain") {
		return nil, fmt.Errorf("a scrape answered %d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	var out []metrics.Sample
	for _, line := range strings.Split(string(body), "\n") {
		m := sampleLine.FindStringSubmatch(line)
		if m == nil || strings.HasPrefix(line, "#") {
			continue
		}
		value, err := strconv.ParseFloat(m[3], 64)
		if err != nil {
			return nil, fmt.Errorf("%q: %w", line, err)
		}
		labels := map[string]string{}
		for _, pair := range labelPair.FindAllStringSubmatch(m[2], -1) {
			labels[pair[1]] = strings.ReplaceAll(pair[2], `\"`, `"`)
		}
		out = append(out, metrics.Sample{Name: m[1], Labels: labels, Value: value})
	}
	return out, nil
}

// version is the schema version the isolated database records.
func (i isolated) version(t *testing.T) uint {
	t.Helper()
	var v int64
	require.NoError(t, i.F.QueryRow(context.Background(), "SELECT version FROM "+store.MigrationsTable).Scan(&v))
	return uint(v)
}
