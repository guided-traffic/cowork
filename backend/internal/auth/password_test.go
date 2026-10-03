package auth

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/argon2"
)

// docs/adr/0033 D4: Argon2id, its parameters recorded in the encoded hash — at
// least OWASP's 19 MiB, two iterations and one lane — and a salt of its own.
func TestHashPasswordRecordsItsParametersAndSalts(t *testing.T) {
	ctx := context.Background()
	a, err := HashPassword(ctx, "correct horse battery staple")
	require.NoError(t, err)
	b, err := HashPassword(ctx, "correct horse battery staple")
	require.NoError(t, err)

	assert.True(t, strings.HasPrefix(a, "$argon2id$v=19$m=19456,t=2,p=1$"), a)
	assert.NotEqual(t, a, b, "every hash has a salt of its own")
	assert.NotContains(t, a, "correct horse", "the password is in no hash")
	assert.GreaterOrEqual(t, ArgonMemoryKiB, uint32(19*1024))
	assert.GreaterOrEqual(t, ArgonIterations, uint32(2))
	assert.GreaterOrEqual(t, ArgonParallelism, uint8(1))
}

func TestVerifyPassword(t *testing.T) {
	ctx := context.Background()
	hash, err := HashPassword(ctx, "correct horse battery staple")
	require.NoError(t, err)

	for name, c := range map[string]struct {
		password string
		want     bool
	}{
		"the password":    {"correct horse battery staple", true},
		"another":         {"correct horse battery stapl", false},
		"empty":           {"", false},
		"another case":    {"Correct horse battery staple", false},
		"with a trailing": {"correct horse battery staple ", false},
	} {
		t.Run(name, func(t *testing.T) {
			ok, err := VerifyPassword(ctx, c.password, hash)
			require.NoError(t, err)
			assert.Equal(t, c.want, ok)
		})
	}
}

// A hash is verified with the parameters it records, so raising the current
// ones leaves the stored hashes working (docs/adr/0033 D4: raised by amendment).
func TestVerifyPasswordUsesTheRecordedParameters(t *testing.T) {
	salt := []byte("0123456789abcdef")
	key := argon2.IDKey([]byte("old password value"), salt, 1, 8*1024, 1, keyBytes)
	encoded := encodeHash(salt, key, 8*1024, 1, 1)
	ok, err := VerifyPassword(context.Background(), "old password value", encoded)
	require.NoError(t, err)
	assert.True(t, ok)
	ok, err = VerifyPassword(context.Background(), "another value here", encoded)
	require.NoError(t, err)
	assert.False(t, ok)
}

// A damaged or foreign row never makes the server compute with a size it did
// not write itself.
func TestVerifyPasswordRefusesMalformedHashes(t *testing.T) {
	good, err := HashPassword(context.Background(), "correct horse battery staple")
	require.NoError(t, err)
	parts := strings.Split(good, "$")
	with := func(i int, v string) string {
		p := append([]string(nil), parts...)
		p[i] = v
		return strings.Join(p, "$")
	}
	for name, encoded := range map[string]string{
		"empty":             "",
		"plaintext":         "correct horse battery staple",
		"another algorithm": with(1, "argon2i"),
		"another version":   with(2, "v=16"),
		"no memory":         with(3, "t=2,p=1"),
		"huge memory":       with(3, "m=4294967295,t=2,p=1"),
		"tiny memory":       with(3, "m=1,t=2,p=1"),
		"huge iterations":   with(3, "m=19456,t=1000,p=1"),
		"no lanes":          with(3, "m=19456,t=2,p=0"),
		"too many lanes":    with(3, "m=19456,t=2,p=256"),
		"unknown field":     with(3, "m=19456,t=2,p=1,x=1"),
		"bad salt":          with(4, "***"),
		"short salt":        with(4, "AAAA"),
		"bad key":           with(5, "***"),
		"short key":         with(5, "AAAA"),
		"too few parts":     strings.Join(parts[:5], "$"),
	} {
		t.Run(name, func(t *testing.T) {
			ok, err := VerifyPassword(context.Background(), "correct horse battery staple", encoded)
			require.Error(t, err)
			assert.False(t, ok)
			assert.NotContains(t, err.Error(), "correct horse", "the error never carries the password")
		})
	}
}

func TestVerifyPasswordHonoursItsContext(t *testing.T) {
	good, err := HashPassword(context.Background(), "correct horse battery staple")
	require.NoError(t, err)
	// Occupy every slot, then ask with a context that ends: no hash is computed.
	for range hashSlots {
		slots <- struct{}{}
	}
	t.Cleanup(func() {
		for range hashSlots {
			<-slots
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = VerifyPassword(ctx, "x", good)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	_, err = HashPassword(ctx, "x")
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestDummyHashIsStableAndVerifiable(t *testing.T) {
	ctx := context.Background()
	a, err := DummyHash(ctx)
	require.NoError(t, err)
	b, err := DummyHash(ctx)
	require.NoError(t, err)
	assert.Equal(t, a, b)
	assert.True(t, strings.HasPrefix(a, "$argon2id$v=19$m=19456,t=2,p=1$"), "the dummy costs what a real hash costs")
	ok, err := VerifyPassword(ctx, "anything a person types", a)
	require.NoError(t, err)
	assert.False(t, ok)
}

// docs/adr/0033 D3: the policy is length only, counted in characters.
func TestCheckPassword(t *testing.T) {
	for name, c := range map[string]struct {
		password string
		min      int
		ok       bool
	}{
		"at the minimum":       {"abcdefghijkl", 12, true},
		"below":                {"abcdefghijk", 12, false},
		"characters, no bytes": {"äöüäöüäöüäöü", 12, true},
		"no character class":   {"aaaaaaaaaaaa", 12, true},
		"the floor":            {"abcdefgh", 8, true},
		"at the maximum":       {strings.Repeat("a", MaxPasswordLength), 12, true},
		"above the maximum":    {strings.Repeat("a", MaxPasswordLength+1), 12, false},
	} {
		t.Run(name, func(t *testing.T) {
			err := CheckPassword(c.password, c.min)
			if c.ok {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, ErrPasswordLength)
			assert.NotContains(t, err.Error(), c.password, "the error names the bound, never the password")
		})
	}
}
