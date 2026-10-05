package api

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/google/uuid"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/problem"
	"github.com/guided-traffic/cowork/backend/internal/store"
	"github.com/guided-traffic/cowork/backend/internal/store/readq"
)

// treeDepth is how deep the prerequisite tree goes, on its route and in the
// context alike (docs/adr/0012 D6, docs/adr/0044 D2). A cursor carries a
// node's whole path, so the depth also keeps a cursor within the length the
// document allows.
const treeDepth = 8

// treeNode is a node of the prerequisite tree or of its upward reading: the
// rows of the two queries have the same fields.
type treeNode = readq.ListPrerequisitesRow

// ListPrerequisites answers a ticket's prerequisite tree, or read upward its
// dependents (docs/adr/0012 D6), depth first. What the caller cannot see is
// absent, and so is what lies only behind it (docs/adr/0065 D5); open counts
// the open tickets of the whole tree on every page.
func (s *Server) ListPrerequisites(ctx context.Context, req apigen.ListPrerequisitesRequestObject) (apigen.ListPrerequisitesResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, read); perr != nil {
		return nil, perr
	}
	up := req.Params.Direction != nil && *req.Params.Direction == apigen.ListPrerequisitesParamsDirectionUp
	op, scope := treeOp, treeScope(t, req.Project, req.Number, up)
	size := s.h.pageSize(req.Params.Limit)
	var rows []treeNode
	open := 0
	err := s.db.InTenant(ctx, t.ID, func(r *store.Reader) error {
		tc, err := visibleTicket(ctx, r, t, req.Project, req.Number)
		if err != nil {
			return err
		}
		var after []uuid.UUID
		if req.Params.Cursor != nil {
			position, perr := s.cursors.decode(op, scope, *req.Params.Cursor)
			if perr != nil {
				return perr
			}
			if after, perr = decodeTreePath(position); perr != nil {
				return perr
			}
		}
		if rows, err = ticketTree(ctx, r, t.ID, tc.row.ID, up, after, limitArg(size)); err != nil {
			return err
		}
		counted := rows
		if len(rows) == 0 && after != nil {
			// A page past the end of a tree that shrank: the count is the
			// whole tree's still.
			if counted, err = ticketTree(ctx, r, t.ID, tc.row.ID, up, nil, 1); err != nil {
				return err
			}
		}
		if len(counted) > 0 {
			open = int(counted[0].OpenCount)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	rows, next := page(s.h, rows, size, op, scope, func(n treeNode) string { return encodeTreePath(n.Path) })
	out := apigen.PrerequisiteTree{Items: []apigen.PrerequisiteNode{}, NextCursor: nullableString(next), Open: open}
	for _, n := range rows {
		out.Items = append(out.Items, treeNodeView(t.Slug, n))
	}
	tag, unchanged := listTag(req.Params.IfNoneMatch, out)
	if unchanged {
		return apigen.ListPrerequisites304Response{Headers: apigen.NotModifiedResponseHeaders{ETag: &tag}}, nil
	}
	return apigen.ListPrerequisites200JSONResponse{Body: out, Headers: apigen.ListPrerequisites200ResponseHeaders{ETag: &tag}}, nil
}

const treeOp = "listPrerequisites"

// treeScope binds a cursor to its ticket and its direction: a cursor of the
// prerequisites does not page the dependents.
func treeScope(t tenantScope, project string, number int, up bool) string {
	return fmt.Sprintf("%s/%s/%d/up=%t", t.ID, project, number, up)
}

// ticketTree reads a page of the prerequisite tree, or with up of the
// dependents, after the node at the path after (nil: from the start).
func ticketTree(ctx context.Context, r *store.Reader, tenant, ticket uuid.UUID, up bool, after []uuid.UUID, limit int32) ([]treeNode, error) {
	params := readq.ListPrerequisitesParams{TenantID: tenant, TicketID: ticket, MaxDepth: treeDepth, After: after, PageSize: limit}
	if !up {
		return r.ListPrerequisites(ctx, params)
	}
	rows, err := r.ListDependents(ctx, readq.ListDependentsParams(params))
	nodes := make([]treeNode, len(rows))
	for i, row := range rows {
		nodes[i] = treeNode(row)
	}
	return nodes, err
}

func treeNodeView(slug string, n treeNode) apigen.PrerequisiteNode {
	shown := stagesOf(store.TicketRow{Progress: n.Progress, ProgressDerived: n.ProgressDerived,
		ProgressRefinement: n.ProgressRefinement, ProgressRefinementDerived: n.ProgressRefinementDerived,
		ProgressReview: n.ProgressReview, ProgressReviewDerived: n.ProgressReviewDerived})
	v := apigen.PrerequisiteNode{Key: domain.FullKey(slug, n.ProjectKey, n.Number), Title: n.Title,
		State: apigen.TicketState(n.State), BlockedFrom: nullableOf[apigen.TicketState](nil),
		Assignee: nullableOf[apigen.Person](nil), Progress: shown.Implementation, ProgressRefinement: shown.Refinement,
		ProgressReview: shown.Review, ProgressDerived: n.ProgressDerived != nil, Depth: int(n.Depth),
		Settled: n.State.Terminal(), Repeated: n.Repeated}
	if n.BlockedFrom != nil {
		from := apigen.TicketState(*n.BlockedFrom)
		v.BlockedFrom = nullableOf(&from)
	}
	if n.AssigneeID != nil {
		a := personView(*n.AssigneeID, n.AssigneeUsername, n.AssigneeName)
		v.Assignee = nullableOf(&a)
	}
	return v
}

// encodeTreePath writes a node's path, the ids from the tree's first level
// down to the node, as one string: sixteen bytes each, base64url — short
// enough that the cursor of a node at the deepest level fits the document's
// limit (TestATreeCursorFitsTheDocument).
func encodeTreePath(path []uuid.UUID) string {
	raw := make([]byte, 0, len(path)*len(uuid.UUID{}))
	for _, id := range path {
		raw = append(raw, id[:]...)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

// decodeTreePath reads what encodeTreePath wrote; anything else is a cursor
// that does not belong to the tree.
func decodeTreePath(s string) ([]uuid.UUID, *problem.Error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	size := len(uuid.UUID{})
	if err != nil || len(raw) == 0 || len(raw)%size != 0 || len(raw) > treeDepth*size {
		return nil, problem.New(problem.InvalidCursor, "the cursor does not belong to this list")
	}
	path := make([]uuid.UUID, 0, len(raw)/size)
	for i := 0; i < len(raw); i += size {
		path = append(path, uuid.UUID(raw[i:i+size]))
	}
	return path, nil
}
