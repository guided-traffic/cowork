package api

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/problem"
	"github.com/guided-traffic/cowork/backend/internal/store"
	"github.com/guided-traffic/cowork/backend/internal/store/readq"
	"github.com/guided-traffic/cowork/backend/internal/store/writeq"
)

// The question's entity type and statuses (docs/adr/0011 D2).
const (
	entityQuestion    = "question"
	questionOpen      = "open"
	questionAnswered  = "answered"
	questionWithdrawn = "withdrawn"
	fieldAnswer       = "answer"
)

// question is the columns every question query returns.
type question = readq.GetQuestionRow

func questionView(q question) apigen.Question {
	v := apigen.Question{
		Id: q.ID, Number: int(q.Number), Question: q.Question, Options: q.Options, Recommendation: q.Recommendation,
		Answer: nullableOf(q.Answer), Status: apigen.QuestionStatus(q.Status),
		AskedBy: personView(q.AskedBy, q.AskedByUsername, q.AskedByName), AskedByAgent: nullableOf(q.AskedByAgent),
		AskedByToken: tokenMarkView(q.AskedByTokenID, q.AskedByTokenName), AskedOf: nullableOf[apigen.Person](nil),
		AnsweredBy: nullableOf[apigen.Person](nil), AnsweredAt: nullableOf(q.AnsweredAt), RecordedByAgent: q.RecordedByAgent,
		AnsweredByToken: tokenMarkView(q.AnsweredByTokenID, q.AnsweredByTokenName), WithdrawnAt: nullableOf(q.WithdrawnAt),
		Version: int(q.Version), CreatedAt: q.CreatedAt, UpdatedAt: q.UpdatedAt,
	}
	if q.AskedOf != nil {
		p := personView(*q.AskedOf, q.AskedOfUsername, q.AskedOfName)
		v.AskedOf = nullableOf(&p)
	}
	if q.AnsweredBy != nil {
		p := personView(*q.AnsweredBy, q.AnsweredByUsername, q.AnsweredByName)
		v.AnsweredBy = nullableOf(&p)
	}
	return v
}

func questionURL(t tenantScope, tc ticketCtx, number int32) string {
	return ticketURL(t, tc.project.Key, tc.row.Number) + "/questions/" + strconv.Itoa(int(number))
}

// visibleQuestion reads a question through its ticket's predicate.
func visibleQuestion(ctx context.Context, r *store.Reader, t tenantScope, project string, number, qnumber int) (ticketCtx, question, error) {
	tc, err := visibleTicket(ctx, r, t, project, number)
	if err != nil {
		return tc, question{}, err
	}
	n, perr := ticketNumber(qnumber)
	if perr != nil {
		return tc, question{}, problem.New(problem.NotFound, "no such question")
	}
	q, err := r.GetQuestion(ctx, readq.GetQuestionParams{TenantID: t.ID, TicketID: tc.row.ID, Number: n})
	if errors.Is(err, pgx.ErrNoRows) {
		return tc, q, problem.New(problem.NotFound, "no such question")
	}
	return tc, q, err
}

// ListQuestions lists a ticket's questions by number.
func (s *Server) ListQuestions(ctx context.Context, req apigen.ListQuestionsRequestObject) (apigen.ListQuestionsResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, read); perr != nil {
		return nil, perr
	}
	const op = "listQuestions"
	scope := fmt.Sprintf("%s/%s/%d", t.ID, req.Project, req.Number)
	size := s.h.pageSize(req.Params.Limit)
	var rows []readq.ListQuestionsRow
	err := s.db.InTenant(ctx, t.ID, func(r *store.Reader) error {
		tc, err := visibleTicket(ctx, r, t, req.Project, req.Number)
		if err != nil {
			return err
		}
		params := readq.ListQuestionsParams{TenantID: t.ID, TicketID: tc.row.ID, PageSize: limitArg(size)}
		if req.Params.Cursor != nil {
			after, perr := s.cursors.decode(op, scope, *req.Params.Cursor)
			if perr != nil {
				return perr
			}
			n, err := strconv.ParseInt(after, 10, 32)
			if err != nil {
				return problem.New(problem.InvalidCursor, "the cursor does not belong to this list")
			}
			last := int32(n)
			params.After = &last
		}
		rows, err = r.ListQuestions(ctx, params)
		return err
	})
	if err != nil {
		return nil, err
	}
	rows, next := page(s.h, rows, size, op, scope, func(q readq.ListQuestionsRow) string { return strconv.Itoa(int(q.Number)) })
	out := apigen.ListQuestions200JSONResponse{Items: make([]apigen.Question, 0, len(rows)), NextCursor: nullableString(next)}
	for _, q := range rows {
		out.Items = append(out.Items, questionView(question(q)))
	}
	return out, nil
}

