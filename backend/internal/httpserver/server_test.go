package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func do(t *testing.T, h http.Handler, method, target string) *http.Response {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec.Result()
}

func decode(t *testing.T, res *http.Response) map[string]any {
	t.Helper()
	defer res.Body.Close()
	var body map[string]any
	require.NoError(t, json.NewDecoder(res.Body).Decode(&body))
	return body
}

// problemOf decodes an RFC 9457 problem details body and checks its envelope:
// the media type, the standard members and that status matches the response.
func problemOf(t *testing.T, res *http.Response) map[string]any {
	t.Helper()
	assert.Equal(t, "application/problem+json; charset=utf-8", res.Header.Get("Content-Type"))
	body := decode(t, res)
	assert.EqualValues(t, res.StatusCode, body["status"])
	assert.NotEmpty(t, body["title"])
	typ, _ := body["type"].(string)
	code, _ := body["code"].(string)
	assert.Equal(t, "https://cowork.dev/problems/"+strings.ReplaceAll(code, "_", "-"), typ)
	assert.Equal(t, res.Header.Get("X-Request-Id"), body["request_id"], "the body names the request the header names")
	assert.NotEmpty(t, body["request_id"])
	return body
}

func TestHealthz(t *testing.T) {
	res := do(t, New(Options{}), http.MethodGet, "/healthz")
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, "application/json; charset=utf-8", res.Header.Get("Content-Type"))
	assert.Equal(t, "no-store", res.Header.Get("Cache-Control"))
	assert.Equal(t, map[string]any{"status": "ok"}, decode(t, res))
}

func TestHealthzAnswersHead(t *testing.T) {
	res := do(t, New(Options{}), http.MethodHead, "/healthz")
	assert.Equal(t, http.StatusOK, res.StatusCode)
}

func TestReadyzWithoutCheckIsReady(t *testing.T) {
	res := do(t, New(Options{}), http.MethodGet, "/readyz")
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, map[string]any{"status": "ready"}, decode(t, res))
}

// The ping error can name the database host and user: it goes to the log,
// never into the answer (docs/adr/0047 D3).
func TestReadyzReportsFailingCheck(t *testing.T) {
	h := New(Options{Ready: func(context.Context) error { return errors.New("dial tcp db.internal:5432: user cowork_app") }})
	res := do(t, h, http.MethodGet, "/readyz")
	assert.Equal(t, http.StatusServiceUnavailable, res.StatusCode)
	p := problemOf(t, res)
	assert.Equal(t, "not_ready", p["code"])
	assert.Equal(t, "the database does not answer", p["detail"])
	assert.Equal(t, "/readyz", p["instance"])
}

func TestAPIPathsReachTheAPIHandler(t *testing.T) {
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	res := do(t, New(Options{API: api}), http.MethodPost, "/api/v1/anything")
	assert.Equal(t, http.StatusTeapot, res.StatusCode)
}

func TestEveryResponseCarriesARequestID(t *testing.T) {
	res := do(t, New(Options{}), http.MethodGet, "/healthz")
	assert.Len(t, res.Header.Get("X-Request-Id"), 36)
}

func TestAPanicIsAnInternalProblem(t *testing.T) {
	api := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("secret internal state") })
	res := do(t, New(Options{API: api}), http.MethodGet, "/api/v1/boom")
	assert.Equal(t, http.StatusInternalServerError, res.StatusCode)
	p := problemOf(t, res)
	assert.Equal(t, "internal", p["code"])
	assert.NotContains(t, p["detail"], "secret")
}

// The backend serves no UI: every path it does not know is a problem+json
// 404, the API ones and the ones the frontend container would have resolved
// alike.
func TestUnknownRouteIsProblem404(t *testing.T) {
	h := New(Options{})
	for _, target := range []string{"/", "/index.html", "/api/v1/nothing", "/t/acme/board", "/api"} {
		t.Run(target, func(t *testing.T) {
			res := do(t, h, http.MethodGet, target)
			assert.Equal(t, http.StatusNotFound, res.StatusCode)
			p := problemOf(t, res)
			assert.Equal(t, "not_found", p["code"])
			assert.Contains(t, p["detail"], "GET "+target)
			assert.Equal(t, target, p["instance"])
		})
	}
}

// A known path with the wrong method is 405 with an Allow header, not the
// catch-all's 404: handleGet registers the path a second time without a method.
func TestKnownPathsRejectOtherMethods(t *testing.T) {
	h := New(Options{})
	for _, target := range []string{"/healthz", "/readyz"} {
		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
			t.Run(method+" "+target, func(t *testing.T) {
				res := do(t, h, method, target)
				assert.Equal(t, http.StatusMethodNotAllowed, res.StatusCode)
				assert.Equal(t, "GET, HEAD", res.Header.Get("Allow"))
				assert.Equal(t, "method_not_allowed", problemOf(t, res)["code"])
			})
		}
	}
}

func TestRequestLogRecordsStatus(t *testing.T) {
	var lines []map[string]any
	logger := newRecordingLogger(&lines)
	h := New(Options{Logger: logger})

	do(t, h, http.MethodGet, "/healthz")
	do(t, h, http.MethodGet, "/nope")

	require.Len(t, lines, 2)
	assert.Equal(t, "/healthz", lines[0]["path"])
	assert.EqualValues(t, 200, lines[0]["status"])
	assert.Equal(t, "/nope", lines[1]["path"])
	assert.EqualValues(t, 404, lines[1]["status"])
}

func TestServeShutsDownOnContextCancel(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, ln, New(Options{}), 5*time.Second) }()

	res, err := http.Get("http://" + ln.Addr().String() + "/healthz")
	require.NoError(t, err)
	res.Body.Close()
	assert.Equal(t, http.StatusOK, res.StatusCode)

	cancel()
	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("server did not stop after context cancel")
	}

	_, err = http.Get("http://" + ln.Addr().String() + "/healthz")
	assert.Error(t, err, "listener is closed after shutdown")
}

func TestListenAndServeReportsBindError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	err = ListenAndServe(context.Background(), ln.Addr().String(), New(Options{}), time.Second)
	assert.Error(t, err)
}
