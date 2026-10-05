package api

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/problem"
	"github.com/guided-traffic/cowork/backend/internal/store"
	"github.com/guided-traffic/cowork/backend/internal/store/readq"
)

// The bytes the search query puts around the words a snippet found
// (queries/read/search.sql); the text loses them before the snippet is taken.
const (
	snippetStart = '\x02'
	snippetStop  = '\x03'
)

// keyQuery is a query that is a ticket's key or the beginning of one, in any
// case: COW-1, cow-12, acme/COW-12 (docs/adr/0025 D3).
var keyQuery = regexp.MustCompile(`^(?:([a-z0-9][a-z0-9-]{1,62})/)?([A-Za-z][A-Za-z0-9]{1,9}-[0-9]{1,10})$`)

// keyPrefix is the beginning of a short key the query names in the tenant of
// the slug, upper case; empty when the query is no key, or names another
// tenant.
func keyPrefix(query, slug string) string {
	m := keyQuery.FindStringSubmatch(query)
	if m == nil || (m[1] != "" && m[1] != slug) {
		return ""
	}
	return strings.ToUpper(m[2])
}

// searchQuery checks the words to find: some, and no more characters than
// COWORK_MAX_QUERY_LENGTH (docs/adr/0039 D2).
func (s *Server) searchQuery(q string) (string, error) {
	trimmed := strings.TrimSpace(q)
	switch {
	case trimmed == "":
		return "", &problem.Error{Code: problem.ValidationFailed, Detail: "the search has no word to find",
			Errors: []problem.FieldError{{Pointer: "query:q", Message: "white space only"}}}
	case s.h.opts.MaxQueryLength > 0 && utf8.RuneCountInString(q) > s.h.opts.MaxQueryLength:
		return "", &problem.Error{Code: problem.ValidationFailed, Detail: "the search is too long",
			Errors: []problem.FieldError{{Pointer: "query:q", Message: "longer than " + strconv.Itoa(s.h.opts.MaxQueryLength) + " characters"}}}
	}
	return trimmed, nil
}

// queryScope binds a search's cursor to its query, without carrying the
// query itself.
func queryScope(q string) string {
	sum := sha256.Sum256([]byte(q))
	return hex.EncodeToString(sum[:16])
}

// searchPosition is where a search resumes: after the hit of this rank and
// ticket, in the order of rank, then id, both descending.
type searchPosition struct {
	rank float32
	id   uuid.UUID
}

func (p searchPosition) String() string {
	return strconv.FormatFloat(float64(p.rank), 'g', -1, 32) + "/" + p.id.String()
}

// searchAfter decodes a search's cursor; nil without one.
func (s *Server) searchAfter(op, scope string, cursor *string) (*searchPosition, error) {
	if cursor == nil {
		return nil, nil
	}
	raw, perr := s.cursors.decode(op, scope, *cursor)
	if perr != nil {
		return nil, perr
	}
	rank, id, ok := strings.Cut(raw, "/")
	r, err1 := strconv.ParseFloat(rank, 32)
	parsed, err2 := uuid.Parse(id)
	if !ok || err1 != nil || err2 != nil {
		return nil, invalidCursor()
	}
	return &searchPosition{rank: float32(r), id: parsed}, nil
}

// searchHit is a hit with the tenant it was found in.
type searchHit struct {
	tenant personTenant
	row    readq.SearchTicketsRow
}

func (h searchHit) position() searchPosition { return searchPosition{rank: h.row.Rank, id: h.row.ID} }

// searchIn reads a tenant's hits after the position, one more than the page
// (limitArg), under the tenant's predicates (docs/adr/0025 D1,
// docs/adr/0065 D5).
func searchIn(ctx context.Context, r *store.Reader, t personTenant, q string, after *searchPosition, size int) ([]searchHit, error) {
	params := readq.SearchTicketsParams{TenantID: t.id, Query: q, KeyPrefix: keyPrefix(q, t.slug), PageSize: limitArg(size)}
	if after != nil {
		params.AfterRank, params.AfterID = &after.rank, &after.id
	}
	rows, err := r.SearchTickets(ctx, params)
	if err != nil {
		return nil, err
	}
	hits := make([]searchHit, 0, len(rows))
	for _, row := range rows {
		hits = append(hits, searchHit{tenant: t, row: row})
	}
	return hits, nil
}

// searchPage cuts the merged hits to a page, the best first: by rank, then by
// id, both descending — the order of every tenant's part.
func (s *Server) searchPage(hits []searchHit, size int, op, scope string) ([]searchHit, *string) {
	slices.SortFunc(hits, func(a, b searchHit) int {
		if c := cmp.Compare(b.row.Rank, a.row.Rank); c != 0 {
			return c
		}
		return bytes.Compare(b.row.ID[:], a.row.ID[:])
	})
	return page(s.h, hits, size, op, scope, func(h searchHit) string { return h.position().String() })
}

