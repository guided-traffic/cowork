package api

import (
	"context"
	"errors"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/store"
	"github.com/guided-traffic/cowork/backend/internal/store/writeq"
)

// fieldChatCapabilities names the person's chat capabilities in an audit row.
const fieldChatCapabilities = "chat_capabilities"

// GetMyChat answers the capabilities the person gives the chat in the UI
// (docs/adr/0043 D5): the set the person chose, or the default.
func (s *Server) GetMyChat(ctx context.Context, _ apigen.GetMyChatRequestObject) (apigen.GetMyChatResponseObject, error) {
	p := principal(ctx)
	caps, chosen, err := s.db.ChatCapabilities(ctx, p.PersonID)
	if err != nil {
		return nil, err
	}
	if !chosen {
		caps = auth.DefaultChatCapabilities
	}
	return apigen.GetMyChat200JSONResponse(chatCapabilitiesView(caps, chosen)), nil
}

// SetMyChat replaces the capabilities the person gives the chat (docs/adr/0043
// D5): what the chat's next request holds, a running turn's included. The
// document takes a browser session only, and the pipeline refuses a session
// the agent header marks: what the chat may do is access to the person's
// tenants, and the chat never widens its own (docs/adr/0035 D5). The change is
// the person's recorded act; the same set again changes nothing.
func (s *Server) SetMyChat(ctx context.Context, req apigen.SetMyChatRequestObject) (apigen.SetMyChatResponseObject, error) {
	p := principal(ctx)
	want := ordered(req.Body.Capabilities)
	_, err := s.db.Mutate(ctx, uuid.Nil, func(w *store.Writer) error {
		before, err := w.GetChatCapabilitiesForUpdate(ctx, p.PersonID)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			before = auth.DefaultChatCapabilities
		case err != nil:
			return err
		case slices.Equal(before, want):
			return store.ErrNoChange
		}
		if err := w.SetChatCapabilities(ctx, writeq.SetChatCapabilitiesParams{UserID: p.PersonID, Capabilities: want}); err != nil {
			return err
		}
		w.Record(store.Event{EntityType: entityUser, EntityID: p.PersonID, Action: actionUpdated,
			Before: map[string]any{fieldChatCapabilities: before}, After: map[string]any{fieldChatCapabilities: want}})
		return nil
	})
	if err != nil && !errors.Is(err, store.ErrNoChange) {
		return nil, err
	}
	return apigen.SetMyChat200JSONResponse(chatCapabilitiesView(want, true)), nil
}

// ordered is a set of capabilities in the order of the catalogue, each once.
func ordered(in []apigen.Capability) []string {
	out := []string{}
	for _, c := range auth.AllCapabilities {
		if slices.Contains(in, apigen.Capability(c)) {
			out = append(out, c)
		}
	}
	return out
}

func chatCapabilitiesView(caps []string, chosen bool) apigen.ChatCapabilities {
	v := apigen.ChatCapabilities{Capabilities: make([]apigen.Capability, 0, len(caps)), Chosen: chosen}
	for _, c := range caps {
		v.Capabilities = append(v.Capabilities, apigen.Capability(c))
	}
	return v
}
