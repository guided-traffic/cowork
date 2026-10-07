// Package events fans the published acts out to the event streams of one
// replica (docs/adr/0054): a ring buffer per tenant for the replay, a filter
// per stream and tenant for what its person may see — a person-level stream
// follows every tenant of its person —, bounded buffers so no stream holds up
// the others.
package events

import (
	"cmp"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/guided-traffic/cowork/backend/internal/metrics"
	"github.com/guided-traffic/cowork/backend/internal/store"
)

// The control messages a stream ends or restarts with (docs/adr/0054 D4,
// D5, D8).
const (
	// Resync tells the client to refetch every list it shows.
	Resync = "resync"
	// Unavailable tells the client to poll and retry the stream later.
	Unavailable = "unavailable"
)

// streamBuffer is what a stream may fall behind by before it is dropped.
const streamBuffer = 256

// Event is a published act as a stream sends it: the name and the key and
// version, never content (docs/adr/0054 D2).
type Event struct {
	store.Notification
	At time.Time
	// Seq is the order the hub received the event in, across its tenants: a
	// stream that follows several merges their rings by it for a replay (D5).
	Seq uint64

	// What the hub tells the stream it hands the event to, never sent on.
	// Refilter is set on an act that changes what a stream may admit
	// (ChangesAdmission): the stream computes its filter again — after at
	// least this many such acts — before it handles the next event. Unjudged
	// is an event the hub handed on while the stream's filter was out of date,
	// which the stream judges with the filter it computed since; Withheld one
	// the hub's filter refused, handed on for its Refilter alone.
	Refilter           uint64
	Unjudged, Withheld bool
}

// ChangesAdmission reports whether the act can change which events a stream
// of its tenant may admit: a project created, and every membership act — a
// grant, a derived membership, a mapping, a project's restriction, an entry
// of its access list (docs/adr/0054 D3).
func (e Event) ChangesAdmission() bool {
	return e.Entity == store.EntityMembership || e.Entity == store.EntityProject
}

// Name is the event's type on the stream.
func (e Event) Name() string {
	switch e.Entity {
	case "comment":
		return "comment.changed"
	case "question":
		return "question.changed"
	case "link":
		return "link.changed"
	case "interest":
		return "interest.changed"
	case store.EntityMembership:
		return "membership.changed"
	case store.EntityInbox:
		return "inbox.changed"
	case store.EntityProjectRank:
		return "project.changed"
	}
	return "ticket.changed"
}

// Filter is what one stream may see of one tenant: the projects visible to
// its person there, computed at connect, again on every act that changes what
// the stream may admit of the tenant (Event.Refilter) and at every heartbeat,
// already narrowed by a project-restricted token, and the confidential rule
// (docs/adr/0054 D3, docs/adr/0065 D5).
type Filter struct {
	Person   uuid.UUID
	Admin    bool
	Projects map[uuid.UUID]bool
	// RestrictedProject is the project a project-restricted token's stream is
	// bound to (docs/adr/0035 D3); uuid.Nil for every other stream.
	RestrictedProject uuid.UUID
}

// Admits reports whether the stream's person may see the event. A
// membership act reaches its audience whatever project it names
// (store.MembershipChange) — but a project-restricted token's stream only one
// that names its project, or names its own person and no other project (the
// security review of 2026-10-04, m10): the token knows nothing of the tenant
// beyond its project.
func (f Filter) Admits(e Event) bool {
	switch e.Entity {
	case store.EntityMembership:
		return f.admitsMembership(e)
	case store.EntityProject:
		// A project's creation changes what the stream admits; it is no event
		// a client is told of.
		return false
	}
	if !f.Projects[e.Project] {
		return false
	}
	if !e.Confidential || f.Admin || e.Reporter == f.Person {
		return true
	}
	return e.Assignee != nil && *e.Assignee == f.Person
}

func (f Filter) admitsMembership(e Event) bool {
	names := e.Person != nil && *e.Person == f.Person
	if f.RestrictedProject != uuid.Nil && e.Project != f.RestrictedProject && (e.Project != uuid.Nil || !names) {
		return false
	}
	switch e.Audience {
	case store.AudienceAdmins:
		return f.Admin
	case store.AudienceAdminsAndPerson:
		return f.Admin || names
	}
	return true
}