// SearchTenant searches the tenant's tickets (docs/adr/0025 D3–D5): ranked
// hits with snippets, under the visibility predicates.
func (s *Server) SearchTenant(ctx context.Context, req apigen.SearchTenantRequestObject) (apigen.SearchTenantResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, read); perr != nil {
		return nil, perr
	}
	q, err := s.searchQuery(req.Params.Q)
	if err != nil {
		return nil, err
	}
	const op = "searchTenant"
	scope := t.ID.String() + "/" + queryScope(q)
	after, err := s.searchAfter(op, scope, req.Params.Cursor)
	if err != nil {
		return nil, err
	}
	size := s.h.pageSize(req.Params.Limit)
	tenant := personTenant{id: t.ID, slug: t.Slug, name: t.Name, role: t.Role}
	var hits []searchHit
	err = s.db.InTenant(ctx, t.ID, func(r *store.Reader) error {
		var err error
		hits, err = searchIn(ctx, r, tenant, q, after, size)
		return err
	})
	if err != nil {
		return nil, err
	}
	hits, next := s.searchPage(hits, size, op, scope)
	return apigen.SearchTenant200JSONResponse(searchList(hits, next)), nil
}

// SearchMyTenants searches every tenant of the person (docs/adr/0023 D2): one
// read per tenant (docs/adr/0021 D5), merged by rank.
func (s *Server) SearchMyTenants(ctx context.Context, req apigen.SearchMyTenantsRequestObject) (apigen.SearchMyTenantsResponseObject, error) {
	p := principal(ctx)
	q, err := s.searchQuery(req.Params.Q)
	if err != nil {
		return nil, err
	}
	const op = "searchMyTenants"
	scope := p.PersonID.String() + "/" + deref(req.Params.Tenant) + "/" + queryScope(q)
	after, err := s.searchAfter(op, scope, req.Params.Cursor)
	if err != nil {
		return nil, err
	}
	tenants, err := s.h.personTenants(ctx, req.Params.Tenant)
	if err != nil {
		return nil, err
	}
	size := s.h.pageSize(req.Params.Limit)
	var hits []searchHit
	for _, t := range tenants {
		err := s.db.InTenant(ctx, t.id, func(r *store.Reader) error {
			found, err := searchIn(ctx, r, t, q, after, size)
			hits = append(hits, found...)
			return err
		})
		if err != nil {
			return nil, err
		}
	}
	hits, next := s.searchPage(hits, size, op, scope)
	return apigen.SearchMyTenants200JSONResponse(searchList(hits, next)), nil
}

func searchList(hits []searchHit, next *string) apigen.SearchHitList {
	out := apigen.SearchHitList{Items: make([]apigen.SearchHit, 0, len(hits)), NextCursor: nullableString(next)}
	for _, h := range hits {
		out.Items = append(out.Items, searchHitView(h))
	}
	return out
}

// searchHitView is a hit as the API shows it (docs/adr/0025 D5): the ticket,
// where it matched, and the snippet in parts.
func searchHitView(h searchHit) apigen.SearchHit {
	row := h.row
	v := apigen.SearchHit{
		Tenant: h.tenant.ref(), Key: domain.FullKey(h.tenant.slug, row.ProjectKey, row.Number), Title: row.Title,
		Type: apigen.TicketType(row.Type), State: apigen.TicketState(row.State), FoundIn: apigen.SearchFoundIn(row.FoundIn),
		Comment: nullableOf[uuid.UUID](nil), Question: nullableOf[int](nil), Snippet: snippetParts(row.Snippet),
	}
	switch v.FoundIn {
	case apigen.SearchFoundInComment:
		v.Comment = nullableOf(row.SourceID)
	case apigen.SearchFoundInQuestion:
		if row.QuestionNumber != nil {
			n := int(*row.QuestionNumber)
			v.Question = nullableOf(&n)
		}
	}
	return v
}

// snippetParts cuts a snippet at the bytes around the words found into
// pieces of text, each marked whether it is a found word; text, never markup.
func snippetParts(snippet string) []apigen.SnippetPart {
	parts := []apigen.SnippetPart{}
	var current strings.Builder
	match := false
	flush := func() {
		if current.Len() > 0 {
			parts = append(parts, apigen.SnippetPart{Text: current.String(), Match: match})
			current.Reset()
		}
	}
	for _, r := range strings.TrimSpace(snippet) {
		switch r {
		case snippetStart:
			flush()
			match = true
		case snippetStop:
			flush()
			match = false
		default:
			current.WriteRune(r)
		}
	}
	flush()
	return parts
}
