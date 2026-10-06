package importer

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/guided-traffic/cowork/backend/internal/domain"
)

// The sources of a link, as the report names them.
const (
	sourceBlockedBy = "blocked-by"
	sourceFiledFrom = "filed-from"
	sourceLinks     = LinksFile
)

// The directions of a link read from one of its ends.
const (
	directionOutgoing = "outgoing"
	directionIncoming = "incoming"
)

// resolve is the ticket a reference names: one the execution creates, or one
// of the project the upload does not bring (docs/adr/0051 D9); else nil and
// why it names none.
func (a *analysis) resolve(v string) (*Ref, string) {
	if e, ok := a.byKey[v]; ok && !e.excluded {
		if e.creates() {
			return &Ref{Number: e.f.Number}, ""
		}
		return nil, fmt.Sprintf("%s is in the upload, but %s is not imported (%s)", v, e.f.Path, e.outcome())
	}
	n, ok := a.u.referenceNumber(v, a.project)
	if !ok {
		return nil, v + " is a ticket of another project"
	}
	if es := a.byNumber[n]; len(es) > 0 {
		if len(es) == 1 && es[0].creates() {
			return &Ref{Number: n}, ""
		}
		return nil, fmt.Sprintf("%s is in the upload, but %s is not imported (%s)", v, es[0].f.Path, es[0].outcome())
	}
	if id, ok := a.t.Existing[n]; ok {
		return &Ref{ID: id}, ""
	}
	return nil, v + " is neither in the upload nor a ticket of the project"
}

// settle turns into errors what keeps a file from being created as it says —
// a block that waits on a ticket nothing resolves, a parent chain that loops —
// until no file changes: an error takes a file out of what others resolve to.
func (a *analysis) settle() {
	for changed := true; changed; {
		changed = false
		for _, e := range a.entries {
			if e.creates() && a.unresolvedBlock(e) {
				changed = true
			}
		}
		if a.parentCycles() {
			changed = true
		}
	}
}

// waitsOnOf is the reference a block of kind ticket waits on: the file's own,
// or the one blocks link of an export's links.json that ends at the ticket.
func (a *analysis) waitsOnOf(e *entry) (string, string) {
	if e.waitsOn != "" {
		return e.waitsOn, ""
	}
	own := domain.FullKey(e.f.Key.Tenant, e.f.Key.Project, e.f.Key.Number)
	var sources []string
	for _, l := range a.u.links {
		if l.Type == string(domain.LinkBlocks) && l.Target == own {
			sources = append(sources, l.Source)
		}
	}
	if len(sources) != 1 {
		return "", fmt.Sprintf("a block of kind ticket names the ticket it waits on; the upload's links name %d", len(sources))
	}
	return sources[0], ""
}

// unresolvedBlock makes a block of kind ticket whose ticket nothing resolves
// an error of the file (docs/adr/0009 D2).
func (a *analysis) unresolvedBlock(e *entry) bool {
	b := e.plan.Block
	if b == nil || b.Kind != domain.BlockTicket {
		return false
	}
	v, why := a.waitsOnOf(e)
	if why == "" {
		if _, why = a.resolve(v); why == "" {
			return false
		}
	}
	e.f.fail(keyBlockedBy, e.f.line(keyBlockedBy), "the block cannot be imported: %s", why)
	return true
}

// parentCycles makes every file of a parent chain that loops within the
// upload an error (docs/adr/0008 D2); true when it found one.
func (a *analysis) parentCycles() bool {
	var loop []*entry
	for _, e := range a.entries {
		if !e.creates() || e.f.Parent == "" {
			continue
		}
		var path []*entry
		at := map[*entry]int{}
		for cur := e; cur != nil && cur.creates(); cur = a.parentEntry(cur) {
			if i, seen := at[cur]; seen {
				loop = append(loop, path[i:]...)
				break
			}
			at[cur] = len(path)
			path = append(path, cur)
		}
	}
	failed := map[*entry]bool{}
	for _, e := range loop {
		if !failed[e] {
			failed[e] = true
			e.f.fail(keyParent, e.f.line(keyParent), "the parent chain loops back to this ticket (docs/adr/0008 D2)")
		}
	}
	return len(loop) > 0
}

