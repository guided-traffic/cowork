package importer

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"mime/multipart"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// entry of an archive: a path and its bytes; a nil body is a directory.
type archived struct {
	name string
	body []byte
}

func tarGz(t *testing.T, entries ...archived) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		h := &tar.Header{Name: e.name, Mode: 0o644, Size: int64(len(e.body)), Typeflag: tar.TypeReg}
		if e.body == nil {
			h.Typeflag, h.Size = tar.TypeDir, 0
		}
		require.NoError(t, tw.WriteHeader(h))
		_, err := tw.Write(e.body)
		require.NoError(t, err)
	}
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "docs/tickets/link.md", Typeflag: tar.TypeSymlink, Linkname: "README.md"}))
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	return buf.Bytes()
}

// tarGzOfType is a tar.gz of one entry of the type flag, with its bytes.
func tarGzOfType(t *testing.T, flag byte, body []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "contiguous", Mode: 0o644, Size: int64(len(body)), Typeflag: flag}))
	_, err := tw.Write(body)
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	return buf.Bytes()
}

func zipped(t *testing.T, entries ...archived) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		w, err := zw.Create(e.name)
		require.NoError(t, err)
		_, err = w.Write(e.body)
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

// form is a multipart upload of parts named name, each a file of its name
// and bytes.
func form(t *testing.T, name string, parts ...archived) *multipart.Reader {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for _, p := range parts {
		w, err := mw.CreateFormFile(name, p.name)
		require.NoError(t, err)
		_, err = w.Write(p.body)
		require.NoError(t, err)
	}
	require.NoError(t, mw.Close())
	return multipart.NewReader(&buf, mw.Boundary())
}

var ticket = []byte("---\nid: T1\n---\n")

// docs/adr/0051 D1: an upload is a tar.gz, a zip or Markdown files, known by
// their bytes; every file is listed in its order, the ones the import does not
// read with the reason, and the stored form gives them back.
func TestReadUploadTakesTheThreeForms(t *testing.T) {
	archive := tarGz(t, archived{name: "./docs/tickets/", body: nil}, archived{name: "./docs/tickets/001-a.md", body: ticket},
		archived{name: "docs/tickets/archive/image.png", body: []byte("png")}, archived{name: "manifest.json", body: []byte("{}")},
		archived{name: "attachments.json", body: []byte("[]")})
	got, err := ReadUpload(form(t, "file", archived{name: "export.tgz", body: archive}), Limits{MaxBytes: 1 << 20, MaxFiles: MaxFiles})
	require.NoError(t, err)
	assert.Equal(t, []Source{
		{Path: "docs/tickets/001-a.md", Content: ticket},
		{Path: "docs/tickets/archive/image.png", Skip: skipNotMarkdown},
		{Path: "manifest.json", Content: []byte("{}")},
		{Path: "attachments.json", Skip: skipAttachments},
		{Path: "docs/tickets/link.md", Skip: skipNotRegular},
	}, got)

	got, err = ReadUpload(form(t, "file", archived{name: "tickets.zip", body: zipped(t,
		archived{name: "tickets/002-b.md", body: ticket}, archived{name: "tickets/notes.txt", body: []byte("n")})}), Limits{})
	require.NoError(t, err)
	assert.Equal(t, []Source{{Path: "tickets/002-b.md", Content: ticket}, {Path: "tickets/notes.txt", Skip: skipNotMarkdown}}, got)

	got, err = ReadUpload(form(t, "file", archived{name: "003-c.md", body: ticket}, archived{name: "README.md", body: []byte("# r")}), Limits{})
	require.NoError(t, err)
	assert.Equal(t, []Source{{Path: "003-c.md", Content: ticket}, {Path: "README.md", Content: []byte("# r")}}, got)

	packed, err := Pack(got)
	require.NoError(t, err)
	back, err := Unpack(packed)
	require.NoError(t, err)
	assert.Equal(t, got, back)
	skipped := []Source{{Path: "a.png", Skip: skipNotMarkdown}, {Path: "b.md", Content: []byte{}}}
	packed, err = Pack(skipped)
	require.NoError(t, err)
	back, err = Unpack(packed)
	require.NoError(t, err)
	assert.Equal(t, []Source{{Path: "a.png", Content: []byte{}, Skip: skipNotMarkdown}, {Path: "b.md", Content: []byte{}}}, back)
}

// docs/adr/0051 D7, docs/adr/0039 D2: the bytes an upload unpacks to — a
// skipped file's by the size its archive declares — and the files it holds
// are bounded; a path two files share, a part of another name, nothing at
// all and a broken archive are refused.
func TestReadUploadRefusesWhatItCannotTake(t *testing.T) {
	big := bytes.Repeat([]byte("x"), 2048)
	for _, c := range []struct {
		name     string
		in       *multipart.Reader
		lim      Limits
		tooLarge bool
		message  string
	}{
		{"too many bytes", form(t, "file", archived{name: "001-a.md", body: big}), Limits{MaxBytes: 1024}, true, "more than 1024 bytes"},
		{"a skipped file too large", form(t, "file", archived{name: "t.tar.gz", body: tarGz(t, archived{name: "x.bin", body: big})}),
			Limits{MaxBytes: 1024}, true, "more than 1024 bytes"},
		// tar reads through the bytes of an entry that is no regular file, so
		// they count as well.
		{"an entry of another type too large", form(t, "file", archived{name: "t.tar.gz", body: tarGzOfType(t, tar.TypeCont, big)}),
			Limits{MaxBytes: 1024}, true, "more than 1024 bytes"},
		{"too many files", form(t, "file", archived{name: "001-a.md", body: ticket}, archived{name: "002-b.md", body: ticket}),
			Limits{MaxFiles: 1}, true, "more than 1 files"},
		{"a path twice", form(t, "file", archived{name: "001-a.md", body: ticket}, archived{name: "001-a.md", body: ticket}),
			Limits{}, false, `two files at "001-a.md"`},
		{"another part", form(t, "upload", archived{name: "001-a.md", body: ticket}), Limits{}, false, "parts named file only"},
		{"nothing", form(t, "file"), Limits{}, false, "holds no file"},
		{"a broken gzip", form(t, "file", archived{name: "t.tgz", body: append([]byte{0x1f, 0x8b}, big...)}), Limits{}, false, "cannot be read"},
		{"a broken zip", form(t, "file", archived{name: "t.zip", body: append([]byte("PK\x03\x04"), big...)}), Limits{}, false, "zip archive cannot be read"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := ReadUpload(c.in, c.lim)
			var ue *UploadError
			require.ErrorAs(t, err, &ue)
			assert.Equal(t, c.tooLarge, ue.TooLarge)
			assert.Contains(t, ue.Message, c.message)
		})
	}
}

