//go:build integration

package integration

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"text/template"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api"
	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/github"
)

// The repository the webhook tests bind, as GitHub names it, and its identity
// (docs/adr/0066 D1).
const (
	hookRepo     = "acme/app"
	hookIdentity = "github.com/acme/app"
)

// webhookEnv is a world whose tenant A binds hookRepo to ALPHA and takes
// GitHub's webhook with a secret the test knows (docs/adr/0071 D1).
type webhookEnv struct {
	ticketEnv
	secret string
}

func newWebhookEnv(t *testing.T, opts ...func(*api.Options)) webhookEnv {
	t.Helper()
	w := newWorld(t)
	e := webhookEnv{ticketEnv: ticketEnv{world: w, tk: issueTokens(t, w), s: newAPI(t, opts...), ctx: context.Background()},
		secret: newHookSecret(t)}
	sealWebhookSecret(t, w.A, w.AdminA, e.secret)
	bindRepository(t, w.A, w.ProjectA, hookIdentity)
	return e
}

func newHookSecret(t *testing.T) string {
	t.Helper()
	b := make([]byte, 32)
	_, err := rand.Read(b)
	require.NoError(t, err)
	return hex.EncodeToString(b)
}

// sealWebhookSecret stores a tenant's secret as the server does: sealed under
// the label of the webhook's secret, bound to the tenant.
func sealWebhookSecret(t *testing.T, tenant, by uuid.UUID, secret string) {
	t.Helper()
	sealed := auth.NewSealer(testSessionKey, auth.LabelGitHubWebhookSecret).Seal([]byte(secret), tenant[:])
	require.NoError(t, fixtures(t).Exec(context.Background(), `INSERT INTO github_webhook_secrets (tenant_id, secret, created_by)
		VALUES ($1, $2, $3) ON CONFLICT (tenant_id) DO UPDATE SET secret = EXCLUDED.secret`, tenant, sealed, by))
}

func bindRepository(t *testing.T, tenant, project uuid.UUID, identity string) {
	t.Helper()
	require.NoError(t, fixtures(t).Exec(context.Background(), `INSERT INTO project_repositories (tenant_id, project_id, identity, remote)
		VALUES ($1, $2, $3, $4)`, tenant, project, identity, "https://"+identity+".git"))
}

// payload renders a delivery's body from GitHub's documented shape in
// testdata/github.
func payload(t *testing.T, name string, data any) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/github/" + name + ".json")
	require.NoError(t, err)
	tmpl := template.Must(template.New(name).Funcs(template.FuncMap{"json": func(v any) (string, error) {
		b, err := json.Marshal(v)
		return string(b), err
	}}).Parse(string(raw)))
	var b bytes.Buffer
	require.NoError(t, tmpl.Execute(&b, data))
	require.True(t, json.Valid(b.Bytes()), "the fixture renders JSON: %s", b.String())
	return b.Bytes()
}

// prEvent is a pull_request delivery's variable part.
type prEvent struct {
	Action, Title, State, UpdatedAt string
	Body, MergedAt                  *string
	Number                          int
	Merged                          bool
	Repo, DefaultBranch             string
}

func (p prEvent) body(t *testing.T) []byte {
	t.Helper()
	if p.Repo == "" {
		p.Repo = hookRepo
	}
	if p.DefaultBranch == "" {
		p.DefaultBranch = "main"
	}
	if p.State == "" {
		p.State = "open"
	}
	if p.UpdatedAt == "" {
		p.UpdatedAt = "2026-10-05T08:00:00Z"
	}
	return payload(t, "pull_request", p)
}

// pushedCommit is a commit of a push delivery.
type pushedCommit struct{ ID, Message string }

type pushEvent struct {
	Ref, Repo, DefaultBranch string
	Commits                  []pushedCommit
}

func (p pushEvent) body(t *testing.T) []byte {
	t.Helper()
	if p.Repo == "" {
		p.Repo = hookRepo
	}
	if p.DefaultBranch == "" {
		p.DefaultBranch = "main"
	}
	return payload(t, "push", p)
}

// hook is one delivery as GitHub sends it.
type hook struct {
	event, delivery, signature, contentType string
	body                                    []byte
}

// post sends a delivery to a tenant's endpoint; an empty signature, delivery
// or content type is left out, a nil one of hookDefaults filled in.
func (s apiServer) post(t *testing.T, slug string, h hook, opts ...reqOpt) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, s.URL+"/api/v1/tenants/"+slug+"/integrations/github/webhook", bytes.NewReader(h.body))
	require.NoError(t, err)
	for k, v := range map[string]string{"Content-Type": h.contentType, github.EventHeader: h.event,
		github.DeliveryHeader: h.delivery, github.SignatureHeader: h.signature, "User-Agent": "GitHub-Hookshot/test"} {
		if v != "" {
			req.Header.Set(k, v)
		}
	}
	for _, o := range opts {
		o(req)
	}
	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = res.Body.Close() })
	return res
}

// deliver signs a delivery with the tenant's secret and posts it under a new
// delivery id, which it returns.
func (e webhookEnv) deliver(t *testing.T, event string, body []byte, opts ...reqOpt) (*http.Response, string) {
	t.Helper()
	id := uuid.NewString()
	return e.s.post(t, e.SlugA, e.signed(event, id, body), opts...), id
}

func (e webhookEnv) signed(event, delivery string, body []byte) hook {
	return hook{event: event, delivery: delivery, body: body, contentType: "application/json",
		signature: github.Sign([]byte(e.secret), body)}
}

// mustTake delivers and requires the 202 of a delivery taken.
func (e webhookEnv) mustTake(t *testing.T, event string, body []byte) {
	t.Helper()
	res, _ := e.deliver(t, event, body)
	if res.StatusCode != http.StatusAccepted {
		require.Equal(t, http.StatusAccepted, res.StatusCode, "%v", problemBody(t, res))
	}
}

