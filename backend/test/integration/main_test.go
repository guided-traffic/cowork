//go:build integration

// Package integration holds the tests that need a running PostgreSQL 18, an
// S3-compatible server and an OpenID Connect issuer. `make test-integration`
// provides COWORK_TEST_DATABASE_URL, an administrative URL, COWORK_TEST_S3_*
// and COWORK_TEST_OIDC_ISSUER, the Dex of `make dex-up` (docs/adr/0029 D3);
// the variables are required, not optional, so a misconfigured job fails
// instead of passing on zero tests (docs/adr/0003 D3).
//
// TestMain gives the package a database of its own, as an installation has
// it (docs/adr/0021 D2, docs/adr/0058 D5): an owner role that owns the
// database and runs the migrations, and a runtime role that owns nothing and
// is what every store and API test connects as. The administrative
// connection is used only to create the two roles and the database, and by
// the fixture package to write past row-level security. The run's bucket is
// its own as well; the run removes both when it ends, so it leaves nothing on
// the test servers.
package integration

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/guided-traffic/cowork/backend/internal/config"
	"github.com/guided-traffic/cowork/backend/internal/oidc"
	"github.com/guided-traffic/cowork/backend/internal/storage"
	"github.com/guided-traffic/cowork/backend/internal/store"
)

// The variables the tier reads.
const (
	envTestDatabaseURL       = "COWORK_TEST_DATABASE_URL"
	envTestS3Endpoint        = "COWORK_TEST_S3_ENDPOINT"
	envTestS3AccessKeyID     = "COWORK_TEST_S3_ACCESS_KEY_ID"
	envTestS3SecretAccessKey = "COWORK_TEST_S3_SECRET_ACCESS_KEY"
	envTestOIDCIssuer        = "COWORK_TEST_OIDC_ISSUER"
)

// The roles the harness creates when they are missing. They exist only on a
// test server; their passwords are not secrets.
const (
	ownerRole   = "cowork_it_owner"
	runtimeRole = "cowork_it_app"
)

// env is the database of this test run.
var env struct {
	// AdminURL reaches the test database as the administrative role.
	AdminURL string
	// OwnerURL reaches it as the owner role.
	OwnerURL string
	// RuntimeURL reaches it as the runtime role.
	RuntimeURL string
	// Migrated is the result of the run TestMain performed.
	Migrated store.MigrateResult
	// Storage is the bucket of this run on the test S3 server.
	Storage config.Storage
	// OIDCIssuer is the Dex the login through the identity provider is
	// proven against.
	OIDCIssuer string
}

func TestMain(m *testing.M) {
	os.Exit(runMain(m))
}

func runMain(m *testing.M) int {
	admin := os.Getenv(envTestDatabaseURL)
	if admin == "" {
		fmt.Fprintf(os.Stderr, "%s is not set: `make postgres-up` starts a local PostgreSQL 18 and `make test-integration` sets the variable\n", envTestDatabaseURL)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := prepareStorage(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "integration setup: %v\n", err)
		return 1
	}
	defer func() {
		removeCtx, removeCancel := context.WithTimeout(context.Background(), time.Minute)
		defer removeCancel()
		if err := removeBucket(removeCtx); err != nil {
			fmt.Fprintf(os.Stderr, "integration teardown: %v\n", err)
		}
	}()
	if err := prepareIssuer(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "integration setup: %v\n", err)
		return 1
	}

	name := fmt.Sprintf("cowork_it_%d", time.Now().UnixNano())
	if err := createDatabase(ctx, admin, name); err != nil {
		fmt.Fprintf(os.Stderr, "integration setup: %v\n", err)
		return 1
	}
	defer func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), time.Minute)
		defer dropCancel()
		if err := dropDatabase(dropCtx, admin, name); err != nil {
			fmt.Fprintf(os.Stderr, "integration teardown: %v\n", err)
		}
	}()

	var err error
	if env.AdminURL, err = withUserAndDatabase(admin, "", "", name); err == nil {
		if env.OwnerURL, err = withUserAndDatabase(admin, ownerRole, ownerRole, name); err == nil {
			env.RuntimeURL, err = withUserAndDatabase(admin, runtimeRole, runtimeRole, name)
		}
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "integration setup: %v\n", err)
		return 1
	}
	env.Migrated, err = store.Migrate(ctx, env.OwnerURL, runtimeRole)
	if err != nil {
		fmt.Fprintf(os.Stderr, "integration setup: migrate: %v\n", err)
		return 1
	}
	return m.Run()
}

