// Package events fans the published acts out to the event streams of one
// replica (docs/adr/0054): a ring buffer per tenant for the replay, a filter
// per stream for what its person may see, bounded buffers so no stream holds
// up the others.
package events

import (
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"

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
	case store.EntityProject:
		return "project.changed"
	}
	return "ticket.changed"
}

// Filter is what one stream may see: the projects visible to its person,
// computed at connect and again at every heartbeat, already narrowed by a
// project-restricted token, and the confidential rule (docs/adr/0054 D3,
// docs/adr/0065 D5).
type Filter struct {
	Person   uuid.UUID
	Admin    bool
	Projects map[uuid.UUID]bool
	// RestrictedProject is the project a project-restricted token's stream is
	// bound to (docs/adr/0035 D3); uuid.Nil for every other stream.
	RestrictedProject uuid.UUID
	// Me marks a person-level stream (?me=true): it hears, besides its
	// tenant's events, the person's own across their tenants — their inbox
	// changing, and the acts of questions asked of them (docs/adr/0054 D1).
	// The hub hands those of another tenant over unfiltered; the stream judges
	// each against that tenant before it writes it.
	Me bool
}

// Admits reports whether the stream's person may see the event. A
// membership act reaches its audience whatever project it names
// (store.MembershipChange) — but a project-restricted token's stream only one
// that names its project, or names its own person and no other project (the
// security review of 2026-10-04, m10): the token knows nothing of the tenant
// beyond its project.
func (f Filter) Admits(e Event) bool {
	if e.Entity == store.EntityMembership {
		return f.admitsMembership(e)
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

// Stream is one subscriber. Its events arrive on C; Done closes when the hub
// ends the stream, and Reason says why: Resync for a stream that fell behind
// or must refetch, Unavailable for one closed by the limit or the shutdown.
type Stream struct {
	C      chan Event
	Done   chan struct{}
	Reason string

	tenant uuid.UUID
	filter Filter
	opened time.Time
	once   sync.Once
}

func (s *Stream) end(reason string) {
	s.once.Do(func() {
		s.Reason = reason
		close(s.Done)
	})
}

// Hub fans out one replica's notifications. The zero value is not usable;
// use New.
type Hub struct {
	window       time.Duration
	maxPerPerson int
	now          func() time.Time

	mu      sync.Mutex
	rings   map[uuid.UUID][]Event
	streams map[uuid.UUID][]*Stream
	down    bool
	closed  bool
}

// New returns a hub that keeps window of events per tenant for the replay
// and at most maxPerPerson streams per person, 0 for no limit
// (docs/adr/0054 D5, D8).
func New(window time.Duration, maxPerPerson int) *Hub {
	return &Hub{window: window, maxPerPerson: maxPerPerson, now: time.Now,
		rings: map[uuid.UUID][]Event{}, streams: map[uuid.UUID][]*Stream{}}
}

// Publish keeps a notification for the replay and hands it to every stream
// of its tenant that admits it; a stream whose buffer is full is told to
// resync and dropped, never waited for (docs/adr/0054 D4). A change of a
// person's inbox goes to that person's person-level streams alone and is kept
// for no replay — it says how things stand, and a stream that opens says it
// anew; a question's act also goes to the person-level streams of the person
// asked in their other tenants (Filter.Me).
func (h *Hub) Publish(n store.Notification) {
	e := Event{Notification: n, At: h.now()}
	h.mu.Lock()
	defer h.mu.Unlock()
	if n.Entity == store.EntityInbox {
		if n.Person != nil {
			h.toPerson(*n.Person, uuid.Nil, e)
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
		if s.filter.Admits(e) {
			send(s, e)
		}
	}
	if n.AskedOf != nil {
		h.toPerson(*n.AskedOf, n.Tenant, e)
	}
}

// toPerson hands an event to the person's person-level streams on every
// tenant but except; the hub's lock is held.
func (h *Hub) toPerson(person, except uuid.UUID, e Event) {
	for tenant, list := range h.streams {
		if tenant == except {
			continue
		}
		for _, s := range list {
			if s.filter.Me && s.filter.Person == person {
				send(s, e)
			}
		}
	}
}

// send hands an event to a stream, or ends one whose buffer is full.
func send(s *Stream, e Event) {
	select {
	case s.C <- e:
	default:
		s.end(Resync)
	}
}

// Subscribe opens a stream on a tenant. With lastEventID it returns the
// events after it that the filter admits, or resync when the id is no
// longer in the window (docs/adr/0054 D5). It returns nil while the hub
// cannot hear the database or has closed: the client polls.
func (h *Hub) Subscribe(tenant uuid.UUID, f Filter, lastEventID *uuid.UUID) (s *Stream, replay []Event, resync bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.down || h.closed {
		return nil, nil, false
	}
	if lastEventID != nil {
		ring := h.rings[tenant]
		i := slices.IndexFunc(ring, func(e Event) bool { return e.ID == *lastEventID })
		if i < 0 {
			resync = true
		} else {
			for _, e := range ring[i+1:] {
				if f.Admits(e) {
					replay = append(replay, e)
				}
			}
		}
	}
	h.limit(f.Person)
	s = &Stream{C: make(chan Event, streamBuffer), Done: make(chan struct{}), tenant: tenant, filter: f, opened: h.now()}
	h.streams[tenant] = append(h.streams[tenant], s)
	return s, replay, resync
}

// limit closes the person's oldest stream when a new one would exceed the
// limit (docs/adr/0054 D8); the hub's lock is held.
func (h *Hub) limit(person uuid.UUID) {
	if h.maxPerPerson <= 0 {
		return
	}
	var own []*Stream
	for _, list := range h.streams {
		for _, s := range list {
			if s.filter.Person == person {
				own = append(own, s)
			}
		}
	}
	slices.SortFunc(own, func(a, b *Stream) int { return a.opened.Compare(b.opened) })
	for len(own) >= h.maxPerPerson {
		h.remove(own[0])
		own[0].end(Unavailable)
		own = own[1:]
	}
}

// Refilter replaces what a stream admits: the heartbeat recomputes it, so a
// project the person gains reaches the stream and one they lose stops
// reaching it within one heartbeat (docs/adr/0054 D3, docs/adr/0035 D6).
func (h *Hub) Refilter(s *Stream, f Filter) {
	h.mu.Lock()
	defer h.mu.Unlock()
	s.filter = f
}

// Unsubscribe removes a stream that ended.
func (h *Hub) Unsubscribe(s *Stream) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.remove(s)
	s.end("")
}

func (h *Hub) remove(s *Stream) {
	h.streams[s.tenant] = slices.DeleteFunc(h.streams[s.tenant], func(o *Stream) bool { return o == s })
}

// SetUp tells the hub whether the database can be heard. Every open stream
// is told to resync when it can again, and the buffer starts afresh: the
// acts of the outage never reached it, so an earlier id is no replay point
// (docs/adr/0054 D4, D5). New streams are refused while it cannot.
func (h *Hub) SetUp(up bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if up && h.down {
		for _, list := range h.streams {
			for _, s := range list {
				s.end(Resync)
			}
		}
		h.streams = map[uuid.UUID][]*Stream{}
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
	for _, list := range h.streams {
		for _, s := range list {
			s.end(Unavailable)
		}
	}
	h.streams = map[uuid.UUID][]*Stream{}
}
