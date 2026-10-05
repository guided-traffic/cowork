//go:build integration

package integration

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// docs/adr/0054 D7: every list the polling fallback reloads answers a weak
// ETag, and 304 without a body to an If-None-Match that names it, so a poll
// that finds nothing new moves no list; a change of the list is a new tag.
func TestThePolledListsAnswerNotModified(t *testing.T) {
	w := newWorld(t)
	tok := issueTokens(t, w)
	s := newAPI(t)
	admin := caller{Token: tok.AdminA}
	_, number, err := fixtures(t).Ticket(context.Background(), w.A, w.ProjectA, w.AdminA, "polled")
	require.NoError(t, err)
	tenant := "/api/v1/tenants/" + w.SlugA
	ticket := tenant + "/projects/ALPHA/tickets/" + strconv.Itoa(number)

	lists := map[string]string{
		"listProjects":       tenant + "/projects",
		"listMembers":        tenant + "/members",
		"listGroupMappings":  tenant + "/group-mappings",
		"listProjectAccess":  tenant + "/projects/ALPHA/access",
		"listComments":       ticket + "/comments",
		"listActivity":       ticket + "/activity",
		"listQuestions":      ticket + "/questions",
		"listTicketLinks":    ticket + "/links",
		"listInterest":       ticket + "/interest",
		"listAttachments":    ticket + "/attachments",
		"listTicketTime":     ticket + "/time-entries",
		"listPrerequisites":  ticket + "/prerequisites",
		"listMyInbox":        "/api/v1/me/inbox",
		"listMyNext":         "/api/v1/me/next",
		"listMyAssigned":     "/api/v1/me/assigned",
		"listMyDecisions":    "/api/v1/me/decisions",
		"listDeletedTickets": tenant + "/deleted-tickets",
		"listSavedFilters":   tenant + "/filters",
	}
	for op, path := range lists {
		t.Run(op, func(t *testing.T) {
			first := s.do(t, admin, http.MethodGet, path, nil)
			require.Equal(t, http.StatusOK, first.StatusCode)
			tag := first.Header.Get("ETag")
			require.True(t, strings.HasPrefix(tag, `W/"`), "a weak ETag: %q", tag)

			same := s.do(t, admin, http.MethodGet, path, nil, "If-None-Match", tag)
			assert.Equal(t, http.StatusNotModified, same.StatusCode)
			assert.Equal(t, tag, same.Header.Get("ETag"))
			body, err := io.ReadAll(same.Body)
			require.NoError(t, err)
			assert.Empty(t, body, "a 304 has no body")

			other := s.do(t, admin, http.MethodGet, path, nil, "If-None-Match", `W/"000000000000000000000000"`)
			assert.Equal(t, http.StatusOK, other.StatusCode, "another tag is answered in full")
		})
	}

	// A change of the list is another page and another tag.
	before := s.do(t, admin, http.MethodGet, lists["listComments"], nil).Header.Get("ETag")
	res := s.do(t, admin, http.MethodPost, lists["listComments"], map[string]any{"body": "a new comment"})
	require.Equal(t, http.StatusCreated, res.StatusCode)
	after := s.do(t, admin, http.MethodGet, lists["listComments"], nil, "If-None-Match", before)
	assert.Equal(t, http.StatusOK, after.StatusCode)
	assert.NotEqual(t, before, after.Header.Get("ETag"))

	// The tag is the caller's page: a member reads the members without their addresses.
	require.NoError(t, fixtures(t).Exec(context.Background(), "UPDATE users SET email = 'viewer@example.test' WHERE id = $1", w.ViewerA))
	adminTag := s.do(t, admin, http.MethodGet, lists["listMembers"], nil).Header.Get("ETag")
	member := s.do(t, caller{Token: tok.MemberA}, http.MethodGet, lists["listMembers"], nil, "If-None-Match", adminTag)
	assert.Equal(t, http.StatusOK, member.StatusCode, "another caller's tag is not this caller's page")
}
