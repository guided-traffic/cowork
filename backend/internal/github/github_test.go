package github

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/domain"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	require.NoError(t, err)
	return b
}

// GitHub's own example of its documentation ("Validating webhook
// deliveries"): the secret "It's a Secret to Everybody" over the body "Hello,
// World!".
func TestSignAndVerifyMatchGitHubsExample(t *testing.T) {
	secret, body := []byte("It's a Secret to Everybody"), []byte("Hello, World!")
	const header = "sha256=757107ea0eb2509fc211221cce984b8a37570b6d7586c22c46f4379c8b043e17"
	assert.Equal(t, header, Sign(secret, body))
	assert.True(t, Verify(secret, body, header))
}

// docs/adr/0071 D3: a missing, malformed, truncated or wrong signature, another
// body or another secret, is refused.
func TestVerifyRefusesWhatIsNotTheSignature(t *testing.T) {
	secret, body := []byte("s3cret"), []byte(`{"zen":"Keep it logically awesome."}`)
	good := Sign(secret, body)
	for name, header := range map[string]string{
		"missing":       "",
		"sha1":          "sha1=" + strings.TrimPrefix(good, "sha256="),
		"not hex":       "sha256=zz" + good[9:],
		"truncated":     good[:len(good)-2],
		"another body":  Sign(secret, append(body, ' ')),
		"another key":   Sign([]byte("other"), body),
		"no prefix":     strings.TrimPrefix(good, "sha256="),
		"upper prefix":  "SHA256=" + strings.TrimPrefix(good, "sha256="),
		"trailing junk": good + "00",
	} {
		assert.False(t, Verify(secret, body, header), name)
	}
	assert.True(t, Verify(secret, body, good))
}

func TestParseAMergedPullRequest(t *testing.T) {
	pr, err := ParsePullRequest(fixture(t, "pull_request.closed.json"))
	require.NoError(t, err)
	assert.Equal(t, "closed", pr.Action)
	assert.True(t, pr.Read())
	assert.Equal(t, int32(34), pr.Number)
	assert.Equal(t, StateMerged, pr.State)
	require.NotNil(t, pr.MergedAt)
	assert.Equal(t, time.Date(2026, 10, 6, 9, 30, 0, 0, time.UTC), pr.MergedAt.UTC())
	assert.Equal(t, time.Date(2026, 10, 6, 9, 30, 0, 0, time.UTC), pr.UpdatedAt.UTC())
	assert.Equal(t, "https://github.com/acme/app/pull/34", pr.URL)
	assert.Equal(t, "octocat", pr.Author)
	assert.Equal(t, "fix(controller): guard the failover gate (VKO-12)", pr.Title)
	assert.Equal(t, Repository{CloneURL: "https://github.com/acme/app.git", DefaultBranch: "main"}, pr.Repository)
}

func TestParseAnOpenedPullRequestWithoutBody(t *testing.T) {
	pr, err := ParsePullRequest(fixture(t, "pull_request.opened.json"))
	require.NoError(t, err)
	assert.Equal(t, StateOpen, pr.State)
	assert.Nil(t, pr.MergedAt)
	assert.Empty(t, pr.Body)
}

// A closed pull request that was not merged is closed; one merged without a
// time is merged as of its update.
func TestParseTheStatesOfAPullRequest(t *testing.T) {
	body := func(state string, merged bool, mergedAt string) []byte {
		return []byte(`{"action":"closed","pull_request":{"number":1,"html_url":"https://github.com/a/b/pull/1",` +
			`"updated_at":"2026-10-06T10:00:00Z","state":"` + state + `","merged":` + map[bool]string{true: "true", false: "false"}[merged] +
			`,"merged_at":` + mergedAt + `,"title":"t","user":{"login":"x"}},"repository":{"clone_url":"https://github.com/a/b.git"}}`)
	}
	closed, err := ParsePullRequest(body("closed", false, "null"))
	require.NoError(t, err)
	assert.Equal(t, StateClosed, closed.State)
	assert.Nil(t, closed.MergedAt)
	merged, err := ParsePullRequest(body("closed", true, "null"))
	require.NoError(t, err)
	assert.Equal(t, StateMerged, merged.State)
	require.NotNil(t, merged.MergedAt)
	assert.Equal(t, merged.UpdatedAt, *merged.MergedAt)
}

func TestParseRefusesWhatGitHubDoesNotSend(t *testing.T) {
	for name, body := range map[string]string{
		"not json":       `{`,
		"no pr":          `{"action":"opened"}`,
		"no number":      `{"pull_request":{"html_url":"https://github.com/a/b/pull/1","updated_at":"2026-10-06T10:00:00Z"}}`,
		"huge number":    `{"pull_request":{"number":4294967296,"html_url":"https://github.com/a/b/pull/1","updated_at":"2026-10-06T10:00:00Z"}}`,
		"no update":      `{"pull_request":{"number":1,"html_url":"https://github.com/a/b/pull/1"}}`,
		"plain http url": `{"pull_request":{"number":1,"html_url":"http://github.com/a/b/pull/1","updated_at":"2026-10-06T10:00:00Z"}}`,
	} {
		_, err := ParsePullRequest([]byte(body))
		assert.ErrorIs(t, err, ErrPayload, name)
	}
	_, err := ParsePush([]byte(`[]`))
	assert.ErrorIs(t, err, ErrPayload)
}

