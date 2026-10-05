package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/store/writeq"
)

// PurgeAfter is how long a deleted ticket stays in its tenant's bin before the
// job purges it (docs/adr/0024 D2).
const PurgeAfter = 30 * 24 * time.Hour

// jobTicketPurge names the purge in app.job: the job's own transactions, and
// an administrator's explicit purge for its part of the request's. The
// restrictive policies of migration 32 admit a delete of a ticket, or of what
// belongs only to it, in no other.
const jobTicketPurge = "ticket-purge"

// purgeBatch bounds the tickets one run of the job purges; the next run takes
// the rest.
const purgeBatch = 200

// The entity, the act and the fields of a purge's acts.
const (
	entityTicket  = "ticket"
	actionPurged  = "purged"
	actionUpdated = "updated"
)

// Purged is a ticket the purge removed: its key, which its audit rows keep,
// and its attachments, whose objects are removed once the purge committed —
// before, a rollback would leave rows that name missing bytes.
type Purged struct {
	TenantID    uuid.UUID
	Key         string
	Attachments []uuid.UUID
}

// PurgeTicket removes a deleted ticket of the transaction's tenant and
// everything that belongs only to it — its notifications and those of its
// acts, attachments, comments with their revisions, questions, time entries
// with their revisions, stakes and links — and empties the content of its
// audit rows, which keep its key, the actor and the act (docs/adr/0024 D2).
// Its children become roots, and a block that waits on it waits on its key as
// an external reference from then on, an act on that ticket. The purge's own
// act is recorded with the facts its publication needs, since the ticket is
// gone when the act is written. ErrNotFound when the ticket is no deleted
// ticket of the tenant — a concurrent purge or restoration came first. slug
// is the tenant's, for the keys.
func (w *Writer) PurgeTicket(ctx context.Context, slug string, ticketID uuid.UUID) (Purged, error) {
	tenantID := w.TenantID
	restore, err := w.namePurge(ctx)
	if err != nil {
		return Purged{}, err
	}
	row, err := w.GetPurgedTicket(ctx, writeq.GetPurgedTicketParams{TenantID: tenantID, ID: ticketID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Purged{}, ErrNotFound
	}
	if err != nil {
		return Purged{}, fmt.Errorf("read the ticket to purge: %w", err)
	}
	key := domain.FullKey(slug, row.ProjectKey, row.Number)
	if _, err := w.PurgeTicketAudit(ctx, ticketID); err != nil {
		return Purged{}, fmt.Errorf("empty the ticket's audit rows: %w", err)
	}
	removed, err := w.removeWhatBelongsTo(ctx, tenantID, ticketID)
	if err != nil {
		return Purged{}, err
	}
	if err := w.releaseBlocksOn(ctx, slug, tenantID, ticketID, key); err != nil {
		return Purged{}, err
	}
	n, err := w.DeletePurgedTicket(ctx, writeq.DeletePurgedTicketParams{TenantID: tenantID, ID: ticketID})
	if err != nil {
		return Purged{}, fmt.Errorf("delete the ticket: %w", err)
	}
	if n != 1 {
		return Purged{}, ErrNotFound
	}
	if err := restore(); err != nil {
		return Purged{}, err
	}
	w.Record(Event{EntityType: entityTicket, EntityID: ticketID, TicketID: ticketID, TicketKey: key, Action: actionPurged,
		After: removed, Published: &TicketFacts{Project: row.ProjectID, Version: row.Version + 1,
			Confidential: row.Confidential, Assignee: row.AssigneeID, Reporter: row.ReporterID}})
	return Purged{TenantID: tenantID, Key: key, Attachments: row.Attachments}, nil
}

// namePurge names the purge in app.job for this part of the transaction and
// returns what names the transaction's job again: none in a request, the job
// itself in the job's own transaction.
func (w *Writer) namePurge(ctx context.Context) (func() error, error) {
	var before string
	if err := w.tx.QueryRow(ctx, "SELECT coalesce(current_setting('app.job', true), '')").Scan(&before); err != nil {
		return nil, fmt.Errorf("read the transaction's job: %w", err)
	}
	if _, err := w.tx.Exec(ctx, "SELECT set_config('app.job', $1, true)", jobTicketPurge); err != nil {
		return nil, fmt.Errorf("name the purge: %w", err)
	}
	return func() error {
		if _, err := w.tx.Exec(ctx, "SELECT set_config('app.job', $1, true)", before); err != nil {
			return fmt.Errorf("leave the purge: %w", err)
		}
		return nil
	}, nil
}

// removeWhatBelongsTo deletes the rows that belong only to the ticket, the
// referencing ones first, and the children's reference to it; it returns the
// count of each, what the purge's act records.
func (w *Writer) removeWhatBelongsTo(ctx context.Context, tenantID, ticketID uuid.UUID) (map[string]int64, error) {
	steps := []struct {
		name string
		run  func() (int64, error)
	}{
		{"notifications", func() (int64, error) {
			return w.DeleteTicketNotifications(ctx, writeq.DeleteTicketNotificationsParams{TenantID: tenantID, TicketID: ticketID})
		}},
		{"attachments", func() (int64, error) {
			return w.DeleteTicketAttachments(ctx, writeq.DeleteTicketAttachmentsParams{TenantID: tenantID, TicketID: ticketID})
		}},
		{"comment_revisions", func() (int64, error) {
			return w.DeleteTicketCommentRevisions(ctx, writeq.DeleteTicketCommentRevisionsParams{TenantID: tenantID, TicketID: ticketID})
		}},
		{"comments", func() (int64, error) {
			return w.DeleteTicketComments(ctx, writeq.DeleteTicketCommentsParams{TenantID: tenantID, TicketID: ticketID})
		}},
		{"questions", func() (int64, error) {
			return w.DeleteTicketQuestions(ctx, writeq.DeleteTicketQuestionsParams{TenantID: tenantID, TicketID: ticketID})
		}},
		{"time_entry_revisions", func() (int64, error) {
			return w.DeleteTicketTimeEntryRevisions(ctx, writeq.DeleteTicketTimeEntryRevisionsParams{TenantID: tenantID, TicketID: ticketID})
		}},
		{"time_entries", func() (int64, error) {
			return w.DeleteTicketTimeEntries(ctx, writeq.DeleteTicketTimeEntriesParams{TenantID: tenantID, TicketID: ticketID})
		}},
		{"interest", func() (int64, error) {
			return w.DeleteTicketInterest(ctx, writeq.DeleteTicketInterestParams{TenantID: tenantID, TicketID: ticketID})
		}},
		{"links", func() (int64, error) {
			return w.DeleteTicketLinks(ctx, writeq.DeleteTicketLinksParams{TenantID: tenantID, TicketID: ticketID})
		}},
		{"children", func() (int64, error) {
			return w.DetachChildren(ctx, writeq.DetachChildrenParams{TenantID: tenantID, TicketID: ticketID})
		}},
	}
	removed := map[string]int64{}
	for _, s := range steps {
		n, err := s.run()
		if err != nil {
			return nil, fmt.Errorf("purge the ticket's %s: %w", s.name, err)
		}
		if n > 0 {
			removed[s.name] = n
		}
	}
	return removed, nil
}

// releaseBlocksOn turns every block that waits on the purged ticket into a
// block on its key as an external reference, an act on each blocked ticket.
func (w *Writer) releaseBlocksOn(ctx context.Context, slug string, tenantID, ticketID uuid.UUID, key string) error {
	rows, err := w.ReleaseBlocksOn(ctx, writeq.ReleaseBlocksOnParams{TenantID: tenantID, TicketID: ticketID, Key: key})
	if err != nil {
		return fmt.Errorf("release the blocks on the purged ticket: %w", err)
	}
	for _, r := range rows {
		w.Record(Event{EntityType: entityTicket, EntityID: r.ID, TicketID: r.ID,
			TicketKey: domain.FullKey(slug, r.ProjectKey, r.Number), Action: actionUpdated,
			Before: map[string]any{"block": map[string]any{"kind": domain.BlockTicket}},
			After:  map[string]any{"block": map[string]any{"kind": domain.BlockExternal, "external_ref": key}},
			Reason: "the ticket it waited on was purged"})
	}
	return nil
}

// PurgeDeletedTickets is the job of docs/adr/0024 D2 (docs/adr/0027 D5): the
// tickets deleted longer than PurgeAfter before now are purged, tenant by
// tenant, each purge an act of system:ticket-purge in its tenant. It returns
// what it purged once the transaction committed — nothing when another
// replica runs the job or nothing is due — for the caller to remove the
// attachment objects and to log.
func (db *DB) PurgeDeletedTickets(ctx context.Context, now time.Time) ([]Purged, error) {
	var purged []Purged
	_, err := db.RunJob(ctx, jobTicketPurge, 6, func(w *Writer) error {
		due, err := w.ListPurgeDue(ctx, writeq.ListPurgeDueParams{DeletedBefore: now.Add(-PurgeAfter), Batch: purgeBatch})
		if err != nil {
			return fmt.Errorf("find the tickets due for the purge: %w", err)
		}
		for start := 0; start < len(due); {
			tenantID, end := due[start].TenantID, start
			for end < len(due) && due[end].TenantID == tenantID {
				end++
			}
			if err := w.inTenant(ctx, tenantID, func() error {
				tenant, err := w.GetTenant(ctx, tenantID)
				if err != nil {
					return fmt.Errorf("read the tenant of a ticket due for the purge: %w", err)
				}
				for _, d := range due[start:end] {
					p, err := w.PurgeTicket(ctx, tenant.Slug, d.ID)
					if err != nil {
						return err
					}
					purged = append(purged, p)
				}
				return nil
			}); err != nil {
				return err
			}
			start = end
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return purged, nil
}
