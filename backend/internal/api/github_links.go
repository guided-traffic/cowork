package api

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/github"
	"github.com/guided-traffic/cowork/backend/internal/store"
	"github.com/guided-traffic/cowork/backend/internal/store/writeq"
)

// The entities, acts and fields of what GitHub's webhook links to a ticket
// (docs/adr/0071 D6). A pull request's state changes are acts of their own;
// its first link to a ticket is linked, a person's removal unlinked, and a
// change of its title, page or author updated, which the activity leaves out.
// No act carries a title or a body: the payload stays out of the audit record,
// which cannot forget (docs/adr/0026 D3).
const (
	entityPullRequest = "pull_request"
	entityCommit      = "commit"
	actionMerged      = "merged"
	actionClosed      = "closed"
	actionReopened    = "reopened"
	fieldFoundIn      = "found_in"
	fieldSHA          = "sha"
)

// maxPushedCommits bounds the commits of one push that are read: a push of
// more links the first of them.
const maxPushedCommits = 100

// ticketRef is a ticket a key resolved to: its id and its canonical key.
type ticketRef struct {
	id  uuid.UUID
	key string
}

// linkTarget is a ticket a text names, and where the key was read.
type linkTarget struct {
	ticketRef
	foundIn string
}

// applyDelivery writes what a delivery links (docs/adr/0071 D4–D6): a pull
// request of a read action whose author is the repository's owner, a member
// of its organisation or a collaborator, or a push to the default branch;
// every other delivery changes nothing.
func applyDelivery(ctx context.Context, w *store.Writer, hook hookTenant, d delivery, now time.Time) error {
	switch {
	case d.pr != nil && d.pr.Read():
		return applyPullRequest(ctx, w, hook, *d.pr, now)
	case d.push != nil && d.push.ToDefaultBranch():
		return applyPush(ctx, w, hook, *d.push, now)
	}
	return nil
}

// boundIdentity is the identity of a delivery's repository, normalised as a
// binding's (docs/adr/0066 D1), when a project of the tenant binds it; empty
// otherwise, and then the delivery links nothing (docs/adr/0071 D4).
func boundIdentity(ctx context.Context, w *store.Writer, tenantID uuid.UUID, cloneURL string) (string, error) {
	identity, err := domain.NormaliseRemote(cloneURL)
	if err != nil {
		return "", nil
	}
	bound, err := w.RepositoryBoundInTenant(ctx, writeq.RepositoryBoundInTenantParams{TenantID: tenantID, Identity: identity})
	if err != nil || !bound {
		return "", err
	}
	return identity, nil
}

// resolveKeys reads the tickets of the tenant the keys name, by short key: a
// full key of another tenant is passed over, and so is a key of no ticket or
// of a deleted one (docs/adr/0071 D5).
func resolveKeys(ctx context.Context, w *store.Writer, hook hookTenant, keys []github.Key) (map[string]ticketRef, error) {
	var short []string
	asked := map[string]bool{}
	for _, k := range keys {
		s := domain.ShortKey(k.Project, k.Number)
		if (k.Tenant != "" && k.Tenant != hook.slug) || asked[s] {
			continue
		}
		asked[s] = true
		short = append(short, s)
	}
	found := map[string]ticketRef{}
	if len(short) == 0 {
		return found, nil
	}
	rows, err := w.ResolveTicketKeys(ctx, writeq.ResolveTicketKeysParams{TenantID: hook.id, Keys: short})
	for _, row := range rows {
		found[domain.ShortKey(row.ProjectKey, row.Number)] = ticketRef{id: row.ID,
			key: domain.FullKey(hook.slug, row.ProjectKey, row.Number)}
	}
	return found, err
}

