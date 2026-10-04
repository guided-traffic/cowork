package tools

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/domain"
)

var blockKinds = []string{"decision", "human", "product", "release", "external", "ticket"}

type transitionInput struct {
	Key          string `json:"key"`
	To           string `json:"to" jsonschema:"the state to move to"`
	ReasonOrNote string `json:"reason_or_note,omitempty" jsonschema:"the verification note for done — what was run, against what, with what result — or the reason a backward move, a block, a drop, a reopen or a withdrawal needs"`
	BlockKind    string `json:"block_kind,omitempty" jsonschema:"what a ticket entering blocked waits on"`
	BlockedBy    string `json:"blocked_by,omitempty" jsonschema:"the ticket it waits on, with block_kind ticket"`
	Comment      string `json:"comment,omitempty" jsonschema:"a comment that explains the move, written with it"`
	From         string `json:"from,omitempty" jsonschema:"the state the ticket was read in; the move is refused when the ticket is no longer there. Left out, the state read now"`
}

func transitionTool() Tool {
	return define(Tool{
		Name: "transition",
		Description: "Move a ticket to another state (docs/adr/0009): forward one step filed → analysed → decided → in-progress → " +
			"review, back with a reason, into blocked with a reason and a block kind and out to where it came from, to done " +
			"with a verification note, to dropped with a reason, dropped → filed with a reason. The current state is read " +
			"first and sent as the move's precondition.",
		Operations: []string{opGetTicket, "transitionTicket"},
		limits: limitsOf("An agent needs decide for analysed → decided, close for done — and closes only from in-progress or "+
			"review — and drop for dropped. Open prerequisites refuse done, and only a person may close over them. "+refusalNote,
			capDecide, capClose, capDrop),
	}, func(s *jsonschema.Schema) {
		enum(s, "to", ticketStates...)
		enum(s, "from", ticketStates...)
		enum(s, "block_kind", blockKinds...)
	}, runTransition)
}

func runTransition(ctx context.Context, s *Session, in transitionInput) (string, error) {
	ref, err := s.resolveKey(in.Key)
	if err != nil {
		return "", err
	}
	tk, _, err := getTicket(ctx, s, ref)
	if err != nil {
		return "", err
	}
	to := apigen.TicketState(in.To)
	body := apigen.Transition{From: tk.State, To: to}
	if in.From != "" {
		// The state the caller read is the move's precondition: a ticket
		// that moved meanwhile refuses it (docs/adr/0045 D2).
		body.From = apigen.TicketState(in.From)
	}
	text := strings.TrimSpace(in.ReasonOrNote)
	switch {
	case to == apigen.TicketStateDone && text == "":
		return "", usage("done needs a verification note in reason_or_note: what was run, against what, with what result")
	case to == apigen.TicketStateDone:
		body.Note = &text
	case text != "":
		body.Reason = &text
	}
	if to == apigen.TicketStateBlocked {
		if in.BlockKind == "" {
			return "", usage("blocked needs block_kind: %s", strings.Join(blockKinds, ", "))
		}
		block := apigen.BlockSet{Kind: apigen.BlockKind(in.BlockKind)}
		if in.BlockedBy != "" {
			block.Ticket = &in.BlockedBy
		}
		body.Block = &block
	}
	if in.Comment != "" {
		body.Comment = &in.Comment
	}
	moved, err := move(ctx, s, ref, body)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Moved %s from %s to %s.", moved.Key, body.From, moved.State), nil
}

// move sends a transition with its Idempotency-Key, which the act records
// (docs/adr/0045 D2, D7).
func move(ctx context.Context, s *Session, ref ticketRef, body apigen.Transition) (apigen.Ticket, error) {
	res, err := s.API.TransitionTicketWithResponse(ctx, ref.Tenant, ref.Project, int(ref.Number),
		&apigen.TransitionTicketParams{IdempotencyKey: s.key()}, body)
	if err := check(res, err, http.StatusOK); err != nil {
		return apigen.Ticket{}, err
	}
	return *res.JSON200, nil
}

type setProgressInput struct {
	Key     string `json:"key"`
	Percent int    `json:"percent" jsonschema:"0 to 100 in steps of five"`
	Stage   string `json:"stage,omitempty" jsonschema:"refinement, implementation (the default) or review"`
	Note    string `json:"note,omitempty" jsonschema:"the verification note, needed when this brings the last of the three stages to 100, which closes the ticket"`
	Reason  string `json:"reason,omitempty" jsonschema:"needed when lowering a stage reopens a ticket its stages closed"`
	Comment string `json:"comment,omitempty" jsonschema:"a comment that explains the change, written with it"`
	Version *int   `json:"version,omitempty" jsonschema:"the ticket's version when it was read; the write is refused when the ticket changed since. Left out, the version read now"`
}

