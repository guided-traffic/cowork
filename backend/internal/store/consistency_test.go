package store

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/storage"
)

// v7At is a UUIDv7 made at t, as the backend makes an attachment's id.
func v7At(t time.Time) uuid.UUID {
	id := uuid.Must(uuid.NewV7())
	ms := uint64(t.UnixMilli()) // #nosec G115 -- a time after 1970
	for i := range 6 {
		id[i] = byte(ms >> (40 - 8*i))
	}
	return id
}

// docs/adr/0059 D6, made concrete: the check is due when no tenant has a
// result, or when the last run lies before the latest 03:00 UTC — daily in
// the hour after it, and at a start that finds the last run older than that.
func TestTheConsistencyCheckIsDueOnceADayFromItsHour(t *testing.T) {
	day := func(d, h, m int) time.Time { return time.Date(2026, 10, d, h, m, 0, 0, time.UTC) }
	for _, c := range []struct {
		name    string
		last    time.Time
		checked bool
		now     time.Time
		due     bool
	}{
		{"never checked", time.Unix(0, 0), false, day(6, 10, 0), true},
		{"checked after today's hour", day(6, 3, 5), true, day(6, 10, 0), false},
		{"the hour comes", day(6, 3, 5), true, day(7, 3, 0), true},
		{"the hour has not come yet", day(6, 3, 5), true, day(7, 2, 59), false},
		{"a start after a missed hour", day(5, 9, 0), true, day(6, 10, 0), true},
		{"a start a day later", day(4, 3, 30), true, day(6, 1, 0), true},
		{"checked at the hour itself", day(6, 3, 0), true, day(6, 23, 59), false},
		{"a clock in another zone", day(6, 3, 5), true, day(6, 10, 0).In(time.FixedZone("UTC+14", 14*3600)), false},
	} {
		assert.Equal(t, c.due, ConsistencyCheckDue(c.last, c.checked, c.now), c.name)
	}
}

// docs/adr/0059 D4: an object no row names is an orphan unless it may be an
// upload in flight — its id made within the grace, or, under a key the backend
// does not write, its last change; an object under any other key than the
// backend's <tenant-id>/<id> is one whatever its name says. An attachment whose
// object the listing did not show is asked for.
func TestTheListingAndTheRowsAreJudged(t *testing.T) {
	now := time.Date(2026, 10, 6, 3, 30, 0, 0, time.UTC)
	tenant := uuid.Must(uuid.NewV7())
	old, fresh := now.Add(-2*OrphanGrace), now.Add(-OrphanGrace/2)
	whole, gone, putLate := v7At(old), v7At(old), v7At(old)
	orphan, inFlight, restored := v7At(old), v7At(fresh), v7At(old)
	upper := v7At(old)
	listed := []storage.Object{
		{Key: storage.Key(tenant, whole), Size: 1, LastModified: old},
		{Key: storage.Key(tenant, orphan), Size: 10, LastModified: old},
		{Key: storage.Key(tenant, inFlight), Size: 20, LastModified: fresh},
		// A restore rewrites the object, which keeps its key and with it the
		// time its id was made.
		{Key: storage.Key(tenant, restored), Size: 30, LastModified: fresh},
		{Key: tenant.String() + "/stray.bin", Size: 40, LastModified: old},
		{Key: tenant.String() + "/copying.bin", Size: 50, LastModified: fresh},
		{Key: tenant.String() + "/" + uuidUpper(upper), Size: 60, LastModified: old},
	}
	j := judge(tenant, listed, []uuid.UUID{whole, gone, putLate}, now)

	keys := make([]string, 0, len(j.orphans))
	for _, o := range j.orphans {
		keys = append(keys, o.Key)
	}
	assert.ElementsMatch(t, []string{storage.Key(tenant, orphan), storage.Key(tenant, restored),
		tenant.String() + "/stray.bin", tenant.String() + "/" + uuidUpper(upper)}, keys)
	assert.IsIncreasing(t, keys, "the orphans are in key order")
	assert.Equal(t, []uuid.UUID{gone, putLate}, j.unlisted, "the attachments the listing did not show")
}

func uuidUpper(id uuid.UUID) string {
	b := []byte(id.String())
	for i, c := range b {
		if c >= 'a' && c <= 'f' {
			b[i] = c - 'a' + 'A'
		}
	}
	return string(b)
}

// fakeObjects answers whether an object exists from a set, or an error.
type fakeObjects struct {
	exists map[string]bool
	err    error
}

func (f fakeObjects) List(context.Context, string) ([]storage.Object, error) { return nil, f.err }

func (f fakeObjects) Exists(_ context.Context, key string) (bool, error) {
	return f.exists[key], f.err
}

// An attachment the listing missed is missing only when the bucket says so
// when asked for it: an upload's object put after the listing passed its key
// is no missing object.
func TestAnUnlistedAttachmentIsAskedFor(t *testing.T) {
	tenant := uuid.Must(uuid.NewV7())
	late, gone := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	missing, err := confirmMissing(context.Background(), fakeObjects{exists: map[string]bool{storage.Key(tenant, late): true}},
		tenant, []uuid.UUID{late, gone})
	require.NoError(t, err)
	assert.Equal(t, []uuid.UUID{gone}, missing)

	_, err = confirmMissing(context.Background(), fakeObjects{err: errors.New("access denied")}, tenant, []uuid.UUID{gone})
	assert.Error(t, err, "a bucket that does not answer fails the run")
}

// The list shows the files nobody accepted as lost first.
func TestTheMissingFilesNobodyAcceptedComeFirst(t *testing.T) {
	a, b, c := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	order, lost := danglingOrder([]uuid.UUID{a, b, c}, setOf([]uuid.UUID{a, uuid.Must(uuid.NewV7())}))
	assert.Equal(t, []uuid.UUID{b, c, a}, order)
	assert.Equal(t, 1, lost)
}

// docs/adr/0059 D4: the run's summary counts, never names: no file name and
// no key, a tenant by its id, and only the tenants that are out of step.
func TestTheSummaryCountsAndNamesNoFile(t *testing.T) {
	quiet, loud := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	summary := summaryOf([]TenantConsistency{
		{TenantID: quiet, Slug: "quiet"},
		{TenantID: loud, Slug: "loud", Dangling: 2, Accepted: 1, Orphans: 3, OrphanBytes: 300},
	})
	assert.Equal(t, map[string]any{"tenants": 2, "dangling": 2, "accepted": 1, "orphans": 3, "orphan_bytes": int64(300),
		"found": map[string]map[string]int{loud.String(): {"dangling": 2, "accepted": 1, "orphans": 3}}}, summary)
}

func TestACountFitsItsColumn(t *testing.T) {
	assert.Equal(t, int32(7), CountColumn(7))
	assert.Equal(t, int32(0), CountColumn(-1))
	assert.Equal(t, int32(math.MaxInt32), CountColumn(math.MaxInt32+1))
}

// The lists keep at most ConsistencyListBound entries; the count is the whole.
func TestTheOrphanListIsBounded(t *testing.T) {
	orphans := make([]storage.Object, ConsistencyListBound+5)
	assert.Len(t, orphanItems(orphans), ConsistencyListBound)
}
