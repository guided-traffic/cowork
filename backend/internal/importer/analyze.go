package importer

import (
	"encoding/json"
	"fmt"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/guided-traffic/cowork/backend/internal/domain"
)

// Upload is the files of an upload as the import reads them: the ticket
// files parsed, and the manifests of an export beside them.
type Upload struct {
	sources []Source
	files   map[string]*File
	links   []manifestLink
	// manifests are what the report says of each manifest it read, by path.
	manifests map[string]string
}

// manifestLink is one link of an export's links.json (docs/adr/0051 D4).
type manifestLink struct {
	Source string `json:"source"`
	Type   string `json:"type"`
	Target string `json:"target"`
	path   string
}

// exportManifest is what the import reads of an export's manifest.json.
type exportManifest struct {
	Format   string `json:"format"`
	Tenant   string `json:"tenant"`
	Projects []struct {
		Key string `json:"key"`
	} `json:"projects"`
}

// ExportFormat is the format an export's manifest names (docs/adr/0051 D4).
const ExportFormat = "cowork export v1"

// Read parses the files of an upload.
func Read(sources []Source) *Upload {
	u := &Upload{sources: sources, files: map[string]*File{}, manifests: map[string]string{}}
	for _, s := range sources {
		if s.Skip != "" {
			continue
		}
		switch path.Base(s.Path) {
		case ManifestFile:
			u.manifests[s.Path] = readManifest(s.Content)
		case LinksFile:
			u.manifests[s.Path] = u.readLinks(s)
		default:
			f := Parse(s.Path, s.Content)
			u.files[s.Path] = &f
		}
	}
	return u
}

func readManifest(content []byte) string {
	var m exportManifest
	if err := json.Unmarshal(content, &m); err != nil || m.Format != ExportFormat {
		return "a manifest.json that is no manifest of a cowork export; the import reads the tickets beside it without it"
	}
	keys := make([]string, 0, len(m.Projects))
	for _, p := range m.Projects {
		keys = append(keys, m.Tenant+"/"+p.Key)
	}
	return "the manifest of an export of " + strings.Join(keys, ", ") + " (docs/adr/0051 D4)"
}

func (u *Upload) readLinks(s Source) string {
	var links []manifestLink
	if err := json.Unmarshal(s.Content, &links); err != nil {
		return "a links.json that is no links manifest of a cowork export; its links are not read"
	}
	for i := range links {
		links[i].path = s.Path
	}
	u.links = append(u.links, links...)
	return fmt.Sprintf("the links manifest of an export: %d links, read with the tickets they join (docs/adr/0051 D4)", len(links))
}

// Needs are what the analysis asks of the project before it runs.
type Needs struct {
	// Numbers are the numbers the ticket files bring, for the conflicts;
	// Referenced the numbers their references name that no file brings.
	Numbers, Referenced []int32
	// Usernames and Subjects are the identities the assignees name: local
	// accounts, and persons of the configured issuer.
	Usernames, Subjects []string
}

// Needs lists what the analysis needs to know of the project and its members.
// Referenced holds every number a reference names, those the upload brings
// as well: a file a correction excludes leaves its number to the project's
// ticket.
func (u *Upload) Needs(issuer string) Needs {
	var n Needs
	values := []string{}
	for _, f := range u.sortedFiles() {
		if f.Number != 0 {
			n.Numbers = append(n.Numbers, f.Number)
		}
		if id, ok := parseIdentity(f.Assignee); ok {
			switch {
			case id.username != "":
				n.Usernames = append(n.Usernames, id.username)
			case id.issuer == issuer && issuer != "":
				n.Subjects = append(n.Subjects, id.subject)
			}
		}
		values = append(values, f.BlockedBy, f.FiledFrom, f.Parent)
	}
	for _, l := range u.links {
		values = append(values, l.Source, l.Target)
	}
	n.Referenced = u.referencedNumbers(values)
	slices.Sort(n.Numbers)
	return n
}

// referencedNumbers are the numbers of the project the values name, sorted.
func (u *Upload) referencedNumbers(values []string) []int32 {
	project := u.sourceProject()
	seen := map[int32]bool{}
	var out []int32
	for _, v := range values {
		if num, ok := u.referenceNumber(v, project); ok && !seen[num] {
			seen[num] = true
			out = append(out, num)
		}
	}
	slices.Sort(out)
	return out
}

// sortedFiles are the ticket files in the order the upload carried them.
func (u *Upload) sortedFiles() []*File {
	out := make([]*File, 0, len(u.files))
	for _, s := range u.sources {
		if f, ok := u.files[s.Path]; ok && f.Skip == "" {
			out = append(out, f)
		}
	}
	return out
}

