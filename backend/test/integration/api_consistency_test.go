//go:build integration

package integration

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/config"
	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/metrics"
	"github.com/guided-traffic/cowork/backend/internal/storage"
	"github.com/guided-traffic/cowork/backend/internal/store"
	"github.com/guided-traffic/cowork/backend/test/fixture"
)

// v7At is a UUIDv7 made at t, as the backend makes an attachment's id: what
// an object uploaded before the consistency check's grace is named.
func v7At(t time.Time) uuid.UUID {
	id := uuid.Must(uuid.NewV7())
	ms := uint64(t.UnixMilli()) // #nosec G115 -- a time after 1970
	for i := range 6 {
		id[i] = byte(ms >> (40 - 8*i))
	}
	return id
}

// consistencyPath is the tenant's consistency route, with a suffix.
func consistencyPath(slug, suffix string) string {
	return "/api/v1/tenants/" + slug + "/attachment-consistency" + suffix
}

// consistencyOf reads the tenant's latest result as c.
func (e ticketEnv) consistencyOf(t *testing.T, c caller, slug string) apigen.AttachmentConsistency {
	t.Helper()
	res := e.s.do(t, c, http.MethodGet, consistencyPath(slug, ""), nil)
	require.Equal(t, http.StatusOK, res.StatusCode)
	return decode[apigen.AttachmentConsistency](t, res)
}

// tenantOf is what a run found in one tenant.
func tenantOf(t *testing.T, run store.ConsistencyRun, id uuid.UUID) store.TenantConsistency {
	t.Helper()
	for _, r := range run.Tenants {
		if r.TenantID == id {
			return r
		}
	}
	t.Fatalf("the run checked no tenant %s", id)
	return store.TenantConsistency{}
}

// putObject puts bytes into the run's bucket under key, as a restore of the
// bucket would.
func putObject(t *testing.T, key string, data []byte) {
	t.Helper()
	require.NoError(t, testStorage(t).Put(context.Background(), key, bytes.NewReader(data), int64(len(data)), "image/png"))
}

func objectExists(t *testing.T, key string) bool {
	t.Helper()
	ok, err := testStorage(t).Exists(context.Background(), key)
	require.NoError(t, err)
	return ok
}

