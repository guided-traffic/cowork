//go:build integration

package integration

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/storage"
	"github.com/guided-traffic/cowork/backend/internal/store"
	"github.com/guided-traffic/cowork/backend/internal/store/writeq"
)

// docs/adr/0059 D4: the consistency check compares the listing and the rows
// as two streams in one order — the store lists the keys in byte order, and
// PostgreSQL reads a tenant's attachments range by range in the order of
// their ids, which is the byte order of their keys, UUIDv7s and UUIDv4s alike.
func TestTheAttachmentsAreReadInRangesInTheOrderOfTheirKeys(t *testing.T) {
	ctx := context.Background()
	f := fixtures(t)
	person, err := f.Person(ctx, uniqueSlug("ranges"), "Ranges")
	require.NoError(t, err)
	tenant, err := f.Tenant(ctx, uniqueSlug("ranges"), "Ranges")
	require.NoError(t, err)
	project, err := f.Project(ctx, tenant, "RNG", "Ranges")
	require.NoError(t, err)
	ticket, _, err := f.Ticket(ctx, tenant, project, person, "Its files")
	require.NoError(t, err)
	ids := make([]uuid.UUID, 0, 400)
	for range 200 {
		ids = append(ids, uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewRandom()))
	}
	require.NoError(t, f.Exec(ctx, `INSERT INTO attachments (id, tenant_id, ticket_id, file_name, size, sha256, content_type, uploaded_by)
		SELECT u.id, $2, $3, 'f.png', 1, sha256('x'::bytea), 'image/png', $4 FROM unnest($1::uuid[]) AS u(id)`,
		ids, tenant, ticket, person))
	t.Cleanup(func() {
		assert.NoError(t, f.Exec(context.Background(), `DELETE FROM attachments WHERE tenant_id = $1`, tenant))
	})
	byKey := slices.SortedFunc(slices.Values(ids), func(a, b uuid.UUID) int {
		return strings.Compare(storage.Key(tenant, a), storage.Key(tenant, b))
	})

	read := func(after, upto uuid.UUID) []uuid.UUID {
		t.Helper()
		var got []uuid.UUID
		_, err := openRuntime(t).Mutate(as(person), tenant, func(w *store.Writer) error {
			var err error
			got, err = w.ListTenantAttachmentIDs(ctx, writeq.ListTenantAttachmentIDsParams{TenantID: tenant, After: after, Upto: upto})
			if err != nil {
				return err
			}
			return store.ErrNoChange
		})
		require.ErrorIs(t, err, store.ErrNoChange)
		return got
	}
	assert.Equal(t, byKey, read(uuid.Nil, uuid.Max), "every attachment, in the order of the keys")
	assert.Equal(t, byKey[11:250], read(byKey[10], byKey[249]), "above the one and up to the other")
	assert.Empty(t, read(byKey[len(byKey)-1], uuid.Max), "nothing above the last")
}

// The listing hands the keys on in byte order — the backend's keys, the same
// ids in capitals and keys it never writes alike, a deeper path among them,
// whose "/" sorts after "-" and "." — and a listing whose context ended ends
// with the error instead of looking complete. (The deeper path sits under a
// name that is no object itself: MinIO keeps both an object and one under its
// name as a prefix, and lists only the first while both exist.)
func TestTheListingHandsTheKeysOnInByteOrderAndEndsWithItsContext(t *testing.T) {
	ctx := context.Background()
	prefix := uuid.Must(uuid.NewV7()).String() + "/"
	keys := make([]string, 0, 6*8+5)
	for range 6 {
		v7, v4, deeper := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewRandom()), uuid.Must(uuid.NewRandom())
		keys = append(keys, prefix+v7.String(), prefix+v4.String(), prefix+strings.ToUpper(v4.String()),
			prefix+v4.String()+".bak", prefix+v4.String()+"-x",
			prefix+deeper.String()+"/nested", prefix+deeper.String()+".bak", prefix+deeper.String()+"-x")
	}
	keys = append(keys, prefix+"0", prefix+"stray.bin", prefix+"~", prefix+"a b", prefix+"ä")
	for _, key := range keys {
		putObject(t, key, []byte("x"))
	}

	var listed []string
	for o, err := range testStorage(t).List(ctx, prefix) {
		require.NoError(t, err)
		listed = append(listed, o.Key)
	}
	assert.Equal(t, slices.Sorted(slices.Values(keys)), listed)

	ended, cancel := context.WithCancel(ctx)
	cancel()
	var failed error
	for _, err := range testStorage(t).List(ended, prefix) {
		failed = err
	}
	assert.ErrorIs(t, failed, context.Canceled)
}
