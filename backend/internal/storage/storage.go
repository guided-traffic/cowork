// Package storage keeps the attachments' bytes in S3-compatible object
// storage (docs/adr/0016 D1). The bucket is private; the bytes leave only
// through the backend (D4), so nothing here signs a URL.
package storage

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/guided-traffic/cowork/backend/internal/config"
)

// ErrMissing is an object the metadata names and the bucket does not hold:
// a restore that brought the database back without its bytes
// (docs/adr/0059 D4).
var ErrMissing = errors.New("the object is missing from storage")

// Client reads and writes the attachments' objects in one bucket.
type Client struct {
	mc     *minio.Client
	bucket string
}

// Key is an attachment's object key, <tenant-id>/<attachment-id>
// (docs/adr/0016 D1); it is derived, never stored.
func Key(tenantID, attachmentID uuid.UUID) string {
	return tenantID.String() + "/" + attachmentID.String()
}

// ParseKey is the attachment id a key names under the tenant's prefix, when
// the key is exactly the one Key writes for it; any other key under the
// prefix names no attachment.
func ParseKey(tenantID uuid.UUID, key string) (uuid.UUID, bool) {
	rest, ok := strings.CutPrefix(key, tenantID.String()+"/")
	if !ok {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(rest)
	if err != nil || Key(tenantID, id) != key {
		return uuid.Nil, false
	}
	return id, true
}

// New connects to the configured storage. It does not reach the server;
// the first request does.
func New(cfg config.Storage) (*Client, error) {
	u, err := url.Parse(cfg.Endpoint)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("parse %s: not a URL with a host", config.EnvS3Endpoint)
	}
	tr, err := transport(cfg.CAFile)
	if err != nil {
		return nil, err
	}
	lookup := minio.BucketLookupDNS
	if cfg.PathStyle {
		lookup = minio.BucketLookupPath
	}
	mc, err := minio.New(u.Host, &minio.Options{
		Creds:        credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		Secure:       u.Scheme == "https",
		Region:       cfg.Region,
		BucketLookup: lookup,
		Transport:    tr,
	})
	if err != nil {
		return nil, fmt.Errorf("create the storage client: %w", err)
	}
	return &Client{mc: mc, bucket: cfg.Bucket}, nil
}

// transport trusts the system pool, and the authority in caFile when one is
// given, for a private endpoint's certificate.
func transport(caFile string) (http.RoundTripper, error) {
	tr, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, errors.New("the default transport is not an *http.Transport")
	}
	tr = tr.Clone()
	if caFile == "" {
		return tr, nil
	}
	pem, err := os.ReadFile(caFile) // #nosec G304 -- the operator's own configuration names the file
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", config.EnvS3CA, err)
	}
	pool, err := x509.SystemCertPool()
	if err != nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("%s holds no PEM certificate", config.EnvS3CA)
	}
	tr.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	return tr, nil
}

// Put stores size bytes under key.
func (c *Client) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	if _, err := c.mc.PutObject(ctx, c.bucket, key, r, size, minio.PutObjectOptions{ContentType: contentType}); err != nil {
		return fmt.Errorf("put object: %w", err)
	}
	return nil
}

// Get opens the object under key for streaming; ErrMissing when the bucket
// does not hold it.
func (c *Client) Get(ctx context.Context, key string) (io.ReadCloser, int64, error) {
	obj, err := c.mc.GetObject(ctx, c.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, 0, fmt.Errorf("get object: %w", err)
	}
	info, err := obj.Stat()
	if err != nil {
		_ = obj.Close()
		if minio.ToErrorResponse(err).Code == minio.NoSuchKey {
			return nil, 0, ErrMissing
		}
		return nil, 0, fmt.Errorf("stat object: %w", err)
	}
	return obj, info.Size, nil
}

// Delete removes the object under key; a missing object is no error.
func (c *Client) Delete(ctx context.Context, key string) error {
	if err := c.mc.RemoveObject(ctx, c.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("remove object: %w", err)
	}
	return nil
}

// Object is what a listing says of an object: its key, size and last change.
type Object struct {
	Key          string
	Size         int64
	LastModified time.Time
}

// List returns every object whose key begins with prefix — the current
// versions only, in a versioned bucket — for the consistency check
// (docs/adr/0059 D4). It takes s3:ListBucket on the bucket, which the
// access key's policy must grant beside reading, writing and deleting
// objects.
func (c *Client) List(ctx context.Context, prefix string) ([]Object, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var out []Object
	for info := range c.mc.ListObjectsIter(ctx, c.bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
		if info.Err != nil {
			return nil, fmt.Errorf("list objects: %w", info.Err)
		}
		out = append(out, Object{Key: info.Key, Size: info.Size, LastModified: info.LastModified})
	}
	return out, nil
}

// Exists says whether the bucket holds an object under key.
func (c *Client) Exists(ctx context.Context, key string) (bool, error) {
	_, err := c.mc.StatObject(ctx, c.bucket, key, minio.StatObjectOptions{})
	if err == nil {
		return true, nil
	}
	if minio.ToErrorResponse(err).Code == minio.NoSuchKey {
		return false, nil
	}
	return false, fmt.Errorf("stat object: %w", err)
}

// EnsureBucket creates the bucket when it does not exist. The server never
// calls it — the operator provides the bucket (docs/adr/0058 D5) — the test
// tier does.
func (c *Client) EnsureBucket(ctx context.Context) error {
	exists, err := c.mc.BucketExists(ctx, c.bucket)
	if err != nil {
		return fmt.Errorf("check the bucket: %w", err)
	}
	if exists {
		return nil
	}
	if err := c.mc.MakeBucket(ctx, c.bucket, minio.MakeBucketOptions{}); err != nil {
		return fmt.Errorf("make the bucket: %w", err)
	}
	return nil
}
