// Package auth resolves who a request acts for: the bearer token, the agent
// mark, and the authorization of an act (docs/adr/0035, 0036, 0043).
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"math/big"
	"regexp"
	"strings"
)

// TokenPrefix marks a cowork personal access token for secret scanners and
// push protection (docs/adr/0035 D1).
const TokenPrefix = "cwk_"

// tokenSecretBytes is the token's entropy: 32 random bytes.
const tokenSecretBytes = 32

// tokenBodyLength is the length of the base62 body. 32 bytes need at most 43
// base62 digits; the body is left-padded with '0' to exactly 43, so the whole
// token has one length and one pattern a scanner can match.
const tokenBodyLength = 43

var tokenPattern = regexp.MustCompile(`^cwk_[0-9A-Za-z]{43}$`)

// GenerateToken returns a new token and the SHA-256 that is all the database
// stores of it. The plaintext is shown once, at creation (docs/adr/0035 D1).
func GenerateToken() (plaintext string, hash [sha256.Size]byte, err error) {
	secret := make([]byte, tokenSecretBytes)
	if _, err := rand.Read(secret); err != nil {
		return "", hash, fmt.Errorf("read random bytes: %w", err)
	}
	// big.Int's base 62 uses 0-9, a-z, A-Z.
	body := new(big.Int).SetBytes(secret).Text(62)
	plaintext = TokenPrefix + strings.Repeat("0", tokenBodyLength-len(body)) + body
	return plaintext, HashToken(plaintext), nil
}

// HashToken returns the SHA-256 of a presented token, the value the tokens
// table is searched by.
func HashToken(plaintext string) [sha256.Size]byte {
	return sha256.Sum256([]byte(plaintext))
}

// WellFormedToken reports whether s has the shape of a cowork token; a request
// with anything else is refused before the database is asked.
func WellFormedToken(s string) bool {
	return tokenPattern.MatchString(s)
}
