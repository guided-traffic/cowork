//go:build integration

package integration

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/store"
	"github.com/guided-traffic/cowork/backend/internal/store/readq"
	"github.com/guided-traffic/cowork/backend/test/fixture"
)

// openRuntime opens the store as the runtime role, the way `cowork serve`
// does.
func openRuntime(t *testing.T) *store.DB {
	t.Helper()
	return openStore(t, env.RuntimeURL)
}

func openStore(t *testing.T, databaseURL string) *store.DB {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := store.Open(ctx, databaseURL, store.Options{})
	require.NoError(t, err)
	t.Cleanup(db.Close)
	return db
}

// fixtures opens the administrative fixture connection to the test database.
func fixtures(t *testing.T) *fixture.DB {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	f, err := fixture.Connect(ctx, env.AdminURL)
	require.NoError(t, err)
	t.Cleanup(f.Close)
	return f
}

var slugCounter atomic.Int64

// uniqueSlug returns a tenant slug no other test of this run uses.
func uniqueSlug(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, slugCounter.Add(1))
}

// world is the two-tenant, several-person shape every isolation test draws
// from (docs/adr/0027 D7, docs/adr/0038 D3): an admin, a member and a viewer
// of tenant A, a member of tenant B, and one person in both.
type world struct {
	A, B                     uuid.UUID
	SlugA, SlugB             string
	AdminA, MemberA, ViewerA uuid.UUID
	MemberB, Both            uuid.UUID
	ProjectA, ProjectB       uuid.UUID
}

func newWorld(t *testing.T) world {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	f := fixtures(t)
	var w world
	var err error
	w.SlugA, w.SlugB = uniqueSlug("tenant-a"), uniqueSlug("tenant-b")
	w.A, err = f.Tenant(ctx, w.SlugA, "Tenant A")
	require.NoError(t, err)
	w.B, err = f.Tenant(ctx, w.SlugB, "Tenant B")
	require.NoError(t, err)

	person := func(role string) uuid.UUID {
		id, err := f.Person(ctx, uniqueSlug(role), role)
		require.NoError(t, err)
		return id
	}
	w.AdminA, w.MemberA, w.ViewerA = person("admin-a"), person("member-a"), person("viewer-a")
	w.MemberB, w.Both = person("member-b"), person("both")
	for _, g := range []struct {
		tenant, user uuid.UUID
		role         domain.Role
	}{
		{w.A, w.AdminA, domain.RoleAdmin},
		{w.A, w.MemberA, domain.RoleMember},
		{w.A, w.ViewerA, domain.RoleViewer},
		{w.B, w.MemberB, domain.RoleMember},
		{w.A, w.Both, domain.RoleMember},
		{w.B, w.Both, domain.RoleMember},
	} {
		require.NoError(t, f.Member(ctx, g.tenant, g.user, g.role))
	}
	w.ProjectA, err = f.Project(ctx, w.A, "ALPHA", "Alpha")
	require.NoError(t, err)
	w.ProjectB, err = f.Project(ctx, w.B, "BETA", "Beta")
	require.NoError(t, err)
	return w
}

// as returns a context whose store calls act for the person.
func as(person uuid.UUID) context.Context {
	return store.WithCaller(context.Background(), store.Caller{UserID: person, RequestID: uuid.Must(uuid.NewV7())})
}

// databaseName is the name of this run's database.
func databaseName(t *testing.T) string {
	t.Helper()
	u, err := url.Parse(env.RuntimeURL)
	require.NoError(t, err)
	return strings.TrimPrefix(u.Path, "/")
}

func readTenantParams(slug string, user uuid.UUID) readq.GetTenantForPersonParams {
	return readq.GetTenantForPersonParams{Slug: slug, UserID: user}
}

// tenantBoundTables lists every table of the schema with a tenant_id column,
// from the catalog.
func tenantBoundTables(t *testing.T) []string {
	t.Helper()
	rows, err := fixtures(t).Query(context.Background(),
		`SELECT c.relname FROM pg_class c
		 JOIN pg_attribute a ON a.attrelid = c.oid AND a.attname = 'tenant_id' AND NOT a.attisdropped
		 WHERE c.relkind = 'r' AND c.relnamespace = 'public'::regnamespace
		 ORDER BY c.relname`)
	require.NoError(t, err)
	tables, err := pgx.CollectRows(rows, pgx.RowTo[string])
	require.NoError(t, err)
	return tables
}

