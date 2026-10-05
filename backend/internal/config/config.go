// Package config loads the server configuration from the environment.
//
// Every setting is an environment variable with the COWORK_ prefix. The
// user-facing reference with the defaults is the configuration table in
// README.md; this package is what that table is checked against.
package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/guided-traffic/cowork/backend/internal/auth"
)

// The environment variables the server reads.
const (
	EnvListenAddr       = "COWORK_LISTEN_ADDR"
	EnvDatabaseURL      = "COWORK_DATABASE_URL"
	EnvDatabaseOwnerURL = "COWORK_DATABASE_OWNER_URL"
	EnvMigrateOnStart   = "COWORK_MIGRATE_ON_START"
	EnvLogLevel         = "COWORK_LOG_LEVEL"
	EnvLogFormat        = "COWORK_LOG_FORMAT"
	EnvShutdownTimeout  = "COWORK_SHUTDOWN_TIMEOUT"
	EnvBaseURL          = "COWORK_BASE_URL"
	EnvSessionKey       = "COWORK_SESSION_KEY"
	EnvMaxJSONBody      = "COWORK_MAX_JSON_BODY"
	EnvRequestTimeout   = "COWORK_REQUEST_TIMEOUT"
	EnvMaxPageSize      = "COWORK_MAX_PAGE_SIZE"
	EnvMaxQueryLength   = "COWORK_MAX_QUERY_LENGTH"

	EnvS3Endpoint             = "COWORK_S3_ENDPOINT"
	EnvS3Bucket               = "COWORK_S3_BUCKET"
	EnvS3Region               = "COWORK_S3_REGION"
	EnvS3AccessKeyID          = "COWORK_S3_ACCESS_KEY_ID"
	EnvS3SecretAccessKey      = "COWORK_S3_SECRET_ACCESS_KEY" // #nosec G101 -- the variable's name, not a credential
	EnvS3UsePathStyle         = "COWORK_S3_USE_PATH_STYLE"
	EnvS3CA                   = "COWORK_S3_CA"
	EnvAttachmentMaxBytes     = "COWORK_ATTACHMENT_MAX_BYTES"
	EnvAttachmentMaxPerTicket = "COWORK_ATTACHMENT_MAX_PER_TICKET"
	EnvAttachmentTenantQuota  = "COWORK_ATTACHMENT_TENANT_QUOTA"

	EnvSSEReplayWindow        = "COWORK_SSE_REPLAY_WINDOW"
	EnvSSEMaxStreamsPerPerson = "COWORK_SSE_MAX_STREAMS_PER_PERSON"

	EnvLocalAdminUsername   = "COWORK_LOCAL_ADMIN_USERNAME"
	EnvLocalAdminPassword   = "COWORK_LOCAL_ADMIN_PASSWORD" // #nosec G101 -- the variable's name, not a credential
	EnvBootstrapTenantSlug  = "COWORK_BOOTSTRAP_TENANT_SLUG"
	EnvBootstrapTenantName  = "COWORK_BOOTSTRAP_TENANT_NAME"
	EnvPasswordMinLength    = "COWORK_PASSWORD_MIN_LENGTH"
	EnvLoginLockout         = "COWORK_LOGIN_LOCKOUT"
	EnvLoginMaxFailures     = "COWORK_LOGIN_MAX_FAILURES"
	EnvLoginAddressLimit    = "COWORK_LOGIN_ADDRESS_LIMIT"
	EnvSessionLifetime      = "COWORK_SESSION_LIFETIME"
	EnvSessionIdle          = "COWORK_SESSION_IDLE"
	EnvTokenDefaultLifetime = "COWORK_TOKEN_DEFAULT_LIFETIME" // #nosec G101 -- the variable's name, not a credential
	EnvTokenMaxLifetime     = "COWORK_TOKEN_MAX_LIFETIME"     // #nosec G101 -- the variable's name, not a credential
	EnvTrustedProxies       = "COWORK_TRUSTED_PROXIES"

	EnvOIDCIssuer        = "COWORK_OIDC_ISSUER"
	EnvOIDCClientID      = "COWORK_OIDC_CLIENT_ID"
	EnvOIDCClientSecret  = "COWORK_OIDC_CLIENT_SECRET" // #nosec G101 -- the variable's name, not a credential
	EnvOIDCScopes        = "COWORK_OIDC_SCOPES"
	EnvOIDCGroupsClaim   = "COWORK_OIDC_GROUPS_CLAIM"
	EnvOIDCAllowedGroups = "COWORK_OIDC_ALLOWED_GROUPS"
	EnvAdminGroup        = "COWORK_ADMIN_GROUP"
	EnvOIDCGroupsRefresh = "COWORK_OIDC_GROUPS_REFRESH"
	EnvOIDCGroupsMaxAge  = "COWORK_OIDC_GROUPS_MAX_AGE"
	EnvOIDCEmailTrusted  = "COWORK_OIDC_EMAIL_TRUSTED"
	EnvOIDCDisplayName   = "COWORK_OIDC_DISPLAY_NAME"
)

