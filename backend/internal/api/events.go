package api

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/events"
	"github.com/guided-traffic/cowork/backend/internal/problem"
	"github.com/guided-traffic/cowork/backend/internal/requestid"
	"github.com/guided-traffic/cowork/backend/internal/store"
	"github.com/guided-traffic/cowork/backend/internal/store/readq"
)

// opStreamEvents is the event stream's operation, served outside the
// generated server.
const opStreamEvents = "streamEvents"

// defaultHeartbeat keeps proxies from closing an idle stream
// (docs/adr/0054 D5).
const defaultHeartbeat = 20 * time.Second

// inboxDebounce gathers a burst of changes of the person's inbox — an agent
// commenting in a loop, every notification marked read at once — into one
// count.
const inboxDebounce = 100 * time.Millisecond

// streamReq is what a stream holds of its request: the tenant it is opened on
// and the caller, whether it is the person-level stream (?me=true), and
// whether that spans every tenant of the person — not for a token restricted
// to a tenant, which reaches no other (docs/adr/0054 D1, docs/adr/0035 D3).
type streamReq struct {
	t        tenantScope
	p        auth.Principal
	me, span bool
}

// serveEvents streams the events the caller may see of the tenant, and for a
// person-level stream of every tenant of the person (docs/adr/0054). The
// pipeline has authenticated the caller and admitted them to the tenant
// before the first byte.
func (h *handler) serveEvents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	t := tenantFrom(ctx)
	p := principal(ctx)
	if perr := auth.Authorize(p, t.Role, read); perr != nil {
		problem.Write(w, r, perr)
		return
	}
	flusher, ok := w.(http.Flusher)
	if h.opts.Events == nil || !ok {
		problem.Write(w, r, problem.New(problem.NotReady, "this server streams no events; poll the lists"))
		return
	}
	me := r.URL.Query().Get("me") == "true"
	sr := streamReq{t: t, p: p, me: me, span: me && !restricted(p)}
	st := pumpState{refiltered: map[uuid.UUID]uint64{}}
	var err error
	if st.filters, st.slugs, err = h.streamFilters(ctx, sr); err != nil {
		h.writeError(w, r, err)
		return
	}
	unread := -1
	if me {
		if unread, err = h.personUnread(ctx); err != nil {
			h.writeError(w, r, err)
			return
		}
	}
	var last *uuid.UUID
	if v := r.Header.Get("Last-Event-ID"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			last = &id
		}
	}
	stream, replay, resync := h.opts.Events.Subscribe(events.Subscription{Tenant: t.ID, Person: p.PersonID,
		Me: me, Span: sr.span, Filters: maps.Clone(st.filters)}, last)
	if stream == nil {
		problem.Write(w, r, problem.New(problem.NotReady, "the event stream is unavailable; poll the lists"))
		return
	}
	defer h.opts.Events.Unsubscribe(stream)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if resync {
		writeControl(w, events.Resync)
	}
	for _, e := range replay {
		writeEvent(w, e, st.slugs[e.Tenant])
	}
	if me {
		writeInbox(w, unread)
	}
	flusher.Flush()
	h.pump(ctx, w, flusher, stream, sr, &st)
}

// personUnread is the unread count a person-level stream tells: the person's
// in every tenant the request reaches (GET /api/v1/me/inbox).
func (h *handler) personUnread(ctx context.Context) (int, error) {
	tenants, err := h.personTenants(ctx, nil)
	if err != nil {
		return 0, err
	}
	return h.unread(ctx, tenants)
}

// pumpState is what a stream's pump holds between two events: the filter of
// every tenant it follows as it computed it last, the slugs it writes, how
// many of the stream's admission changes each tenant's filter knows, and the
// wait of a burst of inbox changes.
type pumpState struct {
	filters    map[uuid.UUID]events.Filter
	slugs      map[uuid.UUID]string
	refiltered map[uuid.UUID]uint64
	inbox      debounce
}

