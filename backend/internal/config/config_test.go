package config

import (
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func envOf(values map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		v, ok := values[key]
		return v, ok
	}
}

const dbURL = "postgres://cowork:secret@db:5432/cowork?sslmode=require"

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(envOf(map[string]string{EnvDatabaseURL: dbURL}))
	require.NoError(t, err)

	assert.Equal(t, DefaultListenAddr, cfg.ListenAddr)
	assert.Equal(t, ":8081", cfg.MetricsAddr, "the metrics listener is on by default (docs/adr/0060 D1)")
	assert.Equal(t, dbURL, cfg.DatabaseURL)
	assert.True(t, cfg.MigrateOnStart)
	assert.Equal(t, slog.LevelInfo, cfg.LogLevel)
	assert.Equal(t, LogFormatJSON, cfg.LogFormat)
	assert.Equal(t, DefaultShutdownTimeout, cfg.ShutdownTimeout)
	assert.Empty(t, cfg.BaseURL)
}

func TestLoadOverrides(t *testing.T) {
	cfg, err := Load(envOf(map[string]string{
		EnvDatabaseURL:     dbURL,
		EnvListenAddr:      "127.0.0.1:9090",
		EnvMigrateOnStart:  "false",
		EnvLogLevel:        "DEBUG",
		EnvLogFormat:       "Text",
		EnvShutdownTimeout: "1m",
		EnvBaseURL:         "https://cowork.example.com/",
	}))
	require.NoError(t, err)

	assert.Equal(t, "127.0.0.1:9090", cfg.ListenAddr)
	assert.False(t, cfg.MigrateOnStart)
	assert.Equal(t, slog.LevelDebug, cfg.LogLevel)
	assert.Equal(t, LogFormatText, cfg.LogFormat)
	assert.Equal(t, time.Minute, cfg.ShutdownTimeout)
	assert.Equal(t, "https://cowork.example.com", cfg.BaseURL, "trailing slash is stripped")
}

func TestLoadRequiresDatabaseURL(t *testing.T) {
	for name, env := range map[string]map[string]string{
		"unset":      {},
		"empty":      {EnvDatabaseURL: ""},
		"whitespace": {EnvDatabaseURL: "   "},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Load(envOf(env))
			require.Error(t, err)
			assert.Contains(t, err.Error(), missingRuntimeConnection)
		})
	}
}

// missingRuntimeConnection is the error of a runtime role without a URL and
// without components (docs/adr/0058 D4).
const missingRuntimeConnection = EnvDatabaseURL + ", or its components " + EnvDatabaseHost + ", " + EnvDatabaseName + ", " +
	EnvDatabaseUser + " and " + EnvDatabasePassword + ", is required"

func TestLoadEmptyValueKeepsDefault(t *testing.T) {
	cfg, err := Load(envOf(map[string]string{
		EnvDatabaseURL:    dbURL,
		EnvListenAddr:     "",
		EnvMigrateOnStart: "",
		EnvLogLevel:       "",
	}))
	require.NoError(t, err)
	assert.Equal(t, DefaultListenAddr, cfg.ListenAddr)
	assert.True(t, cfg.MigrateOnStart)
	assert.Equal(t, slog.LevelInfo, cfg.LogLevel)
}

