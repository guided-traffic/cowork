package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/guided-traffic/cowork/backend/internal/store/writeq"
)

// GitHub's webhook (docs/adr/0071): the job its transactions name, which the
// policies of migration 42 admit to the tenant by slug, the secret and the
// writes of a delivery; the system actor its acts carry; and how long a
// delivery is kept for its repetition to change nothing (D3).
const (
	JobGitHubWebhook  = "github-webhook"
	SystemGitHub      = "system:github"
	DeliveryRetention = 24 * time.Hour
)

// jobDeliveryExpiry is the job that removes the deliveries past their day.
const jobDeliveryExpiry = "github-delivery-expiry"

// WebhookTenant is the tenant a webhook's path names, with its sealed secret.
type WebhookTenant struct {
	ID     uuid.UUID
	Slug   string
	Sealed []byte
}

// WebhookSecret reads the tenant a slug names and its sealed webhook secret
// (docs/adr/0071 D1, D2), in one read-only transaction of the webhook's job,
// which names no person: the tenant by its slug first, then — bound to the
// tenant — the secret. ErrNotFound for an unknown slug and for a tenant
// without a secret alike.
func (db *DB) WebhookSecret(ctx context.Context, slug string) (WebhookTenant, error) {
	caller, _ := CallerFrom(ctx)
	tx, err := db.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return WebhookTenant{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := setContext(ctx, tx, uuid.Nil, caller, JobGitHubWebhook); err != nil {
		return WebhookTenant{}, err
	}
	r := newReader(tx, uuid.Nil, caller)
	tenant, err := r.GetTenantBySlug(ctx, slug)
	if err != nil {
		return WebhookTenant{}, notFound(err)
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenant.ID.String()); err != nil {
		return WebhookTenant{}, fmt.Errorf("bind the webhook to its tenant: %w", err)
	}
	sealed, err := r.GetGitHubWebhookSecret(ctx, tenant.ID)
	if err != nil {
		return WebhookTenant{}, notFound(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return WebhookTenant{}, fmt.Errorf("commit transaction: %w", err)
	}
	return WebhookTenant{ID: tenant.ID, Slug: tenant.Slug, Sealed: sealed}, nil
}

// ReceiveDelivery takes a verified delivery of the tenant (docs/adr/0071 D3):
// in one transaction of the webhook's job, bound to the tenant and acting as
// the context's system actor, it records the delivery for a day and runs fn,
// then writes the acts fn recorded — each with the delivery's id as its key
// (docs/adr/0045 D7), their notifications and their publication — and
// commits. repeat is true, and fn does not run, when the tenant took the same
// delivery within the day: nothing changes. Unlike Mutate it commits a
// delivery that recorded no act — an event passed over, a pull request that
// changed nothing — because the delivery itself is the bookkeeping of D3.
func (db *DB) ReceiveDelivery(ctx context.Context, tenantID, delivery uuid.UUID, now time.Time, fn func(w *Writer) error) (repeat bool, err error) {
	caller, ok := CallerFrom(ctx)
	if !ok || caller.System == "" || caller.UserID != uuid.Nil {
		return false, errors.New("store: a delivery is received by a system caller")
	}
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := setContext(ctx, tx, tenantID, caller, JobGitHubWebhook); err != nil {
		return false, err
	}
	w := &Writer{Reader: newReader(tx, tenantID, caller), Queries: writeq.New(tx)}
	_, err = w.RecordGitHubDelivery(ctx, writeq.RecordGitHubDeliveryParams{TenantID: tenantID, Delivery: delivery,
		ReceivedAt: now, ExpiresAt: now.Add(DeliveryRetention)})
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("record the delivery: %w", err)
	}
	if err := fn(w); err != nil {
		return false, err
	}
	for i := range w.events {
		w.events[i].IdempotencyKey = delivery
	}
	if err := w.writeEvents(ctx, tenantID, caller, Idempotency{}, false); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit transaction: %w", err)
	}
	return false, nil
}

// ExpireGitHubDeliveries deletes the deliveries whose day has passed
// (docs/adr/0071 D3) and records one act per run that removed any.
func (db *DB) ExpireGitHubDeliveries(ctx context.Context, now time.Time) (removed int64, err error) {
	_, err = db.RunJob(ctx, jobDeliveryExpiry, 7, func(w *Writer) error {
		n, err := w.DeleteExpiredGitHubDeliveries(ctx, now)
		if err != nil {
			return fmt.Errorf("delete expired deliveries: %w", err)
		}
		removed = n
		if n == 0 {
			return nil
		}
		w.Record(Event{EntityType: "github_deliveries", Action: actionExpired, After: map[string]int64{fieldRemoved: n}})
		return nil
	})
	return removed, err
}
