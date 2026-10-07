-- GitHub's webhook (docs/adr/0071): the tenant's secret as its
-- administrators and the webhook read it, and a ticket's pull requests.

-- name: GetGitHubWebhook :one
-- When and by whom the tenant's secret was made, never the secret: the
-- restrictive policy of migration 41 shows the row to the tenant's
-- administrators and the webhook's job only.
SELECT s.created_at, s.created_by, u.username AS created_by_username, u.display_name AS created_by_name
FROM github_webhook_secrets s
LEFT JOIN users u ON u.id = s.created_by
WHERE s.tenant_id = sqlc.arg(tenant_id);

-- name: GetGitHubWebhookSecret :one
-- The sealed secret a delivery's signature is verified with (D3).
SELECT secret FROM github_webhook_secrets WHERE tenant_id = sqlc.arg(tenant_id);

-- name: ListTicketPullRequests :many
-- What the webhook linked to a ticket the caller sees (D6), oldest link
-- first, without the links a person removed. The ticket's predicate holds
-- them as it holds the ticket's other children (docs/adr/0065 D1).
SELECT p.id, p.kind, p.repository, p.number, p.sha, p.title, p.state, p.url, p.author, p.merged_at, p.found_in,
       p.first_seen_at, p.last_seen_at
FROM ticket_pull_requests p
JOIN tickets t ON t.tenant_id = p.tenant_id AND t.id = p.ticket_id
WHERE p.tenant_id = sqlc.arg(tenant_id) AND p.ticket_id = sqlc.arg(ticket_id) AND p.removed_at IS NULL
  AND t.deleted_at IS NULL AND app_ticket_visible(t.project_id, t.confidential, t.assignee_id, t.reporter_id)
  AND (sqlc.narg(after)::uuid IS NULL OR p.id > sqlc.narg(after)::uuid)
ORDER BY p.id
LIMIT sqlc.arg(page_size);
