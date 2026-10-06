package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/guided-traffic/cowork/backend/internal/store/writeq"
)

// UnshareAnothersFilter stops sharing another person's filter at the version,
// an administrator's act (docs/adr/0018 D5 as amended 2026-10-06), and returns
// the version and the time the filter ends with; pgx.ErrNoRows when it is no
// shared filter of the tenant at that version. Unshared, the filter is one the
// administrator may not read, and PostgreSQL holds an update's new row to the
// read policy: the statement runs with the filter named in
// app.saved_filter_id, which the read policy of migration 39 admits to an
// administrator of the current tenant, and the name is cleared after it.
func (w *Writer) UnshareAnothersFilter(ctx context.Context, id uuid.UUID, version int32) (writeq.UnshareSavedFilterRow, error) {
	if _, err := w.tx.Exec(ctx, "SELECT set_config('app.saved_filter_id', $1, true)", id.String()); err != nil {
		return writeq.UnshareSavedFilterRow{}, fmt.Errorf("name the filter: %w", err)
	}
	row, err := w.UnshareSavedFilter(ctx, writeq.UnshareSavedFilterParams{TenantID: w.TenantID, ID: id, Version: version})
	if err != nil {
		return row, err
	}
	if _, err := w.tx.Exec(ctx, "SELECT set_config('app.saved_filter_id', '', true)"); err != nil {
		return row, fmt.Errorf("clear the named filter: %w", err)
	}
	return row, nil
}