// Defaults and the accepted log formats.
const (
	DefaultListenAddr      = ":8080"
	DefaultShutdownTimeout = 15 * time.Second
	DefaultMaxJSONBody     = 1 << 20 // 1MiB
	DefaultRequestTimeout  = 30 * time.Second
	DefaultMaxPageSize     = 200
	DefaultMaxQueryLength  = 256
	// DefaultAttachmentMaxBytes is the per-file maximum (docs/adr/0016 D6).
	DefaultAttachmentMaxBytes = 10 << 20 // 10MiB
	// DefaultAttachmentMaxPerTicket is the per-ticket count (docs/adr/0016 D6).
	DefaultAttachmentMaxPerTicket = 100
	// DefaultSSEReplayWindow and DefaultSSEMaxStreamsPerPerson are the event
	// stream's limits (docs/adr/0054 D5, D8).
	DefaultSSEReplayWindow        = 5 * time.Minute
	DefaultSSEMaxStreamsPerPerson = 10
	// MinSessionKeyBytes is the shortest server key accepted.
	MinSessionKeyBytes = 32
	LogFormatJSON      = "json"
	LogFormatText      = "text"

	// The login's defaults (docs/adr/0031 D3, docs/adr/0033 D3, D6,
	// docs/adr/0035 D4).
	DefaultSessionLifetime      = 12 * time.Hour
	DefaultSessionIdle          = 2 * time.Hour
	DefaultPasswordMinLength    = 12
	DefaultLoginMaxFailures     = 5
	DefaultLoginAddressLimit    = 20
	DefaultTokenDefaultLifetime = 90 * 24 * time.Hour
	DefaultTokenMaxLifetime     = 365 * 24 * time.Hour
	// LockoutWindow and LockoutAdmin are the values of COWORK_LOGIN_LOCKOUT:
	// a lock ends with its window, or stays until an administrator unlocks.
	LockoutWindow = "window"
	LockoutAdmin  = "admin"

	// The identity provider's defaults (docs/adr/0029 D4, docs/adr/0030 D5).
	// offline_access is in the scopes because without a refresh token the
	// groups refresh cannot run once the access token has expired: Dex, Entra
	// and Okta issue one only for that scope.
	DefaultOIDCScopes        = "openid profile email groups offline_access"
	DefaultOIDCGroupsClaim   = "groups"
	DefaultOIDCGroupsRefresh = 15 * time.Minute
	MinOIDCGroupsRefresh     = time.Minute
	// DefaultOIDCGroupsMaxAge is how old the groups a token's person is judged
	// by may be (docs/adr/0035 D8): a week, so a weekly sign-in in the browser
	// keeps a person's tokens working.
	DefaultOIDCGroupsMaxAge = 7 * 24 * time.Hour
	DefaultOIDCDisplayName  = "single sign-on"
)

