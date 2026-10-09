package config

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"maps"
	"math/big"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A password a generator writes: every character a URL reserves, a percent
// sign that could read as an escape, and a space.
const reservedPassword = "p@ss:w/rd%41?#&= +[]"

func runtimeComponents(extra map[string]string) map[string]string {
	env := map[string]string{
		EnvDatabaseHost:     "postgres.cowork.svc",
		EnvDatabaseName:     "cowork",
		EnvDatabaseUser:     "cowork_app",
		EnvDatabasePassword: reservedPassword,
	}
	maps.Copy(env, extra)
	return env
}

// docs/adr/0058 D4: the components are composed into the URL the driver reads,
// and the driver reads back exactly what each component said — the password's
// reserved characters included, escaped and not split at.
func TestDatabaseComponentsComposeTheURL(t *testing.T) {
	cfg, err := Load(envOf(runtimeComponents(map[string]string{EnvDatabasePort: "6432", EnvDatabaseSSLMode: "verify-full"})))
	require.NoError(t, err)
	assert.NotContains(t, cfg.DatabaseURL, reservedPassword, "the password is escaped into the URL")

	parsed, err := pgconn.ParseConfig(cfg.DatabaseURL)
	require.NoError(t, err)
	assert.Equal(t, "postgres.cowork.svc", parsed.Host)
	assert.EqualValues(t, 6432, parsed.Port)
	assert.Equal(t, "cowork", parsed.Database)
	assert.Equal(t, "cowork_app", parsed.User)
	assert.Equal(t, reservedPassword, parsed.Password)
	require.NotNil(t, parsed.TLSConfig, "verify-full asks for TLS")
	assert.False(t, parsed.TLSConfig.InsecureSkipVerify, "and verifies the server")
	assert.Empty(t, parsed.Fallbacks, "verify-full has no plain-text fallback")
}

// Without a port the driver connects to 5432; without an sslmode the URL
// carries none and the driver takes its default.
func TestDatabaseComponentsWithoutPortAndSSLMode(t *testing.T) {
	cfg, err := Load(envOf(runtimeComponents(nil)))
	require.NoError(t, err)
	assert.NotContains(t, cfg.DatabaseURL, "sslmode")
	parsed, err := pgconn.ParseConfig(cfg.DatabaseURL)
	require.NoError(t, err)
	assert.EqualValues(t, 5432, parsed.Port)
	assert.Equal(t, reservedPassword, parsed.Password)
}

// A user and a database name with reserved characters are escaped as the
// password is; an IPv6 address is bracketed, written with or without brackets.
func TestDatabaseComponentsEscapeEveryPart(t *testing.T) {
	for name, c := range map[string]struct {
		env  map[string]string
		host string
	}{
		"an IPv6 address":                 {map[string]string{EnvDatabaseHost: "fd00::5"}, "fd00::5"},
		"an IPv6 address in brackets":     {map[string]string{EnvDatabaseHost: "[fd00::5]", EnvDatabasePort: "5433"}, "fd00::5"},
		"a user and a name with reserved": {map[string]string{EnvDatabaseUser: "app@team:1", EnvDatabaseName: "co work/#1?"}, "postgres.cowork.svc"},
	} {
		t.Run(name, func(t *testing.T) {
			env := runtimeComponents(c.env)
			cfg, err := Load(envOf(env))
			require.NoError(t, err)
			parsed, err := pgconn.ParseConfig(cfg.DatabaseURL)
			require.NoError(t, err)
			assert.Equal(t, c.host, parsed.Host)
			assert.Equal(t, env[EnvDatabaseUser], parsed.User)
			assert.Equal(t, env[EnvDatabaseName], parsed.Database)
			assert.Equal(t, reservedPassword, parsed.Password)
		})
	}
}

// The owner role's connection takes the same two shapes, each role on its own.
func TestDatabaseOwnerComponents(t *testing.T) {
	cfg, err := Load(envOf(map[string]string{
		EnvDatabaseURL:           dbURL,
		EnvDatabaseOwnerHost:     "postgres.cowork.svc",
		EnvDatabaseOwnerPort:     "5432",
		EnvDatabaseOwnerName:     "cowork",
		EnvDatabaseOwnerUser:     "cowork_owner",
		EnvDatabaseOwnerPassword: reservedPassword,
		EnvDatabaseOwnerSSLMode:  "require",
	}))
	require.NoError(t, err)
	assert.Equal(t, dbURL, cfg.DatabaseURL, "the runtime role's URL as it is")
	parsed, err := pgconn.ParseConfig(cfg.DatabaseOwnerURL)
	require.NoError(t, err)
	assert.Equal(t, "cowork_owner", parsed.User)
	assert.Equal(t, reservedPassword, parsed.Password)
	assert.Equal(t, "cowork", parsed.Database)
	require.NotNil(t, parsed.TLSConfig, "require asks for TLS")

	cfg, err = Load(envOf(runtimeComponents(map[string]string{EnvDatabaseOwnerURL: "postgres://cowork_owner:secret@db/cowork"})))
	require.NoError(t, err)
	assert.Equal(t, "postgres://cowork_owner:secret@db/cowork", cfg.DatabaseOwnerURL, "the owner's URL beside the runtime role's components")
}

