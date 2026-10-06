package importer

import (
	"bytes"
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"

	"github.com/guided-traffic/cowork/backend/internal/domain"
)

// Format is the grammar a ticket file was read in.
type Format string

// The three grammars: a repository's ticket file (docs/tickets/README.md,
// docs/adr/0063 D3), cowork's own grammar v1 (docs/adr/0044 D1), and a file
// without frontmatter (docs/adr/0063 D4).
const (
	FormatRepository Format = "repository"
	FormatExport     Format = "export"
	FormatPlain      Format = "plain"
)

// Message is a warning or an error of a file: what it is about — a
// frontmatter key, or file, body, Q<n>, links.json —, its line from 1 where
// one applies, and the text.
type Message struct {
	Field   string
	Line    int
	Message string
}

// Question is an open question of a ticket file (docs/adr/0011 D2, D4).
type Question struct {
	Number                            int32
	Question, Options, Recommendation string
	// Status is open, answered or withdrawn; Answer is set when answered.
	Status, Answer string
	Line           int
}

// The statuses of a question.
const (
	questionOpen      = "open"
	questionAnswered  = "answered"
	questionWithdrawn = "withdrawn"
)

// File is a ticket file as Parse reads it. A value the file does not name is
// empty; a value outside its vocabulary is an error and stays empty
// (docs/adr/0010 D5).
type File struct {
	Path   string
	Format Format
	// Skip is why the file is no ticket file (docs/adr/0063 D5).
	Skip string
	// Number is the ticket's number, 0 where the file names none; Local says
	// the name carries the embargo's local_ prefix.
	Number int32
	Local  bool
	// Key is an export's key, the ticket's key where it came from.
	Key   domain.TicketKey
	Title string
	Type  domain.TicketType
	State domain.TicketState
	// The columns (docs/adr/0010 D1).
	Severity domain.Severity
	Security domain.SecurityClass
	Threat   string
	Horizon  domain.Urgency
	Effort   domain.Effort
	// Stages are refinement, implementation and review; Staged says the file
	// names them.
	Stages [3]int
	Staged bool
	// Assignee and Parent are the values as the file writes them.
	Assignee, Parent       string
	Opened, Decided, Done  *time.Time
	Shipped, DroppedReason string
	// BlockedBy, BlockedReason and BlockedFrom are the block of an export, or
	// a repository's blocked-by — a ticket, a kind, an ADR.
	BlockedBy, BlockedReason, BlockedFrom string
	FiledFrom                             string
	PublicationAccepted                   string
	// Confidential is an export's `confidential: true`.
	Confidential bool
	Attachments  []string
	Body         string
	Questions    []Question
	Warnings     []Message
	Errors       []Message
	// lines are the frontmatter keys' lines; unreadable says the file could
	// not be read as far as its frontmatter, and the analysis reads no more
	// of it than its number.
	lines      map[string]int
	unreadable bool
}

func (f *File) warn(field string, line int, format string, args ...any) {
	f.Warnings = append(f.Warnings, Message{Field: field, Line: line, Message: fmt.Sprintf(format, args...)})
}

func (f *File) fail(field string, line int, format string, args ...any) {
	f.Errors = append(f.Errors, Message{Field: field, Line: line, Message: fmt.Sprintf(format, args...)})
}

// line is the line of a frontmatter key, 0 where the file has none.
func (f *File) line(key string) int { return f.lines[key] }

// failed reports whether the file has an error about the field already.
func (f *File) failed(field string) bool {
	for _, e := range f.Errors {
		if e.Field == field {
			return true
		}
	}
	return false
}

// The file names of a ticket: a repository's NNN-<slug>.md, with the
// embargo's local_ prefix or without (docs/tickets/README.md), and an
// export's <PROJECT>-<n>.md (docs/adr/0051 D4).
var (
	repositoryName = regexp.MustCompile(`^(local_)?([0-9]{3,10})-.+\.md$`)
	exportName     = regexp.MustCompile(`^([A-Z][A-Z0-9]{1,9})-([1-9][0-9]{0,9})\.md$`)
)

// byteOrderMark is UTF-8's, which some editors write first.
const byteOrderMark = "\xef\xbb\xbf"

// contextMarker starts a /context document, which is no import format
// (docs/adr/0044 D3).
const contextMarker = "<!-- cowork: context of "

