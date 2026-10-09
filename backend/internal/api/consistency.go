package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/problem"
	"github.com/guided-traffic/cowork/backend/internal/storage"
	"github.com/guided-traffic/cowork/backend/internal/store"
	"github.com/guided-traffic/cowork/backend/internal/store/readq"
	"github.com/guided-traffic/cowork/backend/internal/store/writeq"
)

// The acts of the consistency check's routes (docs/adr/0059 D4, D5): the
// removal of the orphans is a purge of objects, the acceptance of the missing
// files an acceptance.
const (
	actionPurged   = "purged"
	actionAccepted = "accepted"
)

// orphanRemoval is what removing the orphaned objects of a consistency check
// needs: a tenant administrator, never an agent — nothing brings a removed
// object back (docs/adr/0043 D3) —, in a browser session, which the document
// demands of the route (docs/adr/0035 D5).
var orphanRemoval = auth.Need{Role: domain.RoleAdmin, Scope: domain.ScopeAdmin, HardOff: auth.HardOffDeletion}

// GetAttachmentConsistency answers the tenant's latest consistency check of its
// attachments, for its administrators (docs/adr/0059 D4): the lists name files
// of tickets a member may not see. Before the first check the counts are 0 and
// the check's id null. Beside it, when the tenant was last exported
// (docs/adr/0059 D2). An answer the client holds unchanged is a 304
// (docs/adr/0054 D7).
func (s *Server) GetAttachmentConsistency(ctx context.Context, req apigen.GetAttachmentConsistencyRequestObject) (apigen.GetAttachmentConsistencyResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, adminRead); perr != nil {
		return nil, perr
	}
	out := apigen.AttachmentConsistency{CheckId: nullableOf[uuid.UUID](nil), CheckedAt: nullableOf[time.Time](nil),
		DanglingAttachments: []apigen.DanglingAttachment{}, OrphanedObjects: []apigen.OrphanedObject{},
		OrphanRemoval: nullableOf[apigen.OrphanRemovalRecord](nil)}
	err := s.db.InTenant(ctx, t.ID, func(r *store.Reader) error {
		row, err := r.GetAttachmentConsistency(ctx, t.ID)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
		case err != nil:
			return err
		default:
			if out, err = consistencyView(row); err != nil {
				return err
			}
		}
		last, err := lastExport(ctx, r, t.ID)
		out.LastExportedAt = nullableOf(last)
		return err
	})
	if err != nil {
		return nil, err
	}
	tag, unchanged := listTag(req.Params.IfNoneMatch, out)
	if unchanged {
		return apigen.GetAttachmentConsistency304Response{Headers: apigen.NotModifiedResponseHeaders{ETag: &tag}}, nil
	}
	return apigen.GetAttachmentConsistency200JSONResponse{Body: out, Headers: apigen.GetAttachmentConsistency200ResponseHeaders{ETag: &tag}}, nil
}

// lastExport is when a project of the tenant or the whole tenant was last
// exported, as the act exported records it; nil while none was.
func lastExport(ctx context.Context, r *store.Reader, tenantID uuid.UUID) (*time.Time, error) {
	at, err := r.LastTenantExport(ctx, &tenantID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the last export: %w", err)
	}
	return &at, nil
}

// consistencyView is a stored result as the API answers it.
func consistencyView(row readq.GetAttachmentConsistencyRow) (apigen.AttachmentConsistency, error) {
	dangling, orphans, err := consistencyItems(row.DanglingItems, row.OrphanItems)
	if err != nil {
		return apigen.AttachmentConsistency{}, err
	}
	out := apigen.AttachmentConsistency{CheckId: nullableOf(&row.ID), CheckedAt: nullableOf(&row.CheckedAt),
		Dangling: int(row.Dangling), Accepted: int(row.Accepted), Orphans: int(row.Orphans), OrphanBytes: row.OrphanBytes,
		DanglingAttachments: make([]apigen.DanglingAttachment, 0, len(dangling)),
		OrphanedObjects:     make([]apigen.OrphanedObject, 0, len(orphans)),
		OrphanRemoval:       nullableOf[apigen.OrphanRemovalRecord](nil)}
	for _, d := range dangling {
		out.DanglingAttachments = append(out.DanglingAttachments, apigen.DanglingAttachment{Id: d.ID, FileName: d.FileName,
			Size: d.Size, ContentType: d.ContentType, Ticket: d.Ticket, TicketDeleted: d.TicketDeleted,
			UploadedAt: d.UploadedAt, Accepted: d.Accepted})
	}
	for _, o := range orphans {
		out.OrphanedObjects = append(out.OrphanedObjects, apigen.OrphanedObject{Key: o.Key, Size: o.Size, LastModified: o.LastModified})
	}
	if row.OrphansRemovedAt != nil && row.OrphansRemovedBy != nil {
		removal := apigen.OrphanRemovalRecord{RemovedAt: *row.OrphansRemovedAt,
			RemovedBy: personView(*row.OrphansRemovedBy, row.OrphansRemovedByUsername, row.OrphansRemovedByName),
			Removed:   int(valueOr(row.OrphansRemoved)), Kept: int(valueOr(row.OrphansKept))}
		out.OrphanRemoval = nullableOf(&removal)
	}
	return out, nil
}

