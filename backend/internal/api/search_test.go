package api

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
)

// docs/adr/0025 D3: a query that is a key, or the beginning of one, names a
// key prefix in its tenant; one that names another tenant, or is no key,
// names none, and nothing a LIKE would read as a wildcard ever passes.
func TestKeyPrefix(t *testing.T) {
	for q, want := range map[string]string{
		"COW-12":      "COW-12",
		"cow-1":       "COW-1",
		"acme/COW-12": "COW-12",
		"other/COW-1": "",
		"COW-":        "",
		"COW":         "",
		"COW-1%":      "",
		"COW_1":       "",
		"COW-12 gate": "",
		"1COW-2":      "",
	} {
		assert.Equal(t, want, keyPrefix(q, "acme"), q)
	}
}

// docs/adr/0025 D5: a snippet is cut into pieces of text at the bytes the
// query puts around the words found; nothing of it is markup.
func TestSnippetParts(t *testing.T) {
	assert.Equal(t, []apigen.SnippetPart{
		{Text: "the ", Match: false}, {Text: "gate", Match: true}, {Text: " fails on ", Match: false},
		{Text: "<b>gate</b>", Match: true},
	}, snippetParts("  the \x02gate\x03 fails on \x02<b>gate</b>\x03 "))
	assert.Equal(t, []apigen.SnippetPart{{Text: "Steps.", Match: false}}, snippetParts("Steps."))
	assert.Empty(t, snippetParts(""))
	assert.Empty(t, snippetParts("\x02\x03"))
}

// docs/adr/0048 D1: a search's cursor carries its rank exactly, so the next
// page resumes where the last ended.
func TestSearchPositionRoundTrips(t *testing.T) {
	s := &Server{cursors: newCursorCodec(make([]byte, 32))}
	id := uuid.Must(uuid.NewV7())
	for _, rank := range []float32{0.6079271, 0.0607927, 4, 0.03333333} {
		p := searchPosition{rank: rank, id: id}
		cursor := s.cursors.encode("searchTenant", "scope", p.String())
		got, err := s.searchAfter("searchTenant", "scope", &cursor)
		require.NoError(t, err)
		assert.Equal(t, p, *got)
	}
	cursor := s.cursors.encode("searchTenant", "scope", "not a position")
	_, err := s.searchAfter("searchTenant", "scope", &cursor)
	assert.Error(t, err)
	_, err = s.searchAfter("searchTenant", "another query", &cursor)
	assert.Error(t, err)
}
