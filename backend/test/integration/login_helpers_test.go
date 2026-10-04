//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api"
	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/store"
	"github.com/guided-traffic/cowork/backend/test/fixture"
)

// testOrigin is the origin the browsers of these tests come from, and what the
// servers take as COWORK_BASE_URL (docs/adr/0037 D1).
const testOrigin = "https://cowork.test"

// testPassword is the password of every account the tests make; it is long
// enough for the default minimum of twelve characters.
const testPassword = "correct horse battery staple"

// withLogin turns on what a cookie login needs: the origin, the lockout, and
// the default password minimum. The per-address throttle stays off — every
// test of the run reaches the server from 127.0.0.1, and a limit shared by all
// of them would make the rest depend on how fast they run; the tests of the
// throttle switch it on, with a server key of their own so that their address
// is theirs.
func withLogin(o *api.Options) {
	o.BaseOrigin = testOrigin
	o.LoginMaxFailures = 5
	o.LoginAddressLimit = 0
	o.PasswordMinLength = 12
}

// clock is a clock a test moves, so a session's hours and a lock's minutes pass
// without waiting.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *clock { return &clock{t: time.Now()} }

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func withClock(c *clock) func(*api.Options) {
	return func(o *api.Options) { o.Now = c.Now }
}

// browser is a client that holds one session cookie, the way a browser tab
// does: it sends the Origin and X-Requested-With the frontend sends, unless a
// test takes them away to see the refusal.
type browser struct {
	t      *testing.T
	s      apiServer
	Cookie string
}

func (s apiServer) browser(t *testing.T) *browser { return &browser{t: t, s: s} }

type reqOpt func(*http.Request)

func withHeader(k, v string) reqOpt { return func(r *http.Request) { r.Header.Set(k, v) } }
func without(k string) reqOpt       { return func(r *http.Request) { r.Header.Del(k) } }
func withBearer(token string) reqOpt {
	return withHeader("Authorization", "Bearer "+token)
}

// request sends one request as the browser; unsafe methods carry the origin
// and the custom header.
func (b *browser) request(method, path string, body any, opts ...reqOpt) *http.Response {
	b.t.Helper()
	var reader io.Reader
	if body != nil {
		if raw, ok := body.(string); ok {
			reader = strings.NewReader(raw)
		} else {
			enc, err := json.Marshal(body)
			require.NoError(b.t, err)
			reader = bytes.NewReader(enc)
		}
	}
	req, err := http.NewRequest(method, b.s.URL+path, reader)
	require.NoError(b.t, err)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if method != http.MethodGet && method != http.MethodHead {
		req.Header.Set("Origin", testOrigin)
		req.Header.Set("X-Requested-With", "cowork")
	}
	if b.Cookie != "" {
		req.AddCookie(&http.Cookie{Name: auth.SessionCookie, Value: b.Cookie})
	}
	for _, o := range opts {
		o(req)
	}
	res, err := browserClient.Do(req)
	require.NoError(b.t, err)
	b.t.Cleanup(func() { _ = res.Body.Close() })
	return res
}

// browserClient does not follow redirects: a test looks at where the login
// through the identity provider sends the browser, and walks the issuer
// itself.
var browserClient = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

// login posts the credentials and, on 200, keeps the cookie it sets.
func (b *browser) login(username, password string, opts ...reqOpt) *http.Response {
	b.t.Helper()
	res := b.request(http.MethodPost, "/auth/local", map[string]string{"username": username, "password": password}, opts...)
	if res.StatusCode == http.StatusOK {
		b.Cookie = sessionCookieOf(b.t, res)
	}
	return res
}

// mustLogin logs in and requires the 200.
func (b *browser) mustLogin(username, password string) {
	b.t.Helper()
	res := b.login(username, password)
	require.Equal(b.t, http.StatusOK, res.StatusCode, "login of %s", username)
}

func (b *browser) get(path string, opts ...reqOpt) *http.Response {
	b.t.Helper()
	return b.request(http.MethodGet, path, nil, opts...)
}

