//go:build integration

package integration

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api"
	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/test/fixture"
)

// docs/adr/0016 D6 as amended 2026-10-05: a tenant's attachments hold at most
// COWORK_ATTACHMENT_TENANT_QUOTA bytes together. An upload beyond it is refused
// before anything is stored, every attachment of the tenant counts — a
// confidential ticket's the uploader cannot see too — and one tenant's usage
// never counts against, nor shows to, another.
func TestTheTenantAttachmentQuota(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	tk := issueTokens(t, w)
	quota := int64(200)
	e := ticketEnv{world: w, tk: tk, ctx: ctx, s: newAPI(t, func(o *api.Options) { o.AttachmentTenantQuota = quota })}
	f := fixtures(t)
	member, admin := caller{Token: tk.MemberA}, caller{Token: tk.AdminA}

	secret := e.file(t, admin, "ALPHA", task("Hidden"))
	require.NoError(t, f.Exec(ctx, `UPDATE tickets SET confidential = true WHERE id = $1`, secret.Id))
	require.Equal(t, http.StatusNotFound, e.get(t, member, "ALPHA", secret.Number).StatusCode(), "the member cannot see it")
	require.NoError(t, f.Exec(ctx, `INSERT INTO attachments (tenant_id, ticket_id, file_name, size, sha256, content_type, uploaded_by)
		VALUES ($1, $2, 'hidden.txt', 100, sha256(''::bytea), 'text/plain; charset=utf-8', $3)`, w.A, secret.Id, w.AdminA))
	open := e.file(t, member, "ALPHA", task("Open"))
	other := e.file(t, member, "ALPHA", task("Other"))

	decodeAttachment(t, e.uploadTo(t, member, open, "one.png", "image/png", pngBytes, nil, "")) // 100 + 72
	refused := assertProblem(t, e.uploadTo(t, member, other, "two.png", "image/png", pngBytes, nil, ""), http.StatusConflict, "attachment_quota")
	detail, _ := refused["detail"].(string)
	assert.NotContains(t, detail, "172", "the refusal names no sum of the tenant's files")
	assert.NotContains(t, detail, "100")
	n, err := f.QueryCount(ctx, "SELECT count(*) FROM attachments WHERE tenant_id = $1", w.A)
	require.NoError(t, err)
	assert.EqualValues(t, 2, n, "nothing of the refused file was stored")

	// Tenant B holds nothing yet: A's usage does not count against it.
	_, number, err := f.Ticket(ctx, w.B, w.ProjectB, w.MemberB, "In B")
	require.NoError(t, err)
	inB := e
	inB.SlugA = w.SlugB
	ticketB := apigen.Ticket{Project: "BETA", Number: number}
	memberB := caller{Token: tk.MemberB}
	decodeAttachment(t, inB.uploadTo(t, memberB, ticketB, "b1.png", "image/png", pngBytes, nil, ""))
	decodeAttachment(t, inB.uploadTo(t, memberB, ticketB, "b2.png", "image/png", pngBytes, nil, ""))
	assertProblem(t, inB.uploadTo(t, memberB, ticketB, "b3.png", "image/png", pngBytes, nil, ""), http.StatusConflict, "attachment_quota")

	// The usage, for each tenant's administrators, of that tenant alone.
	usage, err := e.s.client(t, admin).GetAttachmentUsageWithResponse(ctx, w.SlugA)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, usage.StatusCode(), string(usage.Body))
	assert.EqualValues(t, 172, usage.JSON200.UsedBytes, "the confidential ticket's file counts")
	assert.EqualValues(t, 2, usage.JSON200.Attachments)
	got, err := usage.JSON200.QuotaBytes.Get()
	require.NoError(t, err)
	assert.Equal(t, quota, got)
	adminB, err := f.Person(ctx, uniqueSlug("admin-b"), "Admin B")
	require.NoError(t, err)
	require.NoError(t, f.Member(ctx, w.B, adminB, "admin"))
	adminBToken, _, err := f.Token(ctx, fixture.TokenSpec{UserID: adminB})
	require.NoError(t, err)
	usageB, err := e.s.client(t, caller{Token: adminBToken}).GetAttachmentUsageWithResponse(ctx, w.SlugB)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, usageB.StatusCode())
	assert.EqualValues(t, 144, usageB.JSON200.UsedBytes, "B's usage is B's")
	assert.EqualValues(t, 2, usageB.JSON200.Attachments)
	notAdmin, err := e.s.client(t, member).GetAttachmentUsageWithResponse(ctx, w.SlugA)
	require.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, notAdmin.StatusCode(), "the sum counts files a member may not see")
	assertProblem(t, e.s.do(t, caller{Token: adminBToken}, http.MethodGet, "/api/v1/tenants/"+w.SlugA+"/attachment-usage", nil),
		http.StatusNotFound, "not_found")

	// Without a quota the usage says so and nothing is refused for it.
	plain, err := newAPI(t).client(t, admin).GetAttachmentUsageWithResponse(ctx, w.SlugA)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, plain.StatusCode())
	assert.True(t, plain.JSON200.QuotaBytes.IsNull())
}

// docs/adr/0016 D6: the tenant's quota is checked under its lock, so uploads
// to different tickets at the same moment cannot pass it together.
func TestSimultaneousUploadsKeepTheTenantQuota(t *testing.T) {
	w := newWorld(t)
	tk := issueTokens(t, w)
	e := ticketEnv{world: w, tk: tk, ctx: context.Background(),
		s: newAPI(t, func(o *api.Options) { o.AttachmentTenantQuota = int64(3 * len(pngBytes)) })}
	member := caller{Token: tk.MemberA}
	sends := make([]func() int, 8)
	for i := range sends {
		ticket := e.file(t, member, "ALPHA", task(fmt.Sprintf("Ticket %d", i)))
		req := e.uploadRequest(t, member, ticket, fmt.Sprintf("q%d.png", i), "image/png", pngBytes, nil, "")
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
	assert.Equal(t, 3, created, "three files fit the quota")
	n, err := fixtures(t).QueryCount(e.ctx, "SELECT count(*) FROM attachments WHERE tenant_id = $1", w.A)
	require.NoError(t, err)
	assert.EqualValues(t, 3, n)
}
