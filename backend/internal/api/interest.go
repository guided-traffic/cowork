package api

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/store"
	"github.com/guided-traffic/cowork/backend/internal/store/readq"
	"github.com/guided-traffic/cowork/backend/internal/store/writeq"
)

const (
	entityInterest = "interest"
	actionInterest = "interest"
	weightWatch    = "watch"
)

// ListInterest lists who holds a stake in a ticket (docs/adr/0013 D2).
func (s *Server) ListInterest(ctx context.Context, req apigen.ListInterestRequestObject) (apigen.ListInterestResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, read); perr != nil {
		return nil, perr
	}
	const op = "listInterest"
	scope := fmt.Sprintf("%s/%s/%d", t.ID, req.Project, req.Number)
	size := s.h.pageSize(req.Params.Limit)
	var rows []readq.ListInterestRow
	err := s.db.InTenant(ctx, t.ID, func(r *store.Reader) error {
		tc, err := visibleTicket(ctx, r, t, req.Project, req.Number)
		if err != nil {
			return err
		}
		after, err := s.uuidAfter(op, scope, req.Params.Cursor)
		if err != nil {
			return err
		}
		rows, err = r.ListInterest(ctx, readq.ListInterestParams{TenantID: t.ID, TicketID: tc.row.ID, After: after, PageSize: limitArg(size)})
		return err
	})
	if err != nil {
		return nil, err
	}
	rows, next := page(s.h, rows, size, op, scope, func(i readq.ListInterestRow) string { return i.UserID.String() })
	out := apigen.InterestList{Items: make([]apigen.Interest, 0, len(rows)), NextCursor: nullableString(next)}
	for _, i := range rows {
		out.Items = append(out.Items, apigen.Interest{Person: personView(i.UserID, i.Username, i.DisplayName),
			Weight: apigen.InterestWeight(i.Weight), Note: i.Note, Agent: nullableOf(i.Agent),
			Token: tokenMarkView(i.TokenID, i.TokenName), Since: i.Since, UpdatedAt: i.UpdatedAt, Settled: i.Settled})
	}
	tag, unchanged := listTag(req.Params.IfNoneMatch, out)
	if unchanged {
		return apigen.ListInterest304Response{Headers: apigen.NotModifiedResponseHeaders{ETag: &tag}}, nil
	}
	return apigen.ListInterest200JSONResponse{Body: out, Headers: apigen.ListInterest200ResponseHeaders{ETag: &tag}}, nil
}

// interestNeed is what a stake of the weight needs: watch is open to viewers
// and agents (docs/adr/0034 D1, docs/adr/0043 D2), need and urgent are a
// member's and an agent's with the interest capability (docs/adr/0043 D4).
func interestNeed(weight string) auth.Need {
	if weight == weightWatch {
		return auth.Need{Role: domain.RoleViewer, Scope: domain.ScopeWrite}
	}
	return auth.Need{Role: domain.RoleMember, Scope: domain.ScopeWrite, Capability: auth.CapInterest}
}

// SetInterest sets the caller's own stake; one row per person and ticket
// (docs/adr/0013 D1). The stake carries the mark of the write that set it
// (docs/adr/0036 D6).
func (s *Server) SetInterest(ctx context.Context, req apigen.SetInterestRequestObject) (apigen.SetInterestResponseObject, error) {
	t := tenantFrom(ctx)
	weight, note := string(req.Body.Weight), deref(req.Body.Note)
	var out readq.GetInterestRow
	var settled, created bool
	_, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		tc, err := visibleTicket(ctx, w.Reader, t, req.Project, req.Number)
		if err != nil {
			return err
		}
		p := principal(ctx)
		if perr := auth.Authorize(p, tc.role, interestNeed(weight)); perr != nil {
			return perr
		}
		settled = tc.row.State.Terminal()
		key := readq.GetInterestParams{TenantID: t.ID, TicketID: tc.row.ID, UserID: p.PersonID}
		cur, err := w.GetInterest(ctx, key)
		ev := store.Event{EntityType: entityInterest, EntityID: p.PersonID, TicketID: tc.row.ID, TicketKey: ticketKey(t, tc.row),
			Action: actionInterest, After: map[string]any{fieldWeight: weight, fieldNote: note}}
		tokenID, tokenName := actToken(p)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			err = w.InsertInterest(ctx, writeq.InsertInterestParams{TenantID: t.ID, TicketID: tc.row.ID, UserID: p.PersonID, Weight: weight,
				Note: note, Agent: actAgent(p), TokenID: tokenID, TokenName: tokenName})
			created = true
		case err != nil:
			return err
		case cur.Weight == weight && cur.Note == note:
			out = cur
			return store.ErrNoChange
		default:
			ev.Before = map[string]any{fieldWeight: cur.Weight, fieldNote: cur.Note}
			err = w.UpdateInterest(ctx, writeq.UpdateInterestParams{TenantID: t.ID, TicketID: tc.row.ID, UserID: p.PersonID, Weight: weight,
				Note: note, Agent: actAgent(p), TokenID: tokenID, TokenName: tokenName})
		}
		if err != nil {
			return fmt.Errorf("write the interest: %w", err)
		}
		w.Record(ev)
		out, err = w.GetInterest(ctx, key)
		return err
	})
	if err != nil && !errors.Is(err, store.ErrNoChange) {
		return nil, err
	}
	v := apigen.Interest{Person: personView(out.UserID, out.Username, out.DisplayName), Weight: apigen.InterestWeight(out.Weight),
		Note: out.Note, Agent: nullableOf(out.Agent), Token: tokenMarkView(out.TokenID, out.TokenName), Since: out.Since,
		UpdatedAt: out.UpdatedAt, Settled: settled}
	if created {
		return apigen.SetInterest201JSONResponse(v), nil
	}
	return apigen.SetInterest200JSONResponse(v), nil
}

// RemoveInterest removes the caller's own stake; removing none is success.
func (s *Server) RemoveInterest(ctx context.Context, req apigen.RemoveInterestRequestObject) (apigen.RemoveInterestResponseObject, error) {
	t := tenantFrom(ctx)
	_, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		tc, err := visibleTicket(ctx, w.Reader, t, req.Project, req.Number)
		if err != nil {
			return err
		}
		p := principal(ctx)
		if perr := auth.Authorize(p, tc.role, interestNeed(weightWatch)); perr != nil {
			return perr
		}
		key := readq.GetInterestParams{TenantID: t.ID, TicketID: tc.row.ID, UserID: p.PersonID}
		cur, err := w.GetInterest(ctx, key)
		if errors.Is(err, pgx.ErrNoRows) {
			return store.ErrNoChange
		}
		if err != nil {
			return err
		}
		if _, err := w.DeleteInterest(ctx, writeq.DeleteInterestParams(key)); err != nil {
			return fmt.Errorf("remove the interest: %w", err)
		}
		w.Record(store.Event{EntityType: entityInterest, EntityID: p.PersonID, TicketID: tc.row.ID, TicketKey: ticketKey(t, tc.row),
			Action: actionInterest, Before: map[string]any{fieldWeight: cur.Weight, fieldNote: cur.Note}})
		return nil
	})
	if err != nil && !errors.Is(err, store.ErrNoChange) {
		return nil, err
	}
	return apigen.RemoveInterest204Response{}, nil
}
