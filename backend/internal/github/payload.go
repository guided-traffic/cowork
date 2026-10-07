package github

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"
)

// The events cowork reads (docs/adr/0071 D4); every other is taken and passed
// over.
const (
	EventPullRequest = "pull_request"
	EventPush        = "push"
)

// The states a pull request's link shows: GitHub's open and closed, and
// merged for a closed pull request whose changes were merged — and for a
// commit that reached the default branch.
const (
	StateOpen   = "open"
	StateClosed = "closed"
	StateMerged = "merged"
)

// readActions are the actions of a pull_request delivery cowork reads; an
// assignment, a label or a review request changes nothing a ticket shows.
var readActions = map[string]bool{"opened": true, "edited": true, "synchronize": true, "reopened": true, "closed": true}

// linkedAuthors are the authors whose pull requests cowork links
// (docs/adr/0071 D4 as amended 2026-10-07), by GitHub's author_association:
// the owner of the repository, a member of the organisation that owns it, a
// collaborator of it. A pull request of any other author is taken and passed
// over.
var linkedAuthors = map[string]bool{"OWNER": true, "MEMBER": true, "COLLABORATOR": true}

// The bounds of what a delivery writes into a ticket's list: a title is cut,
// an author beyond its bound is left out. No page a payload names is kept: the
// page of a pull request or a commit is written from the bound repository's
// identity, so a delivery cannot put a link to anywhere else on a ticket.
const (
	maxTitle  = 500
	maxAuthor = 100
)

// ErrPayload is a payload that is not what GitHub sends for its event.
var ErrPayload = errors.New("the payload is not what GitHub sends for this event")

// Repository is what a delivery says of its repository: the clone URL, whose
// normalised identity finds the binding (docs/adr/0066 D1), and the default
// branch a push must reach.
type Repository struct {
	CloneURL      string
	DefaultBranch string
}

// PullRequest is what cowork reads of a pull_request delivery.
type PullRequest struct {
	// Action is GitHub's: opened, edited, synchronize, reopened, closed, …
	Action     string
	Repository Repository
	Number     int32
	Title      string
	Body       string
	// State is open, closed or merged.
	State  string
	Author string
	// AuthorAssociation is how GitHub relates the author to the repository:
	// OWNER, MEMBER, COLLABORATOR, CONTRIBUTOR, FIRST_TIMER, …
	AuthorAssociation string
	// MergedAt is when it was merged; nil when it was not.
	MergedAt *time.Time
	// UpdatedAt orders the deliveries of one pull request: an older one
	// changes nothing a newer one wrote.
	UpdatedAt time.Time
}

// Read reports whether cowork reads the delivery: an action it reads, of a
// pull request of an author whose pull requests it links.
func (p PullRequest) Read() bool { return readActions[p.Action] && linkedAuthors[p.AuthorAssociation] }

// Push is what cowork reads of a push delivery.
type Push struct {
	Ref        string
	Repository Repository
	Commits    []Commit
}

// ToDefaultBranch reports whether the push reached the repository's default
// branch, the only push cowork reads (docs/adr/0071 D4).
func (p Push) ToDefaultBranch() bool {
	return p.Repository.DefaultBranch != "" && p.Ref == "refs/heads/"+p.Repository.DefaultBranch
}

// Commit is one pushed commit: its id, its message and its author.
type Commit struct {
	SHA     string
	Message string
	Author  string
}

// Subject is the commit's first line, cut to the title's bound.
func (c Commit) Subject() string {
	subject, _, _ := strings.Cut(c.Message, "\n")
	return Cut(strings.TrimSpace(strings.TrimSuffix(subject, "\r")))
}

type repositoryPayload struct {
	CloneURL      string `json:"clone_url"`
	DefaultBranch string `json:"default_branch"`
}

type userPayload struct {
	Login string `json:"login"`
}

type pullRequestPayload struct {
	Action      string `json:"action"`
	PullRequest *struct {
		Number            int64       `json:"number"`
		Title             string      `json:"title"`
		Body              *string     `json:"body"`
		State             string      `json:"state"`
		Merged            bool        `json:"merged"`
		MergedAt          *time.Time  `json:"merged_at"`
		UpdatedAt         time.Time   `json:"updated_at"`
		User              userPayload `json:"user"`
		AuthorAssociation string      `json:"author_association"`
	} `json:"pull_request"`
	Repository repositoryPayload `json:"repository"`
}

type pushPayload struct {
	Ref     string `json:"ref"`
	Commits []struct {
		ID      string `json:"id"`
		Message string `json:"message"`
		Author  struct {
			Username string `json:"username"`
		} `json:"author"`
	} `json:"commits"`
	Repository repositoryPayload `json:"repository"`
}

// ParsePullRequest reads a pull_request delivery's body.
func ParsePullRequest(body []byte) (PullRequest, error) {
	var p pullRequestPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return PullRequest{}, fmt.Errorf("%w: %w", ErrPayload, err)
	}
	pr := p.PullRequest
	if pr == nil || pr.Number <= 0 || pr.Number > math.MaxInt32 || pr.UpdatedAt.IsZero() {
		return PullRequest{}, fmt.Errorf("%w: a pull request needs its number and updated_at", ErrPayload)
	}
	out := PullRequest{
		Action:            p.Action,
		Repository:        Repository(p.Repository),
		Number:            int32(pr.Number),
		Title:             Cut(pr.Title),
		State:             StateOpen,
		Author:            author(pr.User.Login),
		AuthorAssociation: pr.AuthorAssociation,
		UpdatedAt:         pr.UpdatedAt,
	}
	if pr.Body != nil {
		out.Body = *pr.Body
	}
	if pr.State == StateClosed {
		out.State = StateClosed
		if pr.Merged || pr.MergedAt != nil {
			out.State, out.MergedAt = StateMerged, pr.MergedAt
		}
	}
	if out.State == StateMerged && out.MergedAt == nil {
		at := pr.UpdatedAt
		out.MergedAt = &at
	}
	return out, nil
}

// ParsePush reads a push delivery's body. A commit without an id of forty or
// sixty-four hexadecimal characters is passed over.
func ParsePush(body []byte) (Push, error) {
	var p pushPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return Push{}, fmt.Errorf("%w: %w", ErrPayload, err)
	}
	out := Push{Ref: p.Ref, Repository: Repository(p.Repository)}
	for _, c := range p.Commits {
		if !commitID(c.ID) {
			continue
		}
		out.Commits = append(out.Commits, Commit{SHA: strings.ToLower(c.ID), Message: c.Message,
			Author: author(c.Author.Username)})
	}
	return out, nil
}

// Cut bounds a title to what a ticket's list shows, on a character boundary.
func Cut(s string) string {
	if utf8.RuneCountInString(s) <= maxTitle {
		return s
	}
	return string([]rune(s)[:maxTitle-1]) + "…"
}

// author is a login within its bound, or none.
func author(login string) string {
	if login == "" || utf8.RuneCountInString(login) > maxAuthor {
		return ""
	}
	return login
}

func commitID(id string) bool {
	if len(id) != 40 && len(id) != 64 {
		return false
	}
	for _, c := range strings.ToLower(id) {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
