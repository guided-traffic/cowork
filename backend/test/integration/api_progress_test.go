//go:build integration

package integration

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/oapi-codegen/nullable"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
)

// docs/adr/0017 D2, D3, D5: a parent shows the effort-weighted mean of its
// children, dropped ones left out and done ones at 100, rounded to fives with
// halves up; it takes no manual value until the last child leaves, and a
// derived change bumps no version.
func TestDerivedProgress(t *testing.T) {
	e := newTicketEnv(t)
	member := caller{Token: e.tk.MemberA}
	get := func(tk apigen.Ticket) apigen.Ticket { return *e.get(t, member, "ALPHA", tk.Number).JSON200 }
	child := func(title string, parent apigen.Ticket, effort apigen.Effort, progress int) apigen.Ticket {
		c := e.file(t, member, "ALPHA", task(title, func(b *apigen.TicketCreate) {
			b.Effort, b.Parent = effort, ptr("ALPHA-"+strconv.Itoa(parent.Number))
		}))
		if progress > 0 {
			res := e.patch(t, member, c, apigen.TicketPatch{Progress: ptr(progress)})
			require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
			c = *res.JSON200
		}
		return c
	}

	grand := e.file(t, member, "ALPHA", task("Epic"))
	parent := e.file(t, member, "ALPHA", task("Story", func(b *apigen.TicketCreate) { b.Parent = ptr("ALPHA-" + strconv.Itoa(grand.Number)) }))
	version := parent.Version
	a := child("a", parent, apigen.EffortXS, 50)
	b := child("b", parent, apigen.EffortL, 0)
	got := get(parent)
	assert.Equal(t, 10, got.Progress, "(1×50 + 5×0) / 6 = 8.3 → 10")
	assert.True(t, got.ProgressDerived)
	assert.Equal(t, version, got.Version, "a derived change bumps no version")
	assert.Equal(t, 10, get(grand).Progress, "the derivation climbs to the ancestors")

	refused := e.patch(t, member, got, apigen.TicketPatch{Progress: ptr(40)})
	require.Equal(t, http.StatusConflict, refused.StatusCode(), "a parent takes no manual value")

	e.walk(t, member, b, toAnalysed, toDecided, toInProgress, toDone)
	assert.Equal(t, 90, get(parent).Progress, "a done child counts 100: (50 + 500) / 6 = 91.7 → 90")
	res := e.move(t, member, a, apigen.Transition{From: apigen.TicketStateFiled, To: apigen.TicketStateDropped, Reason: ptr("not needed")})
	require.Equal(t, http.StatusOK, res.StatusCode())
	assert.Equal(t, 100, get(parent).Progress, "a dropped child is left out")

	halves := e.file(t, member, "ALPHA", task("Halves"))
	child("h1", halves, apigen.EffortXS, 0)
	child("h2", halves, apigen.EffortXS, 5)
	assert.Equal(t, 5, get(halves).Progress, "a mean of 2.5 rounds up to 5")

	dropped := e.file(t, member, "ALPHA", task("All dropped"))
	d := child("d", dropped, apigen.EffortM, 50)
	require.Equal(t, http.StatusOK, e.move(t, member, d, apigen.Transition{From: apigen.TicketStateFiled, To: apigen.TicketStateDropped, Reason: ptr("x")}).StatusCode())
	assert.Equal(t, 0, get(dropped).Progress, "every child dropped: 0")

	leave := e.file(t, member, "ALPHA", task("Left alone"))
	only := child("only", leave, apigen.EffortS, 35)
	assert.Equal(t, 35, get(leave).Progress)
	moved := e.patch(t, member, get(only), apigen.TicketPatch{Parent: nullable.NewNullNullable[string]()})
	require.Equal(t, http.StatusOK, moved.StatusCode(), string(moved.Body))
	alone := get(leave)
	assert.False(t, alone.ProgressDerived)
	assert.Equal(t, 35, alone.Progress, "the own value starts at the last derived one")
	assert.Equal(t, http.StatusOK, e.patch(t, member, alone, apigen.TicketPatch{Progress: ptr(40)}).StatusCode(), "editable again")
	assert.Equal(t, []string{"Story"}, e.titles(t, member, e.tenantTickets(), "progress_min=100&include_terminal=true&type=task&parent=ALPHA-"+strconv.Itoa(grand.Number)))
}
