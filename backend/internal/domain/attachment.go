package domain

import (
	"bytes"
	"encoding/xml"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// The attachment types the server stores (docs/adr/0016 D3): the type is
// detected from the bytes, never taken from the client.
const (
	TypePNG  = "image/png"
	TypeJPEG = "image/jpeg"
	TypeGIF  = "image/gif"
	TypeWebP = "image/webp"
	TypePDF  = "application/pdf"
	TypeText = "text/plain; charset=utf-8"
	TypeSVG  = "image/svg+xml"
)

// raster are the types delivered inline; everything else is a download
// (docs/adr/0016 D5).
var raster = map[string]bool{TypePNG: true, TypeJPEG: true, TypeGIF: true, TypeWebP: true}

var allowed = map[string]bool{TypePNG: true, TypeJPEG: true, TypeGIF: true, TypeWebP: true, TypePDF: true, TypeText: true}

// DetectAttachmentType returns the type an upload is stored as, judged from
// its first bytes, and the type detected. Markdown and patches are plain
// UTF-8 text; text or XML whose root element is svg is stored as SVG, a file
// never rendered as an image; anything else is refused (ok false) and the
// refusal names the detected type.
func DetectAttachmentType(b []byte) (stored, detected string, ok bool) {
	detected = http.DetectContentType(b)
	if (strings.HasPrefix(detected, "text/plain") || strings.HasPrefix(detected, "text/xml")) && rootIsSVG(b) {
		return TypeSVG, detected, true
	}
	if allowed[detected] {
		return detected, detected, true
	}
	return "", detected, false
}

func rootIsSVG(b []byte) bool {
	dec := xml.NewDecoder(bytes.NewReader(b))
	for {
		tok, err := dec.Token()
		if err != nil {
			return false
		}
		if se, ok := tok.(xml.StartElement); ok {
			return se.Name.Local == "svg"
		}
	}
}

// InlineAttachment reports whether the type is delivered inline: raster
// images only (docs/adr/0016 D5).
func InlineAttachment(contentType string) bool { return raster[contentType] }

// maxFileName is the longest file name kept, in bytes.
const maxFileName = 255

// SanitizeFileName keeps the last path component of an uploaded file name,
// without control characters, bidirectional controls (a right-to-left
// override would make "txt.exe" read as "exe.txt") or quotes, in NFC, at most
// 255 bytes cut at a character boundary (docs/adr/0016 D1); an empty result
// is "attachment".
func SanitizeFileName(name string) string {
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Bidi_Control, r) || r == '"' || r == utf8.RuneError {
			return -1
		}
		return r
	}, norm.NFC.String(name))
	name = strings.TrimSpace(name)
	for len(name) > maxFileName {
		_, size := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-size]
	}
	if name == "" || name == "." || name == ".." {
		return "attachment"
	}
	return name
}

// extensions are the endings a stored type keeps; a name that ends in none of
// them gets the first.
var extensions = map[string][]string{
	TypePNG: {".png"}, TypeJPEG: {".jpg", ".jpeg"}, TypeGIF: {".gif"}, TypeWebP: {".webp"},
	TypePDF: {".pdf"}, TypeSVG: {".svg"},
	TypeText: {".txt", ".md", ".markdown", ".patch", ".diff", ".log"},
}

// FileNameFor makes a sanitised name end in an extension of the type the
// bytes were detected as: "run.bat" detected as text becomes "run.bat.txt",
// so a download never carries an ending that makes the recipient's system run
// it (docs/adr/0016 D5).
func FileNameFor(name, contentType string) string {
	exts := extensions[contentType]
	if len(exts) == 0 {
		return name
	}
	lower := strings.ToLower(name)
	for _, e := range exts {
		if strings.HasSuffix(lower, e) && len(lower) > len(e) {
			return name
		}
	}
	for len(name)+len(exts[0]) > maxFileName {
		_, size := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-size]
	}
	return name + exts[0]
}
