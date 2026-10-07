// Command cowork is the backend binary: it applies the database migrations
// and serves the JSON API. The web UI is the frontend container.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/guided-traffic/cowork/backend/internal/api"
	"github.com/guided-traffic/cowork/backend/internal/bootstrap"
	"github.com/guided-traffic/cowork/backend/internal/config"
	"github.com/guided-traffic/cowork/backend/internal/events"
	"github.com/guided-traffic/cowork/backend/internal/httpserver"
	"github.com/guided-traffic/cowork/backend/internal/llm"
	"github.com/guided-traffic/cowork/backend/internal/metrics"
	"github.com/guided-traffic/cowork/backend/internal/oidc"
	"github.com/guided-traffic/cowork/backend/internal/storage"
	"github.com/guided-traffic/cowork/backend/internal/store"
)

// Set by the linker; see the Containerfile and the Makefile.
var (
	version   = "dev"
	commit    = "unknown"
	buildTime = "unknown"
)

const usageText = `Usage: cowork <command>

Commands:
  serve               Apply pending migrations (unless COWORK_MIGRATE_ON_START=false), then serve the API.
  migrate             Apply pending migrations under the owner role and exit; with
                      COWORK_MIGRATE_BOOTSTRAP=true, run the bootstrap afterwards.
  check-consistency   Compare the attachments with the bucket now, print what is out of step and exit.
  version             Print the build version and exit.

Configuration is read from COWORK_* environment variables; the reference is README.md.
`

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.LookupEnv, os.Stdout, os.Stderr))
}

// run dispatches the command line. It is separated from main so a test can
// drive it with its own environment and writers.
func run(ctx context.Context, args []string, lookup func(string) (string, bool), stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usageText)
		return 2
	}
	switch args[0] {
	case "version":
		fmt.Fprintf(stdout, "cowork %s (commit %s, built %s)\n", version, commit, buildTime)
		return 0
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usageText)
		return 0
	case "migrate":
		return runMigrate(ctx, lookup, stderr)
	case "serve":
		return runServe(ctx, lookup, stderr)
	case "check-consistency":
		return runCheckConsistency(ctx, lookup, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "cowork: unknown command %q\n\n%s", args[0], usageText)
		return 2
	}
}

// runMigrate applies the pending migrations as the owner role and, with
// COWORK_MIGRATE_BOOTSTRAP=true, runs the bootstrap after the schema step, as
// the runtime role and under the bootstrap's own advisory lock — the one
// `cowork serve` takes at its start — with the same configuration
// (docs/adr/0057 D4). Without it the run never touches the bootstrap: the
// chart's init container, which is given no administrator, must not deactivate
// the one the serving container keeps.
func runMigrate(ctx context.Context, lookup func(string) (string, bool), stderr io.Writer) int {
	cfg, err := config.Load(lookup)
	if err == nil && cfg.DatabaseOwnerURL == "" {
		err = fmt.Errorf("%s is required by cowork migrate", config.OwnerConnection())
	}
	if err != nil {
		fmt.Fprintf(stderr, "cowork: %v\n", err)
		return 1
	}
	logger := newLogger(cfg, stderr)
	if err := migrateDatabase(ctx, cfg, logger); err != nil {
		logger.Error("migration failed", "error", err)
		return 1
	}
	if !cfg.MigrateBootstrap {
		return 0
	}
	if err := bootstrapAfterMigration(ctx, cfg, logger); err != nil {
		logger.Error("bootstrap failed", "error", err)
		return 1
	}
	return 0
}

// bootstrapAfterMigration runs the bootstrap of `cowork serve` in the
// migration run: on a pool of the runtime role of its own, closed when it is
// done.
func bootstrapAfterMigration(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	db, err := store.Open(ctx, cfg.DatabaseURL, store.Options{Logger: logger})
	if err != nil {
		return fmt.Errorf("connect as the runtime role: %w", err)
	}
	defer db.Close()
	if err := bootstrap.Sync(ctx, db, bootstrapParams(cfg), logger); err != nil {
		return err
	}
	logger.Info("the bootstrap ran after the migration", "variable", config.EnvMigrateBootstrap)
	return nil
}

