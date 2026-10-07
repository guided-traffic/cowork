package store

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/metrics"
	"github.com/guided-traffic/cowork/backend/internal/storage"
	"github.com/guided-traffic/cowork/backend/internal/store/writeq"
)

// The consistency check of the attachments (docs/adr/0059 D4): per tenant,
// the attachment metadata whose object the bucket lacks — dangling — and the
// objects under the tenant's prefix that no metadata names — orphans —, after
// a restore that brought the database and the bucket back from two points in
// time, or an object a purge failed to remove.
const (
	// JobConsistencyCheck names the job: its system actor, its log lines, its
	// metrics and the policies of migration 43.
	JobConsistencyCheck = "consistency-check"
	// consistencyLockKey is the job's advisory lock key.
	consistencyLockKey int32 = 7
	// ConsistencyCheckHour is the hour of the day, in UTC, from which the
	// daily check is due (docs/adr/0059 D6: a constant, no configuration).
	ConsistencyCheckHour = 3
	// ConsistencyListBound is the most entries a result keeps of each of its
	// two lists; the counts are exact. A removal and an acceptance act on the
	// entries listed, and the next check lists what is left.
	ConsistencyListBound = 1000
	// OrphanGrace is how young an object may be and not be judged: an upload
	// puts its object before its row commits, so an object whose attachment
	// id was made within the grace may be an upload in flight. The id is a
	// UUIDv7 and tells its time even after a restore rewrote the object; an
	// object under another key is judged by its last change.
	OrphanGrace = time.Hour
)

// The installation-level act of a run and its fields.
const (
	// EntityAttachmentConsistency is the entity of the check's acts: the
	// run's summary, a removal of orphans, an acceptance of missing files.
	EntityAttachmentConsistency = "attachment_consistency"
	actionChecked               = "checked"
)

// ObjectStore is the object storage as the check reads it: the objects under
// a prefix, and whether one exists. *storage.Client is one.
type ObjectStore interface {
	List(ctx context.Context, prefix string) ([]storage.Object, error)
	Exists(ctx context.Context, key string) (bool, error)
}

// DanglingAttachment is a file whose metadata is there and whose bytes are
// not, as a result lists it for the tenant's administrators.
type DanglingAttachment struct {
	ID            uuid.UUID `json:"id"`
	FileName      string    `json:"file_name"`
	Size          int64     `json:"size"`
	ContentType   string    `json:"content_type"`
	Ticket        string    `json:"ticket"`
	TicketDeleted bool      `json:"ticket_deleted"`
	UploadedAt    time.Time `json:"uploaded_at"`
	Accepted      bool      `json:"accepted"`
}

// OrphanedObject is an object under the tenant's prefix that no metadata
// names, as a result lists it.
type OrphanedObject struct {
	Key          string    `json:"key"`
	Size         int64     `json:"size"`
	LastModified time.Time `json:"last_modified"`
}

// TenantConsistency is what a run found in one tenant.
type TenantConsistency struct {
	TenantID uuid.UUID
	Slug     string
	// Dangling are the files whose bytes are missing and whose loss nobody
	// accepted; Accepted those whose loss an administrator accepted.
	Dangling, Accepted int
	// Orphans are the objects no metadata names, OrphanBytes their size.
	Orphans     int
	OrphanBytes int64
}

// Found says whether the tenant's attachments and objects are out of step.
func (t TenantConsistency) Found() bool {
	return t.Dangling > 0 || t.Accepted > 0 || t.Orphans > 0
}

// ConsistencyRun is what a run of the check found, tenant by tenant. Ran is
// false when another replica held the job's lock.
type ConsistencyRun struct {
	Ran     bool
	At      time.Time
	Tenants []TenantConsistency
}

// ConsistencyCheckDue says whether the daily check is due at now, given when
// it last checked: when no tenant has a result yet, or when the last run lies
// before the latest ConsistencyCheckHour o'clock UTC at or before now. The
// jobs ask it at the start and every hour, so the check runs once a day in the
// hour after that time, and at a start that finds the last run older than it —
// a run a day old always is (docs/adr/0059 D6, made concrete 2026-10-06). The
// last run is the database's, so replicas agree on it.
func ConsistencyCheckDue(last time.Time, checked bool, now time.Time) bool {
	if !checked {
		return true
	}
	return last.Before(latestCheckHour(now))
}

