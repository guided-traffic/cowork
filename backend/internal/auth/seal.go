package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"errors"
)

// The labels the server key is derived under for what is sealed with it: the
// state of a login through the identity provider, which its cookie carries, and
// a session's refresh token (docs/adr/0031 D1, docs/adr/0029 D1).
const (
	LabelOIDCLogin    = "cowork oidc login v1"
	LabelRefreshToken = "cowork oidc refresh token v1" // #nosec G101 -- an HKDF label, not a credential
)

// ErrSealed is what opening a sealed value fails with: another key, another
// binding, a value that was altered or is no sealed value at all.
var ErrSealed = errors.New("the sealed value does not open")

// Sealer seals with AES-256-GCM under a key derived from the server key by
// HKDF-SHA256 with a label of its own, so no two uses of the server key share a
// key. Each value gets a random nonce, which leads it.
type Sealer struct {
	aead cipher.AEAD
}

// NewSealer derives the key of a label from the server key.
func NewSealer(serverKey []byte, label string) Sealer {
	key, err := hkdf.Key(sha256.New, serverKey, nil, label, 32)
	if err != nil {
		panic(err) // only an impossible key length fails
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		panic(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		panic(err)
	}
	return Sealer{aead: aead}
}

// Seal encrypts plaintext and binds it to binding, which Open must be given
// again: a sealed refresh token opens for its own session only.
func (s Sealer) Seal(plaintext, binding []byte) []byte {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		panic(err) // crypto/rand does not fail
	}
	return s.aead.Seal(nonce, nonce, plaintext, binding)
}

// Open decrypts what Seal made with the same binding.
func (s Sealer) Open(sealed, binding []byte) ([]byte, error) {
	n := s.aead.NonceSize()
	if len(sealed) < n+s.aead.Overhead() {
		return nil, ErrSealed
	}
	plain, err := s.aead.Open(nil, sealed[:n], sealed[n:], binding)
	if err != nil {
		return nil, ErrSealed
	}
	return plain, nil
}