// Subscription is what a stream follows: the tenant it is opened on, its
// person, and the filter of every tenant whose events it hears — the tenant it
// is opened on alone, or for a person-level stream that spans its person's
// tenants every one of them (docs/adr/0054 D1). Me marks a person-level
// stream, which hears its person's inbox changing; Span one that follows its
// person into a tenant they join, which a token restricted to a tenant never
// does (docs/adr/0035 D3).
type Subscription struct {
	Tenant   uuid.UUID
	Person   uuid.UUID
	Me, Span bool
	Filters  map[uuid.UUID]Filter
}

// Stream is one subscriber. Its events arrive on C; Done closes when the hub
// ends the stream, and Reason says why: Resync for a stream that fell behind
// or must refetch, Unavailable for one closed by the limit or the shutdown.
type Stream struct {
	C      chan Event
	Done   chan struct{}
	Reason string

	tenant   uuid.UUID
	person   uuid.UUID
	me, span bool
	// filters holds what the stream admits of each tenant it follows. changes
	// counts, per tenant, the acts that changed what the stream may admit of
	// it since the stream opened — kept when the stream stops following the
	// tenant, so that a later act counts on — and refiltered the ones the
	// tenant's filter was computed after; while a tenant's filter is behind,
	// the hub hands its events on unjudged.
	filters             map[uuid.UUID]Filter
	changes, refiltered map[uuid.UUID]uint64
	opened              time.Time
	once                sync.Once
}

// end ends the stream with the reason, once; ended says this call ended it.
func (s *Stream) end(reason string) (ended bool) {
	s.once.Do(func() {
		s.Reason = reason
		close(s.Done)
		ended = true
	})
	return ended
}

// Hub fans out one replica's notifications. The zero value is not usable;
// use New.
type Hub struct {
	window       time.Duration
	maxPerPerson int
	now          func() time.Time
	metrics      *metrics.Metrics

	mu    sync.Mutex
	seq   uint64
	rings map[uuid.UUID][]Event
	// streams holds the streams that follow a tenant, persons every stream
	// of a person; open counts the streams persons holds.
	streams map[uuid.UUID][]*Stream
	persons map[uuid.UUID][]*Stream
	open    int
	down    bool
	closed  bool
}

// New returns a hub that keeps window of events per tenant for the replay
// and at most maxPerPerson streams per person, 0 for no limit
// (docs/adr/0054 D5, D8), and records its streams, the notifications it
// receives, the streams it drops and the replays in m (docs/adr/0060 D4); a
// nil m records nothing.
func New(window time.Duration, maxPerPerson int, m *metrics.Metrics) *Hub {
	return &Hub{window: window, maxPerPerson: maxPerPerson, now: time.Now, metrics: m,
		rings: map[uuid.UUID][]Event{}, streams: map[uuid.UUID][]*Stream{}, persons: map[uuid.UUID][]*Stream{}}
}

// Publish keeps a notification for the replay and hands it to every stream
// that follows its tenant and admits it; a stream whose buffer is full is told
// to resync and dropped, never waited for (docs/adr/0054 D4). An act that
// changes what a stream may admit reaches every stream that follows its
// tenant, marked to refilter, and until the stream has computed the tenant's
// filter after it the hub hands on every later event of that tenant unjudged,
// so that none is dropped — or let through — by a filter that does not know
// the act yet (D3). Such an act that names a person also reaches that
// person's spanning streams that do not follow its tenant yet — a grant into a
// tenant they had none in —, which then follow it pending their filter. A
// change of a person's inbox goes to that person's person-level streams alone
// and is kept for no replay: it says how things stand, and a stream that opens
// says it anew.
func (h *Hub) Publish(n store.Notification) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.metrics.EventPublished()
	h.seq++
	e := Event{Notification: n, At: h.now(), Seq: h.seq}
	if n.Entity == store.EntityInbox {
		if n.Person != nil {
			for _, s := range h.persons[*n.Person] {
				if s.me {
					h.send(s, e)
				}
			}
		}
		return
	}
	ring := append(h.rings[n.Tenant], e)
	cut := 0
	for cut < len(ring) && e.At.Sub(ring[cut].At) > h.window {
		cut++
	}
	h.rings[n.Tenant] = ring[cut:]
	for _, s := range h.streams[n.Tenant] {
		h.deliver(s, e)
	}
	if e.ChangesAdmission() && n.Person != nil {
		for _, s := range h.persons[*n.Person] {
			if s.span && !s.follows(n.Tenant) {
				s.filters[n.Tenant] = Filter{Person: s.person, Projects: map[uuid.UUID]bool{}}
				h.streams[n.Tenant] = append(h.streams[n.Tenant], s)
				h.deliver(s, e)
			}
		}
	}
}

