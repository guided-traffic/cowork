package api

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/markdown"
	"github.com/guided-traffic/cowork/backend/internal/store"
	"github.com/guided-traffic/cowork/backend/internal/store/readq"
)

// oneRow answers every single-row read with the values or the error it holds;
// the reads of many rows are not called.
type oneRow struct {
	readq.DBTX
	values []any
	err    error
}

func (o oneRow) QueryRow(context.Context, string, ...any) pgx.Row { return o }

func (o oneRow) Scan(dest ...any) error {
	if o.err != nil {
		return o.err
	}
	for i, v := range o.values {
		*dest[i].(**string) = v.(*string)
	}
	return nil
}

func readerOf(db readq.DBTX) *store.Reader { return &store.Reader{Queries: readq.New(db)} }

// docs/adr/0044 D1: the document writes the assignee's name with the identity,
// both or neither. A person who leaves the reader's sight between the ticket
// row and the read of the identity — a membership removed under READ
// COMMITTED — is written by neither, and the export does not fail.
func TestExportAssignee(t *testing.T) {
	ctx := context.Background()
	id, name, username := uuid.New(), "Ada Lovelace", "ada"
	row := store.TicketRow{AssigneeID: &id, AssigneeName: &name}

	p, err := exportAssignee(ctx, readerOf(oneRow{values: []any{&username, (*string)(nil), (*string)(nil)}}), row)
	require.NoError(t, err)
	assert.Equal(t, markdown.Person{Name: name, Username: username}, p)

	p, err = exportAssignee(ctx, readerOf(oneRow{err: pgx.ErrNoRows}), row)
	require.NoError(t, err, "a person out of sight is no failure")
	assert.Equal(t, markdown.Person{}, p, "neither the name nor the identity")

	broken := errors.New("connection reset")
	_, err = exportAssignee(ctx, readerOf(oneRow{err: broken}), row)
	require.ErrorIs(t, err, broken, "any other error fails the export")

	p, err = exportAssignee(ctx, readerOf(oneRow{err: broken}), store.TicketRow{AssigneeID: &id})
	require.NoError(t, err, "a name the ticket row did not show reads no identity")
	assert.Equal(t, markdown.Person{}, p)
}