// docs/adr/0059 D4, D5: in tenant A a file whose bytes a restore lost — a
// dangling row —, an object no row names — an orphan —, one that gains
// metadata before the removal is confirmed, and a stray object under another
// key; in tenant B nothing amiss. The check counts and lists them per tenant,
// sums them up in the installation-level audit record without a file name,
// and the metrics answer the counts on any replica. The administrator reads
// the lists, accepts the lost file, and confirms the removal in a browser
// session: the orphans go, the object that gained metadata stays. The next
// check sees the lost file accepted until its bytes come back.
func TestTheConsistencyCheckFindsWhatARestoreLeftAndTheAdministratorSettlesIt(t *testing.T) {
	e, names, session := sessionEnv(t)
	f := fixtures(t)
	ctx := context.Background()
	db := openRuntime(t)
	admin, member := caller{Token: e.tk.AdminA}, caller{Token: e.tk.MemberA}
	later := time.Now().Add(2 * store.OrphanGrace)

	tk := e.file(t, member, "ALPHA", task("With files a restore loses"))
	lost := decodeAttachment(t, e.uploadTo(t, member, tk, "lost-diagram.png", "image/png", pngBytes, nil, ""))
	whole := decodeAttachment(t, e.uploadTo(t, member, tk, "whole.png", "image/png", pngBytes, nil, ""))
	require.NoError(t, testStorage(t).Delete(ctx, storage.Key(e.A, lost.Id)), "a restore brought the row back without its bytes")
	orphan, gains := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	putObject(t, storage.Key(e.A, orphan), pngBytes)
	putObject(t, storage.Key(e.A, gains), pngBytes)
	putObject(t, e.A.String()+"/stray.bin", []byte("not an attachment"))
	var inB uuid.UUID
	ticketB, _, err := f.Ticket(ctx, e.B, e.ProjectB, e.MemberB, "Whole in B")
	require.NoError(t, err)
	require.NoError(t, f.QueryRow(ctx, `INSERT INTO attachments (tenant_id, ticket_id, file_name, size, sha256, content_type, uploaded_by)
		VALUES ($1, $2, 'b.png', $3, sha256($4), 'image/png', $5) RETURNING id`,
		e.B, ticketB, len(pngBytes), pngBytes, e.MemberB).Scan(&inB))
	putObject(t, storage.Key(e.B, inB), pngBytes)

	before := e.consistencyOf(t, admin, e.SlugA)
	assert.True(t, before.CheckId.IsNull(), "no check has run in the tenant yet")
	assert.Empty(t, before.DanglingAttachments)

	run, err := db.CheckConsistency(ctx, testStorage(t), later)
	require.NoError(t, err)
	require.True(t, run.Ran)
	inA := tenantOf(t, run, e.A)
	assert.Equal(t, 1, inA.Dangling)
	assert.Zero(t, inA.Accepted)
	assert.Equal(t, 3, inA.Orphans, "the two objects no row names and the stray one")
	assert.EqualValues(t, 2*len(pngBytes)+len("not an attachment"), inA.OrphanBytes)
	assert.False(t, tenantOf(t, run, e.B).Found(), "nothing in tenant B")

	got := e.consistencyOf(t, admin, e.SlugA)
	require.False(t, got.CheckId.IsNull())
	assert.Equal(t, 1, got.Dangling)
	require.Len(t, got.DanglingAttachments, 1)
	missing := got.DanglingAttachments[0]
	assert.Equal(t, lost.Id, missing.Id)
	assert.Equal(t, "lost-diagram.png", missing.FileName)
	assert.Equal(t, tk.Key, missing.Ticket)
	assert.False(t, missing.Accepted)
	assert.Equal(t, 3, got.Orphans)
	keys := make([]string, 0, len(got.OrphanedObjects))
	for _, o := range got.OrphanedObjects {
		keys = append(keys, o.Key)
	}
	assert.ElementsMatch(t, []string{storage.Key(e.A, orphan), storage.Key(e.A, gains), e.A.String() + "/stray.bin"}, keys)
	assert.NotContains(t, keys, storage.Key(e.A, whole.Id))
	quiet := e.consistencyOf(t, caller{Token: adminOf(t, e.B)}, e.SlugB)
	assert.Zero(t, quiet.Dangling+quiet.Orphans+quiet.Accepted, "tenant B's own result is clean")

	// The download of the lost file says why (D4).
	gone := assertProblem(t, e.s.do(t, member, http.MethodGet, lost.ContentUrl, nil), http.StatusNotFound, "not_found")
	assert.Contains(t, gone["detail"], "missing from storage")

	// The summary: counts per tenant by id, never a file name or a key.
	summary := scalar[string](t, `SELECT after::text FROM audit_events WHERE tenant_id IS NULL AND action = 'checked'
		AND actor_system = 'system:consistency-check' ORDER BY id DESC LIMIT 1`)
	assert.Contains(t, summary, `"`+e.A.String()+`": {"orphans": 3, "accepted": 0, "dangling": 1}`)
	assert.NotContains(t, summary, e.B.String())
	assert.NotContains(t, summary, "lost-diagram")
	assert.NotContains(t, summary, orphan.String())

	// Every replica answers the counts from the database.
	m := metrics.New()
	replica, err := store.Open(ctx, env.RuntimeURL, store.Options{Metrics: m})
	require.NoError(t, err)
	t.Cleanup(replica.Close)
	samples, err := m.Samples()
	require.NoError(t, err)
	assert.Equal(t, 1.0, metrics.Sum(samples, "cowork_consistency_dangling_attachments", "tenant", e.A.String()))
	assert.Equal(t, 3.0, metrics.Sum(samples, "cowork_consistency_orphaned_objects", "tenant", e.A.String()))
	assert.True(t, metrics.Has(samples, "cowork_consistency_orphaned_objects", "tenant", e.B.String()))
	assert.Equal(t, 0.0, metrics.Sum(samples, "cowork_consistency_orphaned_objects", "tenant", e.B.String()))
	last, checked, err := db.LastConsistencyCheck(ctx)
	require.NoError(t, err)
	assert.True(t, checked)
	assert.WithinDuration(t, later, last, time.Millisecond)
	assert.False(t, store.ConsistencyCheckDue(last, checked, later), "a check that ran is not due again before tomorrow's hour")

	// The administrator accepts the lost file: it counts as accepted, no
	// longer as dangling, and stays listed.
	check := apigen.ConsistencyCheckRef{CheckId: got.CheckId.MustGet()}
	stale := apigen.ConsistencyCheckRef{CheckId: uuid.Must(uuid.NewV7())}
	assertProblem(t, e.s.do(t, admin, http.MethodPost, consistencyPath(e.SlugA, "/dangling-acceptance"), stale),
		http.StatusConflict, "consistency_check_stale")
	accepted := e.s.do(t, admin, http.MethodPost, consistencyPath(e.SlugA, "/dangling-acceptance"), check)
	require.Equal(t, http.StatusOK, accepted.StatusCode)
	assert.Equal(t, 1, decode[apigen.DanglingAcceptance](t, accepted).Accepted)
	got = e.consistencyOf(t, admin, e.SlugA)
	assert.Zero(t, got.Dangling)
	assert.Equal(t, 1, got.Accepted)
	require.Len(t, got.DanglingAttachments, 1)
	assert.True(t, got.DanglingAttachments[0].Accepted)
	again := e.s.do(t, admin, http.MethodPost, consistencyPath(e.SlugA, "/dangling-acceptance"), check)
	require.Equal(t, http.StatusOK, again.StatusCode)
	assert.Zero(t, decode[apigen.DanglingAcceptance](t, again).Accepted, "nothing left to accept, nothing recorded")
	assert.Equal(t, 1, scalar[int](t, `SELECT count(*) FROM audit_events WHERE tenant_id = $1 AND action = 'accepted'
		AND entity_type = 'attachment_consistency' AND actor_user_id = $2 AND after = '{"accepted": 1}'`, e.A, e.AdminA))

	// Before the removal is confirmed, one orphan gains its metadata.
	require.NoError(t, f.Exec(ctx, `INSERT INTO attachments (id, tenant_id, ticket_id, file_name, size, sha256, content_type, uploaded_by)
		VALUES ($1, $2, $3, 'gained.png', $4, sha256($5), 'image/png', $6)`, gains, e.A, tk.Id, len(pngBytes), pngBytes, e.MemberA))

	removal := consistencyPath(e.SlugA, "/orphan-removal")
	assertProblem(t, e.s.do(t, admin, http.MethodPost, removal, check), http.StatusForbidden, "session_required")
	assertProblem(t, session.request(http.MethodPost, removal, stale), http.StatusConflict, "consistency_check_stale")
	res := session.request(http.MethodPost, removal, check)
	requireOK(t, res)
	assert.Equal(t, apigen.OrphanRemoval{Removed: 2, Kept: 1}, decode[apigen.OrphanRemoval](t, res))
	assert.False(t, objectExists(t, storage.Key(e.A, orphan)))
	assert.False(t, objectExists(t, e.A.String()+"/stray.bin"))
	assert.True(t, objectExists(t, storage.Key(e.A, gains)), "the object that gained metadata stays")
	assert.True(t, objectExists(t, storage.Key(e.A, whole.Id)))
	assert.True(t, objectExists(t, storage.Key(e.B, inB)), "nothing of tenant B is touched")
	got = e.consistencyOf(t, admin, e.SlugA)
	assert.Zero(t, got.Orphans)
	assert.Empty(t, got.OrphanedObjects)
	record := got.OrphanRemoval.MustGet()
	assert.Equal(t, 2, record.Removed)
	assert.Equal(t, 1, record.Kept)
	assert.Equal(t, e.AdminA, record.RemovedBy.Id)
	assert.Equal(t, names["adminA"], record.RemovedBy.Username.MustGet())
	assert.Equal(t, 1, scalar[int](t, `SELECT count(*) FROM audit_events WHERE tenant_id = $1 AND action = 'purged'
		AND entity_type = 'attachment_consistency' AND entity_id = $2 AND actor_user_id = $3 AND token_id IS NULL
		AND after = '{"kept": 1, "orphans": 2}'`, e.A, check.CheckId, e.AdminA), "the person's act in a session, counted")
	assertProblem(t, session.request(http.MethodPost, removal, check), http.StatusConflict, "consistency_check_stale")

	// The next check: the accepted loss stays accepted, nothing is orphaned.
	run, err = db.CheckConsistency(ctx, testStorage(t), later)
	require.NoError(t, err)
	inA = tenantOf(t, run, e.A)
	assert.Equal(t, store.TenantConsistency{TenantID: e.A, Slug: e.SlugA, Accepted: 1}, inA)
	assertProblem(t, session.request(http.MethodPost, removal, check), http.StatusConflict, "consistency_check_stale")

	// Its bytes come back: whole again, and a later loss would count anew.
	putObject(t, storage.Key(e.A, lost.Id), pngBytes)
	run, err = db.CheckConsistency(ctx, testStorage(t), later)
	require.NoError(t, err)
	assert.False(t, tenantOf(t, run, e.A).Found())
	assert.Zero(t, scalar[int](t, `SELECT count(*) FROM consistency_acceptances WHERE attachment_id = $1`, lost.Id))
}

