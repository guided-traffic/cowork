-- name: NextQuestionNumber :one
-- The ticket's next question number; the caller holds the ticket's question
-- lock, so two askers never take the same.
SELECT (coalesce(max(number), 0) + 1)::integer AS number
FROM questions
WHERE tenant_id = sqlc.arg(tenant_id) AND ticket_id = sqlc.arg(ticket_id);

-- name: InsertQuestion :one
INSERT INTO questions (tenant_id, ticket_id, number, question, options, recommendation, asked_by, asked_by_agent, asked_of)
VALUES (sqlc.arg(tenant_id), sqlc.arg(ticket_id), sqlc.arg(number), sqlc.arg(question), sqlc.arg(options),
        sqlc.arg(recommendation), sqlc.arg(asked_by), sqlc.narg(asked_by_agent), sqlc.narg(asked_of))
RETURNING id;

-- name: UpdateQuestion :one
-- An edit of an open question, a compare-and-set on its version
-- (docs/adr/0050 D3).
UPDATE questions
SET question = sqlc.arg(question), options = sqlc.arg(options), recommendation = sqlc.arg(recommendation),
    asked_of = sqlc.narg(asked_of), version = version + 1, updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id) AND version = sqlc.arg(version) AND status = 'open'
RETURNING version;

-- name: AnswerQuestion :one
UPDATE questions
SET answer = sqlc.arg(answer), status = 'answered', answered_by = sqlc.arg(answered_by), answered_at = now(),
    recorded_by_agent = sqlc.arg(recorded_by_agent), version = version + 1, updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id) AND version = sqlc.arg(version) AND status <> 'withdrawn'
RETURNING version;

-- name: WithdrawQuestion :one
UPDATE questions
SET status = 'withdrawn', withdrawn_by = sqlc.arg(withdrawn_by), withdrawn_at = now(),
    version = version + 1, updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id) AND status = 'open'
RETURNING version;