// pullRequests reads a ticket's pull requests as c.
func (e webhookEnv) pullRequests(t *testing.T, c caller, number int) []apigen.PullRequest {
	t.Helper()
	res := e.s.do(t, c, http.MethodGet, ticketPath(e.SlugA, "ALPHA", number)+"/pull-requests", nil)
	require.Equal(t, http.StatusOK, res.StatusCode)
	return decode[apigen.PullRequestList](t, res).Items
}

// countRows counts rows over the administrative connection.
func countRows(t *testing.T, sql string, args ...any) int64 {
	t.Helper()
	n, err := fixtures(t).QueryCount(context.Background(), sql, args...)
	require.NoError(t, err)
	return n
}

// written counts what the webhook wrote in a tenant: its deliveries, its links
// and the acts of its system actor.
func written(t *testing.T, tenant uuid.UUID) [3]int64 {
	t.Helper()
	return [3]int64{
		countRows(t, `SELECT count(*) FROM github_deliveries WHERE tenant_id = $1`, tenant),
		countRows(t, `SELECT count(*) FROM ticket_pull_requests WHERE tenant_id = $1`, tenant),
		countRows(t, `SELECT count(*) FROM audit_events WHERE tenant_id = $1 AND actor_system = 'system:github'`, tenant),
	}
}

func ptrTo(s string) *string { return &s }

// strip is a problem body without what differs between two requests.
func strip(t *testing.T, res *http.Response) map[string]any {
	t.Helper()
	body := problemBody(t, res)
	delete(body, "instance")
	delete(body, "request_id")
	return body
}

// docs/adr/0071 D1, D2: until the tenant has a secret its endpoint answers
// every delivery exactly like an unknown tenant, signed or not, whatever
// credential it carries.
func TestTheWebhookOfATenantWithoutSecretIsNoTenant(t *testing.T) {
	w := newWorld(t)
	s := newAPI(t)
	tk := issueTokens(t, w)
	body := prEvent{Action: "opened", Number: 1, Title: "x (ALPHA-1)"}.body(t)
	h := hook{event: "pull_request", delivery: uuid.NewString(), body: body, contentType: "application/json",
		signature: github.Sign([]byte("any secret"), body)}
	none := s.post(t, w.SlugA, h, withBearer(tk.AdminA))
	unknown := s.post(t, "no-such-tenant-9", h)
	require.Equal(t, http.StatusNotFound, none.StatusCode)
	require.Equal(t, http.StatusNotFound, unknown.StatusCode)
	assert.Equal(t, strip(t, unknown), strip(t, none), "a tenant without a secret answers like no tenant")
	assert.Equal(t, [3]int64{0, 0, 0}, written(t, w.A))
}

// docs/adr/0071 D3: a missing or wrong signature is 401 before anything is
// read or written — another secret's, another body's, a session's or a token's
// are no signature —, and a right one is taken whatever credential rides
// along, which is never resolved.
func TestASignatureThatDoesNotHoldIsRefusedAndWritesNothing(t *testing.T) {
	e := newWebhookEnv(t)
	tk := e.file(t, caller{Token: e.tk.MemberA}, "ALPHA", task("signed"))
	body := prEvent{Action: "opened", Number: 1, Title: "x (ALPHA-" + strconv.Itoa(tk.Number) + ")"}.body(t)
	for name, signature := range map[string]string{
		"missing":       "",
		"another key":   github.Sign([]byte("not the secret"), body),
		"another body":  github.Sign([]byte(e.secret), append([]byte(" "), body...)),
		"not a sha256":  "sha1=" + strings.Repeat("0", 40),
		"the secret":    e.secret,
		"empty sha256=": "sha256=",
	} {
		h := e.signed("pull_request", uuid.NewString(), body)
		h.signature = signature
		res := e.s.post(t, e.SlugA, h, withBearer(e.tk.AdminA))
		assertProblem(t, res, http.StatusUnauthorized, "signature_invalid")
		assert.Equal(t, `X-Hub-Signature-256 realm="cowork"`, res.Header.Get("WWW-Authenticate"), name)
	}
	assert.Equal(t, [3]int64{0, 0, 0}, written(t, e.A), "nothing read, nothing written")

	res, _ := e.deliver(t, "pull_request", body, withBearer("cwk_"+strings.Repeat("x", 43)),
		withHeader("Cookie", auth.SessionCookie+"=not-a-session"))
	require.Equal(t, http.StatusAccepted, res.StatusCode, "a bearer token or a cookie beside the signature is not looked at")
	assert.Len(t, e.pullRequests(t, caller{Token: e.tk.MemberA}, tk.Number), 1)
}

// docs/adr/0071 D3: a delivery's id is kept a day; the same delivery again is
// 200 and changes nothing, and the same body under a new id changes nothing
// either, since nothing in it is new. The delivery's id rides on the act.
func TestARepeatedDeliveryChangesNothing(t *testing.T) {
	e := newWebhookEnv(t)
	tk := e.file(t, caller{Token: e.tk.MemberA}, "ALPHA", task("once"))
	body := prEvent{Action: "opened", Number: 7, Title: "x (ALPHA-" + strconv.Itoa(tk.Number) + ")"}.body(t)
	h := e.signed("pull_request", uuid.NewString(), body)
	require.Equal(t, http.StatusAccepted, e.s.post(t, e.SlugA, h).StatusCode)
	after := written(t, e.A)
	assert.Equal(t, [3]int64{1, 1, 1}, after)
	again := e.s.post(t, e.SlugA, h)
	require.Equal(t, http.StatusOK, again.StatusCode, "a repetition is 200")
	raw, err := io.ReadAll(again.Body)
	require.NoError(t, err)
	assert.Empty(t, raw)
	assert.Equal(t, after, written(t, e.A), "and changes nothing")
	assert.Equal(t, int64(1), countRows(t, `SELECT count(*) FROM audit_events WHERE tenant_id = $1 AND idempotency_key = $2`, e.A, h.delivery),
		"the act carries the delivery's id")

	e.mustTake(t, "pull_request", body)
	got := written(t, e.A)
	assert.Equal(t, after[1:], got[1:], "the same body under another id links nothing twice")

	// A day later the id is forgotten: the job removes it, and the delivery is
	// taken anew — and links nothing twice.
	removed, err := openRuntime(t).ExpireGitHubDeliveries(context.Background(), time.Now().Add(25*time.Hour))
	require.NoError(t, err)
	assert.GreaterOrEqual(t, removed, int64(2))
	assert.Zero(t, countRows(t, `SELECT count(*) FROM github_deliveries WHERE tenant_id = $1`, e.A))
	require.Equal(t, http.StatusAccepted, e.s.post(t, e.SlugA, h).StatusCode)
	later := written(t, e.A)
	assert.Equal(t, after[1:], later[1:])
}

