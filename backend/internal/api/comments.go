package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/oapi-codegen/nullable"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/problem"
	"github.com/guided-traffic/cowork/backend/internal/richtext"
	"github.com/guided-traffic/cowork/backend/internal/store"
	"github.com/guided-traffic/cowork/backend/internal/store/readq"
	"github.com/guided-traffic/cowork/backend/internal/store/writeq"
)

const (
	entityComment   = "comment"
	actionCommented = "commented"
	orderDesc       = "desc"
)

// comment is the columns every comment query returns.
type comment = readq.GetCommentRow

// commentView renders a comment without its HTML, which renderedComment adds;
// a withdrawn comment's text and mentions never leave (docs/adr/0015 D3, D5).
func commentView(c comment) apigen.Comment {
	v := apigen.Comment{
		Id: c.ID, Author: personView(c.AuthorID, c.AuthorUsername, c.AuthorName), Agent: nullableOf(c.Agent),
		Token: tokenMarkView(c.TokenID, c.TokenName), Body: nullableOf(&c.Body), BodyHtml: nullableString(nil),
		Withdrawn: c.WithdrawnAt != nil, WithdrawnAt: nullableOf(c.WithdrawnAt), Edited: c.Edited,
		Explains: make([]apigen.AuditAction, 0, len(c.Explains)), Mentions: append([]uuid.UUID{}, c.Mentions...),
		Version: int(c.Version), CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}
	if v.Withdrawn {
		v.Body = nullableString(nil)
		v.Mentions = []uuid.UUID{}
	}
	for _, a := range c.Explains {
		v.Explains = append(v.Explains, apigen.AuditAction(a))
	}
	return v
}

func descending(order *string) bool { return order != nil && *order == orderDesc }

// ListComments lists a ticket's comment thread, oldest first unless asked
// otherwise (docs/adr/0015 D1, docs/adr/0048 D6).
func (s *Server) ListComments(ctx context.Context, req apigen.ListCommentsRequestObject) (apigen.ListCommentsResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, read); perr != nil {
		return nil, perr
	}
	desc := descending((*string)(req.Params.Order))
	const op = "listComments"
	scope := fmt.Sprintf("%s/%s/%d/%t", t.ID, req.Project, req.Number, desc)
	size := s.h.pageSize(req.Params.Limit)
	var (
		rows   []readq.ListCommentsRow
		images richtext.Images
	)
	err := s.db.InTenant(ctx, t.ID, func(r *store.Reader) error {
		tc, err := visibleTicket(ctx, r, t, req.Project, req.Number)
		if err != nil {
			return err
		}
		after, err := s.uuidAfter(op, scope, req.Params.Cursor)
		if err != nil {
			return err
		}
		rows, err = r.ListComments(ctx, readq.ListCommentsParams{TenantID: t.ID, TicketID: tc.row.ID, After: after,
			Descending: desc, PageSize: limitArg(size)})
		if err != nil {
			return err
		}
		images, err = ticketImages(ctx, r, t, tc)
		return err
	})
	if err != nil {
		return nil, err
	}
	rows, next := page(s.h, rows, size, op, scope, func(c readq.ListCommentsRow) string { return c.ID.String() })
	out := apigen.CommentList{Items: make([]apigen.Comment, 0, len(rows)), NextCursor: nullableString(next)}
	for _, c := range rows {
		out.Items = append(out.Items, renderedComment(comment(c), images))
	}
	tag, unchanged := listTag(req.Params.IfNoneMatch, out)
	if unchanged {
		return apigen.ListComments304Response{Headers: apigen.NotModifiedResponseHeaders{ETag: &tag}}, nil
	}
	return apigen.ListComments200JSONResponse{Body: out, Headers: apigen.ListComments200ResponseHeaders{ETag: &tag}}, nil
}

// uuidAfter decodes the cursor of a list ordered by id.
func (s *Server) uuidAfter(op, scope string, cursor *string) (*uuid.UUID, error) {
	if cursor == nil {
		return nil, nil
	}
	after, perr := s.cursors.decode(op, scope, *cursor)
	if perr != nil {
		return nil, perr
	}
	id, err := uuid.Parse(after)
	if err != nil {
		return nil, problem.New(problem.InvalidCursor, "the cursor does not belong to this list")
	}
	return &id, nil
}

