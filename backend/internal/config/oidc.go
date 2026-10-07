package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// oidcVariables are the identity provider's variables besides the issuer: set
// without it, each is a mistake the start names (docs/adr/0029 D4).
var oidcVariables = []string{
	EnvOIDCClientID, EnvOIDCClientSecret, EnvOIDCScopes, EnvOIDCGroupsClaim,
	EnvOIDCAllowedGroups, EnvAdminGroup, EnvOIDCGroupsRefresh, EnvOIDCGroupsMaxAge, EnvOIDCEmailTrusted,
	EnvOIDCDisplayName,
}

// maxGroupName is the longest group name a mapping holds, and so the longest
// one a gate names (docs/adr/0030 D2).
const maxGroupName = 256

// The schemes of a public URL and of an issuer.
const (
	schemeHTTP  = "http"
	schemeHTTPS = "https"
)

// oidc reads the identity provider (docs/adr/0029 D4, docs/adr/0030 D1, D5,
// D8). Without COWORK_OIDC_ISSUER there is none, and every other variable of
// it is an error that names the variable. The client's id and secret are read
// here and required by `cowork serve` alone (RequireClient): the migration run
// reads the administrator group for the bootstrap and never meets the issuer,
// so it holds no client secret (docs/adr/0057 D4). The secret is never echoed.
func (l *loader) oidc(cfg *Config) {
	issuer, ok := l.get(EnvOIDCIssuer)
	if !ok {
		for _, env := range oidcVariables {
			if _, set := l.get(env); set {
				l.fail("%s is set without %s: it configures the identity provider, which needs the issuer", env, EnvOIDCIssuer)
			}
		}
		return
	}
	// The issuer is kept as written: discovery compares it exactly with the
	// issuer the document names, and some issuers end theirs with a slash.
	o := &OIDC{
		Issuer:        issuer,
		GroupsClaim:   DefaultOIDCGroupsClaim,
		GroupsRefresh: DefaultOIDCGroupsRefresh,
		GroupsMaxAge:  DefaultOIDCGroupsMaxAge,
		DisplayName:   DefaultOIDCDisplayName,
		Scopes:        strings.Fields(DefaultOIDCScopes),
	}
	if err := checkIssuer(o.Issuer); err != "" {
		l.fail("%s %s", EnvOIDCIssuer, err)
	}
	o.ClientID, _ = l.get(EnvOIDCClientID)
	// The secret is read as it is: a trimmed secret would be another secret.
	if v, set := l.lookup(EnvOIDCClientSecret); set && v != "" {
		o.ClientSecret = v
	}
	l.oidcScopes(o)
	l.oidcGroups(o)
	l.oidcGroupsTimes(o)
	if v, set := l.get(EnvOIDCEmailTrusted); set {
		b, err := strconv.ParseBool(v)
		if err != nil {
			l.fail("%s: %q is not a boolean", EnvOIDCEmailTrusted, clip(v, 64))
		} else {
			o.EmailTrusted = b
		}
	}
	if v, set := l.get(EnvOIDCDisplayName); set {
		if utf8.RuneCountInString(v) > 64 {
			l.fail("%s must be at most 64 characters", EnvOIDCDisplayName)
		} else {
			o.DisplayName = v
		}
	}
	cfg.OIDC = o
}

// RequireClient reports what `cowork serve` needs of a configured identity
// provider besides what Load checks: the client's id and secret — a public
// client without a secret is not supported (docs/adr/0029 D4). Nil without a
// provider. The error names the variables, never the secret.
func (o *OIDC) RequireClient() error {
	if o == nil {
		return nil
	}
	var errs []error
	if o.ClientID == "" {
		errs = append(errs, fmt.Errorf("%s is required while %s is set", EnvOIDCClientID, EnvOIDCIssuer))
	}
	if o.ClientSecret == "" {
		errs = append(errs, fmt.Errorf("%s is required while %s is set: a public client without a secret is not supported (docs/adr/0029 D4)",
			EnvOIDCClientSecret, EnvOIDCIssuer))
	}
	return errors.Join(errs...)
}

