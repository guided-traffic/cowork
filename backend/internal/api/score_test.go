package api

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/store"
	"github.com/guided-traffic/cowork/backend/internal/store/readq"
)

// A sort by the score gives the tickets the keys they hold among themselves
// in the score's order; an equal score keeps the rank's order, and only the
// tickets whose key changes are written (docs/adr/0014 D3).
func TestSortByScore(t *testing.T) {
	a, b, c, d := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	ids, keys := sortByScore([]rankedScore{{b, "1", 9}, {c, "2", 5}, {a, "3", 2}, {d, "4", 5}})
	assert.Equal(t, []uuid.UUID{d, a}, ids, "d passes a; b and c keep their keys")
	assert.Equal(t, []string{"3", "4"}, keys)

	ids, keys = sortByScore([]rankedScore{{a, "1", 2}, {d, "2", 5}, {c, "3", 5}})
	assert.Equal(t, []uuid.UUID{d, c, a}, ids, "an equal score keeps the rank's order: d before c")
	assert.Equal(t, []string{"1", "2", "3"}, keys)

	ids, keys = sortByScore([]rankedScore{{a, "1", 9}, {b, "2", 5}, {c, "3", 5}})
	assert.Empty(t, ids, "the rank follows the score already")
	assert.Empty(t, keys)

	ids, _ = sortByScore(nil)
	assert.Empty(t, ids)
}

// The keys of a place ask for a rebalancing when they grow past the bound, or
// when no key fits between the neighbours.
func TestCrowded(t *testing.T) {
	assert.False(t, crowded("V", nil))
	assert.False(t, crowded(strings.Repeat("V", domain.RankRebalanceLength), nil))
	assert.True(t, crowded(strings.Repeat("V", domain.RankRebalanceLength+1), nil))
	assert.True(t, crowded("", errors.Join(errors.New("place the rank"), domain.ErrRankTooLong)))
	assert.False(t, crowded("", domain.ErrRankOrder), "another failure is no rebalancing's to mend")
}

// A ticket shows its score at the moment of the read; a done or dropped one,
// and one a release before the score filed, shows none (docs/adr/0014 D3).
func TestScoreView(t *testing.T) {
	opened := time.Date(2026, 9, 5, 9, 0, 0, 0, time.UTC)
	key := domain.ScoreKey(domain.ScoreInputs{Severity: "high", Horizon: domain.UrgencyNow, Need: 1, OpenedAt: opened})
	row := store.TicketRow{State: domain.StateDecided, ScoreKey: key, ScoreVersion: domain.ScoreVersion}
	score, version := scoreView(row, opened.Add(30*24*time.Hour))
	assert.InDelta(t, 15, score.MustGet(), 1e-9, "5 + 8 + 1 and a month of age")
	assert.Equal(t, domain.ScoreVersion, version.MustGet())

	for name, r := range map[string]store.TicketRow{
		"done":     {State: domain.StateDone, ScoreKey: key, ScoreVersion: 1},
		"dropped":  {State: domain.StateDropped, ScoreKey: key, ScoreVersion: 1},
		"unscored": {State: domain.StateFiled, ScoreKey: math.Inf(-1)},
	} {
		score, version := scoreView(r, opened)
		assert.True(t, score.IsNull(), name)
		assert.True(t, version.IsNull(), name)
	}
}

// The person-level lists merge their tenants' parts by the score's key,
// highest first, then by the ticket's id; a ticket without a score is last.
func TestByScore(t *testing.T) {
	low, high := uuid.MustParse("00000000-0000-7000-8000-000000000001"), uuid.MustParse("00000000-0000-7000-8000-000000000002")
	assert.Negative(t, byScore(5, 3, high, low), "the higher score first, whatever the id")
	assert.Positive(t, byScore(3, 5, low, high))
	assert.Negative(t, byScore(5, 5, low, high), "an equal score by the id")
	assert.Positive(t, byScore(math.Inf(-1), -100, low, high), "no score after every score")
}

// The open decisions resume after the position their cursor names; anything
// else is no position.
func TestDecisionPosition(t *testing.T) {
	id := uuid.New()
	d := decision{}
	d.row.TicketScoreKey, d.row.TicketID, d.row.Number = -651.25, id, 3
	var got readq.ListOpenDecisionsParams
	require.True(t, decisionAfter(decisionPosition(d), &got))
	assert.True(t, got.HasAfter)
	assert.Equal(t, -651.25, got.AfterKey)
	assert.Equal(t, id, got.AfterTicket)
	assert.Equal(t, int32(3), got.AfterQuestion)

	none := decision{}
	none.row.TicketScoreKey, none.row.TicketID = math.Inf(-1), id
	got = readq.ListOpenDecisionsParams{}
	require.True(t, decisionAfter(decisionPosition(none), &got))
	assert.True(t, math.IsInf(got.AfterKey, -1), "a ticket without a score")
	for _, bad := range []string{"", "1/2", "x/" + id.String() + "/1", "1/" + id.String() + "/x", "+Inf/" + id.String() + "/1",
		"NaN/" + id.String() + "/1", "1/" + id.String() + "/1/2", "1/not-an-id/1"} {
		var params readq.ListOpenDecisionsParams
		assert.False(t, decisionAfter(bad, &params), bad)
	}
}

// A position of the score's order reads back to the same key and id.
func TestScorePositionRoundTrip(t *testing.T) {
	id := uuid.New()
	for _, key := range []float64{-651.2537190123, 0, 12.5, math.Inf(-1), math.Nextafter(-650, 0)} {
		k, got, err := store.ParseScorePosition(store.ScorePosition(key, id))
		require.NoError(t, err)
		assert.Equal(t, key, k)
		assert.Equal(t, id, got)
	}
}
