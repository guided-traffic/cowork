package importer

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/guided-traffic/cowork/backend/internal/domain"
)

// The frontmatter keys the import reads: grammar v1's (docs/adr/0044 D1) and a
// repository's (docs/tickets/README.md).
const (
	keyID                  = "id"
	keyKey                 = "key"
	keyTitle               = "title"
	keyType                = "type"
	keyState               = "state"
	keySeverity            = "severity"
	keySecurity            = "security"
	keyThreat              = "threat"
	keyHorizon             = "horizon"
	keyUrgency             = "urgency"
	keyEffort              = "effort"
	keyAssignee            = "assignee"
	keyParent              = "parent"
	keyOpened              = "opened"
	keyDecided             = "decided"
	keyDone                = "done"
	keyShipped             = "shipped"
	keyDroppedReason       = "dropped-reason"
	keyBlockedBy           = "blocked-by"
	keyBlockedReason       = "blocked-reason"
	keyBlockedFrom         = "blocked-from"
	keyFiledFrom           = "filed-from"
	keyPublicationAccepted = "publication-accepted"
	keyConfidential        = "confidential"
	keyAttachments         = "attachments"
)

// stageKeys are the three progress stages in the order of File.Stages
// (docs/adr/0017 D2).
var stageKeys = [3]string{"progress-refinement", "progress", "progress-review"}

// repositoryID is a repository's ticket id, T and the number.
var repositoryID = regexp.MustCompile(`^T([1-9][0-9]{0,9})$`)

// frontReader reads the frontmatter's keys into a File.
type frontReader struct {
	f                *File
	horizon, urgency string
}

// read reads one key; a value that breaks its key's rule is an error of the
// file, an unknown key a warning: its value is not read.
func (r *frontReader) read(p pair) {
	if p.key == keyAttachments {
		r.attachments(p)
		return
	}
	v, ok := r.scalar(p)
	if !ok || v == "" {
		return
	}
	if set, known := textKeys[p.key]; known {
		set(r.f, v)
		return
	}
	if check, known := vocabularyKeys[p.key]; known {
		if !check(r, p, v) {
			r.f.fail(p.key, p.line, "%s: %q is outside the vocabulary (docs/adr/0010 D5)", p.key, v)
		}
		return
	}
	r.special(p, v)
}

// special reads the keys whose value is a number, a date, a key or a flag.
func (r *frontReader) special(p pair, v string) {
	f := r.f
	switch p.key {
	case keyID:
		r.id(p, v)
	case keyKey:
		r.key(p, v)
	case keyOpened:
		f.Opened = r.date(p, v)
	case keyDecided:
		f.Decided = r.date(p, v)
	case keyDone:
		f.Done = r.date(p, v)
	case keyPublicationAccepted:
		if r.date(p, v) != nil {
			f.PublicationAccepted = v
		}
	case keyConfidential:
		r.confidential(p, v)
	default:
		if i := stageIndex(p.key); i >= 0 {
			r.stage(p, v, i)
			return
		}
		f.warn(p.key, p.line, "the key %s is not read", p.key)
	}
}

// textKeys are the keys whose value is text.
var textKeys = map[string]func(f *File, v string){
	keyTitle:         func(f *File, v string) { f.Title = oneLine(v) },
	keyThreat:        func(f *File, v string) { f.Threat = v },
	keyAssignee:      func(f *File, v string) { f.Assignee = v },
	keyParent:        func(f *File, v string) { f.Parent = v },
	keyShipped:       func(f *File, v string) { f.Shipped = v },
	keyDroppedReason: func(f *File, v string) { f.DroppedReason = v },
	keyBlockedBy:     func(f *File, v string) { f.BlockedBy = v },
	keyBlockedReason: func(f *File, v string) { f.BlockedReason = v },
	keyBlockedFrom:   func(f *File, v string) { f.BlockedFrom = v },
	keyFiledFrom:     func(f *File, v string) { f.FiledFrom = v },
}