// visibleComment reads a comment through its ticket's predicate.
func visibleComment(ctx context.Context, r *store.Reader, t tenantScope, project string, number int, id uuid.UUID) (ticketCtx, comment, error) {
	tc, err := visibleTicket(ctx, r, t, project, number)
	if err != nil {
		return tc, comment{}, err
	}
	c, err := r.GetComment(ctx, readq.GetCommentParams{TenantID: t.ID, TicketID: tc.row.ID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return tc, c, problem.New(problem.NotFound, "no such comment")
	}
	return tc, c, err
}

// GetComment answers one comment.
func (s *Server) GetComment(ctx context.Context, req apigen.GetCommentRequestObject) (apigen.GetCommentResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, read); perr != nil {
		return nil, perr
	}
	var (
		c      comment
		images richtext.Images
	)
	err := s.db.InTenant(ctx, t.ID, func(r *store.Reader) error {
		tc, found, err := visibleComment(ctx, r, t, req.Project, req.Number, req.Comment)
		if err != nil {
			return err
		}
		c = found
		images, err = ticketImages(ctx, r, t, tc)
		return err
	})
	if err != nil {
		return nil, err
	}
	return apigen.GetComment200JSONResponse{Body: renderedComment(c, images), Headers: apigen.GetComment200ResponseHeaders{ETag: etag(c.Version)}}, nil
}

// writeComment adds a comment as the caller, with its act; an agent writes
// in its person's name with its mark (docs/adr/0015 D3), and a token's comment
// carries the token (docs/adr/0036 D6). The act tells the persons it mentions
// first, then the ticket's watchers — each person once (docs/adr/0020 D2).
func writeComment(ctx context.Context, w *store.Writer, t tenantScope, tc ticketCtx, text string, mentions []uuid.UUID) (uuid.UUID, error) {
	p := principal(ctx)
	ins := writeq.InsertCommentParams{TenantID: t.ID, TicketID: tc.row.ID, AuthorID: p.PersonID, Body: text,
		Mentions: append([]uuid.UUID{}, mentions...)}
	ins.TokenID, ins.TokenName = actToken(p)
	if p.IsAgent() {
		ins.Agent = &p.Agent
	}
	id, err := w.InsertComment(ctx, ins)
	if err != nil {
		return uuid.Nil, fmt.Errorf("insert the comment: %w", err)
	}
	notices := []store.Notice{{Reason: store.NoticeCommented, Watchers: true}}
	if len(mentions) > 0 {
		notices = append([]store.Notice{{Reason: store.NoticeMentioned, People: mentions}}, notices...)
	}
	w.Record(store.Event{EntityType: entityComment, EntityID: id, TicketID: tc.row.ID, TicketKey: ticketKey(t, tc.row),
		Action: actionCommented, Notices: notices})
	return id, nil
}

// explain writes the comment that explains an act of the same request, when
// one was sent (docs/adr/0015 D2); uuid.Nil without one. It mentions nobody.
func explain(ctx context.Context, w *store.Writer, t tenantScope, tc ticketCtx, text *string) (uuid.UUID, error) {
	if text == nil {
		return uuid.Nil, nil
	}
	return writeComment(ctx, w, t, tc, *text, nil)
}

// checkMentions admits as a mention only a member of the tenant who can see
// the ticket, as a question's asked_of (docs/adr/0015 D5 as amended
// 2026-10-05); the first that is none is refused at its place in the body's
// list. at maps a person to that place.
func checkMentions(ctx context.Context, r *store.Reader, t tenantScope, tc ticketCtx, persons []uuid.UUID, at func(uuid.UUID) int) error {
	for _, person := range persons {
		visible, err := r.CanSeeTicket(ctx, readq.CanSeeTicketParams{TenantID: t.ID, TicketID: tc.row.ID, UserID: person})
		if err != nil {
			return fmt.Errorf("check a mention: %w", err)
		}
		if !visible {
			return problem.Field(fmt.Sprintf("/mentions/%d", at(person)), "not a member who can see the ticket")
		}
	}
	return nil
}

