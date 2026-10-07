package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"mime"
	"net/http"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/google/uuid"

	"github.com/guided-traffic/cowork/backend/internal/github"
	"github.com/guided-traffic/cowork/backend/internal/problem"
	"github.com/guided-traffic/cowork/backend/internal/requestid"
	"github.com/guided-traffic/cowork/backend/internal/store"
)

// opReceiveGitHubWebhook is GitHub's webhook, which oapi-codegen leaves out:
// its handler reads the raw body to verify the signature before anything
// parses it (docs/adr/0071 D3).
const opReceiveGitHubWebhook = "receiveGitHubWebhook"

// signed reports whether an operation's credential is a signature over its
// body that its handler verifies — GitHub's webhook, x-cowork-signed: github
// (docs/adr/0071 D2, D3). Such an operation is public, as the document says
// with security: [], and the pipeline treats it as no other: no cookie or
// token is resolved, no origin is checked — GitHub sends none, and a
// signature no browser can make needs no CSRF defence —, the tenant boundary,
// which admits persons, does not run, and the body is not validated, because
// validating parses it.
func signed(op *openapi3.Operation) bool {
	v, _ := op.Extensions["x-cowork-signed"].(string)
	return v != ""
}

// hookTenant is the tenant a delivery's path names, with its opened secret.
type hookTenant struct {
	id     uuid.UUID
	slug   string
	secret []byte
}

type hookKey struct{}

func hookFrom(ctx context.Context) hookTenant {
	t, _ := ctx.Value(hookKey{}).(hookTenant)
	return t
}

// webhookTenant admits a delivery to the tenant its path names, before its
// body is read (docs/adr/0071 D1, D2): the tenant and its secret, which the
// server opens with the key derived for it. An unknown tenant, a tenant
// without a secret and a secret that no longer opens — the server key changed
// since it was made — are one answer, the boundary's 404. The request acts as
// the system actor system:github from here on.
func (h *handler) webhookTenant(ctx context.Context, slug string) (context.Context, *problem.Error) {
	refused := problem.New(problem.NotFound, "no such tenant")
	ctx = store.WithCaller(ctx, store.Caller{System: store.SystemGitHub, RequestID: requestid.UUID(ctx),
		SourceHash: h.sourceHash(clientFrom(ctx).Client)})
	t, err := h.opts.DB.WebhookSecret(ctx, slug)
	if errors.Is(err, store.ErrNotFound) {
		return ctx, refused
	}
	if err != nil {
		h.logger.Error("the webhook's tenant could not be read", "request_id", requestid.From(ctx), "error", err)
		return ctx, problem.New(problem.Internal, "internal error")
	}
	secret, err := h.webhookSealer.Open(t.Sealed, t.ID[:])
	if err != nil {
		h.logger.Error("the tenant's GitHub webhook secret does not open: the server key changed since it was made; rotate the secret",
			"request_id", requestid.From(ctx), "tenant", t.Slug)
		return ctx, refused
	}
	return context.WithValue(ctx, hookKey{}, hookTenant{id: t.ID, slug: t.Slug, secret: secret}), nil
}

// serveGitHubWebhook takes a delivery the pipeline admitted to its tenant and
// bounded by the body limit (docs/adr/0071 D3, D4): the body is read whole,
// its signature verified in constant time before anything parses it, then
// the delivery id, the type and the event; the delivery is recorded for a
// day, and a repetition answers 200 without effect. Every delivery taken
// answers 202, whatever it changed: the answer says nothing of what is bound
// or which keys exist.
func (h *handler) serveGitHubWebhook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	hook := hookFrom(ctx)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var tooLong *http.MaxBytesError
		if errors.As(err, &tooLong) {
			problem.Write(w, r, tooLarge(tooLong.Limit))
			return
		}
		problem.Write(w, r, problem.New(problem.ValidationFailed, "the body could not be read"))
		return
	}
	if !github.Verify(hook.secret, body, r.Header.Get(github.SignatureHeader)) {
		problem.Write(w, r, &problem.Error{Code: problem.SignatureInvalid,
			Detail:  "the delivery's signature is missing or is not the HMAC of its body under the tenant's webhook secret",
			Headers: map[string]string{"WWW-Authenticate": github.SignatureHeader + ` realm="cowork"`}})
		return
	}
	d, perr := readDelivery(r, body)
	if perr != nil {
		problem.Write(w, r, perr)
		return
	}
	now := h.opts.Now()
	repeat, err := h.opts.DB.ReceiveDelivery(ctx, hook.id, d.id, now, func(wr *store.Writer) error {
		return applyDelivery(ctx, wr, hook, d, now)
	})
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	if repeat {
		w.WriteHeader(http.StatusOK)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// delivery is a verified delivery: its id and, of the events cowork reads,
// what it says; neither for every other event.
type delivery struct {
	id   uuid.UUID
	pr   *github.PullRequest
	push *github.Push
}

// readDelivery reads the headers and the body of a delivery whose signature
// holds: the delivery's id, a UUID; the body's type, JSON; and the event.
func readDelivery(r *http.Request, body []byte) (delivery, *problem.Error) {
	id, err := uuid.Parse(r.Header.Get(github.DeliveryHeader))
	if err != nil {
		return delivery{}, &problem.Error{Code: problem.ValidationFailed, Detail: "a delivery carries its id",
			Errors: []problem.FieldError{{Pointer: "header:" + github.DeliveryHeader, Message: "must be a UUID"}}}
	}
	if mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mediaType != "application/json" {
		return delivery{}, problem.New(problem.UnsupportedMediaType,
			"the delivery's content type must be application/json: set it in the webhook's settings at GitHub")
	}
	d := delivery{id: id}
	switch r.Header.Get(github.EventHeader) {
	case github.EventPullRequest:
		pr, err := github.ParsePullRequest(body)
		if err != nil {
			return delivery{}, problem.New(problem.ValidationFailed, err.Error())
		}
		d.pr = &pr
	case github.EventPush:
		push, err := github.ParsePush(body)
		if err != nil {
			return delivery{}, problem.New(problem.ValidationFailed, err.Error())
		}
		d.push = &push
	}
	return d, nil
}

// newWebhookSecret draws a tenant's secret: 256 random bits as 64
// hexadecimal characters, which GitHub takes as they are and a person can
// copy whole (docs/adr/0071 D1).
func newWebhookSecret() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b) // crypto/rand does not fail
	return hex.EncodeToString(b)
}

// webhookPath is the endpoint GitHub posts the tenant's deliveries to.
func webhookPath(slug string) string {
	return "/api/v1/tenants/" + slug + "/integrations/github/webhook"
}
