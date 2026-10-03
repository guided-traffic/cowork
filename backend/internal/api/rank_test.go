package api

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/problem"
	"github.com/guided-traffic/cowork/backend/internal/store"
)

func ptrInt(v int) *int { return &v }

// A move names exactly one neighbour; neither and both are refused on both
// fields.
func TestRankTargetOf(t *testing.T) {
	after, perr := rankTargetOf(apigen.TicketRankSet{After: ptrInt(3)})
	require.Nil(t, perr)
	assert.Equal(t, rankTarget{after: true, number: 3}, after)
	assert.Equal(t, "/after", after.pointer())

	before, perr := rankTargetOf(apigen.TicketRankSet{Before: ptrInt(4)})
	require.Nil(t, perr)
	assert.Equal(t, rankTarget{number: 4}, before)
	assert.Equal(t, "/before", before.pointer())

	for name, body := range map[string]apigen.TicketRankSet{
		"neither": {},
		"both":    {After: ptrInt(3), Before: ptrInt(4)},
	} {
		_, perr := rankTargetOf(body)
		require.NotNil(t, perr, name)
		assert.Equal(t, problem.ValidationFailed, perr.Code, name)
		require.Len(t, perr.Errors, 2, name)
		assert.Equal(t, "/after", perr.Errors[0].Pointer)
		assert.Equal(t, "/before", perr.Errors[1].Pointer)
	}
}

func TestRankSelf(t *testing.T) {
	perr := rankSelf(7, rankTarget{after: true, number: 7})
	require.NotNil(t, perr)
	assert.Equal(t, problem.ValidationFailed, perr.Code)
	assert.Equal(t, "/after", perr.Errors[0].Pointer)
	assert.Nil(t, rankSelf(7, rankTarget{number: 8}))
}

// A done or dropped ticket has no rank, as the moved ticket and as the
// neighbour (docs/adr/0014 D1); a blocked one keeps its rank.
func TestRankStates(t *testing.T) {
	after := rankTarget{after: true, number: 2}
	for _, s := range []domain.TicketState{domain.StateDone, domain.StateDropped} {
		perr := rankStates(s, domain.StateFiled, after)
		require.NotNil(t, perr, s)
		assert.Equal(t, problem.StateConflict, perr.Code)
		assert.Equal(t, "path:number", perr.Errors[0].Pointer)
		assert.Equal(t, string(s), perr.Errors[0].Current)

		perr = rankStates(domain.StateInProgress, s, rankTarget{number: 2})
		require.NotNil(t, perr, s)
		assert.Equal(t, problem.StateConflict, perr.Code)
		assert.Equal(t, "/before", perr.Errors[0].Pointer)
		assert.Equal(t, string(s), perr.Errors[0].Current)
	}
	assert.Nil(t, rankStates(domain.StateBlocked, domain.StateBlocked, after))
}

// A ticket the caller already sees next to its neighbour is a no-op, whatever
// sits between unseen; otherwise the key lies strictly between the neighbour's
// and the next one's on that side, whoever holds it, at an end beyond the
// neighbour (docs/adr/0014 D2).
func TestPlanRank(t *testing.T) {
	moved, other := uuid.New(), uuid.New()

	key, noop, err := planRank(true, "V", "W", moved, moved)
	require.NoError(t, err)
	assert.True(t, noop, "already directly after")
	assert.Empty(t, key)
	_, noop, err = planRank(false, "V", "U", moved, moved)
	require.NoError(t, err)
	assert.True(t, noop, "already directly before")
	_, noop, err = planRank(true, "V", "VV", moved, moved)
	require.NoError(t, err)
	assert.True(t, noop, "a ticket the caller cannot see between the two changes nothing")

	key, noop, err = planRank(true, "V", "X", other, moved)
	require.NoError(t, err)
	assert.False(t, noop)
	assert.Equal(t, "W", key, "between the neighbour and the next key, whoever holds it")
	key, _, err = planRank(true, "V", "W", other, moved)
	require.NoError(t, err)
	assert.True(t, "V" < key && key < "W", key)
	key, _, err = planRank(false, "V", "U", other, moved)
	require.NoError(t, err)
	assert.True(t, "U" < key && key < "V", key)

	key, _, err = planRank(true, "V", "", uuid.Nil, moved)
	require.NoError(t, err)
	assert.Greater(t, key, "V", "after the last ticket")
	key, _, err = planRank(false, "V", "", uuid.Nil, moved)
	require.NoError(t, err)
	assert.Less(t, key, "V", "before the first ticket")

	_, _, err = planRank(true, "V0", "", uuid.Nil, moved)
	assert.ErrorIs(t, err, domain.ErrRankKey)
}