// docs/adr/0071 D4: a repository no project of the tenant binds, an event
// cowork does not read, an action it does not read and a ping are taken with
// the same 202 and change nothing — the answer says nothing of what is bound.
func TestWhatTheWebhookDoesNotReadIsTakenAndPassedOver(t *testing.T) {
	e := newWebhookEnv(t)
	tk := e.file(t, caller{Token: e.tk.MemberA}, "ALPHA", task("passed over"))
	key := "ALPHA-" + strconv.Itoa(tk.Number)
	for name, d := range map[string]struct {
		event string
		body  []byte
	}{
		"an unbound repository": {"pull_request", prEvent{Action: "opened", Number: 1, Title: "x (" + key + ")", Repo: "acme/elsewhere"}.body(t)},
		"another event":         {"issues", []byte(`{"action":"opened","issue":{"title":"x (` + key + `)"}}`)},
		"an action not read":    {"pull_request", prEvent{Action: "labeled", Number: 2, Title: "x (" + key + ")"}.body(t)},
		"a ping":                {"ping", []byte(`{"zen":"Keep it logically awesome.","hook_id":1}`)},
		"no event at all":       {"", []byte(`{}`)},
		"another branch": {"push", pushEvent{Ref: "refs/heads/feature/x",
			Commits: []pushedCommit{{ID: strings.Repeat("a", 40), Message: "fix: x (" + key + ")"}}}.body(t)},
	} {
		res, _ := e.deliver(t, d.event, d.body)
		require.Equal(t, http.StatusAccepted, res.StatusCode, name)
	}
	assert.Empty(t, e.pullRequests(t, caller{Token: e.tk.MemberA}, tk.Number))
	assert.Zero(t, countRows(t, `SELECT count(*) FROM audit_events WHERE tenant_id = $1 AND actor_system = 'system:github'`, e.A))
}

// docs/adr/0071 D3, D4: a delivery's id must be a UUID, its type JSON and its
// payload GitHub's — each refused after the signature holds, with nothing
// written.
func TestASignedDeliveryThatIsNotGitHubsIsRefused(t *testing.T) {
	e := newWebhookEnv(t)
	body := prEvent{Action: "opened", Number: 1, Title: "x (ALPHA-1)"}.body(t)
	h := e.signed("pull_request", "not-a-uuid", body)
	res := e.s.post(t, e.SlugA, h)
	b := assertProblem(t, res, http.StatusBadRequest, "validation_failed")
	assert.Contains(t, b["errors"], map[string]any{"pointer": "header:X-GitHub-Delivery", "message": "must be a UUID"})
	h = e.signed("pull_request", uuid.NewString(), body)
	h.contentType = "application/x-www-form-urlencoded"
	assertProblem(t, e.s.post(t, e.SlugA, h), http.StatusUnsupportedMediaType, "unsupported_media_type")
	broken := []byte(`{"action":"opened","pull_request":{"number":0}}`)
	res, _ = e.deliver(t, "pull_request", broken)
	assertProblem(t, res, http.StatusBadRequest, "validation_failed")
	assert.Equal(t, [3]int64{0, 0, 0}, written(t, e.A))
}

// docs/adr/0071 D3, docs/adr/0039 D2: the body is bounded by the JSON limit,
// before its signature is checked, and a delivery above it writes nothing.
func TestTheWebhookBodyIsBounded(t *testing.T) {
	e := newWebhookEnv(t, func(o *api.Options) { o.MaxJSONBody = 4096 })
	body := prEvent{Action: "opened", Number: 1, Title: "x (ALPHA-1)", Body: ptrTo(strings.Repeat("a", 8192))}.body(t)
	res, _ := e.deliver(t, "pull_request", body)
	assertProblem(t, res, http.StatusRequestEntityTooLarge, "payload_too_large")
	assert.Equal(t, [3]int64{0, 0, 0}, written(t, e.A))
}

