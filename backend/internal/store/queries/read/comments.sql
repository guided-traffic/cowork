-- A comment is visible with its ticket (docs/adr/0065 D4). A withdrawn
-- comment's text is read here and dropped by the API's comment view, the one
-- place that renders a comment (docs/adr/0015 D3).

-- name: ListComments :many
SELECT c.id, c.author_id, u.username AS author_username, u.display_name AS author_name, c.agent,
       c.token_id, c.token_name, c.body, c.withdrawn_at,
       EXISTS (SELECT 1 FROM comment_revisions r WHERE r.tenant_id = c.tenant_id AND r.comment_id = c.id) AS edited,
       ARRAY(SELECT a.action::text FROM audit_events a
             WHERE a.tenant_id = c.tenant_id AND a.explained_by_comment_id = c.id ORDER BY a.id)::text[] AS explains,
       c.mentions, c.version, c.created_at, c.updated_at
FROM comments c
JOIN tickets t ON t.tenant_id = c.tenant_id AND t.id = c.ticket_id
LEFT JOIN users u ON u.id = c.author_id
WHERE c.tenant_id = sqlc.arg(tenant_id) AND c.ticket_id = sqlc.arg(ticket_id)
  AND app_ticket_visible(t.project_id, t.confidential, t.assignee_id, t.reporter_id)
  AND (sqlc.narg(after)::uuid IS NULL
       OR (sqlc.arg(descending)::boolean AND c.id < sqlc.narg(after)::uuid)
       OR (NOT sqlc.arg(descending)::boolean AND c.id > sqlc.narg(after)::uuid))
ORDER BY CASE WHEN sqlc.arg(descending)::boolean THEN c.id END DESC,
         CASE WHEN NOT sqlc.arg(descending)::boolean THEN c.id END ASC
LIMIT sqlc.arg(page_size);

-- name: GetComment :one
SELECT c.id, c.author_id, u.username AS author_username, u.display_name AS author_name, c.agent,
       c.token_id, c.token_name, c.body, c.withdrawn_at,
       EXISTS (SELECT 1 FROM comment_revisions r WHERE r.tenant_id = c.tenant_id AND r.comment_id = c.id) AS edited,
       ARRAY(SELECT a.action::text FROM audit_events a
             WHERE a.tenant_id = c.tenant_id AND a.explained_by_comment_id = c.id ORDER BY a.id)::text[] AS explains,
       c.mentions, c.version, c.created_at, c.updated_at
FROM comments c
JOIN tickets t ON t.tenant_id = c.tenant_id AND t.id = c.ticket_id
LEFT JOIN users u ON u.id = c.author_id
WHERE c.tenant_id = sqlc.arg(tenant_id) AND c.ticket_id = sqlc.arg(ticket_id) AND c.id = sqlc.arg(id)
  AND app_ticket_visible(t.project_id, t.confidential, t.assignee_id, t.reporter_id);

-- name: ListCommentRevisions :many
-- The previous texts of a comment, oldest first; none once it is withdrawn.
SELECT r.id, r.body, r.edited_by, u.username AS edited_by_username, u.display_name AS edited_by_name,
       r.agent, r.token_id, r.token_name, r.created_at
FROM comment_revisions r
JOIN comments c ON c.tenant_id = r.tenant_id AND c.id = r.comment_id
JOIN tickets t ON t.tenant_id = c.tenant_id AND t.id = c.ticket_id
LEFT JOIN users u ON u.id = r.edited_by
WHERE r.tenant_id = sqlc.arg(tenant_id) AND r.comment_id = sqlc.arg(comment_id) AND c.withdrawn_at IS NULL
  AND app_ticket_visible(t.project_id, t.confidential, t.assignee_id, t.reporter_id)
  AND (sqlc.narg(after)::uuid IS NULL OR r.id > sqlc.narg(after)::uuid)
ORDER BY r.id
LIMIT sqlc.arg(page_size);

-- name: GetCommentForWrite :one
-- The comment with its text, for the writer who has read its ticket.
SELECT c.id, c.author_id, c.agent, c.body, c.mentions, c.withdrawn_at, c.version
FROM comments c
WHERE c.tenant_id = sqlc.arg(tenant_id) AND c.ticket_id = sqlc.arg(ticket_id) AND c.id = sqlc.arg(id);

-- name: ListTicketActivity :many
-- The ticket's acts (docs/adr/0015 D1, D6) without time entries
-- (docs/adr/0017 D9) and without data leaving the system (docs/adr/0026 D5).
-- The caller has read the ticket through the predicate; the refs of each act
-- are checked against it before its payload is shown.
SELECT a.id, a.actor_user_id, u.username AS actor_username, u.display_name AS actor_name, a.actor_system,
       a.agent, a.token_id, a.token_name, a.entity_type, a.entity_id, a.action::text AS action, a.before, a.after, a.reason, a.note,
       a.explained_by_comment_id, a.refs, a.created_at
FROM audit_events a
LEFT JOIN users u ON u.id = a.actor_user_id
WHERE a.tenant_id = sqlc.arg(tenant_id) AND a.ticket_id = sqlc.arg(ticket_id)
  AND a.entity_type <> 'time_entry' AND a.action NOT IN ('downloaded', 'exported', 'booked', 'voided', 'locked')
  AND (sqlc.narg(after)::uuid IS NULL
       OR (sqlc.arg(descending)::boolean AND a.id < sqlc.narg(after)::uuid)
       OR (NOT sqlc.arg(descending)::boolean AND a.id > sqlc.narg(after)::uuid))
ORDER BY CASE WHEN sqlc.arg(descending)::boolean THEN a.id END DESC,
         CASE WHEN NOT sqlc.arg(descending)::boolean THEN a.id END ASC
LIMIT sqlc.arg(page_size);

-- name: VisibleTickets :many
-- The ids of the given tickets the caller can see.
SELECT t.id
FROM tickets t
WHERE t.tenant_id = sqlc.arg(tenant_id) AND t.id = ANY (sqlc.arg(ids)::uuid[])
  AND app_ticket_visible(t.project_id, t.confidential, t.assignee_id, t.reporter_id);