// GetQuestion answers one question.
func (s *Server) GetQuestion(ctx context.Context, req apigen.GetQuestionRequestObject) (apigen.GetQuestionResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, read); perr != nil {
		return nil, perr
	}
	var q question
	err := s.db.InTenant(ctx, t.ID, func(r *store.Reader) error {
		var err error
		_, q, err = visibleQuestion(ctx, r, t, req.Project, req.Number, req.Question)
		return err
	})
	if err != nil {
		return nil, err
	}
	return apigen.GetQuestion200JSONResponse{Body: questionView(q), Headers: apigen.GetQuestion200ResponseHeaders{ETag: etag(q.Version)}}, nil
}

// AskQuestion asks a question on a ticket: a member's act, in the agent
// baseline (docs/adr/0043 D2). The person asked must see the ticket.
func (s *Server) AskQuestion(ctx context.Context, req apigen.AskQuestionRequestObject) (apigen.AskQuestionResponseObject, error) {
	t := tenantFrom(ctx)
	body := *req.Body
	ctx, perr := s.keyed(ctx, req.Params.IdempotencyKey, "askQuestion", fmt.Sprintf("%s/%s/%d", t.ID, req.Project, req.Number), body)
	if perr != nil {
		return nil, perr
	}
	var asked question
	var location string
	replay, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		tc, err := visibleTicket(ctx, w.Reader, t, req.Project, req.Number)
		if err != nil {
			return err
		}
		p := principal(ctx)
		if perr := auth.Authorize(p, tc.role, work); perr != nil {
			return perr
		}
		if body.AskedOf != nil {
			if err := checkAskedOf(ctx, w.Reader, t, tc, *body.AskedOf); err != nil {
				return err
			}
		}
		if err := w.LockQuestions(ctx, tc.row.ID); err != nil {
			return err
		}
		n, err := w.NextQuestionNumber(ctx, writeq.NextQuestionNumberParams{TenantID: t.ID, TicketID: tc.row.ID})
		if err != nil {
			return fmt.Errorf("next question number: %w", err)
		}
		ins := writeq.InsertQuestionParams{TenantID: t.ID, TicketID: tc.row.ID, Number: n, Question: body.Question,
			Options: deref(body.Options), Recommendation: deref(body.Recommendation), AskedBy: p.PersonID, AskedOf: body.AskedOf}
		ins.AskedByTokenID, ins.AskedByTokenName = actToken(p)
		if p.IsAgent() {
			ins.AskedByAgent = &p.Agent
		}
		id, err := w.InsertQuestion(ctx, ins)
		if err != nil {
			return fmt.Errorf("insert the question: %w", err)
		}
		w.Record(store.Event{EntityType: entityQuestion, EntityID: id, TicketID: tc.row.ID, TicketKey: ticketKey(t, tc.row),
			Action: "asked", After: map[string]any{"number": n, "question": body.Question, "asked_of": body.AskedOf},
			Notices: told(store.NoticeAsked, body.AskedOf)})
		if asked, err = w.GetQuestion(ctx, readq.GetQuestionParams{TenantID: t.ID, TicketID: tc.row.ID, Number: n}); err != nil {
			return err
		}
		location = questionURL(t, tc, n)
		res, err := stored(questionView(asked), map[string]string{headerETag: *etag(asked.Version), headerLocation: location})
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
		body, err := replayed[apigen.Question](replay)
		if err != nil {
			return nil, err
		}
		return apigen.AskQuestion201JSONResponse{Body: body, Headers: apigen.AskQuestion201ResponseHeaders{
			ETag: header(replay, headerETag), Location: header(replay, headerLocation)}}, nil
	}
	return apigen.AskQuestion201JSONResponse{Body: questionView(asked), Headers: apigen.AskQuestion201ResponseHeaders{
		ETag: etag(asked.Version), Location: &location}}, nil
}