// Config is the complete server configuration.
type Config struct {
	// ListenAddr is the address the HTTP server binds, host:port.
	ListenAddr string
	// DatabaseURL is the PostgreSQL connection URL (postgres://...) of the
	// runtime role, which owns nothing (docs/adr/0021 D2). Required.
	DatabaseURL string
	// DatabaseOwnerURL is the connection URL of the owner role the
	// migrations run under. Required where migrations run: `cowork migrate`,
	// and `cowork serve` while MigrateOnStart is true.
	DatabaseOwnerURL string
	// MigrateOnStart makes `cowork serve` apply pending migrations before it listens.
	MigrateOnStart bool
	// LogLevel is the minimum level written to the log.
	LogLevel slog.Level
	// LogFormat is LogFormatJSON or LogFormatText.
	LogFormat string
	// ShutdownTimeout bounds the graceful shutdown after SIGTERM.
	ShutdownTimeout time.Duration
	// BaseURL is the public URL the UI is reached under, without a trailing
	// slash. Optional until a feature (OIDC redirects, links in notifications)
	// needs it.
	BaseURL string
	// SessionKey is the server key (docs/adr/0031 D1): it signs the list
	// cursors (docs/adr/0048 D1). Required by `cowork serve`; standard base64
	// of at least MinSessionKeyBytes bytes.
	SessionKey []byte
	// MaxJSONBody bounds a JSON request body, in bytes; 0 disables the limit
	// (docs/adr/0039 D2).
	MaxJSONBody int64
	// RequestTimeout bounds a request's handling; 0 disables it.
	RequestTimeout time.Duration
	// MaxPageSize is the largest page a list returns; 0 disables the clamp.
	MaxPageSize int
	// MaxQueryLength bounds a search query; 0 disables the limit.
	MaxQueryLength int
	// Storage is the S3-compatible object storage of the attachments
	// (docs/adr/0016 D1); nil when none is configured, and uploads are
	// refused.
	Storage *Storage
	// AttachmentMaxBytes is the per-file maximum of an upload; 0 disables
	// it, and an upload is then buffered whole (docs/adr/0039 D2).
	AttachmentMaxBytes int64
	// AttachmentMaxPerTicket is the number of attachments a ticket takes; 0
	// disables the limit.
	AttachmentMaxPerTicket int
	// AttachmentTenantQuota is the bytes a tenant's attachments may hold
	// together; 0, the default, sets no quota (docs/adr/0016 D6 as amended
	// 2026-10-05).
	AttachmentTenantQuota int64
	// SSEReplayWindow is how long a replica keeps events for a reconnect's
	// replay; SSEMaxStreamsPerPerson how many streams a person holds per
	// replica, 0 for no limit (docs/adr/0054 D5, D8).
	SSEReplayWindow        time.Duration
	SSEMaxStreamsPerPerson int

	// BaseOrigin is BaseURL as a browser writes it into an Origin header:
	// scheme, host and a port that is not the scheme's default
	// (docs/adr/0037 D1). Empty when BaseURL is.
	BaseOrigin string
	// LocalAdminUsername and LocalAdminPassword are the one local account the
	// configuration keeps (docs/adr/0032 D1); both set or both empty. The
	// password is a secret, never echoed.
	LocalAdminUsername string
	LocalAdminPassword string
	// BootstrapTenantSlug and BootstrapTenantName name the tenant a start
	// creates while none exists (docs/adr/0032 D6); both or neither.
	BootstrapTenantSlug string
	BootstrapTenantName string
	// PasswordMinLength is the shortest password a local account may have
	// (docs/adr/0033 D3).
	PasswordMinLength int
	// LoginLockout is LockoutWindow or LockoutAdmin; LoginMaxFailures failures
	// of one username within the lockout window lock it, 0 never; and
	// LoginAddressLimit attempts of one address within a minute are answered
	// 429, 0 never (docs/adr/0033 D6, docs/adr/0039 D6).
	LoginLockout      string
	LoginMaxFailures  int
	LoginAddressLimit int
	// SessionLifetime is the absolute lifetime of a browser session and
	// SessionIdle the time it may lie unused (docs/adr/0031 D3).
	SessionLifetime time.Duration
	SessionIdle     time.Duration
	// TokenDefaultLifetime and TokenMaxLifetime are the lifetime a new
	// personal access token gets and the longest one it may be given
	// (docs/adr/0035 D4).
	TokenDefaultLifetime time.Duration
	TokenMaxLifetime     time.Duration
	// TrustedProxies are the networks of the proxies in front of the backend
	// (docs/adr/0035 D2): the client address of a request is found by walking
	// X-Forwarded-For from the right through them. Empty — the default — means
	// the TCP peer is the client and the header is never read.
	TrustedProxies []netip.Prefix
	// OIDC is the identity provider the browser logs in through
	// (docs/adr/0029); nil when COWORK_OIDC_ISSUER is unset, and the login
	// page then offers none.
	OIDC *OIDC
	// Chat is the chat in the UI and the providers it talks to
	// (docs/adr/0076); nil when COWORK_CHAT_PROVIDERS is unset, and no tenant
	// has a chat.
	Chat *Chat
}

