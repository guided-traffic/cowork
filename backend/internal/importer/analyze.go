package importer

import (
	"encoding/json"
	"fmt"
	"maps"
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

// exportManifest is what the import reads of an export's manifest.json. It
// names its team by team; an archive written before a tenant was called a
// team names it by tenant alone, and is read so for good, since an archive
// outlives a release (docs/adr/0051 D4, docs/adr/0005 D1).
type exportManifest struct {
	Format   string `json:"format"`
	Team     string `json:"team"`
	Tenant   string `json:"tenant"`
	Projects []struct {
		Key string `json:"key"`
	} `json:"projects"`
}

// team is the slug of the team the archive was exported from: team, or
// tenant where the archive names no team.
func (m exportManifest) team() string {
	if m.Team != "" {
		return m.Team
	}
	return m.Tenant
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
		keys = append(keys, m.team()+"/"+p.Key)
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
	// External are the canonical keys the references name outside the
	// project the upload comes from — another project, another team —, which
	// the caller resolves for the importing person (docs/adr/0051 D9).
	External []string
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
	files := u.sortedFiles()
	values := make([]string, 0, 3*len(files)+2*len(u.links))
	for _, f := range files {
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
	n.External = u.externalKeys(values)
	slices.Sort(n.Numbers)
	return n
}

// MayLinkBlocks reports whether the upload can bring a blocks link: one of
// its links manifest, or a file's blocked-by — a block on a ticket, or a
// repository's prerequisite. An execution that may write one takes the lock of
// the blocks graph before anything else it locks.
func (u *Upload) MayLinkBlocks() bool {
	for _, l := range u.links {
		if l.Type == string(domain.LinkBlocks) {
			return true
		}
	}
	for _, f := range u.sortedFiles() {
		if f.BlockedBy != "" {
			return true
		}
	}
	return false
}

// externalKeys are the canonical keys among the values that name no ticket of
// the project the upload comes from, sorted and each once.
func (u *Upload) externalKeys(values []string) []string {
	project := u.sourceProject()
	seen := map[string]bool{}
	var out []string
	for _, v := range values {
		k, err := domain.ParseTicketKey(v)
		if err != nil || k.Tenant == "" || k.Tenant+"/"+k.Project == project || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	slices.Sort(out)
	return out
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
	// included (docs/adr/0064 D3); Purged the numbers a purged ticket held,
	// which an import gives back (docs/adr/0007 D4).
	Taken, Purged map[int32]bool
	// Existing are the project's live tickets the caller sees that the
	// upload's references name, by number.
	Existing map[int32]uuid.UUID
	// External are the tickets of another project or another team the
	// references name by their canonical keys and the importing person reads,
	// by the key as the upload writes it (docs/adr/0008 D2, docs/adr/0051 D9);
	// a key the person does not read is absent.
	External map[string]External
	// Persons are the members an assignee's identity names, each one who can
	// see the project, by identity; Assignees those the corrections name, by
	// id.
	Persons   map[string]Person
	Assignees map[uuid.UUID]Person
	// Issuer is the identity provider's issuer, "" for none: an identity of
	// another issuer is never resolved (docs/adr/0044 D1).
	Issuer string
	// Named are the members the dry run's report assigned, by the file's
	// path, for an execution; nil while a dry run is analysed. An execution
	// assigns by a file's identity only the member its dry run named: a
	// person who became a member who can see the project since is assigned
	// nobody, as the report the person read said.
	Named map[string]uuid.UUID
	// TokenPerson is the person of a request through a token or an agent's,
	// nil for a person's own browser session: such a request assigns a
	// confidential ticket to that person or to nobody, since its assignee is
	// admitted to it (docs/adr/0043 D3, docs/adr/0065 D9).
	TokenPerson *uuid.UUID
}

// Key is the full key a number gets in the target project.
func (t Target) Key(n int32) string { return domain.FullKey(t.Tenant, t.Project, n) }

// External is a ticket of another project or another team a reference names
// and the importing person reads: its id and its team's slug.
type External struct {
	ID   uuid.UUID
	Team string
}

// Result is the analysis: the report, and the plan the execution writes.
type Result struct {
	Report Report
	Plan   Plan
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
	// Key is the canonical key of a ticket of another project or another
	// team, by which the report and the acts name it; empty for a ticket the
	// import creates or one of the project.
	Key string
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
	// unnamed is the member a file's identity resolves to at an execution
	// whose dry run did not name them, who is assigned nobody.
	unnamed *Person
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
	// long are the errors of the files whose texts a run before found
	// longer than the API takes, by path; tooLong those this run finds.
	long, tooLong map[string][]Message
}

// Analyze reads the upload against the target project with the corrections
// a person sent — none for a dry run — into the report and the plan
// (docs/adr/0051 D2, docs/adr/0063). The corrections must have passed Check.
// A file with an error or a conflict is reported and left out of the plan
// (docs/adr/0051 D2). A text the plan makes longer than the API takes is
// known only once the plan stands — the keys and the lines the import puts
// in depend on what it creates —, so a file found so is an error, and the
// analysis runs again without it, until no file is.
func Analyze(u *Upload, t Target, corrections []Correction) Result {
	long := map[string][]Message{}
	for {
		a := &analysis{u: u, t: t, corr: map[string]*Correction{}, byNumber: map[int32][]*entry{},
			byKey: map[string]*entry{}, project: u.sourceProject(), long: long, tooLong: map[string][]Message{}}
		for i := range corrections {
			a.corr[corrections[i].Path] = &corrections[i]
		}
		r := a.run()
		if len(a.tooLong) == 0 {
			return r
		}
		maps.Copy(long, a.tooLong)
	}
}

// run is one analysis of the upload.
func (a *analysis) run() Result {
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

// enter takes a ticket file into the analysis, a copy of it: a run writes
// its messages and texts into the file it reads, and a next run starts from
// the file as it was parsed.
func (a *analysis) enter(parsed *File, rep *FileReport) {
	f := parsed.clone()
	f.Errors = append(f.Errors, a.long[f.Path]...)
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
// another file brings too is an error of both, one the project holds a
// conflict (docs/adr/0064 D3); one a purged ticket held is given back, with
// a warning (docs/adr/0007 D4).
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
			e.f.warn(fieldFile, 0, "%s was purged; the import gives its number back, and what named %s before names this ticket now (docs/adr/0007 D4)",
				a.t.Key(n), a.t.Key(n))
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
