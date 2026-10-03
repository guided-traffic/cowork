package store

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// queryBlocks splits a query file into its named queries.
func queryBlocks(t *testing.T, path string) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	out := map[string]string{}
	parts := regexp.MustCompile(`(?m)^-- name: (\w+) :\w+`).Split(string(raw), -1)
	names := regexp.MustCompile(`(?m)^-- name: (\w+) :\w+`).FindAllStringSubmatch(string(raw), -1)
	for i, n := range names {
		out[n[1]] = parts[i+1]
	}
	return out
}

func normalise(s string) string { return strings.Join(strings.Fields(s), " ") }

// The list builder selects exactly the columns of GetTicketByNumber, so a list
// row and a single ticket are one type and scan by position.
func TestTicketListSelectsWhatTheQueriesSelect(t *testing.T) {
	q := queryBlocks(t, "queries/read/tickets.sql")["GetTicketByNumber"]
	require.NotEmpty(t, q)
	where := strings.Index(q, "WHERE")
	require.Positive(t, where)
	assert.Equal(t, normalise(ticketSelect+" "+ticketFrom), normalise(q[:where]))
}

// Every query that reads a ticket calls the visibility predicate once per
// ticket it reads, and one that reads a project without a ticket calls the
// project's, or it says why not (docs/adr/0034 D4, docs/adr/0065 D4): the
// restriction and the confidential flag are one predicate in the data layer,
// never a call site's to remember. A predicate on a joined ticket does not
// stand in for the one on the ticket the query reads.
func TestEveryReadOfProjectsAndTicketsCarriesTheVisibilityPredicate(t *testing.T) {
	files, err := filepath.Glob("queries/*/*.sql")
	require.NoError(t, err)
	require.NotEmpty(t, files)
	tickets := regexp.MustCompile(`\b(FROM|JOIN)\s+tickets\b`)
	projects := regexp.MustCompile(`\b(FROM|JOIN)\s+projects\b`)
	for _, f := range files {
		for name, q := range queryBlocks(t, f) {
			if strings.Contains(q, "-- visibility: exempt") {
				continue
			}
			if n := len(tickets.FindAllString(q, -1)); n > 0 {
				assert.GreaterOrEqual(t, strings.Count(q, "app_ticket_visible("), n,
					"%s in %s reads a ticket without the visibility predicate", name, f)
				continue
			}
			if projects.MatchString(q) {
				assert.Contains(t, q, "app_project_visible(", "%s in %s reads a project without the visibility predicate", name, f)
			}
		}
	}
}
