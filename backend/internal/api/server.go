package api

import (
	"context"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/problem"
	"github.com/guided-traffic/cowork/backend/internal/storage"
	"github.com/guided-traffic/cowork/backend/internal/store"
)

// The entity types and field names the audit rows of this package record.
const (
	entityProject        = "project"
	entityTenant         = "tenant"
	entityUser           = "user"
	entityMembership     = "membership"
	entityToken          = "token"
	fieldName            = "name"
	fieldDescription     = "description"
	fieldTimeLockedUntil = "time_locked_until"
	fieldWipLimits       = "wip_limits"
	fieldAssignee        = "assignee"
	fieldBody            = "body"
	fieldUrgencyOverride = "urgency_override"
	fieldType            = "type"
	fieldParent          = "parent"
	fieldNote            = "note"
	fieldSessionsEnded   = "sessions_ended"
	fieldSource          = "source"
	sourceGrant          = "grant"
	messageTaken         = "taken"
	fieldWeight          = "weight"
	fieldDay             = "day"
	fieldMinutes         = "minutes"
	fieldVersion         = "version"
)

// The acts the handlers record more than once (docs/adr/0026 D1) and the
// headers a stored response replays.
const (
	actionCreated    = "created"
	actionUpdated    = "updated"
	actionDeleted    = "deleted"
	actionLinked     = "linked"
	actionUnlinked   = "unlinked"
	actionOverridden = "overridden"
	actionEdited     = "edited"
	headerETag       = "ETag"
	headerLocation   = "Location"
)

// Server implements the generated strict server interface: one method per
// operation of the API document.
type Server struct {
	h       *handler
	db      *store.DB
	cursors cursorCodec
	// storage holds the attachments' bytes; nil without object storage.
	storage *storage.Client
	// uploads bounds the uploads buffered at once against the memory limit.
	uploads chan struct{}
}

var _ apigen.StrictServerInterface = (*Server)(nil)

// principal returns the authenticated caller; the pipeline guarantees one for
// every operation that declares the bearer scheme.
func principal(ctx context.Context) auth.Principal {
	p, _ := auth.PrincipalFrom(ctx)
	return p
}

// GetVersion answers the build (docs/adr/0040 D5).
func (s *Server) GetVersion(context.Context, apigen.GetVersionRequestObject) (apigen.GetVersionResponseObject, error) {
	return apigen.GetVersion200JSONResponse{
		Version:   s.h.opts.Version,
		Commit:    s.h.opts.Commit,
		BuildTime: s.h.opts.BuildTime,
	}, nil
}

// GetOpenAPI answers the document with the backend version
// (docs/adr/0046 D5).
func (s *Server) GetOpenAPI(context.Context, apigen.GetOpenAPIRequestObject) (apigen.GetOpenAPIResponseObject, error) {
	var doc apigen.GetOpenAPI200JSONResponse
	if err := json.Unmarshal(s.h.served, &doc); err != nil {
		return nil, fmt.Errorf("decode the API document: %w", err)
	}
	return doc, nil
}

// etag is the strong validator of a version (docs/adr/0050 D2).
func etag(version int32) *string {
	v := `"` + strconv.FormatInt(int64(version), 10) + `"`
	return &v
}

// ifMatch reads the version an overwriting write was based on
// (docs/adr/0050 D3): none is 428, a weak or unreadable one is 412.
func ifMatch(header *string) (int32, *problem.Error) {
	if header == nil || strings.TrimSpace(*header) == "" || strings.TrimSpace(*header) == "*" {
		return 0, problem.New(problem.PreconditionRequired, "this write overwrites; send If-Match with the ETag you read")
	}
	v := strings.TrimSpace(*header)
	if strings.HasPrefix(v, "W/") {
		return 0, problem.New(problem.PreconditionFailed, "a weak ETag never matches; send the strong ETag of the entity")
	}
	n, err := strconv.ParseInt(strings.Trim(v, `"`), 10, 32)
	if err != nil {
		return 0, problem.New(problem.PreconditionFailed, "If-Match is not an ETag of this entity")
	}
	return int32(n), nil
}

// stale is the 412 of an overwriting write whose version moved: the current
// ETag in the header and, per field the request tried to change, the current
// value in errors[] (docs/adr/0050 D5).
func stale(version int32, current map[string]any) *problem.Error {
	e := &problem.Error{
		Code:    problem.PreconditionFailed,
		Detail:  "the entity changed since you read it; merge and send the current ETag",
		Headers: map[string]string{headerETag: *etag(version)},
	}
	for _, field := range sortedKeys(current) {
		v := current[field]
		if v == nil {
			// An empty field is reported as null, not left out.
			v = json.RawMessage("null")
		}
		e.Errors = append(e.Errors, problem.FieldError{Pointer: "/" + field, Message: "changed since you read it", Current: v})
	}
	return e
}

// keyed prepares a POST for idempotency (docs/adr/0045 D3, D4): an agent's
// POST must carry a key; with a key, the context makes store.Mutate store the
// response with the act and replay it for a repetition. The fingerprint binds
// the key to the operation, its scope and its body.
func (s *Server) keyed(ctx context.Context, key *uuid.UUID, op, scope string, body any) (context.Context, *problem.Error) {
	if key == nil {
		if principal(ctx).IsAgent() {
			return ctx, &problem.Error{Code: problem.IdempotencyKeyRequired, Detail: "an agent's POST needs an Idempotency-Key",
				Errors: []problem.FieldError{{Pointer: "header:Idempotency-Key", Message: "required for agents"}}}
		}
		return ctx, nil
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return ctx, problem.New(problem.Internal, "internal error")
	}
	return store.WithIdempotency(ctx, store.Idempotency{Key: *key, Fingerprint: s.fingerprint(op, scope, encoded)}), nil
}

// fingerprint is what the database keeps of a keyed request for a day: an
// HMAC under a key derived from the server key, not a plain hash. The body of
// a creation can carry a secret — a local account's temporary password — and
// a plain hash of it in a backup could be guessed at the speed of SHA-256,
// against the Argon2id the password is stored with (docs/adr/0045 D4).
func (s *Server) fingerprint(op, scope string, body []byte) [sha256.Size]byte {
	mac := hmac.New(sha256.New, s.h.fingerprintKey)
	mac.Write([]byte(op + "\n" + scope + "\n"))
	mac.Write(body)
	var fp [sha256.Size]byte
	copy(fp[:], mac.Sum(nil))
	return fp
}

// newFingerprintKey derives the key of the fingerprints from the server key,
// apart from every other key derived from it.
func newFingerprintKey(sessionKey []byte) []byte {
	key, err := hkdf.Key(sha256.New, sessionKey, nil, "cowork idempotency fingerprint v1", sha256.Size)
	if err != nil {
		panic(err) // only an impossible key length fails
	}
	return key
}

// stored returns the response a keyed creation stores: 201 with its JSON
// body and headers.
func stored(body any, headers map[string]string) (store.Result, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return store.Result{}, fmt.Errorf("encode the stored response: %w", err)
	}
	return store.Result{Status: http.StatusCreated, Headers: headers, Body: b}, nil
}

// replayed decodes a replayed response body.
func replayed[T any](res *store.Result) (T, error) {
	var v T
	if err := json.Unmarshal(res.Body, &v); err != nil {
		return v, fmt.Errorf("decode the replayed response: %w", err)
	}
	return v, nil
}

func header(res *store.Result, name string) *string {
	if v, ok := res.Headers[name]; ok {
		return &v
	}
	return nil
}
