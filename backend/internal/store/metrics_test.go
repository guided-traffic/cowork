package store

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/metrics"
)

// docs/adr/0060 D4, D5: a failed statement is counted by a kind of a closed
// set — the SQLSTATE's meaning, the request's end, a broken connection —, never
// by its text or its arguments.
func TestAFailedStatementIsCountedByItsKind(t *testing.T) {
	state := func(code string) error {
		return fmt.Errorf("insert: %w", &pgconn.PgError{Code: code, Message: "secret detail"})
	}
	for want, err := range map[string]error{
		"unique_violation":       state("23505"),
		"foreign_key_violation":  state("23503"),
		"check_violation":        state("23514"),
		"not_null_violation":     state("23502"),
		"integrity_violation":    state("23P01"),
		"serialization_failure":  state("40001"),
		"deadlock_detected":      state("40P01"),
		"insufficient_privilege": state("42501"),
		"lock_not_available":     state("55P03"),
		"query_canceled":         state("57014"),
		"connection":             state("08006"),
		"other_sqlstate":         state("42P01"),
		"canceled":               fmt.Errorf("query: %w", context.Canceled),
		"timeout":                fmt.Errorf("query: %w", context.DeadlineExceeded),
		"other":                  errors.New("something else"),
	} {
		assert.Equal(t, want, queryErrorKind(err), "%v", err)
	}

	m := metrics.New()
	tracer := &slowQueryTracer{logger: slog.New(slog.DiscardHandler), threshold: DefaultSlowQuery, metrics: m}
	tracer.TraceQueryEnd(context.Background(), nil, pgx.TraceQueryEndData{Err: state("23505")})
	tracer.TraceQueryEnd(context.Background(), nil, pgx.TraceQueryEndData{Err: pgx.ErrNoRows})
	tracer.TraceQueryEnd(context.Background(), nil, pgx.TraceQueryEndData{})
	samples, err := m.Samples()
	require.NoError(t, err)
	assert.Equal(t, 1.0, metrics.Sum(samples, "cowork_db_query_errors_total"), "no rows is no failure, and a success none either")
	assert.Equal(t, 1.0, metrics.Sum(samples, "cowork_db_query_errors_total", "kind", "unique_violation"))
}

// docs/adr/0060 D4: an act counts for a system actor, a person's agent — a
// token flagged as one or a request the header marks —, or the person.
func TestAnActIsAttributedToItsActor(t *testing.T) {
	person := uuid.New()
	assert.Equal(t, metrics.ActorPerson, actorOf(Caller{UserID: person}, Event{}))
	assert.Equal(t, metrics.ActorAgent, actorOf(Caller{UserID: person, Agent: "claude-code/opus/1"}, Event{}))
	assert.Equal(t, metrics.ActorSystem, actorOf(Caller{System: "system:ticket-purge"}, Event{}))
	assert.Equal(t, metrics.ActorSystem, actorOf(Caller{UserID: person, Agent: "chat/m/c"}, Event{System: systemIdentity}),
		"the identity provider's derivation in an administrator's request")
}
