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
  AND t.deleted_at IS NULL AND app_ticket_visible(t.project_id, t.confidential, t.assignee_id, t.reporter_id)
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
  AND t.deleted_at IS NULL AND app_ticket_visible(t.project_id, t.confidential, t.assignee_id, t.reporter_id);

-- name: CanSeeTicket :one
-- Whether a person can see a ticket: its project (docs/adr/0034 D3) and,
-- while it is confidential, as administrator, assignee or reporter
-- (docs/adr/0065 D4). A question is asked only of such a person.
SELECT person_sees_ticket(sqlc.arg(tenant_id), sqlc.arg(ticket_id), sqlc.arg(user_id)) AS visible;

-- name: ListOpenDecisions :many
-- The open decisions of a person in the tenant (docs/adr/0018 D3): the open
-- questions asked of them and those open in the tenant, on tickets they see.
-- Ordered by the score of the ticket (docs/adr/0014 D5) — its stored key,
-- whose order is the score's at every moment; a done or dropped ticket, which
-- has no score, after the scored ones (D3) — then the ticket's id and the
-- question's number. The cursor resumes after a position of that order: the
-- ids are unique across tenants, so the order of every tenant's part is one
-- order across them.
SELECT q.id, q.number, q.question, q.options, q.recommendation, q.answer, q.status,
       q.asked_by, ab.username AS asked_by_username, ab.display_name AS asked_by_name, q.asked_by_agent,
       q.asked_by_token_id, q.asked_by_token_name,
       q.asked_of, ao.username AS asked_of_username, ao.display_name AS asked_of_name,
       q.answered_by, an.username AS answered_by_username, an.display_name AS answered_by_name,
       q.answered_at, q.recorded_by_agent, q.answered_by_token_id, q.answered_by_token_name,
       q.withdrawn_at, q.version, q.created_at, q.updated_at,
       p.key AS project_key, t.number AS ticket_number, t.title AS ticket_title, t.state AS ticket_state,
       q.ticket_id,
       (CASE WHEN t.state IN ('done', 'dropped') THEN '-Infinity'::double precision ELSE t.score_key END)::double precision
           AS ticket_score_key
FROM questions q
JOIN tickets t ON t.tenant_id = q.tenant_id AND t.id = q.ticket_id
JOIN projects p ON p.tenant_id = t.tenant_id AND p.id = t.project_id
LEFT JOIN users ab ON ab.id = q.asked_by
LEFT JOIN users ao ON ao.id = q.asked_of
LEFT JOIN users an ON an.id = q.answered_by
WHERE q.tenant_id = sqlc.arg(tenant_id) AND q.status = 'open'
  AND (q.asked_of = sqlc.arg(user_id)::uuid OR q.asked_of IS NULL)
  AND t.deleted_at IS NULL AND app_ticket_visible(t.project_id, t.confidential, t.assignee_id, t.reporter_id)
  AND (NOT sqlc.arg(has_after)::boolean
       OR (CASE WHEN t.state IN ('done', 'dropped') THEN '-Infinity'::double precision ELSE t.score_key END)
          < sqlc.arg(after_key)::double precision
       OR ((CASE WHEN t.state IN ('done', 'dropped') THEN '-Infinity'::double precision ELSE t.score_key END)
           = sqlc.arg(after_key)::double precision
           AND (t.id, q.number) > (sqlc.arg(after_ticket)::uuid, sqlc.arg(after_question)::integer)))
ORDER BY (CASE WHEN t.state IN ('done', 'dropped') THEN '-Infinity'::double precision ELSE t.score_key END) DESC,
         t.id, q.number
LIMIT sqlc.arg(page_size);