// deliver hands an event of a tenant the stream follows to it: judged by the
// tenant's filter, or unjudged while that filter is behind, and marked when it
// changes what the stream may admit.
func (h *Hub) deliver(s *Stream, e Event) {
	out := e
	switch {
	case s.refiltered[e.Tenant] < s.changes[e.Tenant]:
		out.Unjudged = true
	case !s.filters[e.Tenant].Admits(e):
		out.Withheld = true
	}
	if e.ChangesAdmission() {
		s.changes[e.Tenant]++
		out.Refilter = s.changes[e.Tenant]
	}
	if !out.Withheld || out.Refilter > 0 {
		h.send(s, out)
	}
}

func (s *Stream) follows(tenant uuid.UUID) bool {
	_, ok := s.filters[tenant]
	return ok
}

// send hands an event to a stream, or ends one whose buffer is full: a
// subscriber dropped for falling behind (docs/adr/0054 D4).
func (h *Hub) send(s *Stream, e Event) {
	select {
	case s.C <- e:
	default:
		if s.end(Resync) {
			h.metrics.SubscriberDropped(metrics.DropBehind)
		}
	}
}

// Subscribe opens a stream. With lastEventID it returns the events after it
// that the filters admit, merged across the tenants the stream follows in the
// order the hub received them, or resync when the id is in none of their
// rings — an id of another replica, or one older than the window
// (docs/adr/0054 D5). It returns nil while the hub cannot hear the database or
// has closed: the client polls.
func (h *Hub) Subscribe(sub Subscription, lastEventID *uuid.UUID) (s *Stream, replay []Event, resync bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.down || h.closed {
		return nil, nil, false
	}
	if lastEventID != nil {
		replay, resync = h.replay(sub, *lastEventID)
		h.metrics.Replay(!resync)
	}
	h.limit(sub.Person)
	s = &Stream{C: make(chan Event, streamBuffer), Done: make(chan struct{}), tenant: sub.Tenant, person: sub.Person,
		me: sub.Me, span: sub.Span, filters: map[uuid.UUID]Filter{}, changes: map[uuid.UUID]uint64{},
		refiltered: map[uuid.UUID]uint64{}, opened: h.now()}
	for tenant, f := range sub.Filters {
		s.filters[tenant] = f
		h.streams[tenant] = append(h.streams[tenant], s)
	}
	h.persons[sub.Person] = append(h.persons[sub.Person], s)
	h.open++
	h.metrics.OpenStreams(h.open)
	return s, replay, resync
}

// replay finds the id in the rings of the subscription's tenants and returns
// the events every ring received after it, by the hub's order; the hub's lock
// is held.
func (h *Hub) replay(sub Subscription, last uuid.UUID) ([]Event, bool) {
	var after uint64
	found := false
	for tenant := range sub.Filters {
		if i := slices.IndexFunc(h.rings[tenant], func(e Event) bool { return e.ID == last }); i >= 0 {
			after, found = h.rings[tenant][i].Seq, true
			break
		}
	}
	if !found {
		return nil, true
	}
	var replay []Event
	for tenant, f := range sub.Filters {
		for _, e := range h.rings[tenant] {
			if e.Seq > after && f.Admits(e) {
				replay = append(replay, e)
			}
		}
	}
	slices.SortFunc(replay, func(a, b Event) int { return cmp.Compare(a.Seq, b.Seq) })
	return replay, false
}

