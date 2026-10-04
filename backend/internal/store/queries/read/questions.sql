-- A question is visible when its ticket is (docs/adr/0065 D4).

-- name: ListQuestions :many
SELECT q.id, q.number, q.question, q.options, q.recommendation, q.answer, q.status,
       q.asked_by, ab.username AS asked_by_username, ab.display_name AS asked_by_name, q.asked_by_agent,
       q.asked_by_token_id, q.asked_by_token_name,
       q.asked_of, ao.username AS asked_of_username, ao.display_name AS asked_of_name,
       q.answered_by, an.username AS answered_by_username, an.display_name AS answered_by_name,
       q.answered_at, q.recorded_by_agent, q.answered_by_token_id, q.answered_by_token_name,
       q.withdrawn_at, q.version, q.created_at, q.updated_at
FROM questions q
JOIN tickets t ON t.tenant_id = q.tenant_id AND t.id = q.ticket_id
LEFT JOIN users ab ON ab.id = q.asked_by
LEFT JOIN users ao ON ao.id = q.asked_of
LEFT JOIN users an ON an.id = q.answered_by
WHERE q.tenant_id = sqlc.arg(tenant_id) AND q.ticket_id = sqlc.arg(ticket_id)
  AND app_ticket_visible(t.project_id, t.confidential, t.assignee_id, t.reporter_id)
  AND (sqlc.narg(after)::integer IS NULL OR q.number > sqlc.narg(after)::integer)
ORDER BY q.number
LIMIT sqlc.arg(page_size);

-- name: GetQuestion :one
SELECT q.id, q.number, q.question, q.options, q.recommendation, q.answer, q.status,
       q.asked_by, ab.username AS asked_by_username, ab.display_name AS asked_by_name, q.asked_by_agent,
       q.asked_by_token_id, q.asked_by_token_name,
       q.asked_of, ao.username AS asked_of_username, ao.display_name AS asked_of_name,
       q.answered_by, an.username AS answered_by_username, an.display_name AS answered_by_name,
       q.answered_at, q.recorded_by_agent, q.answered_by_token_id, q.answered_by_token_name,
       q.withdrawn_at, q.version, q.created_at, q.updated_at
FROM questions q
JOIN tickets t ON t.tenant_id = q.tenant_id AND t.id = q.ticket_id
LEFT JOIN users ab ON ab.id = q.asked_by
LEFT JOIN users ao ON ao.id = q.asked_of
LEFT JOIN users an ON an.id = q.answered_by
WHERE q.tenant_id = sqlc.arg(tenant_id) AND q.ticket_id = sqlc.arg(ticket_id) AND q.number = sqlc.arg(number)
  AND app_ticket_visible(t.project_id, t.confidential, t.assignee_id, t.reporter_id);

-- name: CanSeeTicket :one
-- Whether a person can see a ticket: its project (docs/adr/0034 D3) and,
-- while it is confidential, as administrator, assignee or reporter
-- (docs/adr/0065 D4). A question is asked only of such a person.
-- visibility: exempt (whether another person sees a ticket the caller reads)
SELECT EXISTS (
    SELECT 1
    FROM tickets t
    JOIN projects p ON p.tenant_id = t.tenant_id AND p.id = t.project_id
    JOIN memberships m ON m.tenant_id = t.tenant_id AND m.user_id = sqlc.arg(user_id)
    WHERE t.tenant_id = sqlc.arg(tenant_id) AND t.id = sqlc.arg(ticket_id)
      AND (NOT p.restricted OR m.role = 'admin'
           OR EXISTS (SELECT 1 FROM project_access a
                      WHERE a.tenant_id = t.tenant_id AND a.project_id = p.id AND a.user_id = m.user_id))
      AND (NOT t.confidential OR m.role = 'admin' OR t.assignee_id = m.user_id OR t.reporter_id = m.user_id)
) AS visible;
