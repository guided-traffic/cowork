// Package importer reads what an import takes (docs/adr/0051, docs/adr/0063):
// an upload of a tar.gz, a zip or Markdown files, each ticket file in the
// grammar of a repository's docs/tickets/ or in cowork's own grammar v1
// (docs/adr/0044 D1); and it analyses the files against the project they go
// into, into the report a person corrects and the plan the execution writes.
// It is pure: what it needs of the database comes in as a Target, and the
// writes are the API's.
package importer

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path"
	"strings"
)

// Source is one file of an upload, in the order the upload carried it.
// Content is what the import reads; Skip says why a file is not read, and its
// Content is then empty.
type Source struct {
	Path    string
	Content []byte
	Skip    string
}

// Limits bound what an upload unpacks to (docs/adr/0051 D7,
// docs/adr/0039 D2): MaxBytes the bytes its files hold together, those the
// import skips by the size their archive declares, 0 for no bound; MaxFiles
// the files it holds.
type Limits struct {
	MaxBytes int64
	MaxFiles int
}

// MaxFiles is how many files one upload may hold, read or skipped: the work
// of a dry run and the size of its report stay bounded whatever an archive
// packs.
const MaxFiles = 10000

// The names of an export's manifests (docs/adr/0051 D4).
const (
	ManifestFile    = "manifest.json"
	LinksFile       = "links.json"
	AttachmentsFile = "attachments.json"
)

// The reasons an upload's file is not read.
const (
	skipNotMarkdown  = "not a Markdown file: the import reads ticket files only (docs/adr/0063 D5)"
	skipNotRegular   = "not a regular file"
	skipAttachments  = "the attachments manifest of an export: an export carries no bytes, and the import brings no file (docs/adr/0051 D4)"
	formFieldFile    = "file"
	gzipMagic        = "\x1f\x8b"
	zipMagic         = "PK\x03\x04"
	zipEmptyMagic    = "PK\x05\x06"
	zipCentralMagic  = "PK\x01\x02"
	maxPartNameBytes = 1024
	// maxPathBytes is the longest path a ticket records as the file it came
	// from (docs/adr/0051 D3), and a correction names.
	maxPathBytes = 1024
)

// UploadError is an upload the import refuses as a whole: TooLarge for one
// past the limits, which the API answers 413; anything else is a malformed
// upload, 400.
type UploadError struct {
	TooLarge bool
	Message  string
}

func (e *UploadError) Error() string { return e.Message }

func malformed(format string, args ...any) error {
	return &UploadError{Message: fmt.Sprintf(format, args...)}
}

// upload collects the files of an upload under its limits.
type upload struct {
	lim     Limits
	files   []Source
	paths   map[string]bool
	size    int64
	entries int
}

// ReadUpload reads a multipart upload of one or more parts named file: each
// part is a tar.gz or a zip archive, known by its first bytes, or one file
// named by the part's file name. A path that two files share, a part of
// another name, and an upload past the limits are refused.
func ReadUpload(mr *multipart.Reader, lim Limits) ([]Source, error) {
	u := &upload{lim: lim, paths: map[string]bool{}}
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read the upload: %w", err)
		}
		if part.FormName() != formFieldFile {
			return nil, malformed("an upload takes parts named file only, not %q", part.FormName())
		}
		if err := u.part(part); err != nil {
			return nil, err
		}
	}
	if len(u.files) == 0 {
		return nil, malformed("the upload holds no file")
	}
	return u.files, nil
}

// part reads one part: an archive's files, or the part as one file.
func (u *upload) part(p *multipart.Part) error {
	r := bufio.NewReader(p)
	head, _ := r.Peek(4)
	switch {
	case bytes.HasPrefix(head, []byte(gzipMagic)):
		return u.tarGz(r)
	case bytes.HasPrefix(head, []byte(zipMagic)), bytes.HasPrefix(head, []byte(zipEmptyMagic)):
		return u.zip(r)
	}
	name := p.FileName()
	if name == "" || len(name) > maxPartNameBytes {
		return malformed("a part of the upload names no file")
	}
	return u.add(name, r, true)
}