// consistencyItems decodes a result's two lists.
func consistencyItems(danglingJSON, orphanJSON []byte) ([]store.DanglingAttachment, []store.OrphanedObject, error) {
	var dangling []store.DanglingAttachment
	if err := json.Unmarshal(danglingJSON, &dangling); err != nil {
		return nil, nil, fmt.Errorf("decode the missing files of the check: %w", err)
	}
	var orphans []store.OrphanedObject
	if err := json.Unmarshal(orphanJSON, &orphans); err != nil {
		return nil, nil, fmt.Errorf("decode the orphans of the check: %w", err)
	}
	return dangling, orphans, nil
}

func valueOr(v *int32) int32 {
	if v == nil {
		return 0
	}
	return *v
}

// staleCheck refuses a confirmation of lists that are not the tenant's latest
// any more.
func staleCheck(detail string) *problem.Error {
	return problem.New(problem.ConsistencyCheckStale, detail)
}

// lockedCheck reads the tenant's latest result for a confirmation, held until
// it commits, when it is the check the administrator was shown.
func lockedCheck(ctx context.Context, w *store.Writer, t tenantScope, id uuid.UUID) (writeq.GetConsistencyCheckForUpdateRow, error) {
	check, err := w.GetConsistencyCheckForUpdate(ctx, writeq.GetConsistencyCheckForUpdateParams{TenantID: t.ID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return check, staleCheck("the check is not the tenant's latest: a newer one replaced its lists; read them again")
	}
	return check, err
}

// removalPlan is what a confirmed removal does: the keys of the orphans that
// are orphans still, and how many gained metadata since the check.
type removalPlan struct {
	remove []string
	kept   int
}

// RemoveOrphanedObjects removes the orphaned objects of the consistency check
// the administrator confirms (docs/adr/0059 D4): each is asked again, in the
// confirming transaction, whether an attachment names it now — one that does
// is kept —, the act is recorded with the counts, and the objects are removed
// after the commit, so that a rollback removes nothing. A removal that fails
// is logged with its key; the next check lists the object again.
func (s *Server) RemoveOrphanedObjects(ctx context.Context, req apigen.RemoveOrphanedObjectsRequestObject) (apigen.RemoveOrphanedObjectsResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, orphanRemoval); perr != nil {
		return nil, perr
	}
	if s.storage == nil {
		return nil, problem.New(problem.UploadsDisabled, "this installation has no object storage; no object can be removed")
	}
	var plan removalPlan
	_, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		check, err := lockedCheck(ctx, w, t, req.Body.CheckId)
		if err != nil {
			return err
		}
		if check.OrphansRemovedAt != nil {
			return staleCheck("the orphans of this check were removed already")
		}
		_, orphans, err := consistencyItems(check.DanglingItems, check.OrphanItems)
		if err != nil {
			return err
		}
		if len(orphans) == 0 {
			return store.ErrNoChange
		}
		if plan, err = planRemoval(ctx, w, t.ID, orphans); err != nil {
			return err
		}
		unlisted, unlistedBytes := unlistedOrphans(check, orphans)
		if err := w.RecordOrphanRemoval(ctx, writeq.RecordOrphanRemovalParams{TenantID: t.ID, ID: check.ID,
			Orphans: unlisted, OrphanBytes: unlistedBytes, RemovedAt: s.h.opts.Now(), RemovedBy: principal(ctx).PersonID,
			Removed: store.CountColumn(len(plan.remove)), Kept: store.CountColumn(plan.kept)}); err != nil {
			return fmt.Errorf("record the removal: %w", err)
		}
		w.Record(store.Event{EntityType: store.EntityAttachmentConsistency, EntityID: check.ID, Action: actionPurged,
			After: map[string]int{"orphans": len(plan.remove), "kept": plan.kept}})
		return nil
	})
	if errors.Is(err, store.ErrNoChange) {
		return apigen.RemoveOrphanedObjects200JSONResponse{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := s.removeObjects(context.WithoutCancel(ctx), t, plan.remove)
	out.Kept = plan.kept
	return apigen.RemoveOrphanedObjects200JSONResponse(out), nil
}

// unlistedOrphans are the orphans a result counts and its list, cut at its
// bound, does not show: a removal leaves them counted, and the next check lists
// them.
func unlistedOrphans(check writeq.GetConsistencyCheckForUpdateRow, listed []store.OrphanedObject) (int32, int64) {
	bytes := check.OrphanBytes
	for _, o := range listed {
		bytes -= o.Size
	}
	return store.CountColumn(int(check.Orphans) - len(listed)), max(bytes, 0)
}

// planRemoval asks again which of the listed orphans an attachment names now.
// A key outside the tenant's prefix is never removed; a key under it that no
// attachment can name is an orphan whatever happened since.
func planRemoval(ctx context.Context, w *store.Writer, tenantID uuid.UUID, orphans []store.OrphanedObject) (removalPlan, error) {
	var plan removalPlan
	prefix := tenantID.String() + "/"
	keys := map[uuid.UUID]string{}
	ids := []uuid.UUID{}
	for _, o := range orphans {
		if !strings.HasPrefix(o.Key, prefix) {
			continue
		}
		if id, ok := storage.ParseKey(tenantID, o.Key); ok {
			keys[id] = o.Key
			ids = append(ids, id)
			continue
		}
		plan.remove = append(plan.remove, o.Key)
	}
	named, err := w.ListAttachmentsAmong(ctx, writeq.ListAttachmentsAmongParams{TenantID: tenantID, Ids: ids})
	if err != nil {
		return plan, fmt.Errorf("ask which orphans gained metadata: %w", err)
	}
	gained := make(map[uuid.UUID]bool, len(named))
	for _, id := range named {
		gained[id] = true
	}
	for _, id := range ids {
		if gained[id] {
			plan.kept++
			continue
		}
		plan.remove = append(plan.remove, keys[id])
	}
	return plan, nil
}

// removeObjects removes the objects one by one; a missing one is no error.
func (s *Server) removeObjects(ctx context.Context, t tenantScope, keys []string) apigen.OrphanRemoval {
	var out apigen.OrphanRemoval
	for _, key := range keys {
		if err := s.storage.Delete(ctx, key); err != nil {
			out.Failed++
			s.h.logger.Error("an orphaned object could not be removed", "tenant", t.Slug, "object", key, "error", err)
			continue
		}
		out.Removed++
	}
	return out
}

// AcceptDanglingAttachments accepts the loss of the missing files of the
// consistency check the administrator was shown (docs/adr/0059 D5): they count
// as accepted instead of dangling until their bytes come back. Nothing is
// removed; a file purged since the check is passed over.
func (s *Server) AcceptDanglingAttachments(ctx context.Context, req apigen.AcceptDanglingAttachmentsRequestObject) (apigen.AcceptDanglingAttachmentsResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, administer); perr != nil {
		return nil, perr
	}
	var accepted int64
	_, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		check, err := lockedCheck(ctx, w, t, req.Body.CheckId)
		if err != nil {
			return err
		}
		dangling, _, err := consistencyItems(check.DanglingItems, check.OrphanItems)
		if err != nil {
			return err
		}
		ids := []uuid.UUID{}
		for i := range dangling {
			if !dangling[i].Accepted {
				ids = append(ids, dangling[i].ID)
				dangling[i].Accepted = true
			}
		}
		if len(ids) == 0 {
			return store.ErrNoChange
		}
		if accepted, err = w.AcceptAttachments(ctx, writeq.AcceptAttachmentsParams{TenantID: t.ID, Ids: ids,
			AcceptedBy: principal(ctx).PersonID}); err != nil {
			return fmt.Errorf("accept the missing files: %w", err)
		}
		if accepted == 0 {
			return store.ErrNoChange
		}
		items, err := json.Marshal(dangling)
		if err != nil {
			return fmt.Errorf("encode the missing files: %w", err)
		}
		if err := w.RecordDanglingAcceptance(ctx, writeq.RecordDanglingAcceptanceParams{TenantID: t.ID, ID: check.ID,
			Dangling: store.CountColumn(int(check.Dangling) - len(ids)), Accepted: store.CountColumn(int(check.Accepted) + len(ids)),
			DanglingItems: items}); err != nil {
			return fmt.Errorf("record the acceptance: %w", err)
		}
		w.Record(store.Event{EntityType: store.EntityAttachmentConsistency, EntityID: check.ID, Action: actionAccepted,
			After: map[string]int64{"accepted": accepted}})
		return nil
	})
	if errors.Is(err, store.ErrNoChange) {
		return apigen.AcceptDanglingAttachments200JSONResponse{}, nil
	}
	if err != nil {
		return nil, err
	}
	return apigen.AcceptDanglingAttachments200JSONResponse{Accepted: int(accepted)}, nil
}
