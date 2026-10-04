package config

import (
	"maps"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testChatKey = "sk-a-provider-key-that-must-never-be-echoed"

func chatEnv(extra map[string]string) map[string]string {
	env := map[string]string{
		EnvDatabaseURL:  dbURL,
		EnvChatProvider: "openai",
		EnvChatURL:      "http://localhost:1234/v1/",
		EnvChatModel:    "qwen/qwen3.6-35b-a3b",
	}
	maps.Copy(env, extra)
	return env
}

// docs/adr/0076: no provider, no chat; a provider with its URL and model has
// the defaults of the limits and is outside the trust boundary until the
// operator says otherwise.
func TestLoadChatDefaults(t *testing.T) {
	cfg, err := Load(envOf(map[string]string{EnvDatabaseURL: dbURL}))
	require.NoError(t, err)
	assert.Nil(t, cfg.Chat)

	cfg, err = Load(envOf(chatEnv(nil)))
	require.NoError(t, err)
	require.NotNil(t, cfg.Chat)
	c := cfg.Chat
	assert.Equal(t, ChatProviderOpenAI, c.Provider)
	assert.Equal(t, "http://localhost:1234/v1", c.URL, "the trailing slash is dropped")
	assert.Empty(t, c.APIKey, "LM Studio takes none")
	assert.Equal(t, "qwen/qwen3.6-35b-a3b", c.Model)
	assert.False(t, c.Inside)
	assert.Equal(t, 5*time.Minute, c.TurnTimeout)
	assert.Equal(t, 8, c.MaxSteps)
	assert.Equal(t, 2, c.Turns)
	assert.Equal(t, "openai localhost:1234 qwen/qwen3.6-35b-a3b", c.Fingerprint(), "the format, the host, the model — no key")
}

func TestLoadChatOverrides(t *testing.T) {
	cfg, err := Load(envOf(chatEnv(map[string]string{
		EnvChatProvider:    "Anthropic",
		EnvChatURL:         "https://api.anthropic.com",
		EnvChatAPIKey:      testChatKey,
		EnvChatModel:       "claude-sonnet-4-5",
		EnvChatInside:      "true",
		EnvChatTurnTimeout: "0",
		EnvChatMaxSteps:    "0",
		EnvChatTurns:       "0",
	})))
	require.NoError(t, err)
	c := cfg.Chat
	assert.Equal(t, ChatProviderAnthropic, c.Provider)
	assert.Equal(t, testChatKey, c.APIKey)
	assert.True(t, c.Inside)
	assert.Zero(t, c.TurnTimeout, "0 switches the limit off")
	assert.Zero(t, c.MaxSteps, "0 switches the limit off")
	assert.Zero(t, c.Turns, "0 switches the limit off")
	assert.Equal(t, "anthropic api.anthropic.com claude-sonnet-4-5", c.Fingerprint())
	assert.NotContains(t, c.Fingerprint(), testChatKey)
}

// Plain http only inside the operator's network: the key and the tenants'
// data travel in every request.
func TestLoadChatURL(t *testing.T) {
	for _, ok := range []string{"https://api.openai.com/v1", "http://localhost:1234/v1", "http://127.0.0.1:1234/v1",
		"http://[::1]:11434/v1", "http://10.0.0.5:1234/v1", "http://192.168.1.20:1234/v1", "http://172.16.4.2/v1",
		"http://lmstudio:1234/v1", "http://ollama.ai.svc:11434/v1", "http://vllm.llm.svc.cluster.local/v1",
		"http://gpu.home.arpa/v1", "http://model.internal/v1"} {
		_, err := Load(envOf(chatEnv(map[string]string{EnvChatURL: ok})))
		assert.NoError(t, err, ok)
	}
	for _, bad := range []string{"http://api.openai.com/v1", "http://8.8.8.8/v1", "http://169.254.169.254/latest",
		"ftp://localhost/v1", "models:1234", "https://user:pass@api.openai.com/v1", "https://api.openai.com/v1?x=1"} {
		_, err := Load(envOf(chatEnv(map[string]string{EnvChatURL: bad})))
		require.Error(t, err, bad)
		assert.Contains(t, err.Error(), EnvChatURL, bad)
		assert.NotContains(t, err.Error(), bad, "the URL is not quoted")
	}
}

// What a chat needs and what it refuses, each named by its variable; the key
// is never echoed.
func TestLoadChatRefusals(t *testing.T) {
	for name, c := range map[string]struct {
		env  map[string]string
		want string
	}{
		"an unknown provider":  {chatEnv(map[string]string{EnvChatProvider: "ollama"}), EnvChatProvider},
		"no URL":               {chatEnv(map[string]string{EnvChatURL: ""}), EnvChatURL + " is required"},
		"no model":             {chatEnv(map[string]string{EnvChatModel: " "}), EnvChatModel + " is required"},
		"a model with a space": {chatEnv(map[string]string{EnvChatModel: "my model"}), EnvChatModel},
		"anthropic without a key": {chatEnv(map[string]string{EnvChatProvider: "anthropic", EnvChatURL: "https://api.anthropic.com"}),
			EnvChatAPIKey + " is required"},
		"a negative timeout":      {chatEnv(map[string]string{EnvChatTurnTimeout: "-1s"}), EnvChatTurnTimeout},
		"steps that are no count": {chatEnv(map[string]string{EnvChatMaxSteps: "many"}), EnvChatMaxSteps},
		"turns that are no count": {chatEnv(map[string]string{EnvChatTurns: "-1"}), EnvChatTurns},
		"inside that is no boolean": {chatEnv(map[string]string{EnvChatInside: "yes please", EnvChatAPIKey: testChatKey}),
			EnvChatInside},
	} {
		_, err := Load(envOf(c.env))
		require.Error(t, err, name)
		assert.Contains(t, err.Error(), c.want, name)
		assert.NotContains(t, err.Error(), testChatKey, name)
	}
}

// Every variable of the chat without the provider is a mistake the start
// names, so a half-done configuration does not pass for none.
func TestLoadChatVariablesNeedTheProvider(t *testing.T) {
	for _, env := range chatVariables {
		_, err := Load(envOf(map[string]string{EnvDatabaseURL: dbURL, env: "x"}))
		require.Error(t, err, env)
		assert.Contains(t, err.Error(), env+" is set without "+EnvChatProvider)
	}
}
