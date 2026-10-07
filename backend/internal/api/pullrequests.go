package api

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/store"
	"github.com/guided-traffic/cowork/backend/internal/store/readq"
	"github.com/guided-traffic/cowork/backend/internal/store/writeq"
)

// ListTicketPullRequests answers the pull requests and default-branch commits
// GitHub's webhook linked to the ticket (docs/adr/0071 D6), oldest link first,
// under the ticket's predicate like its other children (docs/adr/0065 D1).
func (s *Server) ListTicketPullRequests(ctx context.Context, req apigen.ListTicketPullRequestsRequestObject) (apigen.ListTicketPullRequestsResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, read); perr != nil {
		return nil, perr
	}
	const op = "listTicketPullRequests"
	scope := fmt.Sprintf("%s/%s/%d", t.ID, req.Project, req.Number)
	size := s.h.pageSize(req.Params.Limit)
	var rows []readq.ListTicketPullRequestsRow
	err := s.db.InTenant(ctx, t.ID, func(r *store.Reader) error {
		tc, err := visibleTicket(ctx, r, t, req.Project, req.Number)
		if err != nil {
			return err
		}
		after, err := s.uuidAfter(op, scope, req.Params.Cursor)
		if err != nil {
			return err
		}
		rows, err = r.ListTicketPullRequests(ctx, readq.ListTicketPullRequestsParams{TenantID: t.ID, TicketID: tc.row.ID,
			After: after, PageSize: limitArg(size)})
		return err
	})
	if err != nil {
		return nil, err
	}
	rows, next := page(s.h, rows, size, op, scope, func(p readq.ListTicketPullRequestsRow) string { return p.ID.String() })
	out := apigen.PullRequestList{Items: make([]apigen.PullRequest, 0, len(rows)), NextCursor: nullableString(next)}
	for _, p := range rows {
		out.Items = append(out.Items, pullRequestView(p))
	}
	tag, unchanged := listTag(req.Params.IfNoneMatch, out)
	if unchanged {
		return apigen.ListTicketPullRequests304Response{Headers: apigen.NotModifiedResponseHeaders{ETag: &tag}}, nil
	}
	return apigen.ListTicketPullRequests200JSONResponse{Body: out,
		Headers: apigen.ListTicketPullRequests200ResponseHeaders{ETag: &tag}}, nil
}

func pullRequestView(p readq.ListTicketPullRequestsRow) apigen.PullRequest {
	v := apigen.PullRequest{Id: p.ID, Kind: apigen.PullRequestKind(p.Kind), Repository: p.Repository, Title: p.Title,
		State: apigen.PullRequestState(p.State), Url: p.Url, Author: nullableOf(p.Author), MergedAt: nullableOf(p.MergedAt),
		FoundIn: apigen.PullRequestFoundIn(p.FoundIn), FirstSeenAt: p.FirstSeenAt, LastSeenAt: p.LastSeenAt,
		Sha: nullableOf(p.Sha), Number: nullableOf[int](nil)}
	if p.Number != nil {
		n := int(*p.Number)
		v.Number = nullableOf(&n)
	}
	return v
}

// RemoveTicketPullRequest removes a wrong link of a pull request or a commit
// (docs/adr/0071 Residual risks), as a person removes any link: a member's act
// with write scope, in the agent baseline (docs/adr/0043 D2). The row stays,
// marked removed, so a later delivery that names the ticket does not bring it
// back; the act unlinked names the pull request or the commit, never its title.
func (s *Server) RemoveTicketPullRequest(ctx context.Context, req apigen.RemoveTicketPullRequestRequestObject) (apigen.RemoveTicketPullRequestResponseObject, error) {
	t := tenantFrom(ctx)
	p := principal(ctx)
	_, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		tc, err := visibleTicket(ctx, w.Reader, t, req.Project, req.Number)
		if err != nil {
			return err
		}
		if perr := auth.Authorize(p, tc.role, work); perr != nil {
			return perr
		}
		row, err := w.RemoveTicketPullRequest(ctx, writeq.RemoveTicketPullRequestParams{RemovedAt: s.h.opts.Now(),
			RemovedBy: p.PersonID, TenantID: t.ID, TicketID: tc.row.ID, ID: req.PullRequest})
		if errors.Is(err, pgx.ErrNoRows) {
			return store.ErrNoChange
		}
		if err != nil {
			return err
		}
		before := map[string]any{fieldRepository: row.Repository}
		if row.Number != nil {
			before[fieldNumber] = *row.Number
		}
		if row.Sha != nil {
			before[fieldSHA] = *row.Sha
		}
		w.Record(store.Event{EntityType: row.Kind, EntityID: req.PullRequest, TicketID: tc.row.ID,
			TicketKey: domain.FullKey(t.Slug, tc.project.Key, tc.row.Number), Action: actionUnlinked, Before: before})
		return nil
	})
	if err != nil && !errors.Is(err, store.ErrNoChange) {
		return nil, err
	}
	return apigen.RemoveTicketPullRequest204Response{}, nil
}