// pump writes the stream's events until it ends: the client leaves, the hub
// ends it, or the heartbeat finds the token or the membership gone. An act
// that changes what the stream may admit of a tenant makes it compute that
// tenant's filter again before the tenant's next event, and every heartbeat
// computes every tenant's (docs/adr/0054 D3). A person-level stream tells the
// unread count once a burst of inbox changes is over.
func (h *handler) pump(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, stream *events.Stream, sr streamReq, st *pumpState) {
	beat := h.opts.Heartbeat
	if beat <= 0 {
		beat = defaultHeartbeat
	}
	ticker := time.NewTicker(beat)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case e := <-stream.C:
			if !h.handOn(ctx, w, stream, st, e, sr) {
				return
			}
		case <-st.inbox.due:
			st.inbox.due = nil
			unread, err := h.personUnread(ctx)
			if err != nil {
				h.streamFailed(ctx, err)
				return
			}
			writeInbox(w, unread)
		case <-stream.Done:
			if stream.Reason != "" {
				writeControl(w, stream.Reason)
				flusher.Flush()
			}
			return
		case <-ticker.C:
			if !h.heartbeat(ctx, w, stream, st, sr) {
				return
			}
		}
		flusher.Flush()
	}
}

// heartbeat checks what a new request would (stillAdmitted), computes the
// filter of every tenant the stream follows again — for a spanning stream the
// person's tenants as their memberships stand now: one they left it no longer
// follows, one they joined it does — and writes the comment. It is false when
// the stream must end.
func (h *handler) heartbeat(ctx context.Context, w http.ResponseWriter, stream *events.Stream, st *pumpState, sr streamReq) bool {
	now, ok := h.stillAdmitted(ctx, sr.t, sr.p)
	if !ok {
		return false
	}
	seen := h.opts.Events.Changes(stream)
	sr.t = now
	filters, slugs, err := h.streamFilters(ctx, sr)
	if err != nil {
		h.streamFailed(ctx, err)
		return false
	}
	h.opts.Events.Refilter(stream, filters, seen)
	st.filters = filters
	maps.Copy(st.slugs, slugs)
	for tenant, n := range seen {
		st.refiltered[tenant] = max(st.refiltered[tenant], n)
	}
	_, _ = fmt.Fprint(w, ": heartbeat\n\n")
	return true
}

// handOn handles one event the hub handed over: it computes the filter of the
// event's tenant again first when the event changed what the stream may admit
// of it, starts the wait of an inbox burst, drops what the hub withheld, what
// the filter refuses of an event the hub could not judge and what belongs to a
// tenant the stream no longer follows, and writes the rest. The one act of a
// tenant left that is written is the membership act that names the person and
// that the stream followed the tenant until — the act that took the tenant
// away, which is theirs; a later act that names them there, the removal of an
// access entry they left behind, is not. It is false when the stream must end.
func (h *handler) handOn(ctx context.Context, w http.ResponseWriter, stream *events.Stream, st *pumpState, e events.Event, sr streamReq) bool {
	_, was := st.filters[e.Tenant]
	if e.Refilter > st.refiltered[e.Tenant] && !h.refilter(ctx, stream, st, sr, e.Tenant) {
		return false
	}
	if e.Entity == store.EntityInbox {
		st.inbox.start()
		return true
	}
	f, followed := st.filters[e.Tenant]
	takenAway := was && e.Entity == store.EntityMembership && e.Person != nil && *e.Person == sr.p.PersonID
	switch {
	case e.Withheld:
	case e.Unjudged && (!followed || !f.Admits(e)):
	case !followed && !takenAway:
	default:
		writeEvent(w, e, st.slugs[e.Tenant])
	}
	return true
}

