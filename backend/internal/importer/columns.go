package importer

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/guided-traffic/cowork/backend/internal/domain"
)

// The note and the reason the import records where its source carries none
// (docs/adr/0063 D2): a done act needs its verification note and a dropped one
// its reason (docs/adr/0009 D4, D5), and the import invents neither.
const (
	NoteDone      = "imported from archive; the source carried no verification note"
	ReasonDropped = "imported from archive; the source carried no reason"
)

// The columns a file without frontmatter is imported with (docs/adr/0063 D4).
const (
	plainSeverity domain.Severity = "low"
	plainEffort   domain.Effort   = "S"
)

// columns reads a ticket file into its planned ticket: what the file says,
// with the person's correction and the defaults the records give.
func (a *analysis) columns(e *entry) {
	f, p := e.f, e.plan
	if f.Format == FormatPlain {
		plainDefaults(f)
	}
	p.Title, p.Body, p.Questions, p.Attachments = f.Title, f.Body, f.Questions, f.Attachments
	p.Severity, p.Security, p.Threat, p.Effort, p.Stages = f.Severity, f.Security, f.Threat, f.Effort, f.Stages
	p.Horizon = f.Horizon
	if p.Horizon == "" {
		p.Horizon = domain.UrgencyDefault
	}
	p.Opened, p.Decided = f.Opened, f.Decided
	if p.Opened == nil && f.Format != FormatPlain {
		f.warn(keyOpened, f.line(keyOpened), "the file names no opened date; the ticket is opened at the execution")
	}
	var reason string
	p.Type, reason = a.ticketType(e)
	e.rep.TypeReason = ptr(reason)
	a.state(e)
	a.assignee(e)
	a.confidential(e)
	if n := len(f.Attachments); n > 0 {
		f.warn(keyAttachments, f.line(keyAttachments), "the source lists %d attachments; an export carries no bytes, and the import brings no file (docs/adr/0051 D4)", n)
	}
}

// plainDefaults are the columns of a file without frontmatter: an archived
// record, one done task (docs/adr/0063 D4).
func plainDefaults(f *File) {
	f.State, f.Severity, f.Security, f.Effort = domain.StateDone, plainSeverity, domain.SecurityNone, plainEffort
	f.warn(fieldFile, 0, "a file without frontmatter: imported as one done task — severity low, security none, effort S — its text the body (docs/adr/0063 D4)")
}

// ticketType is the type a file gets and why (docs/adr/0008 D5): the one a
// correction or the file names, else the one its content suggests.
func (a *analysis) ticketType(e *entry) (domain.TicketType, string) {
	f := e.f
	switch {
	case e.corr != nil && e.corr.Type != "":
		return e.corr.Type, "corrected"
	case f.Type != "":
		return f.Type, "named by the file"
	case f.Format == FormatPlain:
		return domain.TypeTask, "a record without frontmatter becomes a task (docs/adr/0063 D4)"
	}
	return detectType(f.Title, f.Security)
}

// typeRules detect a type from a title's words, first match wins; each word
// is matched whole.
var typeRules = []struct {
	typ   domain.TicketType
	words []string
}{
	{domain.TypeDecision, []string{"undecided", "decide", "decides", "decision", "whether"}},
	{domain.TypeBug, []string{"fail", "fails", "failed", "failing", "broken", "breaks", "crash", "crashes", "wrong",
		"incorrect", "leak", "leaks", "regression", "bug", "defect"}},
	{domain.TypeFeature, []string{"there is no", "there are no", "has no", "have no", "cannot", "can not",
		"is missing", "are missing", "does not exist", "do not exist", "not yet"}},
}

// detectType reads a type from a title by content (docs/adr/0008 D5): a
// question asks, a security finding is a defect, and the title's words name a
// decision, a defect, or something that does not exist yet; else a task. The
// report says which rule matched, for the person who corrects it.
func detectType(title string, security domain.SecurityClass) (domain.TicketType, string) {
	switch {
	case strings.HasSuffix(strings.TrimSpace(title), "?"):
		return domain.TypeQuestion, "detected: the title asks a question"
	case security.MakesConfidential():
		return domain.TypeBug, "detected: a security finding, security " + string(security)
	}
	words := " " + strings.Join(strings.FieldsFunc(strings.ToLower(title), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}), " ") + " "
	for _, rule := range typeRules {
		for _, w := range rule.words {
			if strings.Contains(words, " "+w+" ") {
				return rule.typ, `detected: the title says "` + w + `"`
			}
		}
	}
	return domain.TypeTask, "detected: the title names no question, defect, decision or missing capability"
}

