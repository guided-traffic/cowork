package api

import (
	"encoding/hex"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/github"
	"github.com/guided-traffic/cowork/backend/internal/store"
)

// docs/adr/0071 D6: a pull request's state change is an act of its own, and a
// state that stayed is none.
func TestStateAct(t *testing.T) {
	for _, c := range []struct{ before, after, act string }{
		{github.StateOpen, github.StateOpen, ""},
		{github.StateOpen, github.StateMerged, actionMerged},
		{github.StateClosed, github.StateMerged, actionMerged},
		{github.StateOpen, github.StateClosed, actionClosed},
		{github.StateClosed, github.StateOpen, actionReopened},
		{github.StateMerged, github.StateMerged, ""},
	} {
		assert.Equal(t, c.act, stateAct(c.before, c.after), "%s → %s", c.before, c.after)
	}
}

// docs/adr/0071 D6, docs/adr/0020 D2: a merge tells the watchers, whom the
// assignee is among; nothing else tells anybody.
func TestOnlyAMergeTells(t *testing.T) {
	assert.Equal(t, []store.Notice{{Reason: store.NoticeMerged, Watchers: true}}, mergeNotices(github.StateMerged))
	assert.Nil(t, mergeNotices(github.StateOpen))
	assert.Nil(t, mergeNotices(github.StateClosed))
}

// docs/adr/0071 D5: the tickets a text names, in its order, each once by the
// first place its key was read; a full key of another tenant, and a key that
// named no ticket, are passed over.
func TestTargetsOf(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	found := map[string]ticketRef{"VKO-1": {id: a, key: "acme/VKO-1"}, "VKO-2": {id: b, key: "acme/VKO-2"}}
	key := func(tenant, project string, n int32, in string) github.Key {
		return github.Key{TicketKey: domain.TicketKey{Tenant: tenant, Project: project, Number: n}, FoundIn: in}
	}
	got := targetsOf([]github.Key{
		key("other", "VKO", 2, github.FoundInTrailer),
		key("acme", "VKO", 1, github.FoundInBody),
		key("", "VKO", 1, github.FoundInSubject),
		key("", "VKO", 3, github.FoundInSubject),
		key("", "VKO", 2, github.FoundInSubject),
	}, found, "acme")
	assert.Equal(t, []linkTarget{
		{ticketRef: ticketRef{id: a, key: "acme/VKO-1"}, foundIn: github.FoundInBody},
		{ticketRef: ticketRef{id: b, key: "acme/VKO-2"}, foundIn: github.FoundInSubject},
	}, got)
}

// docs/adr/0071 D1: 256 random bits as 64 hexadecimal characters, a new one
// each time.
func TestNewWebhookSecret(t *testing.T) {
	s := newWebhookSecret()
	require.Len(t, s, 64)
	raw, err := hex.DecodeString(s)
	require.NoError(t, err)
	assert.Len(t, raw, 32)
	assert.NotEqual(t, s, newWebhookSecret())
}

// docs/adr/0071 D6: the page a ticket links is the bound repository's, never
// a payload's.
func TestThePagesAreTheBoundRepositorys(t *testing.T) {
	assert.Equal(t, "https://github.com/acme/app/pull/34", pullRequestPage("github.com/acme/app", 34))
	assert.Equal(t, "https://ghe.example.com/org/sub/repo/commit/0d1a26e6",
		commitPage("ghe.example.com/org/sub/repo", "0d1a26e6"))
}

func TestWebhookPathAndFoundInWord(t *testing.T) {
	assert.Equal(t, "/api/v1/tenants/acme/integrations/github/webhook", webhookPath("acme"))
	assert.Equal(t, "title", foundInWord(entityPullRequest, github.FoundInSubject))
	assert.Equal(t, "subject", foundInWord(entityCommit, github.FoundInSubject))
	assert.Equal(t, "trailer", foundInWord(entityPullRequest, github.FoundInTrailer))
}