// vocabularyKeys are the keys whose value is one of a fixed set; each reports
// whether the value is.
var vocabularyKeys = map[string]func(r *frontReader, p pair, v string) bool{
	keyType: func(r *frontReader, _ pair, v string) bool {
		return in(v, &r.f.Type, domain.TypeTask, domain.TypeBug, domain.TypeFeature, domain.TypeDecision, domain.TypeQuestion)
	},
	keyState: func(r *frontReader, _ pair, v string) bool {
		return in(v, &r.f.State, domain.StateFiled, domain.StateAnalysed, domain.StateDecided, domain.StateInProgress,
			domain.StateReview, domain.StateBlocked, domain.StateDone, domain.StateDropped)
	},
	keySeverity: func(r *frontReader, _ pair, v string) bool {
		return in(v, &r.f.Severity, "critical", "high", "medium", "low", "cosmetic")
	},
	keySecurity: func(r *frontReader, _ pair, v string) bool {
		return in(v, &r.f.Security, domain.SecurityLive, domain.SecurityBoundary, domain.SecurityHardening, domain.SecurityNone)
	},
	keyEffort: func(r *frontReader, _ pair, v string) bool {
		return in(v, &r.f.Effort, "XS", "S", "M", "L")
	},
	keyHorizon: func(r *frontReader, _ pair, v string) bool {
		var u domain.Urgency
		ok := horizonValue(v, &u)
		r.horizon = string(u)
		return ok
	},
	keyUrgency: func(r *frontReader, _ pair, v string) bool {
		var u domain.Urgency
		ok := horizonValue(v, &u)
		r.urgency = string(u)
		return ok
	},
}

func horizonValue(v string, u *domain.Urgency) bool {
	return in(v, u, domain.UrgencyNow, domain.UrgencyRelease, domain.UrgencyNext, domain.UrgencyLater, domain.UrgencyIcebox)
}

// in sets *target to v when v is one of the values.
func in[T ~string](v string, target *T, values ...T) bool {
	for _, w := range values {
		if T(v) == w {
			*target = w
			return true
		}
	}
	return false
}

func stageIndex(key string) int {
	for i, k := range stageKeys {
		if k == key {
			return i
		}
	}
	return -1
}

// scalar is a key's one value, trimmed; "" for an empty one. A list or a
// mapping where one value belongs is an error.
func (r *frontReader) scalar(p pair) (string, bool) {
	if p.value.Kind != yaml.ScalarNode {
		r.f.fail(p.key, p.line, "%s takes one value, not a list or a mapping", p.key)
		return "", false
	}
	if p.value.Tag == yamlNull {
		return "", true
	}
	return strings.TrimSpace(p.value.Value), true
}

// id reads a repository's `id: T<n>`, which must agree with the file name.
func (r *frontReader) id(p pair, v string) {
	m := repositoryID.FindStringSubmatch(v)
	if m == nil {
		r.f.fail(p.key, p.line, "id: %q is not T and a number", v)
		return
	}
	r.number(p, number(m[1]))
}

// key reads an export's key, `<tenant>/<PROJECT>-<n>`.
func (r *frontReader) key(p pair, v string) {
	k, err := domain.ParseTicketKey(v)
	if err != nil || k.Tenant == "" {
		r.f.fail(p.key, p.line, "key: %q is not a full key, <tenant>/<PROJECT>-<number>", v)
		return
	}
	r.f.Key = k
	r.number(p, k.Number)
}

// number takes the number a key names; the file name must say the same.
func (r *frontReader) number(p pair, n int32) {
	if n == 0 {
		r.f.fail(p.key, p.line, "%s names no number in the column's range", p.key)
		return
	}
	if r.f.Number != 0 && r.f.Number != n {
		r.f.fail(p.key, p.line, "%s names the number %d, the file name %d", p.key, n, r.f.Number)
		return
	}
	r.f.Number = n
}

