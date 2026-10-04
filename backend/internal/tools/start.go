package tools

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
)

// The bounds of the session block (docs/adr/0067 D7): the active ticket with
// five comments and ten acts, five candidates, the activity since the last
// session; a session that wants more calls get_ticket or search. MaxBlock
// keeps the block below the ten thousand characters a hook may hand Claude
// Code in one piece.
const (
	MaxBlock         = 9000
	startComments    = 5
	startActivity    = 10
	startCandidates  = 5
	startSinceLines  = 10
	startOtherActive = 5
)

// StartOptions shape a start.
type StartOptions struct {
	// Record keeps the time of this start as the last session's
	// (docs/adr/0042 D5); a lookup that only shows the binding does not.
	Record bool
}

// Start is the procedure of session_start and of the SessionStart hook,
// one function for both (docs/adr/0067 D1): the binding or the proposal, the
// active ticket's context or the candidates, and what happened since the last
// session, as one Markdown block. silent is true when there is nothing to
// say: no remote, no binding file, nothing a hook should print.
func Start(ctx context.Context, s *Session, opts StartOptions) (block string, silent bool, err error) {
	sit, err := Resolve(ctx, s)
	if err != nil {
		return "", false, err
	}
	if sit.Binding == nil {
		text := unboundBlock(sit)
		return text, text == "", nil
	}
	b := *sit.Binding
	key := MemoryKey{Installation: s.Installation, Tenant: b.Tenant, Project: b.Project}
	var last time.Time
	hadLast := false
	if s.Memory != nil {
		if last, hadLast, err = s.Memory.LastStart(key); err != nil {
			hadLast = false
		}
	}
	now := s.Now()
	text, err := boundBlock(ctx, s, sit, last, hadLast)
	if err != nil {
		return "", false, err
	}
	if opts.Record && s.Memory != nil {
		if err := s.Memory.SetLastStart(key, now); err != nil {
			text += "\n\n(The time of this session could not be kept: " + err.Error() + ")"
		}
	}
	return text, false, nil
}

// unboundBlock says why the session runs unbound, and proposes a project when the
// server proposed one (docs/adr/0066 D3); nothing when there is no remote
// and no binding file.
func unboundBlock(sit Situation) string {
	var b strings.Builder
	notes := func() {
		for _, n := range sit.Notes {
			b.WriteString("\n- " + n)
		}
	}
	l := sit.Lookup
	switch {
	case l == nil && len(sit.Notes) == 0:
		return ""
	case l == nil:
		b.WriteString("# cowork: this directory is bound to no project\n")
		notes()
		return b.String()
	case l.Status == apigen.RepositoryLookupStatusAmbiguous:
		var keys []string
		for _, x := range l.Bindings {
			keys = append(keys, x.Tenant.Slug+"/"+x.Project.Key)
		}
		fmt.Fprintf(&b, "# cowork: several projects bind this repository\n\n%s is bound to %s: a repository belongs to one "+
			"project, so this is a data error the person resolves in the UI by unbinding all but one (docs/adr/0066 D6). "+
			"The session runs unbound until then.\n", l.Bindings[0].Identity, strings.Join(keys, " and "))
		notes()
		return b.String()
	}
	b.WriteString("# cowork: this repository is bound to no project\n\n")
	if p, err := l.Proposal.Get(); err == nil {
		proposal(&b, p)
	} else if why, err := l.ProposalUnavailable.Get(); err == nil {
		b.WriteString("No project binds the remotes, and none is proposed: " + why + "\n")
	}
	notes()
	return b.String()
}

// proposal writes what the agent asks the person, and the call it makes on
// yes (docs/adr/0066 D3).
func proposal(b *strings.Builder, p apigen.RepositoryProposal) {
	fmt.Fprintf(b, "No project binds `%s`. Ask the person, and act only on their yes:\n\n", p.Identity)
	if t, err := p.Tenant.Get(); err == nil && len(p.Tenants) == 1 {
		fmt.Fprintf(b, "> Create the project `%s/%s` (\"%s\") for `%s`?\n\n", t, p.Tenants[0].Key, p.Name, p.Identity)
		fmt.Fprintf(b, "On yes: `create_project(tenant: %q, key: %q, name: %q, remote: %q)`. ", t, p.Tenants[0].Key, p.Name, p.Remote)
	} else {
		fmt.Fprintf(b, "> Create a project \"%s\" for `%s` — in which tenant?\n\n", p.Name, p.Identity)
		for _, t := range p.Tenants {
			fmt.Fprintf(b, "- `%s` (%s), key `%s`\n", t.Slug, t.Name, t.Key)
		}
		fmt.Fprintf(b, "\nOn the person's choice: `create_project(tenant, key, name: %q, remote: %q)`. ", p.Name, p.Remote)
	}
	b.WriteString("On no, the session runs unbound. The person may change the key or the name. A .cowork.yaml is not " +
		"needed afterwards: the remote binds the repository (docs/adr/0066 D4).\n")
}

