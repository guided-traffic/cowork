package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"unicode"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/llm"
)

// policy is how the chat runs the calls of a tool (docs/adr/0076, the answer to
// the open question on confirmations).
type policy int

const (
	// read runs at once: the tool changes nothing.
	read policy = iota + 1
	// write runs at once, and waits for the person once the conversation has
	// read a confidential ticket.
	write
	// decision always waits for the person.
	decision
	// move is a transition: it waits for the acts a person owes a reason or a
	// note for (docs/adr/0009) and for the decide gate.
	move
	// stages is a progress write: it waits where it closes or reopens the
	// ticket.
	stages
	// left is a tool the chat does not offer.
	left
)

// policies classify every tool the chat can meet: the shared catalogue's and
// its own. A tool that is not here is not offered, and a test fails while the
// shared catalogue holds one (TestEveryToolIsClassified).
var policies = map[string]policy{
	"session_start": left, "api": left,
	"get_ticket": read, "search": read, "open_ticket": read, "open_backlog": read, "open_board": read,
	"file_ticket": write, "record_state": write, "open_question": write, "comment": write, "link": write, "watch": write,
	"set_urgency":   write,
	"record_answer": decision, "create_project": decision, "finish_work": decision,
	toolTransition: move, toolSetProgress: stages,
}

// The tools whose arguments the review reads.
const (
	toolTransition  = "transition"
	toolSetProgress = "set_progress"
	toolFinishWork  = "finish_work"
)

// verdict is the review of a call: whether it waits for the person, the act
// in words, and the arguments with what the review read pinned into them.
type verdict struct {
	propose bool
	what    string
	args    apigen.ChatToolArguments
}

// review decides whether a call waits for the person. It fails closed: a call
// whose act it cannot tell — arguments it cannot read, a ticket it cannot
// read where the act depends on it — waits. A call the tool refuses before
// it acts — an unknown tool, arguments the schema refuses — runs, and is
// refused.
func (r *runner) review(ctx context.Context, call apigen.ChatToolCall) verdict {
	tool, known := r.tools[call.Name]
	p := policies[call.Name]
	if !known || p == read || r.bad[call.Id] != "" || !tool.Valid(call.Arguments) {
		return verdict{}
	}
	var args map[string]any
	if json.Unmarshal(call.Arguments, &args) != nil {
		return r.waits(call, nil, fmt.Sprintf("Run %s with arguments the chat cannot read.", plain(call.Name, 64)))
	}
	switch p {
	case move:
		return r.reviewMove(ctx, call, args)
	case stages:
		return r.reviewStages(ctx, call, args)
	case decision:
		return r.reviewDecision(ctx, call, args)
	case write:
		if r.tainted {
			return r.waits(call, args, describeWrite(call.Name, args)+" "+taintedReason)
		}
	}
	return verdict{}
}

// taintedReason says why a write waits that would run otherwise.
const taintedReason = "It waits because this conversation read a confidential ticket: every write waits for your decision."

// reviewMove holds a transition: to done, dropped, blocked and decided it
// always waits; backward, a reopen and the withdrawal of a done wait once the
// ticket is read, and wait as well when it cannot be.
func (r *runner) reviewMove(ctx context.Context, call apigen.ChatToolCall, args map[string]any) verdict {
	to, text := str(args, "to"), str(args, "reason_or_note")
	tk, ok := r.ticket(ctx, str(args, "key"))
	what, from := ticketOf(tk, ok, str(args, "key")), "?"
	if ok {
		from = string(tk.State)
		args = pin(args, "from", from)
	}
	switch to {
	case string(apigen.TicketStateDone):
		return r.waits(call, args, fmt.Sprintf("Close %s: move it from %s to done, with the verification note %s.", what, from, quote(text)))
	case string(apigen.TicketStateDropped):
		return r.waits(call, args, fmt.Sprintf("Drop %s (now %s), with the reason %s.", what, from, quote(text)))
	case string(apigen.TicketStateBlocked):
		return r.waits(call, args, fmt.Sprintf("Block %s (now %s): it waits on %s, with the reason %s.", what, from,
			orNone(plain(str(args, "block_kind"), 32)), quote(text)))
	case string(apigen.TicketStateDecided):
		return r.waits(call, args, fmt.Sprintf("Decide %s: move it from %s to decided%s.", what, from, withReason(text)))
	}
	if !ok {
		return r.waits(call, args, fmt.Sprintf("Move %s to %s. The chat could not read the ticket to tell what the move is.", what, plain(to, 32)))
	}
	switch domain.ClassifyMove(domain.TicketState(tk.State), domain.TicketState(to), cameFrom(tk)) {
	case domain.MoveBackward:
		return r.waits(call, args, fmt.Sprintf("Move %s back from %s to %s, with the reason %s.", what, from, to, quote(text)))
	case domain.MoveReopen:
		return r.waits(call, args, fmt.Sprintf("Reopen %s: move it from dropped to filed, with the reason %s.", what, quote(text)))
	case domain.MoveWithdraw:
		return r.waits(call, args, fmt.Sprintf("Take back the done of %s: move it to %s, with the reason %s.", what, to, quote(text)))
	}
	if r.tainted {
		return r.waits(call, args, fmt.Sprintf("Move %s from %s to %s. %s", what, from, to, taintedReason))
	}
	return verdict{}
}

