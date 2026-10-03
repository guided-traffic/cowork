package tools

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
)

// response is what every generated response offers.
type response interface {
	StatusCode() int
	GetBody() []byte
	GetApplicationproblemJSONDefault() *apigen.Problem
}

// APIError is an answer of the API that is not the one a tool wanted: its
// status and the problem it carried, surfaced with its code, never retried
// into success (docs/adr/0040 D3, docs/adr/0047).
type APIError struct {
	Status  int
	Problem *apigen.Problem
	// Body is the answer when it carried no problem.
	Body string
}

func (e *APIError) Error() string {
	if e.Problem == nil {
		return fmt.Sprintf("cowork answered %d", e.Status)
	}
	return fmt.Sprintf("cowork answered %d %s: %s", e.Status, e.Problem.Code, e.detail())
}

// Code is the problem's code, or "" for an answer without one.
func (e *APIError) Code() string {
	if e.Problem == nil {
		return ""
	}
	return string(e.Problem.Code)
}

func (e *APIError) detail() string {
	if e.Problem.Detail != nil && *e.Problem.Detail != "" {
		return *e.Problem.Detail
	}
	return e.Problem.Title
}

// Markdown is the error as a tool answers it: the code and the message the
// API gave, the fields it named, and, for a refusal of the agent rules, that
// it is a refusal and not a failure of the tool (docs/adr/0042 D3).
func (e *APIError) Markdown() string {
	if e.Problem == nil {
		body := strings.TrimSpace(e.Body)
		if len(body) > 500 {
			body = body[:500] + "…"
		}
		return fmt.Sprintf("cowork answered %d without a problem body. %s", e.Status, body)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "cowork answered %d `%s`: %s", e.Status, e.Problem.Code, e.detail())
	if e.Problem.Errors != nil {
		for _, fe := range *e.Problem.Errors {
			fmt.Fprintf(&b, "\n- `%s`: %s", fe.Pointer, fe.Message)
			if cur, err := fe.Current.Get(); err == nil && cur != nil {
				fmt.Fprintf(&b, " (now: %v)", cur)
			}
		}
	}
	if e.Problem.RequestId != nil {
		fmt.Fprintf(&b, "\n(request %s)", *e.Problem.RequestId)
	}
	switch e.Code() {
	case "agent_forbidden":
		b.WriteString("\n\nThis is the agent rules refusing the act, not a failure of the tool: the act is a person's, or needs a capability this token lacks. Tell the person what remains for them.")
	case "unauthenticated", "token_expired", "token_revoked":
		b.WriteString("\n\nThe token in COWORK_TOKEN does not work; the person makes a new one on the installation's token page.")
	}
	return b.String()
}

// check turns a generated call's outcome into an error unless the answer has
// one of the wanted statuses.
func check(res response, err error, want ...int) error {
	if err != nil {
		return err
	}
	if slices.Contains(want, res.StatusCode()) {
		return nil
	}
	return &APIError{Status: res.StatusCode(), Problem: res.GetApplicationproblemJSONDefault(), Body: string(res.GetBody())}
}

// Retrying sends a request once more after a transport failure — no answer at
// all — when sending it twice cannot act twice: a GET, PUT or DELETE, or a
// POST that carries an Idempotency-Key, which the API replays
// (docs/adr/0045 D5). An answer, whatever its status, is returned as it is
// (docs/adr/0040 D3).
type Retrying struct {
	Next apigen.HttpRequestDoer
	// Attempts is how often a request is sent at most; Wait the pause before
	// the second, doubled for each further one.
	Attempts int
	Wait     time.Duration
}

// Do sends the request.
func (r Retrying) Do(req *http.Request) (*http.Response, error) {
	wait := r.Wait
	for attempt := 1; ; attempt++ {
		res, err := r.Next.Do(req)
		if err == nil || attempt >= r.Attempts || !replayable(req) || req.Context().Err() != nil {
			return res, err
		}
		select {
		case <-req.Context().Done():
			return nil, err
		case <-time.After(wait):
		}
		wait *= 2
		if req.GetBody != nil {
			body, berr := req.GetBody()
			if berr != nil {
				return nil, err
			}
			req.Body = body
		}
	}
}

func replayable(req *http.Request) bool {
	switch req.Method {
	case http.MethodGet, http.MethodPut, http.MethodDelete:
		return true
	case http.MethodPost:
		return req.Header.Get("Idempotency-Key") != ""
	}
	return false
}

// HandlerDoer sends a request to an http.Handler in the same process and
// returns what it answers: a host inside the backend calls the API's own
// handler with it, through the whole pipeline, and the tests call a fake API.
type HandlerDoer struct{ Handler http.Handler }

// Do serves the request.
func (d HandlerDoer) Do(req *http.Request) (*http.Response, error) {
	rec := &recorder{header: http.Header{}, status: http.StatusOK}
	if req.Body == nil {
		req.Body = http.NoBody
	}
	d.Handler.ServeHTTP(rec, req)
	return &http.Response{
		StatusCode: rec.status, Status: strconv.Itoa(rec.status) + " " + http.StatusText(rec.status),
		Header: rec.header, Body: io.NopCloser(bytes.NewReader(rec.body.Bytes())),
		ContentLength: int64(rec.body.Len()), Request: req, Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1,
	}, nil
}

// recorder is the response writer HandlerDoer hands the handler.
type recorder struct {
	header  http.Header
	body    bytes.Buffer
	status  int
	written bool
}

func (r *recorder) Header() http.Header { return r.header }

func (r *recorder) WriteHeader(status int) {
	if !r.written {
		r.status, r.written = status, true
	}
}

func (r *recorder) Write(p []byte) (int, error) {
	r.written = true
	return r.body.Write(p)
}

// errNoBody is an answer that should have carried JSON and did not.
var errNoBody = errors.New("cowork answered without the expected body")
