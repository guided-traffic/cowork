package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"regexp"
)

// SessionCookie is the name of the session cookie (docs/adr/0031 D2). The
// __Host- prefix makes the browser itself enforce what the record demands: the
// cookie is Secure, has Path=/ and no Domain, and cannot be set by a sibling
// subdomain.
const SessionCookie = "__Host-cowork-session"

// sessionSecretBytes is the cookie value's entropy: 256 random bits
// (docs/adr/0031 D1).
const sessionSecretBytes = 32

var sessionPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

// GenerateSession returns a new cookie value and the SHA-256 that is all the
// database stores of it. A session is created at login and never before, and
// its value is never reused (docs/adr/0031 D5).
func GenerateSession() (value string, hash [sha256.Size]byte, err error) {
	secret := make([]byte, sessionSecretBytes)
	if _, err := rand.Read(secret); err != nil {
		return "", hash, fmt.Errorf("read random bytes: %w", err)
	}
	value = base64.RawURLEncoding.EncodeToString(secret)
	return value, HashSession(value), nil
}

// HashSession returns the SHA-256 of a presented cookie value, the value the
// sessions table is searched by.
func HashSession(value string) [sha256.Size]byte {
	return sha256.Sum256([]byte(value))
}

// WellFormedSession reports whether s has the shape of a session cookie
// value; anything else is refused before the database is asked.
func WellFormedSession(s string) bool {
	return sessionPattern.MatchString(s)
}