// The fields Parse names in its messages beside the frontmatter's keys.
const (
	fieldFile = "file"
	fieldBody = "body"
)

const skipNotTicketName = "not a ticket file: its name is neither NNN-<slug>.md, local_NNN-<slug>.md nor <PROJECT>-<n>.md (docs/adr/0063 D5)"

// Parse reads one Markdown file of an upload. A file whose name is no ticket
// file's is skipped; everything else is read as far as it goes, and what
// cannot be read is an error of the file, with its line where it has one.
func Parse(p string, content []byte) File {
	f := File{Path: p, lines: map[string]int{}}
	base := path.Base(p)
	if m := repositoryName.FindStringSubmatch(base); m != nil {
		f.Local = m[1] != ""
		f.Number = number(m[2])
	} else if m := exportName.FindStringSubmatch(base); m != nil {
		f.Number = number(m[2])
	} else {
		f.Skip = skipNotTicketName
		return f
	}
	if !utf8.Valid(content) {
		f.Format, f.unreadable = FormatPlain, true
		f.fail(fieldFile, 0, "the file is not UTF-8 text")
		return f
	}
	text := strings.ReplaceAll(string(bytes.TrimPrefix(content, []byte(byteOrderMark))), "\r\n", "\n")
	if strings.HasPrefix(text, contextMarker) {
		f.Format, f.unreadable = FormatExport, true
		f.fail(fieldFile, contextLine(text), "a /context document is no import format: its read-only sections start here (docs/adr/0044 D3)")
		return f
	}
	front, rest, restLine, ok := splitFrontmatter(text)
	switch {
	case !ok:
		f.Format, f.unreadable = FormatRepository, true
		f.fail(fieldFile, 1, "the frontmatter that starts on line 1 has no closing --- line")
		return f
	case front == nil:
		f.Format = FormatPlain
	default:
		f.readFrontmatter(*front, base)
		if f.unreadable {
			return f
		}
	}
	f.Body, f.Questions = splitBody(rest, restLine, &f)
	if f.Format == FormatPlain && f.Title == "" {
		f.Title = plainTitle(base, f.Body)
	}
	return f
}

// number reads the digits of a file name or a key; 0 beyond the column.
func number(digits string) int32 {
	n, err := strconv.ParseInt(digits, 10, 32)
	if err != nil || n <= 0 {
		return 0
	}
	return int32(n)
}

// contextLine is the line of a /context document where its first read-only
// section starts, `## Links`; the marker's line where it has none.
func contextLine(text string) int {
	for i, l := range strings.Split(text, "\n") {
		if strings.TrimSpace(l) == "## Links" {
			return i + 1
		}
	}
	return 1
}

// splitFrontmatter cuts a file into its frontmatter and the text after it,
// with the line the text starts on. front is nil for a file without
// frontmatter; ok is false for one whose frontmatter does not end.
func splitFrontmatter(text string) (front *string, rest string, restLine int, ok bool) {
	lines := strings.Split(text, "\n")
	if strings.TrimRight(lines[0], " \t") != "---" {
		return nil, text, 1, true
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i], " \t") == "---" {
			fm := strings.Join(lines[1:i], "\n")
			return &fm, strings.Join(lines[i+1:], "\n"), i + 2, true
		}
	}
	return nil, "", 0, false
}

// plainTitle is the title of a file without frontmatter: its first heading,
// else its name without the number and the extension.
func plainTitle(base, body string) string {
	for _, l := range strings.Split(body, "\n") {
		if t, ok := strings.CutPrefix(l, "# "); ok && strings.TrimSpace(t) != "" {
			return clip(oneLine(t), maxTitle)
		}
	}
	name := strings.TrimSuffix(strings.TrimPrefix(base, "local_"), ".md")
	if _, slug, ok := strings.Cut(name, "-"); ok {
		name = slug
	}
	return clip(strings.ReplaceAll(name, "-", " "), maxTitle)
}

