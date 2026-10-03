-- name: NextTicketNumber :one
-- The project's next number (docs/adr/0022 D2): the counter row's lock orders
-- concurrent filings; a filing that rolls back hands its number out again,
-- which is right, because that ticket never existed.
UPDATE ticket_counters
SET last_number = last_number + 1
WHERE tenant_id = sqlc.arg(tenant_id) AND project_id = sqlc.arg(project_id)
RETURNING last_number;

-- name: InsertTicket :one
INSERT INTO tickets (
    tenant_id, project_id, number, type, title, body, severity, security, threat,
    urgency_derived, urgency_rule, effort, parent_id, reporter_id, assignee_id, confidential
) VALUES (
    sqlc.arg(tenant_id), sqlc.arg(project_id), sqlc.arg(number), sqlc.arg(type), sqlc.arg(title),
    sqlc.arg(body), sqlc.arg(severity), sqlc.arg(security), sqlc.narg(threat), sqlc.arg(urgency_derived),
    sqlc.arg(urgency_rule), sqlc.arg(effort), sqlc.narg(parent_id), sqlc.arg(reporter_id),
    sqlc.narg(assignee_id), sqlc.arg(confidential)
)
RETURNING id;

-- name: UpdateTicketFields :one
-- The fields of PATCH, a compare-and-set on the version (docs/adr/0050 D1).
UPDATE tickets
SET type = sqlc.arg(type), title = sqlc.arg(title), severity = sqlc.arg(severity),
    security = sqlc.arg(security), threat = sqlc.narg(threat), effort = sqlc.arg(effort),
    parent_id = sqlc.narg(parent_id), assignee_id = sqlc.narg(assignee_id),
    progress = sqlc.arg(progress), confidential = sqlc.arg(confidential),
    version = version + 1, updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id) AND version = sqlc.arg(version)
RETURNING version;

-- name: UpdateTicketBody :one
UPDATE tickets
SET body = sqlc.arg(body), version = version + 1, updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id) AND version = sqlc.arg(version)
RETURNING version;

-- name: SetUrgencyOverride :one
-- A reasoned override, or none (docs/adr/0010 D3).
UPDATE tickets
SET urgency_override = sqlc.narg(urgency_override), urgency_override_reason = sqlc.narg(reason),
    urgency_override_by = sqlc.narg(override_by),
    urgency_override_at = CASE WHEN sqlc.narg(urgency_override)::urgency IS NULL THEN NULL ELSE now() END,
    version = version + 1, updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id) AND version = sqlc.arg(version)
RETURNING version;

-- name: SetConfidential :one
UPDATE tickets
SET confidential = sqlc.arg(confidential), version = version + 1, updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id) AND version = sqlc.arg(version)
RETURNING version;

-- name: GetWrittenTicket :one
-- The ticket as a write in this transaction left it, for the answer to that
-- write. The predicate admitted the writer on the read before the write; the
-- write itself can take the ticket out of the writer's sight (a confidential
-- ticket reassigned away from its assignee), and the answer then shows what
-- the writer sent and read a moment ago. Linked tickets keep their predicate.
-- visibility: exempt (the writer's reread of the row it wrote)
SELECT t.id, t.project_id, p.key AS project_key, t.number, t.type, t.title, t.body, t.state,
       t.blocked_from, t.block_kind, t.block_reason, t.block_ticket_id, t.block_external_ref,
       bp.key AS block_project_key, bt.number AS block_number,
       t.severity, t.security, t.threat, t.urgency_derived, t.urgency_rule, t.urgency_override,
       t.urgency_override_reason, t.urgency_override_by, t.urgency_override_at, t.effort, t.progress, t.progress_derived,
       t.parent_id, pt.number AS parent_number,
       t.reporter_id, ru.username AS reporter_username, ru.display_name AS reporter_name,
       t.assignee_id, au.username AS assignee_username, au.display_name AS assignee_name,
       t.confidential, t.opened_at, t.decided_at, t.done_at, t.version, t.created_at, t.updated_at
FROM tickets t
JOIN projects p ON p.tenant_id = t.tenant_id AND p.id = t.project_id
LEFT JOIN users ru ON ru.id = t.reporter_id
LEFT JOIN users au ON au.id = t.assignee_id
LEFT JOIN tickets pt ON pt.tenant_id = t.tenant_id AND pt.id = t.parent_id
     AND app_ticket_visible(pt.project_id, pt.confidential, pt.assignee_id, pt.reporter_id)
LEFT JOIN tickets bt ON bt.tenant_id = t.tenant_id AND bt.id = t.block_ticket_id
     AND app_ticket_visible(bt.project_id, bt.confidential, bt.assignee_id, bt.reporter_id)
LEFT JOIN projects bp ON bp.tenant_id = bt.tenant_id AND bp.id = bt.project_id
WHERE t.tenant_id = sqlc.arg(tenant_id) AND t.id = sqlc.arg(id);

-- name: TransitionTicket :one
-- A move between states, a compare-and-set on the state the request names
-- (docs/adr/0045 D2). done sets progress to 100 (docs/adr/0017 D5); the
-- dates are the acts' (docs/adr/0009 D6): decided_at the last time the ticket
-- reached decided, done_at while it is done.
UPDATE tickets
SET state = sqlc.arg(to_state),
    blocked_from = sqlc.narg(blocked_from), block_kind = sqlc.narg(block_kind),
    block_reason = sqlc.narg(block_reason), block_ticket_id = sqlc.narg(block_ticket_id),
    block_external_ref = sqlc.narg(block_external_ref),
    progress = CASE WHEN sqlc.arg(to_state)::ticket_state = 'done' THEN 100 ELSE progress END,
    decided_at = CASE WHEN sqlc.arg(to_state)::ticket_state = 'decided' THEN now() ELSE decided_at END,
    done_at = CASE WHEN sqlc.arg(to_state)::ticket_state = 'done' THEN now()
                   WHEN sqlc.arg(to_state)::ticket_state = 'filed' THEN NULL
                   ELSE done_at END,
    version = version + 1, updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id) AND state = sqlc.arg(from_state)
RETURNING version;

-- name: RefreshDerivedProgress :one
-- The ticket's derived progress after a change of its children. It leaves
-- the version alone (docs/adr/0050 D1); when the last child has left, the
-- ticket's own value starts at the last derived one (docs/adr/0017 D3). No
-- row when nothing changed; else the parent, whose progress reads this one.
WITH d AS (SELECT ticket_derived_progress(sqlc.arg(tenant_id), sqlc.arg(id)) AS v)
UPDATE tickets t
SET progress_derived = d.v,
    progress = CASE WHEN d.v IS NULL THEN coalesce(t.progress_derived, t.progress) ELSE t.progress END,
    updated_at = now()
FROM d
WHERE t.tenant_id = sqlc.arg(tenant_id) AND t.id = sqlc.arg(id) AND t.progress_derived IS DISTINCT FROM d.v
RETURNING t.parent_id;

-- name: TicketFacts :one
-- What a published act carries of its ticket: the project, the version and
-- the confidential rule's inputs (docs/adr/0054 D2, D3).
-- visibility: exempt (the publication of a committed act; subscribers filter)
SELECT project_id, version, confidential, assignee_id, reporter_id
FROM tickets
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id);
