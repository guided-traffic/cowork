package api

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"

	"github.com/guided-traffic/cowork/backend/internal/problem"
)

func init() {
	// The Markdown export is text; the validator reads it as such
	// (docs/adr/0044 D1).
	openapi3filter.RegisterBodyDecoder("text/markdown", openapi3filter.PlainBodyDecoder)
	// An export is an archive, bytes (docs/adr/0051 D4).
	openapi3filter.RegisterBodyDecoder("application/gzip", openapi3filter.FileBodyDecoder)
	// format: uuid is checked at the boundary, so a malformed id in a path
	// names nothing and answers 404 (docs/adr/0047 D5). Any version: the
	// ids are UUIDv7, which the validator's RFC 4122 pattern refuses.
	openapi3.DefineStringFormatValidator("uuid", openapi3.NewRegexpFormatValidator(
		`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`))
}

// limitBody refuses a body of a type the operation does not declare
// (acceptedBody), and holds a JSON body to COWORK_MAX_JSON_BODY, an upload to
// COWORK_ATTACHMENT_MAX_BYTES and an import's upload to
// COWORK_MAX_IMPORT_BYTES, each with its multipart overhead (docs/adr/0039 D2,
// docs/adr/0051 D7): a declared length above it is refused before the body is
// read, and a body that turns out longer fails while it is read. Which limit
// holds is the operation's, as the API document declares its body, never the
// Content-Type the request names: an operation without a multipart body is
// held to the JSON limit, whatever the client says.
func (h *handler) limitBody(w http.ResponseWriter, r *http.Request, op *openapi3.Operation) *problem.Error {
	if perr := acceptedBody(r, op); perr != nil {
		return perr
	}
	limit := h.opts.MaxJSONBody
	if declaresMultipart(op) {
		max := h.opts.AttachmentMaxBytes
		if op.OperationID == opCreateImport {
			max = h.opts.MaxImportBytes
		}
		limit = 0
		if max > 0 {
			limit = max + multipartOverhead
		}
	}
	if !hasBody(r) || limit <= 0 {
		return nil
	}
	if r.ContentLength > limit {
		return tooLarge(limit)
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	return nil
}

// multipartOverhead is what an upload may carry beyond its file: the part
// headers, the boundaries and the comment field.
const multipartOverhead = 64 << 10

func tooLarge(limit int64) *problem.Error {
	return problem.New(problem.PayloadTooLarge, fmt.Sprintf("the body is larger than %d bytes", limit))
}

// hasBody reports whether a request carries a body to read: one of a declared
// length above zero, or one sent in chunks.
func hasBody(r *http.Request) bool {
	return r.Body != nil && r.Body != http.NoBody && r.ContentLength != 0
}

// declaresMultipart reports whether the API document gives the operation a
// multipart body: an upload, which the validator leaves to its handler.
func declaresMultipart(op *openapi3.Operation) bool {
	if op.RequestBody == nil || op.RequestBody.Value == nil {
		return false
	}
	for mediaType := range op.RequestBody.Value.Content {
		if strings.HasPrefix(mediaType, "multipart/") {
			return true
		}
	}
	return false
}

// acceptedBody refuses a body whose Content-Type the operation does not
// declare, with 415 and before a byte of it is read: the type decides the limit
// and whether the document validates the body, so a JSON body sent as
// multipart, say, would otherwise escape both. A request without a body, and
// an operation that takes none, are not looked at — the validator says
// whether a body is required.
func acceptedBody(r *http.Request, op *openapi3.Operation) *problem.Error {
	if op.RequestBody == nil || op.RequestBody.Value == nil || !hasBody(r) {
		return nil
	}
	content := op.RequestBody.Value.Content
	if content.Get(r.Header.Get("Content-Type")) != nil {
		return nil
	}
	return problem.New(problem.UnsupportedMediaType, "the body's Content-Type must be "+strings.Join(slices.Sorted(maps.Keys(content)), " or "))
}

// validateRequest holds the request to the API document (docs/adr/0046 D4,
// docs/adr/0047 D7) and refuses a query parameter the operation does not
// declare (docs/adr/0049 D4), which the validator would let pass — unless the
// operation takes such parameters (openQuery).
func (h *handler) validateRequest(r *http.Request, route *routers.Route, pathParams map[string]string) *problem.Error {
	if !openQuery(route.Operation) {
		if perr := unknownQueryParameters(r, route); perr != nil {
			return perr
		}
	}
	err := openapi3filter.ValidateRequest(r.Context(), requestInput(r, route, pathParams))
	if err == nil {
		return nil
	}
	return h.validationProblem(err)
}

func requestInput(r *http.Request, route *routers.Route, pathParams map[string]string) *openapi3filter.RequestValidationInput {
	return &openapi3filter.RequestValidationInput{
		Request:    r,
		PathParams: pathParams,
		Route:      unsecured(route),
		Options: &openapi3filter.Options{
			AuthenticationFunc: openapi3filter.NoopAuthenticationFunc,
			MultiError:         true,
			// An upload is read by its handler, part by part, under the
			// limits of its own; a signed body is read by its handler, whole
			// and unparsed, until its signature holds (docs/adr/0071 D3).
			// Both are the operation's, as the document declares it: a
			// request's Content-Type exempts no body from the validation.
			ExcludeRequestBody: declaresMultipart(route.Operation) || signed(route.Operation),
			// The handlers apply the defaults; a default written into the
			// request would look like a parameter the client sent.
			SkipSettingDefaults: true,
		},
	}
}

// unsecured is the route as the validator sees it: without a security
// requirement. The pipeline has authenticated the caller before it
// validates, and the validator's own security check reads the whole body
// into memory first — an upload before its role, scope and memory slot are
// checked (docs/adr/0039 D2, docs/adr/0016 D6).
func unsecured(route *routers.Route) *routers.Route {
	op := *route.Operation
	op.Security = &openapi3.SecurityRequirements{}
	out := *route
	out.Operation = &op
	return &out
}

// bodyDeadline holds reading the body to the request's deadline
// (docs/adr/0039 D2): a body that trickles in fails at the deadline instead
// of holding the request, and an upload's memory slot, as long as the client
// likes. The deadline is lifted once the body is read, so the connection's
// background read cannot end the request early.
func bodyDeadline(w http.ResponseWriter, r *http.Request) {
	deadline, ok := r.Context().Deadline()
	if !ok || r.Body == nil || r.Body == http.NoBody {
		return
	}
	rc := http.NewResponseController(w)
	if rc.SetReadDeadline(deadline) != nil {
		return
	}
	r.Body = &deadlineBody{ReadCloser: r.Body, lift: func() { _ = rc.SetReadDeadline(time.Time{}) }}
}

type deadlineBody struct {
	io.ReadCloser
	once sync.Once
	lift func()
}

func (b *deadlineBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil {
		b.once.Do(b.lift)
	}
	return n, err
}

func unknownQueryParameters(r *http.Request, route *routers.Route) *problem.Error {
	declared := map[string]bool{}
	for _, params := range []openapi3.Parameters{route.PathItem.Parameters, route.Operation.Parameters} {
		for _, p := range params {
			if p.Value != nil && p.Value.In == openapi3.ParameterInQuery {
				declared[p.Value.Name] = true
			}
		}
	}
	var unknown []problem.FieldError
	for _, name := range slices.Sorted(maps.Keys(r.URL.Query())) {
		if !declared[name] {
			unknown = append(unknown, problem.FieldError{Pointer: "query:" + name, Message: "unknown parameter"})
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	return &problem.Error{Code: problem.ValidationFailed, Detail: "the request has parameters this route does not know", Errors: unknown}
}

// validationProblem turns the validator's errors into errors[] entries. A
// path parameter that breaks its pattern names nothing that can exist and is
// answered like any unknown address, 404 (docs/adr/0047 D5); a body that hit
// the size limit is 413.
func (h *handler) validationProblem(err error) *problem.Error {
	var maxBytes *http.MaxBytesError
	if errors.As(err, &maxBytes) {
		return tooLarge(maxBytes.Limit)
	}
	var fields []problem.FieldError
	for _, e := range flatten(err) {
		var reqErr *openapi3filter.RequestError
		if !errors.As(e, &reqErr) {
			fields = append(fields, problem.FieldError{Pointer: "", Message: e.Error()})
			continue
		}
		if reqErr.Parameter != nil {
			if reqErr.Parameter.In == openapi3.ParameterInPath {
				return problem.New(problem.NotFound, "")
			}
			fields = append(fields, parameterError(reqErr))
			continue
		}
		fields = append(fields, bodyErrors(reqErr)...)
	}
	return &problem.Error{Code: problem.ValidationFailed, Detail: "the request does not match the API document", Errors: fields}
}

// parameterError names a parameter at in:name, one entry for the parameter
// (docs/adr/0047 D2), its failures read as a body's are: each the failure
// alone. The JSON Schema 2020-12 validator a 3.1 document uses writes the
// resource it compiles every schema under and the location in the value
// before the failure; the parameter is the location the client knows, and
// the failures of a repeated one's values share its message.
func parameterError(reqErr *openapi3filter.RequestError) problem.FieldError {
	failures := bodyErrors(reqErr)
	messages := make([]string, 0, len(failures))
	for _, f := range failures {
		messages = append(messages, f.Message)
	}
	return problem.FieldError{Pointer: reqErr.Parameter.In + ":" + reqErr.Parameter.Name, Message: strings.Join(messages, "; ")}
}

// bodyErrors returns a request error's failures, each at its field in the
// body.
func bodyErrors(reqErr *openapi3filter.RequestError) []problem.FieldError {
	var out []problem.FieldError
	for _, e := range flatten(reqErr.Err) {
		var schemaErr *openapi3.SchemaError
		if errors.As(e, &schemaErr) {
			out = append(out, schemaFields(schemaErr)...)
			continue
		}
		out = append(out, problem.FieldError{Pointer: "/", Message: e.Error()})
	}
	if len(out) == 0 {
		out = append(out, problem.FieldError{Pointer: "/", Message: reasonOf(reqErr)})
	}
	return out
}

// schemaFields returns the leaf failures of a schema error. The OpenAPI 3.0
// validator carries the location as a JSON pointer; the JSON Schema 2020-12
// validator a 3.1 document uses writes it into the reason, 'error at
// "/key": …', and nests the single failures in Origin.
func schemaFields(se *openapi3.SchemaError) []problem.FieldError {
	if ptr := se.JSONPointer(); len(ptr) > 0 {
		return []problem.FieldError{{Pointer: "/" + strings.Join(ptr, "/"), Message: se.Reason}}
	}
	var causes openapi3.MultiError
	if se.Origin != nil && errors.As(se.Origin, &causes) {
		var out []problem.FieldError
		for _, c := range causes {
			var sub *openapi3.SchemaError
			if errors.As(c, &sub) {
				out = append(out, schemaFields(sub)...)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	pointer, message := locateReason(se.Reason)
	return []problem.FieldError{{Pointer: pointer, Message: message}}
}

// locateReason splits 'error at "/key": at '/key': message' into the pointer
// and the message.
func locateReason(reason string) (string, string) {
	const prefix = `error at "`
	pointer, message := "/", reason
	if rest, ok := strings.CutPrefix(reason, prefix); ok {
		if path, msg, found := strings.Cut(rest, `": `); found {
			if path != "" {
				pointer = path
			}
			message = msg
		}
	}
	if rest, ok := strings.CutPrefix(message, "at '"); ok {
		if _, msg, found := strings.Cut(rest, "': "); found {
			message = msg
		}
	}
	return pointer, message
}

// reasonOf is the reason of a request error that carries no failure to walk:
// a schema's failures are read by bodyErrors.
func reasonOf(e *openapi3filter.RequestError) string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return e.Reason
}

// flatten unrolls the validator's multi-errors, and only those: a
// RequestError unwraps to its cause and must stay whole.
func flatten(err error) []error {
	if multi, ok := err.(openapi3.MultiError); ok { //nolint:errorlint // only the error itself, not its causes
		out := make([]error, 0, len(multi))
		for _, e := range multi {
			out = append(out, flatten(e)...)
		}
		return out
	}
	if err == nil {
		return nil
	}
	return []error{err}
}

// serveValidated serves the request and holds the response to the document
// before it leaves: a response the document does not describe is answered as
// a 500 that names the mismatch. On in the tests only (docs/adr/0046 D4).
func (h *handler) serveValidated(w http.ResponseWriter, r *http.Request, route *routers.Route, pathParams map[string]string) {
	rec := httptest.NewRecorder()
	maps.Copy(rec.Header(), w.Header())
	body, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(body))
	h.mux.ServeHTTP(rec, r)

	validationReq := r.Clone(r.Context())
	validationReq.Body = io.NopCloser(bytes.NewReader(body))
	in := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: requestInput(validationReq, route, pathParams),
		Status:                 rec.Code,
		Header:                 rec.Header(),
		Options:                &openapi3filter.Options{IncludeResponseStatus: true, MultiError: true},
	}
	in.SetBodyBytes(rec.Body.Bytes())
	if err := openapi3filter.ValidateResponse(r.Context(), in); err != nil {
		h.logger.Error("the response does not match the API document", "method", r.Method, "path", r.URL.Path, "error", err)
		problem.Write(w, r, problem.New(problem.Internal, "the response does not match the API document: "+err.Error()))
		return
	}
	maps.Copy(w.Header(), rec.Header())
	w.WriteHeader(rec.Code)
	_, _ = w.Write(rec.Body.Bytes())
}
