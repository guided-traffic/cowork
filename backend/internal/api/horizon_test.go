package api

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/problem"
)

// docs/adr/0010 D1 as amended 2026-10-05: a filing names its horizon as
// horizon, or as urgency, the name before; both with different values are
// refused at /horizon, the same value twice is that horizon.
//
//nolint:staticcheck // SA1019: TicketCreate.Urgency is the deprecated field under test
func TestAFilingNamesItsHorizonUnderEitherName(t *testing.T) {
	horizon := func(h apigen.Horizon) *apigen.Horizon { return &h }
	urgency := func(u apigen.Urgency) *apigen.Urgency { return &u }
	for name, c := range map[string]struct {
		body apigen.TicketCreate
		want domain.Urgency
	}{
		"none":         {apigen.TicketCreate{}, domain.UrgencyLater},
		"horizon":      {apigen.TicketCreate{Horizon: horizon("next")}, domain.UrgencyNext},
		"urgency":      {apigen.TicketCreate{Urgency: urgency("icebox")}, domain.UrgencyIcebox},
		"both, agreed": {apigen.TicketCreate{Horizon: horizon("now"), Urgency: urgency("now")}, domain.UrgencyNow},
	} {
		f, perr := filingOf(c.body)
		require.Nil(t, perr, name)
		assert.Equal(t, c.want, f.horizon, name)
	}
	_, perr := filingOf(apigen.TicketCreate{Horizon: horizon("now"), Urgency: urgency("next")})
	assertProblem(t, perr, problem.ValidationFailed, "/horizon", "two names, two horizons")

	f, _ := filingOf(apigen.TicketCreate{Urgency: urgency("now")})
	assert.Equal(t, []string{auth.CapSetHorizon}, f.capabilities(), "a horizon other than later needs set-horizon")
	f, _ = filingOf(apigen.TicketCreate{Horizon: horizon("later")})
	assert.Empty(t, f.capabilities())
}

// docs/adr/0049 D4: the list filter takes horizon, or urgency, its name
// before; a query that names both is refused at query:urgency rather than
// combined.
func TestTheHorizonFilterTakesOneName(t *testing.T) {
	values := func(v ...string) *[]string { return &v }
	name, got, conflict := ticketQuery{horizon: values("now")}.horizons()
	assert.Equal(t, "horizon", name)
	assert.Equal(t, []string{"now"}, *got)
	assert.Nil(t, conflict)

	name, got, conflict = ticketQuery{urgency: values("!later")}.horizons()
	assert.Equal(t, "urgency", name, "a refused value names the parameter it came by")
	assert.Equal(t, []string{"!later"}, *got)
	assert.Nil(t, conflict)

	_, _, conflict = ticketQuery{horizon: values("now"), urgency: values("next")}.horizons()
	require.NotNil(t, conflict)
	assert.Equal(t, "query:urgency", conflict.Pointer)

	_, _, conflict = ticketQuery{horizon: values("now"), urgency: values()}.horizons()
	assert.Nil(t, conflict, "an empty urgency names nothing")

	s := &Server{h: &handler{}}
	var l ticketListing
	assert.Empty(t, s.parseFilters(uuid.Nil, ticketQuery{urgency: values("now", "!icebox")}, &l))
	assert.Equal(t, []string{"now"}, l.filter.Horizons.In)
	assert.Equal(t, []string{"icebox"}, l.filter.Horizons.NotIn)
	errs := s.parseFilters(uuid.Nil, ticketQuery{horizon: values("soon")}, &ticketListing{})
	require.Len(t, errs, 1)
	assert.Equal(t, "query:horizon", errs[0].Pointer)
}

// A saved filter stored with urgency reads back under horizon; one stored
// with horizon stays as it is (docs/adr/0010 D1, docs/adr/0049 D7).
//
//nolint:staticcheck // SA1019: SavedFilterParameters.Urgency is the deprecated field under test
func TestASavedFilterReadsBackUnderHorizon(t *testing.T) {
	old := apigen.SavedFilterParameters{Urgency: &[]string{"now", "next"}}
	horizonNamed(&old)
	assert.Equal(t, &[]string{"now", "next"}, old.Horizon)
	assert.Nil(t, old.Urgency)

	current := apigen.SavedFilterParameters{Horizon: &[]string{"later"}}
	horizonNamed(&current)
	assert.Equal(t, &[]string{"later"}, current.Horizon)
	assert.Nil(t, current.Urgency)
}

// docs/adr/0043 D4 as amended 2026-10-05: every answer names a capability set
// in this release's names; /me/token's request set follows set-horizon with
// override-urgency for the cowork-mcp of the release before (docs/adr/0046
// D7).
func TestCapabilitiesAreAnsweredUnderTheirNames(t *testing.T) {
	assert.Equal(t, []apigen.Capability{"rank", "set-horizon"}, capabilitiesView([]string{"rank", "override-urgency"}))
	assert.Equal(t, []apigen.Capability{}, capabilitiesView(nil))
	assert.Equal(t, []apigen.Capability{"rank", "set-horizon", "override-urgency", "upload"},
		requestCapabilities([]string{"rank", "override-urgency", "upload"}))
	assert.Equal(t, []apigen.Capability{"close"}, requestCapabilities([]string{"close"}), "no alias without the capability")
	assert.Equal(t, []apigen.Capability{}, requestCapabilities(nil))
	assert.Equal(t, []string{"rank", "set-horizon"}, ordered([]apigen.Capability{"override-urgency", "rank"}),
		"a chat set sent with the old name is stored with the new one, in the catalogue's order")
}
