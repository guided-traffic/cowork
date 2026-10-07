// Package metrics holds the backend's Prometheus instruments
// (docs/adr/0060): a registry of its own — never the client library's
// default one — with the instruments of the first release (D4) and the Go
// runtime and process collectors, which the metrics listener serves as
// Prometheus text (D1).
//
// The rest of the backend records through the small typed methods of
// Metrics, so that no other package imports the client library and every label
// value comes from a fixed set, from the API document or from the code: no
// person, ticket key, token or request id is ever a label, and a route is its
// pattern, never the path a client sent (D5). A nil *Metrics records nothing,
// for a test or a tool that does not care.
package metrics

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// namespace is the first part of every instrument's name:
// cowork_<subsystem>_<name>_<unit> (docs/adr/0060 D4).
const namespace = "cowork"

// Unmatched is the route of a request no route matched: an unknown path, a
// wrong method on an API path. It keeps a client's paths out of the label.
const Unmatched = "unmatched"

// maxScrapesInFlight is how many scrapes the metrics listener serves at once;
// more are answered 503. The listener has no authentication (D1), so whoever
// reaches it cannot make it gather without bound.
const maxScrapesInFlight = 4

// Actor is who an act is attributed to (docs/adr/0026 D2): a person, a
// person's agent, or a system actor.
type Actor string

// The actors of an act.
const (
	ActorPerson Actor = "person"
	ActorAgent  Actor = "agent"
	ActorSystem Actor = "system"
)

// LoginMethod is how a person logs in.
type LoginMethod string

// The login methods: the local form and the identity provider's code flow.
const (
	LoginLocal LoginMethod = "local"
	LoginOIDC  LoginMethod = "oidc"
)

// LoginOutcome is how a login ended.
type LoginOutcome string

// The outcomes of a login. Locked and throttled are the local form's alone.
const (
	// LoginSuccess made a session.
	LoginSuccess LoginOutcome = "success"
	// LoginFailure: a wrong password, an unknown or deactivated account; the
	// identity provider's state, code or token that did not hold.
	LoginFailure LoginOutcome = "failure"
	// LoginLocked met a locked username.
	LoginLocked LoginOutcome = "locked"
	// LoginThrottled was refused by the address throttle before any password
	// was checked.
	LoginThrottled LoginOutcome = "throttled"
	// LoginRefused proved who it was and was refused all the same: the init
	// state, the identity provider's gate.
	LoginRefused LoginOutcome = "refused"
)

// TokenRefusal is why a personal access token was refused.
type TokenRefusal string

// The reasons a token is refused.
const (
	// TokenMalformed: no bearer credential, or none of a token's shape.
	TokenMalformed TokenRefusal = "malformed"
	// TokenUnknown: no token has this hash.
	TokenUnknown TokenRefusal = "unknown"
	// TokenRevoked: revoked, or its person deactivated.
	TokenRevoked TokenRefusal = "revoked"
	// TokenExpired: past its lifetime.
	TokenExpired TokenRefusal = "expired"
	// TokenNotAllowed: its person is outside the identity provider's gate.
	TokenNotAllowed TokenRefusal = "not_allowed"
	// TokenSessionOnly: a usable token on a route that takes a session only.
	TokenSessionOnly TokenRefusal = "session_only"
)

// StreamDrop is why the hub ended an event stream (docs/adr/0054).
type StreamDrop string

// The reasons a stream is ended by the hub.
const (
	// DropBehind: the stream fell its buffer behind and was told to resync
	// (docs/adr/0054 D4).
	DropBehind StreamDrop = "behind"
	// DropLimit: the person opened one stream more than the limit, and the
	// oldest was closed (D8).
	DropLimit StreamDrop = "limit"
	// DropResync: the listener came back after a loss, and every stream was
	// told to resync (D4, D5).
	DropResync StreamDrop = "resync"
)