// latestCheckHour is the latest ConsistencyCheckHour o'clock UTC at or before
// now.
func latestCheckHour(now time.Time) time.Time {
	now = now.UTC()
	at := time.Date(now.Year(), now.Month(), now.Day(), ConsistencyCheckHour, 0, 0, 0, time.UTC)
	if at.After(now) {
		at = at.AddDate(0, 0, -1)
	}
	return at
}

// LastConsistencyCheck is when the check last ran; checked is false while no
// tenant has a result.
func (db *DB) LastConsistencyCheck(ctx context.Context) (last time.Time, checked bool, err error) {
	err = db.jobRead(ctx, JobConsistencyCheck, func(r *Reader) error {
		row, err := r.LastConsistencyCheck(ctx)
		if err != nil {
			return fmt.Errorf("read the last consistency check: %w", err)
		}
		last, checked = row.LastCheckedAt, row.Tenants > 0
		return nil
	})
	return last, checked, err
}

// consistencyForMetrics reads every tenant's counts of its latest result for
// a scrape (docs/adr/0060 D4); a failed read is logged, never scraped.
func (db *DB) consistencyForMetrics(ctx context.Context) ([]metrics.ConsistencyCounts, error) {
	var out []metrics.ConsistencyCounts
	err := db.jobRead(ctx, JobConsistencyCheck, func(r *Reader) error {
		rows, err := r.ListConsistencyCounts(ctx)
		if err != nil {
			return err
		}
		out = make([]metrics.ConsistencyCounts, 0, len(rows))
		for _, row := range rows {
			out = append(out, metrics.ConsistencyCounts{Tenant: row.TenantID.String(),
				Dangling: int64(row.Dangling), Orphans: int64(row.Orphans)})
		}
		return nil
	})
	if err != nil {
		db.logger.Warn("the consistency counts could not be read for the metrics", "error", err)
		return nil, err
	}
	return out, nil
}

