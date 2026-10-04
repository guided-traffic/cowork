package api

import (
	"encoding/json"
	"testing"

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