// seedEveryTenantTable gives every tenant-bound table a row in each tenant of
// the world, so the isolation tests prove something for every table.
func seedEveryTenantTable(t *testing.T, w world) {
	t.Helper()
	ctx := context.Background()
	f := fixtures(t)
	db := openRuntime(t)
	for _, s := range []struct {
		tenant, project, person uuid.UUID
	}{{w.A, w.ProjectA, w.MemberA}, {w.B, w.ProjectB, w.MemberB}} {
		require.NoError(t, f.Exec(ctx,
			`INSERT INTO project_access (tenant_id, project_id, user_id, role) VALUES ($1, $2, $3, 'member')
			 ON CONFLICT DO NOTHING`, s.tenant, s.project, s.person))
		require.NoError(t, f.Exec(ctx, `INSERT INTO group_mappings (tenant_id, group_name, role) VALUES ($1, 'seed-group', 'member')
			ON CONFLICT DO NOTHING`, s.tenant))
		first, _, err := f.Ticket(ctx, s.tenant, s.project, s.person, "seed")
		require.NoError(t, err)
		second, _, err := f.Ticket(ctx, s.tenant, s.project, s.person, "seed 2")
		require.NoError(t, err)
		require.NoError(t, f.Exec(ctx, `INSERT INTO ticket_links (tenant_id, type, source_id, target_id, created_by)
			VALUES ($1, 'blocks', $2, $3, $4)`, s.tenant, first, second, s.person))
		require.NoError(t, f.Exec(ctx, `INSERT INTO questions (tenant_id, ticket_id, number, question, asked_by)
			VALUES ($1, $2, 1, 'seed?', $3)`, s.tenant, first, s.person))
		var comment uuid.UUID
		require.NoError(t, f.QueryRow(ctx, `INSERT INTO comments (tenant_id, ticket_id, author_id, body)
			VALUES ($1, $2, $3, 'seed') RETURNING id`, s.tenant, first, s.person).Scan(&comment))
		require.NoError(t, f.Exec(ctx, `INSERT INTO comment_revisions (tenant_id, comment_id, body, edited_by)
			VALUES ($1, $2, 'seed before', $3)`, s.tenant, comment, s.person))
		require.NoError(t, f.Exec(ctx, `INSERT INTO ticket_interest (tenant_id, ticket_id, user_id, weight)
			VALUES ($1, $2, $3, 'watch')`, s.tenant, first, s.person))
		var entry uuid.UUID
		require.NoError(t, f.QueryRow(ctx, `INSERT INTO time_entries (tenant_id, ticket_id, person_id, author_id, minutes, day)
			VALUES ($1, $2, $3, $3, 30, '2026-01-01') RETURNING id`, s.tenant, first, s.person).Scan(&entry))
		require.NoError(t, f.Exec(ctx, `INSERT INTO time_entry_revisions (tenant_id, entry_id, minutes, day, note, edited_by)
			VALUES ($1, $2, 15, '2026-01-01', '', $3)`, s.tenant, entry, s.person))
		require.NoError(t, f.Exec(ctx, `INSERT INTO attachments (tenant_id, ticket_id, file_name, size, sha256, content_type, uploaded_by)
			VALUES ($1, $2, 'seed.txt', 0, sha256(''::bytea), 'text/plain; charset=utf-8', $3)`, s.tenant, first, s.person))
		_, tokenID, err := f.Token(ctx, fixture.TokenSpec{UserID: s.person})
		require.NoError(t, err)
		require.NoError(t, f.Exec(ctx, `INSERT INTO idempotency_keys
			(token_id, user_id, tenant_id, key, fingerprint, response_status, response_body, expires_at)
			VALUES ($1, $2, $3, $4, $5, 201, '\x', now() + interval '1 hour')`,
			tokenID, s.person, s.tenant, uuid.Must(uuid.NewV7()), make([]byte, 32)))
		_, err = db.Mutate(as(s.person), s.tenant, func(wr *store.Writer) error {
			wr.Record(store.Event{EntityType: "seed", Action: "created"})
			return nil
		})
		require.NoError(t, err)
	}
}