// bootstrapParams is what the configuration says the installation starts with
// (docs/adr/0032): the local administrator, the bootstrap tenant and the
// identity provider's administrator group.
func bootstrapParams(cfg config.Config) bootstrap.Params {
	params := bootstrap.Params{
		Username: cfg.LocalAdminUsername, Password: cfg.LocalAdminPassword,
		TenantSlug: cfg.BootstrapTenantSlug, TenantName: cfg.BootstrapTenantName,
	}
	if cfg.OIDC != nil {
		params.AdminGroup = cfg.OIDC.AdminGroup
	}
	return params
}

func runServe(ctx context.Context, lookup func(string) (string, bool), stderr io.Writer) int {
	cfg, err := config.Load(lookup)
	if err == nil {
		err = requireForServe(cfg)
	}
	if err != nil {
		fmt.Fprintf(stderr, "cowork: %v\n", err)
		return 1
	}
	logger := newLogger(cfg, stderr)

	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if cfg.MigrateOnStart {
		if err := migrateDatabase(ctx, cfg, logger); err != nil {
			logger.Error("migration failed", "error", err)
			return 1
		}
	} else {
		logger.Info("migrations skipped on start", "reason", config.EnvMigrateOnStart+"=false")
	}

	m := metricsOf(cfg)
	db, err := store.Open(ctx, cfg.DatabaseURL, store.Options{Logger: logger, Metrics: m})
	if err != nil {
		logger.Error("database connection failed", "error", err)
		return 1
	}
	defer db.Close()

	if err := checkDatabase(ctx, db, logger); err != nil {
		logger.Error("database check failed", "error", err)
		return 1
	}

	// The identity provider is discovered before anything else is kept: a
	// configured issuer that cannot be discovered refuses the start, like an
	// invalid configuration value (docs/adr/0029 D4).
	identity, err := discoverIssuer(ctx, cfg, logger)
	if err != nil {
		logger.Error("identity provider discovery failed", "error", err)
		return 1
	}

	// The configured administrator and bootstrap tenant, after the migrations and
	// before the first request (docs/adr/0032 D2, D8). The migration Job of the
	// chart has run it already when it migrated; a start that finds everything
	// in step changes nothing, and a Secret rotated since reaches the account
	// here, at the restart (docs/adr/0057 D4).
	if err := bootstrap.Sync(ctx, db, bootstrapParams(cfg), logger); err != nil {
		logger.Error("bootstrap failed", "error", err)
		return 1
	}

	hub := events.New(cfg.SSEReplayWindow, cfg.SSEMaxStreamsPerPerson, m)
	go db.Listen(ctx, hub.Publish, hub.SetUp)
	objects, err := objectStorage(cfg, logger)
	if err != nil {
		logger.Error("object storage setup failed", "error", err)
		return 1
	}
	if len(cfg.TrustedProxies) > 0 {
		logger.Info("client addresses are read through trusted proxies", "variable", config.EnvTrustedProxies, "networks", cfg.TrustedProxies)
	}
	// The chat's tool calls go through the whole server, which is built
	// after the API it serves: the loop reads it at the first turn.
	var root http.Handler
	chatOptions, err := chatOf(ctx, cfg, func() http.Handler { return root }, logger)
	if err != nil {
		logger.Error("chat setup failed", "error", err)
		return 1
	}
	apiHandler, err := api.New(api.Options{
		DB:                     db,
		Logger:                 logger,
		Version:                version,
		Commit:                 commit,
		BuildTime:              buildTime,
		SessionKey:             cfg.SessionKey,
		MaxJSONBody:            cfg.MaxJSONBody,
		RequestTimeout:         cfg.RequestTimeout,
		MaxPageSize:            cfg.MaxPageSize,
		MaxQueryLength:         cfg.MaxQueryLength,
		Storage:                objects,
		AttachmentMaxBytes:     cfg.AttachmentMaxBytes,
		AttachmentMaxPerTicket: cfg.AttachmentMaxPerTicket,
		AttachmentTenantQuota:  cfg.AttachmentTenantQuota,
		MaxImportBytes:         cfg.MaxImportBytes,
		Events:                 hub,

		BaseOrigin:           cfg.BaseOrigin,
		SessionLifetime:      cfg.SessionLifetime,
		SessionIdle:          cfg.SessionIdle,
		PasswordMinLength:    cfg.PasswordMinLength,
		LoginLockout:         cfg.LoginLockout,
		LoginMaxFailures:     cfg.LoginMaxFailures,
		LoginAddressLimit:    cfg.LoginAddressLimit,
		TokenDefaultLifetime: cfg.TokenDefaultLifetime,
		TokenMaxLifetime:     cfg.TokenMaxLifetime,
		TrustedProxies:       cfg.TrustedProxies,
		OIDC:                 identity,
		Chat:                 chatOptions,
		Metrics:              m,
	})
	if err != nil {
		logger.Error("API setup failed", "error", err)
		return 1
	}
	root = httpserver.New(httpserver.Options{Ready: db.Ping, API: apiHandler, Logger: logger, Metrics: m})
	go runJobs(ctx, db, objects, logger, cfg.SessionIdle)

	if err := serve(ctx, cfg, root, m, hub, logger); err != nil {
		logger.Error("server stopped with error", "error", err)
		return 1
	}
	logger.Info("server stopped")
	return 0
}

