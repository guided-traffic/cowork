package store

import (
	"context"
	"fmt"
	"time"
)

// ImportValidity is how long a dry run's report is valid (docs/adr/0051 D7).
const ImportValidity = 24 * time.Hour

// importExpiryLock is the key of the import expiry's job lock.
const importExpiryLock = 7

// ExpireImportJobs deletes the dry runs whose twenty-four hours have passed,
// in every tenant, with the files they hold (docs/adr/0051 D7), and records
// one act per run that removed any. The policies of migration 41 admit the
// job, named import-expiry, to dry runs alone; an executed job stays.
func (db *DB) ExpireImportJobs(ctx context.Context, now time.Time) (removed int64, err error) {
	_, err = db.RunJob(ctx, "import-expiry", importExpiryLock, func(w *Writer) error {
		n, err := w.DeleteExpiredImportJobs(ctx, &now)
		if err != nil {
			return fmt.Errorf("delete expired dry runs: %w", err)
		}
		removed = n
		if n == 0 {
			return nil
		}
		w.Record(Event{EntityType: "import_jobs", Action: actionExpired, After: map[string]int64{fieldRemoved: n}})
		return nil
	})
	return removed, err
}