// docs/adr/0071 D5, D6, docs/adr/0068 D1, D2, D5: a pull request links the
// tickets its body's full keys name — a trailer, a line that is a key alone
// —, and only where the body names none, the short keys at the end of its
// title; a key of another tenant, of no ticket, or in running text links
// nothing. The ticket gains the list, an act of the system actor, and the
// section of the context; the canonical document stays as it was.
func TestAPullRequestLinksTheTicketsItsTitleAndBodyName(t *testing.T) {
	e := newWebhookEnv(t)
	member := caller{Token: e.tk.MemberA}
	first := e.file(t, member, "ALPHA", task("named by the trailer"))
	second := e.file(t, member, "ALPHA", task("named by the title"))
	third := e.file(t, member, "ALPHA", task("named by a line of the body"))
	mentioned := e.file(t, member, "ALPHA", task("mentioned in passing"))
	bound := e.fileIn(t, caller{Token: e.tk.MemberB}, e.SlugB, "BETA", task("of tenant B"))
	bindRepository(t, e.B, e.ProjectB, hookIdentity)
	key := func(tk apigen.Ticket) string { return strings.SplitN(tk.Key, "/", 2)[1] }

	e.mustTake(t, "pull_request", prEvent{Action: "opened", Number: 21, Title: "fix: both (" + key(second) + ")",
		Body: ptrTo(first.Key + "\r\n\r\nSee also " + mentioned.Key + " in a sentence.\r\n\r\nCowork-Ticket: " + bound.Key +
			"\r\nCowork-Ticket: " + e.SlugA + "/ALPHA-9999\r\ncowork-ticket: " + third.Key)}.body(t))
	e.mustTake(t, "pull_request", prEvent{Action: "opened", Number: 22,
		Title: "feat: the title alone (" + key(second) + ") (#22)"}.body(t))

	one := e.pullRequests(t, member, first.Number)
	require.Len(t, one, 1)
	pr := one[0]
	assert.Equal(t, apigen.PullRequestKindPullRequest, pr.Kind)
	assert.Equal(t, hookIdentity, pr.Repository)
	assert.Equal(t, 21, pr.Number.MustGet())
	assert.True(t, pr.Sha.IsNull())
	assert.Equal(t, "fix: both ("+key(second)+")", pr.Title)
	assert.Equal(t, apigen.PullRequestStateOpen, pr.State)
	assert.Equal(t, "https://github.com/acme/app/pull/21", pr.Url)
	assert.Equal(t, "octocat", pr.Author.MustGet())
	assert.True(t, pr.MergedAt.IsNull())
	assert.Equal(t, apigen.PullRequestFoundInBody, pr.FoundIn)
	third1 := e.pullRequests(t, member, third.Number)
	require.Len(t, third1, 1)
	assert.Equal(t, apigen.PullRequestFoundInTrailer, third1[0].FoundIn)
	two := e.pullRequests(t, member, second.Number)
	require.Len(t, two, 1, "the title of #21 is not read: its body names keys; #22 has none")
	assert.Equal(t, 22, two[0].Number.MustGet())
	assert.Equal(t, apigen.PullRequestFoundInSubject, two[0].FoundIn)
	assert.Empty(t, e.pullRequests(t, member, mentioned.Number), "a key in running text is no key")
	assert.Zero(t, countRows(t, `SELECT count(*) FROM ticket_pull_requests WHERE tenant_id = $1`, e.B), "tenant B gains nothing")

	acts := e.s.do(t, member, http.MethodGet, ticketPath(e.SlugA, "ALPHA", first.Number)+"/activity?order=desc", nil)
	require.Equal(t, http.StatusOK, acts.StatusCode)
	latest := decode[apigen.ActivityList](t, acts).Items[0]
	assert.Equal(t, apigen.AuditActionLinked, latest.Action)
	assert.Equal(t, "pull_request", latest.EntityType)
	assert.Equal(t, "system:github", latest.ActorSystem.MustGet())
	assert.True(t, latest.Actor.IsNull())
	assert.Equal(t, map[string]any{"number": float64(21), "repository": hookIdentity, "state": "open", "found_in": "body"},
		latest.After.MustGet(), "no title, no body in the record")

	context := e.s.do(t, member, http.MethodGet, ticketPath(e.SlugA, "ALPHA", first.Number)+"/context", nil)
	require.Equal(t, http.StatusOK, context.StatusCode)
	doc, err := io.ReadAll(context.Body)
	require.NoError(t, err)
	assert.Contains(t, string(doc), "## Pull requests\n\n- github.com/acme/app#21 \"fix: both ("+key(second)+")\" (open, by octocat, key in the body)")
	markdown := e.s.do(t, member, http.MethodGet, ticketPath(e.SlugA, "ALPHA", first.Number)+"/markdown", nil)
	require.Equal(t, http.StatusOK, markdown.StatusCode)
	canonical, err := io.ReadAll(markdown.Body)
	require.NoError(t, err)
	assert.NotContains(t, string(canonical), "Pull requests", "the canonical ticket carries no pull request (docs/adr/0044 D1)")
}

// docs/adr/0071 D6: the page a ticket links is the bound repository's: a
// payload that names another page — whoever holds the secret writes the
// payload — puts no link to it on the ticket.
func TestAPayloadsPageIsNotWhatATicketLinks(t *testing.T) {
	e := newWebhookEnv(t)
	member := caller{Token: e.tk.MemberA}
	tk := e.file(t, member, "ALPHA", task("its link stays at GitHub"))
	key := strings.SplitN(tk.Key, "/", 2)[1]
	body := prEvent{Action: "opened", Number: 12, Title: "x (" + key + ")"}.body(t)
	body = bytes.ReplaceAll(body, []byte(`"html_url": "https://github.com/acme/app/pull/12"`),
		[]byte(`"html_url": "https://phishing.example/login"`))
	require.Contains(t, string(body), "phishing.example")
	e.mustTake(t, "pull_request", body)
	assert.Equal(t, "https://github.com/acme/app/pull/12", e.pullRequests(t, member, tk.Number)[0].Url)
}

