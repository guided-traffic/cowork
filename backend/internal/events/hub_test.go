package events

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/store"
)

type fixtures struct {
	tenant, project, hidden, person, other uuid.UUID
}

func newFixtures() fixtures {
	return fixtures{tenant: uuid.New(), project: uuid.New(), hidden: uuid.New(), person: uuid.New(), other: uuid.New()}
}

func (f fixtures) note(project uuid.UUID) store.Notification {
	return store.Notification{ID: uuid.Must(uuid.NewV7()), Tenant: f.tenant, Project: project, Entity: "ticket",
		Action: "updated", Key: "acme/A-1", Version: 2, Reporter: f.other}
}

func (f fixtures) filter() Filter {
	return Filter{Person: f.person, Projects: map[uuid.UUID]bool{f.project: true}}
}

// docs/adr/0054 D3, docs/adr/0065 D5: the project and the confidential rule.
func TestFilter(t *testing.T) {
	f := newFixtures()
	flt := f.filter()
	e := Event{Notification: f.note(f.project)}
	assert.True(t, flt.Admits(e))
	assert.False(t, flt.Admits(Event{Notification: f.note(f.hidden)}), "a project the person cannot see")
	e.Confidential = true
	assert.False(t, flt.Admits(e), "confidential: neither admin, assignee nor reporter")
	e.Assignee = &f.person
	assert.True(t, flt.Admits(e), "the assignee")
	e.Assignee, e.Reporter = nil, f.person
	assert.True(t, flt.Admits(e), "the reporter")
	e.Reporter = f.other
	flt.Admin = true
	assert.True(t, flt.Admits(e), "an administrator")
}

func TestPublishReplayAndWindow(t *testing.T) {
	f := newFixtures()
	h := New(time.Minute, 0)
	now := time.Unix(1000, 0)
	h.now = func() time.Time { return now }
	s, _, _ := h.Subscribe(f.tenant, f.filter(), nil)
	first := f.note(f.project)
	h.Publish(first)
	h.Publish(f.note(f.hidden))
	got := <-s.C
	assert.Equal(t, first.ID, got.ID)
	assert.Empty(t, s.C, "the hidden project's event never arrives")

	second := f.note(f.project)
	h.Publish(second)
	_, replay, resync := h.Subscribe(f.tenant, f.filter(), &first.ID)
	assert.False(t, resync)
	require.Len(t, replay, 1, "the replay applies the filter again")
	assert.Equal(t, second.ID, replay[0].ID)

	now = now.Add(2 * time.Minute)
	h.Publish(f.note(f.project))
	_, _, resync = h.Subscribe(f.tenant, f.filter(), &first.ID)
	assert.True(t, resync, "beyond the window: resync")
}

// docs/adr/0054 D4: a stream that falls behind is told to resync and
// dropped; the others go on.
func TestSlowStreamIsDropped(t *testing.T) {
	f := newFixtures()
	h := New(time.Minute, 0)
	slow, _, _ := h.Subscribe(f.tenant, f.filter(), nil)
	for range streamBuffer + 1 {
		h.Publish(f.note(f.project))
	}
	<-slow.Done
	assert.Equal(t, Resync, slow.Reason)
}

// docs/adr/0054 D8: the eleventh stream closes the oldest.
func TestStreamLimit(t *testing.T) {
	f := newFixtures()
	h := New(time.Minute, 2)
	clock := time.Unix(0, 0)
	h.now = func() time.Time { clock = clock.Add(time.Second); return clock }
	a, _, _ := h.Subscribe(f.tenant, f.filter(), nil)
	b, _, _ := h.Subscribe(f.tenant, f.filter(), nil)
	h.Subscribe(f.tenant, f.filter(), nil)
	<-a.Done
	assert.Equal(t, Unavailable, a.Reason)
	select {
	case <-b.Done:
		t.Fatal("only the oldest closes")
	default:
	}
	other := f.filter()
	other.Person = f.other
	_, _, _ = h.Subscribe(f.tenant, other, nil)
	select {
	case <-b.Done:
		t.Fatal("another person's stream counts for them")
	default:
	}
}

// docs/adr/0054 D4, D9: refused while the database cannot be heard, resync
// when it can again; the shutdown ends every stream.
func TestUpDownAndClose(t *testing.T) {
	f := newFixtures()
	h := New(time.Minute, 0)
	s, _, _ := h.Subscribe(f.tenant, f.filter(), nil)
	h.SetUp(false)
	refused, _, _ := h.Subscribe(f.tenant, f.filter(), nil)
	assert.Nil(t, refused)
	h.SetUp(true)
	<-s.Done
	assert.Equal(t, Resync, s.Reason)

	open, _, _ := h.Subscribe(f.tenant, f.filter(), nil)
	h.Close()
	<-open.Done
	assert.Equal(t, Unavailable, open.Reason)
	closed, _, _ := h.Subscribe(f.tenant, f.filter(), nil)
	assert.Nil(t, closed)
}

// docs/adr/0054 D4, D5: what a replica published before it lost the
// database is no replay point once it hears it again — the acts of the
// outage never reached the buffer, so a client reconnecting with an earlier
// id is told to resync instead of being replayed a buffer with a gap.
func TestRecoveryDropsTheBuffer(t *testing.T) {
	f := newFixtures()
	h := New(time.Minute, 0)
	before := f.note(f.project)
	h.Publish(before)
	h.SetUp(false)
	h.SetUp(true)
	h.Publish(f.note(f.project))
	_, replay, resync := h.Subscribe(f.tenant, f.filter(), &before.ID)
	assert.True(t, resync, "an id from before the outage")
	assert.Empty(t, replay)
}

// docs/adr/0054 D3, docs/adr/0035 D6: a stream's filter follows what its
// person may see, recomputed at the heartbeat — a project gained is
// admitted, a project lost is not.
func TestRefilter(t *testing.T) {
	f := newFixtures()
	h := New(time.Minute, 0)
	s, _, _ := h.Subscribe(f.tenant, f.filter(), nil)
	h.Refilter(s, Filter{Person: f.person, Projects: map[uuid.UUID]bool{f.hidden: true}})
	h.Publish(f.note(f.project))
	gained := f.note(f.hidden)
	h.Publish(gained)
	got := <-s.C
	assert.Equal(t, gained.ID, got.ID, "the project gained")
	assert.Empty(t, s.C, "the project lost")
}
