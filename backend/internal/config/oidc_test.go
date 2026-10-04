package config

import (
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testClientSecret = "a-client-secret-that-must-never-be-echoed"

func oidcEnv(extra map[string]string) map[string]string {
	env := map[string]string{
		EnvDatabaseURL:      dbURL,
		EnvBaseURL:          "https://cowork.example.com",
		EnvOIDCIssuer:       "https://login.example.com/realms/acme/",
		EnvOIDCClientID:     "cowork",
		EnvOIDCClientSecret: testClientSecret,
	}
	maps.Copy(env, extra)
	return env
}

// docs/adr/0029 D4, docs/adr/0030 D1, D5, D8: the defaults of the identity
// provider; the gate admits nobody until a group is named.
func TestLoadOIDCDefaults(t *testing.T) {
	cfg, err := Load(envOf(map[string]string{EnvDatabaseURL: dbURL}))
	require.NoError(t, err)
	assert.Nil(t, cfg.OIDC, "no issuer, no identity provider")
	assert.False(t, cfg.OIDC.Admits())

	cfg, err = Load(envOf(oidcEnv(nil)))
	require.NoError(t, err)
	require.NotNil(t, cfg.OIDC)
	o := cfg.OIDC
	assert.Equal(t, "https://login.example.com/realms/acme/", o.Issuer, "as written: discovery compares it exactly")
	assert.Equal(t, "cowork", o.ClientID)
	assert.Equal(t, testClientSecret, o.ClientSecret)
	assert.Equal(t, []string{"openid", "profile", "email", "groups", "offline_access"}, o.Scopes)
	assert.Equal(t, "groups", o.GroupsClaim)
	assert.Empty(t, o.AllowedGroups)
	assert.Empty(t, o.AdminGroup)
	assert.Equal(t, 15*time.Minute, o.GroupsRefresh)
	assert.Equal(t, 168*time.Hour, o.GroupsMaxAge, "a week (docs/adr/0035 D8)")
	assert.False(t, o.EmailTrusted, "only an address the issuer marked verified matches (docs/adr/0030 D3)")
	assert.Equal(t, "single sign-on", o.DisplayName)
	assert.False(t, o.Admits(), "no allowed group and no administrator group admit nobody")
}

func TestLoadOIDCOverrides(t *testing.T) {
	cfg, err := Load(envOf(oidcEnv(map[string]string{
		EnvOIDCIssuer:        "http://localhost:5556/dex",
		EnvOIDCScopes:        "openid,email  groups",
		EnvOIDCGroupsClaim:   "roles",
		EnvOIDCAllowedGroups: " cowork-users , Domain Users,,cowork-users",
		EnvAdminGroup:        "cowork-admins",
		EnvOIDCGroupsRefresh: "1m",
		EnvOIDCGroupsMaxAge:  "72h",
		EnvOIDCEmailTrusted:  "true",
		EnvOIDCDisplayName:   "Dex",
	})))
	require.NoError(t, err)
	o := cfg.OIDC
	assert.Equal(t, "http://localhost:5556/dex", o.Issuer, "plain HTTP on a loopback host, for development")
	assert.Equal(t, []string{"openid", "email", "groups"}, o.Scopes, "spaces or commas")
	assert.Equal(t, "roles", o.GroupsClaim)
	assert.Equal(t, []string{"cowork-users", "Domain Users"}, o.AllowedGroups, "trimmed, a space inside kept, repetitions dropped")
	assert.Equal(t, "cowork-admins", o.AdminGroup)
	assert.Equal(t, time.Minute, o.GroupsRefresh)
	assert.Equal(t, 72*time.Hour, o.GroupsMaxAge)
	assert.True(t, o.EmailTrusted)
	assert.Equal(t, "Dex", o.DisplayName)
	assert.True(t, o.Admits())

	cfg, err = Load(envOf(oidcEnv(map[string]string{EnvAdminGroup: "admins"})))
	require.NoError(t, err)
	assert.True(t, cfg.OIDC.Admits(), "the administrator group is behind the gate by definition")
}

// Every variable of the identity provider without the issuer is a mistake the
// start names, so a half-done configuration does not pass for none.
func TestLoadOIDCVariablesNeedTheIssuer(t *testing.T) {
	for _, env := range oidcVariables {
		t.Run(env, func(t *testing.T) {
			_, err := Load(envOf(map[string]string{EnvDatabaseURL: dbURL, env: "value"}))
			require.Error(t, err)
			assert.Contains(t, err.Error(), env)
			assert.Contains(t, err.Error(), EnvOIDCIssuer)
		})
	}
}

func TestLoadRejectsBadOIDCValues(t *testing.T) {
	for name, c := range map[string]struct {
		env  map[string]string
		want string
	}{
		"no client id":           {map[string]string{EnvOIDCClientID: ""}, EnvOIDCClientID + " is required"},
		"no client secret":       {map[string]string{EnvOIDCClientSecret: ""}, EnvOIDCClientSecret + " is required"},
		"no base URL":            {map[string]string{EnvBaseURL: ""}, EnvBaseURL + " is required while " + EnvOIDCIssuer},
		"plain HTTP elsewhere":   {map[string]string{EnvOIDCIssuer: "http://login.example.com"}, "must be https://"},
		"not a URL":              {map[string]string{EnvOIDCIssuer: "login.example.com"}, "is not a URL"},
		"a query":                {map[string]string{EnvOIDCIssuer: "https://login.example.com/?a=b"}, "no user, query or fragment"},
		"scopes without openid":  {map[string]string{EnvOIDCScopes: "profile email"}, "must contain openid"},
		"a refresh under 1m":     {map[string]string{EnvOIDCGroupsRefresh: "30s"}, "at least 1m0s"},
		"a refresh that is none": {map[string]string{EnvOIDCGroupsRefresh: "soon"}, "not a duration"},
		"a maximum age that is none": {map[string]string{EnvOIDCGroupsMaxAge: "a week"},
			EnvOIDCGroupsMaxAge + `: "a week" is not a duration`},
		"a maximum age no longer than the refresh": {map[string]string{EnvOIDCGroupsRefresh: "1h", EnvOIDCGroupsMaxAge: "1h"},
			EnvOIDCGroupsMaxAge + " must be longer than " + EnvOIDCGroupsRefresh},
		"a refresh beyond the default maximum age": {map[string]string{EnvOIDCGroupsRefresh: "200h"},
			EnvOIDCGroupsMaxAge + " must be longer than " + EnvOIDCGroupsRefresh + ": 168h0m0s is not longer than 200h0m0s"},
		"an e-mail trust that is no boolean": {map[string]string{EnvOIDCEmailTrusted: "verified"},
			EnvOIDCEmailTrusted + `: "verified" is not a boolean`},
		"a long display name":     {map[string]string{EnvOIDCDisplayName: strings.Repeat("x", 65)}, "at most 64"},
		"a long allowed group":    {map[string]string{EnvOIDCAllowedGroups: strings.Repeat("g", 257)}, "at most 256"},
		"a long admin group name": {map[string]string{EnvAdminGroup: strings.Repeat("g", 257)}, "at most 256"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Load(envOf(oidcEnv(c.env)))
			require.Error(t, err)
			assert.Contains(t, err.Error(), c.want)
			assert.NotContains(t, err.Error(), testClientSecret, "the secret is never echoed")
		})
	}
}

// docs/adr/0032 D6 (amended): the bootstrap tenant needs somebody to administer
// it — the local administrator or the administrator group.
func TestLoadBootstrapTenantWithTheAdministratorGroup(t *testing.T) {
	tenant := map[string]string{EnvBootstrapTenantSlug: "acme", EnvBootstrapTenantName: "Acme"}
	cfg, err := Load(envOf(oidcEnv(map[string]string{EnvAdminGroup: "cowork-admins",
		EnvBootstrapTenantSlug: "acme", EnvBootstrapTenantName: "Acme"})))
	require.NoError(t, err)
	assert.Equal(t, "acme", cfg.BootstrapTenantSlug)
	assert.Empty(t, cfg.LocalAdminUsername)

	_, err = Load(envOf(oidcEnv(tenant)))
	require.Error(t, err, "a provider without an administrator group administers nothing")
	assert.Contains(t, err.Error(), EnvAdminGroup)
}
