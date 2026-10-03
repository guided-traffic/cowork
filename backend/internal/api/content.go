package api

import (
	"context"
	"encoding/json"
	"mime"
	"strings"
)

type acceptKey struct{}

// withAccept keeps the request's Accept header for the handlers that answer
// in more than one media type.
func withAccept(ctx context.Context, accept string) context.Context {
	return context.WithValue(ctx, acceptKey{}, accept)
}

// wantsCSV reports whether the request asked for text/csv.
func wantsCSV(ctx context.Context) bool {
	accept, _ := ctx.Value(acceptKey{}).(string)
	for _, part := range strings.Split(accept, ",") {
		mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(part))
		if err == nil && mediaType == "text/csv" {
			return true
		}
	}
	return false
}

// jsonValue decodes a stored JSON document for a response.
func jsonValue(raw []byte) any {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil
	}
	return v
}
