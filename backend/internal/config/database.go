package config

import (
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

// The components of each role's connection (docs/adr/0058 D4): a Secret that
// holds no URL is read key by key, and the URL is composed here.
const (
	EnvDatabaseHost     = "COWORK_DATABASE_HOST"
	EnvDatabasePort     = "COWORK_DATABASE_PORT"
	EnvDatabaseName     = "COWORK_DATABASE_NAME"
	EnvDatabaseUser     = "COWORK_DATABASE_USER"
	EnvDatabasePassword = "COWORK_DATABASE_PASSWORD" // #nosec G101 -- the variable's name, not a credential
	EnvDatabaseSSLMode  = "COWORK_DATABASE_SSLMODE"

	EnvDatabaseOwnerHost     = "COWORK_DATABASE_OWNER_HOST"
	EnvDatabaseOwnerPort     = "COWORK_DATABASE_OWNER_PORT"
	EnvDatabaseOwnerName     = "COWORK_DATABASE_OWNER_NAME"
	EnvDatabaseOwnerUser     = "COWORK_DATABASE_OWNER_USER"
	EnvDatabaseOwnerPassword = "COWORK_DATABASE_OWNER_PASSWORD" // #nosec G101 -- the variable's name, not a credential
	EnvDatabaseOwnerSSLMode  = "COWORK_DATABASE_OWNER_SSLMODE"
)

// databaseVars names the variables of one role's connection: its URL, or the
// components the URL is composed of.
type databaseVars struct {
	url, host, port, name, user, password, sslmode string
}

// The runtime role's variables and the owner role's (docs/adr/0021 D2).
var (
	runtimeDatabase = databaseVars{EnvDatabaseURL, EnvDatabaseHost, EnvDatabasePort, EnvDatabaseName,
		EnvDatabaseUser, EnvDatabasePassword, EnvDatabaseSSLMode}
	ownerDatabase = databaseVars{EnvDatabaseOwnerURL, EnvDatabaseOwnerHost, EnvDatabaseOwnerPort, EnvDatabaseOwnerName,
		EnvDatabaseOwnerUser, EnvDatabaseOwnerPassword, EnvDatabaseOwnerSSLMode}
)

// required lists the components a connection cannot do without.
func (v databaseVars) required() string {
	return strings.Join([]string{v.host, v.name, v.user}, ", ") + " and " + v.password
}

// connection names one role's connection in an error that says it is
// missing: its URL, or the components a connection cannot do without.
func (v databaseVars) connection() string {
	return v.url + ", or its components " + v.required() + ","
}

// OwnerConnection names the owner role's connection, for the commands that
// require it.
func OwnerConnection() string { return ownerDatabase.connection() }

// sslModes are the values of sslmode the driver knows.
var sslModes = []string{"disable", "allow", "prefer", "require", "verify-ca", "verify-full"}

// verifyingModes are the values of sslmode under which the driver checks the
// server's certificate against the sslrootcert it is given: require and
// verify-ca the chain, verify-full the host as well. Under the others it
// checks nothing, or connects in plain text.
var verifyingModes = []string{"require", "verify-ca", "verify-full"}

// dbComponents is what the component variables of one role hold, and which of
// them are set.
type dbComponents struct {
	host, port, name, user, password, sslmode string
	set                                       []string
}

// databaseURL reads one role's connection: its URL as it is, or the URL
// composed of the components — the host, the database's name, the user and
// the password, and an optional port and sslmode. set reports whether any of
// the variables is set; the URL and a component together, a component without
// the others a connection needs, or a component the driver would not read is
// an error. The error names the variables and quotes a rejected port, sslmode
// or host, never the password or the URL; the password is read as it is, a
// trimmed password being another password.
func (l *loader) databaseURL(v databaseVars) (string, bool) {
	raw, haveURL := l.get(v.url)
	c := l.components(v)
	switch {
	case !haveURL && len(c.set) == 0:
		return "", false
	case haveURL && len(c.set) > 0:
		l.fail("set %s or its components, not both: %s set as well", v.url, strings.Join(c.set, ", "))
		return "", true
	case haveURL:
		return raw, true
	}
	var missing []string
	for _, r := range []struct{ env, value string }{{v.host, c.host}, {v.name, c.name}, {v.user, c.user}, {v.password, c.password}} {
		if r.value == "" {
			missing = append(missing, r.env)
		}
	}
	if len(missing) > 0 {
		l.fail("the components of a database connection need %s together; %s missing", v.required(), strings.Join(missing, ", "))
		return "", true
	}
	if !l.validComponents(v, c) {
		return "", true
	}
	return composeDatabaseURL(c), true
}

// components reads the component variables of one role.
func (l *loader) components(v databaseVars) dbComponents {
	var c dbComponents
	for _, r := range []struct {
		env string
		dst *string
	}{{v.host, &c.host}, {v.port, &c.port}, {v.name, &c.name}, {v.user, &c.user}, {v.sslmode, &c.sslmode}} {
		if value, ok := l.get(r.env); ok {
			*r.dst = value
			c.set = append(c.set, r.env)
		}
	}
	if password, ok := l.lookup(v.password); ok && password != "" {
		c.password = password
		c.set = append(c.set, v.password)
	}
	return c
}

// validComponents checks what a URL could not carry or the driver would not
// read: a host with a character no host has, a port that is no port, an
// sslmode the driver does not know. A host written in brackets, as an IPv6
// address is in a URL, is taken without them.
func (l *loader) validComponents(v databaseVars, c dbComponents) bool {
	ok := true
	if host := strings.TrimSuffix(strings.TrimPrefix(c.host, "["), "]"); strings.ContainsAny(host, "/?#@[] \t\r\n") {
		l.fail("%s: %q is not a host name or an IP address", v.host, clip(c.host, 64))
		ok = false
	}
	if c.port != "" {
		if n, err := strconv.Atoi(c.port); err != nil || n < 1 || n > 65535 {
			l.fail("%s: %q is not a port such as 5432", v.port, clip(c.port, 16))
			ok = false
		}
	}
	if c.sslmode != "" && !slices.Contains(sslModes, c.sslmode) {
		l.fail("%s: %q is not one of %s", v.sslmode, clip(c.sslmode, 32), strings.Join(sslModes, ", "))
		ok = false
	}
	return ok
}

// withRootCert names caFile, COWORK_DATABASE_CA, as the sslrootcert of one
// role's connection (docs/adr/0058 D3), which the driver reads as libpq does:
// the server's certificate must chain to that authority alone — the system
// pool is not consulted —, require checks the chain as verify-ca does, and
// verify-full the host as well. A connection that is no postgres:// URL, that
// names an sslrootcert of its own, or whose sslmode checks nothing is an error
// that names the variables and never quotes the URL. An unset connection
// stays unset.
func (l *loader) withRootCert(v databaseVars, conn, caFile string) string {
	if conn == "" {
		return conn
	}
	u, err := url.Parse(conn)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		l.fail("%s needs the connection of %s as a postgres:// URL", EnvDatabaseCA, v.url)
		return conn
	}
	q := u.Query()
	if q.Has("sslrootcert") {
		l.fail("%s and an sslrootcert in %s name two authorities: set one of them", EnvDatabaseCA, v.url)
		return conn
	}
	if mode := q.Get("sslmode"); !slices.Contains(verifyingModes, mode) {
		shown := strconv.Quote(clip(mode, 32))
		if mode == "" {
			shown = "unset, the driver's prefer"
		}
		l.fail("%s checks nothing while the sslmode of %s is %s: set it to verify-full, verify-ca or require (%s, or the URL's sslmode)",
			EnvDatabaseCA, v.url, shown, v.sslmode)
		return conn
	}
	q.Set("sslrootcert", caFile)
	u.RawQuery = q.Encode()
	return u.String()
}

// composeDatabaseURL writes the components as the URL the driver reads:
// postgres://user:password@host:port/name?sslmode=…, the user, the password
// and the name escaped, so that a reserved character in any of them — @, :,
// /, %, ? or # in a generated password — stays part of it. Without a port the
// driver connects to 5432, without an sslmode it takes its default, prefer.
func composeDatabaseURL(c dbComponents) string {
	host := strings.TrimSuffix(strings.TrimPrefix(c.host, "["), "]")
	switch {
	case c.port != "":
		host = net.JoinHostPort(host, c.port)
	case strings.Contains(host, ":"):
		host = "[" + host + "]"
	}
	u := url.URL{Scheme: "postgres", User: url.UserPassword(c.user, c.password), Host: host, Path: "/" + c.name}
	if c.sslmode != "" {
		u.RawQuery = url.Values{"sslmode": {c.sslmode}}.Encode()
	}
	return u.String()
}