// oidcGroupsTimes reads how often a session reads the groups again and how old
// they may be for a token (docs/adr/0030 D5, docs/adr/0035 D8). The maximum age
// must be longer than the interval: a person whose session refreshes the groups
// would otherwise find their tokens refused between two refreshes.
func (l *loader) oidcGroupsTimes(o *OIDC) {
	if v, set := l.get(EnvOIDCGroupsRefresh); set {
		d, err := time.ParseDuration(v)
		switch {
		case err != nil:
			l.fail("%s: %q is not a duration such as 15m", EnvOIDCGroupsRefresh, clip(v, 64))
			return
		case d < MinOIDCGroupsRefresh:
			l.fail("%s: must be at least %s, got %s", EnvOIDCGroupsRefresh, MinOIDCGroupsRefresh, d)
			return
		}
		o.GroupsRefresh = d
	}
	if v, set := l.get(EnvOIDCGroupsMaxAge); set {
		d, err := time.ParseDuration(v)
		if err != nil {
			l.fail("%s: %q is not a duration such as 168h", EnvOIDCGroupsMaxAge, clip(v, 64))
			return
		}
		o.GroupsMaxAge = d
	}
	if o.GroupsMaxAge <= o.GroupsRefresh {
		l.fail("%s must be longer than %s: %s is not longer than %s", EnvOIDCGroupsMaxAge, EnvOIDCGroupsRefresh,
			o.GroupsMaxAge, o.GroupsRefresh)
	}
}

// checkIssuer holds the issuer to https, or to http on a loopback host for a
// development issuer: the browser carries the code over the issuer's
// redirects, and the backend the client secret to its token endpoint. The
// answer never quotes the URL.
func checkIssuer(raw string) string {
	u, err := url.Parse(raw)
	switch {
	case err != nil || u.Host == "":
		return "is not a URL such as https://login.example.com"
	case u.User != nil || u.RawQuery != "" || u.Fragment != "":
		return "must have no user, query or fragment"
	case u.Scheme == schemeHTTPS:
		return ""
	case u.Scheme == schemeHTTP && loopback(u.Hostname()):
		return ""
	}
	return "must be https://, or http:// on localhost for development"
}

func loopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// oidcScopes reads the scopes, separated by spaces or commas; openid is the
// one a relying party cannot do without.
func (l *loader) oidcScopes(o *OIDC) {
	v, set := l.get(EnvOIDCScopes)
	if !set {
		return
	}
	scopes := splitList(v, " ,")
	if !slices.Contains(scopes, "openid") {
		l.fail("%s must contain openid", EnvOIDCScopes)
		return
	}
	o.Scopes = scopes
}

// oidcGroups reads the claim's name, the gate and the administrator group. A
// group is compared exactly, case and all (docs/adr/0029 D2); the lists are
// comma-separated, so a group name holds no comma.
func (l *loader) oidcGroups(o *OIDC) {
	if v, set := l.get(EnvOIDCGroupsClaim); set {
		o.GroupsClaim = v
	}
	if v, set := l.get(EnvOIDCAllowedGroups); set {
		for _, g := range splitList(v, ",") {
			if utf8.RuneCountInString(g) > maxGroupName {
				l.fail("%s: a group name is at most %d characters", EnvOIDCAllowedGroups, maxGroupName)
				return
			}
			o.AllowedGroups = append(o.AllowedGroups, g)
		}
	}
	if v, set := l.get(EnvAdminGroup); set {
		if utf8.RuneCountInString(v) > maxGroupName {
			l.fail("%s: a group name is at most %d characters", EnvAdminGroup, maxGroupName)
			return
		}
		o.AdminGroup = v
	}
}

// splitList splits a value at any of the separators, trims every entry and
// drops the empty ones and repetitions.
func splitList(v, separators string) []string {
	var out []string
	for _, f := range strings.FieldsFunc(v, func(r rune) bool { return strings.ContainsRune(separators, r) }) {
		if f = strings.TrimSpace(f); f != "" && !slices.Contains(out, f) {
			out = append(out, f)
		}
	}
	return out
}
