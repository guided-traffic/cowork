package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/guided-traffic/cowork/backend/internal/store/readq"
)

// ChatCapabilities reads the capabilities a person gave the chat in the UI
// (docs/adr/0043 D5): the set the person chose and true, or nil and false for
// a person who never chose, whom the default holds. It reads in a transaction
// of its own as the person, whose row is the only one the policy admits: the
// session's resolver asks it for every request the agent header marks, before
// the request has a caller.
func (db *DB) ChatCapabilities(ctx context.Context, person uuid.UUID) ([]string, bool, error) {
	tx, err := db.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, false, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := setContext(ctx, tx, uuid.Nil, Caller{UserID: person}, ""); err != nil {
		return nil, false, err
	}
	row, err := readq.New(tx).GetChatCapabilities(ctx, person)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read the chat's capabilities: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, false, fmt.Errorf("commit transaction: %w", err)
	}
	return row.Capabilities, true, nil
}