func createDatabase(ctx context.Context, adminURL, name string) error {
	conn, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		return fmt.Errorf("connect as administrator: %w", err)
	}
	defer func() { _ = conn.Close(ctx) }()
	_, err = conn.Exec(ctx, fmt.Sprintf(`DO $$ BEGIN
		IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = '%[1]s') THEN
			CREATE ROLE %[1]s LOGIN PASSWORD '%[1]s';
		END IF;
		IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = '%[2]s') THEN
			CREATE ROLE %[2]s LOGIN PASSWORD '%[2]s' NOSUPERUSER NOBYPASSRLS;
		END IF;
	END $$`, ownerRole, runtimeRole))
	if err != nil {
		return fmt.Errorf("create roles: %w", err)
	}
	if _, err := conn.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s OWNER %s", name, ownerRole)); err != nil {
		return fmt.Errorf("create database: %w", err)
	}
	return nil
}

func dropDatabase(ctx context.Context, adminURL, name string) error {
	conn, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		return fmt.Errorf("connect as administrator: %w", err)
	}
	defer func() { _ = conn.Close(ctx) }()
	_, err = conn.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", name))
	return err
}

// withUserAndDatabase returns base with another database and, when user is
// set, other credentials.
func withUserAndDatabase(base, user, password, database string) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("parse %s: %w", envTestDatabaseURL, err)
	}
	if user != "" {
		u.User = url.UserPassword(user, password)
	}
	u.Path = "/" + database
	return u.String(), nil
}

// prepareIssuer checks that the test issuer answers its discovery: a missing
// variable or an issuer that is not up fails the run, never skips it.
func prepareIssuer(ctx context.Context) error {
	env.OIDCIssuer = os.Getenv(envTestOIDCIssuer)
	if env.OIDCIssuer == "" {
		return fmt.Errorf("%s is required: `make dex-up` starts the test issuer and `make test-integration` sets it", envTestOIDCIssuer)
	}
	if _, err := oidc.Discover(ctx, dexConfig()); err != nil {
		return fmt.Errorf("the test issuer at %s does not answer its discovery; `make dex-up` starts it: %w", env.OIDCIssuer, err)
	}
	return nil
}

// prepareStorage gives the run a bucket of its own on the test S3 server.
func prepareStorage(ctx context.Context) error {
	env.Storage = config.Storage{
		Endpoint: os.Getenv(envTestS3Endpoint), AccessKeyID: os.Getenv(envTestS3AccessKeyID),
		SecretAccessKey: os.Getenv(envTestS3SecretAccessKey), PathStyle: true,
		Bucket: fmt.Sprintf("cowork-it-%d", time.Now().UnixNano()),
	}
	if env.Storage.Endpoint == "" || env.Storage.AccessKeyID == "" || env.Storage.SecretAccessKey == "" {
		return fmt.Errorf("%s, %s and %s are required: `make minio-up` starts a local S3 server and `make test-integration` sets them",
			envTestS3Endpoint, envTestS3AccessKeyID, envTestS3SecretAccessKey)
	}
	client, err := storage.New(env.Storage)
	if err != nil {
		return err
	}
	return client.EnsureBucket(ctx)
}

// removeBucket empties the run's bucket and removes it, as the run drops its
// database. The server never removes a bucket — the operator provides it
// (docs/adr/0058 D5) — so this lives here and not in the storage package.
func removeBucket(ctx context.Context) error {
	u, err := url.Parse(env.Storage.Endpoint)
	if err != nil {
		return fmt.Errorf("parse %s: %w", envTestS3Endpoint, err)
	}
	mc, err := minio.New(u.Host, &minio.Options{
		Creds:        credentials.NewStaticV4(env.Storage.AccessKeyID, env.Storage.SecretAccessKey, ""),
		Secure:       u.Scheme == "https",
		BucketLookup: minio.BucketLookupPath,
	})
	if err != nil {
		return fmt.Errorf("connect to the test S3 server: %w", err)
	}
	bucket := env.Storage.Bucket
	var listErr error
	objects := make(chan minio.ObjectInfo)
	go func() {
		defer close(objects)
		for object := range mc.ListObjects(ctx, bucket, minio.ListObjectsOptions{Recursive: true}) {
			if object.Err != nil {
				listErr = object.Err
				return
			}
			objects <- object
		}
	}()
	for failed := range mc.RemoveObjects(ctx, bucket, objects, minio.RemoveObjectsOptions{}) {
		if failed.Err != nil {
			err = errors.Join(err, fmt.Errorf("remove %s from the bucket %s: %w", failed.ObjectName, bucket, failed.Err))
		}
	}
	if err = errors.Join(err, listErr); err != nil {
		return err
	}
	if err := mc.RemoveBucket(ctx, bucket); err != nil {
		return fmt.Errorf("remove the bucket %s: %w", bucket, err)
	}
	return nil
}