// docs/adr/0060 D1: COWORK_METRICS_ADDR moves the metrics listener; set to an
// empty value — unlike every other variable, where empty is unset — it
// switches the listener off; it is host:port and never the API's address.
func TestLoadMetricsAddr(t *testing.T) {
	for name, c := range map[string]struct {
		value, listen, want string
	}{
		"another port":       {value: "0.0.0.0:9100", want: "0.0.0.0:9100"},
		"empty: off":         {value: "", want: ""},
		"whitespace: off":    {value: "  ", want: ""},
		"trimmed":            {value: " :9100 ", want: ":9100"},
		"beside a moved API": {value: ":8080", listen: ":9090", want: ":8080"},
	} {
		t.Run(name, func(t *testing.T) {
			env := map[string]string{EnvDatabaseURL: dbURL, EnvMetricsAddr: c.value}
			if c.listen != "" {
				env[EnvListenAddr] = c.listen
			}
			cfg, err := Load(envOf(env))
			require.NoError(t, err)
			assert.Equal(t, c.want, cfg.MetricsAddr)
		})
	}
	for name, c := range map[string]struct {
		env    map[string]string
		wanted string
	}{
		"no port":       {map[string]string{EnvMetricsAddr: "8081"}, EnvMetricsAddr + `: "8081" is not an address such as :8081`},
		"empty port":    {map[string]string{EnvMetricsAddr: "localhost:"}, EnvMetricsAddr + `: "localhost:" is not an address`},
		"the API's":     {map[string]string{EnvMetricsAddr: ":8080"}, EnvMetricsAddr + " must differ from " + EnvListenAddr},
		"a moved API's": {map[string]string{EnvMetricsAddr: "127.0.0.1:9000", EnvListenAddr: "127.0.0.1:9000"}, EnvMetricsAddr + " must differ from " + EnvListenAddr},
	} {
		t.Run(name, func(t *testing.T) {
			env := map[string]string{EnvDatabaseURL: dbURL}
			for k, v := range c.env {
				env[k] = v
			}
			_, err := Load(envOf(env))
			require.Error(t, err)
			assert.Contains(t, err.Error(), c.wanted)
		})
	}
}

func TestLoadReportsEveryInvalidValue(t *testing.T) {
	_, err := Load(envOf(map[string]string{
		EnvMigrateOnStart:  "maybe",
		EnvLogLevel:        "loud",
		EnvLogFormat:       "xml",
		EnvShutdownTimeout: "soon",
	}))
	require.Error(t, err)
	msg := err.Error()
	assert.Contains(t, msg, missingRuntimeConnection)
	assert.Contains(t, msg, EnvMigrateOnStart+`: "maybe" is not a boolean`)
	assert.Contains(t, msg, EnvLogLevel+`: "loud" is not one of`)
	assert.Contains(t, msg, EnvLogFormat+`: "xml" is not one of`)
	assert.Contains(t, msg, EnvShutdownTimeout+`: "soon" is not a duration`)
}