func setProgressTool() Tool {
	return define(Tool{
		Name: "set_progress",
		Description: "Set one of a ticket's three progress stages — refinement, implementation, review — to 0..100 in steps of " +
			"five (docs/adr/0017 D2). A ticket with children derives its stages and takes none. The write that brings the " +
			"last stage to 100 closes the ticket and needs a note; lowering a stage of a ticket its stages closed reopens it " +
			"and needs a reason.",
		Operations: []string{opGetTicket, "updateTicket"},
		limits:     limitsOf("Closing by the stages needs close, from in-progress or review; without it that write is refused whole. "+refusalNote, capClose),
	}, func(s *jsonschema.Schema) {
		bound(s, "percent", 0, 100)
		bound(s, "version", 1, 2147483647)
		five := 5.0
		s.Properties["percent"].MultipleOf = &five
		enum(s, "stage", "refinement", "implementation", "review")
	}, func(ctx context.Context, s *Session, in setProgressInput) (string, error) {
		ref, err := s.resolveKey(in.Key)
		if err != nil {
			return "", err
		}
		before, etag, err := getTicket(ctx, s, ref)
		if err != nil {
			return "", err
		}
		if in.Version != nil {
			etag = strconv.Quote(strconv.Itoa(*in.Version))
		}
		patch := apigen.TicketPatch{}
		stage := in.Stage
		switch stage {
		case "refinement":
			patch.ProgressRefinement = &in.Percent
		case stateReview:
			patch.ProgressReview = &in.Percent
		default:
			stage, patch.Progress = "implementation", &in.Percent
		}
		if in.Note != "" {
			patch.Note = &in.Note
		}
		if in.Reason != "" {
			patch.Reason = &in.Reason
		}
		if in.Comment != "" {
			patch.Comment = &in.Comment
		}
		after, err := patchTicket(ctx, s, ref, etag, patch)
		if err != nil {
			return "", err
		}
		out := fmt.Sprintf("Set the %s stage of %s to %d%%.", stage, after.Key, in.Percent)
		if after.State != before.State {
			out += fmt.Sprintf(" The ticket moved from %s to %s.", before.State, after.State)
		}
		return out, nil
	})
}

// patchTicket changes a ticket's fields with the version read (docs/adr/0050).
func patchTicket(ctx context.Context, s *Session, ref ticketRef, etag string, patch apigen.TicketPatch) (apigen.Ticket, error) {
	res, err := s.API.UpdateTicketWithResponse(ctx, ref.Tenant, ref.Project, int(ref.Number),
		&apigen.UpdateTicketParams{IfMatch: &etag}, patch)
	if err := check(res, err, http.StatusOK); err != nil {
		return apigen.Ticket{}, err
	}
	return *res.JSON200, nil
}

type finishWorkInput struct {
	Key              string `json:"key"`
	VerificationNote string `json:"verification_note" jsonschema:"what was run, against what, with what result"`
	From             string `json:"from,omitempty" jsonschema:"the state the ticket was read in; nothing is done when the ticket is no longer there"`
}

func finishWorkTool() Tool {
	return define(Tool{
		Name: "finish_work",
		Description: "Finish the work on a ticket: write the verification note as a comment, set the implementation stage " +
			"to 100, and make the furthest move this token may — done from in-progress or review with close, else review " +
			"— then report what remains for a person and what the repository still needs: the ADR, page or security gap " +
			"the ticket holds (docs/adr/0069 D5).",
		Operations: []string{opGetTicket, "addComment", "updateTicket", "transitionTicket", "getMyToken"},
		limits:     limitsOf("Closing needs close and a ticket in in-progress or review; open prerequisites stay a person's to override. "+refusalNote, capClose),
	}, func(s *jsonschema.Schema) {
		minLen := 1
		s.Properties["verification_note"].MinLength = &minLen
		enum(s, "from", ticketStates...)
	}, runFinishWork)
}

