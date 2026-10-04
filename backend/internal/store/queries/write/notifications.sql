-- Whom an act tells in their inbox and how it is marked read (docs/adr/0020).
-- The recipients are read inside the act's transaction, past the writer's own
-- predicate: whom an act tells is decided by each recipient's sight of the
-- ticket, never by the writer's.

-- name: ListWatchers :many
-- The watchers of a ticket (docs/adr/0013 D6): every person with a stake of
-- any weight, the assignee, the reporter, and whoever asked or was asked an
-- open question on it.
-- visibility: exempt (whom an act tells; NoticeRecipients holds each to their own sight of the ticket)
SELECT i.user_id FROM ticket_interest i
WHERE i.tenant_id = sqlc.arg(tenant_id) AND i.ticket_id = sqlc.arg(ticket_id)
UNION
SELECT t.assignee_id FROM tickets t
WHERE t.tenant_id = sqlc.arg(tenant_id) AND t.id = sqlc.arg(ticket_id) AND t.assignee_id IS NOT NULL
UNION
SELECT t.reporter_id FROM tickets t
WHERE t.tenant_id = sqlc.arg(tenant_id) AND t.id = sqlc.arg(ticket_id)
UNION
SELECT q.asked_by FROM questions q
WHERE q.tenant_id = sqlc.arg(tenant_id) AND q.ticket_id = sqlc.arg(ticket_id) AND q.status = 'open'
UNION
SELECT q.asked_of FROM questions q
WHERE q.tenant_id = sqlc.arg(tenant_id) AND q.ticket_id = sqlc.arg(ticket_id) AND q.status = 'open'
  AND q.asked_of IS NOT NULL;

-- name: ListTicketsBlockedBy :many
-- The tickets a ticket blocks (docs/adr/0012 D1): the watchers of each hear
-- when it reaches done or dropped (docs/adr/0020 D2).
SELECT l.target_id FROM ticket_links l
WHERE l.tenant_id = sqlc.arg(tenant_id) AND l.source_id = sqlc.arg(ticket_id) AND l.type = 'blocks'
ORDER BY l.target_id;

-- name: NoticeRecipients :many
-- Of the persons an act names, those it tells: active, not the actor — a
-- person's own act and their agent's tell them nothing (docs/adr/0020 D2) —
-- and able to see the ticket the notification is about and the ticket the act
-- is on (docs/adr/0065 D5).
SELECT u.id FROM users u
WHERE u.id = ANY (sqlc.arg(people)::uuid[]) AND u.deactivated_at IS NULL
  AND u.id IS DISTINCT FROM sqlc.narg(actor)::uuid
  AND person_sees_ticket(sqlc.arg(tenant_id), sqlc.arg(ticket_id), u.id)
  AND person_sees_ticket(sqlc.arg(tenant_id), sqlc.arg(act_ticket_id), u.id)
ORDER BY u.id;

-- name: InsertNotifications :exec
INSERT INTO notifications (tenant_id, user_id, ticket_id, audit_event_id, reason)
SELECT sqlc.arg(tenant_id), r.id, sqlc.arg(ticket_id), sqlc.arg(audit_event_id), sqlc.arg(reason)::notification_reason
FROM unnest(sqlc.arg(recipients)::uuid[]) AS r (id);

-- name: QuestionAskedOf :one
-- The person a question is asked of, whom its events reach across their
-- tenants (docs/adr/0054 D1); null for a question open in the tenant.
SELECT asked_of FROM questions
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id);

-- name: MarkNotificationRead :execrows
-- One of the person's notifications, read now; one they no longer see stays
-- as it is (docs/adr/0065 D5).
UPDATE notifications n SET read_at = sqlc.arg(now)::timestamptz
WHERE n.tenant_id = sqlc.arg(tenant_id) AND n.user_id = sqlc.arg(user_id) AND n.id = sqlc.arg(id)
  AND n.read_at IS NULL
  AND EXISTS (SELECT 1
              FROM tickets t
              JOIN audit_events a ON a.tenant_id = n.tenant_id AND a.id = n.audit_event_id
              JOIN tickets xt ON xt.tenant_id = a.tenant_id AND xt.id = a.ticket_id
              WHERE t.tenant_id = n.tenant_id AND t.id = n.ticket_id
                AND app_ticket_visible(t.project_id, t.confidential, t.assignee_id, t.reporter_id)
                AND app_ticket_visible(xt.project_id, xt.confidential, xt.assignee_id, xt.reporter_id));

-- name: MarkInboxRead :execrows
-- The person's unread notifications in the tenant up to and including one,
-- read now (docs/adr/0020 D6): what arrived after the one the person saw
-- stays unread. Those of tickets they no longer see stay as they are.
UPDATE notifications n SET read_at = sqlc.arg(now)::timestamptz
WHERE n.tenant_id = sqlc.arg(tenant_id) AND n.user_id = sqlc.arg(user_id) AND n.id <= sqlc.arg(through)
  AND n.read_at IS NULL
  AND EXISTS (SELECT 1
              FROM tickets t
              JOIN audit_events a ON a.tenant_id = n.tenant_id AND a.id = n.audit_event_id
              JOIN tickets xt ON xt.tenant_id = a.tenant_id AND xt.id = a.ticket_id
              WHERE t.tenant_id = n.tenant_id AND t.id = n.ticket_id
                AND app_ticket_visible(t.project_id, t.confidential, t.assignee_id, t.reporter_id)
                AND app_ticket_visible(xt.project_id, xt.confidential, xt.assignee_id, xt.reporter_id));

-- name: DeleteReadNotifications :execrows
-- The retention of docs/adr/0020 D6: a notification read before the bound
-- goes; an unread one stays.
DELETE FROM notifications WHERE read_at < sqlc.arg(read_before)::timestamptz;