// checkAskedOf admits as the person asked only a member who can see the
// ticket.
func checkAskedOf(ctx context.Context, r *store.Reader, t tenantScope, tc ticketCtx, person uuid.UUID) error {
	visible, err := r.CanSeeTicket(ctx, readq.CanSeeTicketParams{TenantID: t.ID, TicketID: tc.row.ID, UserID: person})
	if err != nil {
		return fmt.Errorf("check the person asked: %w", err)
	}
	if !visible {
		return problem.Field("/asked_of", "not a member who can see the ticket")
	}
	return nil
}

// UpdateQuestion edits an open question: the asker's act, agents included.
func (s *Server) UpdateQuestion(ctx context.Context, req apigen.UpdateQuestionRequestObject) (apigen.UpdateQuestionResponseObject, error) {
	t := tenantFrom(ctx)
	version, perr := ifMatch(req.Params.IfMatch)
	if perr != nil {
		return nil, perr
	}
	body := *req.Body
	var out question
	_, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		tc, q, err := visibleQuestion(ctx, w.Reader, t, req.Project, req.Number, req.Question)
		if err != nil {
			return err
		}
		if perr := mayEdit(principal(ctx), tc, q); perr != nil {
			return perr
		}
		up := writeq.UpdateQuestionParams{TenantID: t.ID, ID: q.ID, Version: q.Version, Question: q.Question,
			Options: q.Options, Recommendation: q.Recommendation, AskedOf: q.AskedOf}
		applyQuestionPatch(body, &up)
		anew := askedAnew(q.AskedOf, up.AskedOf)
		if anew != nil {
			if err := checkAskedOf(ctx, w.Reader, t, tc, *anew); err != nil {
				return err
			}
		}
		before, after := diffDeep(questionFields(writeq.UpdateQuestionParams{Question: q.Question, Options: q.Options,
			Recommendation: q.Recommendation, AskedOf: q.AskedOf}), questionFields(up))
		if q.Version != version {
			return stale(q.Version, before)
		}
		if len(after) == 0 {
			out = q
			return store.ErrNoChange
		}
		if _, err := w.UpdateQuestion(ctx, up); errors.Is(err, pgx.ErrNoRows) {
			return stale(q.Version, before)
		} else if err != nil {
			return err
		}
		// Asked of another person now: a question asked of them (docs/adr/0020 D2).
		w.Record(store.Event{EntityType: entityQuestion, EntityID: q.ID, TicketID: tc.row.ID, TicketKey: ticketKey(t, tc.row),
			Action: actionEdited, Before: before, After: after, Notices: told(store.NoticeAsked, anew)})
		out, err = w.GetQuestion(ctx, readq.GetQuestionParams{TenantID: t.ID, TicketID: tc.row.ID, Number: q.Number})
		return err
	})
	if err != nil && !errors.Is(err, store.ErrNoChange) {
		return nil, err
	}
	return apigen.UpdateQuestion200JSONResponse{Body: questionView(out), Headers: apigen.UpdateQuestion200ResponseHeaders{ETag: etag(out.Version)}}, nil
}

