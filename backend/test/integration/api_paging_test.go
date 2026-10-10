//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api"
	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/test/fixture"
)

// numberedPage is what a numbered page of any list answers besides its items.
type numberedPage struct {
	Total      *int    `json:"total"`
	Page       *int    `json:"page"`
	PerPage    *int    `json:"per_page"`
	NextCursor *string `json:"next_cursor"`
}

// docs/adr/0048 D2: the audit view, the members, the person's tokens and the
// projects are tables — besides the cursor they take page and per_page,
// answered with the total, which counts what the caller may see and the
// filters select; the two modes do not mix, and a numbered page ends at row
// 10 000.
func TestTheTablesTakeNumberedPages(t *testing.T) {
	w := newWorld(t)
	tok := issueTokens(t, w)
	// per_page is clamped like limit: two rows a page show the paging with a few rows.
	s := newAPI(t, func(o *api.Options) { o.MaxPageSize = 2 })
	admin, member := caller{Token: tok.AdminA}, caller{Token: tok.MemberA}
	tenant := "/api/v1/teams/" + w.SlugA

	for _, key := range []string{"CHARLIE", "DELTA", "ECHO"} {
		res := s.do(t, admin, http.MethodPost, tenant+"/projects", map[string]any{"key": key, "name": key})
		require.Equal(t, http.StatusCreated, res.StatusCode)
	}
	require.NoError(t, fixtures(t).Exec(context.Background(), "UPDATE projects SET restricted = true WHERE tenant_id = $1 AND key = 'ECHO'", w.A))

	read := func(c caller, path string) (numberedPage, []string) {
		t.Helper()
		res := s.do(t, c, http.MethodGet, path, nil)
		require.Equal(t, http.StatusOK, res.StatusCode, path)
		body, err := io.ReadAll(res.Body)
		require.NoError(t, err)
		var numbers numberedPage
		require.NoError(t, json.Unmarshal(body, &numbers))
		var items struct {
			Items []map[string]any `json:"items"`
		}
		require.NoError(t, json.Unmarshal(body, &items))
		// One name per item: a project's key, a token's name, an act's action; "" for a member.
		names := make([]string, 0, len(items.Items))
		for _, item := range items.Items {
			name := ""
			for _, field := range []string{"key", "name", "action"} {
				if v, ok := item[field].(string); ok {
					name = v
					break
				}
			}
			names = append(names, name)
		}
		return numbers, names
	}

	t.Run("projects", func(t *testing.T) {
		first, keys := read(admin, tenant+"/projects?page=1&per_page=25")
		assert.Equal(t, 4, *first.Total, "ALPHA and the three new ones")
		assert.Equal(t, 1, *first.Page)
		assert.Equal(t, 2, *first.PerPage, "clamped like limit")
		assert.Nil(t, first.NextCursor)
		assert.Equal(t, []string{"ALPHA", "CHARLIE"}, keys)
		_, keys = read(admin, tenant+"/projects?page=2&per_page=25")
		assert.Equal(t, []string{"DELTA", "ECHO"}, keys)

		seen, keys := read(member, tenant+"/projects?page=2&per_page=25")
		assert.Equal(t, 3, *seen.Total, "a restricted project counts for whoever sees it only")
		assert.Equal(t, []string{"DELTA"}, keys)

		cursor, _ := read(admin, tenant+"/projects?limit=2")
		assert.Nil(t, cursor.Total, "a cursor page carries no total")
		assert.NotNil(t, cursor.NextCursor)
	})

	t.Run("the audit view", func(t *testing.T) {
		first, actions := read(admin, tenant+"/audit?entity_type=project&page=1&per_page=25")
		assert.Equal(t, 3, *first.Total, "the filter decides what counts")
		assert.Equal(t, []string{"created", "created"}, actions)
		last, actions := read(admin, tenant+"/audit?entity_type=project&page=2&per_page=25")
		assert.Equal(t, 3, *last.Total)
		assert.Len(t, actions, 1)

		csv := s.do(t, admin, http.MethodGet, tenant+"/audit?entity_type=project&page=2&per_page=25", nil, "Accept", "text/csv")
		require.Equal(t, http.StatusOK, csv.StatusCode)
		body, err := io.ReadAll(csv.Body)
		require.NoError(t, err)
		assert.Len(t, strings.Split(strings.TrimSpace(string(body)), "\n"), 2, "the header and the one row of the page")
		assertProblem(t, s.do(t, member, http.MethodGet, tenant+"/audit?page=1", nil), http.StatusForbidden, "forbidden")
	})

	t.Run("members", func(t *testing.T) {
		first, _ := read(member, tenant+"/members?page=1&per_page=25")
		assert.Equal(t, 4, *first.Total, "admin, member, viewer and the person of both tenants")
		_, second := read(member, tenant+"/members?page=2&per_page=25")
		assert.Len(t, second, 2)
		_, third := read(member, tenant+"/members?page=3&per_page=25")
		assert.Empty(t, third, "past the end")
	})

	t.Run("tokens", func(t *testing.T) {
		for i := range 2 {
			_, _, err := fixtures(t).Token(context.Background(), fixture.TokenSpec{UserID: w.MemberA, Name: fmt.Sprintf("extra-%d", i)})
			require.NoError(t, err)
		}
		first, names := read(member, "/api/v1/me/tokens?page=1&per_page=25")
		assert.Equal(t, 5, *first.Total, "the member's three of issueTokens and two more")
		assert.Equal(t, []string{"extra-1", "extra-0"}, names, "newest first")

		narrow, _, err := fixtures(t).Token(context.Background(), fixture.TokenSpec{UserID: w.MemberA, TenantID: w.A})
		require.NoError(t, err)
		own, _ := read(caller{Token: narrow}, "/api/v1/me/tokens?page=1&per_page=25")
		assert.Equal(t, 1, *own.Total, "a restricted token counts itself only")
	})

	t.Run("refusals", func(t *testing.T) {
		for _, path := range []string{tenant + "/projects", tenant + "/audit", tenant + "/members", "/api/v1/me/tokens"} {
			for _, q := range []string{"page=1&cursor=abc", "page=1&limit=10", "per_page=25"} {
				assertProblem(t, s.do(t, admin, http.MethodGet, path+"?"+q, nil), http.StatusBadRequest, "validation_failed")
			}
			// 2 rows a page: row 10 000 is on page 5000.
			assertProblem(t, s.do(t, admin, http.MethodGet, path+"?page=5001&per_page=25", nil), http.StatusBadRequest, "page_too_deep")
			assert.Equal(t, http.StatusOK, s.do(t, admin, http.MethodGet, path+"?page=5000&per_page=25", nil).StatusCode)
		}
	})

	t.Run("the generated client", func(t *testing.T) {
		res, err := s.client(t, admin).ListAuditWithResponse(context.Background(), w.SlugA,
			&apigen.ListAuditParams{Page: ptr(1), PerPage: ptr(apigen.ListAuditParamsPerPage(25))})
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
		assert.Equal(t, 1, *res.JSON200.Page)
		assert.Positive(t, *res.JSON200.Total)
	})
}