// boundBlock renders the block of a bound session.
func boundBlock(ctx context.Context, s *Session, sit Situation, last time.Time, hadLast bool) (string, error) {
	b := *sit.Binding
	var head strings.Builder
	fmt.Fprintf(&head, "# cowork: %s", b.Key())
	if b.ProjectName != "" {
		head.WriteString(" — " + b.ProjectName)
	}
	head.WriteString("\n\n" + boundBy(b) + "\n")
	if b.Drift != "" {
		head.WriteString("\n> Drift: " + b.Drift + ".\n")
	}
	for _, n := range sit.Notes {
		head.WriteString("\n- " + n)
	}
	mine, err := listTickets(ctx, s, b.Tenant, b.Project, ticketQuery{states: []string{stateInProgress}, assignee: "me", limit: startOtherActive + 1})
	if err != nil {
		return "", err
	}
	var body string
	if len(mine) > 0 {
		body, err = activeSection(ctx, s, mine, MaxBlock-head.Len()-1500)
	} else {
		body, err = candidatesSection(ctx, s, b)
	}
	if err != nil {
		return "", err
	}
	since := ""
	if hadLast {
		if since, err = sinceSection(ctx, s, b, mine, last); err != nil {
			return "", err
		}
	}
	text := head.String() + "\n" + body + since
	text += "\nThe workflow tools: get_ticket reads a ticket whole, record_state rewrites its current state, open_question " +
		"asks the person one question, comment and transition record the work, finish_work closes with a verification note.\n"
	return text, nil
}

func boundBy(b Binding) string {
	switch b.Source {
	case sourceFile:
		where := "This session is bound by .cowork.yaml"
		if b.Path != "" {
			where += " (the sub-directory `" + b.Path + "`)"
		}
		return where + "."
	case sourceRemote:
		where := "This session is bound by the remote `" + b.Identity + "`"
		if b.Remote != "" {
			where += " (" + b.Remote + ")"
		}
		if b.Path != "" {
			where += ", sub-directory `" + b.Path + "`"
		}
		return where + ", as the server binds it."
	}
	return "This session is bound to it."
}

// activeSection is the active ticket's context and the other tickets of the
// person in progress; the context shrinks to fit the budget.
func activeSection(ctx context.Context, s *Session, mine []apigen.Ticket, budget int) (string, error) {
	active := mine[0]
	ref, err := s.resolveKey(active.Key)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "## Active ticket: %s — %s (%s)\n\n", active.Key, active.Title, active.State)
	if len(mine) > 1 {
		var others []string
		for _, t := range mine[1:min(len(mine), startOtherActive)] {
			others = append(others, t.Key+" — "+t.Title)
		}
		b.WriteString("Also in progress for you: " + strings.Join(others, "; ") + ".\n\n")
	}
	doc, err := contextDocument(ctx, s, ref, startComments, startActivity)
	if err != nil {
		return "", err
	}
	if len(doc) > budget {
		if doc, err = contextDocument(ctx, s, ref, 2, 3); err != nil {
			return "", err
		}
	}
	b.WriteString(fit(doc, budget-b.Len()))
	b.WriteString("\n" + commitLines(ref, string(active.Type), active.Title) + "\n")
	b.WriteString("Its page: " + s.TicketPage(ref.Tenant, ref.Short()) + "\n")
	return b.String(), nil
}

// fit cuts a document at a line to stay within n characters, and says so.
func fit(doc string, n int) string {
	if n < 200 {
		n = 200
	}
	if len(doc) <= n {
		return doc
	}
	cut := strings.LastIndex(doc[:n], "\n")
	if cut < 0 {
		cut = n
	}
	return doc[:cut] + "\n\n… (cut to fit the session start; get_ticket shows the whole ticket)\n"
}

