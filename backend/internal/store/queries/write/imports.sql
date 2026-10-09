-- The writes of an import job (docs/adr/0051 D1–D3, D7). The policies of
-- migration 45 hold every row of import_jobs to the person who made it and
-- the tenant's administrators, and its deletion to the expiry job.

-- name: InsertImportJob :exec
-- A dry run: its report and the files it read, valid until expires_at.
INSERT INTO import_jobs (id, tenant_id, project_id, created_by, expires_at, report, source)
VALUES (sqlc.arg(id), sqlc.arg(tenant_id), sqlc.arg(project_id), sqlc.arg(created_by), sqlc.arg(expires_at),
        sqlc.arg(report), sqlc.arg(source));

-- name: LockImportJob :one
-- The job an execution writes, locked until the transaction ends: a second
-- execution waits for the first and then finds it executed (docs/adr/0051 D3).
-- Its report names the assignees the dry run resolved, which the execution
-- holds the files to.
SELECT status, expires_at, source, report
FROM import_jobs
WHERE tenant_id = sqlc.arg(tenant_id) AND project_id = sqlc.arg(project_id) AND id = sqlc.arg(id)
FOR UPDATE;

-- name: FinishImportJob :execrows
-- The execution's end: the report of what it created replaces the dry run's,
-- and the files it read go.
UPDATE import_jobs
SET status = 'executed', executed_by = sqlc.arg(executed_by), executed_at = sqlc.arg(executed_at),
    report = sqlc.arg(report), source = NULL, expires_at = NULL
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id) AND status = 'dry_run';

-- name: DeleteExpiredImportJobs :execrows
-- The job import-expiry: the dry runs whose twenty-four hours have passed, in
-- every tenant, with the files they hold (docs/adr/0051 D7).
DELETE FROM import_jobs
WHERE status = 'dry_run' AND expires_at <= sqlc.arg(now);

-- name: InsertImportedTicket :one
-- A ticket an import creates, with its number kept (docs/adr/0007 D6), the
-- state, dates, stages, horizon and block of its source, and the file and job
-- it came from (docs/adr/0051 D3). The importer is its reporter.
INSERT INTO tickets (
    tenant_id, project_id, number, type, title, body, state,
    blocked_from, block_kind, block_reason, block_ticket_id,
    severity, security, threat, urgency_derived, urgency_rule,
    urgency_override, urgency_override_by, urgency_override_at,
    effort, progress, progress_refinement, progress_review, parent_id,
    reporter_id, reporter_token_id, reporter_token_name, assignee_id, confidential, rank,
    opened_at, decided_at, done_at, done_from, done_by_hand,
    imported_from_file, imported_from_job
) VALUES (
    sqlc.arg(tenant_id), sqlc.arg(project_id), sqlc.arg(number), sqlc.arg(type), sqlc.arg(title), sqlc.arg(body),
    sqlc.arg(state), sqlc.narg(blocked_from), sqlc.narg(block_kind), sqlc.narg(block_reason), sqlc.narg(block_ticket_id),
    sqlc.arg(severity), sqlc.arg(security), sqlc.narg(threat), sqlc.arg(urgency_derived), sqlc.arg(urgency_rule),
    sqlc.narg(urgency_override)::urgency, sqlc.narg(urgency_override_by)::uuid,
    CASE WHEN sqlc.narg(urgency_override)::urgency IS NULL THEN NULL ELSE now() END,
    sqlc.arg(effort), sqlc.arg(progress), sqlc.arg(progress_refinement), sqlc.arg(progress_review), sqlc.narg(parent_id),
    sqlc.arg(reporter_id), sqlc.narg(reporter_token_id), sqlc.narg(reporter_token_name), sqlc.narg(assignee_id),
    sqlc.arg(confidential), sqlc.narg(rank)::text,
    coalesce(sqlc.narg(opened_at)::timestamptz, now()), sqlc.narg(decided_at), sqlc.narg(done_at),
    sqlc.narg(done_from), sqlc.arg(done_by_hand),
    sqlc.arg(imported_from_file), sqlc.arg(imported_from_job)
)
RETURNING id;

-- name: InsertImportedQuestion :one
-- An open question an import creates on its ticket with the number, the text
-- and the answer of its source (docs/adr/0011 D2, D4); the importer asked it,
-- and gave the answer it carries or withdrew it.
INSERT INTO questions (tenant_id, ticket_id, number, question, options, recommendation, answer, status,
                       asked_by, answered_by, answered_at, withdrawn_by, withdrawn_at)
VALUES (sqlc.arg(tenant_id), sqlc.arg(ticket_id), sqlc.arg(number), sqlc.arg(question), sqlc.arg(options),
        sqlc.arg(recommendation), sqlc.narg(answer), sqlc.arg(status)::question_status, sqlc.arg(asked_by),
        CASE WHEN sqlc.arg(status)::question_status = 'answered' THEN sqlc.arg(asked_by)::uuid END,
        CASE WHEN sqlc.arg(status)::question_status = 'answered' THEN now() END,
        CASE WHEN sqlc.arg(status)::question_status = 'withdrawn' THEN sqlc.arg(asked_by)::uuid END,
        CASE WHEN sqlc.arg(status)::question_status = 'withdrawn' THEN now() END)
RETURNING id;

-- name: AdvanceTicketCounter :exec
-- The project's sequence past the highest number an import brought
-- (docs/adr/0007 D6); it never moves back.
UPDATE ticket_counters
SET last_number = GREATEST(last_number, sqlc.arg(number)::integer)
WHERE tenant_id = sqlc.arg(tenant_id) AND project_id = sqlc.arg(project_id);