// date reads a date, YYYY-MM-DD, or a time whose UTC date it takes.
func (r *frontReader) date(p pair, v string) *time.Time {
	if t, err := time.Parse(time.DateOnly, v); err == nil {
		return &t
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		d := time.Date(t.UTC().Year(), t.UTC().Month(), t.UTC().Day(), 0, 0, 0, 0, time.UTC)
		return &d
	}
	r.f.fail(p.key, p.line, "%s: %q is not a date, YYYY-MM-DD", p.key, v)
	return nil
}

// stage reads a progress stage, 0 to 100 in steps of five
// (docs/adr/0017 D2).
func (r *frontReader) stage(p pair, v string, i int) {
	n, err := strconv.Atoi(v)
	if err != nil || !domain.ValidProgress(n) {
		r.f.fail(p.key, p.line, "%s: %q is not 0 to 100 in steps of five", p.key, v)
		return
	}
	r.f.Stages[i] = n
	r.f.Staged = true
}

// confidential reads an export's flag (docs/adr/0065 D7).
func (r *frontReader) confidential(p pair, v string) {
	switch v {
	case "true":
		r.f.Confidential = true
	case "false":
	default:
		r.f.fail(p.key, p.line, "confidential: %q is neither true nor false", v)
	}
}

// attachments reads an export's list of attachment names
// (docs/adr/0044 D6).
func (r *frontReader) attachments(p pair) {
	if p.value.Kind == yaml.ScalarNode && p.value.Tag == yamlNull {
		return
	}
	if p.value.Kind != yaml.SequenceNode {
		r.f.fail(p.key, p.line, "attachments is a list of file names")
		return
	}
	for _, n := range p.value.Content {
		if n.Kind != yaml.ScalarNode {
			r.f.fail(p.key, p.line, "attachments is a list of file names")
			return
		}
		r.f.Attachments = append(r.f.Attachments, n.Value)
	}
}

// finish holds the file to what its grammar requires once every key is read.
func (r *frontReader) finish(base string) {
	f := r.f
	switch {
	case r.horizon != "" && r.urgency != "" && r.horizon != r.urgency:
		f.fail(keyUrgency, f.line(keyUrgency), "urgency %s and horizon %s disagree; the import reads urgency as horizon (docs/adr/0044 D3)", r.urgency, r.horizon)
	case r.horizon != "":
		f.Horizon = domain.Urgency(r.horizon)
	case r.urgency != "":
		f.Horizon = domain.Urgency(r.urgency)
	}
	if f.Format == FormatRepository && f.line(keyID) == 0 && f.Number != 0 {
		f.warn(keyID, 0, "the file has no id; its number comes from its name %s", base)
	}
	required(f)
}

// required holds a file to the keys every ticket has (docs/adr/0010 D1,
// D2): the title, the state, the severity, the security class with its
// threat, the effort.
func required(f *File) {
	for _, k := range []struct {
		key  string
		have bool
	}{
		{keyTitle, f.Title != ""}, {keyState, f.State != ""}, {keySeverity, f.Severity != ""},
		{keySecurity, f.Security != ""}, {keyEffort, f.Effort != ""},
	} {
		if !k.have && !f.failed(k.key) {
			f.fail(k.key, f.line(k.key), "the frontmatter names no %s", k.key)
		}
	}
	if len([]rune(f.Title)) > maxTitle {
		f.fail(keyTitle, f.line(keyTitle), "the title is longer than %d characters", maxTitle)
	}
	switch {
	case f.Security != "" && f.Security != domain.SecurityNone && f.Threat == "":
		f.fail(keyThreat, f.line(keyThreat), "security %s needs the threat it names (docs/adr/0010 D2)", f.Security)
	case f.Security == domain.SecurityNone && f.Threat != "":
		f.fail(keyThreat, f.line(keyThreat), "a ticket without a security class carries no threat (docs/adr/0010 D2)")
	}
}