// Metrics is the registry and its instruments. Its zero value is not
// usable; use New. Every method may be called on a nil *Metrics.
type Metrics struct {
	registry *prometheus.Registry
	// names are the families of the backend's own instruments, in the order
	// they were made: what a test exercises and the dashboard may name.
	names []string

	httpRequests *prometheus.CounterVec
	httpDuration *prometheus.HistogramVec
	httpInFlight prometheus.Gauge

	pool          *poolCollector
	queryErrors   *prometheus.CounterVec
	schema        *schemaCollector
	jobRuns       *prometheus.CounterVec
	jobFailures   *prometheus.CounterVec
	jobDuration   *prometheus.HistogramVec
	jobFailingRun *prometheus.GaugeVec

	streams   prometheus.Gauge
	published prometheus.Counter
	dropped   *prometheus.CounterVec
	replays   *prometheus.CounterVec

	acts *prometheus.CounterVec

	logins   *prometheus.CounterVec
	lockouts prometheus.Counter
	refusals *prometheus.CounterVec
}

// New makes a registry of its own with every instrument of the first release
// and the Go runtime and process collectors (docs/adr/0060 D4).
func New() *Metrics {
	m := &Metrics{registry: prometheus.NewRegistry()}
	m.registry.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	m.httpInstruments()
	m.dbInstruments()
	m.jobInstruments()
	m.eventInstruments()
	m.auditInstruments()
	m.authInstruments()
	return m
}

// Names are the families of the backend's own instruments, the cowork_
// ones; the Go runtime and process collectors' are not among them.
func (m *Metrics) Names() []string {
	if m == nil {
		return nil
	}
	return append([]string(nil), m.names...)
}

// Handler answers a scrape with the registry in the Prometheus text format.
// A collector that fails leaves its families out and the rest is served; the
// failure goes to logger.
func (m *Metrics) Handler(logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{
		ErrorLog:            slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
		ErrorHandling:       promhttp.ContinueOnError,
		MaxRequestsInFlight: maxScrapesInFlight,
	})
}

func (m *Metrics) name(subsystem, name string) string {
	full := prometheus.BuildFQName(namespace, subsystem, name)
	m.names = append(m.names, full)
	return full
}

func (m *Metrics) counter(subsystem, name, help string, labels ...string) *prometheus.CounterVec {
	c := prometheus.NewCounterVec(prometheus.CounterOpts{Name: m.name(subsystem, name), Help: help}, labels)
	m.registry.MustRegister(c)
	return c
}

func (m *Metrics) gauge(subsystem, name, help string, labels ...string) *prometheus.GaugeVec {
	g := prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: m.name(subsystem, name), Help: help}, labels)
	m.registry.MustRegister(g)
	return g
}

func (m *Metrics) histogram(subsystem, name, help string, buckets []float64, labels ...string) *prometheus.HistogramVec {
	h := prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: m.name(subsystem, name), Help: help, Buckets: buckets}, labels)
	m.registry.MustRegister(h)
	return h
}

// initialise makes each label value of a closed set exist at zero, so that a
// rate over it is defined from the start and a dashboard shows the zero.
func initialise[T ~string](c *prometheus.CounterVec, values ...T) {
	for _, v := range values {
		c.WithLabelValues(string(v))
	}
}

// --- http ---------------------------------------------------------------

func (m *Metrics) httpInstruments() {
	m.httpRequests = m.counter("http", "requests_total",
		"HTTP requests answered on the API listener, by route pattern, method and status.", "route", "method", "status")
	m.httpDuration = m.histogram("http", "request_duration_seconds",
		"Time from a request's arrival to its handler's return, by route pattern and method; a stream counts its whole life.",
		prometheus.DefBuckets, "route", "method")
	m.httpInFlight = m.gauge("http", "requests_in_flight",
		"Requests the API listener is serving, open event streams and turns of the chat among them.").WithLabelValues()
}

type routeKey struct{}

// route is where the handler of a request names its route for the record
// Request began; the handler's goroutine writes it before the record ends.
type route struct{ pattern string }

// Request begins the record of an HTTP request (docs/adr/0060 D4): the
// request counts as in flight until the returned function ends the record with
// the status it was answered with — 0 for none written, which is 200. The
// returned context carries the place SetRoute names the route in; a request
// whose route nobody names is recorded as Unmatched.
func (m *Metrics) Request(ctx context.Context, method string) (context.Context, func(status int)) {
	if m == nil {
		return ctx, func(int) {}
	}
	start := time.Now()
	m.httpInFlight.Inc()
	r := &route{}
	ctx = context.WithValue(ctx, routeKey{}, r)
	return ctx, func(status int) {
		m.httpInFlight.Dec()
		if status == 0 {
			status = http.StatusOK
		}
		pattern := r.pattern
		if pattern == "" {
			pattern = Unmatched
		}
		verb := methodLabel(method)
		m.httpRequests.WithLabelValues(pattern, verb, strconv.Itoa(status)).Inc()
		m.httpDuration.WithLabelValues(pattern, verb).Observe(time.Since(start).Seconds())
	}
}

