// Command cowork is the backend binary: it applies the database migrations
// and serves the JSON API. The web UI is the frontend container.
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/guided-traffic/cowork/backend/internal/config"
	"github.com/guided-traffic/cowork/backend/internal/httpserver"
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
  migrate   Apply pending migrations and exit.
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

	pool, err := store.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("database connection failed", "error", err)
		return 1
	}
	defer pool.Close()

	handler := httpserver.New(httpserver.Options{
		Version:   version,
		Commit:    commit,
		BuildTime: buildTime,
		Ready:     pool.Ping,
		Logger:    logger,
	})

	logger.Info("listening", "addr", cfg.ListenAddr, "version", version, "commit", commit)
	if err := httpserver.ListenAndServe(ctx, cfg.ListenAddr, handler, cfg.ShutdownTimeout); err != nil {
		logger.Error("server stopped with error", "error", err)
		return 1
	}
	logger.Info("server stopped")
	return 0
}

func migrateDatabase(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	res, err := store.Migrate(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
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