// A move is a member's act with write scope; an agent needs rank
// (docs/adr/0043 D4).
func TestRankNeed(t *testing.T) {
	person := auth.Principal{Scope: domain.ScopeWrite}
	full := auth.Principal{Scope: domain.ScopeWrite, Agent: "a/b/c", Capabilities: auth.AllCapabilities}
	assisted := auth.Principal{Scope: domain.ScopeWrite, Agent: "a/b/c", Capabilities: []string{auth.CapDrop, auth.CapUpload}}
	for name, c := range map[string]struct {
		p    auth.Principal
		role domain.Role
		code *problem.Code
	}{
		"a member":                {person, domain.RoleMember, nil},
		"a viewer":                {person, domain.RoleViewer, &problem.Forbidden},
		"a read token":            {auth.Principal{Scope: domain.ScopeRead}, domain.RoleAdmin, &problem.InsufficientScope},
		"an agent with rank":      {full, domain.RoleMember, nil},
		"an agent without rank":   {assisted, domain.RoleMember, &problem.AgentForbidden},
		"the role before the cap": {assisted, domain.RoleViewer, &problem.Forbidden},
	} {
		perr := auth.Authorize(c.p, c.role, rankNeed)
		if c.code == nil {
			assert.Nil(t, perr, name)
			continue
		}
		require.NotNil(t, perr, name)
		assert.Equal(t, *c.code, perr.Code, name)
	}
	perr := auth.Authorize(assisted, domain.RoleMember, rankNeed)
	require.NotNil(t, perr)
	assert.Equal(t, "missing capability: rank", perr.Detail)
}

// The project's list names its rank order in the cursor's scope: a cursor of
// the number order it had before is refused, the tenant-wide list's cursors
// stay valid (docs/adr/0048 D5).
func TestTicketListCursorsNameTheRankOrder(t *testing.T) {
	ts := tenantScope{ID: uuid.New(), Slug: "acme"}
	codec := newCursorCodec([]byte("0123456789abcdef0123456789abcdef"))
	before := codec.encode("listProjectTickets", ts.ID.String()+"/ALPHA", "12")
	_, perr := codec.decode("listProjectTickets", ticketListScope(ts, "ALPHA", store.ByRank), before)
	require.NotNil(t, perr)
	assert.Equal(t, problem.InvalidCursor, perr.Code)
	assert.Equal(t, ts.ID.String()+"/", ticketListScope(ts, "", store.NewestFirst), "the tenant-wide list keeps its scope")
}

// A rank position travels sealed (docs/adr/0014 D2, docs/adr/0048 D1): the
// cursor shows neither the key nor its length, one position always seals the
// same way, and only the server key that sealed it opens it.
func TestSealedRankPositions(t *testing.T) {
	codec := newCursorCodec([]byte("0123456789abcdef0123456789abcdef"))
	longest := strings.Repeat("z", 128) + ".2147483647"
	short, long := codec.sealPosition("V.12"), codec.sealPosition(longest)

	payload, _, _ := strings.Cut(codec.encode("listProjectTickets", "scope", short), ".")
	readable, err := base64.RawURLEncoding.DecodeString(payload)
	require.NoError(t, err)
	assert.NotContains(t, string(readable), "V.12", "what a client can decode holds no key")
	assert.Len(t, long, len(short), "nor its length")
	assert.Equal(t, short, codec.sealPosition("V.12"), "a page and its weak ETag stay the same")
	assert.NotEqual(t, short, codec.sealPosition("V.13"))

	for want, sealed := range map[string]string{"V.12": short, longest: long, ".13": codec.sealPosition(".13")} {
		got, ok := codec.openPosition(sealed)
		require.True(t, ok, want)
		assert.Equal(t, want, got)
	}
	tampered := []byte(short)
	if tampered[20] == 'A' {
		tampered[20] = 'B'
	} else {
		tampered[20] = 'A'
	}
	for name, sealed := range map[string]string{
		"an unsealed position, as a cursor carried it before the seal": "V.12",
		"an altered one":                      string(tampered),
		"one sealed under another server key": newCursorCodec([]byte("fedcba9876543210fedcba9876543210")).sealPosition("V.12"),
	} {
		_, ok := codec.openPosition(sealed)
		assert.False(t, ok, name)
	}
}

// A position carries the key and the number; an unranked ticket's key is
// empty, and so is a done or dropped one's, whatever key the release before
// left in its column (docs/adr/0028 D3).
func TestRankPosition(t *testing.T) {
	key := "V"
	assert.Equal(t, "V.12", store.ByRank.Position(store.TicketRow{Rank: &key, Number: 12, State: domain.StateBlocked}))
	assert.Equal(t, ".13", store.ByRank.Position(store.TicketRow{Number: 13}))
	for _, s := range []domain.TicketState{domain.StateDone, domain.StateDropped} {
		assert.Equal(t, ".14", store.ByRank.Position(store.TicketRow{Rank: &key, Number: 14, State: s}), s)
	}
	id := uuid.New()
	assert.Equal(t, id.String(), store.NewestFirst.Position(store.TicketRow{ID: id}))
}
