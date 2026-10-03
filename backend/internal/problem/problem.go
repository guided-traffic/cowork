// Package problem is the error shape of the API: RFC 9457 problem details
// with a stable code, the request id and field errors (docs/adr/0047).
//
// The catalogue below is the one place a code is defined (D4); `make
// generate` writes the OpenAPI enum of the codes and the README's code table
// from it, so the three cannot disagree.
package problem

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/guided-traffic/cowork/backend/internal/requestid"
)

// TypeBase is the prefix of every problem type URI. The URI is an identifier,
// not a link that must resolve (docs/adr/0047 D1).
const TypeBase = "https://cowork.dev/problems/"

// Code is one entry of the catalogue.
type Code struct {
	// Code is the stable snake_case identifier clients switch on.
	Code string
	// Status is the HTTP status the code is answered with.
	Status int
	// Title is the code in words.
	Title string
	// Meaning says when the code is answered; it is the README's text.
	Meaning string
}

// Type is the code's type URI.
func (c Code) Type() string {
	return TypeBase + strings.ReplaceAll(c.Code, "_", "-")
}

// The catalogue, in the order of the README table.
var (
	ValidationFailed       = Code{"validation_failed", http.StatusBadRequest, "Validation failed", "The request does not match the API document, or a field breaks a rule; `errors[]` names each field"}
	IdempotencyKeyRequired = Code{"idempotency_key_required", http.StatusBadRequest, "Idempotency key required", "An agent's `POST` that creates something came without an `Idempotency-Key`; a transition needs none (docs/adr/0045 D2, D3)"}
	InvalidCursor          = Code{"invalid_cursor", http.StatusBadRequest, "Invalid cursor", "The cursor was altered, belongs to another list, or comes from another installation"}
	PageTooDeep            = Code{"page_too_deep", http.StatusBadRequest, "Page too deep", "A numbered page beyond the depth cap; follow the cursor instead"}
	Unauthenticated        = Code{"unauthenticated", http.StatusUnauthorized, "Unauthenticated", "No token, a malformed one, or one cowork does not know"}
	TokenExpired           = Code{"token_expired", http.StatusUnauthorized, "Token expired", "The token is past its expiry (docs/adr/0035 D4)"}
	TokenRevoked           = Code{"token_revoked", http.StatusUnauthorized, "Token revoked", "The token was revoked, or its person deactivated (docs/adr/0035 D6)"}
	Forbidden              = Code{"forbidden", http.StatusForbidden, "Forbidden", "The person's role does not allow the act (docs/adr/0034)"}
	InsufficientScope      = Code{"insufficient_scope", http.StatusForbidden, "Insufficient scope", "The token's scope does not reach the act (docs/adr/0035 D3)"}
	AgentForbidden         = Code{"agent_forbidden", http.StatusForbidden, "Agent forbidden", "The act is on the agent hard-off list or needs a capability the token lacks; `detail` names which (docs/adr/0043 D5)"}
	NotFound               = Code{"not_found", http.StatusNotFound, "Not found", "No such route, or a tenant, project or ticket the caller cannot see — the answer does not say which (docs/adr/0047 D5)"}
	MethodNotAllowed       = Code{"method_not_allowed", http.StatusMethodNotAllowed, "Method not allowed", "The path exists with other methods; `Allow` names them"}
	ProjectKeyTaken        = Code{"project_key_taken", http.StatusConflict, "Project key taken", "The tenant has a project with this key; keys are never reused (docs/adr/0007 D4)"}
	ProjectArchived        = Code{"project_archived", http.StatusConflict, "Project archived", "An archived project refuses new tickets (docs/adr/0006 D4)"}
	StateConflict          = Code{"state_conflict", http.StatusConflict, "State conflict", "The ticket is not in the state the request assumed, or its state does not allow the change; `errors[]` names the current state (docs/adr/0045 D2)"}
	ParentCycle            = Code{"parent_cycle", http.StatusConflict, "Parent cycle", "The new parent is the ticket itself or one of its descendants (docs/adr/0008 D2)"}
	LinkCycle              = Code{"link_cycle", http.StatusConflict, "Link cycle", "The blocks link would close a cycle of prerequisites (docs/adr/0012 D4)"}
	AttachmentLimit        = Code{"attachment_limit", http.StatusConflict, "Attachment limit", "The ticket holds as many attachments as COWORK_ATTACHMENT_MAX_PER_TICKET allows (docs/adr/0016 D6)"}
	UploadsDisabled        = Code{"uploads_disabled", http.StatusNotImplemented, "Uploads disabled", "The installation has no object storage configured; attachments cannot be uploaded (docs/adr/0016 D1)"}
	PeriodLocked           = Code{"period_locked", http.StatusConflict, "Period locked", "The day lies on or before the tenant's time_locked_until: the period is closed to new, changed and voided entries (docs/adr/0017 D8)"}
	OpenPrerequisites      = Code{"open_prerequisites", http.StatusConflict, "Open prerequisites", "Tickets that block this one are not done or dropped; `errors[]` lists them, and a person may override with a reason (docs/adr/0012 D7)"}
	PreconditionFailed     = Code{"precondition_failed", http.StatusPreconditionFailed, "Precondition failed", "The `If-Match` version is stale; the response carries the current `ETag` and `errors[]` the current values (docs/adr/0050 D5)"}
	PayloadTooLarge        = Code{"payload_too_large", http.StatusRequestEntityTooLarge, "Payload too large", "The body is larger than the configured limit (docs/adr/0039 D2)"}
	UnsupportedMediaType   = Code{"unsupported_media_type", http.StatusUnsupportedMediaType, "Unsupported media type", "The body's type is not one the route accepts"}
	IdempotencyMismatch    = Code{"idempotency_mismatch", http.StatusUnprocessableEntity, "Idempotency mismatch", "The `Idempotency-Key` was used before with a different request (docs/adr/0045 D4)"}
	PreconditionRequired   = Code{"precondition_required", http.StatusPreconditionRequired, "Precondition required", "An overwriting write came without `If-Match` (docs/adr/0050 D3)"}
	Internal               = Code{"internal", http.StatusInternalServerError, "Internal error", "Something failed inside cowork; the `request_id` finds it in the log"}
	NotReady               = Code{"not_ready", http.StatusServiceUnavailable, "Not ready", "The backend cannot reach its database"}
	Timeout                = Code{"timeout", http.StatusGatewayTimeout, "Timeout", "The request took longer than the configured limit (docs/adr/0039 D2)"}
	// BackendUnreachable is answered by the frontend's nginx, never by the
	// backend: its static problem body for a backend it cannot reach
	// (docs/adr/0047 D6).
	BackendUnreachable = Code{"backend_unreachable", http.StatusBadGateway, "Backend unreachable", "The frontend's proxy could not reach the backend; answered by nginx without a request id (docs/adr/0047 D6)"}
)