// OIDC is the identity provider: the issuer cowork is a relying party of, and
// the groups that decide who may log in and who administers the installation
// (docs/adr/0029 D4, docs/adr/0030 D1, D5).
type OIDC struct {
	// Issuer is the URL discovery is fetched from: https, or http on a loopback
	// host for development.
	Issuer string
	// ClientID and ClientSecret are the relying party's credentials; the
	// secret is never echoed.
	ClientID     string
	ClientSecret string
	// Scopes are requested at the login; they contain openid.
	Scopes []string
	// GroupsClaim names the claim that carries the groups (docs/adr/0029 D2).
	GroupsClaim string
	// AllowedGroups is the gate: a person with none of them, and not in
	// AdminGroup, may not log in (docs/adr/0030 D1, D8).
	AllowedGroups []string
	// AdminGroup's members are global administrators and behind the gate by
	// definition; empty for none.
	AdminGroup string
	// GroupsRefresh is how often a session's groups are read again and a
	// token's person checked against the gate (docs/adr/0030 D5,
	// docs/adr/0035 D8).
	GroupsRefresh time.Duration
	// GroupsMaxAge is how old a person's stored groups may be for their tokens
	// to work: older ones — no sign-in and no session refresh read them since —
	// refuse the tokens until the person signs in to the browser once
	// (docs/adr/0035 D8). Longer than GroupsRefresh.
	GroupsMaxAge time.Duration
	// EmailTrusted says the issuer's addresses are verified even where it does
	// not say so: a grant by e-mail address then matches a person without the
	// email_verified claim, not only one the issuer marked verified
	// (docs/adr/0030 D3). An address marked unverified never matches.
	EmailTrusted bool
	// DisplayName is the provider's name on the login page's button.
	DisplayName string
}

// Admits reports whether the gate admits somebody at all: without an allowed
// group and an administrator group nobody may log in through the provider
// (docs/adr/0030 D8).
func (o *OIDC) Admits() bool {
	return o != nil && (len(o.AllowedGroups) > 0 || o.AdminGroup != "")
}

// Storage is the object storage the attachments' bytes live in. Endpoint,
// bucket and both keys come together or not at all.
type Storage struct {
	// Endpoint is http:// or https:// with a host and an optional port.
	Endpoint string
	Bucket   string
	// Region is empty to let the client ask the server.
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	// PathStyle addresses the bucket in the path, as MinIO expects.
	PathStyle bool
	// CAFile is a PEM file of the authority a private endpoint's
	// certificate chains to; empty for the system pool.
	CAFile string
}

// Load reads the configuration through lookup, which has the contract of
// os.LookupEnv. Every invalid or missing required value is reported; the
// returned error joins all of them so one restart fixes everything at once.
func Load(lookup func(string) (string, bool)) (Config, error) {
	cfg := Config{
		ListenAddr:      DefaultListenAddr,
		MigrateOnStart:  true,
		LogLevel:        slog.LevelInfo,
		LogFormat:       LogFormatJSON,
		ShutdownTimeout: DefaultShutdownTimeout,
		MaxJSONBody:     DefaultMaxJSONBody,
		RequestTimeout:  DefaultRequestTimeout,
		MaxPageSize:     DefaultMaxPageSize,
		MaxQueryLength:  DefaultMaxQueryLength,

		AttachmentMaxBytes:     DefaultAttachmentMaxBytes,
		AttachmentMaxPerTicket: DefaultAttachmentMaxPerTicket,
		SSEReplayWindow:        DefaultSSEReplayWindow,
		SSEMaxStreamsPerPerson: DefaultSSEMaxStreamsPerPerson,

		PasswordMinLength:    DefaultPasswordMinLength,
		LoginLockout:         LockoutWindow,
		LoginMaxFailures:     DefaultLoginMaxFailures,
		LoginAddressLimit:    DefaultLoginAddressLimit,
		SessionLifetime:      DefaultSessionLifetime,
		SessionIdle:          DefaultSessionIdle,
		TokenDefaultLifetime: DefaultTokenDefaultLifetime,
		TokenMaxLifetime:     DefaultTokenMaxLifetime,
	}
	l := &loader{lookup: lookup}
	l.server(&cfg)
	l.database(&cfg)
	l.logging(&cfg)
	l.sessionKey(&cfg)
	l.limits(&cfg)
	l.storage(&cfg)
	l.login(&cfg)
	l.trustedProxies(&cfg)
	l.chat(&cfg)
	return cfg, errors.Join(l.errs...)
}

// loader reads variables and collects every problem it finds.
type loader struct {
	lookup func(string) (string, bool)
	errs   []error
}

func (l *loader) fail(format string, args ...any) {
	l.errs = append(l.errs, fmt.Errorf(format, args...))
}