// SetRoute names the route of the request ctx belongs to: the pattern the API
// document or the server's mux matched, such as
// /api/v1/tenants/{tenant}/projects/{project}/tickets/{number} — never the
// path the client sent (docs/adr/0060 D5). Outside a recorded request it does
// nothing.
func SetRoute(ctx context.Context, pattern string) {
	if r, ok := ctx.Value(routeKey{}).(*route); ok {
		r.pattern = pattern
	}
}

// methodLabel keeps the method label to the methods HTTP defines: a client may
// send any token as its method.
func methodLabel(method string) string {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete,
		http.MethodOptions, http.MethodConnect, http.MethodTrace:
		return method
	}
	return "other"
}

// --- db -------------------------------------------------------------------

// PoolStats is what the database pool says of itself at a scrape (pgxpool's
// Stat): its connections, and its acquires — every one, and those that had to
// wait because no connection was free — with the time they took.
type PoolStats struct {
	Idle, InUse, Constructing, Max int32
	// Acquires and AcquireTime are the successful acquires and their total
	// duration.
	Acquires    int64
	AcquireTime time.Duration
	// Waits and WaitTime are the successful acquires that waited for a
	// connection to be released or made, the pool being empty, and the total
	// time they waited.
	Waits    int64
	WaitTime time.Duration
	// Canceled are the acquires a context ended before a connection was free.
	Canceled int64
}

func (m *Metrics) dbInstruments() {
	m.pool = &poolCollector{
		conns: prometheus.NewDesc(m.name("db", "pool_connections"),
			"Connections of the database pool by state: idle, in_use, constructing.", []string{"state"}, nil),
		max: prometheus.NewDesc(m.name("db", "pool_max_connections"),
			"The most connections the database pool opens (pool_max_conns of COWORK_DATABASE_URL).", nil, nil),
		acquire: prometheus.NewDesc(m.name("db", "pool_acquire_duration_seconds"),
			"Successful acquires of a pool connection and the time they took.", nil, nil),
		wait: prometheus.NewDesc(m.name("db", "pool_wait_duration_seconds"),
			"Successful acquires that waited for a connection, the pool being empty, and the time they waited; its rate is the mean number waiting.",
			nil, nil),
		canceled: prometheus.NewDesc(m.name("db", "pool_canceled_acquires_total"),
			"Acquires of a pool connection that a context ended before one was free.", nil, nil),
	}
	m.registry.MustRegister(m.pool)
	m.queryErrors = m.counter("db", "query_errors_total",
		"Statements that failed, by the kind of failure: the SQLSTATE's meaning, or what the client saw.", "kind")
	m.schema = &schemaCollector{
		version: prometheus.NewDesc(m.name("migrations", "schema_version"),
			"The schema version the database records.", nil, nil),
		dirty: prometheus.NewDesc(m.name("migrations", "schema_dirty"),
			"1 while the recorded schema version is dirty: a migration failed halfway, or one is running.", nil, nil),
	}
	m.registry.MustRegister(m.schema)
}

// ObservePool makes every scrape read the database pool's statistics through
// stat; a later call replaces an earlier one.
func (m *Metrics) ObservePool(stat func() PoolStats) {
	if m == nil {
		return
	}
	m.pool.mu.Lock()
	defer m.pool.mu.Unlock()
	m.pool.stat = stat
}

// QueryError counts a failed statement by its kind, a value of the store's
// closed set.
func (m *Metrics) QueryError(kind string) {
	if m == nil {
		return
	}
	m.queryErrors.WithLabelValues(kind).Inc()
}

type poolCollector struct {
	mu                                  sync.Mutex
	stat                                func() PoolStats
	conns, max, acquire, wait, canceled *prometheus.Desc
}

func (c *poolCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range []*prometheus.Desc{c.conns, c.max, c.acquire, c.wait, c.canceled} {
		ch <- d
	}
}

