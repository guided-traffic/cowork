package tools

import (
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apispec "github.com/guided-traffic/cowork/backend/api"
)

// The tool set of docs/adr/0042 D1 and D2: twelve workflow tools,
// record_answer, create_project, set_urgency and the api escape hatch, each
// with a name a host accepts, a description, an object schema and the
// operations it calls.
func TestTheCatalogue(t *testing.T) {
	names := make([]string, 0, len(Catalogue()))
	valid := regexp.MustCompile(`^[a-z][a-z_]{1,63}$`)
	for _, tool := range Catalogue() {
		names = append(names, tool.Name)
		assert.Regexp(t, valid, tool.Name)
		assert.NotEmpty(t, tool.Description, tool.Name)
		assert.NotEmpty(t, tool.Operations, tool.Name)
		var schema map[string]any
		require.NoError(t, json.Unmarshal(tool.Schema(), &schema), tool.Name)
		assert.Equal(t, "object", schema["type"], tool.Name)
		assert.Equal(t, false, schema["additionalProperties"], "%s refuses arguments it does not know", tool.Name)
	}
	assert.ElementsMatch(t, []string{"session_start", "get_ticket", "search", "file_ticket", "record_state", "open_question",
		"record_answer", "comment", "transition", "set_progress", "link", "watch", "set_urgency", "finish_work", "create_project",
		"api"}, names)

	anywhere := Catalogue(Anywhere)
	assert.Len(t, anywhere, len(names)-1, "only session_start needs a terminal")
	assert.False(t, slices.ContainsFunc(anywhere, func(t Tool) bool { return t.Name == "session_start" }))
}

// docs/adr/0042 D6: every route a tool uses exists in the API document.
func TestEveryOperationOfAToolIsInTheDocument(t *testing.T) {
	doc, err := openapi3.NewLoader().LoadFromData(apispec.Document)
	require.NoError(t, err)
	var ops []string
	for _, item := range doc.Paths.Map() {
		for _, op := range item.Operations() {
			ops = append(ops, op.OperationID)
		}
	}
	for _, op := range Operations(Catalogue()) {
		assert.Contains(t, ops, op)
	}
	assert.Equal(t, Operations(Catalogue()), documentOperationsOf(t, Operations(Catalogue())))
}

func documentOperationsOf(t *testing.T, ops []string) []string {
	t.Helper()
	var doc map[string]any
	require.NoError(t, json.Unmarshal(apispec.Document, &doc))
	served := documentOperations(doc)
	var out []string
	for _, op := range ops {
		if slices.Contains(served, op) {
			out = append(out, op)
		}
	}
	return out
}

// docs/adr/0042 D3, docs/adr/0043 D6: a description names the limits, and
// with the token read, which of them this token holds.
func TestDescriptionsNameTheLimits(t *testing.T) {
	var transition Tool
	for _, tool := range Catalogue() {
		if tool.Name == "transition" {
			transition = tool
		}
	}
	unknown := transition.Describe(nil)
	assert.Contains(t, unknown, "close for done")
	assert.NotContains(t, unknown, "This agent ")

	assisted := transition.Describe(&Token{Known: true, Agent: true, Capabilities: []string{"drop"}})
	assert.Contains(t, assisted, "holds drop")
	assert.Contains(t, assisted, "lacks decide, close")
	full := transition.Describe(&Token{Known: true, Agent: true, Capabilities: []string{"decide", "close", "drop"}})
	assert.Contains(t, full, "holds decide, close, drop")
	assert.NotContains(t, full, "lacks")
	person := transition.Describe(&Token{Known: true})
	assert.NotContains(t, person, "This agent ", "a person's request is not bounded by capabilities")

	for _, tool := range Catalogue() {
		d := tool.Describe(&Token{Known: true, Agent: true, Capabilities: []string{"close"}})
		assert.NotContains(t, d, "token .", "%s names no capability, and says nothing of them", tool.Name)
	}
}

// The arguments are held to the schema before anything is sent.
func TestArgumentsAreHeldToTheSchema(t *testing.T) {
	f := newFake(t)
	s := f.session(true)
	for args, want := range map[string]string{
		`{"key": "COW-1", "to": "nowhere"}`:          "to",
		`{"key": "COW-1"}`:                           "to",
		`{"key": "COW-1", "to": "done", "extra": 1}`: "extra",
		`not json`: "not JSON",
	} {
		res := call(t, s, "transition", args)
		assert.True(t, res.IsError, args)
		assert.Contains(t, res.Text, want, args)
	}
	assert.Empty(t, f.writes(), "nothing reached the API")
}

func TestAgentHeader(t *testing.T) {
	assert.Equal(t, "claude-code/unknown/abc", AgentHeader("claude-code", "", "abc"))
	assert.Equal(t, "cowork-mcp/opus/session", AgentHeader("", "opus", ""))
	assert.Equal(t, "a-b/m/s", AgentHeader("a/-b", "m", "s"), "a slash would split the header")
	assert.Equal(t, strings.Repeat("x", 64), strings.Split(AgentHeader(strings.Repeat("x", 70), "m", "s"), "/")[0])
	assert.Equal(t, "Claude/m/s", AgentHeader(" Clau de\t", "m", "s"), "spaces and controls go")
}