// parentEntry is the file a file's parent names, nil for none or for a ticket
// of the project.
func (a *analysis) parentEntry(e *entry) *entry {
	if e.f.Parent == "" {
		return nil
	}
	ref, _ := a.resolve(e.f.Parent)
	if ref == nil || ref.Number == 0 {
		return nil
	}
	return a.byNumber[ref.Number][0]
}

// linkPlan is a link to plan, with the file it was read from.
type linkPlan struct {
	PlannedLink
	from   *entry
	source string
}

// references resolves what the files name of other tickets: the parent, the
// ticket a block waits on, a repository's blocked-by and filed-from, an
// export's links (docs/adr/0063 D3, docs/adr/0051 D4, D9).
func (a *analysis) references() []linkPlan {
	var links []linkPlan
	for _, e := range a.entries {
		if !e.creates() {
			continue
		}
		a.parent(e)
		links = append(links, a.waits(e)...)
		links = append(links, a.blockedBy(e)...)
		links = append(links, a.filedFrom(e)...)
	}
	links = append(links, a.manifestLinks()...)
	return links
}

func (a *analysis) self(e *entry) Ref { return Ref{Number: e.f.Number} }

// parent resolves the parent, a ticket of the same project
// (docs/adr/0008 D2).
func (a *analysis) parent(e *entry) {
	if e.f.Parent == "" {
		return
	}
	ref, why := a.resolve(e.f.Parent)
	if ref == nil {
		e.f.warn(keyParent, e.f.line(keyParent), "the parent is not set: %s", why)
		return
	}
	e.plan.Parent = ref
}

// waits resolves the ticket a block of kind ticket waits on, which a blocks
// link from it records (docs/adr/0009 D2).
func (a *analysis) waits(e *entry) []linkPlan {
	b := e.plan.Block
	if b == nil || b.Kind != domain.BlockTicket {
		return nil
	}
	v, _ := a.waitsOnOf(e)
	ref, _ := a.resolve(v)
	if ref == nil {
		return nil
	}
	b.WaitsOn = ref
	return []linkPlan{{PlannedLink: PlannedLink{Type: domain.LinkBlocks, Source: *ref, Target: a.self(e)}, from: e, source: sourceBlockedBy}}
}

// adrBlock is a repository's blocked-by that names an ADR, a decision
// (docs/tickets/README.md).
var adrBlock = regexp.MustCompile(`^adr-[0-9]{4}$`)

// blockedBy reads a blocked-by beside a state that is not blocked: a ticket
// is a blocks link from it; a kind, or an ADR, is a candidate the report
// names and a person confirms (docs/adr/0009 Consequences); anything that
// resolves to nothing is a line under ## Related, never a silent drop
// (docs/adr/0063 D3).
func (a *analysis) blockedBy(e *entry) []linkPlan {
	f := e.f
	v := f.BlockedBy
	if v == "" || e.plan.State == domain.StateBlocked {
		return nil
	}
	line := f.line(keyBlockedBy)
	if a.isReference(v) {
		ref, why := a.resolve(v)
		if ref == nil {
			f.warn(keyBlockedBy, line, "the blocks link from %s is not made: %s; the body keeps it under ## Related", v, why)
			e.related = append(e.related, fmt.Sprintf("- Blocked by %s, which is not in this import (blocked-by of %s)", v, f.Path))
			return nil
		}
		return []linkPlan{{PlannedLink: PlannedLink{Type: domain.LinkBlocks, Source: *ref, Target: a.self(e)}, from: e, source: sourceBlockedBy}}
	}
	kind, isKind := blockKinds[v]
	switch {
	case isKind && kind != domain.BlockTicket:
		e.rep.BlockCandidate = ptr(kind)
	case adrBlock.MatchString(v):
		e.rep.BlockCandidate = ptr(domain.BlockDecision)
	}
	f.warn(keyBlockedBy, line, "blocked-by %s is not imported as the state blocked, which is never inferred (docs/adr/0009); correct the state to confirm it, and the body keeps it under ## Related", v)
	e.related = append(e.related, fmt.Sprintf("- Blocked by: %s (blocked-by of %s)", v, f.Path))
	return nil
}

