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

// one is the subscription of a stream that follows the tenant alone.
func one(tenant uuid.UUID, f Filter) Subscription {
	return Subscription{Tenant: tenant, Person: f.Person, Filters: map[uuid.UUID]Filter{tenant: f}}
}

// on is a filter of the tenant for the refilter.
func on(tenant uuid.UUID, f Filter) map[uuid.UUID]Filter {
	return map[uuid.UUID]Filter{tenant: f}
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

// docs/adr/0054 D2, docs/adr/0034 D3: a membership act reaches its audience —
// every member, the administrators, or the administrators and the person an
// access entry names — and never through the project filter, which a
// restricted project's own announcement would not pass.
func TestMembershipAudiences(t *testing.T) {
	f := newFixtures()
	member := f.filter()
	admin := f.filter()
	admin.Admin = true
	named := f.filter()
	named.Person = f.other
	event := func(audience string, person *uuid.UUID) Event {
		return Event{Notification: store.Notification{ID: uuid.Must(uuid.NewV7()), Tenant: f.tenant, Project: f.hidden,
			Entity: store.EntityMembership, Action: "updated", Person: person, Audience: audience}}
	}
	everyone := event(store.AudienceMembers, &f.other)
	assert.Equal(t, "membership.changed", everyone.Name())
	assert.True(t, member.Admits(everyone), "a project the stream does not see does not hold it back")
	mapping := event(store.AudienceAdmins, nil)
	assert.False(t, member.Admits(mapping))
	assert.True(t, admin.Admits(mapping))
	entry := event(store.AudienceAdminsAndPerson, &f.other)
	assert.False(t, member.Admits(entry), "a member who does not see the project hears nothing of its list")
	assert.True(t, admin.Admits(entry))
	assert.True(t, named.Admits(entry), "the person the entry names")
}

// The security review of 2026-10-04, m10: a project-restricted token's stream
// hears of a membership act only when it names the token's project, or names
// the token's person and no other project.
func TestMembershipEventsOfAProjectRestrictedStream(t *testing.T) {
	f := newFixtures()
	bound := f.filter()
	bound.Admin = true
	bound.RestrictedProject = f.project
	event := func(person *uuid.UUID, project uuid.UUID, audience string) Event {
		return Event{Notification: store.Notification{ID: uuid.Must(uuid.NewV7()), Tenant: f.tenant, Project: project,
			Entity: store.EntityMembership, Action: "updated", Person: person, Audience: audience}}
	}
	assert.True(t, bound.Admits(event(nil, f.project, store.AudienceMembers)), "its project's restriction")
	assert.True(t, bound.Admits(event(&f.other, f.project, store.AudienceAdminsAndPerson)), "an entry on its project")
	assert.True(t, bound.Admits(event(&f.person, uuid.Nil, store.AudienceMembers)), "its own person's membership")
	assert.False(t, bound.Admits(event(&f.other, uuid.Nil, store.AudienceMembers)), "another person's membership")
	assert.False(t, bound.Admits(event(nil, f.hidden, store.AudienceMembers)), "another project's restriction")
	assert.False(t, bound.Admits(event(&f.person, f.hidden, store.AudienceAdminsAndPerson)), "its person's entry on another project")
	assert.False(t, bound.Admits(event(nil, uuid.Nil, store.AudienceAdmins)), "a mapping")
	unbound := bound
	unbound.RestrictedProject = uuid.Nil
	assert.True(t, unbound.Admits(event(&f.other, uuid.Nil, store.AudienceMembers)), "an unrestricted stream hears every member's")
}

func TestPublishReplayAndWindow(t *testing.T) {
	f := newFixtures()
	h := New(time.Minute, 0)
	now := time.Unix(1000, 0)
	h.now = func() time.Time { return now }
	s, _, _ := h.Subscribe(one(f.tenant, f.filter()), nil)
	first := f.note(f.project)
	h.Publish(first)
	h.Publish(f.note(f.hidden))
	got := <-s.C
	assert.Equal(t, first.ID, got.ID)
	assert.Empty(t, s.C, "the hidden project's event never arrives")

	second := f.note(f.project)
	h.Publish(second)
	_, replay, resync := h.Subscribe(one(f.tenant, f.filter()), &first.ID)
	assert.False(t, resync)
	require.Len(t, replay, 1, "the replay applies the filter again")
	assert.Equal(t, second.ID, replay[0].ID)

	now = now.Add(2 * time.Minute)
	h.Publish(f.note(f.project))
	_, _, resync = h.Subscribe(one(f.tenant, f.filter()), &first.ID)
	assert.True(t, resync, "beyond the window: resync")
}

// docs/adr/0054 D1, D2: a change of a person's inbox reaches the person's
// person-level streams on any tenant, and no other stream, and is kept for no
// replay.
func TestPersonLevelEvents(t *testing.T) {
	f := newFixtures()
	h := New(time.Minute, 0)
	elsewhere := uuid.New()
	me := one(f.tenant, f.filter())
	me.Me = true
	plain, _, _ := h.Subscribe(one(f.tenant, f.filter()), nil)
	own, _, _ := h.Subscribe(me, nil)
	farSub := one(elsewhere, f.filter())
	farSub.Me = true
	far, _, _ := h.Subscribe(farSub, nil)
	otherFilter := f.filter()
	otherFilter.Person = f.other
	anotherSub := one(elsewhere, otherFilter)
	anotherSub.Me = true
	another, _, _ := h.Subscribe(anotherSub, nil)

	inbox := store.Notification{ID: uuid.Must(uuid.NewV7()), Tenant: f.tenant, Entity: store.EntityInbox, Person: &f.person}
	h.Publish(inbox)
	assert.Equal(t, "inbox.changed", (<-own.C).Name())
	assert.Equal(t, "inbox.changed", (<-far.C).Name(), "on another tenant's person-level stream too")
	assert.Empty(t, plain.C, "a stream without me hears no inbox")
	assert.Empty(t, another.C, "another person's inbox")
	_, replay, resync := h.Subscribe(me, &inbox.ID)
	assert.True(t, resync, "an inbox change is no replay point")
	assert.Empty(t, replay)
}

// docs/adr/0054 D1, D3, D5: a person-level stream that spans its person's
// tenants hears every tenant it follows through that tenant's filter, follows
// its person into a tenant a membership act names them in — pending its
// filter, every later event of it unjudged —, stops following one the person
// left unless an act came since, and replays across its tenants in the order
// the hub received the events.
func TestAStreamAcrossTheTenantsOfItsPerson(t *testing.T) {
	f := newFixtures()
	h := New(time.Minute, 0)
	b, c := uuid.New(), uuid.New()
	inB := uuid.New()
	note := func(tenant, project uuid.UUID) store.Notification {
		n := f.note(project)
		n.Tenant = tenant
		return n
	}
	span := Subscription{Tenant: f.tenant, Person: f.person, Me: true, Span: true, Filters: map[uuid.UUID]Filter{
		f.tenant: f.filter(),
		b:        {Person: f.person, Projects: map[uuid.UUID]bool{inB: true}},
	}}
	s, _, _ := h.Subscribe(span, nil)
	narrow, _, _ := h.Subscribe(one(f.tenant, f.filter()), nil)

	first := note(f.tenant, f.project)
	h.Publish(first)
	ofB := note(b, inB)
	h.Publish(ofB)
	h.Publish(note(b, uuid.New()))
	assert.Equal(t, first.ID, (<-s.C).ID)
	assert.Equal(t, ofB.ID, (<-s.C).ID, "another tenant of the person, through its filter")
	assert.Empty(t, s.C, "a project of B the person does not see")
	assert.Equal(t, first.ID, (<-narrow.C).ID)
	assert.Empty(t, narrow.C, "a stream of one tenant hears no other")

	// A grant into C names the person: the spanning stream follows C, pending.
	grant := store.Notification{ID: uuid.Must(uuid.NewV7()), Tenant: c, Entity: store.EntityMembership,
		Action: "created", Person: &f.person, Audience: store.AudienceMembers}
	h.Publish(grant)
	got := <-s.C
	assert.Equal(t, grant.ID, got.ID)
	assert.Equal(t, uint64(1), got.Refilter, "marked for the stream to compute C's filter")
	assert.False(t, got.Withheld, "the grant names the person")
	inC := uuid.New()
	filedInC := note(c, inC)
	h.Publish(filedInC)
	got = <-s.C
	assert.Equal(t, filedInC.ID, got.ID)
	assert.True(t, got.Unjudged, "C's events wait for the stream's judgement until it has C's filter")
	h.Refilter(s, on(c, Filter{Person: f.person, Projects: map[uuid.UUID]bool{inC: true}}), h.Changes(s))
	again := note(c, inC)
	h.Publish(again)
	got = <-s.C
	assert.Equal(t, again.ID, got.ID)
	assert.False(t, got.Unjudged)
	assert.Empty(t, narrow.C)

	// The person left B: the stream no longer follows it.
	h.Refilter(s, nil, map[uuid.UUID]uint64{b: h.Changes(s)[b]})
	later := note(b, inB)
	h.Publish(later)
	assert.Empty(t, s.C, "a tenant the person left")

	// The replay merges the tenants the stream follows, in the order received.
	resub := Subscription{Tenant: f.tenant, Person: f.person, Me: true, Span: true, Filters: map[uuid.UUID]Filter{
		f.tenant: f.filter(),
		b:        {Person: f.person, Projects: map[uuid.UUID]bool{inB: true}},
		c:        {Person: f.person, Projects: map[uuid.UUID]bool{inC: true}},
	}}
	_, replay, resync := h.Subscribe(resub, &first.ID)
	require.False(t, resync)
	ids := make([]uuid.UUID, 0, len(replay))
	for _, e := range replay {
		ids = append(ids, e.ID)
	}
	assert.Equal(t, []uuid.UUID{ofB.ID, grant.ID, filedInC.ID, again.ID, later.ID}, ids)
	_, _, resync = h.Subscribe(one(uuid.New(), f.filter()), &first.ID)
	assert.True(t, resync, "an id of no tenant the stream follows")
}

// docs/adr/0054 D4: a stream that falls behind is told to resync and
// dropped; the others go on.
func TestSlowStreamIsDropped(t *testing.T) {
	f := newFixtures()
	h := New(time.Minute, 0)
	slow, _, _ := h.Subscribe(one(f.tenant, f.filter()), nil)
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
	a, _, _ := h.Subscribe(one(f.tenant, f.filter()), nil)
	b, _, _ := h.Subscribe(one(f.tenant, f.filter()), nil)
	h.Subscribe(one(f.tenant, f.filter()), nil)
	<-a.Done
	assert.Equal(t, Unavailable, a.Reason)
	select {
	case <-b.Done:
		t.Fatal("only the oldest closes")
	default:
	}
	other := f.filter()
	other.Person = f.other
	_, _, _ = h.Subscribe(one(f.tenant, other), nil)
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
	s, _, _ := h.Subscribe(one(f.tenant, f.filter()), nil)
	h.SetUp(false)
	refused, _, _ := h.Subscribe(one(f.tenant, f.filter()), nil)
	assert.Nil(t, refused)
	h.SetUp(true)
	<-s.Done
	assert.Equal(t, Resync, s.Reason)

	open, _, _ := h.Subscribe(one(f.tenant, f.filter()), nil)
	h.Close()
	<-open.Done
	assert.Equal(t, Unavailable, open.Reason)
	closed, _, _ := h.Subscribe(one(f.tenant, f.filter()), nil)
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
	_, replay, resync := h.Subscribe(one(f.tenant, f.filter()), &before.ID)
	assert.True(t, resync, "an id from before the outage")
	assert.Empty(t, replay)
}

// docs/adr/0054 D3, docs/adr/0035 D6: a stream's filter follows what its
// person may see, recomputed at the heartbeat — a project gained is
// admitted, a project lost is not.
func TestRefilter(t *testing.T) {
	f := newFixtures()
	h := New(time.Minute, 0)
	s, _, _ := h.Subscribe(one(f.tenant, f.filter()), nil)
	h.Refilter(s, on(f.tenant, Filter{Person: f.person, Projects: map[uuid.UUID]bool{f.hidden: true}}), h.Changes(s))
	h.Publish(f.note(f.project))
	gained := f.note(f.hidden)
	h.Publish(gained)
	got := <-s.C
	assert.Equal(t, gained.ID, got.ID, "the project gained")
	assert.Empty(t, s.C, "the project lost")
}

// docs/adr/0054 D3: an act that changes what a stream may admit — a project
// created, a membership act — reaches every stream marked to refilter, and the
// hub hands the stream every later event unjudged until the stream has
// computed its filter after the act: no event is dropped by a filter that does
// not know it yet.
func TestAnAdmissionChangeHoldsTheFilterUntilTheStreamRefilters(t *testing.T) {
	f := newFixtures()
	h := New(time.Minute, 0)
	s, _, _ := h.Subscribe(one(f.tenant, f.filter()), nil)
	fresh := uuid.New()
	first := f.note(f.project)
	h.Publish(first)
	<-s.C

	created := store.Notification{ID: uuid.Must(uuid.NewV7()), Tenant: f.tenant, Project: fresh,
		Entity: store.EntityProject, Action: "created"}
	h.Publish(created)
	got := <-s.C
	assert.Equal(t, uint64(1), got.Refilter, "the creation marks the stream to refilter")
	assert.True(t, got.Withheld, "and is no event a client is told of")
	assert.False(t, f.filter().Admits(Event{Notification: created}))

	filed := f.note(fresh)
	h.Publish(filed)
	got = <-s.C
	assert.Equal(t, filed.ID, got.ID, "an event of the new project waits for the stream's own judgement")
	assert.True(t, got.Unjudged)
	assert.Zero(t, got.Refilter)

	// The stream reads how many changes there were before it computes its filter;
	// a change after that leaves it unjudged still.
	seen := h.Changes(s)
	require.Equal(t, uint64(1), seen[f.tenant])
	h.Publish(store.Notification{ID: uuid.Must(uuid.NewV7()), Tenant: f.tenant, Entity: store.EntityMembership,
		Action: "created", Person: &f.other, Audience: store.AudienceMembers})
	got = <-s.C
	assert.Equal(t, uint64(2), got.Refilter)
	assert.True(t, got.Unjudged, "a membership act while the filter is behind")
	h.Refilter(s, on(f.tenant, Filter{Person: f.person, Projects: map[uuid.UUID]bool{f.project: true, fresh: true}}), seen)
	h.Publish(f.note(f.hidden))
	got = <-s.C
	assert.True(t, got.Unjudged, "the filter knows one of two changes: still unjudged")

	h.Refilter(s, on(f.tenant, Filter{Person: f.person, Projects: map[uuid.UUID]bool{f.project: true, fresh: true}}), h.Changes(s))
	h.Publish(f.note(f.hidden))
	assert.Empty(t, s.C, "a filter that knows every change judges again")
	again := f.note(fresh)
	h.Publish(again)
	got = <-s.C
	assert.Equal(t, again.ID, got.ID)
	assert.False(t, got.Unjudged)

	_, replay, _ := h.Subscribe(one(f.tenant, Filter{Person: f.person, Projects: map[uuid.UUID]bool{fresh: true}}), &first.ID)
	require.NotEmpty(t, replay)
	assert.Equal(t, filed.ID, replay[0].ID, "a replay holds no creation of a project")
	for _, e := range replay {
		assert.NotEqual(t, store.EntityProject, e.Entity)
	}
}