// sourceProject is the project every export file of the upload names,
// `<tenant>/<PROJECT>` — the project whose numbers the import keeps; "" when
// the upload holds none or several.
func (u *Upload) sourceProject() string {
	project := ""
	for _, f := range u.sortedFiles() {
		if f.Format != FormatExport || f.Key.Tenant == "" {
			continue
		}
		p := f.Key.Tenant + "/" + f.Key.Project
		if project != "" && project != p {
			return ""
		}
		project = p
	}
	return project
}

// referenceNumber is the number a reference names in the project the upload
// comes from: `T<n>` of a repository, or a full key of the source project.
func (u *Upload) referenceNumber(v, project string) (int32, bool) {
	if m := repositoryID.FindStringSubmatch(v); m != nil {
		n := number(m[1])
		return n, n != 0
	}
	k, err := domain.ParseTicketKey(v)
	if err != nil || k.Tenant == "" || k.Tenant+"/"+k.Project != project {
		return 0, false
	}
	return k.Number, true
}

// Target is the project an import goes into and what the analysis needs to
// know of it, read by the caller in the transaction it runs in.
type Target struct {
	Tenant, Project string
	// Taken are the numbers a ticket of the project holds, deleted ones
	// included; Purged the numbers a purged ticket held (docs/adr/0064 D3,
	// docs/adr/0007 D4).
	Taken, Purged map[int32]bool
	// Existing are the project's live tickets the caller sees that the
	// upload's references name, by number.
	Existing map[int32]uuid.UUID
	// Persons are the members an assignee's identity names, each one who can
	// see the project, by identity; Assignees those the corrections name, by
	// id.
	Persons   map[string]Person
	Assignees map[uuid.UUID]Person
	// Issuer is the identity provider's issuer, "" for none: an identity of
	// another issuer is never resolved (docs/adr/0044 D1).
	Issuer string
}

// Key is the full key a number gets in the target project.
func (t Target) Key(n int32) string { return domain.FullKey(t.Tenant, t.Project, n) }

// Result is the analysis: the report, and the plan the execution writes.
type Result struct {
	Report Report
	Plan   Plan
}

// Blocking lists the files whose error or conflict refuses an execution
// (docs/adr/0064 D3).
func (r Result) Blocking() []*FileReport {
	var out []*FileReport
	for _, f := range r.Report.Files {
		if f.Outcome == OutcomeError || f.Outcome == OutcomeConflict {
			out = append(out, f)
		}
	}
	return out
}

// Plan is what the execution writes: the tickets, parents before their
// children, and the links between them and the project's tickets.
type Plan struct {
	Tickets []*PlannedTicket
	Links   []PlannedLink
	// Highest is the highest number; the project's sequence advances past it.
	Highest int32
}

// Ref names a ticket of a plan: an imported one by its number, or one of the
// project by its id.
type Ref struct {
	Number int32
	ID     uuid.UUID
}

// PlannedTicket is a ticket the execution creates.
type PlannedTicket struct {
	Path, Title, Body string
	Number            int32
	Type              domain.TicketType
	State             domain.TicketState
	Severity          domain.Severity
	Security          domain.SecurityClass
	Threat            string
	Horizon           domain.Urgency
	Effort            domain.Effort
	Stages            [3]int
	Opened, Decided   *time.Time
	Done              *time.Time
	DoneByHand        bool
	// Note is the done act's verification note or the dropped act's reason.
	Note  string
	Block *PlannedBlock
	// Assignee is the member it is assigned to, nil for nobody; Parent its
	// parent, nil for none.
	Assignee           *uuid.UUID
	Parent             *Ref
	Confidential       bool
	ConfidentialReason string
	Questions          []Question
	// Attachments are the names the source lists, which the import does not
	// bring.
	Attachments []string
}

// PlannedBlock is the block of a ticket created as blocked; WaitsOn the ticket
// a block of kind ticket waits on.
type PlannedBlock struct {
	Kind    domain.BlockKind
	Reason  string
	From    domain.TicketState
	WaitsOn *Ref
}

// PlannedLink is a link the execution creates.
type PlannedLink struct {
	Type           domain.LinkType
	Source, Target Ref
}

// entry is a ticket file under analysis.
type entry struct {
	f    *File
	rep  *FileReport
	corr *Correction
	plan *PlannedTicket
	// excluded is a correction's exclusion; conflict the key whose number
	// the file brings again.
	excluded bool
	conflict string
	// waitsOn is the reference a block of kind ticket waits on, "" for one an
	// export's links name.
	waitsOn string
	// related are the lines the body gains under `## Related`.
	related []string
}

func (e *entry) outcome() Outcome {
	switch {
	case e.excluded:
		return OutcomeExclude
	case len(e.f.Errors) > 0:
		return OutcomeError
	case e.conflict != "":
		return OutcomeConflict
	}
	return OutcomeCreate
}