// manyEntries is a zip of n empty entries whose directory's end declares n
// modulo 65,536 of them, as a 16-bit count does — archive/zip compares only
// those bits and reads every entry of the directory regardless.
func manyEntries(n int) []byte {
	var local, central bytes.Buffer
	le := binary.LittleEndian
	for i := range n {
		name := fmt.Sprintf("e/%06d", i)
		offset := local.Len()
		local.Write(le.AppendUint32(nil, 0x04034b50))
		local.Write(make([]byte, 22))
		local.Write(le.AppendUint16(nil, uint16(len(name))))
		local.Write(make([]byte, 2))
		local.WriteString(name)
		central.Write(le.AppendUint32(nil, 0x02014b50))
		central.Write(make([]byte, 24))
		central.Write(le.AppendUint16(nil, uint16(len(name))))
		central.Write(make([]byte, 12))
		central.Write(le.AppendUint32(nil, uint32(offset)))
		central.WriteString(name)
	}
	out := append(local.Bytes(), central.Bytes()...)
	out = le.AppendUint32(out, 0x06054b50)
	out = append(out, make([]byte, 4)...)
	out = le.AppendUint16(out, uint16(n))
	out = le.AppendUint16(out, uint16(n))
	out = le.AppendUint32(out, uint32(central.Len()))
	out = le.AppendUint32(out, uint32(local.Len()))
	return append(out, 0, 0)
}

// docs/adr/0051 D7: a zip's entries are counted before the zip is parsed, so
// a zip of a great many empty entries — more than its directory's end
// declares — is refused at the bound of the files without the memory a parse
// of each entry takes.
func TestReadUploadCountsAZipsEntriesBeforeItParsesThem(t *testing.T) {
	archive := manyEntries(3*65536 + 100)
	declared, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	require.NoError(t, err, "archive/zip takes the zip")
	require.Len(t, declared.File, 3*65536+100, "and parses every entry, though its end declares 100")
	declared = nil
	in := form(t, "file", archived{name: "many.zip", body: archive})
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	_, err = ReadUpload(in, Limits{MaxFiles: MaxFiles})
	runtime.ReadMemStats(&after)

	var ue *UploadError
	require.ErrorAs(t, err, &ue)
	assert.True(t, ue.TooLarge)
	assert.Contains(t, ue.Message, "more than 10000 entries")
	allocated := after.TotalAlloc - before.TotalAlloc
	assert.Less(t, allocated, uint64(3*len(archive)),
		"reading a zip of %d bytes allocated %d bytes: its entries were parsed", len(archive), allocated)

	ok := zipped(t, archived{name: "001-a.md", body: ticket}, archived{name: "002-b.md", body: ticket})
	_, err = ReadUpload(form(t, "file", archived{name: "two.zip", body: ok}), Limits{MaxFiles: 2})
	require.NoError(t, err, "a zip at the bound is read")
	_, err = ReadUpload(form(t, "file", archived{name: "one.md", body: ticket}, archived{name: "two.zip", body: ok}), Limits{MaxFiles: 2})
	require.ErrorAs(t, err, &ue, "the zip's entries count with the files before it")
	assert.True(t, ue.TooLarge)
}

// A repository's frontmatter written by hand that is no YAML — a value with
// a colon in it — is read line by line, and the report says so.
func TestParseReadsAHandWrittenFrontmatterLineByLine(t *testing.T) {
	f := Parse("docs/tickets/048-p.md", []byte("---\nid: T48\ntitle: phase 5\nstate: done\nseverity: high\nsecurity: none\n"+
		"threat:\nurgency: release      # rule 2: gates the release\neffort: L\nopened: 2026-10-04\ndone: 2026-10-04\n"+
		"shipped: 0.3.0 — phase 5: cowork-mcp, \"quoted\" # not a comment? it is\nattachments:\n  - a.png\n  - \"b c.txt\"\n---\n\nBody.\n"))
	assert.Empty(t, f.Errors)
	require.Len(t, f.Warnings, 1)
	assert.Contains(t, f.Warnings[0].Message, "read line by line as key: value")
	assert.Equal(t, "0.3.0 — phase 5: cowork-mcp, \"quoted\"", f.Shipped)
	assert.Equal(t, "release", string(f.Horizon))
	assert.Equal(t, []string{"a.png", "b c.txt"}, f.Attachments)
	assert.Equal(t, "Body.", f.Body)
}