// jobRead runs fn in a read-only transaction that names job and no tenant:
// what the job's policies admit across the tenants, read outside the job's
// run — its schedule and a scrape.
func (db *DB) jobRead(ctx context.Context, job string, fn func(r *Reader) error) error {
	tx, err := db.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := setContext(ctx, tx, uuid.Nil, Caller{}, job); err != nil {
		return err
	}
	if err := fn(newReader(tx, uuid.Nil, Caller{})); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// CheckConsistency is the job of docs/adr/0059 D4 (docs/adr/0027 D5): every
// tenant is checked in its own part of the job's transaction — the objects
// under its prefix listed, then its attachments read, the attachments the
// listing missed asked for one by one — and its result replaces the last one
// under a new id. The run's summary — counts per tenant, never a file name or
// a key — is one installation-level act of system:consistency-check. Nothing
// is removed: an administrator confirms the removal of the orphans.
func (db *DB) CheckConsistency(ctx context.Context, objects ObjectStore, now time.Time) (ConsistencyRun, error) {
	run := ConsistencyRun{At: now.UTC()}
	ran, err := db.RunJob(ctx, JobConsistencyCheck, consistencyLockKey, func(w *Writer) error {
		tenants, err := w.ListTenantsToCheck(ctx)
		if err != nil {
			return fmt.Errorf("list the tenants to check: %w", err)
		}
		for _, t := range tenants {
			var result TenantConsistency
			if err := w.inTenant(ctx, t.ID, func() error {
				var err error
				result, err = w.checkTenant(ctx, objects, t.ID, t.Slug, run.At)
				return err
			}); err != nil {
				return err
			}
			run.Tenants = append(run.Tenants, result)
		}
		if len(run.Tenants) == 0 {
			return nil
		}
		w.Record(Event{EntityType: EntityAttachmentConsistency, Action: actionChecked, After: summaryOf(run.Tenants)})
		return nil
	})
	run.Ran = ran
	if err != nil {
		return ConsistencyRun{At: run.At}, err
	}
	return run, nil
}

// summaryOf is the run's act: the counts in all, and those of each tenant
// whose attachments and objects are out of step, by the tenant's id.
func summaryOf(tenants []TenantConsistency) map[string]any {
	var dangling, accepted, orphans int
	var bytes int64
	found := map[string]map[string]int{}
	for _, t := range tenants {
		dangling, accepted, orphans, bytes = dangling+t.Dangling, accepted+t.Accepted, orphans+t.Orphans, bytes+t.OrphanBytes
		if t.Found() {
			found[t.TenantID.String()] = map[string]int{"dangling": t.Dangling, "accepted": t.Accepted, "orphans": t.Orphans}
		}
	}
	return map[string]any{"tenants": len(tenants), "dangling": dangling, "accepted": accepted, "orphans": orphans,
		"orphan_bytes": bytes, "found": found}
}

// checkTenant checks one tenant, inside the job's transaction bound to it.
// The listing comes first and the rows after it: an upload puts its object
// before its row commits, so a row the read finds has its object listed —
// or put after the listing passed it, which the question for each such
// attachment answers.
func (w *Writer) checkTenant(ctx context.Context, objects ObjectStore, tenantID uuid.UUID, slug string, now time.Time) (TenantConsistency, error) {
	result := TenantConsistency{TenantID: tenantID, Slug: slug}
	listed, err := objects.List(ctx, tenantID.String()+"/")
	if err != nil {
		return result, fmt.Errorf("list the objects of the tenant %s: %w", slug, err)
	}
	rows, err := w.ListTenantAttachmentIDs(ctx, tenantID)
	if err != nil {
		return result, fmt.Errorf("read the attachments of the tenant %s: %w", slug, err)
	}
	j := judge(tenantID, listed, rows, now)
	missing, err := confirmMissing(ctx, objects, tenantID, j.unlisted)
	if err != nil {
		return result, fmt.Errorf("ask for the objects of the tenant %s: %w", slug, err)
	}
	acceptedIDs, err := w.ListAcceptedAttachments(ctx, tenantID)
	if err != nil {
		return result, fmt.Errorf("read the accepted losses of the tenant %s: %w", slug, err)
	}
	if _, err := w.ForgetWholeAcceptances(ctx, writeq.ForgetWholeAcceptancesParams{TenantID: tenantID, Missing: missing}); err != nil {
		return result, fmt.Errorf("forget the acceptances of whole attachments: %w", err)
	}
	accepted := setOf(acceptedIDs)
	order, lost := danglingOrder(missing, accepted)
	result.Accepted = lost
	result.Dangling = len(missing) - lost
	items, err := w.danglingItems(ctx, tenantID, slug, order, accepted)
	if err != nil {
		return result, err
	}
	result.Orphans = len(j.orphans)
	for _, o := range j.orphans {
		result.OrphanBytes += o.Size
	}
	return result, w.saveCheck(ctx, result, now, items, orphanItems(j.orphans))
}

// judgement is what a listing and the rows say of a tenant before the
// attachments the listing missed are asked for.
type judgement struct {
	// orphans are the objects no row names, old enough to be judged, in key
	// order.
	orphans []storage.Object
	// unlisted are the attachments whose object the listing did not show.
	unlisted []uuid.UUID
}

// judge compares the objects listed under the tenant's prefix with the ids of
// its attachments. An object under any other key than <tenant-id>/<id> in the
// form the backend writes is an orphan: no row can name it. One whose id no
// row names is an orphan unless it is younger than OrphanGrace.
func judge(tenantID uuid.UUID, listed []storage.Object, rows []uuid.UUID, now time.Time) judgement {
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

// young says whether an object is too young to be judged an orphan: by the
// time its id was made where the key names a UUIDv7, which a restore keeps,
// else by its last change.
func young(id uuid.UUID, named bool, o storage.Object, now time.Time) bool {
	made := o.LastModified
	if named && id.Version() == 7 {
		sec, nsec := id.Time().UnixTime()
		made = time.Unix(sec, nsec)
	}
	return now.Sub(made) < OrphanGrace
}

// confirmMissing asks for each attachment the listing did not show whether
// its object exists now: an upload's put that came after the listing passed
// its key is no missing object.
func confirmMissing(ctx context.Context, objects ObjectStore, tenantID uuid.UUID, unlisted []uuid.UUID) ([]uuid.UUID, error) {
	missing := []uuid.UUID{}
	for _, id := range unlisted {
		ok, err := objects.Exists(ctx, storage.Key(tenantID, id))
		if err != nil {
			return nil, err
		}
		if !ok {
			missing = append(missing, id)
		}
	}
	return missing, nil
}

// setOf is a set of ids.
func setOf(ids []uuid.UUID) map[uuid.UUID]bool {
	set := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	return set
}

// danglingOrder is the order the list shows the missing files in — the ones
// nobody accepted first, each part by id — and how many of them were accepted.
func danglingOrder(missing []uuid.UUID, accepted map[uuid.UUID]bool) ([]uuid.UUID, int) {
	open := make([]uuid.UUID, 0, len(missing))
	var lost []uuid.UUID
	for _, id := range missing {
		if accepted[id] {
			lost = append(lost, id)
		} else {
			open = append(open, id)
		}
	}
	return append(open, lost...), len(lost)
}

// danglingItems reads what the list shows of the first ConsistencyListBound
// missing files, in order.
func (w *Writer) danglingItems(ctx context.Context, tenantID uuid.UUID, slug string, order []uuid.UUID, accepted map[uuid.UUID]bool) ([]DanglingAttachment, error) {
	shown := order[:min(len(order), ConsistencyListBound)]
	items := make([]DanglingAttachment, 0, len(shown))
	if len(shown) == 0 {
		return items, nil
	}
	rows, err := w.ListCheckedAttachments(ctx, writeq.ListCheckedAttachmentsParams{TenantID: tenantID, Ids: shown})
	if err != nil {
		return nil, fmt.Errorf("read the missing files of the tenant %s: %w", slug, err)
	}
	byID := make(map[uuid.UUID]writeq.ListCheckedAttachmentsRow, len(rows))
	for _, r := range rows {
		byID[r.ID] = r
	}
	for _, id := range shown {
		r, ok := byID[id]
		if !ok {
			// Purged since the read of the rows: gone with its object.
			continue
		}
		items = append(items, DanglingAttachment{ID: r.ID, FileName: r.FileName, Size: r.Size, ContentType: r.ContentType,
			Ticket: domain.FullKey(slug, r.ProjectKey, r.Number), TicketDeleted: r.TicketDeleted, UploadedAt: r.CreatedAt.UTC(),
			Accepted: accepted[id]})
	}
	return items, nil
}

// orphanItems are the first ConsistencyListBound orphans as the list shows
// them.
func orphanItems(orphans []storage.Object) []OrphanedObject {
	shown := orphans[:min(len(orphans), ConsistencyListBound)]
	items := make([]OrphanedObject, 0, len(shown))
	for _, o := range shown {
		items = append(items, OrphanedObject{Key: o.Key, Size: o.Size, LastModified: o.LastModified.UTC()})
	}
	return items
}

// saveCheck writes the tenant's result under a new id.
func (w *Writer) saveCheck(ctx context.Context, r TenantConsistency, now time.Time, dangling []DanglingAttachment, orphans []OrphanedObject) error {
	danglingJSON, err := json.Marshal(dangling)
	if err != nil {
		return fmt.Errorf("encode the missing files: %w", err)
	}
	orphanJSON, err := json.Marshal(orphans)
	if err != nil {
		return fmt.Errorf("encode the orphans: %w", err)
	}
	id, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("make the result's id: %w", err)
	}
	err = w.SaveConsistencyCheck(ctx, writeq.SaveConsistencyCheckParams{ID: id, TenantID: r.TenantID, CheckedAt: now,
		Dangling: CountColumn(r.Dangling), Accepted: CountColumn(r.Accepted), Orphans: CountColumn(r.Orphans),
		OrphanBytes: r.OrphanBytes, DanglingItems: danglingJSON, OrphanItems: orphanJSON})
	if err != nil {
		return fmt.Errorf("save the result of the tenant %s: %w", r.Slug, err)
	}
	return nil
}

// CountColumn is a count as its integer column holds it: at most the largest
// int32, which no tenant's files reach.
func CountColumn(n int) int32 {
	if n > math.MaxInt32 {
		return math.MaxInt32
	}
	if n < 0 {
		return 0
	}
	return int32(n) // #nosec G115 -- bounded above
}