// requireOK requires a 200 and shows the problem otherwise.
func requireOK(t *testing.T, res *http.Response) {
	t.Helper()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("answered %d: %v", res.StatusCode, unsafeBody(t, res))
	}
}

// adminOf makes an administrator of the tenant and returns an admin token of
// theirs.
func adminOf(t *testing.T, tenant uuid.UUID) string {
	t.Helper()
	ctx := context.Background()
	f := fixtures(t)
	person, err := f.Person(ctx, uniqueSlug("admin"), "Admin")
	require.NoError(t, err)
	require.NoError(t, f.Member(ctx, tenant, person, domain.RoleAdmin))
	token, _, err := f.Token(ctx, fixture.TokenSpec{UserID: person, Scope: domain.ScopeAdmin})
	require.NoError(t, err)
	return token
}

// docs/adr/0059 D4, docs/adr/0043 D3, docs/adr/0035 D5: the lists are the
// tenant's administrators' — a member is refused, another tenant's
// administrator finds no such tenant —; no agent accepts a loss or removes an
// object, and a token removes none either.
func TestTheConsistencyCheckIsTheTenantAdministratorsAndNoAgents(t *testing.T) {
	e, names, session := sessionEnv(t)
	ctx := context.Background()
	_, err := openRuntime(t).CheckConsistency(ctx, testStorage(t), time.Now())
	require.NoError(t, err)
	check := apigen.ConsistencyCheckRef{CheckId: e.consistencyOf(t, caller{Token: e.tk.AdminA}, e.SlugA).CheckId.MustGet()}
	acceptance, removal := consistencyPath(e.SlugA, "/dangling-acceptance"), consistencyPath(e.SlugA, "/orphan-removal")

	assertProblem(t, e.s.do(t, caller{Token: e.tk.MemberA}, http.MethodGet, consistencyPath(e.SlugA, ""), nil),
		http.StatusForbidden, "forbidden")
	assertProblem(t, e.s.do(t, caller{Token: e.tk.MemberA}, http.MethodPost, acceptance, check), http.StatusForbidden, "forbidden")
	stranger := caller{Token: adminOf(t, e.B)}
	assertProblem(t, e.s.do(t, stranger, http.MethodGet, consistencyPath(e.SlugA, ""), nil), http.StatusNotFound, "not_found")
	assertProblem(t, e.s.do(t, stranger, http.MethodPost, acceptance, check), http.StatusNotFound, "not_found")
	require.NoError(t, fixtures(t).Member(ctx, e.B, e.MemberB, domain.RoleAdmin))
	strangerSession := e.s.browser(t)
	strangerSession.mustLogin(names["memberB"], testPassword)
	assertProblem(t, strangerSession.request(http.MethodPost, removal, check), http.StatusNotFound, "not_found")

	marked := caller{Token: e.tk.AdminA, Agent: "claude-code/opus/s1"}
	body := assertProblem(t, e.s.do(t, marked, http.MethodPost, acceptance, check), http.StatusForbidden, "agent_forbidden")
	assert.Equal(t, "hard-off: "+auth.HardOffAdministration, body["detail"])
	assertProblem(t, e.s.do(t, marked, http.MethodPost, removal, check), http.StatusForbidden, "session_required")
	assertProblem(t, e.s.do(t, caller{Token: e.tk.AgentA}, http.MethodPost, removal, check), http.StatusForbidden, "session_required")
	assertProblem(t, session.request(http.MethodPost, removal, check, withHeader(auth.AgentHeader, "chat/stub/c1")),
		http.StatusForbidden, "agent_forbidden")
	member := e.s.browser(t)
	member.mustLogin(names["memberA"], testPassword)
	assertProblem(t, member.request(http.MethodPost, removal, check), http.StatusForbidden, "forbidden")

	ok := session.request(http.MethodPost, removal, check)
	requireOK(t, ok)
	assert.Equal(t, apigen.OrphanRemoval{}, decode[apigen.OrphanRemoval](t, ok), "a check without orphans changes nothing")
	assert.Zero(t, scalar[int](t, `SELECT count(*) FROM audit_events WHERE tenant_id = $1 AND entity_type = 'attachment_consistency'`, e.A),
		"no refused or empty act is recorded")

	// Row-level security holds the result to the administrators, under the
	// API's own checks as well.
	err = openRuntime(t).InTenant(as(e.MemberA), e.A, func(r *store.Reader) error {
		_, err := r.GetAttachmentConsistency(ctx, e.A)
		return err
	})
	assert.ErrorContains(t, err, "no rows", "a member's transaction reads no result")
}