// placeIn finds a person's place in a list of mentions, for the pointer of a
// refusal.
func placeIn(list []uuid.UUID) func(uuid.UUID) int {
	return func(person uuid.UUID) int { return slices.Index(list, person) }
}

// mentionsAdded are the persons of next that previous does not hold, in next's
// order.
func mentionsAdded(previous, next []uuid.UUID) []uuid.UUID {
	var out []uuid.UUID
	for _, person := range next {
		if !slices.Contains(previous, person) {
			out = append(out, person)
		}
	}
	return out
}

// AddComment comments on a ticket: a member's act, in the agent baseline
// (docs/adr/0043 D2).
func (s *Server) AddComment(ctx context.Context, req apigen.AddCommentRequestObject) (apigen.AddCommentResponseObject, error) {
	t := tenantFrom(ctx)
	body := *req.Body
	ctx, perr := s.keyed(ctx, req.Params.IdempotencyKey, "addComment", fmt.Sprintf("%s/%s/%d", t.ID, req.Project, req.Number), body)
	if perr != nil {
		return nil, perr
	}
	var (
		added    comment
		images   richtext.Images
		location string
	)
	replay, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		tc, err := visibleTicket(ctx, w.Reader, t, req.Project, req.Number)
		if err != nil {
			return err
		}
		if perr := auth.Authorize(principal(ctx), tc.role, work); perr != nil {
			return perr
		}
		mentions := deref(body.Mentions)
		if err := checkMentions(ctx, w.Reader, t, tc, mentions, placeIn(mentions)); err != nil {
			return err
		}
		id, err := writeComment(ctx, w, t, tc, body.Body, mentions)
		if err != nil {
			return err
		}
		if added, images, err = writtenComment(ctx, w.Reader, t, tc, id); err != nil {
			return err
		}
		location = ticketURL(t, tc.project.Key, tc.row.Number) + "/comments/" + id.String()
		res, err := stored(renderedComment(added, images), map[string]string{headerETag: *etag(added.Version), headerLocation: location})
		if err != nil {
			return err
		}
		w.Respond(res)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if replay != nil {
		body, err := replayed[apigen.Comment](replay)
		if err != nil {
			return nil, err
		}
		return apigen.AddComment201JSONResponse{Body: body, Headers: apigen.AddComment201ResponseHeaders{
			ETag: header(replay, headerETag), Location: header(replay, headerLocation)}}, nil
	}
	return apigen.AddComment201JSONResponse{Body: renderedComment(added, images), Headers: apigen.AddComment201ResponseHeaders{
		ETag: etag(added.Version), Location: &location}}, nil
}

// mayChangeComment holds an edit or a withdrawal to docs/adr/0015 D3, D4: a
// person changes their own comments and their agents'; an agent only those an
// agent of the same person wrote; a tenant administrator withdraws, never
// edits.
func mayChangeComment(p auth.Principal, tc ticketCtx, c readq.GetCommentForWriteRow, withdrawing bool) *problem.Error {
	if perr := auth.Authorize(p, tc.role, work); perr != nil {
		return perr
	}
	switch {
	case p.IsAgent():
		if c.AuthorID != p.PersonID || c.Agent == nil {
			return problem.New(problem.AgentForbidden, "an agent changes only comments an agent of the same person wrote")
		}
	case c.AuthorID == p.PersonID:
	case withdrawing && tc.role == domain.RoleAdmin:
		// Withdrawing another person's comment is moderation, an
		// administrator's act with admin scope (docs/adr/0035 D3).
		return auth.Authorize(p, tc.role, auth.Need{Role: domain.RoleAdmin, Scope: domain.ScopeAdmin})
	default:
		return problem.New(problem.Forbidden, "only the author changes a comment; a team administrator withdraws it")
	}
	return nil
}