// maxTitle is the longest title the column takes.
const maxTitle = 300

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n]))
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// readFrontmatter reads the frontmatter's keys: the key `key` makes the file
// an export's, anything else a repository's. A repository's frontmatter is
// written by hand, one `key: value` per line, and a value such as
// `0.3.0 — phase 5: …` is no YAML: such a frontmatter is read line by line,
// and the report says so.
func (f *File) readFrontmatter(front, base string) {
	var doc yaml.Node
	f.Format = FormatRepository
	var pairs []pair
	if err := yaml.Unmarshal([]byte(front), &doc); err != nil {
		line, msg := yamlError(err)
		var flat bool
		if pairs, flat = linePairs(front); !flat {
			f.unreadable = true
			f.fail(fieldFile, line, "the frontmatter is not YAML: %s", msg)
			return
		}
		f.warn(fieldFile, line, "the frontmatter is not YAML (%s); it is read line by line as key: value", msg)
	} else {
		var ok bool
		if pairs, ok = mappingPairs(&doc); !ok {
			f.unreadable = true
			f.fail(fieldFile, 2, "the frontmatter is not a list of key: value lines")
			return
		}
	}
	seen := map[string]int{}
	for _, p := range pairs {
		if line, twice := seen[p.key]; twice {
			f.unreadable = true
			f.fail(fieldFile, p.line, "the frontmatter names %s twice, also on line %d", p.key, line)
			return
		}
		seen[p.key] = p.line
	}
	for _, p := range pairs {
		if p.key == "key" {
			f.Format = FormatExport
		}
		f.lines[p.key] = p.line
	}
	r := frontReader{f: f}
	for _, p := range pairs {
		r.read(p)
	}
	r.finish(base)
}

// yamlLine is where the YAML parser says an error lies, a line of the
// frontmatter.
var yamlLine = regexp.MustCompile(`^yaml: line ([0-9]+): (.*)$`)

// yamlError is the file line and the message of a YAML error: the
// frontmatter starts on the file's second line.
func yamlError(err error) (int, string) {
	if m := yamlLine.FindStringSubmatch(err.Error()); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n + 1, m[2]
	}
	return 0, strings.TrimPrefix(err.Error(), "yaml: ")
}

// The lines of a frontmatter read line by line: `key: value`, and a list's
// `  - item` under a key without a value.
var (
	flatKeyLine  = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9_-]*):(?:[ \t]+(.*))?$`)
	flatItemLine = regexp.MustCompile(`^[ \t]+-[ \t]+(.*)$`)
	flatComment  = regexp.MustCompile(`[ \t]+#.*$`)
)

// linePairs reads a frontmatter that is no YAML line by line; false when a
// line is neither a key, a list item under one, a comment nor blank. A value
// YAML reads on its own line is read so — quotes, escapes, a comment —, any
// other is the text up to a comment.
func linePairs(front string) ([]pair, bool) {
	var out []pair
	for i, l := range strings.Split(front, "\n") {
		line := i + 2
		trimmed := strings.TrimSpace(l)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if m := flatItemLine.FindStringSubmatch(l); m != nil && len(out) > 0 {
			last := &out[len(out)-1]
			if last.value.Kind == yaml.ScalarNode && last.value.Tag == "!!null" {
				last.value = &yaml.Node{Kind: yaml.SequenceNode}
			}
			if last.value.Kind != yaml.SequenceNode {
				return nil, false
			}
			last.value.Content = append(last.value.Content, flatValue(m[1]))
			continue
		}
		m := flatKeyLine.FindStringSubmatch(l)
		if m == nil {
			return nil, false
		}
		out = append(out, pair{key: m[1], value: flatValue(m[2]), line: line})
	}
	return out, true
}

// flatValue is the scalar of one value read on its own.
func flatValue(raw string) *yaml.Node {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte("v: "+raw), &doc); err == nil {
		if pairs, ok := mappingPairs(&doc); ok && len(pairs) == 1 && pairs[0].value.Kind == yaml.ScalarNode {
			return pairs[0].value
		}
	}
	v := strings.TrimSpace(flatComment.ReplaceAllString(raw, ""))
	if v == "" {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null"}
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}
}

// pair is one key of the frontmatter with its value and line.
type pair struct {
	key   string
	value *yaml.Node
	line  int
}

// mappingPairs lists a document's keys in their order; false for a document
// that is no mapping. An empty frontmatter has no keys.
func mappingPairs(doc *yaml.Node) ([]pair, bool) {
	if doc.Kind == 0 {
		return nil, true
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, false
	}
	m := doc.Content[0]
	out := make([]pair, 0, len(m.Content)/2)
	for i := 0; i+1 < len(m.Content); i += 2 {
		// The frontmatter starts on the file's second line.
		out = append(out, pair{key: m.Content[i].Value, value: m.Content[i+1], line: m.Content[i].Line + 1})
	}
	return out, true
}