// tarGz reads a gzip-compressed tar archive as it streams in.
func (u *upload) tarGz(r io.Reader) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return u.archiveError(err, "tar.gz")
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return u.archiveError(err, "tar.gz")
		}
		if err := u.tarEntry(h, tr); err != nil {
			return u.archiveError(err, "tar.gz")
		}
	}
}

// tarEntry records one entry of a tar archive; a file the import does not
// read counts by the size the archive declares, since tar reads through its
// bytes to the next entry — an entry that is no regular file as well.
func (u *upload) tarEntry(h *tar.Header, tr *tar.Reader) error {
	switch h.Typeflag {
	case tar.TypeDir, tar.TypeXGlobalHeader:
		return nil
	case tar.TypeReg:
		if reason := skipReason(h.Name); reason != "" {
			return u.skip(h.Name, reason, h.Size)
		}
		return u.add(h.Name, tr, false)
	}
	return u.skip(h.Name, skipNotRegular, h.Size)
}

// zip reads a zip archive, which keeps its directory at its end and is read
// whole — the request body's limit bounds it. zip.NewReader parses every
// entry of the directory before any bound here applies, and reads on past the
// count the directory's end declares; every entry it parses starts with the
// central header's signature, so a zip whose bytes hold more of those than
// the upload may still hold files is refused before it is parsed.
func (u *upload) zip(r io.Reader) error {
	raw, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("read the upload: %w", err)
	}
	if left := u.lim.MaxFiles - u.entries; u.lim.MaxFiles > 0 && bytes.Count(raw, []byte(zipCentralMagic)) > left {
		return &UploadError{TooLarge: true, Message: fmt.Sprintf("the upload holds more than %d entries, a zip's directories included", u.lim.MaxFiles)}
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return malformed("the zip archive cannot be read: %v", err)
	}
	for _, f := range zr.File {
		if err := u.zipEntry(f); err != nil {
			return err
		}
	}
	return nil
}

// zipEntry records one entry of a zip archive; a file the import does not
// read counts by the size the archive declares and is never decompressed.
func (u *upload) zipEntry(f *zip.File) error {
	mode := f.Mode()
	switch {
	case mode.IsDir():
		return nil
	case !mode.IsRegular():
		return u.skip(f.Name, skipNotRegular, 0)
	}
	if reason := skipReason(f.Name); reason != "" {
		return u.skip(f.Name, reason, int64(min(f.UncompressedSize64, uint64(1)<<62)))
	}
	rc, err := f.Open()
	if err != nil {
		return malformed("the zip archive's file %q cannot be read: %v", f.Name, err)
	}
	defer func() { _ = rc.Close() }()
	if err := u.add(f.Name, rc, false); err != nil {
		return u.archiveError(err, "zip")
	}
	return nil
}

// archiveError keeps an UploadError and an error of the request's body — its
// limit, its deadline —, and names a broken archive otherwise.
func (u *upload) archiveError(err error, kind string) error {
	var ue *UploadError
	var tooBig *http.MaxBytesError
	if errors.As(err, &ue) || errors.As(err, &tooBig) || errors.Is(err, os.ErrDeadlineExceeded) {
		return err
	}
	return malformed("the %s archive cannot be read: %v", kind, err)
}

// add records one file: its bytes when the import reads it, its reason
// otherwise. base says the name is a part's file name, a base name.
func (u *upload) add(name string, r io.Reader, base bool) error {
	p, ok := cleanPath(name, base)
	if !ok {
		return nil
	}
	if reason := skipReason(p); reason != "" {
		return u.skip(p, reason, 0)
	}
	if err := u.count(p); err != nil {
		return err
	}
	var content []byte
	var err error
	if u.lim.MaxBytes > 0 {
		content, err = io.ReadAll(io.LimitReader(r, u.lim.MaxBytes-u.size+1))
	} else {
		content, err = io.ReadAll(r)
	}
	if err != nil {
		return err
	}
	if err := u.grow(int64(len(content))); err != nil {
		return err
	}
	u.files = append(u.files, Source{Path: p, Content: content})
	return nil
}