// candidatesSection is the top of the project's backlog for the person —
// assigned to them or to nobody, open and not waiting on a prerequisite — in
// the order of the rank, the decision of the backlog (docs/adr/0014 D1).
func candidatesSection(ctx context.Context, s *Session, b Binding) (string, error) {
	blocked := false
	list, err := listTickets(ctx, s, b.Tenant, b.Project, ticketQuery{
		states: []string{stateReview, "decided", "analysed", "filed"}, assignees: []string{"me", "none"},
		blocked: &blocked, limit: startCandidates})
	if err != nil {
		return "", err
	}
	var out strings.Builder
	fmt.Fprintf(&out, "## No ticket of yours is in progress in %s\n\n", b.Key())
	if len(list) == 0 {
		out.WriteString("Nothing is open for you or unassigned. Ask the person what to work on, or file_ticket.\n")
		return out.String(), nil
	}
	out.WriteString("The top of the backlog, by rank:\n\n")
	for i, t := range list {
		fmt.Fprintf(&out, "%d. %s — %s (%s, %s, %s)\n", i+1, t.Key, t.Title, t.State, t.Effort, assigneeName(t))
	}
	out.WriteString("\nPick one with the person: the active ticket is the one in progress and assigned to them.\n")
	return out.String(), nil
}

// sinceSection is what happened since the last session: the acts on the
// person's tickets in progress — for the active one only their number, its
// context lists the last of them — and the project's tickets that changed.
func sinceSection(ctx context.Context, s *Session, b Binding, mine []apigen.Ticket, last time.Time) (string, error) {
	var lines []string
	shown := map[string]bool{}
	for i, t := range mine {
		shown[t.Key] = true
		ref, err := s.resolveKey(t.Key)
		if err != nil {
			return "", err
		}
		acts, err := activitySince(ctx, s, ref, last)
		if err != nil {
			return "", err
		}
		if i == 0 {
			switch n := len(acts); {
			case n == 1:
				lines = append(lines, fmt.Sprintf("- %s: one act, under Recent activity above", t.Key))
			case n > 1:
				lines = append(lines, fmt.Sprintf("- %s: %d acts, the last of them under Recent activity above", t.Key, n))
			}
			continue
		}
		for _, a := range acts {
			if len(lines) < startSinceLines {
				lines = append(lines, fmt.Sprintf("- %s %s — %s", stamp(a.At), t.Key, actLine(a)))
			}
		}
	}
	changed, err := listTenantTickets(ctx, s, b.Tenant, b.Project, last, startSinceLines)
	if err != nil {
		return "", err
	}
	for _, t := range changed {
		if !shown[t.Key] && len(lines) < 2*startSinceLines {
			lines = append(lines, fmt.Sprintf("- %s — %s (%s), changed", t.Key, t.Title, t.State))
		}
	}
	var out strings.Builder
	fmt.Fprintf(&out, "\n## Since your last session (%s)\n\n", stamp(last))
	if len(lines) == 0 {
		out.WriteString("Nothing changed in " + b.Key() + ".\n")
		return out.String(), nil
	}
	out.WriteString(strings.Join(lines, "\n") + "\n")
	return out.String(), nil
}

// activitySince reads the acts on a ticket after a time, oldest first.
func activitySince(ctx context.Context, s *Session, ref ticketRef, since time.Time) ([]apigen.Activity, error) {
	order := apigen.ListActivityParamsOrderDesc
	limit := 20
	res, err := s.API.ListActivityWithResponse(ctx, ref.Tenant, ref.Project, int(ref.Number),
		&apigen.ListActivityParams{Order: &order, Limit: &limit})
	if err := check(res, err, http.StatusOK); err != nil {
		return nil, err
	}
	var out []apigen.Activity
	for i := len(res.JSON200.Items) - 1; i >= 0; i-- {
		if a := res.JSON200.Items[i]; a.At.After(since) {
			out = append(out, a)
		}
	}
	return out, nil
}

func actLine(a apigen.Activity) string {
	who := "the system"
	if p, err := a.Actor.Get(); err == nil {
		who = p.DisplayName
	} else if sys, err := a.ActorSystem.Get(); err == nil {
		who = sys
	}
	if agent, err := a.Agent.Get(); err == nil {
		who += " via " + agent
	} else if tok, err := a.Token.Get(); err == nil {
		// A person's act through a token is named so too (docs/adr/0036 D6).
		if name, err := tok.Name.Get(); err == nil && name != "" {
			who += " through the token " + name
		} else {
			who += " through a token"
		}
	}
	line := who + " " + string(a.Action)
	if a.Redacted {
		return line
	}
	if before, err := a.Before.Get(); err == nil {
		if after, err := a.After.Get(); err == nil && before["state"] != nil && after["state"] != nil {
			line += fmt.Sprintf(" %v → %v", before["state"], after["state"])
		}
	}
	return line
}

func stamp(t time.Time) string { return t.UTC().Format("2006-01-02 15:04 UTC") }

func assigneeName(t apigen.Ticket) string {
	if p, err := t.Assignee.Get(); err == nil {
		return p.DisplayName
	}
	return "unassigned"
}
