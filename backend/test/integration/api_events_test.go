//go:build integration

package integration

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api"
	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/events"
	"github.com/guided-traffic/cowork/backend/internal/store"
)

// sse is one message of a stream; a heartbeat comment has only Comment.
type sse struct {
	ID, Event, Data, Comment string
}

// stream is an open event stream and what it delivers.
type stream struct {
	Messages chan sse
	Ended    chan struct{}
	cancel   context.CancelFunc
}

func (s *stream) Close() { s.cancel() }

// openStream subscribes c to the tenant's events.
func (e ticketEnv) openStream(t *testing.T, srv apiServer, c caller, slug, lastEventID string) *stream {
	t.Helper()
	return e.openStreamAt(t, srv, c, "/api/v1/tenants/"+slug+"/events", lastEventID)
}

// openStreamAt subscribes c to the stream at path, with its query.
func (e ticketEnv) openStreamAt(t *testing.T, srv apiServer, c caller, path, lastEventID string) *stream {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+path, nil)
	require.NoError(t, err)
	require.NoError(t, c.editor(ctx, req))
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}
	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, "text/event-stream", res.Header.Get("Content-Type"))
	assert.Equal(t, "no", res.Header.Get("X-Accel-Buffering"))
	assert.Equal(t, "no-cache", res.Header.Get("Cache-Control"))
	s := &stream{Messages: make(chan sse, 100), Ended: make(chan struct{}), cancel: cancel}
	go func() {
		defer close(s.Ended)
		defer func() { _ = res.Body.Close() }()
		sc := bufio.NewScanner(res.Body)
		var m sse
		for sc.Scan() {
			line := sc.Text()
			switch {
			case line == "":
				s.Messages <- m
				m = sse{}
			case strings.HasPrefix(line, ":"):
				m.Comment = strings.TrimSpace(line[1:])
			default:
				k, v, _ := strings.Cut(line, ": ")
				switch k {
				case "id":
					m.ID = v
				case "event":
					m.Event = v
				case "data":
					m.Data = v
				}
			}
		}
	}()
	t.Cleanup(s.Close)
	return s
}

// next waits for the next event that is not a heartbeat.
func (s *stream) next(t *testing.T, within time.Duration) (sse, bool) {
	t.Helper()
	deadline := time.After(within)
	for {
		select {
		case m := <-s.Messages:
			if m.Comment != "" && m.Event == "" {
				continue
			}
			return m, true
		case <-s.Ended:
			// The reader queues every message before it ends: drain first.
			for {
				select {
				case m := <-s.Messages:
					if m.Comment != "" && m.Event == "" {
						continue
					}
					return m, true
				default:
					return sse{}, false
				}
			}
		case <-deadline:
			return sse{}, false
		}
	}
}

func eventKey(t *testing.T, m sse) string {
	t.Helper()
	var d struct {
		Key     string `json:"key"`
		Version int    `json:"version"`
		Kind    string `json:"kind"`
	}
	require.NoError(t, json.Unmarshal([]byte(m.Data), &d))
	return d.Key
}

// docs/adr/0054 D2–D4: a committed act reaches a subscriber at once with its
// key and version, a rolled-back one never; nothing crosses a tenant, a
// restriction or the confidential rule.
func TestEventStream(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	member := caller{Token: e.tk.MemberA}
	s := e.openStream(t, e.s, member, e.SlugA, "")
	viewer := e.openStream(t, e.s, caller{Token: e.tk.ViewerA}, e.SlugA, "")
	other := e.openStream(t, e.s, caller{Token: e.tk.MemberB}, e.SlugB, "")

	start := time.Now()
	tk := e.file(t, member, "ALPHA", task("Streamed"))
	m, ok := s.next(t, time.Second)
	require.True(t, ok, "a committed act arrives within a second")
	assert.Less(t, time.Since(start), time.Second)
	assert.Equal(t, "ticket.changed", m.Event)
	assert.Equal(t, tk.Key, eventKey(t, m))
	assert.JSONEq(t, `{"key":"`+tk.Key+`","version":1,"kind":"created"}`, m.Data)
	assert.NotEmpty(t, m.ID)
	_, ok = viewer.next(t, time.Second)
	assert.True(t, ok, "a viewer sees the project")
	_, ok = other.next(t, 300*time.Millisecond)
	assert.False(t, ok, "tenant B hears nothing of A")

	db := openRuntime(t)
	_, err := db.Mutate(as(e.MemberA), e.A, func(w *store.Writer) error {
		w.Record(store.Event{EntityType: "ticket", EntityID: tk.Id, TicketID: tk.Id, TicketKey: tk.Key, Action: "updated"})
		return errors.New("roll back")
	})
	require.Error(t, err)
	_, ok = s.next(t, 300*time.Millisecond)
	assert.False(t, ok, "a rolled-back act is never published")

	e.comment(t, member, tk, "a comment")
	m, ok = s.next(t, time.Second)
	require.True(t, ok)
	assert.Equal(t, "comment.changed", m.Event)
	viewer.next(t, time.Second)

	secret := e.file(t, member, "ALPHA", task("Secret", func(b *apigen.TicketCreate) {
		b.Security, b.Threat = apigen.SecurityClassLive, ptr("leak")
	}))
	m, ok = s.next(t, time.Second)
	require.True(t, ok, "the reporter hears the confidential ticket")
	assert.Equal(t, secret.Key, eventKey(t, m))
	_, ok = viewer.next(t, 300*time.Millisecond)
	assert.False(t, ok, "the confidential silence")

	hidden, err := f.Project(e.ctx, e.A, "HIDDEN", "Hidden")
	require.NoError(t, err)
	require.NoError(t, f.Exec(e.ctx, "UPDATE projects SET restricted = true WHERE id = $1", hidden))
	off := e.openStream(t, e.s, member, e.SlugA, "")
	e.file(t, caller{Token: e.tk.AdminA}, "HIDDEN", task("Behind the restriction"))
	_, ok = off.next(t, 300*time.Millisecond)
	assert.False(t, ok, "a person off a restricted project hears nothing of it")
}