// filedFrom reads a repository's filed-from: a ticket is a found-in link to
// it (docs/adr/0012 D1); an event is a line under ## Related
// (docs/adr/0010 D4, docs/adr/0051 D2).
func (a *analysis) filedFrom(e *entry) []linkPlan {
	f := e.f
	v := f.FiledFrom
	if v == "" {
		return nil
	}
	line := f.line(keyFiledFrom)
	if !a.isReference(v) {
		f.warn(keyFiledFrom, line, "filed-from names an event, not a ticket: the body keeps it under ## Related")
		e.related = append(e.related, "- Filed from: "+v)
		return nil
	}
	ref, why := a.resolve(v)
	if ref == nil {
		f.warn(keyFiledFrom, line, "the found-in link to %s is not made: %s; the body keeps it under ## Related", v, why)
		e.related = append(e.related, fmt.Sprintf("- Found in %s, which is not in this import (filed-from of %s)", v, f.Path))
		return nil
	}
	return []linkPlan{{PlannedLink: PlannedLink{Type: domain.LinkFoundIn, Source: a.self(e), Target: *ref}, from: e, source: sourceFiledFrom}}
}

// manifestLinks reads an export's links.json: a link with an end in the
// upload is made where both ends resolve, and reported and omitted where one
// does not (docs/adr/0051 D4, D9).
func (a *analysis) manifestLinks() []linkPlan {
	var out []linkPlan
	for _, l := range a.u.links {
		src, srcIn := a.byKey[l.Source]
		tgt, tgtIn := a.byKey[l.Target]
		var at *entry
		switch {
		case srcIn && src.creates():
			at = src
		case tgtIn && tgt.creates():
			at = tgt
		default:
			// No end of the link is a ticket this import creates.
			continue
		}
		typ := domain.LinkType(l.Type)
		if !validLink(typ) {
			at.f.warn(sourceLinks, 0, "the link %s %s %s of %s is omitted: no link type", l.Source, l.Type, l.Target, l.path)
			continue
		}
		s, whyS := a.resolve(l.Source)
		t, whyT := a.resolve(l.Target)
		if s == nil || t == nil {
			at.f.warn(sourceLinks, 0, "the link %s %s %s is omitted: %s (docs/adr/0051 D9)", l.Source, l.Type, l.Target, whyS+whyT)
			continue
		}
		out = append(out, linkPlan{PlannedLink: PlannedLink{Type: typ, Source: *s, Target: *t}, from: at, source: sourceLinks})
	}
	return out
}

func validLink(t domain.LinkType) bool {
	return t == domain.LinkBlocks || t == domain.LinkRelatesTo || t == domain.LinkDuplicates || t == domain.LinkFoundIn
}

// linkKey identifies a link for its deduplication: relates-to is stored once
// whichever end names it (docs/adr/0012 D1).
func linkKey(l PlannedLink) string {
	s, t := refKey(l.Source), refKey(l.Target)
	if l.Type == domain.LinkRelatesTo && s > t {
		s, t = t, s
	}
	return string(l.Type) + "|" + s + "|" + t
}

func refKey(r Ref) string {
	if r.Number != 0 {
		return "n" + strconv.Itoa(int(r.Number))
	}
	return "i" + r.ID.String()
}

// planLinks keeps each link once, never one from a ticket to itself, and no
// blocks link that would close a cycle among the imported tickets
// (docs/adr/0012 D4); the database's walk holds the links to the project's
// tickets when they are written. Each link is reported at its ends.
func (a *analysis) planLinks(candidates []linkPlan) []PlannedLink {
	seen := map[string]bool{}
	blocks := map[string][]string{}
	var out []PlannedLink
	for _, c := range candidates {
		k := linkKey(c.PlannedLink)
		s, t := refKey(c.Source), refKey(c.Target)
		switch {
		case seen[k]:
			continue
		case s == t:
			c.from.f.warn(c.source, 0, "a link from the ticket to itself is omitted (docs/adr/0012 D4)")
			continue
		case c.Type == domain.LinkBlocks && reaches(blocks, t, s):
			c.from.f.warn(c.source, 0, "the blocks link from %s to %s is omitted: it would close a cycle (docs/adr/0012 D4)", a.refName(c.Source), a.refName(c.Target))
			continue
		}
		seen[k] = true
		if c.Type == domain.LinkBlocks {
			blocks[s] = append(blocks[s], t)
		}
		out = append(out, c.PlannedLink)
		a.reportLink(c)
	}
	return out
}

