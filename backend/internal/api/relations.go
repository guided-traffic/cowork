package api

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/problem"
	"github.com/guided-traffic/cowork/backend/internal/store"
)

// relationKinds are the kinds of relation in the order a ticket's relations
// are listed: the parent, the children, the links.
var relationKinds = []string{store.RelationParent, store.RelationChild, store.RelationLink}

// relationView is a relation as the API shows it: its kind, its link, and the
// ticket at its other end as the caller sees it (docs/adr/0005 D3).
func relationView(r store.Relation) apigen.Relation {
	v := apigen.Relation{Kind: apigen.RelationKind(r.Kind), Head: headView(r.Head), Link: nullableOf[apigen.RelationLink](nil)}
	if l := r.Link; l != nil {
		link := apigen.RelationLink{Id: l.ID, Type: apigen.LinkType(l.Type), Direction: apigen.RelationLinkDirection(direction(l.Outgoing)),
			Name: l.Type.Name(l.Outgoing), CreatedBy: personView(l.CreatedBy.ID, l.CreatedBy.Username, l.CreatedBy.Name),
			CreatedAt: l.CreatedAt}
		v.Link = nullableOf(&link)
	}
	return v
}

// ListTicketRelations answers a ticket's parent, children and links, of any
// project or team, each other end as the caller sees it: readable, by its head,
// or as the placeholder (docs/adr/0005 D3, docs/adr/0008 D2, docs/adr/0012 D2,
// docs/adr/0034 D4, docs/adr/0065 D5). The cursor's position names a ticket or
// a link by its id, so it is sealed: no answer shows a ticket's id.
func (s *Server) ListTicketRelations(ctx context.Context, req apigen.ListTicketRelationsRequestObject) (apigen.ListTicketRelationsResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, read); perr != nil {
		return nil, perr
	}
	kinds := relationKinds
	if req.Params.Kind != nil && len(*req.Params.Kind) > 0 {
		kinds = nil
		for _, k := range relationKinds {
			if slices.Contains(*req.Params.Kind, k) {
				kinds = append(kinds, k)
			}
		}
	}
	const op = "listTicketRelations"
	scope := fmt.Sprintf("%s/%s/%d/%s", t.ID, req.Project, req.Number, strings.Join(kinds, ","))
	size := s.h.pageSize(req.Params.Limit)
	var after string
	if req.Params.Cursor != nil {
		sealed, perr := s.cursors.decode(op, scope, *req.Params.Cursor)
		if perr != nil {
			return nil, perr
		}
		position, ok := s.cursors.openPosition(sealed)
		if !ok {
			return nil, invalidCursor()
		}
		after = position
	}
	var rels []store.Relation
	err := s.db.InTenant(ctx, t.ID, func(r *store.Reader) error {
		tc, err := visibleTicket(ctx, r, t, req.Project, req.Number)
		if err != nil {
			return err
		}
		rels, err = r.RelationHeads(ctx, []uuid.UUID{tc.row.ID}, kinds...)
		return err
	})
	if err != nil {
		return nil, err
	}
	slices.SortFunc(rels, func(a, b store.Relation) int {
		if c := relationPosition(a) - relationPosition(b); c != 0 {
			return c
		}
		return bytes.Compare(a.Position[:], b.Position[:])
	})
	if after != "" {
		start, perr := relationsAfter(rels, after)
		if perr != nil {
			return nil, perr
		}
		rels = rels[start:]
	}
	rows, next := page(s.h, rels, size, op, scope, func(r store.Relation) string {
		return s.cursors.sealPosition(strconv.Itoa(relationPosition(r)) + "/" + r.Position.String())
	})
	out := apigen.RelationList{Items: []apigen.Relation{}, NextCursor: nullableString(next)}
	for _, r := range rows {
		out.Items = append(out.Items, relationView(r))
	}
	tag, unchanged := listTag(req.Params.IfNoneMatch, out)
	if unchanged {
		return apigen.ListTicketRelations304Response{Headers: apigen.NotModifiedResponseHeaders{ETag: &tag}}, nil
	}
	return apigen.ListTicketRelations200JSONResponse{Body: out, Headers: apigen.ListTicketRelations200ResponseHeaders{ETag: &tag}}, nil
}

// relationPosition is a relation's kind in the list's order.
func relationPosition(r store.Relation) int { return slices.Index(relationKinds, r.Kind) }

// relationsAfter is the index of the first relation after a cursor's
// position, "<kind>/<id>".
func relationsAfter(rels []store.Relation, position string) (int, *problem.Error) {
	kind, id, ok := strings.Cut(position, "/")
	k, err := strconv.Atoi(kind)
	if !ok || err != nil {
		return 0, invalidCursor()
	}
	at, err := uuid.Parse(id)
	if err != nil {
		return 0, invalidCursor()
	}
	for i, r := range rels {
		p := relationPosition(r)
		if p > k || (p == k && bytes.Compare(r.Position[:], at[:]) > 0) {
			return i, nil
		}
	}
	return len(rels), nil
}
