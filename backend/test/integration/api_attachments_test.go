//go:build integration

package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api"
	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/storage"
	"github.com/guided-traffic/cowork/backend/test/fixture"
)

var (
	pngBytes  = append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{7}, 64)...)
	htmlBytes = []byte("<!DOCTYPE html><html><script>alert(1)</script></html>")
)

// uploadTo posts one file as multipart, declaring declared as its type; the
// boundary is random, as a client's retry would be.
func (e ticketEnv) uploadTo(t *testing.T, c caller, tk apigen.Ticket, name, declared string, data []byte, extra map[string]string, key string) *http.Response {
	t.Helper()
	res, err := http.DefaultClient.Do(e.uploadRequest(t, c, tk, name, declared, data, extra, key))
	require.NoError(t, err)
	t.Cleanup(func() { _ = res.Body.Close() })
	return res
}

// uploadRequest builds an upload without sending it.
func (e ticketEnv) uploadRequest(t *testing.T, c caller, tk apigen.Ticket, name, declared string, data []byte, extra map[string]string, key string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename=%q`, name))
	h.Set("Content-Type", declared)
	part, err := mw.CreatePart(h)
	require.NoError(t, err)
	_, err = part.Write(data)
	require.NoError(t, err)
	for k, v := range extra {
		require.NoError(t, mw.WriteField(k, v))
	}
	require.NoError(t, mw.Close())
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s%s/%d/attachments", e.s.URL, e.projectTickets(tk.Project), tk.Number), &body)
	require.NoError(t, err)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	require.NoError(t, c.editor(context.Background(), req))
	return req
}

func decodeAttachment(t *testing.T, res *http.Response) apigen.Attachment {
	t.Helper()
	require.Equal(t, http.StatusCreated, res.StatusCode)
	var a apigen.Attachment
	require.NoError(t, json.NewDecoder(res.Body).Decode(&a))
	return a
}

// docs/adr/0016 D1–D5: the type from the bytes, the object under
// <tenant-id>/<id>, the delivery headers, every download recorded.
func TestAttachmentRoundTrip(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	member, viewer := caller{Token: e.tk.MemberA}, caller{Token: e.tk.ViewerA}
	tk := e.file(t, member, "ALPHA", task("With files"))

	a := decodeAttachment(t, e.uploadTo(t, member, tk, "../../shot.png", "text/plain", pngBytes, nil, ""))
	assert.Equal(t, apigen.AttachmentContentTypeImagepng, a.ContentType, "the declared type is ignored")
	assert.Equal(t, "shot.png", a.FileName)
	assert.EqualValues(t, len(pngBytes), a.Size)
	sum := sha256.Sum256(pngBytes)
	assert.Equal(t, hex.EncodeToString(sum[:]), a.Sha256)
	obj, size, err := testStorage(t).Get(e.ctx, storage.Key(e.A, a.Id))
	require.NoError(t, err, "the object key is <tenant-id>/<attachment-id>")
	_ = obj.Close()
	assert.EqualValues(t, len(pngBytes), size)

	res := e.uploadTo(t, member, tk, "page.png", "image/png", htmlBytes, nil, "")
	body := assertProblem(t, res, http.StatusUnsupportedMediaType, "unsupported_media_type")
	assert.Contains(t, body["detail"], "text/html")

	pdf := decodeAttachment(t, e.uploadTo(t, member, tk, "spec.pdf", "application/pdf", []byte("%PDF-1.7\n%%EOF"), nil, ""))
	svg := decodeAttachment(t, e.uploadTo(t, member, tk, "logo.svg", "image/svg+xml", []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), nil, ""))
	assert.Equal(t, apigen.AttachmentContentTypeImagesvgXml, svg.ContentType)

	for _, c := range []struct {
		a           apigen.Attachment
		disposition string
	}{{a, `inline; filename=shot.png`}, {pdf, `attachment; filename=spec.pdf`}, {svg, `attachment; filename=logo.svg`}} {
		res := e.s.do(t, viewer, http.MethodGet, strings.TrimPrefix(c.a.ContentUrl, ""), nil)
		require.Equal(t, http.StatusOK, res.StatusCode, "a viewer downloads")
		assert.Equal(t, string(c.a.ContentType), res.Header.Get("Content-Type"))
		assert.Equal(t, "nosniff", res.Header.Get("X-Content-Type-Options"))
		assert.Equal(t, "sandbox", res.Header.Get("Content-Security-Policy"))
		assert.Equal(t, c.disposition, res.Header.Get("Content-Disposition"))
		assert.Equal(t, `"`+c.a.Sha256+`"`, res.Header.Get("ETag"))
	}
	got, err := io.ReadAll(e.s.do(t, member, http.MethodGet, a.ContentUrl, nil).Body)
	require.NoError(t, err)
	assert.Equal(t, pngBytes, got)
	unchanged := e.s.do(t, member, http.MethodGet, a.ContentUrl, nil, "If-None-Match", `"`+a.Sha256+`"`)
	assert.Equal(t, http.StatusNotModified, unchanged.StatusCode)
	downloads, err := f.QueryCount(e.ctx, "SELECT count(*) FROM audit_events WHERE entity_id = $1 AND action = 'downloaded'", a.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 2, downloads, "every 200 is recorded, the 304 is not")

	assert.Equal(t, http.StatusForbidden, e.uploadTo(t, viewer, tk, "v.png", "image/png", pngBytes, nil, "").StatusCode, "a viewer does not upload")
	narrow, _, err := f.Token(e.ctx, fixture.TokenSpec{UserID: e.MemberA, Agent: true, Capabilities: []string{"interest"}})
	require.NoError(t, err)
	refused := e.uploadTo(t, caller{Token: narrow, Agent: "claude-code/opus/s1"}, tk, "a.png", "image/png", pngBytes, nil, uuid.NewString())
	body = assertProblem(t, refused, http.StatusForbidden, "agent_forbidden")
	assert.Equal(t, "missing capability: upload", body["detail"])

	require.NoError(t, testStorage(t).Delete(e.ctx, storage.Key(e.A, pdf.Id)))
	gone := assertProblem(t, e.s.do(t, member, http.MethodGet, pdf.ContentUrl, nil), http.StatusNotFound, "not_found")
	assert.Contains(t, gone["detail"], "missing from storage")
}

// docs/adr/0016 D6: the per-ticket count holds when uploads race — the
// ticket's attachment lock lets exactly the remaining number pass.
func TestSimultaneousUploadsKeepTheCount(t *testing.T) {
	e := newTicketEnv(t)
	member := caller{Token: e.tk.MemberA}
	tk := e.file(t, member, "ALPHA", task("Many files"))
	sends := make([]func() int, 12)
	for i := range sends {
		req := e.uploadRequest(t, member, tk, fmt.Sprintf("r%d.png", i), "image/png", pngBytes, nil, "")
		sends[i] = func() int {
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				return 0
			}
			_ = res.Body.Close()
			return res.StatusCode
		}
	}
	created := 0
	for i, code := range simultaneously(sends...) {
		switch code {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
		default:
			t.Errorf("upload %d answered %d", i, code)
		}
	}
	assert.Equal(t, 5, created, "the test server's per-ticket count")
	n, err := fixtures(t).QueryCount(e.ctx, "SELECT count(*) FROM attachments WHERE ticket_id = $1", tk.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 5, n)
}

// trickle starts an upload whose body sends the file's first bytes and then
// holds the rest back for hold; the channel receives its status, 0 when the
// connection failed. It asks to continue first, as curl does for a large
// body: the body leaves the client only once the server reads it.
func trickle(t *testing.T, srv apiServer, c caller, url string, hold time.Duration) <-chan int {
	t.Helper()
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		h := textproto.MIMEHeader{}
		h.Set("Content-Disposition", `form-data; name="file"; filename="slow.png"`)
		h.Set("Content-Type", "image/png")
		if part, err := mw.CreatePart(h); err == nil {
			_, _ = part.Write(pngBytes)
		}
		time.Sleep(hold)
		_ = mw.Close()
		_ = pw.Close()
	}()
	req, err := http.NewRequest(http.MethodPost, srv.URL+url, pr)
	require.NoError(t, err)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Expect", "100-continue")
	require.NoError(t, c.editor(context.Background(), req))
	done := make(chan int, 1)
	go func() {
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			done <- 0
			return
		}
		_ = res.Body.Close()
		done <- res.StatusCode
	}()
	return done
}

// docs/adr/0039 D2, docs/adr/0016 D6: the request timeout bounds reading
// the body — an upload that trickles in fails at the deadline and gives its
// memory slot back instead of holding it as long as the client likes.
func TestATricklingUploadEndsAtTheTimeout(t *testing.T) {
	e := newTicketEnv(t)
	srv := newAPI(t, func(o *api.Options) {
		o.RequestTimeout = 500 * time.Millisecond
		o.AttachmentMaxBytes = 64 << 20 // one upload in memory at a time
		// The response check reads the whole body before the handler runs;
		// the server as it runs in production reads it in the handler.
		o.ValidateResponses = false
	})
	env := ticketEnv{world: e.world, tk: e.tk, s: srv, ctx: e.ctx}
	member := caller{Token: e.tk.MemberA}
	tk := env.file(t, member, "ALPHA", task("Slow"))
	slow := trickle(t, srv, member, fmt.Sprintf("%s/%d/attachments", env.projectTickets("ALPHA"), tk.Number), 2*time.Second)

	time.Sleep(700 * time.Millisecond)
	decodeAttachment(t, env.uploadTo(t, member, tk, "quick.png", "image/png", pngBytes, nil, ""))
	select {
	case code := <-slow:
		assert.Contains(t, []int{0, http.StatusGatewayTimeout}, code, "the trickling upload ends at its deadline")
	case <-time.After(time.Second):
		t.Fatal("the trickling upload is still held")
	}
}

// docs/adr/0016 D2, docs/adr/0039 D2: an upload the caller may not make is
// refused before its body is read — nothing buffers it ahead of the role,
// the scope and the memory slot.
func TestAnUploadIsRefusedBeforeItsBodyIsRead(t *testing.T) {
	e := newTicketEnv(t)
	srv := newAPI(t, func(o *api.Options) { o.ValidateResponses = false })
	env := ticketEnv{world: e.world, tk: e.tk, s: srv, ctx: e.ctx}
	tk := env.file(t, caller{Token: e.tk.MemberA}, "ALPHA", task("Read only"))
	refused := trickle(t, srv, caller{Token: e.tk.ViewerA}, fmt.Sprintf("%s/%d/attachments", env.projectTickets("ALPHA"), tk.Number), 3*time.Second)
	select {
	case code := <-refused:
		assert.Equal(t, http.StatusForbidden, code)
	case <-time.After(time.Second):
		t.Fatal("the refusal waited for the body")
	}
}

// docs/adr/0016 D6: above the maximum nothing is stored; the per-ticket
// count; a retry with a new boundary replays; uploads need storage.
func TestAttachmentLimits(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	member := caller{Token: e.tk.MemberA}
	tk := e.file(t, member, "ALPHA", task("Limits"))

	big := append(append([]byte{}, pngBytes...), bytes.Repeat([]byte{1}, 1<<20)...)
	assertProblem(t, e.uploadTo(t, member, tk, "big.png", "image/png", big, nil, ""), http.StatusRequestEntityTooLarge, "payload_too_large")
	n, err := f.QueryCount(e.ctx, "SELECT count(*) FROM attachments WHERE ticket_id = $1", tk.Id)
	require.NoError(t, err)
	assert.Zero(t, n, "neither row nor object above the maximum")

	key := uuid.NewString()
	first := decodeAttachment(t, e.uploadTo(t, member, tk, "same.png", "image/png", pngBytes, nil, key))
	retry := decodeAttachment(t, e.uploadTo(t, member, tk, "same.png", "image/png", pngBytes, nil, key))
	assert.Equal(t, first.Id, retry.Id, "a retry with a new boundary replays")
	for i := range 4 {
		decodeAttachment(t, e.uploadTo(t, member, tk, fmt.Sprintf("n%d.png", i), "image/png", pngBytes, nil, ""))
	}
	assertProblem(t, e.uploadTo(t, member, tk, "sixth.png", "image/png", pngBytes, nil, ""), http.StatusConflict, "attachment_limit")

	plain := newAPI(t, func(o *api.Options) { o.Storage = nil })
	none := ticketEnv{world: e.world, tk: e.tk, s: plain, ctx: e.ctx}
	assertProblem(t, none.uploadTo(t, member, tk, "x.png", "image/png", pngBytes, nil, ""), http.StatusNotImplemented, "uploads_disabled")
	list, err := plain.client(t, member).ListAttachmentsWithResponse(e.ctx, e.SlugA, "ALPHA", tk.Number, &apigen.ListAttachmentsParams{})
	require.NoError(t, err)
	assert.Len(t, list.JSON200.Items, 5, "lists and metadata work without storage")
}

// docs/adr/0016 D2: access follows the ticket; a comment's attachment is its
// author's.
func TestAttachmentAccess(t *testing.T) {
	e := newTicketEnv(t)
	member, both := caller{Token: e.tk.MemberA}, caller{Token: e.tk.Both}
	secret := e.file(t, member, "ALPHA", task("Secret", func(b *apigen.TicketCreate) {
		b.Security, b.Threat = apigen.SecurityClassLive, ptr("leak")
	}))
	a := decodeAttachment(t, e.uploadTo(t, member, secret, "proof.png", "image/png", pngBytes, nil, ""))
	assertProblem(t, e.s.do(t, both, http.MethodGet, a.ContentUrl, nil), http.StatusNotFound, "not_found")
	assertProblem(t, e.s.do(t, caller{Token: e.tk.MemberB}, http.MethodGet, a.ContentUrl, nil), http.StatusNotFound, "not_found")
	assertProblem(t, e.s.do(t, member, http.MethodGet, fmt.Sprintf("%s/%d/attachments/x.png", e.projectTickets("ALPHA"), secret.Number), nil),
		http.StatusNotFound, "not_found")

	tk := e.file(t, member, "ALPHA", task("Open"))
	cm := e.comment(t, member, tk, "see the screenshot")
	ok := decodeAttachment(t, e.uploadTo(t, member, tk, "s.png", "image/png", pngBytes, map[string]string{"comment_id": cm.Id.String()}, ""))
	assert.Equal(t, cm.Id, ok.Comment.MustGet())
	assert.Equal(t, http.StatusForbidden, e.uploadTo(t, both, tk, "s.png", "image/png", pngBytes, map[string]string{"comment_id": cm.Id.String()}, "").StatusCode,
		"another person's comment")
	assertProblem(t, e.uploadTo(t, member, tk, "s.png", "image/png", pngBytes, map[string]string{"comment_id": uuid.NewString()}, ""),
		http.StatusBadRequest, "validation_failed")
}