// metricsOf is the registry of the backend's instruments, or nil — nothing
// recorded — while COWORK_METRICS_ADDR switches the listener that would serve
// them off (docs/adr/0060 D1).
func metricsOf(cfg config.Config) *metrics.Metrics {
	if cfg.MetricsAddr == "" {
		return nil
	}
	return metrics.New()
}

// serve binds the API listener and, unless COWORK_METRICS_ADDR switches it
// off, the metrics listener, before either serves, so that a port that is
// taken refuses the start; the two then share one lifecycle — the signal shuts
// both down within the shutdown timeout, and one that fails stops the other
// (docs/adr/0060 D1). The event streams end as the API listener's shutdown
// begins (docs/adr/0054 D9).
func serve(ctx context.Context, cfg config.Config, root http.Handler, m *metrics.Metrics, hub *events.Hub, logger *slog.Logger) error {
	listeners, err := bind(cfg, root, m, logger)
	if err != nil {
		return err
	}
	listeners[0].OnShutdown = []func(){hub.Close}
	logger.Info("listening", "addr", cfg.ListenAddr, "version", version, "commit", commit)
	if m != nil {
		logger.Info("metrics listening", "addr", cfg.MetricsAddr, "path", "/metrics")
	} else {
		logger.Info("the metrics listener is off", "variable", config.EnvMetricsAddr)
	}
	return httpserver.ServeAll(ctx, cfg.ShutdownTimeout, listeners...)
}

// bind opens the API listener, first, and the metrics listener when m is set.
func bind(cfg config.Config, root http.Handler, m *metrics.Metrics, logger *slog.Logger) ([]httpserver.Listener, error) {
	api, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		return nil, err
	}
	listeners := make([]httpserver.Listener, 0, 2)
	listeners = append(listeners, httpserver.Listener{Listener: api, Handler: root})
	if m == nil {
		return listeners, nil
	}
	scrape, err := net.Listen("tcp", cfg.MetricsAddr)
	if err != nil {
		_ = api.Close()
		return nil, fmt.Errorf("the metrics listener (%s): %w", config.EnvMetricsAddr, err)
	}
	return append(listeners, httpserver.Listener{Listener: scrape, Handler: httpserver.NewMetrics(m.Handler(logger), logger)}), nil
}

// objectStorage is the configuration's object storage, or nil without one,
// which the start says (docs/adr/0016 D1).
func objectStorage(cfg config.Config, logger *slog.Logger) (*storage.Client, error) {
	if cfg.Storage == nil {
		logger.Warn("no object storage configured; attachments cannot be uploaded", "variable", config.EnvS3Endpoint)
		return nil, nil
	}
	return storage.New(*cfg.Storage)
}

