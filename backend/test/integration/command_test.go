//go:build integration

package integration

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/config"
	"github.com/guided-traffic/cowork/backend/internal/store"
	"github.com/guided-traffic/cowork/backend/test/fixture"
)

// commandDatabase is a database of its own that no migration has touched,
// for the binary's commands to run against as the chart runs them.
type commandDatabase struct {
	F                    *fixture.DB
	OwnerURL, RuntimeURL string
}

func newCommandDatabase(t *testing.T) commandDatabase {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	name := fmt.Sprintf("cowork_it_cmd_%d", time.Now().UnixNano())
	require.NoError(t, createDatabase(ctx, env.AdminURL, name))
	t.Cleanup(func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), time.Minute)
		defer dropCancel()
		assert.NoError(t, dropDatabase(dropCtx, env.AdminURL, name))
	})
	adminURL, err := withUserAndDatabase(env.AdminURL, "", "", name)
	require.NoError(t, err)
	ownerURL, err := withUserAndDatabase(env.AdminURL, ownerRole, ownerRole, name)
	require.NoError(t, err)
	runtimeURL, err := withUserAndDatabase(env.AdminURL, runtimeRole, runtimeRole, name)
	require.NoError(t, err)
	f, err := fixture.Connect(ctx, adminURL)
	require.NoError(t, err)
	t.Cleanup(f.Close)
	return commandDatabase{F: f, OwnerURL: ownerURL, RuntimeURL: runtimeURL}
}

// asComponents writes a URL as the component variables of docs/adr/0058 D4,
// under the prefix of the runtime or the owner role.
func asComponents(t *testing.T, raw string, owner bool) map[string]string {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	password, _ := u.User.Password()
	host, port, name, user, pass, sslmode := config.EnvDatabaseHost, config.EnvDatabasePort, config.EnvDatabaseName,
		config.EnvDatabaseUser, config.EnvDatabasePassword, config.EnvDatabaseSSLMode
	if owner {
		host, port, name, user, pass, sslmode = config.EnvDatabaseOwnerHost, config.EnvDatabaseOwnerPort, config.EnvDatabaseOwnerName,
			config.EnvDatabaseOwnerUser, config.EnvDatabaseOwnerPassword, config.EnvDatabaseOwnerSSLMode
	}
	vars := map[string]string{host: u.Hostname(), name: strings.TrimPrefix(u.Path, "/"), user: u.User.Username(), pass: password}
	if u.Port() != "" {
		vars[port] = u.Port()
	}
	if mode := u.Query().Get("sslmode"); mode != "" {
		vars[sslmode] = mode
	}
	return vars
}

// coworkBinary builds cmd/cowork, the binary of both containers and of the
// chart's migration Job.
func coworkBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "cowork")
	out, err := exec.Command("go", "build", "-o", bin, "github.com/guided-traffic/cowork/backend/cmd/cowork").CombinedOutput()
	require.NoError(t, err, "go build: %s", out)
	return bin
}

// runCowork runs a command of the binary with exactly the variables given, as
// a container's environment has them, and returns its exit code and its log.
func runCowork(t *testing.T, bin string, vars map[string]string, args ...string) (int, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir()}
	for k, v := range vars {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	var stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stderr, &stderr
	code := 0
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		require.True(t, errors.As(err, &exit), "run cowork %s: %v", strings.Join(args, " "), err)
		code = exit.ExitCode()
	}
	return code, stderr.String()
}

