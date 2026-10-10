//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
)

func day(s string) openapi_types.Date {
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return openapi_types.Date{Time: d}
}

func (e ticketEnv) book(t *testing.T, c caller, tk apigen.Ticket, minutes int, d string) *apigen.BookTimeResponse {
	t.Helper()
	res, err := e.s.client(t, c).BookTimeWithResponse(e.ctx, e.SlugA, tk.Project, tk.Number, &apigen.BookTimeParams{},
		apigen.TimeEntryCreate{Minutes: minutes, Day: day(d), Note: ptr("work")})
	require.NoError(t, err)
	return res
}

func (e ticketEnv) entryPath(tk apigen.Ticket, id uuid.UUID) string {
	return fmt.Sprintf("%s/%d/time-entries/%s", e.projectTickets(tk.Project), tk.Number, id)
}

// ticketMinutes is the visible total of a ticket's entries as c.
func (e ticketEnv) ticketMinutes(t *testing.T, c caller, tk apigen.Ticket) (int, int) {
	t.Helper()
	res, err := e.s.client(t, c).ListTicketTimeWithResponse(e.ctx, e.SlugA, tk.Project, tk.Number, &apigen.ListTicketTimeParams{})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	return *res.JSON200.TotalMinutes, len(res.JSON200.Items)
}

// docs/adr/0017 D6–D8: people book minutes, correct with a kept history,
// void instead of delete; agents never book; a closed period refuses.
func TestBookingTime(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	member := caller{Token: e.tk.MemberA}
	tk := e.file(t, member, "ALPHA", task("Billable"))

	res := e.book(t, member, tk, 90, "2026-09-01")
	require.Equal(t, http.StatusCreated, res.StatusCode(), string(res.Body))
	entry := *res.JSON201
	assert.Equal(t, e.MemberA, entry.Person.Id)
	assert.Equal(t, tk.Key, entry.Ticket)
	require.Equal(t, http.StatusCreated, e.book(t, member, tk, 30, "2026-09-02").StatusCode())
	zero := e.s.do(t, member, http.MethodPost, fmt.Sprintf("%s/%d/time-entries", e.projectTickets("ALPHA"), tk.Number),
		map[string]any{"minutes": 0, "day": "2026-09-01"})
	assertProblem(t, zero, http.StatusBadRequest, "validation_failed")

	agent := e.book(t, caller{Token: e.tk.AgentA, Agent: "claude-code/opus/s1"}, tk, 10, "2026-09-03")
	require.Equal(t, http.StatusForbidden, agent.StatusCode())
	assert.Equal(t, "hard-off: booking time", *agent.ApplicationproblemJSONDefault.Detail)
	assert.Equal(t, http.StatusForbidden, e.book(t, caller{Token: e.tk.ViewerA}, tk, 10, "2026-09-03").StatusCode())

	etag := fmt.Sprintf(`"%d"`, entry.Version)
	edited, err := e.s.client(t, member).EditTimeEntryWithResponse(e.ctx, e.SlugA, "ALPHA", tk.Number, entry.Id,
		&apigen.EditTimeEntryParams{IfMatch: &etag}, apigen.TimeEntryPatch{Minutes: ptr(120)})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, edited.StatusCode(), string(edited.Body))
	assert.True(t, edited.JSON200.Edited)
	revs, err := e.s.client(t, member).ListTimeEntryRevisionsWithResponse(e.ctx, e.SlugA, "ALPHA", tk.Number, entry.Id, &apigen.ListTimeEntryRevisionsParams{})
	require.NoError(t, err)
	require.Len(t, revs.JSON200.Items, 1)
	assert.Equal(t, 90, revs.JSON200.Items[0].Minutes)
	total, _ := e.ticketMinutes(t, member, tk)
	assert.Equal(t, 150, total)

	other := e.s.do(t, caller{Token: e.tk.AdminA}, http.MethodPut, e.entryPath(tk, entry.Id)+"/void", nil)
	assert.Equal(t, http.StatusForbidden, other.StatusCode, "only the author voids")
	voided := e.s.do(t, member, http.MethodPut, e.entryPath(tk, entry.Id)+"/void", nil)
	require.Equal(t, http.StatusOK, voided.StatusCode)
	assert.Equal(t, http.StatusOK, e.s.do(t, member, http.MethodPut, e.entryPath(tk, entry.Id)+"/void", nil).StatusCode, "idempotent")
	total, n := e.ticketMinutes(t, member, tk)
	assert.Equal(t, 30, total, "a voided entry leaves every sum")
	assert.Equal(t, 2, n, "and stays listed")
	assert.Equal(t, http.StatusMethodNotAllowed, e.s.do(t, member, http.MethodDelete, e.entryPath(tk, entry.Id), nil).StatusCode, "no delete route")
	gone := e.s.do(t, member, http.MethodPatch, e.entryPath(tk, entry.Id), map[string]any{"minutes": 5}, "If-Match", `"3"`)
	assertProblem(t, gone, http.StatusConflict, "state_conflict")

	tenant, err := e.s.client(t, caller{Token: e.tk.AdminA}).GetTeamWithResponse(e.ctx, e.SlugA)
	require.NoError(t, err)
	tetag := tenant.HTTPResponse.Header.Get("ETag")
	lock := e.s.do(t, caller{Token: e.tk.AdminA}, http.MethodPatch, "/api/v1/teams/"+e.SlugA, map[string]any{"time_locked_until": "2026-09-01"}, "If-Match", tetag)
	require.Equal(t, http.StatusOK, lock.StatusCode)
	locked := e.book(t, member, tk, 10, "2026-09-01")
	require.Equal(t, http.StatusConflict, locked.StatusCode())
	assert.Equal(t, "period_locked", string(locked.ApplicationproblemJSONDefault.Code))
	require.Equal(t, http.StatusCreated, e.book(t, member, tk, 10, "2026-09-02").StatusCode())
	second := e.book(t, member, tk, 15, "2026-09-05")
	require.Equal(t, http.StatusCreated, second.StatusCode())
	back := e.s.do(t, member, http.MethodPatch, e.entryPath(tk, second.JSON201.Id), map[string]any{"day": "2026-08-31"}, "If-Match", `"1"`)
	assertProblem(t, back, http.StatusConflict, "period_locked")
	acts, err := f.QueryCount(e.ctx, "SELECT count(*) FROM audit_events WHERE tenant_id = $1 AND entity_type = 'tenant'", e.A)
	require.NoError(t, err)
	assert.Positive(t, acts, "moving the lock is recorded")

	list, raw := e.activity(t, member, tk)
	assert.NotContains(t, raw, "time_entry")
	assert.Len(t, list.Items, 1, "the activity shows no time acts")
}