func (l *loader) get(key string) (string, bool) {
	return nonEmpty(l.lookup, key)
}

func (l *loader) server(cfg *Config) {
	if v, ok := l.get(EnvListenAddr); ok {
		cfg.ListenAddr = v
	}
	if v, ok := l.get(EnvShutdownTimeout); ok {
		d, err := time.ParseDuration(v)
		switch {
		case err != nil:
			l.fail("%s: %q is not a duration such as 15s", EnvShutdownTimeout, v)
		case d <= 0:
			l.fail("%s: must be positive, got %s", EnvShutdownTimeout, d)
		default:
			cfg.ShutdownTimeout = d
		}
	}
	if v, ok := l.get(EnvBaseURL); ok {
		cfg.BaseURL = strings.TrimRight(v, "/")
	}
}

func (l *loader) database(cfg *Config) {
	if v, ok := l.get(EnvDatabaseURL); ok {
		cfg.DatabaseURL = v
	} else {
		l.fail("%s is required", EnvDatabaseURL)
	}
	if v, ok := l.get(EnvDatabaseOwnerURL); ok {
		cfg.DatabaseOwnerURL = v
	}
	if v, ok := l.get(EnvMigrateOnStart); ok {
		b, err := strconv.ParseBool(v)
		if err != nil {
			l.fail("%s: %q is not a boolean", EnvMigrateOnStart, v)
		} else {
			cfg.MigrateOnStart = b
		}
	}
}

func (l *loader) logging(cfg *Config) {
	if v, ok := l.get(EnvLogLevel); ok {
		var level slog.Level
		if err := level.UnmarshalText([]byte(v)); err != nil {
			l.fail("%s: %q is not one of debug, info, warn, error", EnvLogLevel, v)
		} else {
			cfg.LogLevel = level
		}
	}
	if v, ok := l.get(EnvLogFormat); ok {
		switch strings.ToLower(v) {
		case LogFormatJSON, LogFormatText:
			cfg.LogFormat = strings.ToLower(v)
		default:
			l.fail("%s: %q is not one of %s, %s", EnvLogFormat, v, LogFormatJSON, LogFormatText)
		}
	}
}

// sessionKey reads the server key. The value is a secret: an error names the
// variable, never the value.
func (l *loader) sessionKey(cfg *Config) {
	v, ok := l.get(EnvSessionKey)
	if !ok {
		return
	}
	key, err := base64.StdEncoding.DecodeString(v)
	switch {
	case err != nil:
		l.fail("%s is not standard base64", EnvSessionKey)
	case len(key) < MinSessionKeyBytes:
		l.fail("%s must decode to at least %d bytes, got %d", EnvSessionKey, MinSessionKeyBytes, len(key))
	default:
		cfg.SessionKey = key
	}
}

func (l *loader) limits(cfg *Config) {
	if v, ok := l.get(EnvMaxJSONBody); ok {
		n, err := parseSize(v)
		if err != nil {
			l.fail("%s: %q is not a size such as 1MiB or 0", EnvMaxJSONBody, v)
		} else {
			cfg.MaxJSONBody = n
		}
	}
	if v, ok := l.get(EnvRequestTimeout); ok {
		d, err := time.ParseDuration(v)
		switch {
		case err != nil:
			l.fail("%s: %q is not a duration such as 30s or 0", EnvRequestTimeout, v)
		case d < 0:
			l.fail("%s: must not be negative, got %s", EnvRequestTimeout, d)
		default:
			cfg.RequestTimeout = d
		}
	}
	if v, ok := l.get(EnvSSEReplayWindow); ok {
		d, err := time.ParseDuration(v)
		if err != nil || d < 0 {
			l.fail("%s: %q is not a duration such as 5m", EnvSSEReplayWindow, v)
		} else {
			cfg.SSEReplayWindow = d
		}
	}
	for _, c := range []struct {
		env string
		dst *int
	}{{EnvMaxPageSize, &cfg.MaxPageSize}, {EnvMaxQueryLength, &cfg.MaxQueryLength}, {EnvSSEMaxStreamsPerPerson, &cfg.SSEMaxStreamsPerPerson}} {
		if v, ok := l.get(c.env); ok {
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 {
				l.fail("%s: %q is not a count such as 200 or 0", c.env, v)
			} else {
				*c.dst = n
			}
		}
	}
}

