package api

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/problem"
	"github.com/guided-traffic/cowork/backend/internal/store"
	"github.com/guided-traffic/cowork/backend/internal/store/writeq"
)

// The score of a ticket (docs/adr/0014 D3–D5): domain.ScoreKey computes it
// whenever one of its inputs changes — a filing, the severity, the horizon, a
// stake — and the ticket stores the key with the version that computed it;
// the age is added when the ticket is shown (domain.ScoreAt). The score warns
// where the facts disagree with the rank, orders the person-level lists and is
// what the sort by score adopts; it never moves the rank by itself.

// sortedBy is the order a sort of the rank names: the score's, the one there
// is (docs/adr/0014 D3).
const sortedBy = "score"

// scoreView is a ticket's score at a moment and the version that computed it;
// none for a done or dropped ticket (D3) and for one a release before the
// score filed, until an input of it changes.
func scoreView(r store.TicketRow, now time.Time) (nullable.Nullable[float64], nullable.Nullable[int]) {
	if r.State.Terminal() || r.ScoreVersion == 0 {
		return nullableOf[float64](nil), nullableOf[int](nil)
	}
	score, version := domain.ScoreAt(r.ScoreKey, now), int(r.ScoreVersion)
	return nullableOf(&score), nullableOf(&version)
}

// refreshScore scores a ticket again after a write in this transaction
// changed an input of its score: its filing, its severity, its horizon or a
// stake in it (docs/adr/0014 D4). The score is derived, so it records no act
// and leaves the version alone.
func refreshScore(ctx context.Context, w *store.Writer, t tenantScope, id uuid.UUID) error {
	in, err := w.GetScoreInputs(ctx, writeq.GetScoreInputsParams{TenantID: t.ID, ID: id})
	if err != nil {
		return fmt.Errorf("read the score's inputs: %w", err)
	}
	key := domain.ScoreKey(domain.ScoreInputs{Severity: in.Severity, Horizon: in.Horizon, Need: int(in.Need),
		Urgent: int(in.Urgent), OpenedAt: in.OpenedAt})
	if err := w.SetTicketScore(ctx, writeq.SetTicketScoreParams{TenantID: t.ID, ID: id, ScoreKey: key,
		ScoreVersion: domain.ScoreVersion}); err != nil {
		return fmt.Errorf("store the score: %w", err)
	}
	return nil
}

// SortProjectRank reorders the open tickets of a project the caller sees by
// their score, in one recorded act (docs/adr/0014 D3): each is scored anew
// with the function as it stands, and they take the keys they hold among
// themselves in the score's order, so a ticket the caller cannot see keeps its
// key and its place, and every horizon's group and every parent's children,
// which read the rank over a part of the project, read in the score's order.
// The tickets' versions stay: it is the project's act, not theirs.
func (s *Server) SortProjectRank(ctx context.Context, req apigen.SortProjectRankRequestObject) (apigen.SortProjectRankResponseObject, error) {
	t := tenantFrom(ctx)
	if req.Body.By != sortedBy {
		return nil, problem.Field("/by", "the rank is sorted by the score")
	}
	moved := 0
	_, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		p, err := visibleProject(ctx, w.Reader, t, req.Project)
		if err != nil {
			return err
		}
		role, err := projectRole(ctx, w.Reader, t, p)
		if err != nil {
			return err
		}
		if perr := auth.Authorize(principal(ctx), role, rankNeed); perr != nil {
			return perr
		}
		ids, err := sortRank(ctx, w, t, p.ID)
		if err != nil {
			return err
		}
		moved = len(ids)
		// The act names every ticket it moved, whose activity shows it
		// (docs/adr/0015 D1); each is one the caller sees.
		w.Record(store.Event{EntityType: entityProject, EntityID: p.ID, Action: actionRanked,
			After: map[string]any{"by": sortedBy, "score_version": domain.ScoreVersion, "moved": moved},
			Refs:  ids, Project: &store.ProjectChange{ID: p.ID, Key: t.Slug + "/" + p.Key}})
		return nil
	})
	if err != nil && !errors.Is(err, store.ErrNoChange) {
		return nil, err
	}
	return apigen.SortProjectRank200JSONResponse{Moved: moved, ScoreVersion: domain.ScoreVersion}, nil
}