// docs/adr/0034 D5: own entries, all as administrator, the members' while the
// tenant shows time to members; a confidential ticket's entries follow it.
func TestTimeVisibility(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	member, both, admin := caller{Token: e.tk.MemberA}, caller{Token: e.tk.Both}, caller{Token: e.tk.AdminA}
	tk := e.file(t, member, "ALPHA", task("Shared"))
	require.Equal(t, http.StatusCreated, e.book(t, member, tk, 60, "2026-09-10").StatusCode())
	require.Equal(t, http.StatusCreated, e.book(t, both, tk, 30, "2026-09-11").StatusCode())

	sum := func(c caller) int { s, _ := e.ticketMinutes(t, c, tk); return s }
	assert.Equal(t, 60, sum(member), "own only")
	assert.Equal(t, 30, sum(both))
	assert.Equal(t, 90, sum(admin), "an administrator sees all")
	require.NoError(t, f.Exec(e.ctx, "UPDATE tenants SET time_visible_to_members = true WHERE id = $1", e.A))
	assert.Equal(t, 90, sum(member), "members see all while the tenant says so")
	assert.Equal(t, 0, sum(caller{Token: e.tk.ViewerA}), "a viewer is no member")

	secret := e.file(t, member, "ALPHA", task("Secret", func(b *apigen.TicketCreate) {
		b.Security, b.Threat = apigen.SecurityClassLive, ptr("leak")
	}))
	require.Equal(t, http.StatusCreated, e.book(t, member, secret, 45, "2026-09-12").StatusCode())
	report := func(c caller, query string) apigen.TimeReport {
		res := e.s.do(t, c, http.MethodGet, "/api/v1/teams/"+e.SlugA+"/time-report?"+query, nil)
		require.Equal(t, http.StatusOK, res.StatusCode)
		var r apigen.TimeReport
		require.NoError(t, json.NewDecoder(res.Body).Decode(&r))
		return r
	}
	assert.Equal(t, 90, report(both, "group_by=tenant").TotalMinutes, "a confidential ticket's time follows the ticket")
	assert.Equal(t, 135, report(admin, "group_by=tenant").TotalMinutes)
	byPerson := report(admin, "group_by=person")
	assert.Len(t, byPerson.Items, 2)
	byTicket := report(admin, "group_by=ticket&from=2026-09-11&to=2026-09-30")
	require.Len(t, byTicket.Items, 2)
	assert.Equal(t, tk.Key, byTicket.Items[0].Key)
	assert.Equal(t, 30, byTicket.Items[0].Minutes)
	assert.Equal(t, 75, report(admin, "group_by=project&project=ALPHA&from=2026-09-11").TotalMinutes)
	assert.Equal(t, 105, report(admin, "group_by=tenant&person="+e.MemberA.String()).TotalMinutes)

	csvRes := e.s.do(t, admin, http.MethodGet, "/api/v1/teams/"+e.SlugA+"/time-entries", nil, "Accept", "text/csv")
	require.Equal(t, http.StatusOK, csvRes.StatusCode)
	raw, err := io.ReadAll(csvRes.Body)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	assert.Len(t, lines, 4, "a header and three entries")
	assert.True(t, strings.HasPrefix(lines[0], "id,ticket,day,minutes"))
	assert.Contains(t, string(raw), ",2026-09-12,45,")

	paged, err := e.s.client(t, admin).ListTeamTimeWithResponse(e.ctx, e.SlugA, &apigen.ListTeamTimeParams{Page: ptr(1), PerPage: ptr(apigen.ListTeamTimeParamsPerPage(25))})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, paged.StatusCode(), string(paged.Body))
	assert.Equal(t, 3, *paged.JSON200.Total)
	mine, err := e.s.client(t, admin).ListTeamTimeWithResponse(e.ctx, e.SlugA, &apigen.ListTeamTimeParams{Person: ptr("me")})
	require.NoError(t, err)
	assert.Empty(t, mine.JSON200.Items)
	assertProblem(t, e.s.do(t, caller{Token: e.tk.MemberB}, http.MethodGet, fmt.Sprintf("%s/%d/time-entries", e.projectTickets("ALPHA"), tk.Number), nil),
		http.StatusNotFound, "not_found")
}
