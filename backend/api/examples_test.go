package apispec

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// uuidPattern is format uuid as the server holds a request to it at the
// boundary (internal/api/validate.go): any version, since the ids are UUIDv7,
// which kin-openapi's RFC 4122 pattern refuses. kin-openapi checks no uuid
// unless it is told how, and the bundler tells it nothing, so without this an
// example could carry an id in no shape at all.
const uuidPattern = `^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`

func init() {
	openapi3.DefineStringFormatValidator("uuid", openapi3.NewRegexpFormatValidator(uuidPattern))
}

// body is one request or response body the document declares: one media type
// of an operation's request body, or of one of its responses.
type body struct {
	// where names the body in a failure: the operation, its method and path,
	// and the status — or the shared response the operation names.
	where     string
	mediaType string
	content   *openapi3.MediaType
	// description is the request body's: the parts of a multipart body are
	// described there.
	description string
	response    bool
}

func (b body) String() string { return fmt.Sprintf("%s (%s)", b.where, b.mediaType) }

// loadDocument loads the bundled document as the server does.
func loadDocument(t *testing.T) *openapi3.T {
	t.Helper()
	doc, err := openapi3.NewLoader().LoadFromData(Document)
	require.NoError(t, err)
	return doc
}

// bodies lists every body of every operation, paths and methods in order. A
// response of components/responses is listed once, under its own name: every
// operation that declares it shares its examples.
func bodies(doc *openapi3.T) []body {
	var out []body
	shared := map[string]bool{}
	for _, path := range slices.Sorted(maps.Keys(doc.Paths.Map())) {
		item := doc.Paths.Value(path)
		for _, method := range slices.Sorted(maps.Keys(item.Operations())) {
			op := item.GetOperation(method)
			operation := fmt.Sprintf("%s (%s %s)", op.OperationID, method, path)
			if op.RequestBody != nil {
				rb := op.RequestBody.Value
				for _, mediaType := range slices.Sorted(maps.Keys(rb.Content)) {
					out = append(out, body{where: operation + " request", mediaType: mediaType,
						content: rb.Content[mediaType], description: rb.Description})
				}
			}
			for _, status := range slices.Sorted(maps.Keys(op.Responses.Map())) {
				ref := op.Responses.Value(status)
				where := operation + " response " + status
				if ref.Ref != "" {
					if shared[ref.Ref] {
						continue
					}
					shared[ref.Ref] = true
					where = "the shared response " + ref.Ref
				}
				for _, mediaType := range slices.Sorted(maps.Keys(ref.Value.Content)) {
					out = append(out, body{where: where, mediaType: mediaType, content: ref.Value.Content[mediaType], response: true})
				}
			}
		}
	}
	return out
}

// examplesOf are a media type's own examples, by name; the one of `example`
// is named "example".
func examplesOf(content *openapi3.MediaType) map[string]any {
	out := map[string]any{}
	if content.Example != nil {
		out["example"] = content.Example
	}
	for name, ref := range content.Examples {
		out[name] = ref.Value.Value
	}
	return out
}

// schemaOf is the schema a body names, an empty one where it names none.
func schemaOf(content *openapi3.MediaType) *openapi3.Schema {
	if content.Schema == nil || content.Schema.Value == nil {
		return &openapi3.Schema{}
	}
	return content.Schema.Value
}

// binary reports whether a body is bytes, of which no example can be written.
func binary(content *openapi3.MediaType) bool {
	s := schemaOf(content)
	return s.Type.Is(openapi3.TypeString) && s.Format == "binary"
}

// lacksExample says what a body lacks of the rule of docs/adr/0046 D6, or ""
// when it holds: an example of its own, or the example of the schema it names
// (docs/developer/api.md says which goes where). A body of bytes has none to
// give. A multipart body without one describes each of its parts by name in
// the request body's description instead.
func lacksExample(b body) string {
	switch {
	case binary(b.content), len(examplesOf(b.content)) > 0, schemaOf(b.content).Example != nil:
		return ""
	case strings.HasPrefix(b.mediaType, "multipart/"):
		for _, part := range slices.Sorted(maps.Keys(schemaOf(b.content).Properties)) {
			if !strings.Contains(b.description, "`"+part+"`") {
				return fmt.Sprintf("no example, and the description names no part `%s`", part)
			}
		}
		return ""
	}
	return "no example, neither its own nor its schema's"
}