// creates reports whether the execution creates the file's ticket.
func (e *entry) creates() bool { return e.outcome() == OutcomeCreate }

// analysis is one run of Analyze.
type analysis struct {
	u        *Upload
	t        Target
	corr     map[string]*Correction
	reports  []*FileReport
	entries  []*entry
	byNumber map[int32][]*entry
	byKey    map[string]*entry
	project  string
}

// Analyze reads the upload against the target project with the corrections
// a person sent — none for a dry run — into the report and the plan
// (docs/adr/0051 D2, docs/adr/0063). The corrections must have passed Check.
func Analyze(u *Upload, t Target, corrections []Correction) Result {
	a := &analysis{u: u, t: t, corr: map[string]*Correction{}, byNumber: map[int32][]*entry{},
		byKey: map[string]*entry{}, project: u.sourceProject()}
	for i := range corrections {
		a.corr[corrections[i].Path] = &corrections[i]
	}
	a.classify()
	a.numbers()
	for _, e := range a.entries {
		if !e.excluded && !e.f.unreadable {
			a.columns(e)
		}
	}
	a.settle()
	plan := a.plan()
	for _, e := range a.entries {
		a.finish(e)
	}
	return Result{Report: Report{Summary: a.summary(plan), Files: a.reports}, Plan: plan}
}

// classify reports every file of the upload in its order: a skipped one with
// its reason, a ticket file as an entry.
func (a *analysis) classify() {
	for _, s := range a.u.sources {
		rep := &FileReport{Path: s.Path, Outcome: OutcomeSkip, Questions: []QuestionReport{}, Links: []LinkReport{},
			Attachments: []string{}, Warnings: []MessageReport{}, Errors: []MessageReport{}}
		a.reports = append(a.reports, rep)
		f, ticket := a.u.files[s.Path]
		switch {
		case s.Skip != "":
			rep.Reason = ptr(s.Skip)
		case a.u.manifests[s.Path] != "":
			rep.Reason = ptr(a.u.manifests[s.Path])
		case !ticket:
			rep.Reason = ptr(skipNotMarkdown)
		case f.Skip != "":
			rep.Reason = ptr(f.Skip)
		default:
			a.enter(f, rep)
		}
	}
}

// enter takes a ticket file into the analysis.
func (a *analysis) enter(f *File, rep *FileReport) {
	e := &entry{f: f, rep: rep, corr: a.corr[f.Path], plan: &PlannedTicket{Path: f.Path, Number: f.Number}}
	e.excluded = e.corr != nil && e.corr.Exclude
	a.entries = append(a.entries, e)
	if f.Number != 0 && !e.excluded {
		a.byNumber[f.Number] = append(a.byNumber[f.Number], e)
	}
	if f.Format == FormatExport && f.Key.Tenant != "" {
		a.byKey[domain.FullKey(f.Key.Tenant, f.Key.Project, f.Key.Number)] = e
	}
}

// numbers holds every number to the project and to the upload: a number
// another file brings too is an error of both, one the project holds — or a
// purged ticket held — a conflict (docs/adr/0064 D3, docs/adr/0007 D4).
func (a *analysis) numbers() {
	for _, e := range a.entries {
		n := e.f.Number
		switch {
		case e.excluded:
			continue
		case n == 0:
			if len(e.f.Errors) == 0 {
				e.f.fail(fieldFile, 0, "the file names no ticket number")
			}
			continue
		}
		for _, other := range a.byNumber[n] {
			if other != e {
				e.f.fail(fieldFile, 0, "the upload brings the number %d twice, here and in %s", n, other.f.Path)
			}
		}
		switch {
		case a.t.Taken[n]:
			e.conflict = a.t.Key(n)
		case a.t.Purged[n]:
			e.conflict = a.t.Key(n)
			e.f.warn(fieldFile, 0, "%s was purged; its number is not handed out again (docs/adr/0007 D4)", a.t.Key(n))
		}
	}
}

// summary counts the report.
func (a *analysis) summary(plan Plan) Summary {
	s := Summary{Files: len(a.reports)}
	for _, r := range a.reports {
		switch r.Outcome {
		case OutcomeCreate, OutcomeCreated:
			if r.Outcome == OutcomeCreate {
				s.Create++
			} else {
				s.Created++
			}
			if r.State != nil && !r.State.Terminal() {
				s.Open++
			}
			if r.Confidential {
				s.Confidential++
			}
		case OutcomeConflict:
			s.Conflict++
		case OutcomeError:
			s.Error++
		case OutcomeSkip:
			s.Skip++
		case OutcomeExclude:
			s.Exclude++
		}
	}
	if plan.Highest > 0 {
		s.HighestNumber = ptr(plan.Highest)
	}
	return s
}
