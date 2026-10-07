-- GitHub's webhook (docs/adr/0071): the tenant's secret, the deliveries, and
-- the links of pull requests and default-branch commits to tickets.

-- name: SetGitHubWebhookSecret :exec
-- The tenant's secret, sealed, made or rotated by an administrator (D1): a
-- rotation replaces the old one at once.
INSERT INTO github_webhook_secrets (tenant_id, secret, created_by, created_at)
VALUES (sqlc.arg(tenant_id), sqlc.arg(secret), sqlc.arg(created_by), sqlc.arg(created_at))
ON CONFLICT (tenant_id) DO UPDATE
    SET secret = EXCLUDED.secret, created_by = EXCLUDED.created_by, created_at = EXCLUDED.created_at;

-- name: DeleteGitHubWebhookSecret :execrows
-- The revocation (D1): from now on the endpoint answers like an unknown
-- tenant.
DELETE FROM github_webhook_secrets WHERE tenant_id = sqlc.arg(tenant_id);

-- name: RecordGitHubDelivery :one
-- A delivery the tenant takes, kept a day (D3). No row when the tenant holds
-- the same delivery unexpired: a repetition, which changes nothing. An expired
-- row of the same id that the job has not removed yet is taken over.
INSERT INTO github_deliveries (tenant_id, delivery, received_at, expires_at)
VALUES (sqlc.arg(tenant_id), sqlc.arg(delivery), sqlc.arg(received_at), sqlc.arg(expires_at))
ON CONFLICT (tenant_id, delivery) DO UPDATE
    SET received_at = EXCLUDED.received_at, expires_at = EXCLUDED.expires_at
    WHERE github_deliveries.expires_at <= EXCLUDED.received_at
RETURNING id;

-- name: DeleteExpiredGitHubDeliveries :execrows
-- The expiry job's sweep across the tenants (migration 42).
DELETE FROM github_deliveries WHERE expires_at <= sqlc.arg(now)::timestamptz;

-- name: RepositoryBoundInTenant :one
-- Whether a project of the tenant binds the repository, whatever its
-- sub-directory (D4): the webhook's system actor asks past the projects'
-- restriction and learns no project.
SELECT EXISTS (SELECT 1 FROM project_repositories r
               WHERE r.tenant_id = sqlc.arg(tenant_id) AND r.identity = sqlc.arg(identity))::boolean AS bound;

-- name: ResolveTicketKeys :many
-- The tickets of the tenant the short keys of a delivery name (D5), each key
-- <PROJECT>-<number> as the caller parsed it — a project key has no hyphen: a
-- key of no ticket, or of a deleted one, is no row.
-- visibility: exempt (the webhook's system actor resolves the keys a signed delivery names; whoever reads a link is held to the ticket's predicate)
SELECT t.id, p.key AS project_key, t.number
FROM unnest(sqlc.arg(keys)::text[]) AS k (key)
JOIN projects p ON p.tenant_id = sqlc.arg(tenant_id) AND p.key = split_part(k.key, '-', 1)
JOIN tickets t ON t.tenant_id = p.tenant_id AND t.project_id = p.id
                  AND t.number = split_part(k.key, '-', 2)::integer
WHERE t.deleted_at IS NULL
ORDER BY p.key, t.number;

-- name: InsertTicketPullRequest :one
-- A pull request's link to a ticket (D6). No row when the ticket holds the
-- link already, or held it until a person removed it: a removal stays.
INSERT INTO ticket_pull_requests (tenant_id, ticket_id, kind, repository, number, title, state, url, author,
                                  merged_at, found_in, source_updated_at, first_seen_at, last_seen_at)
VALUES (sqlc.arg(tenant_id), sqlc.arg(ticket_id), 'pull_request', sqlc.arg(repository), sqlc.arg(number)::integer,
        sqlc.arg(title), sqlc.arg(state), sqlc.arg(url), sqlc.narg(author), sqlc.narg(merged_at), sqlc.arg(found_in),
        sqlc.narg(source_updated_at), sqlc.arg(seen_at), sqlc.arg(seen_at))