// docs/adr/0071 D6: edited and synchronize bring the title up to date on every
// ticket the pull request is linked to — an act updated, which the activity
// leaves out —, and a delivery older than the facts a link holds changes
// nothing.
func TestAnEditOrASynchronizeUpdatesThePullRequest(t *testing.T) {
	e := newWebhookEnv(t)
	member := caller{Token: e.tk.MemberA}
	tk := e.file(t, member, "ALPHA", task("its title follows"))
	key := strings.SplitN(tk.Key, "/", 2)[1]
	e.mustTake(t, "pull_request", prEvent{Action: "opened", Number: 5, Title: "draft (" + key + ")", UpdatedAt: "2026-10-05T08:00:00Z"}.body(t))
	e.mustTake(t, "pull_request", prEvent{Action: "edited", Number: 5, Title: "a better title (" + key + ")", UpdatedAt: "2026-10-05T09:00:00Z"}.body(t))
	assert.Equal(t, "a better title ("+key+")", e.pullRequests(t, member, tk.Number)[0].Title)
	e.mustTake(t, "pull_request", prEvent{Action: "synchronize", Number: 5, Title: "after a push (" + key + ")", UpdatedAt: "2026-10-05T10:00:00Z"}.body(t))
	prs := e.pullRequests(t, member, tk.Number)
	assert.Equal(t, "after a push ("+key+")", prs[0].Title)
	assert.True(t, prs[0].LastSeenAt.After(prs[0].FirstSeenAt) || prs[0].LastSeenAt.Equal(prs[0].FirstSeenAt))
	e.mustTake(t, "pull_request", prEvent{Action: "edited", Number: 5, Title: "stale (" + key + ")", UpdatedAt: "2026-10-05T09:30:00Z"}.body(t))
	assert.Equal(t, "after a push ("+key+")", e.pullRequests(t, member, tk.Number)[0].Title, "an older delivery changes nothing")

	assert.Equal(t, int64(2), countRows(t, `SELECT count(*) FROM audit_events WHERE tenant_id = $1 AND ticket_id = $2
		AND entity_type = 'pull_request' AND action = 'updated'`, e.A, tk.Id))
	acts := e.s.do(t, member, http.MethodGet, ticketPath(e.SlugA, "ALPHA", tk.Number)+"/activity", nil)
	for _, a := range decode[apigen.ActivityList](t, acts).Items {
		assert.False(t, a.EntityType == "pull_request" && a.Action == apigen.AuditActionUpdated, "the activity leaves a title change out")
	}
}

// docs/adr/0071 D6, docs/adr/0020 D2: a merge tells the assignee and the
// watchers — a merge first heard of included — and changes no state; a
// replayed merge tells nobody twice; a close without a merge and a reopening
// are acts that tell nobody. The streams hear pull_request.changed.
func TestAMergeTellsTheAssigneeAndTheWatchersAndMovesNothing(t *testing.T) {
	e := newWebhookEnv(t)
	admin, member, viewer := caller{Token: e.tk.AdminA}, caller{Token: e.tk.MemberA}, caller{Token: e.tk.ViewerA}
	tk := e.fileIn(t, admin, e.SlugA, "ALPHA", task("merged elsewhere", func(b *apigen.TicketCreate) { b.Assignee = &e.MemberA }))
	path := ticketPath(e.SlugA, "ALPHA", tk.Number)
	e.send(t, viewer, http.StatusCreated, http.MethodPut, path+"/interest", map[string]any{"weight": "watch"})
	e.send(t, admin, http.StatusOK, http.MethodPost, path+"/transitions", map[string]any{"from": "filed", "to": "analysed"})
	key := strings.SplitN(tk.Key, "/", 2)[1]
	stream := e.openStream(t, e.s, member, e.SlugA, "")
	defer stream.Close()

	e.mustTake(t, "pull_request", prEvent{Action: "opened", Number: 34, Title: "fix (" + key + ")", UpdatedAt: "2026-10-06T08:00:00Z"}.body(t))
	merged := prEvent{Action: "closed", Number: 34, Title: "fix (" + key + ")", State: "closed", Merged: true,
		MergedAt: ptrTo("2026-10-06T09:30:00Z"), UpdatedAt: "2026-10-06T09:30:00Z"}.body(t)
	e.mustTake(t, "pull_request", merged)

	_, ev := stream.until(t, func(m sse) bool { return m.Event == "pull_request.changed" })
	assert.Contains(t, ev.Data, `"key":"`+tk.Key+`"`)
	for _, c := range []caller{member, viewer, admin} {
		reasons := reasonsAbout(e.inbox(t, c, ""), tk.Key)
		require.NotEmpty(t, reasons)
		assert.Equal(t, "merged", reasons[0], "the assignee, a watcher and the reporter are told")
	}
	entry := e.inbox(t, member, "").Items[0]
	assert.Equal(t, apigen.AuditActionMerged, entry.Act.Action)
	assert.Equal(t, "system:github", entry.Act.ActorSystem.MustGet())
	got := e.get(t, member, "ALPHA", tk.Number).JSON200
	assert.Equal(t, apigen.TicketStateAnalysed, got.State, "a merge moves no state")
	prs := e.pullRequests(t, member, tk.Number)
	assert.Equal(t, apigen.PullRequestStateMerged, prs[0].State)
	assert.Equal(t, time.Date(2026, 10, 6, 9, 30, 0, 0, time.UTC), prs[0].MergedAt.MustGet().UTC())

	e.mustTake(t, "pull_request", merged)
	assert.Equal(t, 1, countReason(e.inbox(t, member, ""), tk.Key, "merged"), "a replayed merge tells nobody twice")

	// A merge first heard of links and tells at once.
	late := e.fileIn(t, admin, e.SlugA, "ALPHA", task("linked by its merge", func(b *apigen.TicketCreate) { b.Assignee = &e.MemberA }))
	e.mustTake(t, "pull_request", prEvent{Action: "closed", Number: 35, Title: "late (" + strings.SplitN(late.Key, "/", 2)[1] + ")",
		State: "closed", Merged: true, MergedAt: ptrTo("2026-10-06T10:00:00Z"), UpdatedAt: "2026-10-06T10:00:00Z"}.body(t))
	assert.Equal(t, []string{"merged", "assigned"}, reasonsAbout(e.inbox(t, member, ""), late.Key))

	// Closed without a merge, then reopened: acts, and nobody told.
	other := e.fileIn(t, admin, e.SlugA, "ALPHA", task("closed unmerged", func(b *apigen.TicketCreate) { b.Assignee = &e.MemberA }))
	okey := strings.SplitN(other.Key, "/", 2)[1]
	e.mustTake(t, "pull_request", prEvent{Action: "opened", Number: 40, Title: "try (" + okey + ")", UpdatedAt: "2026-10-06T08:00:00Z"}.body(t))
	e.mustTake(t, "pull_request", prEvent{Action: "closed", Number: 40, Title: "try (" + okey + ")", State: "closed", UpdatedAt: "2026-10-06T09:00:00Z"}.body(t))
	e.mustTake(t, "pull_request", prEvent{Action: "reopened", Number: 40, Title: "try (" + okey + ")", UpdatedAt: "2026-10-06T10:00:00Z"}.body(t))
	assert.Equal(t, []string{"assigned"}, reasonsAbout(e.inbox(t, member, ""), other.Key))
	actions := map[string]int{}
	acts := e.s.do(t, member, http.MethodGet, ticketPath(e.SlugA, "ALPHA", other.Number)+"/activity", nil)
	for _, a := range decode[apigen.ActivityList](t, acts).Items {
		if a.EntityType == "pull_request" {
			actions[string(a.Action)]++
		}
	}
	assert.Equal(t, map[string]int{"linked": 1, "closed": 1, "reopened": 1}, actions)
	assert.Equal(t, apigen.PullRequestStateOpen, e.pullRequests(t, member, other.Number)[0].State)
}