// sessionCookieOf returns the value of the session cookie a response sets.
func sessionCookieOf(t *testing.T, res *http.Response) string {
	t.Helper()
	for _, c := range res.Cookies() {
		if c.Name == auth.SessionCookie {
			return c.Value
		}
	}
	require.Fail(t, "the response sets no session cookie")
	return ""
}

func decode[T any](t *testing.T, res *http.Response) T {
	t.Helper()
	var v T
	require.NoError(t, json.NewDecoder(res.Body).Decode(&v))
	return v
}

// unsafeBody is a problem body without what differs between two requests.
func unsafeBody(t *testing.T, res *http.Response) map[string]any {
	t.Helper()
	body := problemBody(t, res)
	delete(body, "request_id")
	return body
}

// usernameOf reads a person's username over the administrative connection.
func usernameOf(t *testing.T, person uuid.UUID) string {
	t.Helper()
	var username string
	require.NoError(t, fixtures(t).QueryRow(context.Background(), `SELECT username FROM users WHERE id = $1`, person).Scan(&username))
	return username
}

// withAccounts gives the persons of a world local accounts with testPassword,
// managed by tenant A, and returns their usernames.
func withAccounts(t *testing.T, w world) map[string]string {
	t.Helper()
	ctx := context.Background()
	f := fixtures(t)
	names := map[string]string{}
	for label, id := range map[string]uuid.UUID{"adminA": w.AdminA, "memberA": w.MemberA, "viewerA": w.ViewerA, "memberB": w.MemberB, "both": w.Both} {
		managing := w.A
		if label == "memberB" {
			managing = w.B
		}
		require.NoError(t, f.Account(ctx, id, testPassword, managing, false))
		names[label] = usernameOf(t, id)
	}
	return names
}

// scalar runs a counting or reading query over the administrative connection.
func scalar[T any](t *testing.T, sql string, args ...any) T {
	t.Helper()
	var v T
	require.NoError(t, fixtures(t).QueryRow(context.Background(), sql, args...).Scan(&v))
	return v
}

// isolated is a database of its own, migrated and empty, with the runtime
// role's store open on it: the init state of docs/adr/0032 D5 and the start-up
// synchronisation of D2 and D6 need an installation that has no tenant, which
// the shared database of the run no longer is after the first test.
type isolated struct {
	DB         *store.DB
	F          *fixture.DB
	RuntimeURL string
	OwnerURL   string
}

func newIsolated(t *testing.T) isolated {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	name := fmt.Sprintf("cowork_it_iso_%d", time.Now().UnixNano())
	require.NoError(t, createDatabase(ctx, env.AdminURL, name))
	t.Cleanup(func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), time.Minute)
		defer dropCancel()
		assert.NoError(t, dropDatabase(dropCtx, env.AdminURL, name))
	})
	adminURL, err := withUserAndDatabase(env.AdminURL, "", "", name)
	require.NoError(t, err)
	ownerURL, err := withUserAndDatabase(env.AdminURL, ownerRole, ownerRole, name)
	require.NoError(t, err)
	runtimeURL, err := withUserAndDatabase(env.AdminURL, runtimeRole, runtimeRole, name)
	require.NoError(t, err)
	_, err = store.Migrate(ctx, ownerURL, runtimeRole)
	require.NoError(t, err)
	f, err := fixture.Connect(ctx, adminURL)
	require.NoError(t, err)
	t.Cleanup(f.Close)
	return isolated{DB: openStore(t, runtimeURL), F: f, RuntimeURL: runtimeURL, OwnerURL: ownerURL}
}

func (i isolated) option(o *api.Options) { o.DB = i.DB }

// recordingLogger collects every record, at every level, as a line of text:
// what a test searches for a secret.
type recordingLogger struct {
	mu    sync.Mutex
	lines []string
}

func (l *recordingLogger) logger() *slog.Logger {
	return slog.New(slog.NewTextHandler(l, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func (l *recordingLogger) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, string(p))
	return len(p), nil
}

func (l *recordingLogger) text() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.lines, "\n")
}
