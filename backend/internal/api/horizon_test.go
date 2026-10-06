package api

import (
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/domain"
)

// docs/adr/0010 D3: a filing names its horizon, later when left out; an
// agent needs set-horizon for one other than later (docs/adr/0043 D4).
func TestAFilingNamesItsHorizon(t *testing.T) {
	horizon := func(h apigen.Horizon) *apigen.Horizon { return &h }
	for name, c := range map[string]struct {
		body apigen.TicketCreate
		want domain.Urgency
	}{
		"none":    {apigen.TicketCreate{}, domain.UrgencyLater},
		"horizon": {apigen.TicketCreate{Horizon: horizon("next")}, domain.UrgencyNext},
	} {
		f, perr := filingOf(c.body)
		require.Nil(t, perr, name)
		assert.Equal(t, c.want, f.horizon, name)
	}

	f, _ := filingOf(apigen.TicketCreate{Horizon: horizon("now")})
	assert.Equal(t, []string{auth.CapSetHorizon}, f.capabilities(), "a horizon other than later needs set-horizon")
	f, _ = filingOf(apigen.TicketCreate{Horizon: horizon("later")})
	assert.Empty(t, f.capabilities())
}

// docs/adr/0049 D1, D4: the list filter horizon takes the horizons, each
// negatable, and names a value it does not know at query:horizon.
func TestTheHorizonFilter(t *testing.T) {
	values := func(v ...string) *[]string { return &v }
	s := &Server{h: &handler{}}
	var l ticketListing
	assert.Empty(t, s.parseFilters(uuid.Nil, ticketQuery{horizon: values("now", "!icebox")}, &l))
	assert.Equal(t, []string{"now"}, l.filter.Horizons.In)
	assert.Equal(t, []string{"icebox"}, l.filter.Horizons.NotIn)
	errs := s.parseFilters(uuid.Nil, ticketQuery{horizon: values("soon")}, &ticketListing{})
	require.Len(t, errs, 1)
	assert.Equal(t, "query:horizon", errs[0].Pointer)
}

// docs/adr/0043 D4: a capability set is answered each name once, and a chat's
// set is stored in the catalogue's order.
func TestCapabilitiesAreAnsweredEachOnce(t *testing.T) {
	assert.Equal(t, []apigen.Capability{"rank", "set-horizon"}, capabilitiesView([]string{"rank", "set-horizon", "rank"}))
	assert.Equal(t, []apigen.Capability{}, capabilitiesView(nil))
	assert.Equal(t, []string{"rank", "set-horizon"}, ordered([]apigen.Capability{"set-horizon", "rank"}),
		"a chat set is stored in the catalogue's order")
}

// docs/adr/0043 D4 as amended 2026-10-06: a set release 0.5 stored after an
// image rollback — override-urgency beside set-horizon — is answered without
// the old name, so every answer holds only values of the document's
// Capability.
func TestASetTheReleaseBeforeStoredIsAnsweredInTheDocumentsValues(t *testing.T) {
	stored := append(slices.Clone(auth.AllCapabilities), "override-urgency")
	answered := capabilitiesView(stored)
	assert.Len(t, answered, len(auth.AllCapabilities))
	for _, c := range answered {
		assert.True(t, c.Valid(), "%s is a value of Capability", c)
	}
	assert.Equal(t, []apigen.Capability{"rank", "set-horizon"}, chatCapabilitiesView([]string{"rank", "set-horizon", "override-urgency"}, true).Capabilities)
}