func countReason(l apigen.InboxList, key, reason string) int {
	n := 0
	for _, r := range reasonsAbout(l, key) {
		if r == reason {
			n++
		}
	}
	return n
}

// docs/adr/0071 D4, D5: a push to the default branch links each commit by its
// message — its trailers, else its subject's short keys —, and a push to
// another branch links nothing; a commit is listed as merged and tells nobody.
func TestAPushToTheDefaultBranchLinksItsCommits(t *testing.T) {
	e := newWebhookEnv(t)
	member := caller{Token: e.tk.MemberA}
	byTrailer := e.file(t, member, "ALPHA", task("named by a trailer"))
	bySubject := e.file(t, member, "ALPHA", task("named by a subject"))
	short := func(tk apigen.Ticket) string { return strings.SplitN(tk.Key, "/", 2)[1] }
	first, second := strings.Repeat("0d1a26e6", 5), strings.Repeat("1e2f3a4b", 5)
	commits := []pushedCommit{
		{ID: first, Message: "fix: guard (" + short(bySubject) + ") (#9)\n\nBody.\n\nCowork-Ticket: " + byTrailer.Key},
		{ID: second, Message: "docs: name it (" + short(bySubject) + ")"},
	}
	e.mustTake(t, "push", pushEvent{Ref: "refs/heads/feature", Commits: commits}.body(t))
	assert.Empty(t, e.pullRequests(t, member, byTrailer.Number), "another branch links nothing")

	e.mustTake(t, "push", pushEvent{Ref: "refs/heads/main", Commits: commits}.body(t))
	got := e.pullRequests(t, member, byTrailer.Number)
	require.Len(t, got, 1, "the first commit's trailer wins over its subject")
	assert.Equal(t, apigen.PullRequestKindCommit, got[0].Kind)
	assert.Equal(t, first, got[0].Sha.MustGet())
	assert.True(t, got[0].Number.IsNull())
	assert.Equal(t, apigen.PullRequestStateMerged, got[0].State)
	assert.Equal(t, apigen.PullRequestFoundInTrailer, got[0].FoundIn)
	assert.Equal(t, "fix: guard ("+short(bySubject)+") (#9)", got[0].Title, "the subject is the title")
	assert.Equal(t, "https://github.com/acme/app/commit/"+first, got[0].Url)
	assert.False(t, got[0].MergedAt.IsNull())
	subject := e.pullRequests(t, member, bySubject.Number)
	require.Len(t, subject, 1)
	assert.Equal(t, second, subject[0].Sha.MustGet())
	assert.Equal(t, apigen.PullRequestFoundInSubject, subject[0].FoundIn)
	assert.Empty(t, reasonsAbout(e.inbox(t, member, ""), byTrailer.Key), "a commit tells nobody")

	e.mustTake(t, "push", pushEvent{Ref: "refs/heads/main", Commits: commits}.body(t))
	assert.Len(t, e.pullRequests(t, member, byTrailer.Number), 1, "a commit is linked once")
}

// docs/adr/0071 D6, docs/adr/0065 D1, D5: a confidential ticket's pull
// requests are its readers' only — its route, its context and its stream —
// and its merge tells only who may read it, though a watcher who lost sight
// of it is still among its watchers.
func TestAConfidentialTicketsPullRequestsAreItsReadersOnly(t *testing.T) {
	e := newWebhookEnv(t)
	admin, member, both := caller{Token: e.tk.AdminA}, caller{Token: e.tk.MemberA}, caller{Token: e.tk.Both}
	tk := e.fileIn(t, admin, e.SlugA, "ALPHA", task("becomes confidential", func(b *apigen.TicketCreate) { b.Assignee = &e.Both }))
	path := ticketPath(e.SlugA, "ALPHA", tk.Number)
	e.send(t, member, http.StatusCreated, http.MethodPut, path+"/interest", map[string]any{"weight": "watch"})
	e.send(t, admin, http.StatusOK, http.MethodPut, path+"/confidential", map[string]any{"confidential": true},
		"If-Match", strconv.Quote(strconv.Itoa(e.get(t, admin, "ALPHA", tk.Number).JSON200.Version)))
	stream := e.openStream(t, e.s, member, e.SlugA, "")
	defer stream.Close()

	key := strings.SplitN(tk.Key, "/", 2)[1]
	e.mustTake(t, "pull_request", prEvent{Action: "closed", Number: 3, Title: "secret fix (" + key + ")", State: "closed",
		Merged: true, MergedAt: ptrTo("2026-10-06T09:30:00Z"), UpdatedAt: "2026-10-06T09:30:00Z"}.body(t))

	assertProblem(t, e.s.do(t, member, http.MethodGet, path+"/pull-requests", nil), http.StatusNotFound, "not_found")
	assert.Empty(t, reasonsAbout(e.inbox(t, member, ""), tk.Key), "the watcher who cannot read the ticket is told nothing")
	assert.Equal(t, "merged", reasonsAbout(e.inbox(t, both, ""), tk.Key)[0], "the assignee is told")
	assert.Len(t, e.pullRequests(t, both, tk.Number), 1)
	assert.Len(t, e.pullRequests(t, admin, tk.Number), 1)

	// The member's stream hears nothing of it: a later public act arrives first.
	visible := e.file(t, member, "ALPHA", task("public"))
	_, m := stream.until(t, func(m sse) bool { return m.Event != "" })
	assert.Equal(t, "ticket.changed", m.Event)
	assert.Contains(t, m.Data, visible.Key, "the confidential ticket's merge never reached the member's stream")
}

