package config

import (
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// The chat's variables (docs/adr/0076).
const (
	EnvChatProvider    = "COWORK_CHAT_PROVIDER"
	EnvChatURL         = "COWORK_CHAT_URL"
	EnvChatAPIKey      = "COWORK_CHAT_API_KEY" // #nosec G101 -- the variable's name, not a credential
	EnvChatModel       = "COWORK_CHAT_MODEL"
	EnvChatInside      = "COWORK_CHAT_INSIDE"
	EnvChatTurnTimeout = "COWORK_CHAT_TURN_TIMEOUT"
	EnvChatMaxSteps    = "COWORK_CHAT_MAX_STEPS"
	EnvChatTurns       = "COWORK_CHAT_TURNS_PER_PERSON"
)

// The chat's providers — the wire formats the gateway speaks — and defaults.
const (
	ChatProviderOpenAI    = "openai"
	ChatProviderAnthropic = "anthropic"
	// DefaultChatTurnTimeout bounds a turn: the model's calls and the tools'.
	DefaultChatTurnTimeout = 5 * time.Minute
	// DefaultChatMaxSteps bounds the calls of the model in one turn.
	DefaultChatMaxSteps = 8
	// DefaultChatTurns bounds the turns one person runs at once, per replica.
	DefaultChatTurns = 2
	// maxChatModel is the longest model name accepted.
	maxChatModel = 200
)

// chatVariables are the chat's variables besides the provider: set without
// it, each is a mistake the start names.
var chatVariables = []string{EnvChatURL, EnvChatAPIKey, EnvChatModel, EnvChatInside, EnvChatTurnTimeout, EnvChatMaxSteps, EnvChatTurns}

// Chat is the model the chat in the UI talks to (docs/adr/0076). Where it
// connects and with which key is configuration only: no request names a host.
type Chat struct {
	// Provider is the wire format, ChatProviderOpenAI or ChatProviderAnthropic.
	Provider string
	// URL is the base URL the gateway appends its path to: /chat/completions
	// for openai (so it ends in /v1, as http://localhost:1234/v1 for LM Studio),
	// /v1/messages for anthropic (https://api.anthropic.com).
	URL string
	// APIKey is the provider's key, a secret that is never echoed; empty for a
	// provider that takes none. Anthropic needs one.
	APIKey string
	// Model is the model by the name its provider knows it.
	Model string
	// Inside declares the provider inside the installation's trust boundary:
	// the chat is available in every tenant. Outside, a tenant's
	// administrators allow it first.
	Inside bool
	// TurnTimeout bounds a turn; 0 disables the limit (docs/adr/0039 D2).
	TurnTimeout time.Duration
	// MaxSteps bounds the calls of the model in one turn; 0 disables it.
	MaxSteps int
	// Turns bounds the turns one person runs at once on one replica; 0
	// disables it.
	Turns int
}

// Fingerprint names the provider a tenant's consent is given to: the wire
// format, the host of the URL and the model. A consent given to another
// fingerprint is none (docs/adr/0076). It holds no key.
func (c Chat) Fingerprint() string {
	host := c.URL
	if u, err := url.Parse(c.URL); err == nil && u.Host != "" {
		host = strings.ToLower(u.Host)
	}
	return c.Provider + " " + host + " " + c.Model
}

// chat reads the chat (docs/adr/0076). Without COWORK_CHAT_PROVIDER there is
// none, and every other variable of it is an error that names the variable.
// The key is never echoed.
func (l *loader) chat(cfg *Config) {
	provider, ok := l.get(EnvChatProvider)
	if !ok {
		for _, env := range chatVariables {
			if _, set := l.get(env); set {
				l.fail("%s is set without %s: it configures the chat, which needs the provider", env, EnvChatProvider)
			}
		}
		return
	}
	c := &Chat{Provider: strings.ToLower(provider), TurnTimeout: DefaultChatTurnTimeout, MaxSteps: DefaultChatMaxSteps, Turns: DefaultChatTurns}
	if c.Provider != ChatProviderOpenAI && c.Provider != ChatProviderAnthropic {
		l.fail("%s: %q is not one of %s, %s", EnvChatProvider, clip(provider, 32), ChatProviderOpenAI, ChatProviderAnthropic)
	}
	if c.URL, ok = l.get(EnvChatURL); !ok {
		l.fail("%s is required while %s is set", EnvChatURL, EnvChatProvider)
	} else if err := checkChatURL(c.URL); err != "" {
		l.fail("%s %s", EnvChatURL, err)
	}
	c.URL = strings.TrimRight(c.URL, "/")
	// The key is read as it is: a trimmed key would be another key.
	if v, set := l.lookup(EnvChatAPIKey); set && v != "" {
		c.APIKey = v
	} else if c.Provider == ChatProviderAnthropic {
		l.fail("%s is required while %s is %s", EnvChatAPIKey, EnvChatProvider, ChatProviderAnthropic)
	}
	if c.Model, ok = l.get(EnvChatModel); !ok {
		l.fail("%s is required while %s is set", EnvChatModel, EnvChatProvider)
	} else if !validModel(c.Model) {
		l.fail("%s must be 1 to %d characters without spaces or control characters", EnvChatModel, maxChatModel)
	}
	l.chatLimits(c)
	cfg.Chat = c
}

func (l *loader) chatLimits(c *Chat) {
	if v, set := l.get(EnvChatInside); set {
		b, err := strconv.ParseBool(v)
		if err != nil {
			l.fail("%s: %q is not a boolean", EnvChatInside, clip(v, 32))
		}
		c.Inside = b
	}
	if v, set := l.get(EnvChatTurnTimeout); set {
		d, err := time.ParseDuration(v)
		if err != nil || d < 0 {
			l.fail("%s: %q is not a duration such as 5m or 0", EnvChatTurnTimeout, clip(v, 32))
		} else {
			c.TurnTimeout = d
		}
	}
	for _, n := range []struct {
		env, example string
		dst          *int
	}{{EnvChatMaxSteps, "8", &c.MaxSteps}, {EnvChatTurns, "2", &c.Turns}} {
		if v, set := l.get(n.env); set {
			count, err := strconv.Atoi(v)
			if err != nil || count < 0 {
				l.fail("%s: %q is not a count such as %s or 0", n.env, clip(v, 32), n.example)
			} else {
				*n.dst = count
			}
		}
	}
}

// checkChatURL holds the provider's URL to https, or to http on a host of
// the operator's own network — localhost, a loopback or private address, a
// name without a domain or under a domain that never leaves a network
// (Kubernetes' .svc and .cluster.local, .internal, .local, .lan, .home.arpa).
// The key and the tenants' data travel in every request, and plain http
// beyond the operator's network would hand both to every hop. The answer never
// quotes the URL.
func checkChatURL(raw string) string {
	u, err := url.Parse(raw)
	switch {
	case err != nil || u.Host == "" || u.Hostname() == "":
		return "is not a URL such as https://api.anthropic.com or http://localhost:1234/v1"
	case u.User != nil || u.RawQuery != "" || u.Fragment != "":
		return "must have no user, query or fragment"
	case u.Scheme == schemeHTTPS:
		return ""
	case u.Scheme == schemeHTTP && ownNetwork(u.Hostname()):
		return ""
	}
	return "must be https://, or http:// on a host of the operator's network: localhost, a private address, or a name such as lmstudio or ollama.ai.svc"
}

// ownNetwork reports whether a host is one plain http may reach: see
// checkChatURL.
func ownNetwork(host string) bool {
	if loopback(host) {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback() || ip.IsPrivate()
	}
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if !strings.Contains(host, ".") {
		return true
	}
	for _, suffix := range []string{".localhost", ".svc", ".cluster.local", ".internal", ".local", ".lan", ".home.arpa"} {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
}

// validModel accepts a model name as providers write them — qwen/qwen3.6-35b-a3b,
// claude-sonnet-4-5, llama3:8b — and nothing with a space or a control
// character in it.
func validModel(s string) bool {
	if s == "" || len(s) > maxChatModel {
		return false
	}
	for _, r := range s {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
	}
	return true
}
