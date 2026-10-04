package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/guided-traffic/cowork/backend/internal/store/readq"
	"github.com/guided-traffic/cowork/backend/internal/store/writeq"
)

// TokenRecord is a presented token and its person, as the resolver needs them.
type TokenRecord struct {
	Token  readq.GetTokenByHashRow
	Person readq.GetUserRow
}

// LookupToken finds the token whose SHA-256 a request presented, and its
// person. The lookup transaction names the hash in app.token_hash, which the
// tokens policy admits exactly that row for (docs/adr/0021 D6); the person is
// read once the token names it. An unknown hash is ErrNotFound.
func (db *DB) LookupToken(ctx context.Context, hash [sha256.Size]byte) (TokenRecord, error) {
	var rec TokenRecord
	tx, err := db.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return rec, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SELECT set_config('app.token_hash', $1, true)", hex.EncodeToString(hash[:])); err != nil {
		return rec, fmt.Errorf("set transaction context: %w", err)
	}
	q := readq.New(tx)
	rec.Token, err = q.GetTokenByHash(ctx, hash[:])
	if err != nil {
		return rec, notFound(err)
	}
	if err := setContext(ctx, tx, uuid.Nil, Caller{UserID: rec.Token.UserID}, ""); err != nil {
		return rec, err
	}
	rec.Person, err = q.GetUser(ctx, rec.Token.UserID)
	if err != nil {
		return rec, fmt.Errorf("read the token's person: %w", notFound(err))
	}
	if err := tx.Commit(ctx); err != nil {
		return rec, fmt.Errorf("commit transaction: %w", err)
	}
	return rec, nil
}

// RecordTokenRefusal records that a token was presented after its expiry or
// revocation (docs/adr/0035 D9): an installation-level act of the token's
// person, at most once per token, reason and hour; every refusal stays in
// the request log either way.
func (db *DB) RecordTokenRefusal(ctx context.Context, rec TokenRecord, reason string, requestID uuid.UUID, sourceHash []byte) error {
	ctx = WithCaller(ctx, Caller{UserID: rec.Token.UserID, TokenID: rec.Token.ID, TokenName: rec.Token.Name,
		RequestID: requestID, SourceHash: sourceHash})
	_, err := db.Mutate(ctx, uuid.Nil, func(w *Writer) error {
		n, err := w.CountRecentRefusals(ctx, readq.CountRecentRefusalsParams{TokenID: &rec.Token.ID, Reason: &reason})
		if err != nil {
			return fmt.Errorf("count recent refusals: %w", err)
		}
		if n > 0 {
			return ErrNoChange
		}
		w.Record(Event{EntityType: "token", EntityID: rec.Token.ID, Action: "refused", Reason: reason})
		return nil
	})
	if errors.Is(err, ErrNoChange) {
		return nil
	}
	return err
}

// TouchTokenLastUsed sets a token's last-used date to day (UTC). It is
// bookkeeping, not an act, and one of the three writes outside Mutate
// (docs/adr/0035 D2, docs/adr/0027 D3), at most one write per token and day.
func (db *DB) TouchTokenLastUsed(ctx context.Context, userID, tokenID uuid.UUID, day time.Time) error {
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := setContext(ctx, tx, uuid.Nil, Caller{UserID: userID}, ""); err != nil {
		return err
	}
	y, m, d := day.UTC().Date()
	date := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	if err := writeq.New(tx).TouchTokenLastUsed(ctx, writeq.TouchTokenLastUsedParams{
		TokenID: tokenID,
		Day:     &date,
	}); err != nil {
		return fmt.Errorf("touch token: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}
