// Package config loads the server configuration from the environment.
//
// Every setting is an environment variable with the COWORK_ prefix. The
// user-facing reference with the defaults is the configuration table in
// README.md; this package is what that table is checked against.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
)

// The environment variables the server reads.
const (
	EnvListenAddr      = "COWORK_LISTEN_ADDR"
	EnvDatabaseURL     = "COWORK_DATABASE_URL"
	EnvMigrateOnStart  = "COWORK_MIGRATE_ON_START"
	EnvLogLevel        = "COWORK_LOG_LEVEL"
	EnvLogFormat       = "COWORK_LOG_FORMAT"
	EnvShutdownTimeout = "COWORK_SHUTDOWN_TIMEOUT"
	EnvBaseURL         = "COWORK_BASE_URL"
)

// Defaults and the accepted log formats.
const (
	DefaultListenAddr      = ":8080"
	DefaultShutdownTimeout = 15 * time.Second
	LogFormatJSON          = "json"
	LogFormatText          = "text"
)

// Config is the complete server configuration.
type Config struct {
	// ListenAddr is the address the HTTP server binds, host:port.
	ListenAddr string
	// DatabaseURL is the PostgreSQL connection URL (postgres://...). Required.
	DatabaseURL string
	// MigrateOnStart makes `cowork serve` apply pending migrations before it listens.
	MigrateOnStart bool
	// LogLevel is the minimum level written to the log.
	LogLevel slog.Level
	// LogFormat is LogFormatJSON or LogFormatText.
	LogFormat string
	// ShutdownTimeout bounds the graceful shutdown after SIGTERM.
	ShutdownTimeout time.Duration
	// BaseURL is the public URL the UI is reached under, without a trailing
	// slash. Optional until a feature (OIDC redirects, links in notifications)
	// needs it.
	BaseURL string
}

// Load reads the configuration through lookup, which has the contract of
// os.LookupEnv. Every invalid or missing required value is reported; the
// returned error joins all of them so one restart fixes everything at once.
func Load(lookup func(string) (string, bool)) (Config, error) {
	cfg := Config{
		ListenAddr:      DefaultListenAddr,
		MigrateOnStart:  true,
		LogLevel:        slog.LevelInfo,
		LogFormat:       LogFormatJSON,
		ShutdownTimeout: DefaultShutdownTimeout,
	}
	var errs []error

	if v, ok := nonEmpty(lookup, EnvListenAddr); ok {
		cfg.ListenAddr = v
	}

	if v, ok := nonEmpty(lookup, EnvDatabaseURL); ok {
		cfg.DatabaseURL = v
	} else {
		errs = append(errs, fmt.Errorf("%s is required", EnvDatabaseURL))
	}

	if v, ok := nonEmpty(lookup, EnvMigrateOnStart); ok {
		b, err := strconv.ParseBool(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %q is not a boolean", EnvMigrateOnStart, v))
		} else {
			cfg.MigrateOnStart = b
		}
	}

	if v, ok := nonEmpty(lookup, EnvLogLevel); ok {
		var level slog.Level
		if err := level.UnmarshalText([]byte(v)); err != nil {
			errs = append(errs, fmt.Errorf("%s: %q is not one of debug, info, warn, error", EnvLogLevel, v))
		} else {
			cfg.LogLevel = level
		}
	}

	if v, ok := nonEmpty(lookup, EnvLogFormat); ok {
		switch strings.ToLower(v) {
		case LogFormatJSON, LogFormatText:
			cfg.LogFormat = strings.ToLower(v)
		default:
			errs = append(errs, fmt.Errorf("%s: %q is not one of %s, %s", EnvLogFormat, v, LogFormatJSON, LogFormatText))
		}
	}

	if v, ok := nonEmpty(lookup, EnvShutdownTimeout); ok {
		d, err := time.ParseDuration(v)
		switch {
		case err != nil:
			errs = append(errs, fmt.Errorf("%s: %q is not a duration such as 15s", EnvShutdownTimeout, v))
		case d <= 0:
			errs = append(errs, fmt.Errorf("%s: must be positive, got %s", EnvShutdownTimeout, d))
		default:
			cfg.ShutdownTimeout = d
		}
	}

	if v, ok := nonEmpty(lookup, EnvBaseURL); ok {
		cfg.BaseURL = strings.TrimRight(v, "/")
	}

	return cfg, errors.Join(errs...)
}

// nonEmpty reports a variable that is set to something other than whitespace.
func nonEmpty(lookup func(string) (string, bool), key string) (string, bool) {
	v, ok := lookup(key)
	v = strings.TrimSpace(v)
	return v, ok && v != ""
}