// targetsOf are the tickets a text's keys name, in the text's order, each
// once, by the first place it was read.
func targetsOf(keys []github.Key, found map[string]ticketRef, slug string) []linkTarget {
	seen := map[uuid.UUID]bool{}
	var out []linkTarget
	for _, k := range keys {
		if k.Tenant != "" && k.Tenant != slug {
			continue
		}
		t, ok := found[domain.ShortKey(k.Project, k.Number)]
		if !ok || seen[t.id] {
			continue
		}
		seen[t.id] = true
		out = append(out, linkTarget{ticketRef: t, foundIn: k.FoundIn})
	}
	return out
}

// applyPullRequest links the tickets a pull request's title and body name,
// then brings the pull request's facts up to date on every ticket it is linked
// to (docs/adr/0071 D5, D6). Nothing changes a ticket's state.
func applyPullRequest(ctx context.Context, w *store.Writer, hook hookTenant, pr github.PullRequest, now time.Time) error {
	identity, err := boundIdentity(ctx, w, hook.id, pr.Repository.CloneURL)
	if identity == "" || err != nil {
		return err
	}
	keys := github.PullRequestKeys(pr.Title, pr.Body)
	found, err := resolveKeys(ctx, w, hook, keys)
	if err != nil {
		return err
	}
	for _, t := range targetsOf(keys, found, hook.slug) {
		if err := linkPullRequest(ctx, w, hook, identity, pr, t, now); err != nil {
			return err
		}
	}
	return updatePullRequest(ctx, w, hook, identity, pr, now)
}

// linkPullRequest links a pull request to a ticket, an act linked; a merged
// one tells the assignee and the watchers as a merge does. A ticket that holds
// the link, or held it until a person removed it, changes nothing here.
func linkPullRequest(ctx context.Context, w *store.Writer, hook hookTenant, identity string, pr github.PullRequest, t linkTarget, now time.Time) error {
	at := pr.UpdatedAt
	id, err := w.InsertTicketPullRequest(ctx, writeq.InsertTicketPullRequestParams{TenantID: hook.id, TicketID: t.id,
		Repository: identity, Number: pr.Number, Title: pr.Title, State: pr.State, Url: pullRequestPage(identity, pr.Number),
		Author:   optional(pr.Author),
		MergedAt: pr.MergedAt, FoundIn: t.foundIn, SourceUpdatedAt: &at, SeenAt: now})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	w.Record(store.Event{EntityType: entityPullRequest, EntityID: id, TicketID: t.id, TicketKey: t.key, Action: actionLinked,
		After:   map[string]any{fieldNumber: pr.Number, fieldRepository: identity, fieldState: pr.State, fieldFoundIn: t.foundIn},
		Notices: mergeNotices(pr.State)})
	return nil
}

// updatePullRequest writes a pull request's facts on each of its links that
// holds no newer ones, and records each change: a state change as an act of
// its own — a merge tells the assignee and the watchers (docs/adr/0020 D2) —,
// another change of its facts as updated. A deleted ticket's link keeps the
// facts quietly.
func updatePullRequest(ctx context.Context, w *store.Writer, hook hookTenant, identity string, pr github.PullRequest, now time.Time) error {
	rows, err := w.UpdatePullRequestFacts(ctx, writeq.UpdatePullRequestFactsParams{Title: pr.Title, State: pr.State,
		Url: pullRequestPage(identity, pr.Number), Author: optional(pr.Author), MergedAt: pr.MergedAt, SourceUpdatedAt: pr.UpdatedAt, SeenAt: now,
		TenantID: hook.id, Repository: identity, Number: pr.Number})
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row.TicketDeleted {
			continue
		}
		e := store.Event{EntityType: entityPullRequest, EntityID: row.ID, TicketID: row.TicketID,
			TicketKey: domain.FullKey(hook.slug, row.ProjectKey, row.TicketNumber),
			After:     map[string]any{fieldNumber: pr.Number, fieldRepository: identity, fieldState: pr.State}}
		switch action := stateAct(row.StateBefore, pr.State); {
		case action != "":
			e.Action, e.Before, e.Notices = action, map[string]any{fieldState: row.StateBefore}, mergeNotices(pr.State)
		case row.FactsChanged:
			e.Action = actionUpdated
		default:
			continue
		}
		w.Record(e)
	}
	return nil
}