// docs/adr/0058 D4: a URL and a component of the same role is an error, and so
// is a component without the others a connection needs; the error names the
// variables and never echoes the password or the URL.
func TestDatabaseComponentsRefusals(t *testing.T) {
	for name, c := range map[string]struct {
		env  map[string]string
		want []string
	}{
		"the URL and a component": {
			runtimeComponents(map[string]string{EnvDatabaseURL: "postgres://app:url-secret@db/cowork"}),
			[]string{"set " + EnvDatabaseURL + " or its components, not both", EnvDatabaseHost, EnvDatabasePassword},
		},
		"the owner URL and a component": {
			map[string]string{EnvDatabaseURL: dbURL, EnvDatabaseOwnerURL: "postgres://owner:url-secret@db/cowork", EnvDatabaseOwnerPassword: reservedPassword},
			[]string{"set " + EnvDatabaseOwnerURL + " or its components, not both: " + EnvDatabaseOwnerPassword + " set as well"},
		},
		"a password alone": {
			map[string]string{EnvDatabasePassword: reservedPassword},
			[]string{"need " + EnvDatabaseHost + ", " + EnvDatabaseName + ", " + EnvDatabaseUser + " and " + EnvDatabasePassword + " together",
				"missing", EnvDatabaseHost},
		},
		"no user": {
			runtimeComponents(map[string]string{EnvDatabaseUser: ""}),
			[]string{EnvDatabaseUser + " missing"},
		},
		"a port that is none": {
			runtimeComponents(map[string]string{EnvDatabasePort: "postgres"}),
			[]string{EnvDatabasePort + `: "postgres" is not a port`},
		},
		"a port out of range": {
			runtimeComponents(map[string]string{EnvDatabasePort: "65536"}),
			[]string{EnvDatabasePort + `: "65536" is not a port`},
		},
		"an sslmode the driver does not know": {
			runtimeComponents(map[string]string{EnvDatabaseSSLMode: "required"}),
			[]string{EnvDatabaseSSLMode + `: "required" is not one of disable, allow, prefer, require, verify-ca, verify-full`},
		},
		"a host with a user in it": {
			runtimeComponents(map[string]string{EnvDatabaseHost: "app@db"}),
			[]string{EnvDatabaseHost + `: "app@db" is not a host name or an IP address`},
		},
		"a host with a path": {
			runtimeComponents(map[string]string{EnvDatabaseHost: "db/cowork"}),
			[]string{EnvDatabaseHost + `: "db/cowork" is not a host name or an IP address`},
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Load(envOf(c.env))
			require.Error(t, err)
			for _, want := range c.want {
				assert.Contains(t, err.Error(), want)
			}
			assert.NotContains(t, err.Error(), reservedPassword, "the password is never echoed")
			assert.NotContains(t, err.Error(), "url-secret", "the URL is never echoed")
			assert.NotContains(t, err.Error(), EnvDatabaseURL+", or its components", "a role whose variables are set is not reported as missing")
		})
	}
}

// docs/adr/0057 D4: the migration run performs the bootstrap only when told to.
func TestMigrateBootstrapSwitch(t *testing.T) {
	cfg, err := Load(envOf(map[string]string{EnvDatabaseURL: dbURL}))
	require.NoError(t, err)
	assert.False(t, cfg.MigrateBootstrap, "off by default: the init container never bootstraps")

	cfg, err = Load(envOf(map[string]string{EnvDatabaseURL: dbURL, EnvMigrateBootstrap: "true"}))
	require.NoError(t, err)
	assert.True(t, cfg.MigrateBootstrap)

	_, err = Load(envOf(map[string]string{EnvDatabaseURL: dbURL, EnvMigrateBootstrap: "sometimes"}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), EnvMigrateBootstrap+`: "sometimes" is not a boolean`)
}

// writeAuthority writes the PEM of a self-signed authority into a file of
// the test's own and returns its path: the driver reads the file when it
// parses a connection that names it.
func writeAuthority(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "cowork test authority"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true,
		KeyUsage: x509.KeyUsageCertSign, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "ca.crt")
	require.NoError(t, os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600))
	return path
}