func (c *poolCollector) Collect(ch chan<- prometheus.Metric) {
	c.mu.Lock()
	stat := c.stat
	c.mu.Unlock()
	if stat == nil {
		return
	}
	s := stat()
	ch <- prometheus.MustNewConstMetric(c.conns, prometheus.GaugeValue, float64(s.Idle), "idle")
	ch <- prometheus.MustNewConstMetric(c.conns, prometheus.GaugeValue, float64(s.InUse), "in_use")
	ch <- prometheus.MustNewConstMetric(c.conns, prometheus.GaugeValue, float64(s.Constructing), "constructing")
	ch <- prometheus.MustNewConstMetric(c.max, prometheus.GaugeValue, float64(s.Max))
	ch <- prometheus.MustNewConstSummary(c.acquire, uint64(max(s.Acquires, 0)), s.AcquireTime.Seconds(), nil)
	ch <- prometheus.MustNewConstSummary(c.wait, uint64(max(s.Waits, 0)), s.WaitTime.Seconds(), nil)
	ch <- prometheus.MustNewConstMetric(c.canceled, prometheus.CounterValue, float64(s.Canceled))
}

// schemaReadEvery is how long a schema state read for a scrape is reused: the
// listener has no authentication, and a scrape must not become a query per
// request. schemaReadTimeout bounds the read.
const (
	schemaReadEvery   = 10 * time.Second
	schemaReadTimeout = 2 * time.Second
)

// ObserveSchema makes a scrape read the schema's recorded version and dirty
// flag through read, at most once every ten seconds and within two; a read that
// fails leaves both out of the scrapes until the next read
// (docs/adr/0060 D4, D6). The flag is read while the backend serves, so a
// migration of a newer release that failed halfway beside it shows.
func (m *Metrics) ObserveSchema(read func(ctx context.Context) (version uint, dirty bool, err error)) {
	if m == nil {
		return
	}
	m.schema.mu.Lock()
	defer m.schema.mu.Unlock()
	m.schema.read, m.schema.at = read, time.Time{}
}

type schemaCollector struct {
	version, dirty *prometheus.Desc
	// now is the clock the reuse of a read is measured by; nil is time.Now.
	now func() time.Time

	mu   sync.Mutex
	read func(ctx context.Context) (uint, bool, error)
	at   time.Time
	ok   bool
	// last is the state the last read returned.
	last struct {
		version uint
		dirty   bool
	}
}

func (c *schemaCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.version
	ch <- c.dirty
}

func (c *schemaCollector) Collect(ch chan<- prometheus.Metric) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.read == nil {
		return
	}
	clock := c.now
	if clock == nil {
		clock = time.Now
	}
	if now := clock(); c.at.IsZero() || now.Sub(c.at) >= schemaReadEvery {
		ctx, cancel := context.WithTimeout(context.Background(), schemaReadTimeout)
		version, dirty, err := c.read(ctx)
		cancel()
		c.at, c.ok = now, err == nil
		c.last.version, c.last.dirty = version, dirty
	}
	if !c.ok {
		return
	}
	dirty := 0.0
	if c.last.dirty {
		dirty = 1
	}
	ch <- prometheus.MustNewConstMetric(c.version, prometheus.GaugeValue, float64(c.last.version))
	ch <- prometheus.MustNewConstMetric(c.dirty, prometheus.GaugeValue, dirty)
}

// --- jobs -----------------------------------------------------------------

func (m *Metrics) jobInstruments() {
	m.jobRuns = m.counter("jobs", "runs_total",
		"Runs of a background job on this replica that took the job's lock, by job.", "name")
	m.jobFailures = m.counter("jobs", "failures_total",
		"Runs of a background job that failed, by job.", "name")
	m.jobDuration = m.histogram("jobs", "duration_seconds",
		"Time a background job's run took, by job.", []float64{0.01, 0.05, 0.25, 1, 5, 30, 120, 600}, "name")
	m.jobFailingRun = m.gauge("jobs", "consecutive_failures",
		"Runs of a background job on this replica that failed one after the other since its last success, by job.", "name")
}