// blockable are the states a block comes from (docs/adr/0009 D2).
var blockable = map[domain.TicketState]bool{domain.StateFiled: true, domain.StateAnalysed: true,
	domain.StateDecided: true, domain.StateInProgress: true, domain.StateReview: true}

// blockKinds are the kinds of a block (docs/adr/0009 D2).
var blockKinds = map[string]domain.BlockKind{"decision": domain.BlockDecision, "human": domain.BlockHuman,
	"product": domain.BlockProduct, "release": domain.BlockRelease, "external": domain.BlockExternal, "ticket": domain.BlockTicket}

// state reads the ticket's state with its note and dates; `blocked` only as
// the file or a correction says it, never inferred (docs/adr/0009).
func (a *analysis) state(e *entry) {
	f, p := e.f, e.plan
	p.State = f.State
	if e.corr != nil && e.corr.State != "" {
		p.State = e.corr.State
	}
	switch p.State {
	case domain.StateBlocked:
		a.block(e)
	case domain.StateDone:
		p.Note, p.Done = f.Shipped, f.Done
		if p.Note == "" {
			p.Note = NoteDone
			f.warn(keyShipped, 0, "done without a shipped line: the done act carries the note %q (docs/adr/0063 D2)", NoteDone)
		}
		if p.Done == nil {
			f.warn(keyDone, f.line(keyDone), "done without a done date: the ticket keeps none")
		}
	case domain.StateDropped:
		p.Note = f.DroppedReason
		if p.Note == "" {
			p.Note = ReasonDropped
			f.warn(keyDroppedReason, 0, "dropped without a dropped-reason: the act carries the reason %q", ReasonDropped)
		}
	}
	if p.State != domain.StateDone && f.Done != nil {
		f.warn(keyDone, f.line(keyDone), "the done date is not read: the ticket is not imported as done")
	}
}

// block reads the block of a ticket imported as blocked: the correction's,
// or the source's `blocked-by`, `blocked-reason` and `blocked-from`.
func (a *analysis) block(e *entry) {
	f, p := e.f, e.plan
	if c := e.corr; c != nil && c.State == domain.StateBlocked && c.Block != nil {
		from := c.Block.From
		if from == "" {
			from = f.State
		}
		p.Block = &PlannedBlock{Kind: c.Block.Kind, Reason: c.Block.Reason, From: from}
		return
	}
	line := f.line(keyBlockedBy)
	kind, ok := blockKinds[f.BlockedBy]
	waits := ""
	if !ok && f.BlockedBy != "" && a.isReference(f.BlockedBy) {
		kind, ok, waits = domain.BlockTicket, true, f.BlockedBy
	}
	from := domain.TicketState(f.BlockedFrom)
	switch {
	case f.BlockedBy == "":
		f.fail(keyBlockedBy, line, "a blocked ticket names what it waits on in blocked-by (docs/adr/0009 D2)")
	case !ok:
		f.fail(keyBlockedBy, line, "blocked-by %q is neither a block kind nor a ticket", f.BlockedBy)
	case f.BlockedReason == "":
		f.fail(keyBlockedReason, f.line(keyBlockedReason), "a blocked ticket needs its blocked-reason (docs/adr/0009 D2)")
	case !blockable[from]:
		f.fail(keyBlockedFrom, f.line(keyBlockedFrom), "blocked-from %q is no state a block comes from: filed, analysed, decided, in-progress or review", f.BlockedFrom)
	default:
		p.Block = &PlannedBlock{Kind: kind, Reason: f.BlockedReason, From: from}
		e.waitsOn = waits
	}
}

