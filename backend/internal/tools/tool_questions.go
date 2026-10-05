package tools

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
)

type openQuestionInput struct {
	Key            string `json:"key"`
	Question       string `json:"question" jsonschema:"one question, answerable on its own"`
	Options        string `json:"options" jsonschema:"Markdown: the context and the sensible options, researched"`
	Recommendation string `json:"recommendation" jsonschema:"the recommended option and why"`
	AskedOf        string `json:"asked_of,omitempty" jsonschema:"me for the person this session works for, a username, or a person id; left out, the question is open to the tenant"`
}

func openQuestionTool() Tool {
	return define(Tool{
		Name: "open_question",
		Description: "Open a decision on a ticket as a question for a person: the context, the options, the recommended one " +
			"with its reason. One question at a time: wait for the answer before the next. The answer is the person's — " +
			"in the UI, or in chat, where you write it down with record_answer; never answer it yourself.",
		Operations: []string{"askQuestion", "listQuestions", opGetMe, opListMembers},
		limits:     limitsOf(refusalNote),
	}, func(s *jsonschema.Schema) {
		minLen := 1
		s.Properties["question"].MinLength = &minLen
	}, runOpenQuestion)
}

func runOpenQuestion(ctx context.Context, s *Session, in openQuestionInput) (string, error) {
	ref, err := s.resolveKey(in.Key)
	if err != nil {
		return "", err
	}
	body := apigen.QuestionCreate{Question: in.Question, Options: &in.Options, Recommendation: &in.Recommendation}
	asked := "open to the tenant"
	if strings.TrimSpace(in.AskedOf) != "" {
		id, name, err := person(ctx, s, ref.Tenant, in.AskedOf)
		if err != nil {
			return "", err
		}
		body.AskedOf, asked = &id, "asked of "+name
	}
	res, err := s.API.AskQuestionWithResponse(ctx, ref.Tenant, ref.Project, int(ref.Number),
		&apigen.AskQuestionParams{IdempotencyKey: s.key()}, body)
	if err := check(res, err, http.StatusCreated); err != nil {
		return "", err
	}
	q := res.JSON201
	out := fmt.Sprintf("Asked Q%d on %s, %s: %s\nThe decision is the person's. Wait for the answer; when they give it in chat, record it with record_answer(key: %q, question: %d).",
		q.Number, ref.Full(), asked, q.Question, ref.Full(), q.Number)
	open, err := s.API.ListQuestionsWithResponse(ctx, ref.Tenant, ref.Project, int(ref.Number), &apigen.ListQuestionsParams{})
	if err == nil && open.JSON200 != nil {
		others := 0
		for _, o := range open.JSON200.Items {
			if o.Status == apigen.QuestionStatusOpen && o.Number != q.Number {
				others++
			}
		}
		if others > 0 {
			out += fmt.Sprintf("\n%d more questions are open on this ticket: put them to the person one at a time.", others)
		}
	}
	return out, nil
}

// person resolves who a question is asked of: me, a username or a display
// name of a member of the tenant, or a person id.
func person(ctx context.Context, s *Session, tenant, who string) (openapi_types.UUID, string, error) {
	who = strings.TrimSpace(who)
	if id, ok := uuidOf(who); ok {
		return id, who, nil
	}
	if strings.EqualFold(who, "me") {
		me, err := s.Me(ctx)
		if err != nil {
			return openapi_types.UUID{}, "", err
		}
		return me.ID, me.Name, nil
	}
	var cursor *string
	for range 20 {
		res, err := s.API.ListMembersWithResponse(ctx, tenant, &apigen.ListMembersParams{Cursor: cursor})
		if err := check(res, err, http.StatusOK); err != nil {
			return openapi_types.UUID{}, "", err
		}
		for _, m := range res.JSON200.Items {
			username, _ := m.Person.Username.Get()
			if strings.EqualFold(username, who) || strings.EqualFold(m.Person.DisplayName, who) {
				return m.Person.Id, m.Person.DisplayName, nil
			}
		}
		next, err := res.JSON200.NextCursor.Get()
		if err != nil || next == "" {
			break
		}
		cursor = &next
	}
	return openapi_types.UUID{}, "", usage("%q is no member of the tenant %s: name a username, a display name, me, or a person id", who, tenant)
}

type recordAnswerInput struct {
	Key      string `json:"key" jsonschema:"the ticket the question is on"`
	Question int    `json:"question" jsonschema:"the question's number, Q<n>"`
	Answer   string `json:"answer" jsonschema:"the answer the person gave, in their words"`
}

func recordAnswerTool() Tool {
	return define(Tool{
		Name: "record_answer",
		Description: "Write down the answer a person gave in chat to an open question (docs/adr/0066 D8): the answer stays " +
			"theirs and is marked as recorded by the agent. Only an answer the person actually gave — never one you chose. " +
			"Changing an answer an agent recorded is allowed; one the person wrote themselves is theirs to change.",
		Operations: []string{"getQuestion", "answerQuestion"},
		limits:     limitsOf("Recording an answer needs the record-answer capability. "+refusalNote, capRecordAnswer),
	}, func(s *jsonschema.Schema) {
		bound(s, "question", 1, 2147483647)
		minLen := 1
		s.Properties["answer"].MinLength = &minLen
	}, func(ctx context.Context, s *Session, in recordAnswerInput) (string, error) {
		ref, err := s.resolveKey(in.Key)
		if err != nil {
			return "", err
		}
		got, err := s.API.GetQuestionWithResponse(ctx, ref.Tenant, ref.Project, int(ref.Number), in.Question)
		if err := check(got, err, http.StatusOK); err != nil {
			return "", err
		}
		params := &apigen.AnswerQuestionParams{}
		if got.JSON200.Status == apigen.QuestionStatusAnswered {
			etag := got.HTTPResponse.Header.Get("ETag")
			params.IfMatch = &etag
		}
		res, err := s.API.AnswerQuestionWithResponse(ctx, ref.Tenant, ref.Project, int(ref.Number), in.Question, params,
			apigen.AnswerSet{Answer: in.Answer})
		if err := check(res, err, http.StatusOK); err != nil {
			return "", err
		}
		by := "the person"
		if p, err := res.JSON200.AnsweredBy.Get(); err == nil {
			by = p.DisplayName
		}
		return fmt.Sprintf("Recorded %s's answer to Q%d on %s, marked as recorded by the agent.", by, in.Question, ref.Full()), nil
	})
}