// limit closes the person's oldest stream when a new one would exceed the
// limit (docs/adr/0054 D8); the hub's lock is held.
func (h *Hub) limit(person uuid.UUID) {
	if h.maxPerPerson <= 0 {
		return
	}
	own := slices.Clone(h.persons[person])
	slices.SortFunc(own, func(a, b *Stream) int { return a.opened.Compare(b.opened) })
	for len(own) >= h.maxPerPerson {
		h.remove(own[0])
		if own[0].end(Unavailable) {
			h.metrics.SubscriberDropped(metrics.DropLimit)
		}
		own = own[1:]
	}
}

// Changes is, per tenant, how many acts have changed what the stream may
// admit of it so far: a filter of the tenant computed after this call knows
// all of them.
func (h *Hub) Changes(s *Stream) map[uuid.UUID]uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return maps.Clone(s.changes)
}

// Refilter replaces what the stream admits of the tenants in filters with
// filters computed after Changes answered seen, and follows a tenant it did
// not; a tenant in seen and not in filters — its person left it — it no longer
// follows, unless an act changed what it may admit of that tenant since: then
// the tenant's events stay unjudged until the stream refilters again. The
// stream recomputes on every act that changes what it may admit and at every
// heartbeat (docs/adr/0054 D3, docs/adr/0035 D6).
func (h *Hub) Refilter(s *Stream, filters map[uuid.UUID]Filter, seen map[uuid.UUID]uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for tenant, f := range filters {
		if !s.follows(tenant) {
			h.streams[tenant] = append(h.streams[tenant], s)
		}
		s.filters[tenant] = f
		s.refiltered[tenant] = max(s.refiltered[tenant], seen[tenant])
	}
	for tenant, n := range seen {
		if _, kept := filters[tenant]; kept || !s.follows(tenant) || s.changes[tenant] != n {
			continue
		}
		delete(s.filters, tenant)
		s.refiltered[tenant] = n
		h.streams[tenant] = slices.DeleteFunc(h.streams[tenant], func(o *Stream) bool { return o == s })
	}
}

// Unsubscribe removes a stream that ended.
func (h *Hub) Unsubscribe(s *Stream) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.remove(s)
	s.end("")
}

func (h *Hub) remove(s *Stream) {
	for tenant := range s.filters {
		h.streams[tenant] = slices.DeleteFunc(h.streams[tenant], func(o *Stream) bool { return o == s })
	}
	held := len(h.persons[s.person])
	h.persons[s.person] = slices.DeleteFunc(h.persons[s.person], func(o *Stream) bool { return o == s })
	if len(h.persons[s.person]) < held {
		h.open--
		h.metrics.OpenStreams(h.open)
	}
}

// SetUp tells the hub whether the database can be heard. Every open stream
// is told to resync when it can again, and the buffer starts afresh: the
// acts of the outage never reached it, so an earlier id is no replay point
// (docs/adr/0054 D4, D5). New streams are refused while it cannot.
func (h *Hub) SetUp(up bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if up && h.down {
		for range h.endAll(Resync) {
			h.metrics.SubscriberDropped(metrics.DropResync)
		}
		h.rings = map[uuid.UUID][]Event{}
	}
	h.down = !up
}

// Close ends every stream and refuses new ones: the shutdown closes streams
// before it drains the requests (docs/adr/0054 D9).
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	h.endAll(Unavailable)
}

// endAll ends every stream with the reason and returns how many it ended;
// the hub's lock is held.
func (h *Hub) endAll(reason string) (ended int) {
	for _, list := range h.persons {
		for _, s := range list {
			if s.end(reason) {
				ended++
			}
		}
	}
	h.streams = map[uuid.UUID][]*Stream{}
	h.persons = map[uuid.UUID][]*Stream{}
	h.open = 0
	h.metrics.OpenStreams(0)
	return ended
}