// attachmentLimits reads the limits of the attachments: the per-file maximum,
// the per-ticket count and the tenant's quota (docs/adr/0016 D6), each 0 for
// none.
func (l *loader) attachmentLimits(cfg *Config) {
	if v, ok := l.get(EnvAttachmentMaxBytes); ok {
		n, err := parseSize(v)
		if err != nil {
			l.fail("%s: %q is not a size such as 10MiB or 0", EnvAttachmentMaxBytes, v)
		} else {
			cfg.AttachmentMaxBytes = n
		}
	}
	if v, ok := l.get(EnvAttachmentMaxPerTicket); ok {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			l.fail("%s: %q is not a count such as 100 or 0", EnvAttachmentMaxPerTicket, v)
		} else {
			cfg.AttachmentMaxPerTicket = n
		}
	}
	if v, ok := l.get(EnvAttachmentTenantQuota); ok {
		n, err := parseSize(v)
		if err != nil {
			l.fail("%s: %q is not a size such as 10GiB or 0", EnvAttachmentTenantQuota, v)
		} else {
			cfg.AttachmentTenantQuota = n
		}
	}
}

// storage reads the object storage: all of endpoint, bucket and both keys,
// or none of them. The secret key is never echoed.
func (l *loader) storage(cfg *Config) {
	l.attachmentLimits(cfg)
	st := Storage{PathStyle: true}
	required := []struct {
		env string
		dst *string
	}{{EnvS3Endpoint, &st.Endpoint}, {EnvS3Bucket, &st.Bucket}, {EnvS3AccessKeyID, &st.AccessKeyID}, {EnvS3SecretAccessKey, &st.SecretAccessKey}}
	var set, missing []string
	for _, r := range required {
		if v, ok := l.get(r.env); ok {
			*r.dst = v
			set = append(set, r.env)
		} else {
			missing = append(missing, r.env)
		}
	}
	if len(set) == 0 {
		return
	}
	if len(missing) > 0 {
		l.fail("object storage needs %s together; %s missing", strings.Join([]string{EnvS3Endpoint, EnvS3Bucket, EnvS3AccessKeyID, EnvS3SecretAccessKey}, ", "),
			strings.Join(missing, ", "))
		return
	}
	if !strings.HasPrefix(st.Endpoint, "http://") && !strings.HasPrefix(st.Endpoint, "https://") {
		l.fail("%s is not an http:// or https:// URL", EnvS3Endpoint)
	}
	st.Region, _ = l.get(EnvS3Region)
	st.CAFile, _ = l.get(EnvS3CA)
	if v, ok := l.get(EnvS3UsePathStyle); ok {
		b, err := strconv.ParseBool(v)
		if err != nil {
			l.fail("%s: %q is not a boolean", EnvS3UsePathStyle, v)
		}
		st.PathStyle = b
	}
	cfg.Storage = &st
}

// parseSize reads a byte count: a plain number of bytes, or a number with one
// of the IEC units KiB, MiB, GiB.
func parseSize(v string) (int64, error) {
	units := []struct {
		suffix string
		factor int64
	}{{"GiB", 1 << 30}, {"MiB", 1 << 20}, {"KiB", 1 << 10}, {"B", 1}}
	factor := int64(1)
	for _, u := range units {
		if n, ok := strings.CutSuffix(v, u.suffix); ok {
			v, factor = strings.TrimSpace(n), u.factor
			break
		}
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, err
	}
	if n < 0 || n > (1<<62)/factor {
		return 0, errors.New("out of range")
	}
	return n * factor, nil
}

// nonEmpty reports a variable that is set to something other than whitespace.
func nonEmpty(lookup func(string) (string, bool), key string) (string, bool) {
	v, ok := lookup(key)
	v = strings.TrimSpace(v)
	return v, ok && v != ""
}

