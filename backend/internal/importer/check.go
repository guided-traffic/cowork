package importer

import (
	"strconv"

	"github.com/guided-traffic/cowork/backend/internal/domain"
)

// CorrectionError is a correction the execution refuses: the JSON pointer of
// the field in the request's body and why.
type CorrectionError struct {
	Pointer string
	Message string
}

// Check holds the corrections to the upload and to the rules of the API
// document's ImportCorrection: each names a ticket file of the upload once; an
// exclusion takes no other correction; the state blocked takes a block — not
// on a ticket, from a state a block comes from — and nothing else does. That
// a corrected assignee may be assigned is the caller's to check.
func (u *Upload) Check(corrections []Correction) []CorrectionError {
	var out []CorrectionError
	seen := map[string]bool{}
	for i, c := range corrections {
		at := "/corrections/" + strconv.Itoa(i)
		f, ticket := u.files[c.Path]
		switch {
		case !u.holds(c.Path):
			out = append(out, CorrectionError{at + "/path", "names no file of the upload"})
			continue
		case seen[c.Path]:
			out = append(out, CorrectionError{at + "/path", "a second correction of the same file"})
			continue
		case !ticket || f.Skip != "":
			out = append(out, CorrectionError{at + "/path", "the file is no ticket file; it takes no correction"})
			continue
		}
		seen[c.Path] = true
		out = append(out, checkOne(at, c, f)...)
	}
	return out
}

// holds reports whether the upload has a file at the path.
func (u *Upload) holds(p string) bool {
	for _, s := range u.sources {
		if s.Path == p {
			return true
		}
	}
	return false
}

// checkOne holds one correction of a ticket file to the rules.
func checkOne(at string, c Correction, f *File) []CorrectionError {
	switch {
	case c.Exclude && (c.Type != "" || c.State != "" || c.Block != nil || c.AssigneeSet):
		return []CorrectionError{{at + "/exclude", "an excluded file takes no other correction"}}
	case c.Exclude:
		return nil
	case c.Block != nil && c.State != domain.StateBlocked:
		return []CorrectionError{{at + "/block", "a block goes with the state blocked only"}}
	case c.Block == nil && c.State == domain.StateBlocked && f.State != domain.StateBlocked:
		return []CorrectionError{{at + "/block", "the state blocked needs its block: what it waits on and why (docs/adr/0009 D2)"}}
	case c.Block == nil:
		return nil
	}
	return checkBlock(at, c.Block, f)
}

// checkBlock holds a correction's block to the kinds and the states a block
// takes (docs/adr/0009 D2).
func checkBlock(at string, b *BlockCorrection, f *File) []CorrectionError {
	switch {
	case b.Kind == domain.BlockTicket:
		return []CorrectionError{{at + "/block/kind", "a block on a ticket is a blocks link; make it after the import"}}
	case b.From != "" && !blockable[b.From]:
		return []CorrectionError{{at + "/block/from", "a block comes from filed, analysed, decided, in-progress or review"}}
	case b.From == "" && !blockable[f.State]:
		return []CorrectionError{{at + "/block/from", "the file's state " + string(f.State) + " is no state a block comes from; name the one it does"}}
	}
	return nil
}

// Executed turns the report of an execution that committed into what the job
// keeps: every file it was to create is created.
func (r *Report) Executed() {
	for _, f := range r.Files {
		if f.Outcome == OutcomeCreate {
			f.Outcome = OutcomeCreated
		}
	}
	r.Summary.Created, r.Summary.Create = r.Summary.Create, 0
}
