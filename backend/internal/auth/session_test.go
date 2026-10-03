package auth

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// docs/adr/0031 D1, D2: 256 random bits in the cookie, only their hash stored,
// and the name carries the __Host- prefix.
func TestGenerateSession(t *testing.T) {
	value, hash, err := GenerateSession()
	require.NoError(t, err)
	assert.Len(t, value, 43, "256 bits in base64url without padding")
	assert.True(t, WellFormedSession(value))
	assert.Equal(t, HashSession(value), hash)
	assert.NotContains(t, string(hash[:]), value)
	other, _, err := GenerateSession()
	require.NoError(t, err)
	assert.NotEqual(t, value, other, "a session value is never reused (D5)")
	assert.True(t, strings.HasPrefix(SessionCookie, "__Host-"))
}

func TestWellFormedSession(t *testing.T) {
	good := strings.Repeat("A", 43)
	assert.True(t, WellFormedSession(good))
	for name, s := range map[string]string{
		"empty":      "",
		"short":      good[:42],
		"long":       good + "A",
		"a token":    "cwk_" + strings.Repeat("A", 43),
		"padding":    good[:42] + "=",
		"a space":    good[:42] + " ",
		"an unicode": good[:42] + "ä",
	} {
		assert.False(t, WellFormedSession(s), name)
	}
}

func TestUsernames(t *testing.T) {
	for _, ok := range []string{"a", "ada", "ada.lovelace", "ada_l-1", "0abc", strings.Repeat("a", 63)} {
		assert.True(t, ValidUsername(ok), ok)
	}
	for _, bad := range []string{"", "Ada", "-ada", ".ada", "ada lovelace", "ada@host", strings.Repeat("a", 64), "äda"} {
		assert.False(t, ValidUsername(bad), bad)
	}
	assert.Equal(t, "ada", NormaliseUsername("  Ada  "), "trimmed and lower-cased")
	assert.Equal(t, "", NormaliseUsername("not a username"), "a value that cannot be one names no account")
	assert.Equal(t, "", NormaliseUsername(""))
}
