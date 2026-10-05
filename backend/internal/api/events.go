package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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

// serveEvents streams the tenant's events the caller may see
// (docs/adr/0054). The pipeline has authenticated the caller and admitted
// them to the tenant before the first byte.
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
	filter, err := h.streamFilter(ctx, t, p, me)
	if err != nil {
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
	stream, replay, resync := h.opts.Events.Subscribe(t.ID, filter, last)
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
		writeEvent(w, e)
	}
	if me {
		writeInbox(w, unread)
	}
	flusher.Flush()
	h.pump(ctx, w, flusher, stream, t, p, me)
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

// pump writes the stream's events until it ends: the client leaves, the hub
// ends it, or the heartbeat finds the token or the membership gone. Each
// heartbeat also recomputes what the stream admits. A person-level stream
// tells the unread count once a burst of inbox changes is over.
func (h *handler) pump(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, stream *events.Stream, t tenantScope, p auth.Principal, me bool) {
	beat := h.opts.Heartbeat
	if beat <= 0 {
		beat = defaultHeartbeat
	}
	ticker := time.NewTicker(beat)
	defer ticker.Stop()
	var inbox debounce
	for {
		select {
		case <-ctx.Done():
			return
		case e := <-stream.C:
			if e.Entity == store.EntityInbox {
				inbox.start()
			} else if !h.writeStreamed(ctx, w, e, t, p) {
				return
			}
		case <-inbox.due:
			inbox.due = nil
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
			now, ok := h.stillAdmitted(ctx, t, p)
			if !ok {
				return
			}
			filter, err := h.streamFilter(ctx, now, p, me)
			if err != nil {
				return
			}
			h.opts.Events.Refilter(stream, filter)
			_, _ = fmt.Fprint(w, ": heartbeat\n\n")
		}
		flusher.Flush()
	}
}

// debounce is the wait after the first inbox change of a burst
// (inboxDebounce); due is nil while none is waiting.
type debounce struct{ due <-chan time.Time }

func (d *debounce) start() {
	if d.due == nil {
		d.due = time.After(inboxDebounce)
	}
}

// writeStreamed writes an event the hub handed over: one of the stream's
// tenant as it is, and a person-level one of another tenant — a question asked
// of the person — only while the person belongs to that tenant and sees the
// ticket, and a token restricted to a tenant never (docs/adr/0054 D1, D3,
// docs/adr/0035 D3). That one carries no id: the stream's replay point stays
// its tenant's (D5). It is false when the judgement failed.
func (h *handler) writeStreamed(ctx context.Context, w http.ResponseWriter, e events.Event, t tenantScope, p auth.Principal) bool {
	if e.Tenant == t.ID {
		writeEvent(w, e)
		return true
	}
	if restricted(p) && e.Tenant != p.RestrictedTenantID {
		return true
	}
	var visible bool
	err := h.opts.DB.InTenant(ctx, e.Tenant, func(r *store.Reader) error {
		var err error
		visible, err = r.SeesPublishedTicket(ctx, readq.SeesPublishedTicketParams{TenantID: e.Tenant, ProjectID: e.Project,
			Confidential: e.Confidential, AssigneeID: e.Assignee, ReporterID: e.Reporter})
		return err
	})
	if err != nil {
		h.streamFailed(ctx, err)
		return false
	}
	if visible {
		writeAddressed(w, e)
	}
	return true
}

// streamFailed logs why a stream ended on a failure of its own; the client
// reconnects.
func (h *handler) streamFailed(ctx context.Context, err error) {
	if ctx.Err() == nil {
		h.logger.Error("event stream ended", "request_id", requestid.From(ctx), "error", err)
	}
}

// streamFilter computes what the stream admits: the projects visible to the
// caller now, which a project-restricted token narrows to its own, and the
// confidential rule (docs/adr/0054 D3, docs/adr/0065 D5); me marks a
// person-level stream.
func (h *handler) streamFilter(ctx context.Context, t tenantScope, p auth.Principal, me bool) (events.Filter, error) {
	f := events.Filter{Person: p.PersonID, Admin: t.Role == domain.RoleAdmin, Projects: map[uuid.UUID]bool{},
		RestrictedProject: p.RestrictedProjectID, Me: me}
	err := h.opts.DB.InTenant(ctx, t.ID, func(r *store.Reader) error {
		ids, err := r.ListVisibleProjectIDs(ctx, t.ID)
		for _, id := range ids {
			f.Projects[id] = true
		}
		return err
	})
	return f, err
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

// membershipData is what membership.changed tells: the keys of what changed,
// each where it applies (the API document, the event stream).
type membershipData struct {
	PersonID  *uuid.UUID `json:"person_id,omitempty"`
	ProjectID *uuid.UUID `json:"project_id,omitempty"`
	MappingID *uuid.UUID `json:"mapping_id,omitempty"`
}

func writeEvent(w http.ResponseWriter, e events.Event) {
	// #nosec G705 -- text/event-stream of a uuid, a fixed event name and JSON the server encodes; no HTML
	_, _ = fmt.Fprintf(w, "id: %s\nevent: %s\ndata: %s\n\n", e.ID, e.Name(), dataOf(e))
}

// writeAddressed writes a person-level event of another tenant than the
// stream's, without an id (writeStreamed).
func writeAddressed(w http.ResponseWriter, e events.Event) {
	// #nosec G705 -- text/event-stream of a fixed event name and JSON the server encodes; no HTML
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Name(), dataOf(e))
}

func dataOf(e events.Event) []byte {
	if e.Entity == store.EntityMembership {
		m := membershipData{PersonID: e.Person, MappingID: e.Mapping}
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