// docs/adr/0054 D3, docs/adr/0035 D6: the heartbeat recomputes what a
// stream admits — a project restricted away from its person stops reaching
// it, a project created after it connected starts reaching it.
func TestStreamFollowsAccess(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	srv := newAPI(t, func(o *api.Options) { o.Heartbeat = 100 * time.Millisecond })
	s := e.openStream(t, srv, caller{Token: e.tk.MemberA}, e.SlugA, "")
	admin := caller{Token: e.tk.AdminA}

	_, err := f.Project(e.ctx, e.A, "FRESH", "Fresh")
	require.NoError(t, err)
	require.NoError(t, f.Exec(e.ctx, "UPDATE projects SET restricted = true WHERE id = $1", e.ProjectA))
	for beats := 0; beats < 2; {
		select {
		case m := <-s.Messages:
			if m.Comment == "heartbeat" {
				beats++
			}
		case <-time.After(2 * time.Second):
			t.Fatal("no heartbeat")
		}
	}
	e.file(t, admin, "ALPHA", task("Restricted away"))
	fresh := e.file(t, admin, "FRESH", task("Created after the connect"))
	m, ok := s.next(t, time.Second)
	require.True(t, ok, "the project created after the connect reaches the stream")
	assert.Equal(t, fresh.Key, eventKey(t, m), "and the project restricted away does not")
	_, ok = s.next(t, 300*time.Millisecond)
	assert.False(t, ok)
}

// docs/adr/0054 D3: a stream recomputes what it admits on the act that
// changes it, before it filters the next event — a ticket filed in a project
// created after the stream opened, in a project opened for everyone, or in one
// whose access list took the person in, arrives within a second, and the
// stream of a person whose grant is removed ends; the heartbeat, an hour here,
// plays no part.
func TestTheStreamAdmitsWhatAnActOpensAtOnce(t *testing.T) {
	e := newTicketEnv(t)
	names := withAccounts(t, e.world)
	srv := newAPI(t, withLogin, func(o *api.Options) { o.Heartbeat = time.Hour })
	admin := srv.browser(t)
	admin.mustLogin(names["adminA"], testPassword)
	adminToken := caller{Token: e.tk.AdminA}
	s := e.openStream(t, srv, caller{Token: e.tk.MemberA}, e.SlugA, "")
	tenant := "/api/v1/tenants/" + e.SlugA

	// A project created after the stream opened: its creation is no event a client hears.
	res := srv.do(t, adminToken, http.MethodPost, tenant+"/projects", map[string]any{"key": "LATE", "name": "Late"})
	require.Equal(t, http.StatusCreated, res.StatusCode)
	start := time.Now()
	filed := e.file(t, adminToken, "LATE", task("Filed in a new project"))
	m, ok := s.next(t, time.Second)
	require.True(t, ok, "the ticket of a project created after the stream opened arrives within a second")
	assert.Less(t, time.Since(start), time.Second)
	assert.Equal(t, "ticket.changed", m.Event)
	assert.Equal(t, filed.Key, eventKey(t, m))

	// A restricted project opened for every member.
	for _, key := range []string{"SHUT", "LISTED"} {
		id, err := fixtures(t).Project(e.ctx, e.A, key, key)
		require.NoError(t, err)
		require.NoError(t, fixtures(t).Exec(e.ctx, "UPDATE projects SET restricted = true WHERE id = $1", id))
	}
	restricted := e.openStream(t, srv, caller{Token: e.tk.MemberA}, e.SlugA, "")
	project := tenant + "/projects/SHUT"
	res = admin.request(http.MethodPut, project+"/restriction", map[string]bool{"restricted": false},
		withHeader("If-Match", admin.get(project).Header.Get("ETag")))
	require.Equal(t, http.StatusOK, res.StatusCode)
	opened := e.file(t, adminToken, "SHUT", task("Filed in an opened project"))
	m, ok = restricted.next(t, time.Second)
	require.True(t, ok)
	assert.Equal(t, "membership.changed", m.Event, "the restriction lifted, heard by every member")
	m, ok = restricted.next(t, time.Second)
	require.True(t, ok, "the ticket of the opened project arrives within a second")
	assert.Equal(t, opened.Key, eventKey(t, m))

	// A restricted project whose access list takes the person in.
	res = admin.request(http.MethodPut, tenant+"/projects/LISTED/access/"+e.MemberA.String(), map[string]string{"role": "viewer"})
	require.Equal(t, http.StatusOK, res.StatusCode)
	listed := e.file(t, adminToken, "LISTED", task("Filed in a project the person was let into"))
	m, ok = restricted.next(t, time.Second)
	require.True(t, ok)
	assert.Equal(t, "membership.changed", m.Event, "the entry, heard by the person it names")
	m, ok = restricted.next(t, time.Second)
	require.True(t, ok, "the ticket of the project the person was let into arrives within a second")
	assert.Equal(t, listed.Key, eventKey(t, m))

	// The first stream heard all of it as well, and nothing of a project it does not see.
	for range 4 {
		_, ok = s.next(t, time.Second)
		require.True(t, ok)
	}
	hidden, err := fixtures(t).Project(e.ctx, e.A, "HIDDEN", "Hidden")
	require.NoError(t, err)
	require.NoError(t, fixtures(t).Exec(e.ctx, "UPDATE projects SET restricted = true WHERE id = $1", hidden))
	e.file(t, adminToken, "HIDDEN", task("Behind the restriction"))
	_, ok = s.next(t, 300*time.Millisecond)
	assert.False(t, ok, "a recomputed filter still holds the restriction")

	// A grant removed takes the tenant away: the stream ends before its next event.
	res = srv.do(t, adminToken, http.MethodDelete, tenant+"/members/"+e.MemberA.String()+"/grant", nil)
	require.Equal(t, http.StatusNoContent, res.StatusCode)
	select {
	case <-s.Ended:
	case <-time.After(time.Second):
		t.Fatal("the stream of a person who left the tenant goes on")
	}
	e.file(t, adminToken, "LATE", task("After the person left"))
	for {
		m, ok := s.next(t, 100*time.Millisecond)
		if !ok {
			break
		}
		assert.NotEqual(t, "ticket.changed", m.Event, "nothing after the grant is gone")
	}
}

