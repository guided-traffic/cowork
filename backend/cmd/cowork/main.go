// Command cowork is the backend binary: it applies the database migrations
// and serves the JSON API. The web UI is the frontend container.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/guided-traffic/cowork/backend/internal/api"
	"github.com/guided-traffic/cowork/backend/internal/config"
	"github.com/guided-traffic/cowork/backend/internal/events"
	"github.com/guided-traffic/cowork/backend/internal/httpserver"
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
  serve     Apply pending migrations (unless COWORK_MIGRATE_ON_START=false), then serve the API.
  migrate   Apply pending migrations under the owner role and exit.
  version   Print the build version and exit.

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
	default:
		fmt.Fprintf(stderr, "cowork: unknown command %q\n\n%s", args[0], usageText)
		return 2
	}
}

func runMigrate(ctx context.Context, lookup func(string) (string, bool), stderr io.Writer) int {
	cfg, err := config.Load(lookup)
	if err == nil && cfg.DatabaseOwnerURL == "" {
		err = fmt.Errorf("%s is required by cowork migrate", config.EnvDatabaseOwnerURL)
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
	return 0
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

	hub := events.New(cfg.SSEReplayWindow, cfg.SSEMaxStreamsPerPerson)
	go db.Listen(ctx, hub.Publish, hub.SetUp)
	var objects *storage.Client
	if cfg.Storage != nil {
		if objects, err = storage.New(*cfg.Storage); err != nil {
			logger.Error("object storage setup failed", "error", err)
			return 1
		}
	} else {
		logger.Warn("no object storage configured; attachments cannot be uploaded", "variable", config.EnvS3Endpoint)
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
		Events:                 hub,
	})
	if err != nil {
		logger.Error("API setup failed", "error", err)
		return 1
	}
	handler := httpserver.New(httpserver.Options{Ready: db.Ping, API: apiHandler, Logger: logger})
	go runJobs(ctx, db, logger)

	logger.Info("listening", "addr", cfg.ListenAddr, "version", version, "commit", commit)
	if err := httpserver.ListenAndServe(ctx, cfg.ListenAddr, handler, cfg.ShutdownTimeout, hub.Close); err != nil {
		logger.Error("server stopped with error", "error", err)
		return 1
	}
	logger.Info("server stopped")
	return 0
}

// requireForServe checks what only `cowork serve` needs: the server key, and
// the owner role's URL while it migrates on start.
func requireForServe(cfg config.Config) error {
	var errs []error
	if cfg.SessionKey == nil {
		errs = append(errs, fmt.Errorf("%s is required by cowork serve", config.EnvSessionKey))
	}
	if cfg.MigrateOnStart && cfg.DatabaseOwnerURL == "" {
		errs = append(errs, fmt.Errorf("%s is required while %s is true; the chart migrates in an init container and sets it to false",
			config.EnvDatabaseOwnerURL, config.EnvMigrateOnStart))
	}
	return errors.Join(errs...)
}

// runJobs runs the background jobs on their tickers until ctx ends; each job
// holds its own advisory lock, so every replica may tick (docs/adr/0027 D5).
func runJobs(ctx context.Context, db *store.DB, logger *slog.Logger) {
	expiry := time.NewTicker(time.Hour)
	defer expiry.Stop()
	for {
		if removed, err := db.ExpireIdempotencyKeys(ctx); err != nil && ctx.Err() == nil {
			logger.Error("idempotency expiry failed", "error", err)
		} else if removed > 0 {
			logger.Info("expired idempotency keys removed", "removed", removed)
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
