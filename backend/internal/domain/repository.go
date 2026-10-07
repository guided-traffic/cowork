package domain

import (
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// A repository is bound to a project by its normalised remote identity,
// host/path (docs/adr/0066 D1, docs/adr/0006 D3), and optionally by a
// sub-directory of a monorepo.

// ErrNotARemote is a remote that names no host: a local path, a file URL, or
// a string that is no URL at all. It has no identity to bind.
var ErrNotARemote = errors.New("not a network remote: expected an SSH, scp-style, git or HTTP(S) URL with a host and a path")

// maxIdentity bounds an identity, a binding's path and a stored remote.
const maxIdentity = 512

// defaultPorts are the ports a scheme implies; the identity drops them.
var defaultPorts = map[string]string{
	"ssh": "22", "git+ssh": "22", "ssh+git": "22", "git": "9418", "http": "80", "https": "443",
}

// scpLike is git's scp-style remote, [user@]host:path, whose host carries no
// slash; a one-letter host would be a Windows drive and is refused.
var scpLike = regexp.MustCompile(`^(?:([^@/:]+)@)?([A-Za-z0-9.-]{2,}|\[[0-9A-Fa-f:.]+\]):(.*)$`)

var hostPattern = regexp.MustCompile(`^(?:[a-z0-9]([a-z0-9-]*[a-z0-9])?)(?:\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*$|^\[[0-9a-f:.]+\]$`)

// NormaliseRemote reduces a remote URL to its identity, host/path: the
// scheme, the user, a default port, a trailing .git and trailing slashes are
// removed, the host is lower-cased, and the path keeps its case and every
// segment (docs/adr/0066 D1). git@github.com:guided-traffic/cowork.git,
// https://github.com/guided-traffic/cowork and
// ssh://git@github.com:22/guided-traffic/cowork/ are one identity,
// github.com/guided-traffic/cowork. A remote that names no host is
// ErrNotARemote.
func NormaliseRemote(raw string) (string, error) {
	host, port, path, ok := splitRemote(strings.TrimSpace(raw))
	if !ok {
		return "", ErrNotARemote
	}
	host = strings.ToLower(host)
	if !hostPattern.MatchString(host) {
		return "", ErrNotARemote
	}
	segments, ok := pathSegments(path)
	if !ok {
		return "", ErrNotARemote
	}
	last := len(segments) - 1
	segments[last] = strings.TrimSuffix(segments[last], ".git")
	if segments[last] == "" {
		return "", ErrNotARemote
	}
	if port != "" {
		host += ":" + port
	}
	identity := host + "/" + strings.Join(segments, "/")
	if len(identity) > maxIdentity {
		return "", ErrNotARemote
	}
	return identity, nil
}

// splitRemote reads the host, the non-default port and the path of a URL or
// an scp-style remote.
func splitRemote(raw string) (host, port, path string, ok bool) {
	if raw == "" || strings.ContainsAny(raw, " \t\r\n\\") {
		return "", "", "", false
	}
	if strings.Contains(raw, "://") {
		return splitURL(raw)
	}
	m := scpLike.FindStringSubmatch(raw)
	if m == nil {
		return "", "", "", false
	}
	return m[2], "", m[3], true
}

// splitURL reads a remote with a scheme git knows; a default port is
// dropped.
func splitURL(raw string) (host, port, path string, ok bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Opaque != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", "", "", false
	}
	def, known := defaultPorts[strings.ToLower(u.Scheme)]
	if !known || u.Hostname() == "" {
		return "", "", "", false
	}
	host = u.Hostname()
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if p := u.Port(); p != "" && p != def {
		if n, err := strconv.Atoi(p); err != nil || n < 1 || n > 65535 {
			return "", "", "", false
		}
		port = p
	}
	return host, port, u.Path, true
}

// pathSegments splits a remote's path into its segments, dropping empty ones
// and refusing a relative step.
func pathSegments(path string) ([]string, bool) {
	var out []string
	for _, s := range strings.Split(path, "/") {
		switch {
		case s == "":
			continue
		case s == "." || s == "..":
			return nil, false
		case !printable(s) || strings.ContainsAny(s, "?#"):
			return nil, false
		}
		out = append(out, s)
	}
	return out, len(out) > 0
}

