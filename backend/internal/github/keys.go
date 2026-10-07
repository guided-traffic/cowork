package github

import (
	"regexp"
	"strings"

	"github.com/guided-traffic/cowork/backend/internal/domain"
)

// Where a key was read (docs/adr/0068 D1, D2, D5): a Cowork-Ticket trailer
// line, a line of a pull request's body that is a full key alone, or the short
// keys in parentheses at the end of a title or a commit's subject.
const (
	FoundInTrailer = "trailer"
	FoundInBody    = "body"
	FoundInSubject = "subject"
)

// MaxKeys bounds the keys one text names: a delivery that names more links the
// first of them only.
const MaxKeys = 50

// Key is a ticket key a text names and where it was read. Tenant is the slug
// of a full key, empty for a short one.
type Key struct {
	domain.TicketKey
	FoundIn string
}

var (
	// trailerLine is a git trailer naming a ticket: Cowork-Ticket: <key>. Git
	// reads a trailer's name without regard to case.
	trailerLine = regexp.MustCompile(`(?i)^cowork-ticket\s*:\s*(\S+)$`)
	// pullRequestNumber is the suffix GitHub gives a squash or merge commit's
	// subject, (#34), which may follow the keys.
	pullRequestNumber = regexp.MustCompile(`\s*\(#[0-9]+\)$`)
	// subjectKeys are the short keys in parentheses that end a subject:
	// (VKO-12) or (VKO-12, VKO-13).
	subjectKeys = regexp.MustCompile(`\(\s*([A-Z][A-Z0-9]{1,9}-[1-9][0-9]*(?:\s*,\s*[A-Z][A-Z0-9]{1,9}-[1-9][0-9]*)*)\s*\)$`)
)

// PullRequestKeys reads the keys a pull request names (docs/adr/0071 D5): the
// full keys of its body — Cowork-Ticket trailer lines, and lines that are a
// full key alone, as ADR 0068 D5 puts the full key on the body's first line —
// and, only where the body names none, the short keys at the end of its title,
// which ADR 0068 D5 makes the squash commit's subject. A key in running text
// is not read: a key mentioned is not a key meant.
func PullRequestKeys(title, body string) []Key {
	if keys := fullKeys(body, true); len(keys) > 0 {
		return keys
	}
	return shortKeys(title)
}

// CommitKeys reads the keys a commit message names (docs/adr/0068 D1, D2): its
// Cowork-Ticket trailers, and only where it has none, the short keys at the end
// of its subject.
func CommitKeys(message string) []Key {
	if keys := fullKeys(message, false); len(keys) > 0 {
		return keys
	}
	subject, _, _ := strings.Cut(message, "\n")
	return shortKeys(subject)
}

// fullKeys reads the trailer lines of a text and, where bare is set, the lines
// that are a full key alone. A trailer may carry a short key as well.
func fullKeys(text string, bare bool) []Key {
	var keys []Key
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if m := trailerLine.FindStringSubmatch(line); m != nil {
			if k, err := domain.ParseTicketKey(m[1]); err == nil {
				keys = add(keys, Key{TicketKey: k, FoundIn: FoundInTrailer})
			}
			continue
		}
		if !bare || !strings.Contains(line, "/") {
			continue
		}
		if k, err := domain.ParseTicketKey(line); err == nil {
			keys = add(keys, Key{TicketKey: k, FoundIn: FoundInBody})
		}
	}
	return keys
}

// shortKeys reads the short keys in parentheses at the end of a subject, past
// GitHub's (#n) suffixes.
func shortKeys(subject string) []Key {
	subject = strings.TrimSpace(strings.TrimSuffix(subject, "\r"))
	for {
		stripped := pullRequestNumber.ReplaceAllString(subject, "")
		if stripped == subject {
			break
		}
		subject = stripped
	}
	m := subjectKeys.FindStringSubmatch(subject)
	if m == nil {
		return nil
	}
	var keys []Key
	for _, part := range strings.Split(m[1], ",") {
		if k, err := domain.ParseTicketKey(strings.TrimSpace(part)); err == nil {
			keys = add(keys, Key{TicketKey: k, FoundIn: FoundInSubject})
		}
	}
	return keys
}

// add appends a key the list does not hold yet, up to MaxKeys; the first place
// a key was read is the one kept.
func add(keys []Key, k Key) []Key {
	if len(keys) >= MaxKeys {
		return keys
	}
	for _, have := range keys {
		if have.TicketKey == k.TicketKey {
			return keys
		}
	}
	return append(keys, k)
}