// exportAge is the seconds since the tenant's last export a replica that
// starts answers at its first scrape.
func exportAge(t *testing.T, tenant uuid.UUID) float64 {
	t.Helper()
	m := metrics.New()
	replica, err := store.Open(context.Background(), env.RuntimeURL, store.Options{Metrics: m})
	require.NoError(t, err)
	defer replica.Close()
	samples, err := m.Samples()
	require.NoError(t, err)
	require.True(t, metrics.Has(samples, "cowork_consistency_last_export_age_seconds", "tenant", tenant.String()),
		"every tenant has the age of its last export")
	return metrics.Sum(samples, "cowork_consistency_last_export_age_seconds", "tenant", tenant.String())
}

// docs/adr/0059 D2, D3, docs/adr/0060 D4, D6: when a tenant was last exported
// — a project of it or the whole tenant, by anybody, as the act exported
// records it — is read from the audit record: by its administrators beside the
// consistency check, null while it never was, and by a scrape on any replica
// as the seconds since it, counted from the tenant's creation while it was
// never exported, so that the alert sees a schedule nobody set up. A ticket's
// Markdown is no export of the tenant. Across the tenants, the job's read
// admits those acts and no other row of the audit record.
func TestTheLastExportIsReadFromTheAuditRecord(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	admin, member := caller{Token: e.tk.AdminA}, caller{Token: e.tk.MemberA}
	require.NoError(t, f.Exec(e.ctx, `UPDATE tenants SET created_at = now() - interval '10 days' WHERE id = $1`, e.A))
	require.NoError(t, f.Exec(e.ctx, `UPDATE tenants SET created_at = now() - interval '3 days' WHERE id = $1`, e.B))
	const day = 24 * 60 * 60.0

	assert.True(t, e.consistencyOf(t, admin, e.SlugA).LastExportedAt.IsNull(), "never exported")
	assert.InDelta(t, 10*day, exportAge(t, e.A), 60, "never exported: counted from the tenant's creation")

	tk := e.file(t, member, "ALPHA", task("Exported alone"))
	res := e.s.do(t, member, http.MethodGet, fmt.Sprintf("%s/%d/markdown", e.projectTickets("ALPHA"), tk.Number), nil)
	require.Equal(t, http.StatusOK, res.StatusCode)
	assert.True(t, e.consistencyOf(t, admin, e.SlugA).LastExportedAt.IsNull(), "a ticket's Markdown is no export of the tenant")
	assert.InDelta(t, 10*day, exportAge(t, e.A), 60)

	res, _, _ = e.exportOf(t, member, "ALPHA")
	require.Equal(t, http.StatusOK, res.StatusCode)
	last := e.consistencyOf(t, admin, e.SlugA).LastExportedAt.MustGet()
	assert.WithinDuration(t, time.Now(), last, time.Minute, "a member's export of a project is the tenant's last")
	assert.Less(t, exportAge(t, e.A), 60.0)
	assert.InDelta(t, 3*day, exportAge(t, e.B), 60, "another tenant's export is not B's")

	res, _, _ = e.exportOf(t, admin, "")
	require.Equal(t, http.StatusOK, res.StatusCode)
	assert.False(t, e.consistencyOf(t, admin, e.SlugA).LastExportedAt.MustGet().Before(last), "the tenant's export is its last")

	job := settings{"app.job": store.JobConsistencyCheck}
	assert.Equal(t, int64(2), count(t, job, `SELECT count(*) FROM audit_events WHERE tenant_id = $1`, e.A),
		"the job reads the project's and the tenant's export, and nothing else of the tenant's record")
	assert.Zero(t, count(t, settings{"app.job": "ticket-purge"}, `SELECT count(*) FROM audit_events WHERE tenant_id = $1`, e.A),
		"another job reads none of them")
	assert.Zero(t, count(t, settings{}, `SELECT count(*) FROM audit_events WHERE tenant_id = $1`, e.A), "nor a context without a job")
}