// docs/adr/0071 Residual risks, docs/adr/0043 D2: a person removes a wrong
// link like any link, for good — a later delivery that names the ticket does
// not bring it back —, recorded as unlinked; a viewer may not.
func TestAPersonRemovesAWrongLinkForGood(t *testing.T) {
	e := newWebhookEnv(t)
	member, viewer := caller{Token: e.tk.MemberA}, caller{Token: e.tk.ViewerA}
	tk := e.file(t, member, "ALPHA", task("wrongly named"))
	key := strings.SplitN(tk.Key, "/", 2)[1]
	e.mustTake(t, "pull_request", prEvent{Action: "opened", Number: 8, Title: "unrelated (" + key + ")", UpdatedAt: "2026-10-06T08:00:00Z"}.body(t))
	prs := e.pullRequests(t, member, tk.Number)
	require.Len(t, prs, 1)
	link := ticketPath(e.SlugA, "ALPHA", tk.Number) + "/pull-requests/" + prs[0].Id.String()

	assertProblem(t, e.s.do(t, viewer, http.MethodDelete, link, nil), http.StatusForbidden, "forbidden")
	e.send(t, member, http.StatusNoContent, http.MethodDelete, link, nil)
	e.send(t, member, http.StatusNoContent, http.MethodDelete, link, nil)
	assert.Empty(t, e.pullRequests(t, member, tk.Number))
	e.mustTake(t, "pull_request", prEvent{Action: "edited", Number: 8, Title: "still unrelated (" + key + ")", UpdatedAt: "2026-10-06T09:00:00Z"}.body(t))
	assert.Empty(t, e.pullRequests(t, member, tk.Number), "the removal stays")
	assert.Equal(t, int64(1), countRows(t, `SELECT count(*) FROM audit_events WHERE tenant_id = $1 AND ticket_id = $2
		AND action = 'unlinked' AND entity_type = 'pull_request' AND actor_user_id = $3`, e.A, tk.Id, e.MemberA))
}

// docs/adr/0024 D2, docs/adr/0071 D6: the purge of a deleted ticket removes
// its pull requests with it, under the purge's restrictive policy.
func TestThePurgeRemovesTheTicketsPullRequests(t *testing.T) {
	e := newWebhookEnv(t)
	admin := caller{Token: e.tk.AdminA}
	tk := e.fileIn(t, admin, e.SlugA, "ALPHA", task("purged with its pull request"))
	key := strings.SplitN(tk.Key, "/", 2)[1]
	e.mustTake(t, "pull_request", prEvent{Action: "opened", Number: 9, Title: "x (" + key + ")"}.body(t))
	require.Equal(t, int64(1), countRows(t, `SELECT count(*) FROM ticket_pull_requests WHERE tenant_id = $1 AND ticket_id = $2`, e.A, tk.Id))
	e.send(t, admin, http.StatusNoContent, http.MethodDelete, ticketPath(e.SlugA, "ALPHA", tk.Number), nil)
	purged, err := openRuntime(t).PurgeDeletedTickets(context.Background(), time.Now().Add(31*24*time.Hour))
	require.NoError(t, err)
	var keys []string
	for _, p := range purged {
		keys = append(keys, p.Key)
	}
	require.Contains(t, keys, tk.Key)
	assert.Zero(t, countRows(t, `SELECT count(*) FROM ticket_pull_requests WHERE tenant_id = $1 AND ticket_id = $2`, e.A, tk.Id))
	e.mustTake(t, "pull_request", prEvent{Action: "edited", Number: 9, Title: "x (" + key + ")", UpdatedAt: "2026-10-06T08:00:00Z"}.body(t))
}

