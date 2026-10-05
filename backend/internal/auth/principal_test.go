package auth

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/problem"
)

func TestParseAgentHeader(t *testing.T) {
	long := strings.Repeat("x", 64)
	cases := map[string]bool{
		"claude-code/claude-opus-5-5/s1": true,
		"Claude Code/opus/session 7":     true,
		long + "/" + long + "/" + long:   true,
		"":                               false,
		"claude-code":                    false,
		"claude-code/opus":               false,
		"a/b/c/d":                        false,
		"a//c":                           false,
		long + "x/b/c":                   false,
		" a/b/c":                         false,
		"a/b /c":                         false,
		"a/b/c\t":                        false,
		"a/b/é":                          false,
	}
	for in, ok := range cases {
		_, err := ParseAgentHeader(in)
		if ok {
			assert.NoError(t, err, "%q", in)
		} else {
			assert.Error(t, err, "%q", in)
		}
	}
}

// A flagged token is an agent's whatever the header says; no header value
// removes the flag's effect (docs/adr/0036 D2, D3 and its residual risks).
func TestMarkOnlyNarrows(t *testing.T) {
	assisted := []string{CapDrop, CapUpload}

	agent, caps := Mark(true, assisted, "")
	assert.Equal(t, UnknownAgent, agent)
	assert.Equal(t, assisted, caps)

	agent, caps = Mark(true, assisted, "claude-code/opus/s1")
	assert.Equal(t, "claude-code/opus/s1", agent)
	assert.Equal(t, assisted, caps, "the header never widens the token's set")

	agent, caps = Mark(false, nil, "script/none/s2")
	assert.Equal(t, "script/none/s2", agent, "a plain token's header marks the request")
	assert.Equal(t, AllCapabilities, caps)

	agent, caps = Mark(false, nil, "")
	assert.Empty(t, agent)
	assert.Empty(t, caps)
}

// The capability set-horizon was override-urgency before (docs/adr/0043 D4 as
// amended 2026-10-05): a set under either name is the set of this release's
// names, each once, in the order given, and a token stored with the old name
// holds the new one.
func TestCanonical(t *testing.T) {
	assert.Equal(t, []string{CapRank, CapSetHorizon, CapUpload}, Canonical([]string{CapRank, CapOverrideUrgency, CapUpload}))
	assert.Equal(t, []string{CapSetHorizon}, Canonical([]string{CapOverrideUrgency, CapSetHorizon}), "both names are one capability")
	assert.Equal(t, []string{CapSetHorizon}, Canonical([]string{CapSetHorizon, CapOverrideUrgency}))
	assert.Equal(t, []string{}, Canonical(nil))
	assert.Equal(t, AllCapabilities, Canonical(AllCapabilities), "this release's names stay")
	assert.NotContains(t, AllCapabilities, CapOverrideUrgency)
	assert.NotContains(t, DefaultChatCapabilities, CapOverrideUrgency)

	_, caps := Mark(true, []string{CapClose, CapOverrideUrgency}, "")
	p := Principal{Scope: domain.ScopeWrite, Agent: UnknownAgent, Capabilities: caps}
	assert.True(t, p.Can(CapSetHorizon), "a token stored with the old name holds set-horizon")
	assert.Nil(t, Authorize(p, domain.RoleMember, Need{Role: domain.RoleMember, Scope: domain.ScopeWrite, Capability: CapSetHorizon}))
}

// docs/adr/0043 D4 as amended 2026-10-05: a set this release stores carries
// override-urgency beside set-horizon, which the release before knows, and
// reads back as the set it was.
func TestStored(t *testing.T) {
	assert.Equal(t, []string{CapRank, CapSetHorizon, CapOverrideUrgency}, Stored([]string{CapRank, CapSetHorizon}))
	assert.Equal(t, []string{CapRank}, Stored([]string{CapRank}), "nothing to add without set-horizon")
	assert.Equal(t, []string{CapSetHorizon, CapOverrideUrgency}, Stored([]string{CapSetHorizon, CapOverrideUrgency}), "each name once")
	assert.Equal(t, []string{}, Stored([]string{}))
	for _, set := range [][]string{AllCapabilities, DefaultChatCapabilities, {CapRank, CapSetHorizon}} {
		assert.Equal(t, set, Canonical(Stored(set)), "a stored set reads back as the set it was")
	}
}

func TestAuthorize(t *testing.T) {
	person := Principal{Scope: domain.ScopeWrite}
	agent := Principal{Scope: domain.ScopeWrite, Agent: "a/b/c", Capabilities: []string{CapClose}}
	cases := map[string]struct {
		p    Principal
		role domain.Role
		need Need
		code string
	}{
		"member writes":              {person, domain.RoleMember, Need{Role: domain.RoleMember, Scope: domain.ScopeWrite}, ""},
		"viewer cannot write":        {person, domain.RoleViewer, Need{Role: domain.RoleMember, Scope: domain.ScopeWrite}, problem.Forbidden.Code},
		"read token cannot write":    {Principal{Scope: domain.ScopeRead}, domain.RoleAdmin, Need{Role: domain.RoleMember, Scope: domain.ScopeWrite}, problem.InsufficientScope.Code},
		"admin act needs admin":      {person, domain.RoleAdmin, Need{Role: domain.RoleAdmin, Scope: domain.ScopeAdmin}, problem.InsufficientScope.Code},
		"agent with capability":      {agent, domain.RoleMember, Need{Role: domain.RoleMember, Scope: domain.ScopeWrite, Capability: CapClose}, ""},
		"agent without capability":   {agent, domain.RoleMember, Need{Role: domain.RoleMember, Scope: domain.ScopeWrite, Capability: CapDecide}, problem.AgentForbidden.Code},
		"agent on the hard-off list": {agent, domain.RoleAdmin, Need{Role: domain.RoleMember, Scope: domain.ScopeWrite, HardOff: HardOffBookingTime}, problem.AgentForbidden.Code},
		"person on a hard-off act":   {person, domain.RoleMember, Need{Role: domain.RoleMember, Scope: domain.ScopeWrite, HardOff: HardOffBookingTime}, ""},
		"role before the agent rule": {agent, domain.RoleViewer, Need{Role: domain.RoleMember, Scope: domain.ScopeWrite, HardOff: HardOffBookingTime}, problem.Forbidden.Code},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			err := Authorize(c.p, c.role, c.need)
			if c.code == "" {
				require.Nil(t, err)
				return
			}
			require.NotNil(t, err)
			assert.Equal(t, c.code, err.Code.Code)
		})
	}
	err := Authorize(agent, domain.RoleMember, Need{Capability: CapDecide})
	require.NotNil(t, err)
	assert.Equal(t, "missing capability: decide", err.Detail)
	err = Authorize(agent, domain.RoleMember, Need{HardOff: HardOffBookingTime})
	require.NotNil(t, err)
	assert.Equal(t, "hard-off: booking time", err.Detail)
}
