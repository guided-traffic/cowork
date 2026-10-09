package api

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/problem"
)

// docs/adr/0035 D5 as amended 2026-10-07: a change of the tenant's settings
// that widens what the members may see or do — their sight of everyone's
// time, their creation of projects, closed days opened again by an earlier or
// lifted time lock — takes a browser session; the other direction and the name
// stay open to a token.
func TestWideningTheTenantSettingsTakesASession(t *testing.T) {
	day := func(s string) *time.Time {
		d, err := time.Parse(time.DateOnly, s)
		require.NoError(t, err)
		return &d
	}
	closed := tenantSettings{Name: "Acme", TimeLockedUntil: day("2026-09-30")}
	for name, c := range map[string]struct {
		before, after tenantSettings
		widened       []string
	}{
		"nothing changes":          {closed, closed, nil},
		"a new name":               {closed, tenantSettings{Name: "Acme Corp", TimeLockedUntil: day("2026-09-30")}, nil},
		"time shown to members":    {closed, tenantSettings{Name: "Acme", TimeVisibleToMembers: true, TimeLockedUntil: day("2026-09-30")}, []string{"time_visible_to_members"}},
		"time hidden from members": {tenantSettings{TimeVisibleToMembers: true}, tenantSettings{}, nil},
		"members create projects":  {tenantSettings{}, tenantSettings{MembersCreateProjects: true}, []string{"members_create_projects"}},
		"members create none":      {tenantSettings{MembersCreateProjects: true}, tenantSettings{}, nil},
		"the lock moved earlier":   {closed, tenantSettings{Name: "Acme", TimeLockedUntil: day("2026-09-01")}, []string{"time_locked_until"}},
		"the lock lifted":          {closed, tenantSettings{Name: "Acme"}, []string{"time_locked_until"}},
		"the lock moved later":     {closed, tenantSettings{Name: "Acme", TimeLockedUntil: day("2026-10-31")}, nil},
		"a first lock":             {tenantSettings{}, tenantSettings{TimeLockedUntil: day("2026-09-30")}, nil},
		"the lock where it was":    {closed, tenantSettings{Name: "Acme", TimeLockedUntil: day("2026-09-30")}, nil},
		"everything widened at once": {tenantSettings{TimeLockedUntil: day("2026-09-30")}, tenantSettings{TimeVisibleToMembers: true, MembersCreateProjects: true},
			[]string{"time_visible_to_members", "members_create_projects", "time_locked_until"}},
	} {
		assert.Equal(t, c.widened, c.before.gives(c.after), name)
	}

	token := auth.Principal{Scope: domain.ScopeAdmin}
	session := auth.Principal{Session: true, Scope: domain.ScopeAdmin}
	perr := sessionToGive(token, []string{"time_visible_to_members", "time_locked_until"})
	require.NotNil(t, perr)
	assert.Equal(t, problem.SessionRequired, perr.Code)
	assert.Contains(t, perr.Detail, "time_visible_to_members, time_locked_until")
	assert.Nil(t, sessionToGive(token, nil), "a token narrows")
	assert.Nil(t, sessionToGive(session, []string{"members_create_projects"}), "a session widens")
}
