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
	h.pump(ctx, w, flusher, stream, t, p)
}

// pump writes the stream's events until it ends: the client leaves, the hub
// ends it, or the heartbeat finds the token or the membership gone. Each
// heartbeat also recomputes what the stream admits.
func (h *handler) pump(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, stream *events.Stream, t tenantScope, p auth.Principal) {
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
			filter, err := h.streamFilter(ctx, now, p)
			if err != nil {
				return
			}
			h.opts.Events.Refilter(stream, filter)
			_, _ = fmt.Fprint(w, ": heartbeat\n\n")
		}
		flusher.Flush()
	}
}

// streamFilter computes what the stream admits: the projects visible to the
// caller now, which a project-restricted token narrows to its own, and the
// confidential rule (docs/adr/0054 D3, docs/adr/0065 D5).
func (h *handler) streamFilter(ctx context.Context, t tenantScope, p auth.Principal) (events.Filter, error) {
	f := events.Filter{Person: p.PersonID, Admin: t.Role == domain.RoleAdmin, Projects: map[uuid.UUID]bool{}}
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
// token is usable and the person still belongs to the tenant
// (docs/adr/0035 D6). It returns the tenant as the person holds it now,
// their role included.
func (h *handler) stillAdmitted(ctx context.Context, t tenantScope, p auth.Principal) (tenantScope, bool) {
	usable := false
	err := h.opts.DB.Installation(ctx, func(r *store.Reader) error {
		var err error
		usable, err = r.TokenStillUsable(ctx, readq.TokenStillUsableParams{TokenID: p.TokenID, UserID: p.PersonID})
		return err
	})
	if err != nil || !usable {
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

func writeEvent(w http.ResponseWriter, e events.Event) {
	data, _ := json.Marshal(eventData{Key: e.Key, Version: e.Version, Kind: e.Action})
	// #nosec G705 -- text/event-stream of a uuid, a fixed event name and JSON the server encodes; no HTML
	_, _ = fmt.Fprintf(w, "id: %s\nevent: %s\ndata: %s\n\n", e.ID, e.Name(), data)
}

func writeControl(w http.ResponseWriter, name string) {
	// #nosec G705 -- text/event-stream of a fixed control name; no HTML
	_, _ = fmt.Fprintf(w, "event: %s\ndata: {}\n\n", name)
}