// mayEdit holds an edit to the asker and to an open question.
func mayEdit(p auth.Principal, tc ticketCtx, q question) *problem.Error {
	if perr := auth.Authorize(p, tc.role, work); perr != nil {
		return perr
	}
	if q.AskedBy != p.PersonID {
		return problem.New(problem.Forbidden, "only the asker edits a question")
	}
	if q.Status != questionOpen {
		return &problem.Error{Code: problem.StateConflict, Detail: "the question is " + q.Status,
			Errors: []problem.FieldError{{Pointer: "/", Message: "not open", Current: q.Status}}}
	}
	return nil
}

func applyQuestionPatch(body apigen.QuestionPatch, up *writeq.UpdateQuestionParams) {
	if body.Question != nil {
		up.Question = *body.Question
	}
	if body.Options != nil {
		up.Options = *body.Options
	}
	if body.Recommendation != nil {
		up.Recommendation = *body.Recommendation
	}
	if body.AskedOf.IsSpecified() {
		up.AskedOf = nil
		if !body.AskedOf.IsNull() {
			person := body.AskedOf.MustGet()
			up.AskedOf = &person
		}
	}
}

// askedAnew is the person an edit asks a question of who was not asked
// before; nil when it is asked of nobody or of the same person.
func askedAnew(before, after *uuid.UUID) *uuid.UUID {
	if after == nil || (before != nil && *before == *after) {
		return nil
	}
	return after
}

func questionFields(p writeq.UpdateQuestionParams) map[string]any {
	var askedOf any
	if p.AskedOf != nil {
		askedOf = p.AskedOf.String()
	}
	return map[string]any{"question": p.Question, "options": p.Options, "recommendation": p.Recommendation, "asked_of": askedOf}
}

// AnswerQuestion records a person's answer (docs/adr/0011 D2): the person
// asked, or any member when the question is open in the tenant; a person
// changes their own answer. An agent writes its person's answer down with
// record-answer and changes only an answer an agent recorded
// (docs/adr/0066 D8).
func (s *Server) AnswerQuestion(ctx context.Context, req apigen.AnswerQuestionRequestObject) (apigen.AnswerQuestionResponseObject, error) {
	t := tenantFrom(ctx)
	var out question
	_, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		tc, q, err := visibleQuestion(ctx, w.Reader, t, req.Project, req.Number, req.Question)
		if err != nil {
			return err
		}
		p := principal(ctx)
		if perr := mayAnswer(p, tc, q, req.Params.IfMatch); perr != nil {
			return perr
		}
		if q.Answer != nil && *q.Answer == req.Body.Answer {
			out = q
			return store.ErrNoChange
		}
		answer := writeq.AnswerQuestionParams{TenantID: t.ID, ID: q.ID, Version: q.Version,
			Answer: &req.Body.Answer, AnsweredBy: &p.PersonID, RecordedByAgent: p.IsAgent()}
		answer.AnsweredByTokenID, answer.AnsweredByTokenName = actToken(p)
		if _, err := w.AnswerQuestion(ctx, answer); errors.Is(err, pgx.ErrNoRows) {
			return stale(q.Version, map[string]any{fieldAnswer: q.Answer})
		} else if err != nil {
			return err
		}
		w.Record(store.Event{EntityType: entityQuestion, EntityID: q.ID, TicketID: tc.row.ID, TicketKey: ticketKey(t, tc.row),
			Action: questionAnswered, Before: map[string]any{fieldAnswer: q.Answer},
			After:   map[string]any{fieldAnswer: req.Body.Answer, "recorded_by_agent": p.IsAgent()},
			Notices: []store.Notice{{Reason: store.NoticeAnswered, People: []uuid.UUID{q.AskedBy}}}})
		out, err = w.GetQuestion(ctx, readq.GetQuestionParams{TenantID: t.ID, TicketID: tc.row.ID, Number: q.Number})
		return err
	})
	if err != nil && !errors.Is(err, store.ErrNoChange) {
		return nil, err
	}
	return apigen.AnswerQuestion200JSONResponse{Body: questionView(out), Headers: apigen.AnswerQuestion200ResponseHeaders{ETag: etag(out.Version)}}, nil
}

