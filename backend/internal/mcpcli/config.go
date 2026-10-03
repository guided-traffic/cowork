package mcpcli

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/guided-traffic/cowork/backend/internal/auth"
)

// The configuration of cowork-mcp is the environment and nothing else
// (docs/adr/0040 D4): the installation and the token. The working directory
// is the host's, CLAUDE_PROJECT_DIR when the host names it
// (docs/adr/0041 D4).
const (
	EnvURL        = "COWORK_URL"
	EnvToken      = "COWORK_TOKEN" // #nosec G101 -- the variable's name, not a credential
	EnvProjectDir = "CLAUDE_PROJECT_DIR"
)

type config struct {
	url, token, dir string
}

// errUnconfigured is a configuration that names no installation or no token:
// the hooks are silent about it (docs/adr/0067 D2).
var errUnconfigured = errors.New("not configured")

// loadConfig reads and checks the environment. An error names the variable
// and never the token's value (docs/adr/0041 D5).
func loadConfig(lookup func(string) (string, bool)) (config, error) {
	var c config
	raw, _ := lookup(EnvURL)
	c.token, _ = lookup(EnvToken)
	c.dir, _ = lookup(EnvProjectDir)
	raw, c.token = strings.TrimSpace(raw), strings.TrimSpace(c.token)
	switch {
	case raw == "":
		return c, fmt.Errorf("%w: %s is not set; it is the URL of the cowork installation, such as https://cowork.example.com", errUnconfigured, EnvURL)
	case c.token == "":
		return c, fmt.Errorf("%w: %s is not set; make a token on the installation's token page, %s/me/tokens", errUnconfigured, EnvToken, strings.TrimRight(raw, "/"))
	}
	u, err := url.Parse(raw)
	switch {
	case err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "":
		return c, fmt.Errorf("%s must be the installation's URL, http(s)://host[:port][/path], without user, query or fragment", EnvURL)
	case u.Scheme == "http" && !loopback(u.Hostname()):
		return c, fmt.Errorf("%s uses http to a host that is not this machine; the token would cross the network in plain text: use https", EnvURL)
	}
	c.url = strings.TrimRight(u.String(), "/")
	if !auth.WellFormedToken(c.token) {
		return c, fmt.Errorf("%s is not a cowork token, cwk_ and 43 letters and digits; make one on %s/me/tokens", EnvToken, c.url)
	}
	return c, nil
}

// loopback reports whether a host is this machine: a port-forward to the
// backend Service is served over plain http.
func loopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
