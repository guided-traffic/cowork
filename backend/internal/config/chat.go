package config

import (
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// The chat's variables (docs/adr/0076). COWORK_CHAT_PROVIDERS names the
// providers; each has variables of its own, ChatEnv(id, suffix).
const (
	EnvChatProviders   = "COWORK_CHAT_PROVIDERS"
	EnvChatTurnTimeout = "COWORK_CHAT_TURN_TIMEOUT"
	EnvChatMaxSteps    = "COWORK_CHAT_MAX_STEPS"
	EnvChatTurns       = "COWORK_CHAT_TURNS_PER_PERSON"
)

// The variables of one provider, COWORK_CHAT_<ID>_<suffix>.
const (
	ChatSuffixName   = "NAME"
	ChatSuffixKind   = "KIND"
	ChatSuffixURL    = "URL"
	ChatSuffixModel  = "MODEL"
	ChatSuffixAPIKey = "API_KEY" // #nosec G101 -- a variable name's suffix, not a credential
)

// The chat's providers' wire formats, and its defaults.
const (
	ChatKindOpenAI    = "openai"
	ChatKindAnthropic = "anthropic"
	// DefaultChatTurnTimeout bounds a turn: the model's calls and the tools'.
	DefaultChatTurnTimeout = 5 * time.Minute
	// DefaultChatMaxSteps bounds the calls of the model in one turn.
	DefaultChatMaxSteps = 8
	// DefaultChatTurns bounds the turns one person runs at once, per replica.
	DefaultChatTurns = 2
	// maxChatModel is the longest model name accepted, maxChatName the
	// longest name of a provider.
	maxChatModel = 200
	maxChatName  = 64
)

// chatID is a provider's id: lowercase letters, digits and dashes, 1 to 32
// characters, a dash neither first nor last.
var chatID = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,30}[a-z0-9])?$`)

// chatVariables are the chat's variables besides the providers: set without
// them, each is a mistake the start names.
var chatVariables = []string{EnvChatTurnTimeout, EnvChatMaxSteps, EnvChatTurns}

// Chat is the chat in the UI (docs/adr/0076): the providers the person picks
// from, and the limits of a turn. Where a provider connects and with which
// key is configuration only: no request names a host.
type Chat struct {
	// Providers are the configured providers in the order of
	// COWORK_CHAT_PROVIDERS; the first is a turn's default.
	Providers []ChatProvider
	// TurnTimeout bounds a turn; 0 disables the limit (docs/adr/0039 D2).
	TurnTimeout time.Duration
	// MaxSteps bounds the calls of the model in one turn; 0 disables it.
	MaxSteps int
	// Turns bounds the turns one person runs at once on one replica; 0
	// disables it.
	Turns int
}

// ChatProvider is one model the chat talks to.
type ChatProvider struct {
	// ID names the provider in COWORK_CHAT_PROVIDERS, in its variables and in
	// a turn; Name is what the panel shows.
	ID, Name string
	// Kind is the wire format, ChatKindOpenAI or ChatKindAnthropic.
	Kind string
	// URL is the base URL the gateway appends its path to: /chat/completions
	// for openai (so it ends in /v1, as http://localhost:1234/v1 for LM
	// Studio), /v1/messages for anthropic (https://api.anthropic.com).
	URL string
	// APIKey is the provider's key, a secret that is never echoed; empty for a
	// provider that takes none. Anthropic needs one.
	APIKey string
	// Model is the model by the name its provider knows it.
	Model string
}

// ChatEnv is the variable of a provider: COWORK_CHAT_ followed by the id
// upper-cased with its dashes as underscores, and the suffix —
// ChatEnv("lm-studio", ChatSuffixURL) is COWORK_CHAT_LM_STUDIO_URL. An id has no
// underscore, so two ids never share a variable.
func ChatEnv(id, suffix string) string {
	return "COWORK_CHAT_" + strings.ToUpper(strings.ReplaceAll(id, "-", "_")) + "_" + suffix
}

// chat reads the chat (docs/adr/0076). Without COWORK_CHAT_PROVIDERS there is
// none, and a limit of it is an error that names the variable. Every error
// names its variable and never quotes a URL or a key.
func (l *loader) chat(cfg *Config) {
	list, ok := l.get(EnvChatProviders)
	if !ok {
		for _, env := range chatVariables {
			if _, set := l.get(env); set {
				l.fail("%s is set without %s: it configures the chat, which needs a provider", env, EnvChatProviders)
			}
		}
		return
	}
	c := &Chat{TurnTimeout: DefaultChatTurnTimeout, MaxSteps: DefaultChatMaxSteps, Turns: DefaultChatTurns}
	seen := map[string]bool{}
	for i, raw := range strings.Split(list, ",") {
		id := strings.TrimSpace(raw)
		switch {
		case !chatID.MatchString(id):
			// The entry is not quoted: a URL or a key put there by mistake
			// stays out of the log.
			l.fail("%s: entry %d is not a provider id — 1 to 32 lowercase letters, digits and dashes, a dash neither first nor last",
				EnvChatProviders, i+1)
			continue
		case seen[id]:
			l.fail("%s names %q twice", EnvChatProviders, id)
			continue
		}
		seen[id] = true
		c.Providers = append(c.Providers, l.chatProvider(id))
	}
	l.chatLimits(c)
	cfg.Chat = c
}

// chatProvider reads the variables of one provider.
func (l *loader) chatProvider(id string) ChatProvider {
	p := ChatProvider{ID: id, Name: id}
	env := func(suffix string) string { return ChatEnv(id, suffix) }
	if v, ok := l.get(env(ChatSuffixName)); ok {
		if !validText(v, maxChatName) {
			l.fail("%s must be 1 to %d characters without control characters", env(ChatSuffixName), maxChatName)
		} else {
			p.Name = strings.TrimSpace(v)
		}
	}
	kind, ok := l.get(env(ChatSuffixKind))
	p.Kind = strings.ToLower(kind)
	switch {
	case !ok:
		l.fail("%s is required for the provider %s: %s or %s", env(ChatSuffixKind), id, ChatKindOpenAI, ChatKindAnthropic)
	case p.Kind != ChatKindOpenAI && p.Kind != ChatKindAnthropic:
		l.fail("%s: %q is not one of %s, %s", env(ChatSuffixKind), clip(kind, 32), ChatKindOpenAI, ChatKindAnthropic)
	}
	if p.URL, ok = l.get(env(ChatSuffixURL)); !ok {
		l.fail("%s is required for the provider %s", env(ChatSuffixURL), id)
	} else if err := checkChatURL(p.URL); err != "" {
		l.fail("%s %s", env(ChatSuffixURL), err)
	}
	p.URL = strings.TrimRight(p.URL, "/")
	// The key is read as it is: a trimmed key would be another key.
	if v, set := l.lookup(env(ChatSuffixAPIKey)); set && v != "" {
		p.APIKey = v
	} else if p.Kind == ChatKindAnthropic {
		l.fail("%s is required for the provider %s, which is %s", env(ChatSuffixAPIKey), id, ChatKindAnthropic)
	}
	if p.Model, ok = l.get(env(ChatSuffixModel)); !ok {
		l.fail("%s is required for the provider %s", env(ChatSuffixModel), id)
	} else if !validModel(p.Model) {
		l.fail("%s must be 1 to %d characters without spaces or control characters", env(ChatSuffixModel), maxChatModel)
	}
	return p
}

func (l *loader) chatLimits(c *Chat) {
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

// checkChatURL holds a provider's URL to https, or to http on a host of the
// operator's own network — localhost, a loopback or private address, a name
// without a domain or under a domain that never leaves a network
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

// validModel accepts a model name as providers write them — qwen/qwen3-30b-a3b-2507,
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

// validText accepts a name a person reads: 1 to n characters once trimmed,
// none of them a control or format character.
func validText(s string, n int) bool {
	s = strings.TrimSpace(s)
	if s == "" || len([]rune(s)) > n {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return false
		}
	}
	return true
}