// docs/adr/0071 D1, docs/adr/0035 D5, docs/adr/0043 D3: the secret is made and
// rotated by a tenant administrator in a browser session — never a token, an
// agent or a member —, shown once and recorded without it; a rotation refuses
// the old secret at once, and a revocation, which a token may make, answers
// every delivery like an unknown tenant.
func TestTheWebhookSecretIsAnAdministratorsAct(t *testing.T) {
	w := newWorld(t)
	names := withAccounts(t, w)
	rec := &recordingLogger{}
	s := newAPI(t, withLogin, func(o *api.Options) { o.Logger = rec.logger() })
	tk := issueTokens(t, w)
	bindRepository(t, w.A, w.ProjectA, hookIdentity)
	base := "/api/v1/tenants/" + w.SlugA + "/integrations/github"
	admin := s.browser(t)
	admin.mustLogin(names["adminA"], testPassword)

	res := admin.get(base)
	require.Equal(t, http.StatusOK, res.StatusCode)
	before := decode[apigen.GitHubIntegration](t, res)
	assert.True(t, before.Secret.IsNull())
	assert.Equal(t, "/api/v1/tenants/"+w.SlugA+"/integrations/github/webhook", before.WebhookPath)
	assert.Equal(t, []string{"pull_request", "push"}, before.Events)

	res = admin.request(http.MethodPost, base+"/secret", nil)
	require.Equal(t, http.StatusCreated, res.StatusCode)
	made := decode[apigen.GitHubSecretCreated](t, res)
	assert.Regexp(t, `^[0-9a-f]{64}$`, made.Secret)
	assert.False(t, made.Replaced)
	assert.Equal(t, w.AdminA, made.CreatedBy.Id)
	res = admin.get(base)
	shown := decode[apigen.GitHubIntegration](t, res)
	assert.Equal(t, w.AdminA, shown.Secret.MustGet().CreatedBy.Id)
	raw, err := json.Marshal(shown)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), made.Secret, "shown once, never again")

	e := webhookEnv{ticketEnv: ticketEnv{world: w, tk: tk, s: s, ctx: context.Background()}, secret: made.Secret}
	body := prEvent{Action: "opened", Number: 1, Title: "x (ALPHA-1)"}.body(t)
	e.mustTake(t, "pull_request", body)

	res = admin.request(http.MethodPost, base+"/secret", nil)
	require.Equal(t, http.StatusCreated, res.StatusCode)
	rotated := decode[apigen.GitHubSecretCreated](t, res)
	assert.True(t, rotated.Replaced)
	assert.NotEqual(t, made.Secret, rotated.Secret)
	old, _ := e.deliver(t, "pull_request", body)
	assertProblem(t, old, http.StatusUnauthorized, "signature_invalid")
	e.secret = rotated.Secret
	e.mustTake(t, "pull_request", body)

	// Never a token, an agent or a member.
	assertProblem(t, s.do(t, caller{Token: tk.AdminA}, http.MethodPost, base+"/secret", nil), http.StatusForbidden, "session_required")
	assertProblem(t, admin.request(http.MethodPost, base+"/secret", nil, withHeader("X-Cowork-Agent", "chat/model/c")),
		http.StatusForbidden, "agent_forbidden")
	member := s.browser(t)
	member.mustLogin(names["memberA"], testPassword)
	assertProblem(t, member.request(http.MethodPost, base+"/secret", nil), http.StatusForbidden, "forbidden")
	assertProblem(t, member.get(base), http.StatusForbidden, "forbidden")
	assertProblem(t, s.do(t, caller{Token: tk.AdminA, Agent: "claude-code/opus/1"}, http.MethodDelete, base+"/secret", nil),
		http.StatusForbidden, "agent_forbidden")
	assertProblem(t, s.do(t, caller{Token: tk.AdminAWrite}, http.MethodDelete, base+"/secret", nil),
		http.StatusForbidden, "insufficient_scope")

	// A token revokes: it only takes access away.
	require.Equal(t, http.StatusNoContent, s.do(t, caller{Token: tk.AdminA}, http.MethodDelete, base+"/secret", nil).StatusCode)
	require.Equal(t, http.StatusNoContent, admin.request(http.MethodDelete, base+"/secret", nil).StatusCode)
	gone, _ := e.deliver(t, "pull_request", body)
	assertProblem(t, gone, http.StatusNotFound, "not_found")
	assert.True(t, decode[apigen.GitHubIntegration](t, admin.get(base)).Secret.IsNull())

	// Recorded, without the secret anywhere but its one answer.
	rows, err := fixtures(t).Query(context.Background(), `SELECT action::text, coalesce(after::text, ''), coalesce(token_id::text, '')
		FROM audit_events WHERE tenant_id = $1 AND entity_type = 'github_webhook_secret' ORDER BY id`, w.A)
	require.NoError(t, err)
	var acts []string
	for rows.Next() {
		var action, after, token string
		require.NoError(t, rows.Scan(&action, &after, &token))
		acts = append(acts, action+" "+after+" "+map[bool]string{true: "token", false: "session"}[token != ""])
		assert.NotContains(t, after, made.Secret)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []string{`created {"replaced": false} session`, `created {"replaced": true} session`, "revoked  token"}, acts)
	for _, secret := range []string{made.Secret, rotated.Secret} {
		assert.NotContains(t, rec.text(), secret, "no log line carries the secret")
		assert.Zero(t, countRows(t, `SELECT count(*) FROM audit_events WHERE tenant_id = $1 AND (after::text LIKE '%' || $2 || '%'
			OR before::text LIKE '%' || $2 || '%' OR reason LIKE '%' || $2 || '%' OR note LIKE '%' || $2 || '%')`, w.A, secret))
	}
}

// docs/adr/0071 D1: the secret is sealed at rest, bound to its tenant: what
// the table holds is no plaintext, and the sealed value of one tenant moved
// to another does not open there — the delivery answers like no secret.
func TestTheSecretIsSealedForItsTenant(t *testing.T) {
	e := newWebhookEnv(t)
	var sealed []byte
	require.NoError(t, fixtures(t).QueryRow(context.Background(), `SELECT secret FROM github_webhook_secrets WHERE tenant_id = $1`, e.A).Scan(&sealed))
	assert.NotContains(t, string(sealed), e.secret)
	require.NoError(t, fixtures(t).Exec(context.Background(), `INSERT INTO github_webhook_secrets (tenant_id, secret, created_by)
		VALUES ($1, $2, $3)`, e.B, sealed, e.MemberB))
	body := prEvent{Action: "opened", Number: 1, Title: "x (BETA-1)"}.body(t)
	res := e.s.post(t, e.SlugB, e.signed("pull_request", uuid.NewString(), body))
	assertProblem(t, res, http.StatusNotFound, "not_found")
}