// Catalogue lists every code; the generators read it.
var Catalogue = []Code{
	ValidationFailed, IdempotencyKeyRequired, InvalidCursor, PageTooDeep,
	Unauthenticated, TokenExpired, TokenRevoked,
	Forbidden, InsufficientScope, AgentForbidden,
	NotFound, MethodNotAllowed, ProjectKeyTaken, ProjectArchived, StateConflict, ParentCycle, LinkCycle, OpenPrerequisites, PeriodLocked, AttachmentLimit, UploadsDisabled,
	PreconditionFailed, PayloadTooLarge,
	UnsupportedMediaType, IdempotencyMismatch, PreconditionRequired,
	Internal, NotReady, Timeout, BackendUnreachable,
}

// FieldError is one entry of errors[] (docs/adr/0047 D2, docs/adr/0050 D5).
type FieldError struct {
	// Pointer is a JSON pointer into the body, or query:<name> /
	// header:<name>.
	Pointer string `json:"pointer"`
	Message string `json:"message"`
	// Current is the server's value of the field, on a 412.
	Current any `json:"current,omitempty"`
}

// Body is the problem details body.
type Body struct {
	Type      string       `json:"type"`
	Title     string       `json:"title"`
	Status    int          `json:"status"`
	Detail    string       `json:"detail,omitempty"`
	Instance  string       `json:"instance,omitempty"`
	Code      string       `json:"code"`
	RequestID string       `json:"request_id,omitempty"`
	Errors    []FieldError `json:"errors,omitempty"`
}

// Error is a problem a handler returns instead of a response; the API
// renders it. detail is for a person and never carries a secret, SQL, a stack
// trace or an internal path (docs/adr/0047 D3).
type Error struct {
	Code    Code
	Detail  string
	Errors  []FieldError
	Headers map[string]string
}

// New returns a problem with a detail.
func New(code Code, detail string) *Error {
	return &Error{Code: code, Detail: detail}
}

// Field returns a validation problem for one field.
func Field(pointer, message string) *Error {
	return &Error{Code: ValidationFailed, Detail: message, Errors: []FieldError{{Pointer: pointer, Message: message}}}
}

func (e *Error) Error() string {
	if e.Detail == "" {
		return e.Code.Code
	}
	return e.Code.Code + ": " + e.Detail
}

// Write answers r with the problem.
func Write(w http.ResponseWriter, r *http.Request, e *Error) {
	for k, v := range e.Headers {
		w.Header().Set(k, v)
	}
	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(e.Code.Status)
	body := Body{
		Type:   e.Code.Type(),
		Title:  e.Code.Title,
		Status: e.Code.Status,
		Detail: e.Detail,
		Code:   e.Code.Code,
		Errors: e.Errors,
	}
	if r != nil {
		body.Instance = r.URL.Path
		body.RequestID = requestid.From(r.Context())
	}
	_ = json.NewEncoder(w).Encode(body)
}
