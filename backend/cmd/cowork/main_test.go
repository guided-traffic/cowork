package main

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
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

func TestRunDownIsUnknown(t *testing.T) {
	// Migrations only go forward; there is no down command (docs/adr/0028).
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"down"}, envOf(nil), &stdout, &stderr)
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr.String(), `unknown command "down"`)
}

func TestMigrateWithoutDatabaseURLFails(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"migrate"}, envOf(nil), &stdout, &stderr)
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr.String(), config.EnvDatabaseURL+" is required")
}

func TestMigrateWithoutOwnerURLFails(t *testing.T) {
	var stdout, stderr bytes.Buffer
	env := envOf(map[string]string{config.EnvDatabaseURL: "postgres://cowork_app@db/cowork"})
	code := run(context.Background(), []string{"migrate"}, env, &stdout, &stderr)
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr.String(), config.EnvDatabaseOwnerURL+" is required by cowork migrate")
}

func TestServeWithoutDatabaseURLFails(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"serve"}, envOf(nil), &stdout, &stderr)
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr.String(), config.EnvDatabaseURL+" is required")
}

// serve migrates on start by default, which needs the owner role's URL; the
// chart migrates in an init container and turns the start-up run off.
func TestServeMigratingOnStartWithoutOwnerURLFails(t *testing.T) {
	var stdout, stderr bytes.Buffer
	env := envOf(map[string]string{config.EnvDatabaseURL: "postgres://cowork_app@db/cowork"})
	code := run(context.Background(), []string{"serve"}, env, &stdout, &stderr)
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr.String(), config.EnvDatabaseOwnerURL+" is required while "+config.EnvMigrateOnStart+" is true")
}

func TestMigrateWithUnparsableDatabaseURLFails(t *testing.T) {
	var stdout, stderr bytes.Buffer
	env := envOf(map[string]string{
		config.EnvDatabaseURL:      "://not-a-url",
		config.EnvDatabaseOwnerURL: "postgres://cowork_owner@db/cowork",
		config.EnvLogFormat:        "text",
	})
	code := run(context.Background(), []string{"migrate"}, env, &stdout, &stderr)
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr.String(), "migration failed")
}

// The test fixture writes past row-level security and mints tokens; it is a
// test asset and never part of the binary (docs/adr/0038 D6).
func TestTheBinaryContainsNoTestPackage(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", ".").Output()
	require.NoError(t, err)
	for _, pkg := range strings.Fields(string(out)) {
		assert.False(t, strings.HasPrefix(pkg, "github.com/guided-traffic/cowork/backend/test/"),
			"cmd/cowork depends on %s", pkg)
	}
}