ON CONFLICT (tenant_id, ticket_id, repository, number) WHERE kind = 'pull_request' DO NOTHING
RETURNING id;

-- name: InsertTicketCommit :one
-- A default-branch commit's link to a ticket (D6): merged when it was pushed.
-- No row when the ticket holds the link already, or held it until a person
-- removed it.
INSERT INTO ticket_pull_requests (tenant_id, ticket_id, kind, repository, sha, title, state, url, author,
                                  merged_at, found_in, first_seen_at, last_seen_at)
VALUES (sqlc.arg(tenant_id), sqlc.arg(ticket_id), 'commit', sqlc.arg(repository), sqlc.arg(sha)::text,
        sqlc.arg(title), 'merged', sqlc.arg(url), sqlc.narg(author), sqlc.arg(seen_at)::timestamptz,
        sqlc.arg(found_in), sqlc.arg(seen_at)::timestamptz, sqlc.arg(seen_at)::timestamptz)
ON CONFLICT (tenant_id, ticket_id, repository, sha) WHERE kind = 'commit' DO NOTHING
RETURNING id;

-- name: UpdatePullRequestFacts :many
-- A pull request's facts on every ticket it is linked to (D6), from a delivery
-- no older than what a row holds: one GitHub sent before, or one replayed,
-- changes nothing. A link a person removed stays as it was. Each row answers
-- what it held before, its ticket's key and whether the ticket is deleted —
-- whose link keeps the facts and records no act.
-- visibility: exempt (the webhook's system actor keeps every link of the pull request; nobody reads through it)
-- deletion: exempt (a deleted ticket's link keeps the pull request's facts and records no act)
WITH old AS (
    SELECT p.id, p.state, p.title, p.url, p.author, pr.key AS project_key, t.number AS ticket_number,
           (t.deleted_at IS NOT NULL)::boolean AS ticket_deleted
    FROM ticket_pull_requests p
    JOIN tickets t ON t.tenant_id = p.tenant_id AND t.id = p.ticket_id
    JOIN projects pr ON pr.tenant_id = t.tenant_id AND pr.id = t.project_id
    WHERE p.tenant_id = sqlc.arg(tenant_id) AND p.kind = 'pull_request' AND p.repository = sqlc.arg(repository)
      AND p.number = sqlc.arg(number)::integer AND p.removed_at IS NULL
      AND (p.source_updated_at IS NULL OR p.source_updated_at <= sqlc.arg(source_updated_at)::timestamptz)
    FOR UPDATE OF p
)
UPDATE ticket_pull_requests p
SET title = sqlc.arg(title), state = sqlc.arg(state), url = sqlc.arg(url), author = sqlc.narg(author),
    merged_at = sqlc.narg(merged_at), source_updated_at = sqlc.arg(source_updated_at)::timestamptz,
    last_seen_at = sqlc.arg(seen_at)
FROM old
WHERE p.id = old.id
RETURNING p.id, p.ticket_id, old.project_key, old.ticket_number, old.ticket_deleted, old.state AS state_before,
          (old.title IS DISTINCT FROM p.title OR old.url IS DISTINCT FROM p.url
           OR old.author IS DISTINCT FROM p.author)::boolean AS facts_changed;

-- name: RemoveTicketPullRequest :one
-- A person removes a wrong link (docs/adr/0071 Residual risks): the row stays,
-- so a later delivery that names the ticket does not bring it back. No row
-- when the link was removed already or is another ticket's.
UPDATE ticket_pull_requests
SET removed_at = sqlc.arg(removed_at)::timestamptz, removed_by = sqlc.arg(removed_by)::uuid
WHERE tenant_id = sqlc.arg(tenant_id) AND ticket_id = sqlc.arg(ticket_id) AND id = sqlc.arg(id) AND removed_at IS NULL
RETURNING kind, repository, number, sha;
