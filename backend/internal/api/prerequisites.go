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

// headTreeOp is the operation of the tree across teams.
const headTreeOp = "listPrerequisiteTree"

// ListPrerequisiteTree answers a ticket's prerequisite tree across teams, or
// read upward its dependents, every node by its head as the caller sees it
// (docs/adr/0012 D6 as amended 2026-10-10): the walk goes on only from a
// ticket the caller reads, a head and a placeholder are leaves, and a node of
// the caller's own team they read shows its assignee and its progress. A
// cursor carries the node's path, ids of tickets the caller may not see among
// them, so it is sealed.
func (s *Server) ListPrerequisiteTree(ctx context.Context, req apigen.ListPrerequisiteTreeRequestObject) (apigen.ListPrerequisiteTreeResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, read); perr != nil {
		return nil, perr
	}
	up := req.Params.Direction != nil && *req.Params.Direction == apigen.ListPrerequisiteTreeParamsDirectionUp
	scope := treeScope(t, req.Project, req.Number, up)
	size := s.h.pageSize(req.Params.Limit)
	position, perr := s.cursors.sealedPosition(headTreeOp, scope, req.Params.Cursor)
	if perr != nil {
		return nil, perr
	}
	var after []uuid.UUID
	if position != "" {
		if after, perr = decodeTreePath(position); perr != nil {
			return nil, perr
		}
	}
	var nodes []store.TreeHead
	open := 0
	err := s.db.InTenant(ctx, t.ID, func(r *store.Reader) error {
		tc, err := visibleTicket(ctx, r, t, req.Project, req.Number)
		if err != nil {
			return err
		}
		nodes, open, err = headTree(ctx, r, tc.row.ID, up, after, size)
		return err
	})
	if err != nil {
		return nil, err
	}
	nodes, next := page(s.h, nodes, size, headTreeOp, scope, func(n store.TreeHead) string {
		return s.cursors.sealPosition(encodeTreePath(n.Path))
	})
	out := apigen.PrerequisiteHeadTree{Items: []apigen.PrerequisiteHeadNode{}, NextCursor: nullableString(next), Open: open}
	for _, n := range nodes {
		out.Items = append(out.Items, headNodeView(n))
	}
	tag, unchanged := listTag(req.Params.IfNoneMatch, out)
	if unchanged {
		return apigen.ListPrerequisiteTree304Response{Headers: apigen.NotModifiedResponseHeaders{ETag: &tag}}, nil
	}
	return apigen.ListPrerequisiteTree200JSONResponse{Body: out, Headers: apigen.ListPrerequisiteTree200ResponseHeaders{ETag: &tag}}, nil
}

// headTree reads a page of the tree across teams after the node at the path
// after (nil: from the start), and how many of the ticket's prerequisites are
// open, which a page past the last node reads from the first.
func headTree(ctx context.Context, r *store.Reader, ticket uuid.UUID, up bool, after []uuid.UUID, size int) ([]store.TreeHead, int, error) {
	nodes, err := r.PrerequisiteHeads(ctx, ticket, up, treeDepth, after, limitArg(size))
	if err != nil {
		return nil, 0, err
	}
	counted := nodes
	if len(nodes) == 0 && after != nil {
		if counted, err = r.PrerequisiteHeads(ctx, ticket, up, treeDepth, nil, 1); err != nil {
			return nil, 0, err
		}
	}
	if len(counted) == 0 {
		return nodes, 0, nil
	}
	return nodes, int(counted[0].OpenCount), nil
}

// headNodeView is a node of the tree across teams as the API shows it: its
// head; settled where its state is the caller's to read; its assignee and its
// stages only for a ticket of the caller's own team they read.
func headNodeView(n store.TreeHead) apigen.PrerequisiteHeadNode {
	v := apigen.PrerequisiteHeadNode{Depth: int(n.Depth), Repeated: n.Repeated, Head: headView(n.Head),
		Settled: nullableOf[bool](nil), BlockedFrom: nullableOf[apigen.TicketState](nil),
		Assignee: nullableOf[apigen.Person](nil), Progress: nullableOf[apigen.NodeProgress](nil)}
	if !n.Head.Placeholder() {
		settled := n.Head.State.Terminal()
		v.Settled = nullableOf(&settled)
	}
	if n.BlockedFrom != nil {
		from := apigen.TicketState(*n.BlockedFrom)
		v.BlockedFrom = nullableOf(&from)
	}
	if !n.Own || n.Progress == nil || n.Refinement == nil || n.Review == nil {
		return v
	}
	if n.Assignee != nil {
		a := personView(n.Assignee.ID, n.Assignee.Username, n.Assignee.Name)
		v.Assignee = nullableOf(&a)
	}
	stages := stagesOf(store.TicketRow{Progress: *n.Progress, ProgressDerived: n.ProgressDerived,
		ProgressRefinement: *n.Refinement, ProgressRefinementDerived: n.RefinementDer,
		ProgressReview: *n.Review, ProgressReviewDerived: n.ReviewDerived})
	progress := apigen.NodeProgress{Implementation: stages.Implementation, Refinement: stages.Refinement,
		Review: stages.Review, Derived: n.ProgressDerived != nil}
	v.Progress = nullableOf(&progress)
	return v
}

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
