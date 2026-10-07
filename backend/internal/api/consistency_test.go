package api

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/store"
	"github.com/guided-traffic/cowork/backend/internal/store/readq"
	"github.com/guided-traffic/cowork/backend/internal/store/writeq"
)

// docs/adr/0059 D4: a removal confirms the listed orphans; the ones a list cut
// at its bound does not show stay counted, so the counts — and the alert on
// them — do not end before the next check lists them.
func TestARemovalKeepsTheOrphansTheListDidNotShow(t *testing.T) {
	listed := []store.OrphanedObject{{Key: "t/a", Size: 10}, {Key: "t/b", Size: 20}}
	n, bytes := unlistedOrphans(writeq.GetConsistencyCheckForUpdateRow{Orphans: 5, OrphanBytes: 100}, listed)
	assert.Equal(t, int32(3), n)
	assert.Equal(t, int64(70), bytes)

	n, bytes = unlistedOrphans(writeq.GetConsistencyCheckForUpdateRow{Orphans: 2, OrphanBytes: 30}, listed)
	assert.Zero(t, n, "a list that shows every orphan leaves none")
	assert.Zero(t, bytes)
}

// The stored result as the API answers it: the lists decoded, the removal with
// its person, and null where no removal was confirmed.
func TestAResultIsAnsweredAsStored(t *testing.T) {
	id, file, by := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	at := time.Date(2026, 10, 6, 3, 12, 0, 0, time.UTC)
	dangling, err := json.Marshal([]store.DanglingAttachment{{ID: file, FileName: "lost.png", Size: 3,
		ContentType: "image/png", Ticket: "acme/COW-7", UploadedAt: at, Accepted: true}})
	require.NoError(t, err)
	orphans, err := json.Marshal([]store.OrphanedObject{{Key: "k", Size: 9, LastModified: at}})
	require.NoError(t, err)
	row := readq.GetAttachmentConsistencyRow{ID: id, CheckedAt: at, Dangling: 0, Accepted: 1, Orphans: 1, OrphanBytes: 9,
		DanglingItems: dangling, OrphanItems: orphans}

	out, err := consistencyView(row)
	require.NoError(t, err)
	assert.Equal(t, id, out.CheckId.MustGet())
	require.Len(t, out.DanglingAttachments, 1)
	assert.Equal(t, "lost.png", out.DanglingAttachments[0].FileName)
	assert.True(t, out.DanglingAttachments[0].Accepted)
	require.Len(t, out.OrphanedObjects, 1)
	assert.Equal(t, int64(9), out.OrphanedObjects[0].Size)
	assert.True(t, out.OrphanRemoval.IsNull())

	removed, kept, name := int32(2), int32(1), "Ada"
	row.OrphansRemovedAt, row.OrphansRemovedBy, row.OrphansRemovedByName = &at, &by, &name
	row.OrphansRemoved, row.OrphansKept = &removed, &kept
	out, err = consistencyView(row)
	require.NoError(t, err)
	removal := out.OrphanRemoval.MustGet()
	assert.Equal(t, 2, removal.Removed)
	assert.Equal(t, 1, removal.Kept)
	assert.Equal(t, by, removal.RemovedBy.Id)
	assert.Equal(t, "Ada", removal.RemovedBy.DisplayName)

	row.DanglingItems = []byte("not json")
	_, err = consistencyView(row)
	assert.Error(t, err, "a list that does not decode is an error, never an empty list")
}