// docs/adr/0054 D5: a reconnect inside the window replays the gap, one
// beyond it starts with resync.
func TestEventReplay(t *testing.T) {
	e := newTicketEnv(t)
	member := caller{Token: e.tk.MemberA}
	s := e.openStream(t, e.s, member, e.SlugA, "")
	e.file(t, member, "ALPHA", task("First"))
	first, ok := s.next(t, time.Second)
	require.True(t, ok)
	s.Close()
	second := e.file(t, member, "ALPHA", task("Second"))

	again := e.openStream(t, e.s, member, e.SlugA, first.ID)
	m, ok := again.next(t, time.Second)
	require.True(t, ok)
	assert.Equal(t, second.Key, eventKey(t, m), "the gap is replayed")

	lost := e.openStream(t, e.s, member, e.SlugA, "01900000-0000-7000-8000-000000000000")
	m, ok = lost.next(t, time.Second)
	require.True(t, ok)
	assert.Equal(t, events.Resync, m.Event)
}

// docs/adr/0054 D5, D8, D9, docs/adr/0035 D6: the heartbeat, the limit per
// person, a revoked token's stream closing, the shutdown ending every stream.
func TestEventStreamLifecycle(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	srv := newAPI(t, func(o *api.Options) {
		o.Events = events.New(time.Minute, 2)
		o.Heartbeat = 100 * time.Millisecond
	})
	member := caller{Token: e.tk.MemberA}
	oldest := e.openStream(t, srv, member, e.SlugA, "")
	beat := false
	for !beat {
		select {
		case m := <-oldest.Messages:
			beat = m.Comment == "heartbeat"
		case <-time.After(2 * time.Second):
			t.Fatal("no heartbeat")
		}
	}
	e.openStream(t, srv, member, e.SlugA, "")
	e.openStream(t, srv, member, e.SlugA, "")
	m, ok := oldest.next(t, time.Second)
	require.True(t, ok)
	assert.Equal(t, events.Unavailable, m.Event, "the third stream closes the oldest")

	plaintext, tokenID, err := f.Token(e.ctx, fixtureSpec(e.Both))
	require.NoError(t, err)
	revoked := e.openStream(t, srv, caller{Token: plaintext}, e.SlugA, "")
	require.NoError(t, f.Exec(e.ctx, "UPDATE tokens SET revoked_at = now() WHERE id = $1", tokenID))
	select {
	case <-revoked.Ended:
	case <-time.After(2 * time.Second):
		t.Fatal("a revoked token's stream stays open")
	}

	last := e.openStream(t, srv, caller{Token: e.tk.AdminA}, e.SlugA, "")
	srv.Hub.Close()
	m, ok = last.next(t, time.Second)
	require.True(t, ok)
	assert.Equal(t, events.Unavailable, m.Event, "the shutdown ends every stream")
}