func TestLoadRejectsNonPositiveShutdownTimeout(t *testing.T) {
	_, err := Load(envOf(map[string]string{EnvDatabaseURL: dbURL, EnvShutdownTimeout: "0s"}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be positive")
}

func TestLoadLimitsDefaultsAndOverrides(t *testing.T) {
	cfg, err := Load(envOf(map[string]string{EnvDatabaseURL: dbURL}))
	require.NoError(t, err)
	assert.EqualValues(t, 1<<20, cfg.MaxJSONBody)
	assert.Equal(t, 30*time.Second, cfg.RequestTimeout)
	assert.Equal(t, 200, cfg.MaxPageSize)
	assert.Equal(t, 256, cfg.MaxQueryLength)
	assert.EqualValues(t, 50<<20, cfg.MaxImportBytes, "docs/adr/0051 D7")
	assert.Nil(t, cfg.SessionKey)

	cfg, err = Load(envOf(map[string]string{
		EnvDatabaseURL:    dbURL,
		EnvMaxJSONBody:    "512KiB",
		EnvRequestTimeout: "0",
		EnvMaxPageSize:    "0",
		EnvMaxQueryLength: "100",
		EnvMaxImportBytes: "0",
		EnvSessionKey:     "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=",
	}))
	require.NoError(t, err)
	assert.EqualValues(t, 512<<10, cfg.MaxJSONBody)
	assert.Zero(t, cfg.RequestTimeout, "0 disables the timeout")
	assert.Zero(t, cfg.MaxPageSize)
	assert.Equal(t, 100, cfg.MaxQueryLength)
	assert.Zero(t, cfg.MaxImportBytes, "0 disables the bound of an import")
	assert.Len(t, cfg.SessionKey, 32)

	cfg, err = Load(envOf(map[string]string{EnvDatabaseURL: dbURL, EnvMaxImportBytes: "200MiB"}))
	require.NoError(t, err)
	assert.EqualValues(t, 200<<20, cfg.MaxImportBytes)
}

func TestLoadRejectsBadLimitsAndNeverEchoesTheKey(t *testing.T) {
	_, err := Load(envOf(map[string]string{
		EnvDatabaseURL:    dbURL,
		EnvMaxJSONBody:    "lots",
		EnvRequestTimeout: "-1s",
		EnvMaxPageSize:    "-3",
		EnvMaxQueryLength: "x",
		EnvMaxImportBytes: "plenty",
		EnvSessionKey:     "dG9vLXNob3J0",
	}))
	require.Error(t, err)
	msg := err.Error()
	for _, want := range []string{EnvMaxJSONBody, EnvRequestTimeout, EnvMaxPageSize, EnvMaxQueryLength,
		EnvMaxImportBytes + `: "plenty" is not a size such as 50MiB or 0`, EnvSessionKey + " must decode to at least 32 bytes"} {
		assert.Contains(t, msg, want)
	}
	assert.NotContains(t, msg, "dG9vLXNob3J0", "the key is a secret and never echoed")

	_, err = Load(envOf(map[string]string{EnvDatabaseURL: dbURL, EnvSessionKey: "not base64!"}))
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "not base64!")
}

func TestParseSize(t *testing.T) {
	for in, want := range map[string]int64{"0": 0, "1024": 1024, "1KiB": 1024, "1MiB": 1 << 20, "2 GiB": 2 << 30, "10B": 10} {
		got, err := parseSize(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}
	for _, in := range []string{"", "MiB", "1MB", "-1", "1.5MiB"} {
		_, err := parseSize(in)
		assert.Error(t, err, in)
	}
}

// docs/adr/0016 D1: object storage is all of endpoint, bucket and both keys
// or none; the secret never appears in an error.
func TestStorageIsAllOrNone(t *testing.T) {
	base := map[string]string{EnvDatabaseURL: "postgres://x"}
	with := func(extra map[string]string) (Config, error) {
		env := map[string]string{}
		for k, v := range base {
			env[k] = v
		}
		for k, v := range extra {
			env[k] = v
		}
		return Load(func(k string) (string, bool) { v, ok := env[k]; return v, ok })
	}
	cfg, err := with(nil)
	require.NoError(t, err)
	assert.Nil(t, cfg.Storage, "none configured: uploads are refused")
	assert.EqualValues(t, 10<<20, cfg.AttachmentMaxBytes)
	assert.Equal(t, 100, cfg.AttachmentMaxPerTicket)

	_, err = with(map[string]string{EnvS3Endpoint: "http://minio:9000", EnvS3SecretAccessKey: "s3cr3t-value"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), EnvS3Bucket)
	assert.NotContains(t, err.Error(), "s3cr3t-value")

	cfg, err = with(map[string]string{EnvS3Endpoint: "http://minio:9000", EnvS3Bucket: "cowork", EnvS3AccessKeyID: "id",
		EnvS3SecretAccessKey: "secret", EnvS3UsePathStyle: "false", EnvAttachmentMaxBytes: "2MiB", EnvAttachmentMaxPerTicket: "0"})
	require.NoError(t, err)
	require.NotNil(t, cfg.Storage)
	assert.False(t, cfg.Storage.PathStyle)
	assert.EqualValues(t, 2<<20, cfg.AttachmentMaxBytes)
	assert.Equal(t, 0, cfg.AttachmentMaxPerTicket)

	cfg, err = with(map[string]string{EnvAttachmentMaxBytes: "0"})
	require.NoError(t, err, "0 disables the limit (docs/adr/0039 D2)")
	assert.Zero(t, cfg.AttachmentMaxBytes)

	// docs/adr/0016 D6 as amended 2026-10-05: no tenant quota unless one is set.
	assert.Zero(t, cfg.AttachmentTenantQuota, "no quota by default")
	cfg, err = with(map[string]string{EnvAttachmentTenantQuota: "2GiB"})
	require.NoError(t, err)
	assert.EqualValues(t, 2<<30, cfg.AttachmentTenantQuota)
	cfg, err = with(map[string]string{EnvAttachmentTenantQuota: "0"})
	require.NoError(t, err)
	assert.Zero(t, cfg.AttachmentTenantQuota)
	_, err = with(map[string]string{EnvAttachmentTenantQuota: "a lot"})
	assert.ErrorContains(t, err, EnvAttachmentTenantQuota)
	_, err = with(map[string]string{EnvAttachmentTenantQuota: "-1"})
	assert.ErrorContains(t, err, EnvAttachmentTenantQuota)

	_, err = with(map[string]string{EnvS3Endpoint: "minio:9000", EnvS3Bucket: "b", EnvS3AccessKeyID: "i", EnvS3SecretAccessKey: "s"})
	assert.ErrorContains(t, err, "not an http:// or https:// URL")
	_, err = with(map[string]string{EnvS3Endpoint: "ftp://key:s3cr3t@minio", EnvS3Bucket: "b", EnvS3AccessKeyID: "i", EnvS3SecretAccessKey: "s"})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "s3cr3t", "an endpoint may carry credentials; it is never echoed")
}

const (
	adminPassword = "a long enough password"
	baseURL       = "https://cowork.example.com"
)

// loginEnv is a configuration with the local administrator and what it needs.
func loginEnv(extra map[string]string) func(string) (string, bool) {
	values := map[string]string{
		EnvDatabaseURL: dbURL, EnvLocalAdminUsername: "ada", EnvLocalAdminPassword: adminPassword, EnvBaseURL: baseURL,
	}
	for k, v := range extra {
		values[k] = v
	}
	return envOf(values)
}

func TestLoadLoginDefaults(t *testing.T) {
	cfg, err := Load(envOf(map[string]string{EnvDatabaseURL: dbURL}))
	require.NoError(t, err)
	assert.Equal(t, 12*time.Hour, cfg.SessionLifetime)
	assert.Equal(t, 2*time.Hour, cfg.SessionIdle)
	assert.Equal(t, 12, cfg.PasswordMinLength)
	assert.Equal(t, LockoutWindow, cfg.LoginLockout)
	assert.Equal(t, 5, cfg.LoginMaxFailures)
	assert.Equal(t, 20, cfg.LoginAddressLimit)
	assert.Equal(t, 90*24*time.Hour, cfg.TokenDefaultLifetime)
	assert.Equal(t, 365*24*time.Hour, cfg.TokenMaxLifetime)
	assert.Empty(t, cfg.LocalAdminUsername, "no local administrator unless configured")
	assert.Empty(t, cfg.BootstrapTenantSlug)
	assert.Empty(t, cfg.BaseOrigin)
}

func TestLoadLoginOverrides(t *testing.T) {
	cfg, err := Load(loginEnv(map[string]string{
		EnvSessionLifetime: "8h", EnvSessionIdle: "30m", EnvPasswordMinLength: "16", EnvLoginLockout: "Admin",
		EnvLoginMaxFailures: "3", EnvLoginAddressLimit: "0", EnvTokenDefaultLifetime: "720h", EnvTokenMaxLifetime: "2160h",
		EnvBootstrapTenantSlug: "acme", EnvBootstrapTenantName: "Acme Corp", EnvBaseURL: "HTTPS://Cowork.Example.com:443/",
		EnvLocalAdminPassword: "sixteen characters+",
	}))
	require.NoError(t, err)
	assert.Equal(t, 8*time.Hour, cfg.SessionLifetime)
	assert.Equal(t, 30*time.Minute, cfg.SessionIdle)
	assert.Equal(t, 16, cfg.PasswordMinLength)
	assert.Equal(t, LockoutAdmin, cfg.LoginLockout)
	assert.Equal(t, 3, cfg.LoginMaxFailures)
	assert.Zero(t, cfg.LoginAddressLimit, "0 switches the throttle off (docs/adr/0039 D6)")
	assert.Equal(t, 720*time.Hour, cfg.TokenDefaultLifetime)
	assert.Equal(t, "ada", cfg.LocalAdminUsername)
	assert.Equal(t, "sixteen characters+", cfg.LocalAdminPassword)
	assert.Equal(t, "acme", cfg.BootstrapTenantSlug)
	assert.Equal(t, "Acme Corp", cfg.BootstrapTenantName)
	assert.Equal(t, "https://cowork.example.com", cfg.BaseOrigin, "the origin as a browser writes it")

	// A password shorter than the configured minimum refuses the start.
	_, err = Load(loginEnv(map[string]string{EnvPasswordMinLength: "30"}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), EnvLocalAdminPassword+" must be at least 30 characters")
}

// docs/adr/0032 D2: both variables or neither; one alone refuses the start and
// names the variable that is missing, never a value.
func TestLoadLocalAdminIsAllOrNone(t *testing.T) {
	_, err := Load(envOf(map[string]string{EnvDatabaseURL: dbURL, EnvLocalAdminUsername: "ada", EnvBaseURL: baseURL}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), EnvLocalAdminPassword+" is required while "+EnvLocalAdminUsername+" is set")

	_, err = Load(envOf(map[string]string{EnvDatabaseURL: dbURL, EnvLocalAdminPassword: adminPassword, EnvBaseURL: baseURL}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), EnvLocalAdminUsername+" is required while "+EnvLocalAdminPassword+" is set")
	assert.NotContains(t, err.Error(), adminPassword)

	for name, env := range map[string]map[string]string{
		"both empty": {EnvLocalAdminUsername: "", EnvLocalAdminPassword: ""},
		"unset":      {},
	} {
		t.Run(name, func(t *testing.T) {
			env[EnvDatabaseURL] = dbURL
			cfg, err := Load(envOf(env))
			require.NoError(t, err, "both empty is the deactivated state, not an error")
			assert.Empty(t, cfg.LocalAdminUsername)
		})
	}

	_, err = Load(envOf(map[string]string{EnvDatabaseURL: dbURL, EnvLocalAdminUsername: "ada", EnvLocalAdminPassword: "", EnvBaseURL: baseURL}))
	require.Error(t, err, "one set and one empty is refused")
}

func TestLoadLocalAdminRules(t *testing.T) {
	// The password is length only (docs/adr/0033 D3), and never echoed.
	_, err := Load(loginEnv(map[string]string{EnvLocalAdminPassword: "too short"}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), EnvLocalAdminPassword+" must be at least 12 characters")
	assert.NotContains(t, err.Error(), "too short")

	_, err = Load(loginEnv(map[string]string{EnvLocalAdminPassword: "12345678", EnvPasswordMinLength: "8"}))
	require.NoError(t, err, "the floor is 8")

	// Spaces are part of a password and are not trimmed away.
	cfg, err := Load(loginEnv(map[string]string{EnvLocalAdminPassword: "  spaces around it  "}))
	require.NoError(t, err)
	assert.Equal(t, "  spaces around it  ", cfg.LocalAdminPassword)

	_, err = Load(loginEnv(map[string]string{EnvLocalAdminUsername: "Ada Lovelace"}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), EnvLocalAdminUsername+" must be 1 to 63 characters")
}

// docs/adr/0033 D3: a configured minimum below eight refuses the start.
func TestLoadPasswordMinLength(t *testing.T) {
	for _, bad := range []string{"7", "0", "-1", "twelve", "1025"} {
		_, err := Load(envOf(map[string]string{EnvDatabaseURL: dbURL, EnvPasswordMinLength: bad}))
		require.Error(t, err, bad)
		assert.Contains(t, err.Error(), EnvPasswordMinLength, bad)
	}
	cfg, err := Load(envOf(map[string]string{EnvDatabaseURL: dbURL, EnvPasswordMinLength: "8"}))
	require.NoError(t, err)
	assert.Equal(t, 8, cfg.PasswordMinLength)
}

func TestLoadRejectsBadLoginValues(t *testing.T) {
	_, err := Load(envOf(map[string]string{
		EnvDatabaseURL: dbURL, EnvLoginLockout: "forever", EnvLoginMaxFailures: "-1", EnvLoginAddressLimit: "x",
		EnvSessionLifetime: "0s", EnvSessionIdle: "soon", EnvTokenDefaultLifetime: "-1h", EnvTokenMaxLifetime: "x",
	}))
	require.Error(t, err)
	msg := err.Error()
	for _, want := range []string{EnvLoginLockout, EnvLoginMaxFailures, EnvLoginAddressLimit, EnvSessionLifetime, EnvSessionIdle,
		EnvTokenDefaultLifetime, EnvTokenMaxLifetime} {
		assert.Contains(t, msg, want)
	}

	_, err = Load(envOf(map[string]string{EnvDatabaseURL: dbURL, EnvTokenDefaultLifetime: "17520h"}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), EnvTokenDefaultLifetime+" must not exceed "+EnvTokenMaxLifetime, "a default beyond the maximum")
}

// docs/adr/0037 D6: COWORK_BASE_URL is required while a cookie login exists, and
// it is the origin a browser sends: an error names the variable, never the URL.
func TestLoadBaseURL(t *testing.T) {
	_, err := Load(envOf(map[string]string{EnvDatabaseURL: dbURL, EnvLocalAdminUsername: "ada", EnvLocalAdminPassword: adminPassword}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), EnvBaseURL+" is required while "+EnvLocalAdminUsername+" is set")

	for in, want := range map[string]string{
		"https://cowork.example.com":      "https://cowork.example.com",
		"https://cowork.example.com/":     "https://cowork.example.com",
		"HTTPS://COWORK.example.com":      "https://cowork.example.com",
		"https://cowork.example.com:443":  "https://cowork.example.com",
		"http://cowork.example.com:80":    "http://cowork.example.com",
		"http://localhost:4200":           "http://localhost:4200",
		"https://cowork.example.com:8443": "https://cowork.example.com:8443",
		"http://[::1]:8080":               "http://[::1]:8080",
	} {
		origin, err := Origin(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, origin, in)
	}
	for _, bad := range []string{"cowork.example.com", "ftp://cowork.example.com", "https://", "https://user@cowork.example.com",
		"https://cowork.example.com/app", "https://cowork.example.com?x=1", "https://cowork.example.com#x", "://"} {
		_, err := Load(envOf(map[string]string{EnvDatabaseURL: dbURL, EnvBaseURL: bad}))
		require.Error(t, err, bad)
		assert.Contains(t, err.Error(), EnvBaseURL, bad)
		assert.NotContains(t, err.Error(), bad, "the error never echoes the URL")
	}
}

// docs/adr/0032 D6, D7: the bootstrap tenant needs both variables and the
// administrator who becomes its first administrator.
func TestLoadBootstrapTenant(t *testing.T) {
	cfg, err := Load(loginEnv(map[string]string{EnvBootstrapTenantSlug: "acme", EnvBootstrapTenantName: "Acme"}))
	require.NoError(t, err)
	assert.Equal(t, "acme", cfg.BootstrapTenantSlug)

	_, err = Load(loginEnv(map[string]string{EnvBootstrapTenantSlug: "acme"}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), EnvBootstrapTenantName+" is required while "+EnvBootstrapTenantSlug+" is set")
	_, err = Load(loginEnv(map[string]string{EnvBootstrapTenantName: "Acme"}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), EnvBootstrapTenantSlug+" is required while "+EnvBootstrapTenantName+" is set")

	for _, slug := range []string{"A", "Acme", "-acme", "acme corp", "a"} {
		_, err = Load(loginEnv(map[string]string{EnvBootstrapTenantSlug: slug, EnvBootstrapTenantName: "Acme"}))
		require.Error(t, err, slug)
		assert.Contains(t, err.Error(), EnvBootstrapTenantSlug, slug)
	}

	_, err = Load(envOf(map[string]string{EnvDatabaseURL: dbURL, EnvBootstrapTenantSlug: "acme", EnvBootstrapTenantName: "Acme"}))
	require.Error(t, err, "a tenant without an administrator cannot come to exist")
	assert.Contains(t, err.Error(), EnvLocalAdminUsername)
}

// docs/adr/0035 D2: COWORK_TRUSTED_PROXIES is a list of CIDRs, IPv4 and IPv6,
// empty by default — and then no header is ever read.
func TestLoadTrustedProxies(t *testing.T) {
	cfg, err := Load(envOf(map[string]string{EnvDatabaseURL: dbURL}))
	require.NoError(t, err)
	assert.Empty(t, cfg.TrustedProxies, "default: none")

	for name, tc := range map[string]struct {
		value string
		want  []string
	}{
		"empty value keeps the default": {"", nil},
		"only blanks":                   {" , ,", nil},
		"one IPv4 network":              {"10.0.0.0/8", []string{"10.0.0.0/8"}},
		"one IPv6 network":              {"fd00::/8", []string{"fd00::/8"}},
		"both families, spaced":         {" 10.244.0.0/16 , fd00:10::/48 ", []string{"10.244.0.0/16", "fd00:10::/48"}},
		"single hosts":                  {"192.0.2.7/32,2001:db8::7/128", []string{"192.0.2.7/32", "2001:db8::7/128"}},
		"host bits are masked":          {"10.1.2.3/16", []string{"10.1.0.0/16"}},
		"a trailing comma":              {"10.0.0.0/8,", []string{"10.0.0.0/8"}},
		"the whole of a family":         {"0.0.0.0/0,::/0", []string{"0.0.0.0/0", "::/0"}},
	} {
		t.Run(name, func(t *testing.T) {
			cfg, err := Load(envOf(map[string]string{EnvDatabaseURL: dbURL, EnvTrustedProxies: tc.value}))
			require.NoError(t, err)
			got := make([]string, 0, len(cfg.TrustedProxies))
			for _, p := range cfg.TrustedProxies {
				got = append(got, p.String())
			}
			if tc.want == nil {
				assert.Empty(t, got)
				return
			}
			assert.Equal(t, tc.want, got)
		})
	}
}

// The error names the variable and quotes the offending entry — every one of
// them — and nothing else of the value.
func TestLoadRejectsBadTrustedProxies(t *testing.T) {
	for name, bad := range map[string]string{
		"a bare address":       "192.0.2.7",
		"a bad length":         "10.0.0.0/33",
		"an IPv6 bad length":   "fd00::/129",
		"a name":               "proxy.example.com",
		"a zone":               "fe80::1%eth0/64",
		"a URL":                "http://10.0.0.1/8",
		"a negative length":    "10.0.0.0/-1",
		"two networks at once": "10.0.0.0/8 192.168.0.0/16",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Load(envOf(map[string]string{EnvDatabaseURL: dbURL, EnvTrustedProxies: "172.16.0.0/12," + bad + ",2001:db8:abcd::/48"}))
			require.Error(t, err)
			assert.Contains(t, err.Error(), EnvTrustedProxies)
			assert.Contains(t, err.Error(), bad, "the offending entry is quoted")
			assert.NotContains(t, err.Error(), "172.16.0.0/12", "no other entry is echoed")
			assert.NotContains(t, err.Error(), "2001:db8:abcd", "no other entry is echoed")
		})
	}

	_, err := Load(envOf(map[string]string{EnvDatabaseURL: dbURL, EnvTrustedProxies: "one,10.0.0.0/8,two"}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"one"`)
	assert.Contains(t, err.Error(), `"two"`, "every offending entry is reported at once")

	long := strings.Repeat("x", 500)
	_, err = Load(envOf(map[string]string{EnvDatabaseURL: dbURL, EnvTrustedProxies: long}))
	require.Error(t, err)
	assert.Less(t, len(err.Error()), 300, "a long entry is clipped")
}
