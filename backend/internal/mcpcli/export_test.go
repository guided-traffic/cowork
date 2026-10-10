package mcpcli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// archiveOf is a tar.gz of the headers, each regular file with its body, the
// manifest an empty one.
func archiveOf(t *testing.T, entries ...tar.Header) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, h := range entries {
		body := []byte("body of " + h.Name)
		if h.Name == exportManifest {
			body = []byte("{}")
		}
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

// regular is a regular file of an archive.
func regular(name string) tar.Header {
	return tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o644}
}

// docs/adr/0070 D5: an export is unpacked into an empty or a new directory,
// each file created anew inside it; a path out of it, a link, and a file that
// exists are refused.
func TestUnpackStaysInsideAndNeverOverwrites(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "export")
	m, err := unpack(archiveOf(t, regular("acme/VKO-1.md")), dir, "acme", "VKO")
	require.NoError(t, err)
	assert.Zero(t, m.Tickets)
	got, err := os.ReadFile(filepath.Join(dir, "acme", "VKO-1.md"))
	require.NoError(t, err)
	assert.Equal(t, "body of acme/VKO-1.md", string(got))
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(dir, "acme", "VKO-1.md"))
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "an export may hold confidential tickets")
		info, err = os.Stat(filepath.Join(dir, "acme"))
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())
	}

	_, err = unpack(archiveOf(t, regular("acme/VKO-1.md")), dir, "acme", "VKO")
	assert.ErrorContains(t, err, "file exists", "never over a file that exists")
	for _, h := range []tar.Header{
		regular("../escape.md"),
		regular("acme/../../escape.md"),
		regular("/etc/escape.md"),
		{Name: "acme/link.md", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"},
	} {
		_, err := unpack(archiveOf(t, regular("manifest.json"), h), filepath.Join(t.TempDir(), "x"), "acme", "VKO")
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

// docs/adr/0051 D4, docs/adr/0070 D5: the unpacking writes the names an
// export of the project holds — the three manifests and its tickets'
// documents <team>/<PROJECT>-<n>.md — and no other, whatever separator,
// step, volume or form of a number a name holds; and it writes through a root
// at its target, so a directory link planted in the target leads nowhere
// outside it.
func TestTheExportWritesOnlyItsOwnNamesInsideItsTarget(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "export")
	_, err := unpack(archiveOf(t, regular("manifest.json"), regular("links.json"), regular("attachments.json"),
		regular("acme/VKO-1.md"), regular("acme/VKO-2147483647.md")), dir, "acme", "VKO")
	require.NoError(t, err)
	for _, name := range []string{"manifest.json", "links.json", "attachments.json", "acme/VKO-1.md", "acme/VKO-2147483647.md"} {
		_, err := os.Stat(filepath.Join(dir, filepath.FromSlash(name)))
		assert.NoError(t, err, name)
	}

	outside := t.TempDir()
	for _, name := range []string{
		`..\escape.md`,
		`..\..\escape.md`,
		`acme\..\..\escape.md`,
		`acme/..\..\escape.md`,
		`acme\VKO-1.md`,
		`C:\escape.md`,
		`C:escape.md`,
		`\\host\share\escape.md`,
		`\escape.md`,
		"../escape.md",
		"acme/../../escape.md",
		"/etc/escape.md",
		"./manifest.json",
		"acme/./VKO-1.md",
		"acme//VKO-1.md",
		"acme/VKO-01.md",
		"acme/VKO-0.md",
		"acme/VKO-1.txt",
		"acme/OPS-1.md",
		"other/VKO-1.md",
		"ACME/VKO-1.md",
		"acme/sub/VKO-1.md",
		"VKO-1.md",
		"escape.md",
		"MANIFEST.JSON",
		"acme/manifest.json",
	} {
		target := filepath.Join(t.TempDir(), "x")
		_, err := unpack(archiveOf(t, regular(name)), target, "acme", "VKO")
		assert.ErrorContains(t, err, "a name no export of acme/VKO holds", name)
		entries, err := os.ReadDir(target)
		require.NoError(t, err)
		assert.Empty(t, entries, "nothing written for %s", name)
	}
	_, err = unpack(archiveOf(t, tar.Header{Name: "acme", Typeflag: tar.TypeDir, Mode: 0o755}), filepath.Join(t.TempDir(), "x"), "acme", "VKO")
	assert.ErrorContains(t, err, "which is no regular file")

	planted := filepath.Join(t.TempDir(), "planted")
	require.NoError(t, os.Mkdir(planted, 0o700))
	if err := os.Symlink(outside, filepath.Join(planted, "acme")); err != nil {
		t.Fatalf("a directory link cannot be made here: %v", err)
	}
	_, err = unpack(archiveOf(t, regular("acme/VKO-1.md")), planted, "acme", "VKO")
	assert.Error(t, err, "the root refuses a link that leads out of it")
	entries, err := os.ReadDir(outside)
	require.NoError(t, err)
	assert.Empty(t, entries, "nothing is written where the link leads")
}
