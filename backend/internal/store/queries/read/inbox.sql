-- The person's inbox in one tenant (docs/adr/0020 D1); the person-level route
-- reads it once per tenant of the person (docs/adr/0021 D5). A notification of
-- a ticket the person no longer sees, or whose act is on such a ticket, is
-- absent (docs/adr/0065 D5).

-- name: ListInbox :many
-- Newest first, each with the act it renders from (D3) and the tickets as
-- they are now: the one it is about, and the one the act is on.
SELECT n.id, n.reason::text AS reason, n.read_at, n.created_at,
       tp.key AS project_key, t.number, t.title, t.state,
       a.actor_user_id, u.username AS actor_username, u.display_name AS actor_name, a.actor_system,
       a.agent, a.token_id, a.token_name, a.entity_type, a.entity_id, a.action::text AS action, a.before, a.after,
       a.reason AS act_reason, a.note AS act_note, a.explained_by_comment_id, a.refs, a.id AS act_id,
       a.created_at AS act_at,
       xt.id AS act_ticket_id, xp.key AS act_project_key, xt.number AS act_number, xt.title AS act_title,
       xt.state AS act_state,
       (c.withdrawn_at IS NOT NULL OR q.withdrawn_at IS NOT NULL)::boolean AS withdrawn
FROM notifications n
JOIN tickets t ON t.tenant_id = n.tenant_id AND t.id = n.ticket_id
JOIN projects tp ON tp.tenant_id = t.tenant_id AND tp.id = t.project_id
JOIN audit_events a ON a.tenant_id = n.tenant_id AND a.id = n.audit_event_id
JOIN tickets xt ON xt.tenant_id = a.tenant_id AND xt.id = a.ticket_id
JOIN projects xp ON xp.tenant_id = xt.tenant_id AND xp.id = xt.project_id
LEFT JOIN users u ON u.id = a.actor_user_id
LEFT JOIN comments c ON a.entity_type = 'comment' AND c.tenant_id = a.tenant_id AND c.id = a.entity_id
LEFT JOIN questions q ON a.entity_type = 'question' AND q.tenant_id = a.tenant_id AND q.id = a.entity_id
WHERE n.tenant_id = sqlc.arg(tenant_id) AND n.user_id = sqlc.arg(user_id)
  AND app_ticket_visible(t.project_id, t.confidential, t.assignee_id, t.reporter_id)
  AND app_ticket_visible(xt.project_id, xt.confidential, xt.assignee_id, xt.reporter_id)
  AND (sqlc.narg(before)::uuid IS NULL OR n.id < sqlc.narg(before)::uuid)
ORDER BY n.id DESC
LIMIT sqlc.arg(page_size);

-- name: CountUnread :one
-- The unread count of the page header (D1), over what ListInbox shows.
SELECT count(*)::integer AS unread
FROM notifications n
JOIN tickets t ON t.tenant_id = n.tenant_id AND t.id = n.ticket_id
JOIN audit_events a ON a.tenant_id = n.tenant_id AND a.id = n.audit_event_id
JOIN tickets xt ON xt.tenant_id = a.tenant_id AND xt.id = a.ticket_id
WHERE n.tenant_id = sqlc.arg(tenant_id) AND n.user_id = sqlc.arg(user_id) AND n.read_at IS NULL
  AND app_ticket_visible(t.project_id, t.confidential, t.assignee_id, t.reporter_id)
  AND app_ticket_visible(xt.project_id, xt.confidential, xt.assignee_id, xt.reporter_id);

-- name: FindNotification :one
-- Whether one of the person's notifications is in the tenant, and read, over
-- what ListInbox shows: marking it read finds its tenant so.
SELECT (n.read_at IS NOT NULL)::boolean AS read
FROM notifications n
JOIN tickets t ON t.tenant_id = n.tenant_id AND t.id = n.ticket_id
JOIN audit_events a ON a.tenant_id = n.tenant_id AND a.id = n.audit_event_id
JOIN tickets xt ON xt.tenant_id = a.tenant_id AND xt.id = a.ticket_id
WHERE n.tenant_id = sqlc.arg(tenant_id) AND n.user_id = sqlc.arg(user_id) AND n.id = sqlc.arg(id)
  AND app_ticket_visible(t.project_id, t.confidential, t.assignee_id, t.reporter_id)
  AND app_ticket_visible(xt.project_id, xt.confidential, xt.assignee_id, xt.reporter_id);
