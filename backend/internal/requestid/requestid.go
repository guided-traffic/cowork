// Package requestid carries the id of a request through its context: it is
// the X-Request-Id response header, the request_id of a problem body, the
// field of the request's log line and the request_id of its audit rows
// (docs/adr/0047 D2, docs/adr/0026 D1).
package requestid

import (
	"context"

	"github.com/google/uuid"
)

type key struct{}

// New returns a fresh request id, a UUIDv7.
func New() uuid.UUID {
	return uuid.Must(uuid.NewV7())
}

// With returns a context that carries id.
func With(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, key{}, id)
}

// From returns the request id the context carries, or "" when it carries none.
func From(ctx context.Context) string {
	if id, ok := ctx.Value(key{}).(uuid.UUID); ok {
		return id.String()
	}
	return ""
}

// UUID returns the request id the context carries, or uuid.Nil.
func UUID(ctx context.Context) uuid.UUID {
	id, _ := ctx.Value(key{}).(uuid.UUID)
	return id
}
