package auth

import (
	"crypto/sha256"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateTokenHasTheScannableShape(t *testing.T) {
	seen := map[string]bool{}
	for range 200 {
		plaintext, hash, err := GenerateToken()
		require.NoError(t, err)
		assert.True(t, WellFormedToken(plaintext), plaintext)
		assert.Len(t, plaintext, len(TokenPrefix)+tokenBodyLength)
		assert.Equal(t, sha256.Sum256([]byte(plaintext)), hash)
		assert.False(t, seen[plaintext], "a token repeated")
		seen[plaintext] = true
	}
}

func TestWellFormedToken(t *testing.T) {
	plaintext, _, err := GenerateToken()
	require.NoError(t, err)
	cases := map[string]bool{
		plaintext:                                true,
		"":                                       false,
		"cwk_":                                   false,
		"Bearer " + plaintext:                    false,
		plaintext + "x":                          false,
		"cwx_" + plaintext[len("cwk_"):]:         false,
		"cwk_" + "-" + plaintext[len("cwk_")+1:]: false,
	}
	for in, want := range cases {
		assert.Equal(t, want, WellFormedToken(in), "%q", in)
	}
}
