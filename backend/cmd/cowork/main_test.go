package main

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/config"
)

func envOf(values map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		v, ok := values[key]
		return v, ok
	}
}

func TestRunWithoutCommandPrintsUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), nil, envOf(nil), &stdout, &stderr)
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr.String(), "Usage: cowork <command>")
	assert.Empty(t, stdout.String())
}

func TestRunUnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"dance"}, envOf(nil), &stdout, &stderr)
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr.String(), `unknown command "dance"`)
}

func TestRunVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"version"}, envOf(nil), &stdout, &stderr)
	require.Equal(t, 0, code)
	assert.Contains(t, stdout.String(), "cowork dev (commit unknown, built unknown)")
	assert.Empty(t, stderr.String())
}

func TestRunHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"help"}, envOf(nil), &stdout, &stderr)
	require.Equal(t, 0, code)
	assert.Contains(t, stdout.String(), "serve")
	assert.Contains(t, stdout.String(), "migrate")
}

func TestMigrateWithoutDatabaseURLFails(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"migrate"}, envOf(nil), &stdout, &stderr)
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr.String(), config.EnvDatabaseURL+" is required")
}

func TestServeWithoutDatabaseURLFails(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"serve"}, envOf(nil), &stdout, &stderr)
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr.String(), config.EnvDatabaseURL+" is required")
}

func TestMigrateWithUnparsableDatabaseURLFails(t *testing.T) {
	var stdout, stderr bytes.Buffer
	env := envOf(map[string]string{config.EnvDatabaseURL: "://not-a-url", config.EnvLogFormat: "text"})
	code := run(context.Background(), []string{"migrate"}, env, &stdout, &stderr)
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr.String(), "migration failed")
}