// skip records a file the import does not read, of the size its archive
// declares.
func (u *upload) skip(name, reason string, size int64) error {
	p, ok := cleanPath(name, false)
	if !ok {
		return nil
	}
	if err := u.count(p); err != nil {
		return err
	}
	if err := u.grow(size); err != nil {
		return err
	}
	u.files = append(u.files, Source{Path: p, Skip: reason})
	return nil
}

// grow holds the upload's files together to MaxBytes.
func (u *upload) grow(n int64) error {
	u.size += n
	if u.lim.MaxBytes > 0 && (n > u.lim.MaxBytes || u.size > u.lim.MaxBytes) {
		return &UploadError{TooLarge: true, Message: fmt.Sprintf("the files of the upload hold more than %d bytes", u.lim.MaxBytes)}
	}
	return nil
}

// count holds the upload to MaxFiles and refuses a path two files share: a
// correction names a file by its path.
func (u *upload) count(p string) error {
	if len(p) > maxPathBytes {
		return malformed("the upload holds a path longer than %d bytes", maxPathBytes)
	}
	u.entries++
	if u.lim.MaxFiles > 0 && u.entries > u.lim.MaxFiles {
		return &UploadError{TooLarge: true, Message: fmt.Sprintf("the upload holds more than %d files", u.lim.MaxFiles)}
	}
	if u.paths[p] {
		return malformed("the upload holds two files at %q", p)
	}
	u.paths[p] = true
	return nil
}

// cleanPath is a file's path as the report names it: `/`-separated, relative,
// without `.` or `..` — a label, never a place anything is written to. A
// part's file name stands as it is. false for a name that is no file.
func cleanPath(name string, base bool) (string, bool) {
	name = strings.ReplaceAll(name, "\\", "/")
	if base {
		name = path.Base(name)
	}
	p := strings.TrimPrefix(path.Clean("/"+name), "/")
	if p == "" || p == "." {
		return "", false
	}
	return p, true
}

// skipReason is why the import does not read a file of this path, "" for a
// file it reads: a Markdown file, and the manifests of an export it reads
// beside the tickets.
func skipReason(p string) string {
	base := path.Base(p)
	switch {
	case base == ManifestFile, base == LinksFile:
		return ""
	case base == AttachmentsFile:
		return skipAttachments
	case strings.HasSuffix(strings.ToLower(base), ".md"):
		return ""
	}
	return skipNotMarkdown
}

// paxSkip is the record that keeps a skipped file's reason in the stored form.
const paxSkip = "COWORK.skip"

// Pack is the stored form of an upload's files, which the dry run keeps for
// its execution (docs/adr/0051 D2): a gzip-compressed tar, a skipped file with
// its reason and no bytes.
func Pack(files []Source) ([]byte, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, f := range files {
		h := &tar.Header{Name: f.Path, Mode: 0o600, Size: int64(len(f.Content)), Typeflag: tar.TypeReg, Format: tar.FormatPAX}
		if f.Skip != "" {
			h.PAXRecords = map[string]string{paxSkip: f.Skip}
		}
		if err := tw.WriteHeader(h); err != nil {
			return nil, fmt.Errorf("pack the upload: %w", err)
		}
		if _, err := tw.Write(f.Content); err != nil {
			return nil, fmt.Errorf("pack the upload: %w", err)
		}
	}
	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("pack the upload: %w", err)
	}
	if err := gz.Close(); err != nil {
		return nil, fmt.Errorf("pack the upload: %w", err)
	}
	return buf.Bytes(), nil
}

// Unpack reads the stored form back, the files in their order.
func Unpack(packed []byte) ([]Source, error) {
	gz, err := gzip.NewReader(bytes.NewReader(packed))
	if err != nil {
		return nil, fmt.Errorf("unpack the upload: %w", err)
	}
	tr := tar.NewReader(gz)
	var files []Source
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return files, nil
		}
		if err != nil {
			return nil, fmt.Errorf("unpack the upload: %w", err)
		}
		content, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("unpack the upload: %w", err)
		}
		files = append(files, Source{Path: h.Name, Content: content, Skip: h.PAXRecords[paxSkip]})
	}
}