// stateAct is the act of a pull request's state change: merged, closed, or
// reopened; none when the state stayed.
func stateAct(before, after string) string {
	switch {
	case before == after:
		return ""
	case after == github.StateMerged:
		return actionMerged
	case after == github.StateClosed:
		return actionClosed
	default:
		return actionReopened
	}
}

// mergeNotices tells the assignee and the watchers of a ticket that its pull
// request merged (docs/adr/0020 D2 as amended 2026-10-06) — the watchers hold
// the assignee (docs/adr/0013 D6). Whoever cannot see the ticket is told
// nothing; the state stays a person's to move.
func mergeNotices(state string) []store.Notice {
	if state != github.StateMerged {
		return nil
	}
	return []store.Notice{{Reason: store.NoticeMerged, Watchers: true}}
}

// applyPush links the tickets the messages of a push to the default branch
// name, each commit by its trailers, else its subject's short keys
// (docs/adr/0071 D5): a pull request's own commits are not fetched — cowork
// calls nothing at GitHub (D2) — and reach a ticket this way once they land.
func applyPush(ctx context.Context, w *store.Writer, hook hookTenant, push github.Push, now time.Time) error {
	identity, err := boundIdentity(ctx, w, hook.id, push.Repository.CloneURL)
	if identity == "" || err != nil {
		return err
	}
	commits := push.Commits[:min(len(push.Commits), maxPushedCommits)]
	keysOf := make([][]github.Key, len(commits))
	var all []github.Key
	for i, c := range commits {
		keysOf[i] = github.CommitKeys(c.Message)
		all = append(all, keysOf[i]...)
	}
	found, err := resolveKeys(ctx, w, hook, all)
	if err != nil {
		return err
	}
	for i, c := range commits {
		for _, t := range targetsOf(keysOf[i], found, hook.slug) {
			if err := linkCommit(ctx, w, hook, identity, c, t, now); err != nil {
				return err
			}
		}
	}
	return nil
}

// linkCommit links a commit that reached the default branch to a ticket, an
// act linked; it tells nobody — a pull request's merge does. A ticket that
// holds the link, or held it until a person removed it, changes nothing.
func linkCommit(ctx context.Context, w *store.Writer, hook hookTenant, identity string, c github.Commit, t linkTarget, now time.Time) error {
	id, err := w.InsertTicketCommit(ctx, writeq.InsertTicketCommitParams{TenantID: hook.id, TicketID: t.id,
		Repository: identity, Sha: c.SHA, Title: c.Subject(), Url: commitPage(identity, c.SHA), Author: optional(c.Author),
		SeenAt:  now,
		FoundIn: t.foundIn})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	w.Record(store.Event{EntityType: entityCommit, EntityID: id, TicketID: t.id, TicketKey: t.key, Action: actionLinked,
		After: map[string]any{fieldSHA: c.SHA, fieldRepository: identity, fieldFoundIn: t.foundIn}})
	return nil
}

// pullRequestPage and commitPage are the pages of a pull request and a commit
// at GitHub, written from the bound repository's identity and never taken
// from a payload: whoever holds the secret puts no link to anywhere but the
// bound repository on a ticket (docs/adr/0071 D6). GitHub's own pages, and
// those of a GitHub Enterprise Server, are https://<host>/<owner>/<repo>/….
func pullRequestPage(identity string, number int32) string {
	return "https://" + identity + "/pull/" + strconv.FormatInt(int64(number), 10)
}

func commitPage(identity, sha string) string {
	return "https://" + identity + "/commit/" + sha
}

// optional is a string a column holds as NULL when it is empty.
func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
