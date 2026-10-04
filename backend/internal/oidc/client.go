package oidc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// maxAnswer is the most cowork reads of any answer of the issuer — the
// discovery, the keys, the token endpoint's, UserInfo's (the security review
// of 2026-10-04, m7): an issuer, or what stands in its place, cannot make the
// backend read without bound.
const maxAnswer = 1 << 20

// requestTimeout bounds every single call to the issuer.
const requestTimeout = 10 * time.Second

// errTooLarge is what reading an answer beyond maxAnswer fails with.
var errTooLarge = errors.New("the issuer's answer is larger than 1 MiB")

// newClient is the client of every call to the issuer: it follows no redirect
// — an answer that redirects is no answer of the issuer's, and a redirected
// token request would carry the client secret elsewhere — and its transport
// reads at most maxAnswer of any answer.
func newClient() *http.Client {
	return &http.Client{
		Timeout:       requestTimeout,
		Transport:     cappedTransport{base: http.DefaultTransport},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

type cappedTransport struct{ base http.RoundTripper }

func (t cappedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	res, err := t.base.RoundTrip(r)
	if err != nil {
		return nil, err
	}
	if res.ContentLength > maxAnswer {
		_ = res.Body.Close()
		return nil, errTooLarge
	}
	res.Body = &cappedBody{ReadCloser: res.Body, left: maxAnswer}
	return res, nil
}

// cappedBody reads at most left bytes and fails, rather than truncates, when
// the answer goes on.
type cappedBody struct {
	io.ReadCloser
	left int64
}

func (b *cappedBody) Read(p []byte) (int, error) {
	if b.left <= 0 {
		var probe [1]byte
		n, err := b.ReadCloser.Read(probe[:])
		if n > 0 {
			return 0, errTooLarge
		}
		return 0, err
	}
	if int64(len(p)) > b.left {
		p = p[:b.left]
	}
	n, err := b.ReadCloser.Read(p)
	b.left -= int64(n)
	return n, err
}

// checkEndpoint holds an endpoint of the issuer to the issuer's own rule
// (internal/config): https, or http on a loopback host for a development
// issuer. The browser and the backend carry codes, tokens and the client
// secret to these endpoints. The answer never quotes the URL.
func checkEndpoint(raw string) error {
	u, err := url.Parse(raw)
	switch {
	case err != nil || u.Host == "":
		return errors.New("is not a URL")
	case u.User != nil || u.Fragment != "":
		return errors.New("has a user or a fragment")
	case u.Scheme == "https":
		return nil
	case u.Scheme == "http" && loopback(u.Hostname()):
		return nil
	}
	return errors.New("is neither https nor http on a loopback host")
}

func loopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// transportError is a call that got no answer, or an answer it could not
// read: what it says is the client's — the method, the URL, the cause — and
// never the answer's body.
func transportError(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || errors.Is(err, errTooLarge) {
		return err
	}
	return errors.New("the issuer's answer could not be read")
}

// safeCode is an OAuth error code as a log line may quote it: the characters
// RFC 6749 allows a code to have that are letters, digits, '_', '.' and '-',
// at most sixty-four of them.
func safeCode(code string) string {
	if code == "" {
		return "without an error code"
	}
	var b strings.Builder
	for _, r := range code {
		if b.Len() >= 64 {
			break
		}
		if r == '_' || r == '.' || r == '-' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// shortError is an error's text, cut for a log line.
func shortError(err error) string {
	s := err.Error()
	if r := []rune(s); len(r) > 200 {
		return string(r[:200]) + "…"
	}
	return s
}

// getJSON reads an answer of the issuer that must be 200 and JSON.
func getJSON(ctx context.Context, client *http.Client, endpoint string, into func(io.Reader) error) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	res, err := client.Do(req)
	if err != nil {
		return transportError(err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("the issuer answered %d", res.StatusCode)
	}
	if err := into(res.Body); err != nil {
		if errors.Is(err, errTooLarge) {
			return err
		}
		return errors.New("the issuer's answer is not the JSON document it should be")
	}
	return nil
}
