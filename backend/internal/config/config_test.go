package config

import (
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func envOf(values map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		v, ok := values[key]
		return v, ok
	}
}

const dbURL = "postgres://cowork:secret@db:5432/cowork?sslmode=require"

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(envOf(map[string]string{EnvDatabaseURL: dbURL}))
	require.NoError(t, err)

	assert.Equal(t, DefaultListenAddr, cfg.ListenAddr)
	assert.Equal(t, dbURL, cfg.DatabaseURL)
	assert.True(t, cfg.MigrateOnStart)
	assert.Equal(t, slog.LevelInfo, cfg.LogLevel)
	assert.Equal(t, LogFormatJSON, cfg.LogFormat)
	assert.Equal(t, DefaultShutdownTimeout, cfg.ShutdownTimeout)
	assert.Empty(t, cfg.BaseURL)
}

func TestLoadOverrides(t *testing.T) {
	cfg, err := Load(envOf(map[string]string{
		EnvDatabaseURL:     dbURL,
		EnvListenAddr:      "127.0.0.1:9090",
		EnvMigrateOnStart:  "false",
		EnvLogLevel:        "DEBUG",
		EnvLogFormat:       "Text",
		EnvShutdownTimeout: "1m",
		EnvBaseURL:         "https://cowork.example.com/",
	}))
	require.NoError(t, err)

	assert.Equal(t, "127.0.0.1:9090", cfg.ListenAddr)
	assert.False(t, cfg.MigrateOnStart)
	assert.Equal(t, slog.LevelDebug, cfg.LogLevel)
	assert.Equal(t, LogFormatText, cfg.LogFormat)
	assert.Equal(t, time.Minute, cfg.ShutdownTimeout)
	assert.Equal(t, "https://cowork.example.com", cfg.BaseURL, "trailing slash is stripped")
}

func TestLoadRequiresDatabaseURL(t *testing.T) {
	for name, env := range map[string]map[string]string{
		"unset":      {},
		"empty":      {EnvDatabaseURL: ""},
		"whitespace": {EnvDatabaseURL: "   "},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Load(envOf(env))
			require.Error(t, err)
			assert.Contains(t, err.Error(), EnvDatabaseURL+" is required")
		})
	}
}

func TestLoadEmptyValueKeepsDefault(t *testing.T) {
	cfg, err := Load(envOf(map[string]string{
		EnvDatabaseURL:    dbURL,
		EnvListenAddr:     "",
		EnvMigrateOnStart: "",
		EnvLogLevel:       "",
	}))
	require.NoError(t, err)
	assert.Equal(t, DefaultListenAddr, cfg.ListenAddr)
	assert.True(t, cfg.MigrateOnStart)
	assert.Equal(t, slog.LevelInfo, cfg.LogLevel)
}

func TestLoadReportsEveryInvalidValue(t *testing.T) {
	_, err := Load(envOf(map[string]string{
		EnvMigrateOnStart:  "maybe",
		EnvLogLevel:        "loud",
		EnvLogFormat:       "xml",
		EnvShutdownTimeout: "soon",
	}))
	require.Error(t, err)
	msg := err.Error()
	assert.Contains(t, msg, EnvDatabaseURL+" is required")
	assert.Contains(t, msg, EnvMigrateOnStart+`: "maybe" is not a boolean`)
	assert.Contains(t, msg, EnvLogLevel+`: "loud" is not one of`)
	assert.Contains(t, msg, EnvLogFormat+`: "xml" is not one of`)
	assert.Contains(t, msg, EnvShutdownTimeout+`: "soon" is not a duration`)
}

func TestLoadRejectsNonPositiveShutdownTimeout(t *testing.T) {
	_, err := Load(envOf(map[string]string{EnvDatabaseURL: dbURL, EnvShutdownTimeout: "0s"}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be positive")
}