// sortRank writes the score's order into the project's rank under the rank
// lock and returns the tickets that changed their place; ErrNoChange when the
// rank followed the score already, which rolls back the scores written anew
// and the keys given to unranked tickets with it.
func sortRank(ctx context.Context, w *store.Writer, t tenantScope, projectID uuid.UUID) ([]uuid.UUID, error) {
	if err := lockRank(ctx, w, t, projectID); err != nil {
		return nil, err
	}
	if _, err := rankUnranked(ctx, w, t, projectID); err != nil {
		return nil, err
	}
	rows, err := w.ListScoredTickets(ctx, writeq.ListScoredTicketsParams{TenantID: t.ID, ProjectID: projectID})
	if err != nil {
		return nil, fmt.Errorf("read the tickets to sort: %w", err)
	}
	tickets := make([]rankedScore, 0, len(rows))
	for _, r := range rows {
		key := domain.ScoreKey(domain.ScoreInputs{Severity: r.Severity, Horizon: r.Horizon, Need: int(r.Need),
			Urgent: int(r.Urgent), OpenedAt: r.OpenedAt})
		if key != r.ScoreKey || r.ScoreVersion != domain.ScoreVersion {
			if err := w.SetTicketScore(ctx, writeq.SetTicketScoreParams{TenantID: t.ID, ID: r.ID, ScoreKey: key,
				ScoreVersion: domain.ScoreVersion}); err != nil {
				return nil, fmt.Errorf("store the score: %w", err)
			}
		}
		tickets = append(tickets, rankedScore{id: r.ID, rank: r.Rank, score: key})
	}
	ids, keys := sortByScore(tickets)
	if len(ids) == 0 {
		return nil, store.ErrNoChange
	}
	if err := writeRanks(ctx, w, t, ids, ids, keys); err != nil {
		return nil, err
	}
	return ids, nil
}

// rankedScore is a ticket in the rank with its score's key.
type rankedScore struct {
	id    uuid.UUID
	rank  string
	score float64
}

// sortByScore decides a sort: the tickets, given in the rank's order, take
// the keys they hold among themselves in the score's order — highest first,
// an equal score keeping the rank's order. It returns the tickets whose key
// changes and their new keys; none when the rank follows the score already.
func sortByScore(tickets []rankedScore) ([]uuid.UUID, []string) {
	sorted := slices.Clone(tickets)
	slices.SortStableFunc(sorted, func(a, b rankedScore) int { return cmp.Compare(b.score, a.score) })
	var ids []uuid.UUID
	var keys []string
	for i, tk := range sorted {
		if slot := tickets[i].rank; slot != tk.rank {
			ids, keys = append(ids, tk.id), append(keys, slot)
		}
	}
	return ids, keys
}

// writeRanks takes the keys of release away and gives ids their keys, keys
// pairwise: a key belongs to one ticket of its project, so keys that change
// hands are released first (ReleaseRanks).
func writeRanks(ctx context.Context, w *store.Writer, t tenantScope, release, ids []uuid.UUID, keys []string) error {
	if err := w.ReleaseRanks(ctx, writeq.ReleaseRanksParams{TenantID: t.ID, Ids: release}); err != nil {
		return fmt.Errorf("release the rank keys: %w", err)
	}
	if err := w.SetRanks(ctx, writeq.SetRanksParams{TenantID: t.ID, Ids: ids, Ranks: keys}); err != nil {
		return fmt.Errorf("write the rank keys: %w", err)
	}
	return nil
}