// refilter computes what the stream admits of one tenant again after an act
// that changes it: for the tenant the stream is opened on, the person's role
// as the boundary reads it now and the projects they see — a person the
// boundary no longer admits ends the stream, as the heartbeat would; for
// another tenant of a spanning stream, the person's membership there and the
// projects they see, the stream no longer following a tenant the person left.
// It is false when the stream must end.
func (h *handler) refilter(ctx context.Context, stream *events.Stream, st *pumpState, sr streamReq, tenant uuid.UUID) bool {
	seen := h.opts.Events.Changes(stream)
	n := map[uuid.UUID]uint64{tenant: seen[tenant]}
	var f events.Filter
	var err error
	if tenant == sr.t.ID {
		now, perr := h.boundary(ctx, sr.t.Slug, "/api/v1/tenants/{tenant}/events", opStreamEvents)
		if perr != nil {
			return false
		}
		f, err = h.streamFilter(ctx, now, sr.p)
	} else {
		var pt *personTenant
		if pt, err = h.personTenant(ctx, tenant); err == nil && (pt == nil || !sr.span) {
			h.opts.Events.Refilter(stream, nil, n)
			delete(st.filters, tenant)
			st.refiltered[tenant] = seen[tenant]
			return true
		}
		if err == nil {
			st.slugs[tenant] = pt.slug
			f, err = h.tenantFilter(ctx, *pt, sr.p)
		}
	}
	if err != nil {
		h.streamFailed(ctx, err)
		return false
	}
	h.opts.Events.Refilter(stream, map[uuid.UUID]events.Filter{tenant: f}, n)
	st.filters[tenant], st.refiltered[tenant] = f, seen[tenant]
	return true
}

// personTenant is the person's membership in the tenant, nil where they hold
// none — or the request reaches it not (personTenants).
func (h *handler) personTenant(ctx context.Context, tenant uuid.UUID) (*personTenant, error) {
	tenants, err := h.personTenants(ctx, nil)
	if err != nil {
		return nil, err
	}
	if i := slices.IndexFunc(tenants, func(t personTenant) bool { return t.id == tenant }); i >= 0 {
		return &tenants[i], nil
	}
	return nil, nil
}

// debounce is the wait after the first inbox change of a burst
// (inboxDebounce); due is nil while none is waiting.
type debounce struct{ due <-chan time.Time }

func (d *debounce) start() {
	if d.due == nil {
		d.due = time.After(inboxDebounce)
	}
}

// streamFailed logs why a stream ended on a failure of its own; the client
// reconnects.
func (h *handler) streamFailed(ctx context.Context, err error) {
	if ctx.Err() == nil {
		h.logger.Error("event stream ended", "request_id", requestid.From(ctx), "error", err)
	}
}

