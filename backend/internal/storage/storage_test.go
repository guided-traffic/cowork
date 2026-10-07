package storage

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

// A key names an attachment only in the form Key writes it, under the
// tenant's own prefix: the consistency check judges every other object under
// the prefix an orphan, and a removal never reaches past the prefix.
func TestParseKeyIsKeysInverse(t *testing.T) {
	tenant, other, id := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())

	got, ok := ParseKey(tenant, Key(tenant, id))
	assert.True(t, ok)
	assert.Equal(t, id, got)

	for _, key := range []string{
		Key(other, id),
		tenant.String() + "/" + strings.ToUpper(id.String()),
		tenant.String() + "/stray.bin",
		tenant.String() + "/" + id.String() + "/nested",
		tenant.String() + "/",
		tenant.String() + id.String(),
		"",
	} {
		_, ok := ParseKey(tenant, key)
		assert.False(t, ok, key)
	}
}
