//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api"
	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/events"
	"github.com/guided-traffic/cowork/backend/internal/httpserver"
	"github.com/guided-traffic/cowork/backend/internal/storage"
	"github.com/guided-traffic/cowork/backend/test/fixture"
)

// testSessionKey is the server key of the API tests; 32 bytes.
var testSessionKey = []byte("0123456789abcdef0123456789abcdef")

// apiServer runs the whole handler — health, request id, log, the API
// pipeline — against the runtime role, with every response checked against
// the API document (docs/adr/0046 D4).
type apiServer struct {
	URL string
	// Hub is the server's event hub; its listener hears the run's database.
	Hub *events.Hub
}

func newAPI(t *testing.T, opts ...func(*api.Options)) apiServer {
	t.Helper()
	o := api.Options{
		Logger:            slog.New(slog.NewTextHandler(testLog{t}, &slog.HandlerOptions{Level: slog.LevelWarn})),
		Version:           "9.9.9-test",
		Commit:            "test",
		BuildTime:         "0",
		SessionKey:        testSessionKey,
		MaxJSONBody:       1 << 20,
		RequestTimeout:    30 * time.Second,
		MaxPageSize:       200,
		MaxQueryLength:    256,
		ValidateResponses: true,

		Storage:                testStorage(t),
		AttachmentMaxBytes:     1 << 20,
		AttachmentMaxPerTicket: 5,
	}
	for _, f := range opts {
		f(&o)
	}
	if o.DB == nil {
		o.DB = openRuntime(t)
	}
	if o.Events == nil {
		o.Events = events.New(time.Minute, 10)
	}
	listening := make(chan struct{})
	var once sync.Once
	ctx, cancel := context.WithCancel(context.Background())
	go o.DB.Listen(ctx, o.Events.Publish, func(up bool) {
		o.Events.SetUp(up)
		if up {
			once.Do(func() { close(listening) })
		}
	})
	select {
	case <-listening:
	case <-time.After(10 * time.Second):
		t.Fatal("the event listener did not start")
	}
	h, err := api.New(o)
	require.NoError(t, err)
	srv := httptest.NewServer(httpserver.New(httpserver.Options{API: h, Logger: o.Logger}))
	t.Cleanup(func() {
		o.Events.Close()
		srv.Close()
		cancel()
	})
	return apiServer{URL: srv.URL, Hub: o.Events}
}

// testStorage connects to this run's bucket.
func testStorage(t *testing.T) *storage.Client {
	t.Helper()
	c, err := storage.New(env.Storage)
	require.NoError(t, err)
	return c
}

// testLog writes the server's warnings and errors into the test's log, so
// a 500 shows its cause.
type testLog struct{ t *testing.T }

func (l testLog) Write(p []byte) (int, error) {
	l.t.Log(strings.TrimSpace(string(p)))
	return len(p), nil
}

// caller is a token, and optionally an agent header, presented on every
// request.
type caller struct {
	Token string
	Agent string
}

func (c caller) editor(_ context.Context, req *http.Request) error {
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if c.Agent != "" {
		req.Header.Set("X-Cowork-Agent", c.Agent)
	}
	return nil
}

// client returns the generated Go client acting as c (docs/adr/0046 D2).
func (s apiServer) client(t *testing.T, c caller) *apigen.ClientWithResponses {
	t.Helper()
	cl, err := apigen.NewClientWithResponses(s.URL, apigen.WithRequestEditorFn(c.editor))
	require.NoError(t, err)
	return cl
}

// do sends a raw request, for what the generated client cannot express: a
// malformed header, an unknown parameter, a wrong method.
func (s apiServer) do(t *testing.T, c caller, method, path string, body any, headers ...string) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		if raw, ok := body.(string); ok {
			reader = strings.NewReader(raw)
		} else {
			b, err := json.Marshal(body)
			require.NoError(t, err)
			reader = bytes.NewReader(b)
		}
	}
	req, err := http.NewRequest(method, s.URL+path, reader)
	require.NoError(t, err)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	require.NoError(t, c.editor(context.Background(), req))
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = res.Body.Close() })
	return res
}

// simultaneously starts every send at once and collects the status codes
// they return, for the races a conditional write loses: the request that
// comes second must answer as if it had come later, never 500.
func simultaneously(sends ...func() int) []int {
	codes := make([]int, len(sends))
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, send := range sends {
		wg.Go(func() {
			<-start
			codes[i] = send()
		})
	}
	close(start)
	wg.Wait()
	return codes
}

// times is send n times over, for simultaneously.
func times(n int, send func() int) []func() int {
	return slices.Repeat([]func() int{send}, n)
}

// problemBody decodes a problem details response and checks its envelope.
func problemBody(t *testing.T, res *http.Response) map[string]any {
	t.Helper()
	assert.Equal(t, "application/problem+json; charset=utf-8", res.Header.Get("Content-Type"))
	var body map[string]any
	require.NoError(t, json.NewDecoder(res.Body).Decode(&body))
	assert.EqualValues(t, res.StatusCode, body["status"])
	assert.Equal(t, res.Header.Get("X-Request-Id"), body["request_id"])
	return body
}

func assertProblem(t *testing.T, res *http.Response, status int, code string) map[string]any {
	t.Helper()
	require.Equal(t, status, res.StatusCode)
	body := problemBody(t, res)
	assert.Equal(t, code, body["code"])
	return body
}

// tokens is a world with a token per person: plain write tokens, an admin
// token for the administrator, and agent tokens.
type tokens struct {
	AdminA, AdminAWrite, MemberA, ViewerA, MemberB, Both string
	AgentA, AssistedAgentA                               string
}

func issueTokens(t *testing.T, w world) tokens {
	t.Helper()
	ctx := context.Background()
	f := fixtures(t)
	mint := func(spec fixture.TokenSpec) string {
		plaintext, _, err := f.Token(ctx, spec)
		require.NoError(t, err)
		return plaintext
	}
	return tokens{
		AdminA:         mint(fixture.TokenSpec{UserID: w.AdminA, Scope: domain.ScopeAdmin}),
		AdminAWrite:    mint(fixture.TokenSpec{UserID: w.AdminA, Scope: domain.ScopeWrite}),
		MemberA:        mint(fixture.TokenSpec{UserID: w.MemberA}),
		ViewerA:        mint(fixture.TokenSpec{UserID: w.ViewerA}),
		MemberB:        mint(fixture.TokenSpec{UserID: w.MemberB}),
		Both:           mint(fixture.TokenSpec{UserID: w.Both}),
		AgentA:         mint(fixture.TokenSpec{UserID: w.MemberA, Agent: true}),
		AssistedAgentA: mint(fixture.TokenSpec{UserID: w.MemberA, Agent: true, Capabilities: fixture.AssistedCapabilities}),
	}
}

// fixtureSpec is a plain write token of the person.
func fixtureSpec(person uuid.UUID) fixture.TokenSpec {
	return fixture.TokenSpec{UserID: person}
}

// fixtureAgent is an agent token of the person with every capability.
func fixtureAgent(person uuid.UUID) fixture.TokenSpec {
	return fixture.TokenSpec{UserID: person, Agent: true}
}

func newKey() *uuid.UUID {
	k := uuid.Must(uuid.NewV7())
	return &k
}

func ptr[T any](v T) *T { return &v }
