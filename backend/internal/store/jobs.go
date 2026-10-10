package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/guided-traffic/cowork/backend/internal/store/writeq"
)

// jobLockNamespace is the first key of every job's advisory lock, "cowk";
// golang-migrate takes a single bigint key, so the two key spaces never meet.
const jobLockNamespace int32 = 0x636f776b

// actionExpired is the act of a cleanup job that removed rows, fieldRemoved
// the count it records.
const (
	actionExpired = "expired"
	fieldRemoved  = "removed"
)

// The first keys of the locks that order concurrent writes: the
// installation's graphs, "cowg", which an integrity walk checks; question
// numbers per ticket, "cowq"; the attachment count per ticket, "cowa"; the
// attachment quota per tenant, "cowu".
const (
	graphLockNamespace      int32 = 0x636f7767
	questionLockNamespace   int32 = 0x636f7771
	attachmentLockNamespace int32 = 0x636f7761
	quotaLockNamespace      int32 = 0x636f7775
)

// Graph is one of the installation's two graphs of tickets whose cycles a
// write refuses: the parents (docs/adr/0008 D2) and the blocks links
// (docs/adr/0012 D4). Each crosses projects and teams, so each has one lock
// for the whole installation, its second key.
type Graph int32

// The two graphs, in the order a transaction that needs both takes them.
const (
	GraphParents Graph = 1
	GraphBlocks  Graph = 2
)

// LockGraph takes the installation's lock of a graph until the transaction
// ends. The cycle walk that follows is a new statement and sees every edge
// committed before the lock was granted, so two concurrent writers cannot
// close a cycle together, whatever teams the cycle runs through. The graph
// locks are the first locks a transaction takes — the parents before the
// blocks, both before the rank's row lock and any ticket row
// (docs/developer/data-access.md#advisory-locks) —, so a transaction that
// waits for one holds no other but the parents', and two writers never wait
// on each other. Asking for the parents after the blocks is refused.
func (w *Writer) LockGraph(ctx context.Context, g Graph) error {
	if w.graphs&(1<<g) != 0 {
		return nil
	}
	if g == GraphParents && w.graphs&(1<<GraphBlocks) != 0 {
		return errors.New("store: the parents' lock is taken before the blocks' lock, never after it")
	}
	if _, err := w.tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1, $2)", graphLockNamespace, int32(g)); err != nil {
		return fmt.Errorf("take the lock of the graph %d: %w", g, err)
	}
	w.graphs |= 1 << g
	return nil
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

// LockAttachmentQuota takes the tenant's attachment quota lock, so two uploads
// cannot both pass the tenant's quota (docs/adr/0016 D6). An upload takes it
// before the ticket's attachment lock.
func (w *Writer) LockAttachmentQuota(ctx context.Context) error {
	return w.lock(ctx, quotaLockNamespace, w.TenantID, "attachment quota")
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
// the lock. A job that records no act commits nothing and is no error. A job
// that works in the tenants one by one writes each tenant's acts there
// (Writer.inTenant); the rest are installation-level acts. A run that took
// the lock, or failed before it could, is recorded in the metrics by the
// job's name — one another replica ran is not, nor one the end of ctx cut
// short (docs/adr/0060 D4).
func (db *DB) RunJob(ctx context.Context, name string, lockKey int32, fn func(w *Writer) error) (ran bool, err error) {
	start := time.Now()
	defer func() {
		if (ran || err != nil) && ctx.Err() == nil {
			db.metrics.JobRun(name, time.Since(start), err != nil)
		}
	}()
	return db.runJob(ctx, name, lockKey, fn)
}

func (db *DB) runJob(ctx context.Context, name string, lockKey int32, fn func(w *Writer) error) (ran bool, err error) {
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
	w := &Writer{Reader: newReader(tx, uuid.Nil, caller), Queries: writeq.New(tx), caller: caller}
	if err := fn(w); err != nil {
		if errors.Is(err, ErrNoChange) {
			return true, nil
		}
		return true, err
	}
	if len(w.events) == 0 && !w.flushed {
		return true, nil
	}
	if err := w.writeEvents(ctx, uuid.Nil, caller, Idempotency{}, false); err != nil {
		return true, err
	}
	if err := tx.Commit(ctx); err != nil {
		return true, fmt.Errorf("commit job %s: %w", name, err)
	}
	countActs(db.metrics, w)
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
		w.Record(Event{EntityType: "idempotency_keys", Action: actionExpired, After: map[string]int64{fieldRemoved: n}})
		return nil
	})
	return removed, err
}