func printable(s string) bool {
	for _, r := range s {
		if !unicode.IsPrint(r) || unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

// SanitiseRemote is a remote as cowork may store and show it, the last
// original form a binding keeps (docs/adr/0066 D1): an HTTP(S) URL loses its
// user information, which can carry a password or a token, and any other URL
// loses a password. An scp-style remote carries no password and stays. A URL
// that url.Parse refuses loses its whole user information (withoutUserinfo).
func SanitiseRemote(raw string) string {
	raw = strings.TrimSpace(raw)
	if !strings.Contains(raw, "://") {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return withoutUserinfo(raw)
	}
	if u.User == nil {
		return raw
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		u.User = nil
	default:
		u.User = url.User(u.User.Username())
	}
	return u.String()
}

// withoutUserinfo is a URL that url.Parse refuses — for a character its user
// information must escape and does not, or an escape that is malformed —
// without everything between its "://" and the last "@" of its authority,
// which ends at the first "/", "?" or "#", as url.Parse and git read it.
// Neither the user nor the password can be told apart in such a remote, so
// both go, whatever the scheme. The remote is cut rather than left out: it is
// still shown, and one whose user information was its only flaw now parses
// and binds by its identity.
func withoutUserinfo(raw string) string {
	scheme, rest, _ := strings.Cut(raw, "://")
	authority := rest
	if i := strings.IndexAny(rest, "/?#"); i >= 0 {
		authority = rest[:i]
	}
	at := strings.LastIndex(authority, "@")
	if at < 0 {
		return raw
	}
	return scheme + "://" + rest[at+1:]
}

// NormaliseRepositoryPath is the sub-directory of a monorepo a binding names
// (docs/adr/0006 D3), relative to the repository root, without leading or
// trailing slashes; "" is the whole repository. A relative step is refused.
func NormaliseRepositoryPath(path string) (string, error) {
	var out []string
	for _, s := range strings.Split(strings.ReplaceAll(strings.TrimSpace(path), `\`, "/"), "/") {
		switch {
		case s == "" || s == ".":
			continue
		case s == ".." || !printable(s):
			return "", errors.New("must be a path inside the repository, without .. and without spaces or control characters")
		}
		out = append(out, s)
	}
	p := strings.Join(out, "/")
	if len(p) > maxIdentity {
		return "", errors.New("must be at most 512 characters")
	}
	return p, nil
}

// PathCovers reports whether a binding of the sub-directory bound applies to
// a session in the sub-directory at: the whole repository covers every path,
// and a sub-directory itself and everything below it.
func PathCovers(bound, at string) bool {
	return bound == "" || at == bound || strings.HasPrefix(at, bound+"/")
}

// RepositoryName is the last segment of an identity, the repository's name.
func RepositoryName(identity string) string {
	return identity[strings.LastIndex(identity, "/")+1:]
}

// RemoteOwner is an identity without its last segment: the host and the
// owner or group the repository lives under (docs/adr/0066 D2).
func RemoteOwner(identity string) string {
	if i := strings.LastIndex(identity, "/"); i >= 0 {
		return identity[:i]
	}
	return identity
}

// fallbackKey is the proposal for a name with no letter to build a key from.
const fallbackKey = "REPO"

// ProposeProjectKey derives a project key from a repository's name
// (docs/adr/0066 D2): the initials of its parts when a hyphen, an underscore
// or a dot divides it — valkey-operator is VO — and otherwise its first three
// letters, upper case, two to ten characters, starting with a letter
// (docs/adr/0007 D1).
func ProposeProjectKey(name string) string {
	parts := strings.FieldsFunc(name, func(r rune) bool { return r == '-' || r == '_' || r == '.' || unicode.IsSpace(r) })
	var key []byte
	if len(parts) >= 2 {
		for _, p := range parts {
			if c := keyChars(p); c != "" {
				key = append(key, c[0])
			}
		}
	}
	key = []byte(strings.TrimLeft(string(key), "0123456789"))
	if len(key) < 2 {
		key = []byte(strings.TrimLeft(keyChars(name), "0123456789"))
		key = key[:min(len(key), 3)]
	}
	switch {
	case len(key) == 0:
		return fallbackKey
	case len(key) == 1:
		key = append(key, 'X')
	}
	return string(key[:min(len(key), 10)])
}

// keyChars is s upper-cased with everything but A-Z and 0-9 removed.
func keyChars(s string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(s) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// KeyCandidate is the n-th proposal for a base key whose earlier ones are
// taken (docs/adr/0066 D2): the base itself for n < 2, else the base with n
// appended, shortened to fit ten characters.
func KeyCandidate(base string, n int) string {
	if n < 2 {
		return base
	}
	suffix := strconv.Itoa(n)
	return base[:min(len(base), 10-len(suffix))] + suffix
}