// chatOf is the chat of the configuration (docs/adr/0076), or nil without a
// provider. The start says which providers and models the chat talks to —
// never a URL or a key. The signal that ends the server ends the turns that
// run (docs/adr/0054 D9).
func chatOf(ctx context.Context, cfg config.Config, root func() http.Handler, logger *slog.Logger) (*api.ChatOptions, error) {
	c := cfg.Chat
	if c == nil {
		return nil, nil
	}
	opts := &api.ChatOptions{TurnTimeout: c.TurnTimeout, MaxSteps: c.MaxSteps, TurnsPerPerson: c.Turns, Shutdown: ctx, Loopback: root}
	for _, p := range c.Providers {
		provider, err := llm.New(llm.Config{Format: p.Kind, URL: p.URL, APIKey: p.APIKey, Model: p.Model})
		if err != nil {
			return nil, err
		}
		opts.Providers = append(opts.Providers, api.ChatProvider{ID: p.ID, Name: p.Name, Kind: p.Kind, Model: p.Model, Provider: provider})
		logger.Info("the chat talks to a model", "variable", config.EnvChatProviders, "provider", p.ID, "kind", p.Kind, "model", p.Model)
	}
	logger.Info("the chat's limits", "turn_timeout", c.TurnTimeout, "max_steps", c.MaxSteps, "turns_per_person", c.Turns)
	return opts, nil
}

// discoverIssuer discovers the configured identity provider (docs/adr/0029 D1,
// D4), or returns the zero options when none is configured. A gate that admits
// nobody is said once at the start: the login page then offers no button
// (docs/adr/0030 D8).
func discoverIssuer(ctx context.Context, cfg config.Config, logger *slog.Logger) (api.OIDCOptions, error) {
	o := cfg.OIDC
	if o == nil {
		return api.OIDCOptions{}, nil
	}
	discoverCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	provider, err := oidc.Discover(discoverCtx, oidc.Config{
		Issuer: o.Issuer, ClientID: o.ClientID, ClientSecret: o.ClientSecret, RedirectURL: cfg.BaseOrigin + "/auth/callback",
		Scopes: o.Scopes, GroupsClaim: o.GroupsClaim, Logger: logger,
	})
	if err != nil {
		return api.OIDCOptions{}, err
	}
	if !o.Admits() {
		logger.Warn("the identity provider's gate admits nobody: name a group in COWORK_OIDC_ALLOWED_GROUPS or COWORK_ADMIN_GROUP")
	}
	logger.Info("identity provider discovered", "issuer", o.Issuer, "allowed_groups", len(o.AllowedGroups),
		"administrator_group", o.AdminGroup != "", "groups_refresh", o.GroupsRefresh, "groups_max_age", o.GroupsMaxAge,
		"email_trusted", o.EmailTrusted)
	return api.OIDCOptions{Provider: provider, AllowedGroups: o.AllowedGroups, AdminGroup: o.AdminGroup,
		GroupsRefresh: o.GroupsRefresh, GroupsMaxAge: o.GroupsMaxAge, EmailTrusted: o.EmailTrusted,
		DisplayName: o.DisplayName}, nil
}