// commentForWrite reads the comment a write changes.
func commentForWrite(ctx context.Context, r *store.Reader, t tenantScope, tc ticketCtx, id uuid.UUID) (readq.GetCommentForWriteRow, error) {
	c, err := r.GetCommentForWrite(ctx, readq.GetCommentForWriteParams{TenantID: t.ID, TicketID: tc.row.ID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return c, problem.New(problem.NotFound, "no such comment")
	}
	return c, err
}

// EditComment replaces a comment's text and keeps the previous one
// (docs/adr/0015 D3).
func (s *Server) EditComment(ctx context.Context, req apigen.EditCommentRequestObject) (apigen.EditCommentResponseObject, error) {
	t := tenantFrom(ctx)
	version, perr := ifMatch(req.Params.IfMatch)
	if perr != nil {
		return nil, perr
	}
	var (
		out    comment
		images richtext.Images
	)
	_, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		var err error
		out, images, err = s.editComment(ctx, w, t, req, version)
		return err
	})
	if err != nil && !errors.Is(err, store.ErrNoChange) {
		return nil, err
	}
	return apigen.EditComment200JSONResponse{Body: renderedComment(out, images), Headers: apigen.EditComment200ResponseHeaders{ETag: etag(out.Version)}}, nil
}

// editComment is the edit's transaction: the comment through its ticket's
// predicate, the author's rule, the version the edit read, and the text and
// mentions written with the act, which tells the persons the edit adds
// (docs/adr/0015 D3–D5). It answers the comment as the write left it, with the
// images its text may show; a write that changes nothing answers it as it is
// with store.ErrNoChange.
func (s *Server) editComment(ctx context.Context, w *store.Writer, t tenantScope, req apigen.EditCommentRequestObject,
	version int32) (comment, richtext.Images, error) {
	tc, err := visibleTicket(ctx, w.Reader, t, req.Project, req.Number)
	if err != nil {
		return comment{}, nil, err
	}
	c, err := commentForWrite(ctx, w.Reader, t, tc, req.Comment)
	if err != nil {
		return comment{}, nil, err
	}
	if perr := mayChangeComment(principal(ctx), tc, c, false); perr != nil {
		return comment{}, nil, perr
	}
	if c.WithdrawnAt != nil {
		return comment{}, nil, problem.New(problem.StateConflict, "the comment is withdrawn")
	}
	if c.Version != version {
		return comment{}, nil, stale(c.Version, map[string]any{fieldBody: c.Body})
	}
	mentions := c.Mentions
	if req.Body.Mentions != nil {
		mentions = *req.Body.Mentions
	}
	if c.Body == req.Body.Body && sameMentions(c.Mentions, mentions) {
		out, images, err := writtenComment(ctx, w.Reader, t, tc, c.ID)
		if err == nil {
			err = store.ErrNoChange
		}
		return out, images, err
	}
	// Only a person the edit adds is checked and told: those it keeps were
	// checked when they were added (docs/adr/0015 D5).
	anew := mentionsAdded(c.Mentions, mentions)
	if err := checkMentions(ctx, w.Reader, t, tc, anew, placeIn(mentions)); err != nil {
		return comment{}, nil, err
	}
	if err := s.reviseComment(ctx, w, t, c, version, req.Body.Body, mentions); err != nil {
		return comment{}, nil, err
	}
	ev := store.Event{EntityType: entityComment, EntityID: c.ID, TicketID: tc.row.ID, TicketKey: ticketKey(t, tc.row), Action: actionEdited}
	if len(anew) > 0 {
		ev.Notices = []store.Notice{{Reason: store.NoticeMentioned, People: anew}}
	}
	w.Record(ev)
	return writtenComment(ctx, w.Reader, t, tc, c.ID)
}

// reviseComment keeps the comment's previous text in its history and writes
// the new text and mentions over the version the edit read (docs/adr/0015
// D3, docs/adr/0050 D3).
func (s *Server) reviseComment(ctx context.Context, w *store.Writer, t tenantScope, c readq.GetCommentForWriteRow, version int32,
	body string, mentions []uuid.UUID) error {
	p := principal(ctx)
	rev := writeq.InsertCommentRevisionParams{TenantID: t.ID, CommentID: c.ID, Body: c.Body, EditedBy: p.PersonID}
	rev.TokenID, rev.TokenName = actToken(p)
	if p.IsAgent() {
		rev.Agent = &p.Agent
	}
	if err := w.InsertCommentRevision(ctx, rev); err != nil {
		return fmt.Errorf("keep the previous text: %w", err)
	}
	_, err := w.UpdateCommentBody(ctx, writeq.UpdateCommentBodyParams{TenantID: t.ID, ID: c.ID, Version: version, Body: body,
		Mentions: append([]uuid.UUID{}, mentions...)})
	if errors.Is(err, pgx.ErrNoRows) {
		return stale(c.Version, map[string]any{fieldBody: c.Body})
	}
	return err
}