// docs/adr/0071 D4: only a push to the default branch is read; a commit
// without a proper id is passed over.
func TestParseAPushToTheDefaultBranch(t *testing.T) {
	push, err := ParsePush(fixture(t, "push.json"))
	require.NoError(t, err)
	assert.True(t, push.ToDefaultBranch())
	require.Len(t, push.Commits, 2, "the commit without an id is passed over")
	assert.Equal(t, "0d1a26e67d8f5eaf1f6ba5c57fc3c7d91ac0fd1c", push.Commits[0].SHA)
	assert.Equal(t, "octocat", push.Commits[0].Author)
	assert.Equal(t, "fix(controller): guard the failover gate (VKO-12) (#34)", push.Commits[0].Subject())
	assert.Empty(t, push.Commits[1].Author, "no GitHub login, no author")

	push.Ref = "refs/heads/feature"
	assert.False(t, push.ToDefaultBranch())
	push.Ref, push.Repository.DefaultBranch = "refs/heads/main", ""
	assert.False(t, push.ToDefaultBranch(), "without a default branch nothing is the default branch")
}

func TestOnlyTheReadActionsAreRead(t *testing.T) {
	for _, action := range []string{"opened", "edited", "synchronize", "reopened", "closed"} {
		assert.True(t, PullRequest{Action: action}.Read(), action)
	}
	for _, action := range []string{"assigned", "labeled", "review_requested", "ready_for_review", ""} {
		assert.False(t, PullRequest{Action: action}.Read(), action)
	}
}

func keyStrings(keys []Key) []string {
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		s := domain.ShortKey(k.Project, k.Number)
		if k.Tenant != "" {
			s = k.Tenant + "/" + s
		}
		out = append(out, s+"@"+k.FoundIn)
	}
	return out
}

// docs/adr/0068 D1, D2, D5, docs/adr/0071 D5: the full keys of a pull
// request's body — trailers and bare full-key lines — first; the title's short
// keys only where the body has none; a key in running text never.
func TestPullRequestKeys(t *testing.T) {
	pr, err := ParsePullRequest(fixture(t, "pull_request.closed.json"))
	require.NoError(t, err)
	assert.Equal(t, []string{"acme/VKO-12@body", "acme/VKO-13@trailer"}, keyStrings(PullRequestKeys(pr.Title, pr.Body)))

	assert.Equal(t, []string{"VKO-7@subject", "OPS-3@subject"},
		keyStrings(PullRequestKeys("Update the README (VKO-7, OPS-3)", "")))
	assert.Equal(t, []string{"VKO-7@subject"},
		keyStrings(PullRequestKeys("feat: x (VKO-7) (#12)", "This mentions VKO-9 and acme/VKO-10 in a sentence.")),
		"running text names no key; GitHub's (#12) is passed over")
	assert.Empty(t, PullRequestKeys("fix(VKO-7): a scope is no key", ""))
	assert.Empty(t, PullRequestKeys("(VKO-7) at the start", ""))
	assert.Empty(t, PullRequestKeys("lower case (vko-7)", ""))
	assert.Empty(t, PullRequestKeys("a leading zero (VKO-07)", ""))
	assert.Equal(t, []string{"other/VKO-1@trailer"}, keyStrings(PullRequestKeys("x (VKO-2)", "cowork-ticket:other/VKO-1")),
		"a key of another tenant is read here and passed over where it is resolved")
}

func TestCommitKeys(t *testing.T) {
	assert.Equal(t, []string{"acme/VKO-12@trailer"},
		keyStrings(CommitKeys("fix: x (VKO-99)\n\nbody\n\nCowork-Ticket: acme/VKO-12\nSigned-off-by: A <a@b>")),
		"the trailer wins over the subject")
	assert.Equal(t, []string{"VKO-99@subject", "VKO-100@subject"}, keyStrings(CommitKeys("fix: x (VKO-99, VKO-100) (#3)\r\n\r\nbody")))
	assert.Empty(t, CommitKeys("acme/VKO-1\n\na bare full key in a commit is no trailer"))
	assert.Empty(t, CommitKeys(""))
}

// A text names a ticket once, by the first place it was read, and at most
// MaxKeys tickets: a delivery that names more links the first of them.
func TestKeysAreBoundedAndReadOnce(t *testing.T) {
	var body strings.Builder
	body.WriteString("acme/VKO-1\n")
	for i := 1; i <= MaxKeys+10; i++ {
		body.WriteString("Cowork-Ticket: acme/VKO-" + strconv.Itoa(i) + "\n")
	}
	keys := PullRequestKeys("", body.String())
	require.Len(t, keys, MaxKeys)
	assert.Equal(t, Key{TicketKey: domain.TicketKey{Tenant: "acme", Project: "VKO", Number: 1}, FoundIn: FoundInBody}, keys[0])
	assert.Equal(t, int32(MaxKeys), keys[MaxKeys-1].Number)
}

func TestCutKeepsATitleWithinItsBound(t *testing.T) {
	assert.Equal(t, "short", Cut("short"))
	long := strings.Repeat("ä", maxTitle+20)
	cut := Cut(long)
	assert.Equal(t, maxTitle, len([]rune(cut)))
	assert.True(t, strings.HasSuffix(cut, "…"))
}
