package store

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"iter"
	"math/rand/v2"
	"runtime"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/storage"
)

// layout is a tenant's objects and attachments as a test lays them out.
type layout struct {
	tenant uuid.UUID
	listed []storage.Object
	rows   []uuid.UUID
}

func newLayout() *layout { return &layout{tenant: uuid.Must(uuid.NewV7())} }

// file lays out an attachment: its object, its row, or both.
func (l *layout) file(id uuid.UUID, object, row bool, modified time.Time) *layout {
	if object {
		l.listed = append(l.listed, storage.Object{Key: storage.Key(l.tenant, id), Size: int64(id[15]) + 1, LastModified: modified})
	}
	if row {
		l.rows = append(l.rows, id)
	}
	return l
}

// stray lays out an object under a name the backend does not write.
func (l *layout) stray(name string, modified time.Time) *layout {
	l.listed = append(l.listed, storage.Object{Key: l.tenant.String() + "/" + name, Size: int64(len(name)) + 1, LastModified: modified})
	return l
}

// inOrder is the layout as the store and the query hand it on: the listing
// in the byte order of its keys, the rows in the order of their ids.
func (l *layout) inOrder() layout {
	slices.SortFunc(l.listed, func(a, b storage.Object) int { return strings.Compare(a.Key, b.Key) })
	slices.SortFunc(l.rows, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
	return *l
}

// listingOf hands the objects on one at a time, in the order given, as a
// store's listing does.
func listingOf(objects []storage.Object) iter.Seq2[storage.Object, error] {
	return func(yield func(storage.Object, error) bool) {
		for _, o := range objects {
			if !yield(o, nil) {
				return
			}
		}
	}
}

// rowsOf reads the ids above after and up to upto of ids, which are in id
// order, as ListTenantAttachmentIDs does.
func rowsOf(ids []uuid.UUID) readRange {
	return func(_ context.Context, after, upto uuid.UUID) ([]uuid.UUID, error) {
		lo := sort.Search(len(ids), func(i int) bool { return bytes.Compare(ids[i][:], after[:]) > 0 })
		hi := sort.Search(len(ids), func(i int) bool { return bytes.Compare(ids[i][:], upto[:]) > 0 })
		if hi < lo {
			return nil, nil
		}
		return slices.Clone(ids[lo:hi]), nil
	}
}

// judgedAtOnce is judge as it was before the check compared batch by batch:
// a set of every row and one of every listed id, over the whole listing. The
// comparison finds what it found.
func judgedAtOnce(tenantID uuid.UUID, listed []storage.Object, rows []uuid.UUID, now time.Time) judgement {
	named := make(map[uuid.UUID]bool, len(rows))
	for _, id := range rows {
		named[id] = true
	}
	seen := make(map[uuid.UUID]bool, len(listed))
	var j judgement
	for _, o := range listed {
		id, ok := storage.ParseKey(tenantID, o.Key)
		switch {
		case ok && named[id]:
			seen[id] = true
		case young(id, ok, o, now):
		default:
			j.orphans = append(j.orphans, o)
		}
	}
	for _, id := range rows {
		if !seen[id] {
			j.unlisted = append(j.unlisted, id)
		}
	}
	slices.SortFunc(j.orphans, func(a, b storage.Object) int { return strings.Compare(a.Key, b.Key) })
	return j
}

// orNone is an empty list as nil, so that two lists compare by what they hold.
func orNone[T any](s []T) []T {
	if len(s) == 0 {
		return nil
	}
	return s
}

// assertFound says the comparison found what judgedAtOnce found: every
// orphan counted with its bytes, the first ConsistencyListBound kept in key
// order, every attachment the listing did not show in id order.
func assertFound(t *testing.T, want judgement, got comparison, msg string) {
	t.Helper()
	var sum int64
	for _, o := range want.orphans {
		sum += o.Size
	}
	assert.Equal(t, len(want.orphans), got.orphans, "orphans counted: %s", msg)
	assert.Equal(t, sum, got.orphanBytes, "orphan bytes: %s", msg)
	assert.Equal(t, orNone(want.orphans[:min(len(want.orphans), ConsistencyListBound)]), orNone(got.firstOrphans), "orphans kept: %s", msg)
	assert.Equal(t, orNone(want.unlisted), orNone(got.unlisted), "unlisted: %s", msg)
}

// docs/adr/0059 D4: the comparison takes the listing in the byte order of its
// keys and the rows in PostgreSQL's order of uuid, which compares the sixteen
// bytes (uuid_cmp, a memcmp; the integration tier reads the order back from
// the database). The two agree for every id: a key is the tenant's prefix and
// the id in lowercase hexadecimal with its hyphens in fixed places, and the
// digits sort before the letters a to f.
func TestTheKeysSortAsTheirIDs(t *testing.T) {
	tenant := uuid.Must(uuid.NewV7())
	ids := make([]uuid.UUID, 0, 2+2*10_000+4*32)
	ids = append(ids, uuid.Nil, uuid.Max)
	for range 10_000 {
		ids = append(ids, uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewRandom()))
	}
	// One nibble apart, at every place, on either side of the digits' end
	// and the letters' start.
	base := uuid.Must(uuid.NewRandom())
	for place := range 32 {
		for _, nibble := range []byte{0x0, 0x9, 0xa, 0xf} {
			id := base
			shift := 4 * (1 - place%2)
			id[place/2] = id[place/2]&^(0xf<<shift) | nibble<<shift
			ids = append(ids, id)
		}
	}
	byBytes := slices.SortedFunc(slices.Values(ids), func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
	byKey := slices.SortedFunc(slices.Values(ids), func(a, b uuid.UUID) int {
		return strings.Compare(storage.Key(tenant, a), storage.Key(tenant, b))
	})
	assert.Equal(t, byBytes, byKey)
	for i := 1; i < len(byBytes); i++ {
		a, b := byBytes[i-1], byBytes[i]
		require.Equal(t, bytes.Compare(a[:], b[:]), strings.Compare(a.String(), b.String()), "%s, %s", a, b)
	}
}

// layouts are the tenants the comparison is held to judgedAtOnce over: the
// keys of attachments with and without a row interleaved, runs of rows
// whose object is missing and of objects no row names longer than a batch
// and than the list's bound, keys the backend does not write — a run of them
// longer than a batch among them —, young objects, the ids at the ends of
// their order, and tenants with nothing listed, no rows or neither.
func layouts(now time.Time) map[string]layout {
	old, fresh := now.Add(-2*OrphanGrace), now.Add(-OrphanGrace/2)
	at := func(base time.Time, i int) uuid.UUID { return v7At(base.Add(time.Duration(i) * time.Millisecond)) }
	out := map[string]layout{}

	l := newLayout()
	for i := range 30 {
		l.file(at(old, i), i%3 != 1, i%3 != 2, old)
	}
	out["interleaved"] = l.inOrder()

	l = newLayout()
	for i := range 1206 {
		edge := i < 3 || i >= 1203
		l.file(at(old, i), edge, true, old)
	}
	out["a run of missing objects"] = l.inOrder()

	l = newLayout()
	for i := range 1206 {
		edge := i < 3 || i >= 1203
		l.file(at(old, i), true, edge, old)
	}
	out["a run of orphans past the list's bound"] = l.inOrder()

	l = newLayout()
	named := at(old, 1)
	l.file(at(old, 0), true, true, old).file(named, true, true, old).file(at(old, 2), true, false, old)
	for i := range 9 {
		l.stray(named.String()+".bak"+string(rune('0'+i)), old)
	}
	upper := at(old, 3)
	l.stray(strings.ToUpper(upper.String()), old).file(upper, false, true, old)
	urn := at(old, 4)
	l.stray("urn:uuid:"+urn.String(), old).stray("{"+urn.String()+"}", old).stray(strings.ReplaceAll(urn.String(), "-", ""), old)
	l.file(urn, false, true, old)
	l.stray("", old).stray("0", old).stray("stray.bin", old).stray("zzz", old).stray(named.String()+"/nested", old)
	l.stray("copying.bin", fresh)
	out["keys the backend does not write"] = l.inOrder()

	l = newLayout()
	l.file(at(fresh, 0), true, false, fresh)                // an upload in flight
	l.file(at(old, 0), true, false, fresh)                  // a restore rewrote it: judged by its id
	l.file(uuid.Must(uuid.NewRandom()), true, false, fresh) // no UUIDv7: judged by its last change
	l.file(uuid.Must(uuid.NewRandom()), true, false, old)   // old by its last change
	l.file(at(fresh, 1), true, true, fresh).stray("new.bin", fresh)
	out["young objects"] = l.inOrder()

	l = newLayout()
	low := uuid.UUID{15: 1}
	l.file(low, true, true, old).file(uuid.Max, true, true, old).stray(uuid.Max.String()+".bak", old)
	out["the ids at the ends"] = l.inOrder()

	l = newLayout()
	for i := range 7 {
		l.file(at(old, i), false, true, old)
	}
	out["nothing listed"] = l.inOrder()

	l = newLayout()
	for i := range 7 {
		l.file(at(old, i), true, false, old)
	}
	out["no rows"] = l.inOrder()
	out["neither"] = newLayout().inOrder()
	return out
}

// randomLayout lays out runs of files of one kind each — with an object and
// a row, a row alone, an object alone, under a key the backend does not
// write — made at one time a run, some within the grace, some by UUIDv4.
func randomLayout(seed uint64, files int, now time.Time) layout {
	r := rand.New(rand.NewPCG(seed, seed)) // #nosec G404 -- a test's reproducible layout
	l := newLayout()
	for n := 0; n < files; {
		made := now.Add(-time.Duration(r.Int64N(int64(3 * OrphanGrace))))
		kind, run := r.IntN(5), 1+r.IntN(12)
		for i := range run {
			id := v7At(made)
			if r.IntN(8) == 0 {
				id = uuid.Must(uuid.NewRandom())
			}
			switch kind {
			case 0:
				l.file(id, true, true, made)
			case 1:
				l.file(id, false, true, made)
			case 2:
				l.file(id, true, false, made)
			case 3:
				l.stray(strings.ToUpper(id.String()), made)
			default:
				l.file(id, i%2 == 0, i%3 != 0, made)
			}
		}
		n += run
	}
	return l.inOrder()
}

// docs/adr/0059 D4, as amended 2026-10-07: the comparison holds one batch of
// the listing at a time and finds what the check found when it held the whole
// listing and every row — at every size of the batch, across its edges.
func TestTheComparisonFindsBatchByBatchWhatTheWholeListingShowed(t *testing.T) {
	now := time.Date(2026, 10, 7, 3, 30, 0, 0, time.UTC)
	all := layouts(now)
	for seed := range uint64(20) {
		all["random "+string(rune('a'+seed))] = randomLayout(seed, 300, now)
	}
	shuffle := rand.New(rand.NewPCG(1, 2)) // #nosec G404 -- a test's reproducible order
	for name, l := range all {
		want := judgedAtOnce(l.tenant, l.listed, l.rows, now)
		for _, size := range []int{1, 2, 3, 5, consistencyBatch} {
			c := comparison{tenantID: l.tenant, now: now, batch: size}
			require.NoError(t, c.run(context.Background(), listingOf(l.listed), rowsOf(l.rows)), name)
			assertFound(t, want, c, name)
		}
		// judge alone, over a listing and rows in any order, finds what it found.
		listed, rows := slices.Clone(l.listed), slices.Clone(l.rows)
		shuffle.Shuffle(len(listed), func(i, j int) { listed[i], listed[j] = listed[j], listed[i] })
		shuffle.Shuffle(len(rows), func(i, j int) { rows[i], rows[j] = rows[j], rows[i] })
		assert.Equal(t, judgedAtOnce(l.tenant, listed, rows, now), judge(l.tenant, listed, rows, now), name)
	}

	runs := all["a run of orphans past the list's bound"]
	c := comparison{tenantID: runs.tenant, now: now, batch: consistencyBatch}
	require.NoError(t, c.run(context.Background(), listingOf(runs.listed), rowsOf(runs.rows)))
	assert.Equal(t, 1200, c.orphans, "every orphan is counted")
	assert.Len(t, c.firstOrphans, ConsistencyListBound, "the first of them are kept")
	runs = all["a run of missing objects"]
	c = comparison{tenantID: runs.tenant, now: now, batch: consistencyBatch}
	require.NoError(t, c.run(context.Background(), listingOf(runs.listed), rowsOf(runs.rows)))
	assert.Len(t, c.unlisted, 1200, "every attachment the listing did not show is asked for")
}

// docs/adr/0059 D4: the listing comes before the rows — an upload puts its
// object before its row commits. A range of rows is read only once the
// listing has handed on a key at or past the range's end, the rows left once
// it has ended, and the ranges follow each other without a gap.
func TestTheRowsAreReadOnceTheListingHasPassedThem(t *testing.T) {
	now := time.Date(2026, 10, 7, 3, 30, 0, 0, time.UTC)
	l := randomLayout(7, 400, now)
	for _, size := range []int{1, 4, consistencyBatch} {
		var handed string
		ended, reads := false, 0
		listing := func(yield func(storage.Object, error) bool) {
			for _, o := range l.listed {
				handed = o.Key
				if !yield(o, nil) {
					return
				}
			}
			ended = true
		}
		next := uuid.Nil
		read := rowsOf(l.rows)
		rows := func(ctx context.Context, after, upto uuid.UUID) ([]uuid.UUID, error) {
			reads++
			assert.Equal(t, next, after, "the ranges follow each other")
			next = upto
			if upto == uuid.Max {
				assert.True(t, ended, "the rows left are read once the listing has ended")
			} else {
				assert.LessOrEqual(t, storage.Key(l.tenant, upto), handed, "a range is read once the listing passed its end")
			}
			return read(ctx, after, upto)
		}
		c := comparison{tenantID: l.tenant, now: now, batch: size}
		require.NoError(t, c.run(context.Background(), listing, rows))
		assert.Equal(t, uuid.Max, next, "the last range reaches every id")
		assert.LessOrEqual(t, reads, len(l.listed)/size+1)
	}
}

// The comparison relies on the byte order of the keys, which S3 promises: a
// store that lists out of it fails the run, loudly, and so does a listing or
// a read of the rows that fails.
func TestAListingOutOfOrderOrThatFailsFailsTheComparison(t *testing.T) {
	now := time.Date(2026, 10, 7, 3, 30, 0, 0, time.UTC)
	l := randomLayout(3, 40, now)
	backwards := slices.Clone(l.listed)
	slices.Reverse(backwards)
	twice := append(slices.Clone(l.listed[:3]), l.listed[2:]...)
	for _, size := range []int{1, 2, consistencyBatch} {
		for name, listed := range map[string][]storage.Object{"backwards": backwards, "a key twice": twice} {
			c := comparison{tenantID: l.tenant, now: now, batch: size}
			err := c.run(context.Background(), listingOf(listed), rowsOf(l.rows))
			assert.ErrorContains(t, err, "the comparison needs the keys in byte order", name)
		}
	}

	refused := errors.New("access denied")
	failing := func(yield func(storage.Object, error) bool) {
		if yield(l.listed[0], nil) {
			yield(storage.Object{}, refused)
		}
	}
	c := comparison{tenantID: l.tenant, now: now, batch: 1}
	assert.ErrorIs(t, c.run(context.Background(), failing, rowsOf(l.rows)), refused, "a listing that fails")
	unreadable := func(context.Context, uuid.UUID, uuid.UUID) ([]uuid.UUID, error) { return nil, refused }
	c = comparison{tenantID: l.tenant, now: now, batch: 1}
	assert.ErrorIs(t, c.run(context.Background(), listingOf(l.listed), unreadable), refused, "a read of the rows that fails")
}

// syntheticID is the i-th of a tenant's synthetic attachments, made at
// made: a UUIDv7 that carries i in its last eight bytes, so that the ids,
// and their keys, sort as i does.
func syntheticID(made time.Time, i int) uuid.UUID {
	var id uuid.UUID
	ms := uint64(made.UnixMilli()) // #nosec G115 -- a time after 1970
	for b := range 6 {
		id[b] = byte(ms >> (40 - 8*b))
	}
	id[6] = 0x70
	binary.BigEndian.PutUint64(id[8:], 1<<63|uint64(i)) // #nosec G115 -- i is not negative
	return id
}

// heapPeak is the most live heap — after a collection — seen at a sample.
type heapPeak struct{ base, peak uint64 }

func (h *heapPeak) sample() {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	h.peak = max(h.peak, m.HeapAlloc)
}

// A tenant of a million objects, whether every one is named by its row or
// none is: the comparison holds a bounded memory whatever the number of
// objects. It holds one batch of the listing — a thousand objects of about
// 130 bytes each —, the rows that batch reaches, a set of its ids and at most
// a thousand orphans: well under a megabyte. The bound, 8 MiB, leaves room for
// the runtime and the test, and is a sixteenth of what the listing and the
// rows alone took when the check read them whole: 145 MiB live for a million
// objects, measured on 2026-10-07 against the check that collected them.
func TestTheComparisonHoldsABoundedMemoryWhateverTheNumberOfObjects(t *testing.T) {
	const objects, every, bound = 1_000_000, 50_000, 8 << 20
	now := time.Date(2026, 10, 7, 3, 30, 0, 0, time.UTC)
	made := now.Add(-2 * OrphanGrace)
	tenant := uuid.Must(uuid.NewV7())
	for _, named := range []bool{true, false} {
		h := &heapPeak{}
		listing := func(yield func(storage.Object, error) bool) {
			for i := range objects {
				if i%every == 0 {
					h.sample()
				}
				if !yield(storage.Object{Key: storage.Key(tenant, syntheticID(made, i)), Size: 1, LastModified: made}, nil) {
					return
				}
			}
			h.sample()
		}
		rows := func(_ context.Context, after, upto uuid.UUID) ([]uuid.UUID, error) {
			if !named {
				return nil, nil
			}
			lo := sort.Search(objects, func(i int) bool { id := syntheticID(made, i); return bytes.Compare(id[:], after[:]) > 0 })
			hi := sort.Search(objects, func(i int) bool { id := syntheticID(made, i); return bytes.Compare(id[:], upto[:]) > 0 })
			ids := make([]uuid.UUID, 0, max(hi-lo, 0))
			for i := lo; i < hi; i++ {
				ids = append(ids, syntheticID(made, i))
			}
			return ids, nil
		}
		c := comparison{tenantID: tenant, now: now, batch: consistencyBatch}
		runtime.GC()
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		h.base, h.peak = m.HeapAlloc, m.HeapAlloc

		require.NoError(t, c.run(context.Background(), listing, rows))

		growth := h.peak - h.base
		t.Logf("named by their rows: %t; %d objects; the live heap grew by %.2f MiB at most", named, objects, float64(growth)/(1<<20))
		assert.Less(t, growth, uint64(bound), "the comparison holds a bounded memory whatever the number of objects")
		assert.Empty(t, c.unlisted)
		if named {
			assert.Zero(t, c.orphans)
			continue
		}
		assert.Equal(t, objects, c.orphans)
		assert.EqualValues(t, objects, c.orphanBytes)
		assert.Len(t, c.firstOrphans, ConsistencyListBound)
	}
}