// isReference reports whether a value names a ticket: T<n>, or a full key.
func (a *analysis) isReference(v string) bool {
	if repositoryID.MatchString(v) {
		return true
	}
	k, err := domain.ParseTicketKey(v)
	return err == nil && k.Tenant != ""
}

// identity is a person's identity as grammar v1 writes it (docs/adr/0044 D1):
// local:<username>, or oidc:<issuer>#<subject>.
type identity struct {
	username, issuer, subject string
}

func (i identity) String() string {
	if i.username != "" {
		return "local:" + i.username
	}
	return "oidc:" + i.issuer + "#" + i.subject
}

var personForm = regexp.MustCompile(`^(.*?)\s*<([^<>]+)>$`)

// parseIdentity reads `Name <identity>`; the issuer ends at the first #.
func parseIdentity(v string) (identity, bool) {
	m := personForm.FindStringSubmatch(strings.TrimSpace(v))
	if m == nil {
		return identity{}, false
	}
	if u, ok := strings.CutPrefix(m[2], "local:"); ok && u != "" {
		return identity{username: u}, true
	}
	if rest, ok := strings.CutPrefix(m[2], "oidc:"); ok {
		if iss, sub, ok := strings.Cut(rest, "#"); ok && iss != "" && sub != "" {
			return identity{issuer: iss, subject: sub}, true
		}
	}
	return identity{}, false
}

// IdentityOf is how a person's identity is written, the key of
// Target.Persons.
func IdentityOf(username, issuer, subject string) string {
	return identity{username: username, issuer: issuer, subject: subject}.String()
}

// assignee resolves the assignee by identity, never by name
// (docs/adr/0044 D1); a correction names the person instead.
func (a *analysis) assignee(e *entry) {
	f, p := e.f, e.plan
	if c := e.corr; c != nil && c.AssigneeSet {
		e.rep.Assignee = &AssigneeReport{Source: f.Assignee}
		if c.Assignee != nil {
			person := a.t.Assignees[*c.Assignee]
			p.Assignee, e.rep.Assignee.Person = &person.ID, &person
		}
		return
	}
	if f.Assignee == "" {
		return
	}
	rep := &AssigneeReport{Source: f.Assignee}
	e.rep.Assignee = rep
	line := f.line(keyAssignee)
	id, ok := parseIdentity(f.Assignee)
	switch {
	case !ok:
		f.warn(keyAssignee, line, "the assignee %q names no identity, local:<username> or oidc:<issuer>#<subject>: nobody is assigned, never a guess by the name (docs/adr/0044 D1)", f.Assignee)
	case id.username == "" && id.issuer != a.t.Issuer:
		f.warn(keyAssignee, line, "the assignee's identity is of another issuer than this installation's: nobody is assigned (docs/adr/0044 D1)")
	default:
		person, found := a.t.Persons[id.String()]
		if !found {
			f.warn(keyAssignee, line, "the assignee %s is no member of the tenant who can see the project: nobody is assigned", id)
			return
		}
		p.Assignee, rep.Person = &person.ID, &person
	}
}

// confidential applies the rule of the source (docs/adr/0065 D7): an export's
// flag, the embargo's local_ prefix, or a live or boundary finding without a
// shipped line sets it; a publication-accepted date leaves it unset, and the
// report says so.
func (a *analysis) confidential(e *entry) {
	f, p := e.f, e.plan
	finding := p.Security.MakesConfidential()
	switch {
	case f.PublicationAccepted != "":
		p.ConfidentialReason = "publication accepted on " + f.PublicationAccepted + ": the flag is left unset (docs/adr/0065 D7)"
	case f.Confidential:
		p.Confidential, p.ConfidentialReason = true, "the export marks it confidential"
	case f.Local:
		p.Confidential, p.ConfidentialReason = true, "the file name carries the local_ prefix of an open finding's embargo (docs/adr/0065 D7)"
	case finding && f.Shipped == "":
		p.Confidential, p.ConfidentialReason = true, "security "+string(p.Security)+" without a shipped line: a finding not yet fixed (docs/adr/0065 D7)"
	case finding:
		p.ConfidentialReason = "security " + string(p.Security) + " with a shipped line that names the fix: the flag is left unset (docs/adr/0065 D7)"
	}
}