// sameMentions reports whether two lists name the same persons, in any order.
func sameMentions(a, b []uuid.UUID) bool {
	return len(mentionsAdded(a, b)) == 0 && len(mentionsAdded(b, a)) == 0
}

// WithdrawComment hides a comment's text and keeps the entry
// (docs/adr/0015 D3); withdrawing a withdrawn comment changes nothing.
func (s *Server) WithdrawComment(ctx context.Context, req apigen.WithdrawCommentRequestObject) (apigen.WithdrawCommentResponseObject, error) {
	t := tenantFrom(ctx)
	var out comment
	_, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		tc, err := visibleTicket(ctx, w.Reader, t, req.Project, req.Number)
		if err != nil {
			return err
		}
		c, err := commentForWrite(ctx, w.Reader, t, tc, req.Comment)
		if err != nil {
			return err
		}
		p := principal(ctx)
		if perr := mayChangeComment(p, tc, c, true); perr != nil {
			return perr
		}
		if c.WithdrawnAt == nil {
			if err := w.WithdrawComment(ctx, writeq.WithdrawCommentParams{TenantID: t.ID, ID: c.ID, WithdrawnBy: &p.PersonID}); err != nil {
				return fmt.Errorf("withdraw the comment: %w", err)
			}
			w.Record(store.Event{EntityType: entityComment, EntityID: c.ID, TicketID: tc.row.ID, TicketKey: ticketKey(t, tc.row), Action: "withdrawn"})
		}
		out, err = w.GetComment(ctx, readq.GetCommentParams{TenantID: t.ID, TicketID: tc.row.ID, ID: c.ID})
		if err == nil && c.WithdrawnAt != nil {
			err = store.ErrNoChange
		}
		return err
	})
	if err != nil && !errors.Is(err, store.ErrNoChange) {
		return nil, err
	}
	// A withdrawn comment has no text to render.
	return apigen.WithdrawComment200JSONResponse{Body: renderedComment(out, nil), Headers: apigen.WithdrawComment200ResponseHeaders{ETag: etag(out.Version)}}, nil
}

// ListCommentRevisions lists a comment's previous texts; none once it is
// withdrawn (docs/adr/0015 D3).
func (s *Server) ListCommentRevisions(ctx context.Context, req apigen.ListCommentRevisionsRequestObject) (apigen.ListCommentRevisionsResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, read); perr != nil {
		return nil, perr
	}
	const op = "listCommentRevisions"
	scope := fmt.Sprintf("%s/%s", t.ID, req.Comment)
	size := s.h.pageSize(req.Params.Limit)
	var rows []readq.ListCommentRevisionsRow
	err := s.db.InTenant(ctx, t.ID, func(r *store.Reader) error {
		if _, _, err := visibleComment(ctx, r, t, req.Project, req.Number, req.Comment); err != nil {
			return err
		}
		after, err := s.uuidAfter(op, scope, req.Params.Cursor)
		if err != nil {
			return err
		}
		rows, err = r.ListCommentRevisions(ctx, readq.ListCommentRevisionsParams{TenantID: t.ID, CommentID: req.Comment,
			After: after, PageSize: limitArg(size)})
		return err
	})
	if err != nil {
		return nil, err
	}
	rows, next := page(s.h, rows, size, op, scope, func(r readq.ListCommentRevisionsRow) string { return r.ID.String() })
	out := apigen.ListCommentRevisions200JSONResponse{Items: make([]apigen.CommentRevision, 0, len(rows)), NextCursor: nullableString(next)}
	for _, r := range rows {
		out.Items = append(out.Items, apigen.CommentRevision{Body: r.Body, EditedBy: personView(r.EditedBy, r.EditedByUsername, r.EditedByName),
			Agent: nullableOf(r.Agent), Token: tokenMarkView(r.TokenID, r.TokenName), At: r.CreatedAt})
	}
	return out, nil
}

