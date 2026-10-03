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

func TestLoadLimitsDefaultsAndOverrides(t *testing.T) {
	cfg, err := Load(envOf(map[string]string{EnvDatabaseURL: dbURL}))
	require.NoError(t, err)
	assert.EqualValues(t, 1<<20, cfg.MaxJSONBody)
	assert.Equal(t, 30*time.Second, cfg.RequestTimeout)
	assert.Equal(t, 200, cfg.MaxPageSize)
	assert.Equal(t, 256, cfg.MaxQueryLength)
	assert.Nil(t, cfg.SessionKey)

	cfg, err = Load(envOf(map[string]string{
		EnvDatabaseURL:    dbURL,
		EnvMaxJSONBody:    "512KiB",
		EnvRequestTimeout: "0",
		EnvMaxPageSize:    "0",
		EnvMaxQueryLength: "100",
		EnvSessionKey:     "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=",
	}))
	require.NoError(t, err)
	assert.EqualValues(t, 512<<10, cfg.MaxJSONBody)
	assert.Zero(t, cfg.RequestTimeout, "0 disables the timeout")
	assert.Zero(t, cfg.MaxPageSize)
	assert.Equal(t, 100, cfg.MaxQueryLength)
	assert.Len(t, cfg.SessionKey, 32)
}

func TestLoadRejectsBadLimitsAndNeverEchoesTheKey(t *testing.T) {
	_, err := Load(envOf(map[string]string{
		EnvDatabaseURL:    dbURL,
		EnvMaxJSONBody:    "lots",
		EnvRequestTimeout: "-1s",
		EnvMaxPageSize:    "-3",
		EnvMaxQueryLength: "x",
		EnvSessionKey:     "dG9vLXNob3J0",
	}))
	require.Error(t, err)
	msg := err.Error()
	for _, want := range []string{EnvMaxJSONBody, EnvRequestTimeout, EnvMaxPageSize, EnvMaxQueryLength, EnvSessionKey + " must decode to at least 32 bytes"} {
		assert.Contains(t, msg, want)
	}
	assert.NotContains(t, msg, "dG9vLXNob3J0", "the key is a secret and never echoed")

	_, err = Load(envOf(map[string]string{EnvDatabaseURL: dbURL, EnvSessionKey: "not base64!"}))
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "not base64!")
}

func TestParseSize(t *testing.T) {
	for in, want := range map[string]int64{"0": 0, "1024": 1024, "1KiB": 1024, "1MiB": 1 << 20, "2 GiB": 2 << 30, "10B": 10} {
		got, err := parseSize(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}
	for _, in := range []string{"", "MiB", "1MB", "-1", "1.5MiB"} {
		_, err := parseSize(in)
		assert.Error(t, err, in)
	}
}

// docs/adr/0016 D1: object storage is all of endpoint, bucket and both keys
// or none; the secret never appears in an error.
func TestStorageIsAllOrNone(t *testing.T) {
	base := map[string]string{EnvDatabaseURL: "postgres://x"}
	with := func(extra map[string]string) (Config, error) {
		env := map[string]string{}
		for k, v := range base {
			env[k] = v
		}
		for k, v := range extra {
			env[k] = v
		}
		return Load(func(k string) (string, bool) { v, ok := env[k]; return v, ok })
	}
	cfg, err := with(nil)
	require.NoError(t, err)
	assert.Nil(t, cfg.Storage, "none configured: uploads are refused")
	assert.EqualValues(t, 10<<20, cfg.AttachmentMaxBytes)
	assert.Equal(t, 100, cfg.AttachmentMaxPerTicket)

	_, err = with(map[string]string{EnvS3Endpoint: "http://minio:9000", EnvS3SecretAccessKey: "s3cr3t-value"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), EnvS3Bucket)
	assert.NotContains(t, err.Error(), "s3cr3t-value")

	cfg, err = with(map[string]string{EnvS3Endpoint: "http://minio:9000", EnvS3Bucket: "cowork", EnvS3AccessKeyID: "id",
		EnvS3SecretAccessKey: "secret", EnvS3UsePathStyle: "false", EnvAttachmentMaxBytes: "2MiB", EnvAttachmentMaxPerTicket: "0"})
	require.NoError(t, err)
	require.NotNil(t, cfg.Storage)
	assert.False(t, cfg.Storage.PathStyle)
	assert.EqualValues(t, 2<<20, cfg.AttachmentMaxBytes)
	assert.Equal(t, 0, cfg.AttachmentMaxPerTicket)

	cfg, err = with(map[string]string{EnvAttachmentMaxBytes: "0"})
	require.NoError(t, err, "0 disables the limit (docs/adr/0039 D2)")
	assert.Zero(t, cfg.AttachmentMaxBytes)

	_, err = with(map[string]string{EnvS3Endpoint: "minio:9000", EnvS3Bucket: "b", EnvS3AccessKeyID: "i", EnvS3SecretAccessKey: "s"})
	assert.ErrorContains(t, err, "not an http:// or https:// URL")
	_, err = with(map[string]string{EnvS3Endpoint: "ftp://key:s3cr3t@minio", EnvS3Bucket: "b", EnvS3AccessKeyID: "i", EnvS3SecretAccessKey: "s"})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "s3cr3t", "an endpoint may carry credentials; it is never echoed")
}