// streamFilters computes what the stream admits of every tenant it follows,
// and their slugs: the tenant it is opened on (streamFilter), and for a
// spanning stream every other tenant of the person (tenantFilter).
func (h *handler) streamFilters(ctx context.Context, sr streamReq) (map[uuid.UUID]events.Filter, map[uuid.UUID]string, error) {
	own, err := h.streamFilter(ctx, sr.t, sr.p)
	if err != nil {
		return nil, nil, err
	}
	filters := map[uuid.UUID]events.Filter{sr.t.ID: own}
	slugs := map[uuid.UUID]string{sr.t.ID: sr.t.Slug}
	if !sr.span {
		return filters, slugs, nil
	}
	tenants, err := h.personTenants(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	for _, pt := range tenants {
		if pt.id == sr.t.ID {
			continue
		}
		if filters[pt.id], err = h.tenantFilter(ctx, pt, sr.p); err != nil {
			return nil, nil, err
		}
		slugs[pt.id] = pt.slug
	}
	return filters, slugs, nil
}

// streamFilter computes what the stream admits of the tenant it is opened on:
// the projects visible to the caller now, which a project-restricted token
// narrows to its own, and the confidential rule (docs/adr/0054 D3,
// docs/adr/0065 D5).
func (h *handler) streamFilter(ctx context.Context, t tenantScope, p auth.Principal) (events.Filter, error) {
	f := events.Filter{Person: p.PersonID, Admin: t.Role == domain.RoleAdmin, Projects: map[uuid.UUID]bool{},
		RestrictedProject: p.RestrictedProjectID}
	return f, h.visibleProjects(ctx, t.ID, f.Projects)
}

// tenantFilter computes what a spanning stream admits of another tenant of
// its person: their role there by their membership, and the projects they see
// in it, read in that tenant's transaction as a request of theirs would.
func (h *handler) tenantFilter(ctx context.Context, pt personTenant, p auth.Principal) (events.Filter, error) {
	f := events.Filter{Person: p.PersonID, Admin: pt.role == domain.RoleAdmin, Projects: map[uuid.UUID]bool{}}
	return f, h.visibleProjects(ctx, pt.id, f.Projects)
}

// visibleProjects marks the projects of the tenant the caller sees.
func (h *handler) visibleProjects(ctx context.Context, tenant uuid.UUID, into map[uuid.UUID]bool) error {
	return h.opts.DB.InTenant(ctx, tenant, func(r *store.Reader) error {
		ids, err := r.ListVisibleProjectIDs(ctx, tenant)
		for _, id := range ids {
			into[id] = true
		}
		return err
	})
}

// stillAdmitted checks at every heartbeat what a new request would: the
// token is usable — or the session is, neither limit passed — the identity
// provider still admits the person, and the person still belongs to the tenant
// (docs/adr/0035 D6, D8, docs/adr/0031 D3, D4, docs/adr/0030 D5). An open
// stream does not extend the session's idle time: a forgotten tab must log
// out. It returns the tenant as the person holds it now, their role included.
func (h *handler) stillAdmitted(ctx context.Context, t tenantScope, p auth.Principal) (tenantScope, bool) {
	usable := false
	err := h.opts.DB.Installation(ctx, func(r *store.Reader) error {
		var err error
		if p.Session {
			now := h.opts.Now()
			usable, err = r.SessionStillUsable(ctx, readq.SessionStillUsableParams{
				TokenHash: p.SessionHash, UserID: p.PersonID, Now: now, IdleBefore: now.Add(-h.opts.SessionIdle)})
			return err
		}
		usable, err = r.TokenStillUsable(ctx, readq.TokenStillUsableParams{TokenID: p.TokenID, UserID: p.PersonID})
		return err
	})
	if err != nil || !usable || !h.streamStillAdmitted(ctx, p) {
		return tenantScope{}, false
	}
	now, perr := h.boundary(ctx, t.Slug, "/api/v1/tenants/{tenant}/events", opStreamEvents)
	return now, perr == nil
}

// eventData is what an event tells: a key and a version, never content
// (docs/adr/0054 D2).
type eventData struct {
	Key string `json:"key"`
	// Version is the ticket's; a project's act carries none (project.changed).
	Version int32  `json:"version,omitempty"`
	Kind    string `json:"kind"`
}

// membershipData is what membership.changed tells: the tenant it happened in
// — a person-level stream carries every tenant of its person — and the keys of
// what changed, each where it applies (the API document, the event stream).
type membershipData struct {
	Tenant    string     `json:"tenant"`
	PersonID  *uuid.UUID `json:"person_id,omitempty"`
	ProjectID *uuid.UUID `json:"project_id,omitempty"`
	MappingID *uuid.UUID `json:"mapping_id,omitempty"`
}

// writeEvent writes an event with its id; slug is its tenant's.
func writeEvent(w http.ResponseWriter, e events.Event, slug string) {
	// #nosec G705 -- text/event-stream of a uuid, a fixed event name and JSON the server encodes; no HTML
	_, _ = fmt.Fprintf(w, "id: %s\nevent: %s\ndata: %s\n\n", e.ID, e.Name(), dataOf(e, slug))
}

func dataOf(e events.Event, slug string) []byte {
	if e.Entity == store.EntityMembership {
		m := membershipData{Tenant: slug, PersonID: e.Person, MappingID: e.Mapping}
		if e.Project != uuid.Nil {
			m.ProjectID = &e.Project
		}
		data, _ := json.Marshal(m)
		return data
	}
	data, _ := json.Marshal(eventData{Key: e.Key, Version: e.Version, Kind: e.Action})
	return data
}

// writeInbox tells a person-level stream how many of the person's
// notifications are unread (docs/adr/0054 D2): a state, not an act, so it
// carries no id.
func writeInbox(w http.ResponseWriter, unread int) {
	_, _ = fmt.Fprintf(w, "event: inbox.changed\ndata: {\"unread\":%d}\n\n", unread)
}

func writeControl(w http.ResponseWriter, name string) {
	// #nosec G705 -- text/event-stream of a fixed control name; no HTML
	_, _ = fmt.Fprintf(w, "event: %s\ndata: {}\n\n", name)
}
