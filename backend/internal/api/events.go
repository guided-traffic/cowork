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
	"github.com/guided-traffic/cowork/backend/internal/store"
	"github.com/guided-traffic/cowork/backend/internal/store/readq"
)

// opStreamEvents is the event stream's operation, served outside the
// generated server.
const opStreamEvents = "streamEvents"

// defaultHeartbeat keeps proxies from closing an idle stream
// (docs/adr/0054 D5).
const defaultHeartbeat = 20 * time.Second

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
	filter, err := h.streamFilter(ctx, t, p)
	if err != nil {
		h.writeError(w, r, err)
		return
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
	flusher.Flush()
	h.pump(ctx, w, flusher, stream, t, p, filter)
}

// pump writes the stream's events until it ends: the client leaves, the hub
// ends it, or the heartbeat finds the token or the membership gone. An act
// that changes what the stream may admit makes it compute its filter again
// before the next event, and so does every heartbeat (docs/adr/0054 D3).
func (h *handler) pump(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, stream *events.Stream, t tenantScope, p auth.Principal, filter events.Filter) {
	beat := h.opts.Heartbeat
	if beat <= 0 {
		beat = defaultHeartbeat
	}
	ticker := time.NewTicker(beat)
	defer ticker.Stop()
	// refiltered is how many of the stream's admission changes the filter knows.
	var refiltered uint64
	for {
		select {
		case <-ctx.Done():
			return
		case e := <-stream.C:
			if e.Refilter > refiltered {
				var ok bool
				if filter, refiltered, ok = h.refilter(ctx, stream, t, p); !ok {
					return
				}
			}
			if e.Withheld || (e.Unjudged && !filter.Admits(e)) {
				continue
			}
			writeEvent(w, e)
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
			seen := h.opts.Events.Changes(stream)
			f, err := h.streamFilter(ctx, now, p)
			if err != nil {
				return
			}
			h.opts.Events.Refilter(stream, f, seen)
			filter, refiltered = f, max(refiltered, seen)
			_, _ = fmt.Fprint(w, ": heartbeat\n\n")
		}
		flusher.Flush()
	}
}

// refilter computes what the stream admits again after an act that changes
// it: the person's role in the tenant as the boundary reads it now, and the
// projects they see. A person the boundary no longer admits ends the stream,
// as the heartbeat would. It answers the filter and how many admission
// changes it knows.
func (h *handler) refilter(ctx context.Context, stream *events.Stream, t tenantScope, p auth.Principal) (events.Filter, uint64, bool) {
	seen := h.opts.Events.Changes(stream)
	now, perr := h.boundary(ctx, t.Slug, "/api/v1/tenants/{tenant}/events", opStreamEvents)
	if perr != nil {
		return events.Filter{}, 0, false
	}
	f, err := h.streamFilter(ctx, now, p)
	if err != nil {
		return events.Filter{}, 0, false
	}
	h.opts.Events.Refilter(stream, f, seen)
	return f, seen, true
}

// streamFilter computes what the stream admits: the projects visible to the
// caller now, which a project-restricted token narrows to its own, and the
// confidential rule (docs/adr/0054 D3, docs/adr/0065 D5).
func (h *handler) streamFilter(ctx context.Context, t tenantScope, p auth.Principal) (events.Filter, error) {
	f := events.Filter{Person: p.PersonID, Admin: t.Role == domain.RoleAdmin, Projects: map[uuid.UUID]bool{},
		RestrictedProject: p.RestrictedProjectID}
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
	Key     string `json:"key"`
	Version int32  `json:"version"`
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
	data, _ := json.Marshal(eventData{Key: e.Key, Version: e.Version, Kind: e.Action})
	if e.Entity == store.EntityMembership {
		m := membershipData{PersonID: e.Person, MappingID: e.Mapping}
		if e.Project != uuid.Nil {
			m.ProjectID = &e.Project
		}
		data, _ = json.Marshal(m)
	}
	// #nosec G705 -- text/event-stream of a uuid, a fixed event name and JSON the server encodes; no HTML
	_, _ = fmt.Fprintf(w, "id: %s\nevent: %s\ndata: %s\n\n", e.ID, e.Name(), data)
}

func writeControl(w http.ResponseWriter, name string) {
	// #nosec G705 -- text/event-stream of a fixed control name; no HTML
	_, _ = fmt.Fprintf(w, "event: %s\ndata: {}\n\n", name)
}