// reaches reports whether the blocks graph leads from one ticket to another.
func reaches(graph map[string][]string, from, to string) bool {
	seen := map[string]bool{}
	stack := []string{from}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if cur == to {
			return true
		}
		if seen[cur] {
			continue
		}
		seen[cur] = true
		stack = append(stack, graph[cur]...)
	}
	return false
}

// refName is the key a planned ticket gets or the project's ticket has.
func (a *analysis) refName(r Ref) string {
	if r.Number != 0 {
		return a.t.Key(r.Number)
	}
	for n, id := range a.t.Existing {
		if id == r.ID {
			return a.t.Key(n)
		}
	}
	return r.ID.String()
}

// reportLink names a planned link at each end the upload brings.
func (a *analysis) reportLink(c linkPlan) {
	for _, end := range []struct {
		at, other Ref
		direction string
	}{{c.Source, c.Target, directionOutgoing}, {c.Target, c.Source, directionIncoming}} {
		if end.at.Number == 0 {
			continue
		}
		e := a.byNumber[end.at.Number][0]
		e.rep.Links = append(e.rep.Links, LinkReport{Type: c.Type, Direction: end.direction, Key: a.refName(end.other), Source: c.source})
	}
}

// mention is a repository's T<n> in prose (docs/adr/0063 D3).
var mention = regexp.MustCompile(`\bT([1-9][0-9]{0,9})\b`)

// rewrite turns every T<n> outside code into the full key of the imported
// ticket n; a mention of a ticket the import does not create stays as it is.
func (a *analysis) rewrite(text string, creates map[int32]bool) string {
	if !strings.Contains(text, "T") {
		return text
	}
	lines := strings.Split(text, "\n")
	fenced := fences(lines)
	for i, l := range lines {
		if fenced[i] {
			continue
		}
		lines[i] = outsideCodeSpans(l, func(s string) string {
			return mention.ReplaceAllStringFunc(s, func(m string) string {
				n := number(m[1:])
				if !creates[n] {
					return m
				}
				return a.t.Key(n)
			})
		})
	}
	return strings.Join(lines, "\n")
}

// outsideCodeSpans applies fn to the parts of a line outside `code spans`.
func outsideCodeSpans(l string, fn func(string) string) string {
	parts := strings.Split(l, "`")
	if len(parts)%2 == 0 {
		return fn(l)
	}
	for i := 0; i < len(parts); i += 2 {
		parts[i] = fn(parts[i])
	}
	return strings.Join(parts, "`")
}

// plan orders the tickets the execution creates, parents before their
// children and otherwise by number, with their final bodies.
func (a *analysis) plan() Plan {
	candidates := a.references()
	var p Plan
	p.Links = a.planLinks(candidates)
	creates := map[int32]bool{}
	children := map[int32]bool{}
	for _, e := range a.entries {
		if e.creates() {
			creates[e.f.Number] = true
			if e.plan.Parent != nil && e.plan.Parent.Number != 0 {
				children[e.plan.Parent.Number] = true
			}
		}
	}
	var order []*entry
	done := map[*entry]bool{}
	var visit func(e *entry)
	visit = func(e *entry) {
		if done[e] {
			return
		}
		done[e] = true
		if par := e.plan.Parent; par != nil && par.Number != 0 {
			visit(a.byNumber[par.Number][0])
		}
		order = append(order, e)
	}
	byNumber := slices.Clone(a.entries)
	slices.SortStableFunc(byNumber, func(x, y *entry) int { return int(x.f.Number) - int(y.f.Number) })
	for _, e := range byNumber {
		if e.creates() {
			visit(e)
		}
	}
	for _, e := range order {
		a.finishPlan(e, creates, children[e.f.Number])
		p.Tickets = append(p.Tickets, e.plan)
		p.Highest = max(p.Highest, e.f.Number)
	}
	return p
}