// JobRun records a run of the background job name — the system actor's name,
// such as session-expiry — that took the job's lock: its duration and whether
// it failed. A failure counts on the job's consecutive failures, a success
// sets them back to zero (docs/adr/0060 D6).
func (m *Metrics) JobRun(name string, took time.Duration, failed bool) {
	if m == nil {
		return
	}
	m.jobRuns.WithLabelValues(name).Inc()
	m.jobDuration.WithLabelValues(name).Observe(took.Seconds())
	if failed {
		m.jobFailures.WithLabelValues(name).Inc()
		m.jobFailingRun.WithLabelValues(name).Inc()
		return
	}
	m.jobFailures.WithLabelValues(name)
	m.jobFailingRun.WithLabelValues(name).Set(0)
}

// --- events ---------------------------------------------------------------

func (m *Metrics) eventInstruments() {
	m.streams = m.gauge("events", "open_streams",
		"Event streams this replica's hub holds.").WithLabelValues()
	m.published = m.counter("events", "published_total",
		"Notifications this replica's hub received from the database's listener.").WithLabelValues()
	m.dropped = m.counter("events", "subscribers_dropped_total",
		"Event streams the hub ended, by reason: behind (its buffer was full), limit (the person's oldest beyond the limit), resync (the listener came back after a loss).",
		"reason")
	initialise(m.dropped, DropBehind, DropLimit, DropResync)
	m.replays = m.counter("events", "replays_total",
		"Reconnects with a Last-Event-ID, by outcome: hit (replayed from the ring), miss (told to resync).", "outcome")
	initialise(m.replays, "hit", "miss")
}

// OpenStreams sets the number of event streams the hub holds.
func (m *Metrics) OpenStreams(n int) {
	if m == nil {
		return
	}
	m.streams.Set(float64(n))
}

// EventPublished counts a notification the hub received.
func (m *Metrics) EventPublished() {
	if m == nil {
		return
	}
	m.published.Inc()
}

// SubscriberDropped counts a stream the hub ended, by why.
func (m *Metrics) SubscriberDropped(reason StreamDrop) {
	if m == nil {
		return
	}
	m.dropped.WithLabelValues(string(reason)).Inc()
}

// Replay counts a reconnect with a Last-Event-ID: hit when the id was in a
// ring and the stream replayed from it, miss when it was told to resync.
func (m *Metrics) Replay(hit bool) {
	if m == nil {
		return
	}
	outcome := "miss"
	if hit {
		outcome = "hit"
	}
	m.replays.WithLabelValues(outcome).Inc()
}

// --- audit ----------------------------------------------------------------

func (m *Metrics) auditInstruments() {
	m.acts = m.counter("audit", "acts_total",
		"Acts committed to the audit record, by action and by actor: person, agent, system.", "action", "actor")
}

// Act counts an act the audit record committed (docs/adr/0026): action is
// its audit_action value.
func (m *Metrics) Act(action string, actor Actor) {
	if m == nil {
		return
	}
	m.acts.WithLabelValues(action, string(actor)).Inc()
}

// --- auth -----------------------------------------------------------------

func (m *Metrics) authInstruments() {
	m.logins = m.counter("auth", "logins_total",
		"Logins by method (local, oidc) and outcome (success, failure, locked, throttled, refused).", "method", "outcome")
	for _, method := range []LoginMethod{LoginLocal, LoginOIDC} {
		for _, outcome := range []LoginOutcome{LoginSuccess, LoginFailure, LoginLocked, LoginThrottled, LoginRefused} {
			m.logins.WithLabelValues(string(method), string(outcome))
		}
	}
	m.lockouts = m.counter("auth", "lockouts_total",
		"Usernames locked by failed attempts (COWORK_LOGIN_MAX_FAILURES).").WithLabelValues()
	m.refusals = m.counter("auth", "token_refusals_total",
		"Personal access tokens refused, by reason.", "reason")
	initialise(m.refusals, TokenMalformed, TokenUnknown, TokenRevoked, TokenExpired, TokenNotAllowed, TokenSessionOnly)
}

// Login counts a login by its method and outcome.
func (m *Metrics) Login(method LoginMethod, outcome LoginOutcome) {
	if m == nil {
		return
	}
	m.logins.WithLabelValues(string(method), string(outcome)).Inc()
}

// Lockout counts a username locked by its failures.
func (m *Metrics) Lockout() {
	if m == nil {
		return
	}
	m.lockouts.Inc()
}

// TokenRefused counts a refused token by why.
func (m *Metrics) TokenRefused(reason TokenRefusal) {
	if m == nil {
		return
	}
	m.refusals.WithLabelValues(string(reason)).Inc()
}
