package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/tools"
)

// connect serves a session over an in-memory transport and returns the
// client's side.
func connect(t *testing.T, o Options) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	serverSide, clientSide := mcp.NewInMemoryTransports()
	ss, err := New(o).Connect(ctx, serverSide, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "claude-code", Version: "test"}, nil).Connect(ctx, clientSide, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func fakeSession(t *testing.T, mux *http.ServeMux, agent func() string) *tools.Session {
	t.Helper()
	api, err := apigen.NewClientWithResponses("http://cowork.test", apigen.WithHTTPClient(tools.HandlerDoer{Handler: mux}),
		apigen.WithRequestEditorFn(tools.Editor("cwk_0000000000000000000000000000000000000000000", agent, "test")))
	require.NoError(t, err)
	s := tools.NewSession(api, "https://cowork.example.com")
	s.Bind("acme", "COW")
	return s
}

func text(r *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range r.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

// The catalogue over MCP (docs/adr/0041 D1, D6): every tool listed with its
// schema and a description naming the token's limits (docs/adr/0043 D6), a
// call answered with the tool's Markdown, a refusal of the API a tool error
// with its code, and the client's name in the agent header (docs/adr/0036 D3).
func TestServingTheCatalogue(t *testing.T) {
	mux := http.NewServeMux()
	var mu sync.Mutex
	var agents []string
	mux.HandleFunc("PUT /api/v1/teams/acme/projects/COW/tickets/12/interest", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		agents = append(agents, r.Header.Get("X-Cowork-Agent"))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{}`))
	})
	mux.HandleFunc("PUT /api/v1/teams/acme/projects/COW/tickets/13/interest", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"type":"t","title":"Not found","status":404,"code":"not_found","detail":"no such ticket"}`))
	})
	var name string
	var nameMu sync.Mutex
	agent := func() string {
		nameMu.Lock()
		defer nameMu.Unlock()
		return tools.AgentHeader(name, "unknown", "s1")
	}
	cs := connect(t, Options{
		Session: fakeSession(t, mux, agent), Version: "0.9.0",
		Token:  &tools.Token{Known: true, Agent: true, Capabilities: []string{"drop"}},
		Client: func(n string) { nameMu.Lock(); name = n; nameMu.Unlock() },
	})
	ctx := context.Background()

	listed, err := cs.ListTools(ctx, nil)
	require.NoError(t, err)
	require.Len(t, listed.Tools, len(tools.Catalogue()))
	byName := map[string]*mcp.Tool{}
	for _, tool := range listed.Tools {
		byName[tool.Name] = tool
	}
	assert.Contains(t, byName["transition"].Description, "lacks decide, close")
	assert.True(t, byName["get_ticket"].Annotations.ReadOnlyHint)
	schema, err := json.Marshal(byName["finish_work"].InputSchema)
	require.NoError(t, err)
	assert.Contains(t, string(schema), `"verification_note"`)
	assert.Contains(t, cs.InitializeResult().Instructions, "one at a time")
	assert.Contains(t, cs.InitializeResult().Instructions, "A ticket's key is team/PROJECT-n", "the model reads team (docs/adr/0005 D1)")
	assert.NotContains(t, strings.ToLower(cs.InitializeResult().Instructions), "tenant")

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "watch", Arguments: map[string]any{"key": "COW-12"}})
	require.NoError(t, err)
	assert.False(t, res.IsError, text(res))
	assert.Equal(t, "The person watches acme/COW-12.", text(res))
	require.Len(t, agents, 1)
	assert.Equal(t, "claude-code/unknown/s1", agents[0], "the MCP client's name is the agent's")

	res, err = cs.CallTool(ctx, &mcp.CallToolParams{Name: "watch", Arguments: map[string]any{"key": "COW-13"}})
	require.NoError(t, err)
	assert.True(t, res.IsError, "a refusal is a tool error the model sees, not a protocol error")
	assert.Contains(t, text(res), "404 `not_found`: no such ticket")

	res, err = cs.CallTool(ctx, &mcp.CallToolParams{Name: "watch", Arguments: map[string]any{"ticket": "COW-12"}})
	require.NoError(t, err)
	assert.True(t, res.IsError, "arguments outside the schema")
}

// docs/adr/0040 D5: a server that does not know the API refuses every tool
// with the reason.
func TestARefusedServerAnswersEveryToolWithTheReason(t *testing.T) {
	cs := connect(t, Options{Session: fakeSession(t, http.NewServeMux(), func() string { return "" }),
		Ready: func(context.Context) error { return errors.New("the installation runs cowork 2.0.0") }})
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "watch", Arguments: map[string]any{"key": "COW-12"}})
	require.NoError(t, err)
	assert.True(t, res.IsError)
	assert.Equal(t, "cowork is not available to this session: the installation runs cowork 2.0.0", text(res))
}
