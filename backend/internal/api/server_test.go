package api

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/store"
)

// replay decodes a stored body as the type T and encodes it as the replay
// answers it.
func replay[T any](t *testing.T, body string) map[string]any {
	t.Helper()
	v, err := replayed[T](&store.Result{Body: []byte(body)})
	require.NoError(t, err)
	b, err := json.Marshal(v)
	require.NoError(t, err)
	var out map[string]any
	require.NoError(t, json.Unmarshal(b, &out))
	return out
}

// An answer stored by the release before the token of an act existed
// (docs/adr/0036 D6) is replayed with the token as null — the act's own row,
// written by that release, holds none — and never as a token with the nil id.
func TestAReplayAnswersARequiredFieldTheStoredAnswerLacksAsNull(t *testing.T) {
	comment := replay[apigen.Comment](t, `{"id":"0199a3c2-1d2e-7f00-8000-000000000001",
		"author":{"id":"0199a3c2-1d2e-7f00-8000-000000000002","username":null,"display_name":"Ada"},
		"agent":null,"body":"Found it.","withdrawn":false,"withdrawn_at":null,"edited":false,"explains":[],
		"version":1,"created_at":"2026-10-04T10:00:00Z","updated_at":"2026-10-04T10:00:00Z"}`)
	assert.Contains(t, comment, "token")
	assert.Nil(t, comment["token"], "a token the stored answer did not carry")
	assert.Equal(t, []any{}, comment["mentions"], "a required list the stored answer did not carry is empty, never null")
	assert.Equal(t, "Found it.", comment["body"], "what the stored answer carries stays")

	question := replay[apigen.Question](t, `{"id":"0199a3c2-1d2e-7f00-8000-000000000003","number":1,
		"question":"Ship it?","options":"","recommendation":"","answer":null,"status":"open",
		"asked_by":{"id":"0199a3c2-1d2e-7f00-8000-000000000002","username":null,"display_name":"Ada"},
		"asked_by_agent":"claude-code/opus/s-1","asked_of":null,"answered_by":null,"answered_at":null,
		"recorded_by_agent":false,"withdrawn_at":null,"version":1,"created_at":"2026-10-04T10:00:00Z",
		"updated_at":"2026-10-04T10:00:00Z"}`)
	assert.Nil(t, question["asked_by_token"])
	assert.Nil(t, question["answered_by_token"])
	assert.Equal(t, "claude-code/opus/s-1", question["asked_by_agent"])

	stored := replay[apigen.TimeEntry](t, `{"id":"0199a3c2-1d2e-7f00-8000-000000000004","ticket":"acme/COW-1",
		"person":{"id":"0199a3c2-1d2e-7f00-8000-000000000002","username":null,"display_name":"Ada"},
		"author":{"id":"0199a3c2-1d2e-7f00-8000-000000000002","username":null,"display_name":"Ada"},
		"token":{"id":"0199a3c2-1d2e-7f00-8000-000000000005","name":"ci-script"},
		"minutes":30,"day":"2026-10-01","note":"","voided":false,"voided_at":null,"edited":false,"version":1,
		"created_at":"2026-10-04T10:00:00Z","updated_at":"2026-10-04T10:00:00Z"}`)
	assert.Equal(t, map[string]any{"id": "0199a3c2-1d2e-7f00-8000-000000000005", "name": "ci-script"}, stored["token"],
		"a token the stored answer carries stays")
}

// A filing an older release stored for its Idempotency-Key carries the
// horizon under the name it had then (docs/adr/0010 D1): its replay answers
// the horizon and the horizon set from those fields, never an empty horizon or
// a null that the ticket's row does not hold.
func TestAReplayOfAFilingStoredBeforeTheHorizonAnswersIt(t *testing.T) {
	filing := func(urgency, override string) apigen.Ticket {
		body := `{"id":"0199a3c2-1d2e-7f00-8000-000000000001","key":"acme/COW-1","project":"COW","number":1,"type":"task",
			"title":"t","body":"","state":"filed","block":null,"severity":"low","security":"none","threat":null,
			"urgency":"` + urgency + `","urgency_derived":"later","urgency_rule":"v2:default","urgency_override":` + override + `,
			"effort":"S","progress":0,"progress_refinement":0,"progress_review":0,"progress_derived":false,"parent":null,
			"reporter":{"id":"0199a3c2-1d2e-7f00-8000-000000000002","username":null,"display_name":"Ada"},"reporter_agent":null,
			"reporter_token":null,"assignee":null,"confidential":false,"opened_at":"2026-10-04T10:00:00Z","decided_at":null,
			"done_at":null,"done_from":null,"done_by_hand":false,"open_prerequisites":0,"score":null,"score_version":null,
			"version":1,"created_at":"2026-10-04T10:00:00Z","updated_at":"2026-10-04T10:00:00Z"}`
		v, err := replayed[apigen.Ticket](&store.Result{Body: []byte(body)})
		require.NoError(t, err)
		horizonOfStored(&v)
		return v
	}
	v := filing("next", `{"value":"next","reason":null,"at":"2026-10-04T10:00:00Z",
		"by":{"id":"0199a3c2-1d2e-7f00-8000-000000000002","username":null,"display_name":"Ada"}}`)
	assert.Equal(t, apigen.Horizon("next"), v.Horizon)
	set := v.HorizonSet.MustGet()
	assert.Equal(t, apigen.Horizon("next"), set.Value)
	assert.Equal(t, "0199a3c2-1d2e-7f00-8000-000000000002", set.By.MustGet().Id.String())

	v = filing("later", "null")
	assert.Equal(t, apigen.Horizon("later"), v.Horizon)
	assert.True(t, v.HorizonSet.IsNull(), "nothing set, nothing answered")

	current := ticketView(tenantScope{Slug: "acme"}, store.TicketRow{UrgencyDerived: "later", Number: 2}, time.Time{})
	current.Horizon = "now"
	horizonOfStored(&current)
	assert.Equal(t, apigen.Horizon("now"), current.Horizon, "an answer this release stored keeps its horizon")
}

// A field the document leaves optional is left out of a replay as the stored
// answer left it out: the token list's last-used day of a token never used.
func TestAReplayLeavesOutAnOptionalFieldTheStoredAnswerLeftOut(t *testing.T) {
	created := replay[apigen.TokenCreated](t, `{"id":"0199a3c2-1d2e-7f00-8000-000000000005","name":"ci-script",
		"scope":"write","agent":false,"capabilities":[],"created_at":"2026-10-04T10:00:00Z",
		"expires_at":"2027-01-02T10:00:00Z","state":"active","restricted_tenant":null,"revoked_at":null}`)
	assert.NotContains(t, created, "last_used_on")
	assert.NotContains(t, created, "restricted_project_id")
	assert.NotContains(t, created, "token", "a replay never carries the plaintext")
	assert.Contains(t, created, "revoked_at")
}
