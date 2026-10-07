package mcpcli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// archiveOf is a tar.gz of the headers, each regular file with its body.
func archiveOf(t *testing.T, entries ...tar.Header) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, h := range entries {
		body := []byte("body of " + h.Name)
		if h.Typeflag == tar.TypeReg {
			h.Size = int64(len(body))
		}
		require.NoError(t, tw.WriteHeader(&h))
		if h.Typeflag == tar.TypeReg {
			_, err := tw.Write(body)
			require.NoError(t, err)
		}
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	return buf.Bytes()
}

// docs/adr/0070 D5: an export is unpacked into an empty or a new directory,
// each file created anew inside it; a path out of it, a link, and a file that
// exists are refused.
func TestUnpackStaysInsideAndNeverOverwrites(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "export")
	manifest := tar.Header{Name: "manifest.json", Typeflag: tar.TypeReg, Mode: 0o644}
	m, err := unpack(archiveOf(t, tar.Header{Name: "acme/VKO-1.md", Typeflag: tar.TypeReg, Mode: 0o644}), dir)
	require.NoError(t, err)
	assert.Zero(t, m.Tickets)
	got, err := os.ReadFile(filepath.Join(dir, "acme", "VKO-1.md"))
	require.NoError(t, err)
	assert.Equal(t, "body of acme/VKO-1.md", string(got))
	info, err := os.Stat(filepath.Join(dir, "acme", "VKO-1.md"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "an export may hold confidential tickets")

	_, err = unpack(archiveOf(t, tar.Header{Name: "acme/VKO-1.md", Typeflag: tar.TypeReg, Mode: 0o644}), dir)
	assert.ErrorContains(t, err, "file exists", "never over a file that exists")
	for _, h := range []tar.Header{
		{Name: "../escape.md", Typeflag: tar.TypeReg},
		{Name: "acme/../../escape.md", Typeflag: tar.TypeReg},
		{Name: "/etc/escape.md", Typeflag: tar.TypeReg},
		{Name: "acme/link.md", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"},
	} {
		_, err := unpack(archiveOf(t, manifest, h), filepath.Join(t.TempDir(), "x"))
		assert.Error(t, err, h.Name)
	}
	_, err = os.Stat(filepath.Join(filepath.Dir(dir), "escape.md"))
	assert.ErrorIs(t, err, os.ErrNotExist)

	assert.NoError(t, emptyTarget(filepath.Join(t.TempDir(), "new")))
	assert.NoError(t, emptyTarget(t.TempDir()))
	assert.ErrorContains(t, emptyTarget(dir), "is not empty")
	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	assert.ErrorContains(t, emptyTarget(file), "is no directory")
}
