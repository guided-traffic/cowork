package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
)

type apiInput struct {
	Method  string `json:"method"`
	Path    string `json:"path" jsonschema:"the path under the installation, starting /api/v1/, with its query; /api/v1/openapi.json is the document"`
	Body    any    `json:"body,omitempty" jsonschema:"the JSON body of a POST, PUT or PATCH"`
	IfMatch string `json:"if_match,omitempty" jsonschema:"the ETag an overwriting write sends in If-Match"`
}

// maxAPIAnswer bounds what the escape hatch hands the model.
const maxAPIAnswer = 100_000

func apiTool() Tool {
	return define(Tool{
		Name: "api",
		Description: "Call any route of the cowork API with this session's token and limits — the escape hatch for what no " +
			"other tool does (docs/adr/0042 D1). Answers the status and the API's JSON unchanged; GET /api/v1/openapi.json " +
			"is the contract. A POST carries an Idempotency-Key the tool makes. Only paths under /api/v1/ of this " +
			"installation; the token goes nowhere else.",
		Operations: []string{"getOpenAPI"},
		limits: limitsOf("The same rules hold as everywhere: an agent never deletes, books time, overrides prerequisites or "+
			"administers members, tokens or tenants, and decide, close, drop, rank, override-urgency, interest, upload, "+
			"create-project and record-answer are capabilities. "+refusalNote,
			capDecide, capClose, capDrop, "rank", "override-urgency", capInterest, "upload", capCreateProject, capRecordAnswer),
	}, func(s *jsonschema.Schema) {
		enum(s, "method", http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete)
	}, runAPI)
}

// apiPath accepts a path under /api/v1/ of the installation and nothing
// else: no scheme, no host, no step out, so the token is sent to this
// installation's API only.
func apiPath(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	switch {
	case err != nil:
		return "", usage("the path is no URL path: %v", err)
	case u.Scheme != "" || u.Host != "" || u.User != nil || u.Opaque != "":
		return "", usage("the path names a host; give the path under the installation, /api/v1/…")
	case !strings.HasPrefix(u.Path, "/api/v1/"):
		return "", usage("the path must start with /api/v1/")
	case path.Clean(u.Path) != strings.TrimSuffix(u.Path, "/") || strings.Contains(u.Path, "//"):
		return "", usage("the path must be clean: no . or .. segments, no empty ones")
	}
	return u.RequestURI(), nil
}

func runAPI(ctx context.Context, s *Session, in apiInput) (string, error) {
	target, err := apiPath(in.Path)
	if err != nil {
		return "", err
	}
	c, ok := s.API.ClientInterface.(*apigen.Client)
	if !ok {
		return "", fmt.Errorf("the session's client cannot send a request of its own")
	}
	req, err := apiRequest(ctx, s, c, in, target)
	if err != nil {
		return "", err
	}
	res, err := c.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = res.Body.Close() }()
	return apiAnswer(in.Method+" "+target, res)
}

// apiRequest is the request of the escape hatch with the session's own
// headers: the token and the agent mark, and a key on a POST.
func apiRequest(ctx context.Context, s *Session, c *apigen.Client, in apiInput, target string) (*http.Request, error) {
	var body io.Reader
	if in.Body != nil {
		raw, err := json.Marshal(in.Body)
		if err != nil {
			return nil, usage("the body is no JSON: %v", err)
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, in.Method, strings.TrimRight(c.Server, "/")+target, body)
	if err != nil {
		return nil, fmt.Errorf("build the request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if in.Method == http.MethodPost {
		req.Header.Set("Idempotency-Key", s.key().String())
	}
	if in.IfMatch != "" {
		req.Header.Set("If-Match", in.IfMatch)
	}
	for _, edit := range c.RequestEditors {
		if err := edit(ctx, req); err != nil {
			return nil, err
		}
	}
	return req, nil
}

// apiAnswer is the status and the body unchanged, cut to a bound; an error
// status is the tool's failure with the same text.
func apiAnswer(request string, res *http.Response) (string, error) {
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxAPIAnswer+1))
	if err != nil {
		return "", fmt.Errorf("read the answer: %w", err)
	}
	text := request + " → " + res.Status
	if etag := res.Header.Get("ETag"); etag != "" {
		text += " (ETag " + etag + ")"
	}
	if len(raw) > maxAPIAnswer {
		raw = append(raw[:maxAPIAnswer], []byte("\n… (cut at 100000 bytes)")...)
	}
	if len(raw) > 0 {
		text += "\n\n" + string(raw)
	}
	if res.StatusCode >= http.StatusBadRequest {
		return "", textError(text)
	}
	return text, nil
}

// textError is a failure whose text is the answer itself.
type textError string

func (e textError) Error() string { return string(e) }