func runFinishWork(ctx context.Context, s *Session, in finishWorkInput) (string, error) {
	ref, err := s.resolveKey(in.Key)
	if err != nil {
		return "", err
	}
	tk, etag, err := getTicket(ctx, s, ref)
	if err != nil {
		return "", err
	}
	if in.From != "" && string(tk.State) != in.From {
		return "", usage("%s is %s now, not %s as it was read: nothing was done. Read it again and decide anew", ref.Full(), tk.State, in.From)
	}
	tok := s.Token()
	if !tok.Known {
		if tok, err = s.ReadToken(ctx); err != nil {
			return "", err
		}
	}
	note := strings.TrimSpace(in.VerificationNote)
	done, remains := make([]string, 0, 4), []string{}
	res, err := s.API.AddCommentWithResponse(ctx, ref.Tenant, ref.Project, int(ref.Number),
		&apigen.AddCommentParams{IdempotencyKey: s.key()}, apigen.CommentWrite{Body: "Verification: " + note})
	if err := check(res, err, http.StatusCreated); err != nil {
		return "", err
	}
	done = append(done, "wrote the verification note as a comment")
	closable := tok.Can(capClose) && domain.TicketState(tk.State).AgentCloses()
	if tk, err = finishStages(ctx, s, ref, tk, etag, closable, note, &done); err != nil {
		return "", err
	}
	if tk.State != apigen.TicketStateDone {
		tk, err = finishMove(ctx, s, ref, tk, tok, note, &done, &remains)
		if err != nil {
			return "", err
		}
	}
	var out strings.Builder
	fmt.Fprintf(&out, "Finished the work on %s (now %s): %s.\n", ref.Full(), tk.State, strings.Join(done, "; "))
	if len(remains) > 0 {
		out.WriteString("\nWhat remains for a person:\n")
		for _, r := range remains {
			out.WriteString("- " + r + "\n")
		}
	}
	out.WriteString("\nBefore the session ends, in the repository: does the ticket hold a decision that must become an ADR, a " +
		"behaviour that must reach an operations or developer page, or a gap for a security page? Extract it now " +
		"(docs/adr/0069 D5).\n")
	out.WriteString(commitLines(ref, string(tk.Type), tk.Title) + "\n")
	return out.String(), nil
}

// finishStages sets the implementation stage to 100 where that changes
// nothing else, or closes the ticket by its stages where it would and may
// (docs/adr/0009 D5).
func finishStages(ctx context.Context, s *Session, ref ticketRef, tk apigen.Ticket, etag string, closable bool, note string, done *[]string) (apigen.Ticket, error) {
	if tk.ProgressDerived || tk.Progress >= 100 || tk.State == apigen.TicketStateDone || tk.State == apigen.TicketStateDropped {
		return tk, nil
	}
	full := 100
	patch := apigen.TicketPatch{Progress: &full}
	closes := tk.ProgressRefinement == 100 && tk.ProgressReview == 100
	switch {
	case closes && !closable:
		return tk, nil
	case closes:
		patch.Note = &note
	}
	after, err := patchTicket(ctx, s, ref, etag, patch)
	var api *APIError
	if errors.As(err, &api) && api.Status == http.StatusConflict {
		// Open prerequisites refuse the close by the stages; the move reports them.
		return tk, nil
	}
	if err != nil {
		return tk, err
	}
	*done = append(*done, "set the implementation stage to 100")
	if after.State == apigen.TicketStateDone {
		*done = append(*done, "closed it: its three stages are full")
	}
	return after, nil
}

// finishMove makes the furthest transition the token may: done from
// in-progress or review with close, else review from in-progress
// (docs/adr/0043 D6).
func finishMove(ctx context.Context, s *Session, ref ticketRef, tk apigen.Ticket, tok Token, note string, done, remains *[]string) (apigen.Ticket, error) {
	state := domain.TicketState(tk.State)
	switch {
	case !state.AgentCloses():
		*remains = append(*remains, fmt.Sprintf("the ticket is %s: an agent closes from in-progress or review only, so its next move is a person's or a later step's", tk.State))
		return tk, nil
	case tok.Can(capClose):
		moved, err := move(ctx, s, ref, apigen.Transition{From: tk.State, To: apigen.TicketStateDone, Note: &note})
		var api *APIError
		switch {
		case err == nil:
			*done = append(*done, "closed it, done by hand with the note")
			return moved, nil
		case errors.As(err, &api) && api.Code() == "open_prerequisites":
			*remains = append(*remains, "closing it: "+strings.TrimPrefix(api.Error(), "cowork answered ")+" — a person may close over them with a reason")
		default:
			return tk, err
		}
	default:
		*remains = append(*remains, "closing it: this token lacks close, so done is the person's")
	}
	if tk.State == apigen.TicketStateInProgress {
		moved, err := move(ctx, s, ref, apigen.Transition{From: tk.State, To: apigen.TicketStateReview})
		if err != nil {
			return tk, err
		}
		*done = append(*done, "moved it to review")
		return moved, nil
	}
	return tk, nil
}