// docs/adr/0059 D5: `cowork check-consistency`, the step of a restore, runs
// the check at once against the database and the bucket the server uses and
// prints every tenant's counts — never a file name.
func TestTheCheckRunsAtOnceFromTheCommandLine(t *testing.T) {
	ctx := context.Background()
	iso := newIsolated(t)
	bin := filepath.Join(t.TempDir(), "cowork")
	out, err := exec.Command("go", "build", "-o", bin, "github.com/guided-traffic/cowork/backend/cmd/cowork").CombinedOutput()
	require.NoError(t, err, "go build: %s", out)

	person, err := iso.F.Person(ctx, "ida", "Ida")
	require.NoError(t, err)
	tenant, err := iso.F.Tenant(ctx, "restored", "Restored")
	require.NoError(t, err)
	project, err := iso.F.Project(ctx, tenant, "REST", "Restore")
	require.NoError(t, err)
	ticket, _, err := iso.F.Ticket(ctx, tenant, project, person, "Its file is gone")
	require.NoError(t, err)
	require.NoError(t, iso.F.Exec(ctx, `INSERT INTO attachments (tenant_id, ticket_id, file_name, size, sha256, content_type, uploaded_by)
		VALUES ($1, $2, 'secret-plan.png', 1, sha256('x'::bytea), 'image/png', $3)`, tenant, ticket, person))
	putObject(t, storage.Key(tenant, v7At(time.Now().Add(-2*store.OrphanGrace))), pngBytes)

	cmd := exec.Command(bin, "check-consistency")
	cmd.Env = []string{
		config.EnvDatabaseURL + "=" + iso.RuntimeURL,
		config.EnvS3Endpoint + "=" + env.Storage.Endpoint, config.EnvS3Bucket + "=" + env.Storage.Bucket,
		config.EnvS3AccessKeyID + "=" + env.Storage.AccessKeyID, config.EnvS3SecretAccessKey + "=" + env.Storage.SecretAccessKey,
		config.EnvLogFormat + "=text",
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	require.NoError(t, cmd.Run(), "stderr: %s", stderr.String())
	assert.Contains(t, stdout.String(), "1 tenants, 1 dangling, 0 accepted as lost, 1 orphaned objects")
	assert.Contains(t, stdout.String(), "tenant restored ("+tenant.String()+"): 1 dangling, 0 accepted as lost, 1 orphaned objects")
	assert.NotContains(t, stdout.String()+stderr.String(), "secret-plan")
	n, err := iso.F.QueryCount(ctx, "SELECT count(*) FROM consistency_checks WHERE tenant_id = $1 AND dangling = 1 AND orphans = 1", tenant)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n, "the result is stored, as the job stores it")
	assert.True(t, strings.HasPrefix(stdout.String(), "consistency check at "))
}
