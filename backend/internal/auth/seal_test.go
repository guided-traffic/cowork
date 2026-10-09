package auth

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// docs/adr/0031 D1: what is sealed opens with the same server key, label and
// binding only, and two seals of one value differ.
func TestSealer(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	s := NewSealer(key, LabelRefreshToken)
	sealed := s.Seal([]byte("a refresh token"), []byte("session one"))
	assert.NotContains(t, string(sealed), "a refresh token")
	plain, err := s.Open(sealed, []byte("session one"))
	require.NoError(t, err)
	assert.Equal(t, "a refresh token", string(plain))
	assert.NotEqual(t, sealed, s.Seal([]byte("a refresh token"), []byte("session one")), "a random nonce each time")

	_, err = s.Open(sealed, []byte("session two"))
	assert.ErrorIs(t, err, ErrSealed, "bound to its session")
	_, err = NewSealer(key, LabelOIDCLogin).Open(sealed, []byte("session one"))
	assert.ErrorIs(t, err, ErrSealed, "another label is another key")
	_, err = NewSealer(bytes.Repeat([]byte{8}, 32), LabelRefreshToken).Open(sealed, []byte("session one"))
	assert.ErrorIs(t, err, ErrSealed, "another server key")
	altered := bytes.Clone(sealed)
	altered[len(altered)-1] ^= 1
	_, err = s.Open(altered, []byte("session one"))
	assert.ErrorIs(t, err, ErrSealed)
	_, err = s.Open([]byte("short"), nil)
	assert.ErrorIs(t, err, ErrSealed)
}
