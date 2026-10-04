package config

import (
	"maps"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testChatKey = "sk-a-provider-key-that-must-never-be-echoed"

// chatEnv is one provider, LM Studio on the operator's machine.
func chatEnv(extra map[string]string) map[string]string {
	env := map[string]string{
		EnvDatabaseURL:               dbURL,
		EnvChatProviders:             "lmstudio",
		"COWORK_CHAT_LMSTUDIO_KIND":  "openai",
		"COWORK_CHAT_LMSTUDIO_URL":   "http://localhost:1234/v1/",
		"COWORK_CHAT_LMSTUDIO_MODEL": "qwen/qwen3-30b-a3b-2507",
	}
	maps.Copy(env, extra)
	return env
}

// docs/adr/0076: no provider, no chat; a provider with its kind, URL and
// model has its id for a name, and the limits their defaults.
func TestLoadChatDefaults(t *testing.T) {
	cfg, err := Load(envOf(map[string]string{EnvDatabaseURL: dbURL}))
	require.NoError(t, err)
	assert.Nil(t, cfg.Chat)

	cfg, err = Load(envOf(chatEnv(nil)))
	require.NoError(t, err)
	require.NotNil(t, cfg.Chat)
	c := cfg.Chat
	require.Len(t, c.Providers, 1)
	assert.Equal(t, ChatProvider{ID: "lmstudio", Name: "lmstudio", Kind: ChatKindOpenAI, URL: "http://localhost:1234/v1",
		Model: "qwen/qwen3-30b-a3b-2507"}, c.Providers[0], "the trailing slash is dropped, LM Studio takes no key")
	assert.Equal(t, 5*time.Minute, c.TurnTimeout)
	assert.Equal(t, 8, c.MaxSteps)
	assert.Equal(t, 2, c.Turns)
}

// docs/adr/0076: a list of providers in its order, each with its variables —
// the id upper-cased with dashes as underscores.
func TestLoadChatProviders(t *testing.T) {
	assert.Equal(t, "COWORK_CHAT_LM_STUDIO_URL", ChatEnv("lm-studio", ChatSuffixURL))
	cfg, err := Load(envOf(chatEnv(map[string]string{
		EnvChatProviders:                  " lmstudio , claude-work ",
		"COWORK_CHAT_LMSTUDIO_NAME":       " LM Studio ",
		"COWORK_CHAT_CLAUDE_WORK_NAME":    "Claude",
		"COWORK_CHAT_CLAUDE_WORK_KIND":    "Anthropic",
		"COWORK_CHAT_CLAUDE_WORK_URL":     "https://api.anthropic.com",
		"COWORK_CHAT_CLAUDE_WORK_API_KEY": testChatKey,
		"COWORK_CHAT_CLAUDE_WORK_MODEL":   "claude-sonnet-4-5",
		EnvChatTurnTimeout:                "0",
		EnvChatMaxSteps:                   "0",
		EnvChatTurns:                      "0",
	})))
	require.NoError(t, err)
	c := cfg.Chat
	require.Len(t, c.Providers, 2)
	assert.Equal(t, "lmstudio", c.Providers[0].ID, "the configured order: the first is the default")
	assert.Equal(t, "LM Studio", c.Providers[0].Name)
	assert.Equal(t, ChatProvider{ID: "claude-work", Name: "Claude", Kind: ChatKindAnthropic, URL: "https://api.anthropic.com",
		APIKey: testChatKey, Model: "claude-sonnet-4-5"}, c.Providers[1])
	assert.Zero(t, c.TurnTimeout, "0 switches the limit off")
	assert.Zero(t, c.MaxSteps, "0 switches the limit off")
	assert.Zero(t, c.Turns, "0 switches the limit off")
}

// Plain http only inside the operator's network: the key and the tenants'
// data travel in every request.
func TestLoadChatURL(t *testing.T) {
	const env = "COWORK_CHAT_LMSTUDIO_URL"
	for _, ok := range []string{"https://api.openai.com/v1", "http://localhost:1234/v1", "http://127.0.0.1:1234/v1",
		"http://[::1]:11434/v1", "http://10.0.0.5:1234/v1", "http://192.168.1.20:1234/v1", "http://172.16.4.2/v1",
		"http://lmstudio:1234/v1", "http://ollama.ai.svc:11434/v1", "http://vllm.llm.svc.cluster.local/v1",
		"http://gpu.home.arpa/v1", "http://model.internal/v1"} {
		_, err := Load(envOf(chatEnv(map[string]string{env: ok})))
		assert.NoError(t, err, ok)
	}
	for _, bad := range []string{"http://api.openai.com/v1", "http://8.8.8.8/v1", "http://169.254.169.254/latest",
		"ftp://localhost/v1", "models:1234", "https://user:pass@api.openai.com/v1", "https://api.openai.com/v1?x=1"} {
		_, err := Load(envOf(chatEnv(map[string]string{env: bad})))
		require.Error(t, err, bad)
		assert.Contains(t, err.Error(), env, bad)
		assert.NotContains(t, err.Error(), bad, "the URL is not quoted")
	}
}

// What a provider needs and what the chat refuses, each named by its
// variable; a key and a URL are never echoed.
func TestLoadChatRefusals(t *testing.T) {
	const prefix = "COWORK_CHAT_LMSTUDIO_"
	for name, c := range map[string]struct {
		env  map[string]string
		want string
	}{
		"no kind":               {chatEnv(map[string]string{prefix + "KIND": ""}), prefix + "KIND is required"},
		"an unknown kind":       {chatEnv(map[string]string{prefix + "KIND": "ollama"}), prefix + "KIND"},
		"no URL":                {chatEnv(map[string]string{prefix + "URL": ""}), prefix + "URL is required"},
		"no model":              {chatEnv(map[string]string{prefix + "MODEL": " "}), prefix + "MODEL is required"},
		"a model with a space":  {chatEnv(map[string]string{prefix + "MODEL": "my model"}), prefix + "MODEL"},
		"a name with a control": {chatEnv(map[string]string{prefix + "NAME": "LM\x1b[31mStudio"}), prefix + "NAME"},
		"anthropic without a key": {chatEnv(map[string]string{prefix + "KIND": "anthropic", prefix + "URL": "https://api.anthropic.com"}),
			prefix + "API_KEY is required"},
		"an id with a capital": {chatEnv(map[string]string{EnvChatProviders: "LMStudio"}), EnvChatProviders + ": entry 1"},
		"an id with an underscore": {chatEnv(map[string]string{EnvChatProviders: "lmstudio,lm_studio"}),
			EnvChatProviders + ": entry 2"},
		"a URL for an id": {chatEnv(map[string]string{EnvChatProviders: "https://" + testChatKey + "@api.openai.com"}),
			EnvChatProviders + ": entry 1"},
		"an empty entry":          {chatEnv(map[string]string{EnvChatProviders: "lmstudio,"}), EnvChatProviders + ": entry 2"},
		"an id twice":             {chatEnv(map[string]string{EnvChatProviders: "lmstudio,lmstudio"}), `names "lmstudio" twice`},
		"a negative timeout":      {chatEnv(map[string]string{EnvChatTurnTimeout: "-1s"}), EnvChatTurnTimeout},
		"steps that are no count": {chatEnv(map[string]string{EnvChatMaxSteps: "many"}), EnvChatMaxSteps},
		"turns that are no count": {chatEnv(map[string]string{EnvChatTurns: "-1"}), EnvChatTurns},
		"a second provider without its variables": {chatEnv(map[string]string{EnvChatProviders: "lmstudio,claude",
			"COWORK_CHAT_CLAUDE_API_KEY": testChatKey}), "COWORK_CHAT_CLAUDE_KIND is required"},
	} {
		_, err := Load(envOf(c.env))
		require.Error(t, err, name)
		assert.Contains(t, err.Error(), c.want, name)
		assert.NotContains(t, err.Error(), testChatKey, name)
	}
}

// A limit of the chat without a provider is a mistake the start names, so a
// half-done configuration does not pass for none.
func TestLoadChatVariablesNeedTheProvider(t *testing.T) {
	for _, env := range chatVariables {
		_, err := Load(envOf(map[string]string{EnvDatabaseURL: dbURL, env: "1"}))
		require.Error(t, err, env)
		assert.Contains(t, err.Error(), env+" is set without "+EnvChatProviders)
	}
}
