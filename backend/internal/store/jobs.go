package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/guided-traffic/cowork/backend/internal/store/writeq"
)

// jobLockNamespace is the first key of every job's advisory lock, "cowk";
// golang-migrate takes a single bigint key, so the two key spaces never meet.
const jobLockNamespace int32 = 0x636f776b

// actionExpired is the act of a cleanup job that removed rows.
const actionExpired = "expired"

// The first keys of the locks that order concurrent writes: re-parentings
// per project, "cowp", and blocks links per tenant, "cowb", which an
// integrity walk checks; question numbers per ticket, "cowq"; the attachment
// count per ticket, "cowa".
const (
	parentLockNamespace     int32 = 0x636f7770
	blocksLockNamespace     int32 = 0x636f7762
	questionLockNamespace   int32 = 0x636f7771
	attachmentLockNamespace int32 = 0x636f7761
)

// LockParents takes the project's re-parenting lock until the transaction
// ends. The parent cycle walk that follows is a new statement and sees every
// re-parenting committed before the lock was granted, so two concurrent ones
// cannot close a cycle together (docs/adr/0008 D2).
func (w *Writer) LockParents(ctx context.Context, projectID uuid.UUID) error {
	return w.lock(ctx, parentLockNamespace, projectID, "re-parenting")
}

// LockBlocks takes the tenant's lock for new blocks links, for the same
// reason over the blocks graph (docs/adr/0012 D4).
func (w *Writer) LockBlocks(ctx context.Context) error {
	return w.lock(ctx, blocksLockNamespace, w.TenantID, "blocks")
}

// LockQuestions takes the ticket's question lock, so two askers never take
// the same number.
func (w *Writer) LockQuestions(ctx context.Context, ticketID uuid.UUID) error {
	return w.lock(ctx, questionLockNamespace, ticketID, "question")
}

// LockAttachments takes the ticket's attachment lock, so two uploads cannot
// both pass the per-ticket count.
func (w *Writer) LockAttachments(ctx context.Context, ticketID uuid.UUID) error {
	return w.lock(ctx, attachmentLockNamespace, ticketID, "attachment")
}

func (w *Writer) lock(ctx context.Context, namespace int32, key uuid.UUID, name string) error {
	if _, err := w.tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1, hashtext($2::text))", namespace, key); err != nil {
		return fmt.Errorf("take the %s lock: %w", name, err)
	}
	return nil
}

// RunJob runs a background job's work in one transaction under a
// transaction-level advisory lock on (jobLockNamespace, lockKey), so exactly
// one backend replica runs it at a time (docs/adr/0027 D5). The lock is
// released with the transaction, by commit or rollback; a session-level lock
// would survive on an idle pooled connection. The job acts as the system
// actor system:<name>, and the transaction names it in app.job, which the
// policies of the job's tables admit. ran is false when another replica holds
// the lock. A job that records no act commits nothing and is no error.
func (db *DB) RunJob(ctx context.Context, name string, lockKey int32, fn func(w *Writer) error) (ran bool, err error) {
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var locked bool
	if err := tx.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock($1, $2)", jobLockNamespace, lockKey).Scan(&locked); err != nil {
		return false, fmt.Errorf("take the lock of job %s: %w", name, err)
	}
	if !locked {
		return false, nil
	}
	caller := Caller{System: "system:" + name}
	if err := setContext(ctx, tx, uuid.Nil, caller, name); err != nil {
		return false, err
	}
	w := &Writer{Reader: newReader(tx, uuid.Nil, caller), Queries: writeq.New(tx)}
	if err := fn(w); err != nil {
		if errors.Is(err, ErrNoChange) {
			return true, nil
		}
		return true, err
	}
	if len(w.events) == 0 {
		return true, nil
	}
	if err := w.writeEvents(ctx, uuid.Nil, caller, Idempotency{}, false); err != nil {
		return true, err
	}
	if err := tx.Commit(ctx); err != nil {
		return true, fmt.Errorf("commit job %s: %w", name, err)
	}
	return true, nil
}

// ExpireIdempotencyKeys deletes the stored responses whose twenty-four hours
// have passed (docs/adr/0045 D4) and records one act per run that removed any.
func (db *DB) ExpireIdempotencyKeys(ctx context.Context) (removed int64, err error) {
	_, err = db.RunJob(ctx, "idempotency-expiry", 1, func(w *Writer) error {
		n, err := w.DeleteExpiredIdempotencyKeys(ctx)
		if err != nil {
			return fmt.Errorf("delete expired idempotency keys: %w", err)
		}
		removed = n
		if n == 0 {
			return nil
		}
		w.Record(Event{EntityType: "idempotency_keys", Action: actionExpired, After: map[string]int64{"removed": n}})
		return nil
	})
	return removed, err
}