// slugPattern is the tenant slug rule of docs/adr/0005 D4.
var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,62}$`)

// login reads everything the browser login needs: the public URL, the
// sessions, the passwords, the lockout, the local administrator, the
// bootstrap tenant and the lifetime of a new token (docs/adr/0031–0033,
// docs/adr/0035 D4, docs/adr/0037 D6). Where a value is a secret the error
// names the variable and never the value.
func (l *loader) login(cfg *Config) {
	l.passwordPolicy(cfg)
	l.lockout(cfg)
	for _, d := range []struct {
		env string
		dst *time.Duration
	}{{EnvSessionLifetime, &cfg.SessionLifetime}, {EnvSessionIdle, &cfg.SessionIdle},
		{EnvTokenDefaultLifetime, &cfg.TokenDefaultLifetime}, {EnvTokenMaxLifetime, &cfg.TokenMaxLifetime}} {
		if v, ok := l.get(d.env); ok {
			n, err := time.ParseDuration(v)
			switch {
			case err != nil:
				l.fail("%s: %q is not a duration such as 12h", d.env, v)
			case n <= 0:
				l.fail("%s: must be positive, got %s", d.env, n)
			default:
				*d.dst = n
			}
		}
	}
	if cfg.TokenDefaultLifetime > cfg.TokenMaxLifetime {
		l.fail("%s must not exceed %s", EnvTokenDefaultLifetime, EnvTokenMaxLifetime)
	}
	l.localAdmin(cfg)
	l.oidc(cfg)
	l.bootstrapTenant(cfg)
	l.baseURL(cfg)
}

// trustedProxies reads the networks of the proxies in front of the backend
// (docs/adr/0035 D2): a comma-separated list of CIDRs, IPv4 and IPv6, a single
// host written /32 or /128. An entry that is none is an error that names the
// variable and quotes that entry and nothing else of the value; every such
// entry is reported. An empty entry, such as a trailing comma, trusts nothing
// and is skipped.
func (l *loader) trustedProxies(cfg *Config) {
	v, ok := l.get(EnvTrustedProxies)
	if !ok {
		return
	}
	for _, entry := range strings.Split(v, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(entry)
		if err != nil {
			l.fail("%s: %q is not a CIDR such as 10.0.0.0/8 or fd00::/8; a single host is written 10.0.0.5/32 or fd00::5/128",
				EnvTrustedProxies, clip(entry, 64))
			continue
		}
		cfg.TrustedProxies = append(cfg.TrustedProxies, prefix.Masked())
	}
}

// clip shortens a value an error quotes, so that a long one in the wrong
// variable does not fill the log.
func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

func (l *loader) passwordPolicy(cfg *Config) {
	v, ok := l.get(EnvPasswordMinLength)
	if !ok {
		return
	}
	n, err := strconv.Atoi(v)
	switch {
	case err != nil:
		l.fail("%s: %q is not a number of characters such as 12", EnvPasswordMinLength, v)
	case n < auth.MinPasswordLengthFloor:
		l.fail("%s: must be at least %d, got %d (docs/adr/0033 D3)", EnvPasswordMinLength, auth.MinPasswordLengthFloor, n)
	case n > auth.MaxPasswordLength:
		l.fail("%s: must be at most %d, got %d", EnvPasswordMinLength, auth.MaxPasswordLength, n)
	default:
		cfg.PasswordMinLength = n
	}
}

func (l *loader) lockout(cfg *Config) {
	if v, ok := l.get(EnvLoginLockout); ok {
		switch strings.ToLower(v) {
		case LockoutWindow, LockoutAdmin:
			cfg.LoginLockout = strings.ToLower(v)
		default:
			l.fail("%s: %q is not one of %s, %s", EnvLoginLockout, v, LockoutWindow, LockoutAdmin)
		}
	}
	for _, c := range []struct {
		env string
		dst *int
	}{{EnvLoginMaxFailures, &cfg.LoginMaxFailures}, {EnvLoginAddressLimit, &cfg.LoginAddressLimit}} {
		if v, ok := l.get(c.env); ok {
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 {
				l.fail("%s: %q is not a count such as 5 or 0", c.env, v)
			} else {
				*c.dst = n
			}
		}
	}
}

// localAdmin reads the one account the configuration keeps (docs/adr/0032 D1,
// D2, D3): both variables or neither. The password is read as it is — a
// trimmed password would be another password — and never echoed.
func (l *loader) localAdmin(cfg *Config) {
	username, haveUser := l.get(EnvLocalAdminUsername)
	password, havePassword := l.lookup(EnvLocalAdminPassword)
	havePassword = havePassword && password != ""
	switch {
	case haveUser && !havePassword:
		l.fail("%s is required while %s is set", EnvLocalAdminPassword, EnvLocalAdminUsername)
		return
	case havePassword && !haveUser:
		l.fail("%s is required while %s is set", EnvLocalAdminUsername, EnvLocalAdminPassword)
		return
	case !haveUser:
		return
	}
	if !auth.ValidUsername(username) {
		l.fail("%s must be 1 to 63 characters of a-z, 0-9, '.', '_' and '-', starting with a letter or a digit", EnvLocalAdminUsername)
		return
	}
	if err := auth.CheckPassword(password, cfg.PasswordMinLength); err != nil {
		// The error names the bound, never the password.
		l.fail("%s %s (%s)", EnvLocalAdminPassword, strings.TrimPrefix(err.Error(), auth.ErrPasswordLength.Error()+": "), EnvPasswordMinLength)
		return
	}
	cfg.LocalAdminUsername, cfg.LocalAdminPassword = username, password
}

// bootstrapTenant reads the tenant a start creates while none exists
// (docs/adr/0032 D6): both variables or neither, and only with somebody to
// administer it — the local administrator, who gets a marked grant, or the
// administrator group, whose mapping the start seeds. A tenant without an
// administrator cannot come to exist (D7).
func (l *loader) bootstrapTenant(cfg *Config) {
	slug, haveSlug := l.get(EnvBootstrapTenantSlug)
	name, haveName := l.get(EnvBootstrapTenantName)
	switch {
	case haveSlug && !haveName:
		l.fail("%s is required while %s is set", EnvBootstrapTenantName, EnvBootstrapTenantSlug)
		return
	case haveName && !haveSlug:
		l.fail("%s is required while %s is set", EnvBootstrapTenantSlug, EnvBootstrapTenantName)
		return
	case !haveSlug:
		return
	}
	if !slugPattern.MatchString(slug) {
		l.fail("%s must be 2 to 63 characters of a-z, 0-9 and '-', not starting with '-' (docs/adr/0005 D4)", EnvBootstrapTenantSlug)
		return
	}
	if n := utf8.RuneCountInString(name); n > 200 {
		l.fail("%s must be at most 200 characters", EnvBootstrapTenantName)
		return
	}
	if cfg.LocalAdminUsername == "" && (cfg.OIDC == nil || cfg.OIDC.AdminGroup == "") {
		l.fail("%s needs %s and %s, or %s: the local administrator or the administrator group becomes the first administrator of the tenant (docs/adr/0032 D6, D7)",
			EnvBootstrapTenantSlug, EnvLocalAdminUsername, EnvLocalAdminPassword, EnvAdminGroup)
		return
	}
	cfg.BootstrapTenantSlug, cfg.BootstrapTenantName = slug, name
}

// baseURL checks the public URL and derives the origin the CSRF check
// compares against (docs/adr/0037 D1). A cookie login needs it: it is required
// while the local administrator is configured (docs/adr/0037 D6), and while an
// identity provider is, whose redirect URI is the URL and /auth/callback
// (docs/adr/0029 D4).
func (l *loader) baseURL(cfg *Config) {
	if cfg.BaseURL == "" {
		if cfg.LocalAdminUsername != "" {
			l.fail("%s is required while %s is set: a cookie login needs the origin the browser sees (docs/adr/0037 D6)",
				EnvBaseURL, EnvLocalAdminUsername)
		}
		if cfg.OIDC != nil {
			l.fail("%s is required while %s is set: the redirect URI is %s/auth/callback (docs/adr/0029 D4)",
				EnvBaseURL, EnvOIDCIssuer, EnvBaseURL)
		}
		return
	}
	origin, err := Origin(cfg.BaseURL)
	if err != nil {
		l.fail("%s %v", EnvBaseURL, err)
		return
	}
	cfg.BaseOrigin = origin
}

// Origin returns the origin a browser sends for a public URL: the lower-case
// scheme and host, and a port only when it is not the scheme's default. The
// URL must be an origin of its own — a scheme, a host, an optional port — as
// the Origin header never carries more (docs/adr/0037 D1). The error never
// quotes the URL.
func Origin(raw string) (string, error) {
	u, err := url.Parse(raw)
	switch {
	case err != nil:
		return "", errors.New("is not a URL")
	case u.Scheme != schemeHTTP && u.Scheme != schemeHTTPS:
		return "", errors.New("must start with http:// or https://")
	case u.Hostname() == "":
		return "", errors.New("names no host")
	case u.User != nil, u.RawQuery != "", u.Fragment != "", (u.Path != "" && u.Path != "/"):
		return "", errors.New("must be an origin such as https://cowork.example.com: no user, path, query or fragment")
	}
	host, port := strings.ToLower(u.Hostname()), u.Port()
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if (u.Scheme == schemeHTTP && port == "80") || (u.Scheme == schemeHTTPS && port == "443") {
		port = ""
	}
	origin := u.Scheme + "://" + host
	if port != "" {
		origin += ":" + port
	}
	return origin, nil
}