// docs/adr/0058 D3: COWORK_DATABASE_CA becomes the sslrootcert of both roles'
// connections, a URL and one composed of components alike, so the runtime
// pool, the owner's and the migration run trust that authority alone: the
// driver reads it into the pool it verifies against, verify-full checks the
// host as well, and require checks the chain as verify-ca does. Everything
// else of the connection stays as it was given.
func TestTheDatabaseAuthorityIsEachConnectionsRootCert(t *testing.T) {
	ca := writeAuthority(t)
	env := runtimeComponents(map[string]string{EnvDatabaseSSLMode: "verify-full", EnvDatabaseCA: ca,
		EnvDatabaseOwnerURL: "postgres://cowork_owner:owner-secret@postgres.cowork.svc:5432/cowork?sslmode=require&pool_max_conns=4"})
	cfg, err := Load(envOf(env))
	require.NoError(t, err)
	assert.Equal(t, ca, cfg.DatabaseCA)

	runtime, err := pgconn.ParseConfig(cfg.DatabaseURL)
	require.NoError(t, err)
	assert.Equal(t, reservedPassword, runtime.Password, "the password survives the rewrite")
	require.NotNil(t, runtime.TLSConfig)
	assert.NotNil(t, runtime.TLSConfig.RootCAs, "the authority is the pool the server is verified against")
	assert.Equal(t, "postgres.cowork.svc", runtime.TLSConfig.ServerName, "verify-full checks the host")
	assert.False(t, runtime.TLSConfig.InsecureSkipVerify)
	assert.Empty(t, runtime.Fallbacks, "no plain-text fallback")

	owner, err := pgconn.ParseConfig(cfg.DatabaseOwnerURL)
	require.NoError(t, err)
	assert.Equal(t, "owner-secret", owner.Password)
	assert.Equal(t, "4", mustQuery(t, cfg.DatabaseOwnerURL).Get("pool_max_conns"), "the URL's other parameters stay")
	require.NotNil(t, owner.TLSConfig)
	assert.NotNil(t, owner.TLSConfig.RootCAs)
	assert.NotNil(t, owner.TLSConfig.VerifyPeerCertificate, "require with an authority checks the chain, as verify-ca does")
	assert.Empty(t, owner.Fallbacks)

	cfg, err = Load(envOf(runtimeComponents(map[string]string{EnvDatabaseSSLMode: "verify-ca", EnvDatabaseCA: ca})))
	require.NoError(t, err)
	assert.Empty(t, cfg.DatabaseOwnerURL, "a role that is not given stays unset")

	cfg, err = Load(envOf(runtimeComponents(map[string]string{EnvDatabaseSSLMode: "verify-full"})))
	require.NoError(t, err)
	assert.NotContains(t, cfg.DatabaseURL, "sslrootcert", "without the variable the connection is as it was given")
	assert.Empty(t, cfg.DatabaseCA)
}

func mustQuery(t *testing.T, raw string) url.Values {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	return u.Query()
}

// docs/adr/0058 D3: an authority beside a connection that would not use it is
// a configuration error — an sslmode that checks nothing, a second authority
// in the URL, a connection that is no URL —, which names the variables and
// never quotes the URL or the password.
func TestTheDatabaseAuthorityRefusals(t *testing.T) {
	const ca = "/etc/cowork/database-ca/ca.crt"
	for name, c := range map[string]struct {
		env  map[string]string
		want string
	}{
		"no sslmode": {
			runtimeComponents(map[string]string{EnvDatabaseCA: ca}),
			EnvDatabaseCA + " checks nothing while the sslmode of " + EnvDatabaseURL + " is unset, the driver's prefer",
		},
		"an sslmode that checks nothing": {
			runtimeComponents(map[string]string{EnvDatabaseCA: ca, EnvDatabaseSSLMode: "prefer"}),
			EnvDatabaseCA + ` checks nothing while the sslmode of ` + EnvDatabaseURL + ` is "prefer"`,
		},
		"plain text": {
			map[string]string{EnvDatabaseCA: ca, EnvDatabaseURL: "postgres://app:url-secret@db/cowork?sslmode=disable"},
			EnvDatabaseCA + ` checks nothing while the sslmode of ` + EnvDatabaseURL + ` is "disable"`,
		},
		"the owner's sslmode": {
			map[string]string{EnvDatabaseCA: ca, EnvDatabaseURL: "postgres://app:url-secret@db/cowork?sslmode=verify-full",
				EnvDatabaseOwnerURL: "postgres://owner:url-secret@db/cowork?sslmode=allow"},
			`the sslmode of ` + EnvDatabaseOwnerURL + ` is "allow"`,
		},
		"a second authority": {
			map[string]string{EnvDatabaseCA: ca, EnvDatabaseURL: "postgres://app:url-secret@db/cowork?sslmode=verify-full&sslrootcert=/other.crt"},
			EnvDatabaseCA + " and an sslrootcert in " + EnvDatabaseURL + " name two authorities",
		},
		"no URL": {
			map[string]string{EnvDatabaseCA: ca, EnvDatabaseURL: "host=db user=app password=url-secret sslmode=verify-full"},
			EnvDatabaseCA + " needs the connection of " + EnvDatabaseURL + " as a postgres:// URL",
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Load(envOf(c.env))
			require.Error(t, err)
			assert.Contains(t, err.Error(), c.want)
			assert.NotContains(t, err.Error(), reservedPassword, "the password is never echoed")
			assert.NotContains(t, err.Error(), "url-secret", "the URL is never echoed")
		})
	}
}
