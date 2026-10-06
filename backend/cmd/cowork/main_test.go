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
	assert.Contains(t, stderr.String(), config.EnvDatabaseURL+", or its components "+config.EnvDatabaseHost)
}

func TestMigrateWithoutOwnerURLFails(t *testing.T) {
	var stdout, stderr bytes.Buffer
	env := envOf(map[string]string{config.EnvDatabaseURL: "postgres://cowork_app@db/cowork"})
	code := run(context.Background(), []string{"migrate"}, env, &stdout, &stderr)
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr.String(), config.OwnerConnection()+" is required by cowork migrate")
	assert.Contains(t, stderr.String(), config.EnvDatabaseOwnerHost, "the components are named as the other way")
}

func TestServeWithoutDatabaseURLFails(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"serve"}, envOf(nil), &stdout, &stderr)
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr.String(), config.EnvDatabaseURL+", or its components "+config.EnvDatabaseHost)
}

// serve migrates on start by default, which needs the owner role's URL; the
// chart migrates in an init container and turns the start-up run off.
func TestServeMigratingOnStartWithoutOwnerURLFails(t *testing.T) {
	var stdout, stderr bytes.Buffer
	env := envOf(map[string]string{config.EnvDatabaseURL: "postgres://cowork_app@db/cowork"})
	code := run(context.Background(), []string{"serve"}, env, &stdout, &stderr)
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr.String(), config.OwnerConnection()+" is required while "+config.EnvMigrateOnStart+" is true")
}

// docs/adr/0057 D4: serve needs the identity provider's client; the
// migration run reads the administrator group without it and gets past the
// configuration — it fails here only on the runtime URL nobody can parse,
// before it reaches any network.
func TestTheIdentityProvidersClientIsServesRequirementAlone(t *testing.T) {
	provider := map[string]string{
		config.EnvDatabaseURL: "://not-a-url", config.EnvDatabaseOwnerURL: "postgres://cowork_owner@db/cowork",
		config.EnvSessionKey: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=", config.EnvBaseURL: "https://cowork.example.com",
		config.EnvMigrateOnStart: "false", config.EnvMigrateBootstrap: "true", config.EnvLogFormat: "text",
		config.EnvOIDCIssuer: "https://login.example.com/realms/acme", config.EnvAdminGroup: "cowork-admins",
		config.EnvBootstrapTenantSlug: "acme", config.EnvBootstrapTenantName: "Acme",
	}
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"serve"}, envOf(provider), &stdout, &stderr)
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr.String(), config.EnvOIDCClientID+" is required while "+config.EnvOIDCIssuer+" is set")
	assert.Contains(t, stderr.String(), config.EnvOIDCClientSecret+" is required while "+config.EnvOIDCIssuer+" is set")

	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"migrate"}, envOf(provider), &stdout, &stderr)
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr.String(), "migration failed", "the configuration passed")
	assert.NotContains(t, stderr.String(), config.EnvOIDCClientSecret)
}

// docs/adr/0058 D4: migrate takes the owner role's connection as components
// as well; it gets past its requirements and fails only on the runtime URL
// nobody can parse, before it reaches any network.
func TestMigrateTakesTheOwnerAsComponents(t *testing.T) {
	var stdout, stderr bytes.Buffer
	env := envOf(map[string]string{
		config.EnvDatabaseURL: "://not-a-url", config.EnvLogFormat: "text",
		config.EnvDatabaseOwnerHost: "db", config.EnvDatabaseOwnerName: "cowork",
		config.EnvDatabaseOwnerUser: "cowork_owner", config.EnvDatabaseOwnerPassword: "p@ss:w/rd%",
	})
	code := run(context.Background(), []string{"migrate"}, env, &stdout, &stderr)
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr.String(), "migration failed")
	assert.NotContains(t, stderr.String(), "is required by cowork migrate")
	assert.NotContains(t, stderr.String(), "p@ss:w/rd%", "the password is never echoed")
}

// docs/adr/0032 D2, D3: one of the two local administrator variables alone, or a
// password shorter than the configured minimum, refuses the start with a
// message that names the variable and never the value.
func TestServeRefusesAHalfConfiguredLocalAdministrator(t *testing.T) {
	base := map[string]string{
		config.EnvDatabaseURL: "postgres://cowork_app@db/cowork", config.EnvDatabaseOwnerURL: "postgres://cowork_owner@db/cowork",
		config.EnvSessionKey: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=", config.EnvBaseURL: "https://cowork.example.com",
	}
	for name, c := range map[string]struct {
		env    map[string]string
		wanted string
	}{
		"a username alone": {map[string]string{config.EnvLocalAdminUsername: "ada"}, config.EnvLocalAdminPassword + " is required while " + config.EnvLocalAdminUsername + " is set"},
		"a password alone": {map[string]string{config.EnvLocalAdminPassword: "a very secret password"}, config.EnvLocalAdminUsername + " is required while " + config.EnvLocalAdminPassword + " is set"},
		"a short password": {map[string]string{config.EnvLocalAdminUsername: "ada", config.EnvLocalAdminPassword: "tooshort"}, config.EnvLocalAdminPassword + " must be at least 12 characters"},
		"no base URL": {map[string]string{config.EnvLocalAdminUsername: "ada", config.EnvLocalAdminPassword: "a very secret password", config.EnvBaseURL: ""},
			config.EnvBaseURL + " is required while " + config.EnvLocalAdminUsername + " is set"},
	} {
		t.Run(name, func(t *testing.T) {
			env := map[string]string{}
			for k, v := range base {
				env[k] = v
			}
			for k, v := range c.env {
				env[k] = v
			}
			var stdout, stderr bytes.Buffer
			code := run(context.Background(), []string{"serve"}, envOf(env), &stdout, &stderr)
			assert.Equal(t, 1, code)
			assert.Contains(t, stderr.String(), c.wanted)
			for _, secret := range []string{"a very secret password", "tooshort"} {
				assert.NotContains(t, stderr.String(), secret)
			}
		})
	}
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