// reviewStages holds a progress write: it waits where it fills the last stage
// of a ticket, which closes it, or lowers a stage of a ticket its stages
// closed, which reopens it — and when the ticket cannot be read.
func (r *runner) reviewStages(ctx context.Context, call apigen.ChatToolCall, args map[string]any) verdict {
	percent, stage := num(args, "percent"), str(args, "stage")
	tk, ok := r.ticket(ctx, str(args, "key"))
	what := ticketOf(tk, ok, str(args, "key"))
	if !ok {
		return r.waits(call, args, fmt.Sprintf("Set a progress stage of %s to %d%%. The chat could not read the ticket to tell whether that closes or reopens it.",
			what, percent))
	}
	args = pin(args, "version", tk.Version)
	if desc := progressProposal(tk, what, percent, stage, str(args, "note"), str(args, "reason")); desc != "" {
		return r.waits(call, args, desc)
	}
	if r.tainted {
		return r.waits(call, args, fmt.Sprintf("Set the %s stage of %s to %d%%. %s", stageName(stage), what, percent, taintedReason))
	}
	return verdict{}
}

// reviewDecision describes the acts that always wait: finish_work, which
// closes where it may, recording a person's answer, and creating a project.
func (r *runner) reviewDecision(ctx context.Context, call apigen.ChatToolCall, args map[string]any) verdict {
	switch call.Name {
	case toolFinishWork:
		tk, ok := r.ticket(ctx, str(args, "key"))
		if ok {
			args = pin(args, "from", string(tk.State))
		}
		return r.waits(call, args, fmt.Sprintf("Finish the work on %s: write the verification note %s as a comment, set the "+
			"implementation stage to 100, and close the ticket where it is in progress or in review — else move it to review.",
			ticketOf(tk, ok, str(args, "key")), quote(str(args, "verification_note"))))
	case "record_answer":
		tk, ok := r.ticket(ctx, str(args, "key"))
		return r.waits(call, args, fmt.Sprintf("Record %s as your answer to Q%d on %s — an answer you gave, written down for you.",
			quote(str(args, "answer")), num(args, "question"), ticketOf(tk, ok, str(args, "key"))))
	case "create_project":
		return r.waits(call, args, fmt.Sprintf("Create the project %s/%s, %s, and bind the repository %s to it.",
			plain(str(args, "tenant"), 64), plain(str(args, "key"), 16), quote(str(args, "name")), quote(str(args, "remote"))))
	}
	return r.waits(call, args, describeWrite(call.Name, args))
}

// waits is the verdict that the call waits, its arguments as pinned.
func (r *runner) waits(call apigen.ChatToolCall, args map[string]any, what string) verdict {
	v := verdict{propose: true, what: what, args: call.Arguments}
	if args != nil {
		if b, err := json.Marshal(args); err == nil {
			v.args = b
		}
	}
	return v
}

// ticket reads the ticket a call names, in the turn's tenant.
func (r *runner) ticket(ctx context.Context, raw string) (apigen.Ticket, bool) {
	k, err := domain.ParseTicketKey(strings.TrimSpace(raw))
	if err != nil || (k.Tenant != "" && k.Tenant != r.t.Tenant) {
		return apigen.Ticket{}, false
	}
	res, err := r.t.Session.API.GetTicketWithResponse(ctx, r.t.Tenant, k.Project, int(k.Number))
	if err != nil || res.StatusCode() != http.StatusOK || res.JSON200 == nil {
		return apigen.Ticket{}, false
	}
	return *res.JSON200, true
}

// cameFrom is where a blocked ticket came from, or a done ticket was done from.
func cameFrom(tk apigen.Ticket) domain.TicketState {
	if b, err := tk.Block.Get(); err == nil {
		return domain.TicketState(b.From)
	}
	if d, err := tk.DoneFrom.Get(); err == nil {
		return domain.TicketState(d)
	}
	return ""
}

