package importer

import (
	"encoding/json"

	"github.com/google/uuid"

	"github.com/guided-traffic/cowork/backend/internal/domain"
)

// Outcome is what happens to a file of the upload (docs/adr/0051 D2,
// docs/adr/0064 D3).
type Outcome string

// The outcomes, as the API document's ImportOutcome names them.
const (
	OutcomeCreate   Outcome = "create"
	OutcomeConflict Outcome = "conflict"
	OutcomeError    Outcome = "error"
	OutcomeSkip     Outcome = "skip"
	OutcomeExclude  Outcome = "exclude"
	OutcomeCreated  Outcome = "created"
)

// Report is what a job answers: the summary and every file of the upload. Its
// JSON is the API document's ImportSummary and ImportFile, field by field,
// and is what the job stores.
type Report struct {
	Summary Summary       `json:"summary"`
	Files   []*FileReport `json:"files"`
}

// Summary counts the files by outcome.
type Summary struct {
	Files         int    `json:"files"`
	Create        int    `json:"create"`
	Conflict      int    `json:"conflict"`
	Error         int    `json:"error"`
	Skip          int    `json:"skip"`
	Exclude       int    `json:"exclude"`
	Created       int    `json:"created"`
	Open          int    `json:"open"`
	Confidential  int    `json:"confidential"`
	HighestNumber *int32 `json:"highest_number"`
}

// FileReport is one file as the report shows it.
type FileReport struct {
	Path               string              `json:"path"`
	Outcome            Outcome             `json:"outcome"`
	Reason             *string             `json:"reason"`
	Format             *Format             `json:"format"`
	Number             *int32              `json:"number"`
	Key                *string             `json:"key"`
	Conflict           *string             `json:"conflict"`
	Title              *string             `json:"title"`
	Type               *domain.TicketType  `json:"type"`
	TypeReason         *string             `json:"type_reason"`
	State              *domain.TicketState `json:"state"`
	BlockCandidate     *domain.BlockKind   `json:"block_candidate"`
	Block              *BlockReport        `json:"block"`
	Columns            *Columns            `json:"columns"`
	Assignee           *AssigneeReport     `json:"assignee"`
	Parent             *string             `json:"parent"`
	Note               *string             `json:"note"`
	Questions          []QuestionReport    `json:"questions"`
	Links              []LinkReport        `json:"links"`
	Attachments        []string            `json:"attachments"`
	Confidential       bool                `json:"confidential"`
	ConfidentialReason *string             `json:"confidential_reason"`
	Warnings           []MessageReport     `json:"warnings"`
	Errors             []MessageReport     `json:"errors"`
	Correction         *Correction         `json:"correction"`
}

// BlockReport is the block of a ticket created as blocked.
type BlockReport struct {
	Kind   domain.BlockKind   `json:"kind"`
	Reason string             `json:"reason"`
	From   domain.TicketState `json:"from"`
	Ticket *string            `json:"ticket"`
}

// Columns are a ticket's columns as the import reads them; dates are
// YYYY-MM-DD.
type Columns struct {
	Severity           *domain.Severity      `json:"severity"`
	Security           *domain.SecurityClass `json:"security"`
	Threat             *string               `json:"threat"`
	Horizon            *domain.Urgency       `json:"horizon"`
	Effort             *domain.Effort        `json:"effort"`
	ProgressRefinement int                   `json:"progress_refinement"`
	Progress           int                   `json:"progress"`
	ProgressReview     int                   `json:"progress_review"`
	Opened             *string               `json:"opened"`
	Decided            *string               `json:"decided"`
	Done               *string               `json:"done"`
}

// AssigneeReport is an assignee as the file writes it and the member it is.
type AssigneeReport struct {
	Source string  `json:"source"`
	Person *Person `json:"person"`
}

// Person is a member as the API names one.
type Person struct {
	ID       uuid.UUID `json:"id"`
	Username *string   `json:"username"`
	Name     string    `json:"display_name"`
}

// QuestionReport is a question the import creates.
type QuestionReport struct {
	Number   int32  `json:"number"`
	Question string `json:"question"`
	Status   string `json:"status"`
}

// LinkReport is a link the import creates, read from the file's end.
type LinkReport struct {
	Type      domain.LinkType `json:"type"`
	Direction string          `json:"direction"`
	Key       string          `json:"key"`
	Source    string          `json:"source"`
}

// MessageReport is a warning or an error.
type MessageReport struct {
	Field   *string `json:"field"`
	Line    *int    `json:"line"`
	Message string  `json:"message"`
}

// Correction is a person's correction of one file before the execution
// (docs/adr/0051 D2, docs/adr/0063 D1): leave it out, or change its type, its
// state with the block a state blocked needs, or its assignee — AssigneeSet
// says the correction names one, nil for nobody.
type Correction struct {
	Path        string
	Exclude     bool
	Type        domain.TicketType
	State       domain.TicketState
	Block       *BlockCorrection
	AssigneeSet bool
	Assignee    *uuid.UUID
}

// BlockCorrection is the block a correction to the state blocked gives the
// ticket; From is empty where the file's own state is it.
type BlockCorrection struct {
	Kind   domain.BlockKind   `json:"kind"`
	Reason string             `json:"reason"`
	From   domain.TicketState `json:"from,omitempty"`
}

// MarshalJSON writes what the correction names and nothing else, as the API
// document's ImportCorrection.
func (c Correction) MarshalJSON() ([]byte, error) {
	out := map[string]any{"path": c.Path}
	if c.Exclude {
		out["exclude"] = true
	}
	if c.Type != "" {
		out["type"] = c.Type
	}
	if c.State != "" {
		out["state"] = c.State
	}
	if c.Block != nil {
		out["block"] = c.Block
	}
	if c.AssigneeSet {
		out["assignee"] = c.Assignee
	}
	return json.Marshal(out)
}

// UnmarshalJSON reads what MarshalJSON writes: a stored report's correction.
func (c *Correction) UnmarshalJSON(b []byte) error {
	var in struct {
		Path     string             `json:"path"`
		Exclude  bool               `json:"exclude"`
		Type     domain.TicketType  `json:"type"`
		State    domain.TicketState `json:"state"`
		Block    *BlockCorrection   `json:"block"`
		Assignee json.RawMessage    `json:"assignee"`
	}
	if err := json.Unmarshal(b, &in); err != nil {
		return err
	}
	*c = Correction{Path: in.Path, Exclude: in.Exclude, Type: in.Type, State: in.State, Block: in.Block}
	if len(in.Assignee) > 0 {
		c.AssigneeSet = true
		if string(in.Assignee) != "null" {
			var id uuid.UUID
			if err := json.Unmarshal(in.Assignee, &id); err != nil {
				return err
			}
			c.Assignee = &id
		}
	}
	return nil
}

func ptr[T any](v T) *T { return &v }

// optional is a pointer to s, nil for "".
func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func messages(in []Message) []MessageReport {
	out := make([]MessageReport, 0, len(in))
	for _, m := range in {
		r := MessageReport{Message: m.Message, Field: optional(m.Field)}
		if m.Line > 0 {
			r.Line = ptr(m.Line)
		}
		out = append(out, r)
	}
	return out
}