// mayAnswer holds an answer to the rules of docs/adr/0011 D2 and
// docs/adr/0066 D8; a changed answer needs the version read.
func mayAnswer(p auth.Principal, tc ticketCtx, q question, ifm *string) *problem.Error {
	need := work
	if p.IsAgent() {
		need.Capability = auth.CapRecordAnswer
	}
	if perr := auth.Authorize(p, tc.role, need); perr != nil {
		return perr
	}
	switch q.Status {
	case questionWithdrawn:
		return &problem.Error{Code: problem.StateConflict, Detail: "the question is withdrawn",
			Errors: []problem.FieldError{{Pointer: "/", Message: "withdrawn", Current: q.Status}}}
	case questionOpen:
		if q.AskedOf != nil && *q.AskedOf != p.PersonID {
			return problem.New(problem.Forbidden, "the question is asked of another person")
		}
		return nil
	}
	version, perr := ifMatch(ifm)
	if perr != nil {
		return perr
	}
	switch {
	case q.AnsweredBy == nil || *q.AnsweredBy != p.PersonID:
		return problem.New(problem.Forbidden, "the answer is another person's")
	case p.IsAgent() && !q.RecordedByAgent:
		return problem.New(problem.AgentForbidden, "an agent changes only an answer an agent recorded")
	case version != q.Version:
		return stale(q.Version, map[string]any{fieldAnswer: q.Answer})
	}
	return nil
}

// WithdrawQuestion withdraws an open question: the asker's act; an agent
// withdraws only what an agent of the same person asked (docs/adr/0011 D2).
func (s *Server) WithdrawQuestion(ctx context.Context, req apigen.WithdrawQuestionRequestObject) (apigen.WithdrawQuestionResponseObject, error) {
	t := tenantFrom(ctx)
	var out question
	_, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		tc, q, err := visibleQuestion(ctx, w.Reader, t, req.Project, req.Number, req.Question)
		if err != nil {
			return err
		}
		p := principal(ctx)
		if perr := auth.Authorize(p, tc.role, work); perr != nil {
			return perr
		}
		switch {
		case q.AskedBy != p.PersonID:
			return problem.New(problem.Forbidden, "only the asker withdraws a question")
		case p.IsAgent() && q.AskedByAgent == nil:
			return problem.New(problem.AgentForbidden, "an agent withdraws only a question an agent asked")
		}
		if q.Status == questionOpen {
			_, err = w.WithdrawQuestion(ctx, writeq.WithdrawQuestionParams{TenantID: t.ID, ID: q.ID, WithdrawnBy: &p.PersonID})
			if err == nil {
				w.Record(store.Event{EntityType: entityQuestion, EntityID: q.ID, TicketID: tc.row.ID, TicketKey: ticketKey(t, tc.row),
					Action: questionWithdrawn, After: map[string]any{"number": q.Number}})
				out, err = w.GetQuestion(ctx, readq.GetQuestionParams{TenantID: t.ID, TicketID: tc.row.ID, Number: q.Number})
				return err
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("withdraw the question: %w", err)
			}
			// A simultaneous withdrawal or answer came first.
			if q, err = w.GetQuestion(ctx, readq.GetQuestionParams{TenantID: t.ID, TicketID: tc.row.ID, Number: q.Number}); err != nil {
				return err
			}
		}
		if q.Status == questionWithdrawn {
			out = q
			return store.ErrNoChange
		}
		return &problem.Error{Code: problem.StateConflict, Detail: "the question is " + q.Status,
			Errors: []problem.FieldError{{Pointer: "/", Message: "not open", Current: q.Status}}}
	})
	if err != nil && !errors.Is(err, store.ErrNoChange) {
		return nil, err
	}
	return apigen.WithdrawQuestion200JSONResponse{Body: questionView(out), Headers: apigen.WithdrawQuestion200ResponseHeaders{ETag: etag(out.Version)}}, nil
}