// ListActivity lists a ticket's acts (docs/adr/0015 D1, D6). An act whose
// payload names a ticket the caller cannot see shows without the payload
// (docs/adr/0065 D4).
func (s *Server) ListActivity(ctx context.Context, req apigen.ListActivityRequestObject) (apigen.ListActivityResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, read); perr != nil {
		return nil, perr
	}
	desc := descending((*string)(req.Params.Order))
	const op = "listActivity"
	scope := fmt.Sprintf("%s/%s/%d/%t", t.ID, req.Project, req.Number, desc)
	size := s.h.pageSize(req.Params.Limit)
	var rows []readq.ListTicketActivityRow
	visible := map[uuid.UUID]bool{}
	err := s.db.InTenant(ctx, t.ID, func(r *store.Reader) error {
		tc, err := visibleTicket(ctx, r, t, req.Project, req.Number)
		if err != nil {
			return err
		}
		after, err := s.uuidAfter(op, scope, req.Params.Cursor)
		if err != nil {
			return err
		}
		if rows, err = r.ListTicketActivity(ctx, readq.ListTicketActivityParams{TenantID: &t.ID, TicketID: &tc.row.ID,
			After: after, Descending: desc, PageSize: limitArg(size)}); err != nil {
			return err
		}
		var refs []uuid.UUID
		for _, a := range rows {
			refs = append(refs, a.Refs...)
		}
		if len(refs) == 0 {
			return nil
		}
		ids, err := r.VisibleTickets(ctx, readq.VisibleTicketsParams{TenantID: t.ID, Ids: refs})
		for _, id := range ids {
			visible[id] = true
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	rows, next := page(s.h, rows, size, op, scope, func(a readq.ListTicketActivityRow) string { return a.ID.String() })
	out := apigen.ActivityList{Items: make([]apigen.Activity, 0, len(rows)), NextCursor: nullableString(next)}
	for _, a := range rows {
		out.Items = append(out.Items, activityView(a, visible))
	}
	tag, unchanged := listTag(req.Params.IfNoneMatch, out)
	if unchanged {
		return apigen.ListActivity304Response{Headers: apigen.NotModifiedResponseHeaders{ETag: &tag}}, nil
	}
	return apigen.ListActivity200JSONResponse{Body: out, Headers: apigen.ListActivity200ResponseHeaders{ETag: &tag}}, nil
}

func activityView(a readq.ListTicketActivityRow, visible map[uuid.UUID]bool) apigen.Activity {
	v := apigen.Activity{
		Id: a.ID, At: a.CreatedAt, Actor: nullableOf[apigen.Person](nil), ActorSystem: nullableOf(a.ActorSystem),
		Agent: nullableOf(a.Agent), Token: tokenMarkView(a.TokenID, a.TokenName), Action: apigen.AuditAction(a.Action),
		EntityType: a.EntityType, EntityId: nullableOf(a.EntityID), ExplainedByComment: nullableOf(a.ExplainedByCommentID),
		Reason: nullableOf(a.Reason), Note: nullableOf(a.Note), Before: jsonObject(a.Before), After: jsonObject(a.After),
	}
	if a.ActorUserID != nil {
		p := personView(*a.ActorUserID, a.ActorUsername, a.ActorName)
		v.Actor = nullableOf(&p)
	}
	for _, ref := range a.Refs {
		if !visible[ref] {
			v.Redacted, v.Before, v.After = true, jsonObject(nil), jsonObject(nil)
			v.Reason, v.Note = nullableString(nil), nullableString(nil)
			break
		}
	}
	return v
}

// jsonObject decodes a stored payload; NULL, or a value that is not an
// object, is null.
func jsonObject(b []byte) nullable.Nullable[map[string]any] {
	var m map[string]any
	if b == nil || json.Unmarshal(b, &m) != nil || m == nil {
		return nullable.NewNullNullable[map[string]any]()
	}
	return nullable.NewNullableWithValue(m)
}