func merged(maps ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, m := range maps {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

// docs/adr/0057 D4: in the chart's job mode `cowork migrate` performs the
// bootstrap after the schema step, with the configuration `cowork serve` has —
// here the local administrator, the bootstrap tenant and the identity
// provider's administrator group, without the provider's client secret — so the
// installation is ready before a pod serves. Run again it changes nothing. A run
// without the switch, as the init container's, never touches the bootstrap: it
// does not deactivate the administrator the configuration it was not given
// keeps. Both roles are given as components (docs/adr/0058 D4), as a Secret
// without a URL key hands them over.
func TestMigrateInJobModeLeavesTheBootstrapDone(t *testing.T) {
	ctx := context.Background()
	db := newCommandDatabase(t)
	bin := coworkBinary(t)
	connection := merged(asComponents(t, db.RuntimeURL, false), asComponents(t, db.OwnerURL, true),
		map[string]string{config.EnvLogFormat: "text"})
	job := merged(connection, map[string]string{
		config.EnvMigrateBootstrap:   "true",
		config.EnvLocalAdminUsername: "job-admin",
		config.EnvLocalAdminPassword: testPassword,
		config.EnvBaseURL:            "https://cowork.example.com",
		config.EnvBootstrapTeamSlug:  "acme",
		config.EnvBootstrapTeamName:  "Acme Corp",
		config.EnvOIDCIssuer:         "https://login.example.com/realms/acme",
		config.EnvAdminGroup:         "cowork-admins",
	})

	code, log := runCowork(t, bin, job, "migrate")
	require.Equal(t, 0, code, log)
	embedded, err := store.EmbeddedVersion()
	require.NoError(t, err)
	assert.Contains(t, log, "database schema is current")
	assert.Contains(t, log, fmt.Sprintf("applied=%d", embedded), "a fresh database applies every migration")
	assert.Contains(t, log, "the local administrator is created")
	assert.Contains(t, log, "the bootstrap team is created")
	assert.Contains(t, log, "the bootstrap ran after the migration")
	assert.NotContains(t, log, testPassword, "the password is never logged")

	var admin uuid.UUID
	var global bool
	var deactivated *time.Time
	require.NoError(t, db.F.QueryRow(ctx, `SELECT u.id, u.global_admin, u.deactivated_at FROM users u
		JOIN local_accounts a ON a.user_id = u.id WHERE u.username = 'job-admin' AND a.origin = 'config'`).Scan(&admin, &global, &deactivated))
	assert.True(t, global, "the configured administrator is a global administrator")
	assert.Nil(t, deactivated)
	var tenant uuid.UUID
	require.NoError(t, db.F.QueryRow(ctx, `SELECT id FROM tenants WHERE slug = 'acme'`).Scan(&tenant))
	grants, err := db.F.QueryCount(ctx, `SELECT count(*) FROM memberships WHERE tenant_id = $1 AND user_id = $2 AND source = 'grant' AND role = 'admin'`, tenant, admin)
	require.NoError(t, err)
	assert.EqualValues(t, 1, grants, "the administrator's marked grant on the bootstrap tenant")
	mappings, err := db.F.QueryCount(ctx, `SELECT count(*) FROM group_mappings WHERE tenant_id = $1 AND group_name = 'cowork-admins' AND role = 'admin'`, tenant)
	require.NoError(t, err)
	assert.EqualValues(t, 1, mappings, "the administrator group mapped to the bootstrap tenant")
	rows, err := db.F.QueryCount(ctx, `SELECT count(*) FROM audit_events WHERE actor_system = 'system:bootstrap'`)
	require.NoError(t, err)

	code, log = runCowork(t, bin, job, "migrate")
	require.Equal(t, 0, code, log)
	assert.Contains(t, log, "applied=0")
	assert.NotContains(t, log, "is created", "a run that finds everything in step changes nothing")
	again, err := db.F.QueryCount(ctx, `SELECT count(*) FROM audit_events WHERE actor_system = 'system:bootstrap'`)
	require.NoError(t, err)
	assert.Equal(t, rows, again, "and records nothing")

	// The init container's environment: both roles, no switch, no administrator.
	code, log = runCowork(t, bin, connection, "migrate")
	require.Equal(t, 0, code, log)
	assert.NotContains(t, log, "bootstrap")
	require.NoError(t, db.F.QueryRow(ctx, `SELECT deactivated_at FROM users WHERE id = $1`, admin).Scan(&deactivated))
	assert.Nil(t, deactivated, "a run without the switch leaves the administrator as it is")
}

// docs/adr/0057 D3: whatever the mode, `cowork serve` refuses a schema with
// pending migrations — a job that did not run — and exits before it listens;
// once `cowork migrate` ran it gets past that check. The runtime role is given
// as components (docs/adr/0058 D4), so the refusal is read through the URL the
// configuration composed.
func TestServeRefusesAStaleSchema(t *testing.T) {
	db := newCommandDatabase(t)
	bin := coworkBinary(t)
	embedded, err := store.EmbeddedVersion()
	require.NoError(t, err)
	migrateTo(t, db.OwnerURL, embedded-1)

	serve := merged(asComponents(t, db.RuntimeURL, false), map[string]string{
		config.EnvMigrateOnStart: "false",
		config.EnvSessionKey:     "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=",
		config.EnvLogFormat:      "text",
		config.EnvListenAddr:     "127.0.0.1:0",
	})
	code, log := runCowork(t, bin, serve, "serve")
	assert.Equal(t, 1, code, log)
	assert.Contains(t, log, "database check failed")
	assert.Contains(t, log, "pending migrations: 1; run the migration job (or set "+config.EnvMigrateOnStart+"=true)")
	assert.NotContains(t, log, "listening")

	code, log = runCowork(t, bin, merged(serve, map[string]string{config.EnvDatabaseOwnerURL: db.OwnerURL}), "migrate")
	require.Equal(t, 0, code, log)
	assert.Contains(t, log, "applied=1")
}