// finishPlan writes a planned ticket's body with its related lines and its
// mentions rewritten, and decides how a done ticket is done
// (docs/adr/0009 D5): by its stages only while it has no children and all
// three are full, by hand otherwise.
func (a *analysis) finishPlan(e *entry, creates map[int32]bool, hasChildren bool) {
	p := e.plan
	if len(e.related) > 0 {
		p.Body = withRelated(p.Body, e.related)
	}
	if e.f.Format != FormatExport {
		p.Body = a.rewrite(p.Body, creates)
		for i := range p.Questions {
			q := &p.Questions[i]
			q.Question = a.rewrite(q.Question, creates)
			q.Options = a.rewrite(q.Options, creates)
			q.Recommendation = a.rewrite(q.Recommendation, creates)
			q.Answer = a.rewrite(q.Answer, creates)
		}
	}
	full := p.Stages == [3]int{100, 100, 100}
	p.DoneByHand = p.State == domain.StateDone && (hasChildren || !full)
}

// withRelated appends lines to the body's `## Related` section, or adds the
// section at its end.
func withRelated(body string, lines []string) string {
	block := strings.Join(lines, "\n")
	ls := strings.Split(body, "\n")
	fenced := fences(ls)
	for i, l := range ls {
		if !fenced[i] && strings.TrimSpace(l) == "## Related" {
			end := len(ls)
			for j := i + 1; j < len(ls); j++ {
				if !fenced[j] && isHeadingTwo(ls[j]) {
					end = j
					break
				}
			}
			section := strings.TrimRight(strings.Join(ls[:end], "\n"), "\n") + "\n" + block
			if end < len(ls) {
				return section + "\n\n" + strings.Join(ls[end:], "\n")
			}
			return section
		}
	}
	if strings.TrimSpace(body) == "" {
		return "## Related\n\n" + block
	}
	return body + "\n\n## Related\n\n" + block
}

// finish fills a file's report from what the analysis made of it.
func (a *analysis) finish(e *entry) {
	f, p, r := e.f, e.plan, e.rep
	r.Outcome = e.outcome()
	r.Format = ptr(f.Format)
	if e.corr != nil {
		r.Correction = e.corr
	}
	if e.excluded {
		r.Reason = ptr("excluded by the correction")
	}
	if f.Number != 0 {
		r.Number, r.Key = ptr(f.Number), ptr(a.t.Key(f.Number))
	}
	r.Conflict = optional(e.conflict)
	r.Title = optional(f.Title)
	if !e.excluded && !f.unreadable {
		a.finishColumns(e)
	}
	r.Attachments = append([]string{}, f.Attachments...)
	r.Confidential, r.ConfidentialReason = p.Confidential, optional(p.ConfidentialReason)
	r.Warnings, r.Errors = messages(f.Warnings), messages(f.Errors)
}

// finishColumns reports the type, state, block, columns, parent, note and
// questions of a file the analysis read.
func (a *analysis) finishColumns(e *entry) {
	p, r := e.plan, e.rep
	if p.Type != "" {
		r.Type = ptr(p.Type)
	}
	if p.State != "" {
		r.State = ptr(p.State)
	}
	if b := p.Block; b != nil {
		r.Block = &BlockReport{Kind: b.Kind, Reason: b.Reason, From: b.From}
		if b.WaitsOn != nil {
			r.Block.Ticket = ptr(a.refName(*b.WaitsOn))
		}
	}
	c := &Columns{Threat: optional(p.Threat), ProgressRefinement: p.Stages[0], Progress: p.Stages[1], ProgressReview: p.Stages[2],
		Opened: day(p.Opened), Decided: day(p.Decided), Done: day(p.Done)}
	if p.Severity != "" {
		c.Severity = ptr(p.Severity)
	}
	if p.Security != "" {
		c.Security = ptr(p.Security)
	}
	if p.Horizon != "" {
		c.Horizon = ptr(p.Horizon)
	}
	if p.Effort != "" {
		c.Effort = ptr(p.Effort)
	}
	r.Columns = c
	if p.Parent != nil {
		r.Parent = ptr(a.refName(*p.Parent))
	}
	r.Note = optional(p.Note)
	for _, q := range p.Questions {
		r.Questions = append(r.Questions, QuestionReport{Number: q.Number, Question: q.Question, Status: q.Status})
	}
}

func day(t *time.Time) *string {
	if t == nil {
		return nil
	}
	return ptr(t.UTC().Format(time.DateOnly))
}
