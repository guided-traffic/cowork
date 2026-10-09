//go:build integration

package integration

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/config"
	"github.com/guided-traffic/cowork/backend/internal/store"
)

// The PostgreSQL that serves TLS under a private authority, which
// `make postgres-tls-up` starts (docs/adr/0058 D3): its administrative URL,
// without an sslmode, and the authority's certificate.
const (
	envTestDatabaseTLSURL = "COWORK_TEST_DATABASE_TLS_URL"
	envTestDatabaseTLSCA  = "COWORK_TEST_DATABASE_TLS_CA"
)

// tlsServer reads the PostgreSQL that serves TLS. Both variables are
// required: a run without them fails and says how to set them.
func tlsServer(t *testing.T) (adminURL, ca string) {
	t.Helper()
	adminURL, ca = os.Getenv(envTestDatabaseTLSURL), os.Getenv(envTestDatabaseTLSCA)
	if adminURL == "" || ca == "" {
		t.Fatalf("%s and %s are required: `make postgres-tls-up` starts a PostgreSQL that serves TLS under a private authority and `make test-integration` sets them",
			envTestDatabaseTLSURL, envTestDatabaseTLSCA)
	}
	return adminURL, ca
}

// withQuery returns raw with the query parameters set.
func withQuery(t *testing.T, raw string, params map[string]string) string {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	q := u.Query()
	for k, v := range params {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// otherAuthority writes the certificate of an authority that issued nothing
// the test servers present: to a connection that trusts it alone, every test
// server is outside it.
func otherAuthority(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "another authority"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true,
		KeyUsage: x509.KeyUsageCertSign, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "other-ca.crt")
	require.NoError(t, os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600))
	return path
}

// lookupOf has the contract of os.LookupEnv over vars, for config.Load.
func lookupOf(vars map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := vars[k]
		return v, ok
	}
}

// docs/adr/0058 D3: with COWORK_DATABASE_CA every connection of the backend
// holds sslmode verify-full against a private authority — the migration run of
// the binary, as the chart's init container and Job run it, and the runtime
// pool of the serving process, both over TLS —, and refuses a server outside
// that authority: the same server, trusted through an authority that did not
// issue its certificate, and the system pool when the variable is not set.
func TestTheDatabaseAuthorityHoldsVerifyFull(t *testing.T) {
	admin, ca := tlsServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	verified := withQuery(t, admin, map[string]string{"sslmode": "verify-full", "sslrootcert": ca})
	name := fmt.Sprintf("cowork_it_tls_%d", time.Now().UnixNano())
	require.NoError(t, createDatabase(ctx, verified, name))
	t.Cleanup(func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), time.Minute)
		defer dropCancel()
		assert.NoError(t, dropDatabase(dropCtx, verified, name))
	})
	ownerURL, err := withUserAndDatabase(admin, ownerRole, ownerRole, name)
	require.NoError(t, err)
	runtimeURL, err := withUserAndDatabase(admin, runtimeRole, runtimeRole, name)
	require.NoError(t, err)
	full := map[string]string{"sslmode": "verify-full"}
	vars := map[string]string{
		config.EnvDatabaseURL:      withQuery(t, runtimeURL, full),
		config.EnvDatabaseOwnerURL: withQuery(t, ownerURL, full),
		config.EnvDatabaseCA:       ca,
		config.EnvLogFormat:        "text",
	}
	bin := coworkBinary(t)

	code, log := runCowork(t, bin, vars, "migrate")
	require.Equal(t, 0, code, log)
	assert.Contains(t, log, "database schema is current")

	cfg, err := config.Load(lookupOf(vars))
	require.NoError(t, err)
	db, err := store.Open(ctx, cfg.DatabaseURL, store.Options{})
	require.NoError(t, err, "the runtime pool holds verify-full against the authority")
	db.Close()
	conn, err := pgx.Connect(ctx, cfg.DatabaseURL)
	require.NoError(t, err)
	defer func() { _ = conn.Close(ctx) }()
	var ssl bool
	require.NoError(t, conn.QueryRow(ctx, "SELECT ssl FROM pg_stat_ssl WHERE pid = pg_backend_pid()").Scan(&ssl))
	assert.True(t, ssl, "the connection is encrypted")

	outside := merged(vars, map[string]string{config.EnvDatabaseCA: otherAuthority(t)})
	code, log = runCowork(t, bin, outside, "migrate")
	assert.Equal(t, 1, code, log)
	assert.Contains(t, log, "certificate signed by unknown authority", "the migration run refuses a server outside the authority")
	cfg, err = config.Load(lookupOf(outside))
	require.NoError(t, err)
	_, err = store.Open(ctx, cfg.DatabaseURL, store.Options{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "certificate signed by unknown authority", "so does the runtime pool")

	system := merged(vars)
	delete(system, config.EnvDatabaseCA)
	code, log = runCowork(t, bin, system, "migrate")
	assert.Equal(t, 1, code, log)
	assert.Contains(t, log, "tls: failed to verify certificate", "the system pool does not hold the private authority")
}