// Every request and response body of every operation has an example
// (docs/adr/0046 D6): a request's, a success's, and the problem every error
// answers with, which the operations share through components/responses. A
// response without a body — 204, 304, a redirect — needs none.
func TestEveryBodyHasAnExample(t *testing.T) {
	for _, b := range bodies(loadDocument(t)) {
		if lack := lacksExample(b); lack != "" {
			t.Errorf("%s: %s", b, lack)
		}
	}
}

// Every example validates against its schema (docs/adr/0046 D6) as the
// document's JSON Schema 2020-12 reads it, with the formats the server checks:
// a body's own examples, as a request or as a response, and the example of
// every schema in components/schemas. An event stream's example is its first
// lines, held to the format a client reads.
func TestEveryExampleValidates(t *testing.T) {
	doc := loadDocument(t)
	for _, b := range bodies(doc) {
		examples := examplesOf(b.content)
		for _, name := range slices.Sorted(maps.Keys(examples)) {
			where := fmt.Sprintf("%s, the example %q", b, name)
			mode := openapi3.VisitAsRequest()
			if b.response {
				mode = openapi3.VisitAsResponse()
			}
			validate(t, where, schemaOf(b.content), examples[name], mode)
			if b.mediaType == "text/event-stream" {
				eventStream(t, where, examples[name])
			}
		}
	}
	for _, name := range slices.Sorted(maps.Keys(doc.Components.Schemas)) {
		if s := doc.Components.Schemas[name].Value; s.Example != nil {
			validate(t, "the schema "+name+", its example", s, s.Example)
		}
	}
}

func validate(t *testing.T, where string, schema *openapi3.Schema, example any, opts ...openapi3.SchemaValidationOption) {
	t.Helper()
	opts = append(opts, openapi3.MultiErrors(), openapi3.EnableJSONSchema2020())
	if err := schema.VisitJSON(example, opts...); err != nil {
		t.Errorf("%s does not validate: %v", where, err)
	}
}

// eventStream holds an event stream's example to the text a client reads
// (docs/adr/0054, docs/adr/0076): every line a comment or one of the fields
// event, data, id and retry, every data line the JSON cowork sends.
func eventStream(t *testing.T, where string, example any) {
	t.Helper()
	text, ok := example.(string)
	if !ok {
		t.Errorf("%s is no text, but an event stream's example is the stream's text", where)
		return
	}
	for i, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		field, value, _ := strings.Cut(line, ":")
		switch field {
		case "", "event", "id", "retry":
		case "data":
			if data := strings.TrimPrefix(value, " "); !json.Valid([]byte(data)) {
				t.Errorf("%s, line %d: the data %q is no JSON", where, i+1, data)
			}
		default:
			t.Errorf("%s, line %d: %q is no field of an event stream", where, i+1, line)
		}
	}
}

// The two tests above are not vacuous: their walk reaches the bodies of every
// kind the document has — requests of JSON and a multipart upload; responses
// of JSON, the shared problem, an event stream, CSV, Markdown and bytes.
func TestTheExamplesWalkReachesEveryKindOfBody(t *testing.T) {
	type kind struct {
		response  bool
		mediaType string
	}
	seen := map[kind]bool{}
	for _, b := range bodies(loadDocument(t)) {
		seen[kind{b.response, b.mediaType}] = true
	}
	for _, mediaType := range []string{"application/json", "multipart/form-data"} {
		assert.True(t, seen[kind{false, mediaType}], "the walk reaches a request body of %s", mediaType)
	}
	for _, mediaType := range []string{"application/json", "application/problem+json", "text/event-stream", "text/csv",
		"text/markdown", "*/*"} {
		assert.True(t, seen[kind{true, mediaType}], "the walk reaches a response body of %s", mediaType)
	}
}
