package store

import (
	"context"
	"crypto/sha256"

	"github.com/google/uuid"
)

// Caller is who a transaction acts for: a person, with the token and agent
// facts its audit rows record (docs/adr/0026 D1, docs/adr/0036), or a system
// actor for background work (docs/adr/0027 D5). The request layer puts it
// into the context after authentication; the wrappers read it from there, so
// the person a query runs for is never a call-site argument
// (docs/adr/0034 D4).
type Caller struct {
	// UserID is the person; uuid.Nil for a system caller.
	UserID uuid.UUID
	// System names a system actor, "system:<name>"; empty for a person.
	System string
	// TokenID is the personal access token the request presented.
	TokenID uuid.UUID
	// RestrictedProjectID is the token's project restriction
	// (docs/adr/0035 D3); the visibility predicate admits that project only.
	RestrictedProjectID uuid.UUID
	// Agent is the agent mark of the request: the validated X-Cowork-Agent
	// value, "unknown-agent", or empty for a person's own request.
	Agent string
	// Capabilities is the agent's capability set, recorded on its acts
	// (docs/adr/0043 D5).
	Capabilities []string
	// RequestID is the request's id (docs/adr/0047 D2).
	RequestID uuid.UUID
}

type callerKey struct{}

// WithCaller returns a context that carries c.
func WithCaller(ctx context.Context, c Caller) context.Context {
	return context.WithValue(ctx, callerKey{}, c)
}

// CallerFrom returns the caller the context carries.
func CallerFrom(ctx context.Context) (Caller, bool) {
	c, ok := ctx.Value(callerKey{}).(Caller)
	return c, ok
}

// Idempotency is a keyed POST's key and the fingerprint of the request it
// came with (docs/adr/0045 D3, D4).
type Idempotency struct {
	Key         uuid.UUID
	Fingerprint [sha256.Size]byte
}

type idempotencyKey struct{}

// WithIdempotency returns a context whose Mutate stores its act's response
// under i, or replays the response stored for an earlier request with i.
func WithIdempotency(ctx context.Context, i Idempotency) context.Context {
	return context.WithValue(ctx, idempotencyKey{}, i)
}

func idempotencyFrom(ctx context.Context) (Idempotency, bool) {
	i, ok := ctx.Value(idempotencyKey{}).(Idempotency)
	return i, ok
}
