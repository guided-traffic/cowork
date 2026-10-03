package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

// The Argon2id parameters of a new password hash (docs/adr/0033 D4): OWASP's
// recommended minimum of 19 MiB of memory, two passes and one lane. They are
// recorded in every encoded hash, so raising them is an amendment that leaves
// the hashes already stored verifiable; docs/security/local-accounts.md names
// the values in force.
const (
	ArgonMemoryKiB   uint32 = 19 * 1024
	ArgonIterations  uint32 = 2
	ArgonParallelism uint8  = 1

	saltBytes = 16
	keyBytes  = 32
)

// MaxPasswordLength bounds a password in characters, so that no body makes the
// hash read more than a few kilobytes. A password is accepted from the
// configured minimum (COWORK_PASSWORD_MIN_LENGTH) up to this.
const MaxPasswordLength = 1024

// MinPasswordLengthFloor is the lowest minimum the configuration accepts
// (docs/adr/0033 D3): a configured value below it refuses the start.
const MinPasswordLengthFloor = 8

// hashSlots bounds the hashes computed at once. Each holds ArgonMemoryKiB of
// memory, and the backend runs in a container with a memory limit that
// uploads share: two at a time is ample for the logins the throttle admits
// and keeps a flood of attempts from holding more than twice 19 MiB.
const hashSlots = 2

var slots = make(chan struct{}, hashSlots)

// computations counts the Argon2id hashes computed since the process started.
var computations atomic.Int64

// Computations reports how many Argon2id hashes — to store or to verify — this
// process has computed. The integration tests read it to show that an unknown
// username costs the login what a known one does (docs/adr/0033 D6).
func Computations() int64 { return computations.Load() }

// ErrPasswordLength is returned by CheckPassword; its text names the bound and
// never the password.
var ErrPasswordLength = errors.New("password length")

// CheckPassword applies the whole password policy of docs/adr/0033 D3: length
// only, counted in characters, between the configured minimum and
// MaxPasswordLength; no character classes, no history. The error says which
// bound is broken and never carries the password.
func CheckPassword(password string, minLength int) error {
	n := utf8.RuneCountInString(password)
	switch {
	case n < minLength:
		return fmt.Errorf("%w: must be at least %d characters", ErrPasswordLength, minLength)
	case n > MaxPasswordLength:
		return fmt.Errorf("%w: must be at most %d characters", ErrPasswordLength, MaxPasswordLength)
	}
	return nil
}

// HashPassword returns the Argon2id hash of a password in the PHC string form,
// `$argon2id$v=19$m=19456,t=2,p=1$<salt>$<key>`, with a salt of its own.
func HashPassword(ctx context.Context, password string) (string, error) {
	salt := make([]byte, saltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	if err := acquire(ctx); err != nil {
		return "", err
	}
	defer release()
	computations.Add(1)
	key := argon2.IDKey([]byte(password), salt, ArgonIterations, ArgonMemoryKiB, ArgonParallelism, keyBytes)
	return encodeHash(salt, key, ArgonMemoryKiB, ArgonIterations, ArgonParallelism), nil
}

// VerifyPassword reports whether password hashes to encoded, with the
// parameters encoded records and in constant time. A malformed hash is an
// error; a wrong password is false and no error.
func VerifyPassword(ctx context.Context, password, encoded string) (bool, error) {
	p, err := parseHash(encoded)
	if err != nil {
		return false, err
	}
	if err := acquire(ctx); err != nil {
		return false, err
	}
	defer release()
	computations.Add(1)
	// #nosec G115 -- parseHash holds the key to 16 to 64 bytes
	key := argon2.IDKey([]byte(password), p.salt, p.iterations, p.memory, p.parallelism, uint32(len(p.key)))
	return subtle.ConstantTimeCompare(key, p.key) == 1, nil
}

var (
	dummyMu sync.Mutex
	dummy   string
)

// DummyHash is a hash of a random password under the current parameters. The
// login verifies a presented password against it when the username names no
// usable account, so that an unknown username costs what a known one does
// (docs/adr/0033 D6). The first call computes it; the server calls it at
// start, so no request pays for it.
func DummyHash(ctx context.Context) (string, error) {
	dummyMu.Lock()
	defer dummyMu.Unlock()
	if dummy != "" {
		return dummy, nil
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", fmt.Errorf("prepare the dummy hash: %w", err)
	}
	hash, err := HashPassword(ctx, base64.StdEncoding.EncodeToString(secret))
	if err != nil {
		return "", fmt.Errorf("prepare the dummy hash: %w", err)
	}
	dummy = hash
	return dummy, nil
}

func acquire(ctx context.Context) error {
	select {
	case slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func release() { <-slots }

type hashParams struct {
	memory, iterations uint32
	parallelism        uint8
	salt, key          []byte
}

func encodeHash(salt, key []byte, memory, iterations uint32, parallelism uint8) string {
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, memory, iterations, parallelism,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key))
}

var errMalformedHash = errors.New("malformed password hash")

// parseHash reads an encoded Argon2id hash. The bounds keep a damaged or
// foreign row from asking the server for more memory or time than any
// parameters this code ever wrote.
func parseHash(encoded string) (hashParams, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return hashParams{}, errMalformedHash
	}
	if v, ok := strings.CutPrefix(parts[2], "v="); !ok || v != strconv.Itoa(argon2.Version) {
		return hashParams{}, errMalformedHash
	}
	p, err := parseCost(parts[3])
	if err != nil {
		return hashParams{}, err
	}
	var err1, err2 error
	p.salt, err1 = base64.RawStdEncoding.DecodeString(parts[4])
	p.key, err2 = base64.RawStdEncoding.DecodeString(parts[5])
	if err1 != nil || err2 != nil || len(p.salt) < 8 || len(p.salt) > 64 || len(p.key) < 16 || len(p.key) > 64 {
		return hashParams{}, errMalformedHash
	}
	return p, nil
}

// parseCost reads the `m=…,t=…,p=…` field and holds it to the bounds.
func parseCost(field string) (hashParams, error) {
	var p hashParams
	for _, f := range strings.Split(field, ",") {
		name, value, ok := strings.Cut(f, "=")
		n, err := strconv.ParseUint(value, 10, 32)
		if !ok || err != nil {
			return hashParams{}, errMalformedHash
		}
		switch name {
		case "m":
			p.memory = uint32(n)
		case "t":
			p.iterations = uint32(n)
		case "p":
			if n > 255 {
				return hashParams{}, errMalformedHash
			}
			p.parallelism = uint8(n)
		default:
			return hashParams{}, errMalformedHash
		}
	}
	if p.memory < 8 || p.memory > 1<<20 || p.iterations < 1 || p.iterations > 64 || p.parallelism < 1 {
		return hashParams{}, errMalformedHash
	}
	return p, nil
}