func progressProposal(tk apigen.Ticket, what string, percent int, stage, note, reason string) string {
	if tk.ProgressDerived {
		return ""
	}
	stage = stageName(stage)
	stages := map[string]int{"refinement": tk.ProgressRefinement, "implementation": tk.Progress, "review": tk.ProgressReview}
	before := stages[stage]
	stages[stage] = percent
	full := stages["refinement"] == 100 && stages["implementation"] == 100 && stages["review"] == 100
	switch {
	case full && tk.State != apigen.TicketStateDone && tk.State != apigen.TicketStateDropped:
		return fmt.Sprintf("Set the %s stage of %s to 100%%: all three stages are full, which closes the ticket, with the note %s.",
			stage, what, quote(note))
	case tk.State == apigen.TicketStateDone && !tk.DoneByHand && percent < before:
		return fmt.Sprintf("Lower the %s stage of %s to %d%%, which reopens the ticket its stages closed, with the reason %s.",
			stage, what, percent, quote(reason))
	}
	return ""
}

func stageName(stage string) string {
	if stage == "refinement" || stage == "review" {
		return stage
	}
	return "implementation"
}

// describeWrite is a write that would run at once, in words.
func describeWrite(name string, args map[string]any) string {
	key := plain(str(args, "key"), 80)
	switch name {
	case "file_ticket":
		return fmt.Sprintf("File the ticket %s in %s.", quote(str(args, "title")), orNone(plain(str(args, "project"), 80)))
	case "comment":
		return fmt.Sprintf("Comment on %s: %s.", key, quote(str(args, "text")))
	case "record_state":
		return fmt.Sprintf("Replace the body of %s with %s.", key, quote(str(args, "body")))
	case "open_question":
		return fmt.Sprintf("Ask on %s: %s.", key, quote(str(args, "question")))
	case "link":
		return fmt.Sprintf("Link %s %s %s.", key, plain(str(args, "type"), 16), plain(str(args, "other_key"), 80))
	case "watch":
		return fmt.Sprintf("Watch %s.", key)
	case "set_urgency":
		if args["withdraw"] == true {
			return fmt.Sprintf("Withdraw the urgency override of %s.", key)
		}
		return fmt.Sprintf("Set the urgency of %s to %s, with the reason %s.", key, plain(str(args, "urgency"), 16), quote(str(args, "reason")))
	}
	return fmt.Sprintf("Run %s on %s.", plain(name, 64), orNone(key))
}

// pin sets what the review read into the call's arguments: the call then
// carries it as its precondition, and the API refuses the call when the
// ticket moved before the person decided (docs/adr/0045 D2, docs/adr/0050).
func pin(args map[string]any, field string, value any) map[string]any {
	out := make(map[string]any, len(args)+1)
	for k, v := range args {
		out[k] = v
	}
	out[field] = value
	return out
}

func str(args map[string]any, key string) string {
	s, _ := args[key].(string)
	return s
}

func num(args map[string]any, key string) int {
	f, _ := args[key].(float64)
	return int(f)
}

// ticketOf names a ticket for the person: its canonical key and title as the
// API read them, or the key the model wrote.
func ticketOf(tk apigen.Ticket, read bool, raw string) string {
	if !read {
		return orNone(plain(raw, 80))
	}
	return tk.Key + " — " + quote(tk.Title)
}

func withReason(text string) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	return ", with the reason " + quote(text)
}

// quote is a text the model or another person wrote, made plain and quoted for
// the person who decides.
func quote(s string) string {
	s = plain(s, 300)
	if s == "" {
		return "(none given)"
	}
	return "“" + s + "”"
}

func orNone(s string) string {
	if s == "" {
		return "(not given)"
	}
	return s
}

// plain is a text a description quotes, made harmless to show: every control,
// format and line-separating character a space — no line break, no
// right-to-left override reorders what the person reads —, every quote mark
// an apostrophe, so it cannot close the description's own, runs of spaces one,
// and at most n characters.
func plain(s string, n int) string {
	s, cut := llm.Head(llm.Cut(s, 8*n+8), n)
	var b strings.Builder
	space := false
	for _, r := range s {
		switch {
		case unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r) || unicode.IsSpace(r):
			if !space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			space = true
			continue
		case strings.ContainsRune("\"“”„‟«»‹›`´‘’‚‛", r):
			r = '\''
		}
		b.WriteRune(r)
		space = false
	}
	out := strings.TrimSpace(b.String())
	if cut {
		out += "…"
	}
	return out
}