// requireForServe checks what only `cowork serve` needs: the server key, the
// owner role's connection while it migrates on start, and the client of a
// configured identity provider, which the start's discovery and every login
// use.
func requireForServe(cfg config.Config) error {
	var errs []error
	if cfg.SessionKey == nil {
		errs = append(errs, fmt.Errorf("%s is required by cowork serve", config.EnvSessionKey))
	}
	if cfg.MigrateOnStart && cfg.DatabaseOwnerURL == "" {
		errs = append(errs, fmt.Errorf("%s is required while %s is true; the chart migrates in an init container or a Job and sets it to false",
			config.OwnerConnection(), config.EnvMigrateOnStart))
	}
	if err := cfg.OIDC.RequireClient(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// runCheckConsistency runs the consistency check of the attachments once, now,
// whatever its schedule says — the step of a restore (docs/adr/0059 D5) —
// and prints what it found in every tenant. It needs the runtime role's URL
// and the object storage, as the server has them; in the chart it runs in a
// backend container (`kubectl exec … -- /app/cowork check-consistency`).
func runCheckConsistency(ctx context.Context, lookup func(string) (string, bool), stdout, stderr io.Writer) int {
	cfg, err := config.Load(lookup)
	if err == nil && cfg.Storage == nil {
		err = fmt.Errorf("%s is required by cowork check-consistency: it compares the bucket with the database", config.EnvS3Endpoint)
	}
	if err != nil {
		fmt.Fprintf(stderr, "cowork: %v\n", err)
		return 1
	}
	logger := newLogger(cfg, stderr)
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	db, err := store.Open(ctx, cfg.DatabaseURL, store.Options{Logger: logger})
	if err != nil {
		logger.Error("database connection failed", "error", err)
		return 1
	}
	defer db.Close()
	if err := checkDatabase(ctx, db, logger); err != nil {
		logger.Error("database check failed", "error", err)
		return 1
	}
	objects, err := storage.New(*cfg.Storage)
	if err != nil {
		logger.Error("object storage setup failed", "error", err)
		return 1
	}
	run, err := db.CheckConsistency(ctx, objects, time.Now())
	if err != nil {
		logger.Error("consistency check failed", "job", store.JobConsistencyCheck, "error", err)
		return 1
	}
	if !run.Ran {
		fmt.Fprintln(stderr, "cowork: another replica is running the consistency check; run it again once that is done")
		return 1
	}
	printConsistency(stdout, run)
	return 0
}

// printConsistency writes what a run found: the run in all, then every
// tenant by its slug and id with its counts — never a file name.
func printConsistency(w io.Writer, run store.ConsistencyRun) {
	var dangling, accepted, orphans int
	var bytes int64
	for _, t := range run.Tenants {
		dangling, accepted, orphans, bytes = dangling+t.Dangling, accepted+t.Accepted, orphans+t.Orphans, bytes+t.OrphanBytes
	}
	fmt.Fprintf(w, "consistency check at %s: %d tenants, %d dangling, %d accepted as lost, %d orphaned objects (%d bytes)\n",
		run.At.Format(time.RFC3339), len(run.Tenants), dangling, accepted, orphans, bytes)
	for _, t := range run.Tenants {
		fmt.Fprintf(w, "tenant %s (%s): %d dangling, %d accepted as lost, %d orphaned objects (%d bytes)\n",
			t.Slug, t.TenantID, t.Dangling, t.Accepted, t.Orphans, t.OrphanBytes)
	}
}

// checkConsistencyWhenDue runs the consistency check of the attachments when
// it is due (docs/adr/0059 D4, D6): once a day in the hour after 03:00 UTC, and
// at a start that finds the last run older than that. Without object storage
// there is nothing to compare, and it never runs.
func checkConsistencyWhenDue(ctx context.Context, db *store.DB, objects *storage.Client, logger *slog.Logger) error {
	if objects == nil {
		return nil
	}
	last, checked, err := db.LastConsistencyCheck(ctx)
	if err != nil {
		return err
	}
	now := time.Now()
	if !store.ConsistencyCheckDue(last, checked, now) {
		return nil
	}
	run, err := db.CheckConsistency(ctx, objects, now)
	if err != nil || !run.Ran {
		return err
	}
	var dangling, orphans int
	for _, t := range run.Tenants {
		dangling, orphans = dangling+t.Dangling, orphans+t.Orphans
		if t.Dangling > 0 || t.Orphans > 0 {
			logger.Warn("the attachments of a tenant are out of step with the bucket", "job", store.JobConsistencyCheck,
				"tenant", t.Slug, "dangling", t.Dangling, "accepted", t.Accepted, "orphans", t.Orphans, "orphan_bytes", t.OrphanBytes)
		}
	}
	logger.Info("consistency check done", "job", store.JobConsistencyCheck, "tenants", len(run.Tenants),
		"dangling", dangling, "orphans", orphans)
	return nil
}

// runJobs runs the background jobs on their tickers until ctx ends; each job
// holds its own advisory lock, so every replica may tick (docs/adr/0027 D5).
// The sessions and the login's attempts are cleaned up here; neither is
// enforced by the cleanup — a session past a limit is refused at its next
// request, a lock that ended holds nothing at the next attempt. The purge of
// the tickets deleted thirty days ago is here too (docs/adr/0024 D2): their
// attachment objects go once the purge committed, and each purged key is
// logged. The consistency check of the attachments is asked every hour whether
// it is due, and runs once a day (docs/adr/0059 D4, D6). A job's name in the
// log is its system actor's, which the metrics name it by too
// (docs/adr/0060 D4).
func runJobs(ctx context.Context, db *store.DB, objects *storage.Client, logger *slog.Logger, sessionIdle time.Duration) {
	expiry := time.NewTicker(time.Hour)
	defer expiry.Stop()
	jobs := []struct {
		name string
		run  func(ctx context.Context) (int64, error)
	}{
		{"idempotency-expiry", db.ExpireIdempotencyKeys},
		{"session-expiry", func(ctx context.Context) (int64, error) { return db.ExpireSessions(ctx, time.Now(), sessionIdle) }},
		{"login-expiry", func(ctx context.Context) (int64, error) {
			return db.ExpireLoginState(ctx, time.Now(), store.LoginWindow)
		}},
		{"notification-expiry", func(ctx context.Context) (int64, error) { return db.ExpireNotifications(ctx, time.Now()) }},
		{"github-delivery-expiry", func(ctx context.Context) (int64, error) { return db.ExpireGitHubDeliveries(ctx, time.Now()) }},
		{"import-expiry", func(ctx context.Context) (int64, error) { return db.ExpireImportJobs(ctx, time.Now()) }},
		{"ticket-purge", func(ctx context.Context) (int64, error) {
			purged, err := db.PurgeDeletedTickets(ctx, time.Now())
			for _, p := range purged {
				logger.Info("ticket purged", "job", "ticket-purge", "ticket", p.Key, "attachments", len(p.Attachments))
			}
			api.RemovePurgedObjects(ctx, objects, logger, purged)
			return int64(len(purged)), err
		}},
		{store.JobConsistencyCheck, func(ctx context.Context) (int64, error) {
			return 0, checkConsistencyWhenDue(ctx, db, objects, logger)
		}},
	}
	for {
		for _, job := range jobs {
			if removed, err := job.run(ctx); err != nil && ctx.Err() == nil {
				logger.Error("job failed", "job", job.name, "error", err)
			} else if removed > 0 {
				logger.Info("job removed expired rows", "job", job.name, "removed", removed)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-expiry.C:
		}
	}
}

// checkDatabase refuses a runtime role that could bypass row-level security
// (docs/adr/0021 D2) and a schema the binary cannot serve: a dirty one, or one
// with pending migrations (docs/adr/0057 D3). A schema ahead of the binary is
// served: an image rollback over a newer schema is safe because no migration
// removes what the previous release reads (docs/adr/0028).
func checkDatabase(ctx context.Context, db *store.DB, logger *slog.Logger) error {
	if err := db.CheckRuntimeRole(ctx); err != nil {
		return err
	}
	state, err := db.SchemaState(ctx)
	if err != nil {
		return err
	}
	if state.Dirty {
		return fmt.Errorf("schema version %d is dirty: a previous migration failed halfway and needs a manual repair", state.Version)
	}
	pending, err := state.Pending()
	if err != nil {
		return err
	}
	if pending > 0 {
		return fmt.Errorf("pending migrations: %d; run the migration job (or set %s=true)", pending, config.EnvMigrateOnStart)
	}
	if state.Ahead() {
		logger.Warn("database schema is ahead of this binary; serving it", "database", state.Version, "binary", state.Embedded)
	}
	return nil
}

func migrateDatabase(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	runtimeRole, err := store.RoleFromURL(cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("%s: %w", config.EnvDatabaseURL, err)
	}
	res, err := store.Migrate(ctx, cfg.DatabaseOwnerURL, runtimeRole)
	if err != nil {
		return err
	}
	if res.Ahead {
		logger.Warn("database schema is ahead of this binary; nothing applied", "version", res.Version)
		return nil
	}
	logger.Info("database schema is current", "version", res.Version, "applied", res.Applied)
	return nil
}

func newLogger(cfg config.Config, w io.Writer) *slog.Logger {
	opts := &slog.HandlerOptions{Level: cfg.LogLevel}
	if cfg.LogFormat == config.LogFormatText {
		return slog.New(slog.NewTextHandler(w, opts))
	}
	return slog.New(slog.NewJSONHandler(w, opts))
}
