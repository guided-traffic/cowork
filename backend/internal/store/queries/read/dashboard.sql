-- The tiles of the tenant's dashboard (docs/adr/0018 D6), read in one
-- transaction by GET …/dashboard. Each counts only what the caller can see:
-- app_ticket_visible holds every ticket a query reads, so a restricted project
-- or a confidential ticket the caller cannot see counts nowhere and is named
-- nowhere (docs/adr/0034 D4, docs/adr/0065 D4); beside it the deletion filter
-- leaves a ticket in the bin out, as if it did not exist (docs/adr/0024 D1).
--
-- The counted projects: the keys of `projects`, or — while it is empty —
-- every project that is not archived; never one of `without_projects`. The
-- handler passes both arrays, empty rather than NULL. Open is every state but
-- done and dropped (docs/adr/0009 D1).

-- name: DashboardOpenByState :many
-- Tile 1: the open tickets per project and state.
-- visibility: app_ticket_visible and the deletion filter on every ticket counted.
SELECT p.key AS project_key, t.state, count(*)::bigint AS tickets
FROM tickets t
JOIN projects p ON p.tenant_id = t.tenant_id AND p.id = t.project_id
WHERE t.tenant_id = sqlc.arg(tenant_id)
  AND app_ticket_visible(t.project_id, t.confidential, t.assignee_id, t.reporter_id)
  AND t.deleted_at IS NULL
  AND t.state NOT IN ('done', 'dropped')
  AND (p.key = ANY (sqlc.arg(projects)::text[])
       OR (cardinality(sqlc.arg(projects)::text[]) = 0 AND p.archived_at IS NULL))
  AND NOT (p.key = ANY (sqlc.arg(without_projects)::text[]))
GROUP BY p.key, t.state
ORDER BY p.key, t.state;

-- name: DashboardOpenBySeverity :many
-- Tile 2: the open tickets per severity; a severity without one has no row.
-- visibility: app_ticket_visible and the deletion filter on every ticket counted.
SELECT t.severity, count(*)::bigint AS tickets
FROM tickets t
JOIN projects p ON p.tenant_id = t.tenant_id AND p.id = t.project_id
WHERE t.tenant_id = sqlc.arg(tenant_id)
  AND app_ticket_visible(t.project_id, t.confidential, t.assignee_id, t.reporter_id)
  AND t.deleted_at IS NULL
  AND t.state NOT IN ('done', 'dropped')
  AND (p.key = ANY (sqlc.arg(projects)::text[])
       OR (cardinality(sqlc.arg(projects)::text[]) = 0 AND p.archived_at IS NULL))
  AND NOT (p.key = ANY (sqlc.arg(without_projects)::text[]))
GROUP BY t.severity
ORDER BY t.severity;

-- name: DashboardSecurity :many
-- Tile 3: per class live and boundary that has an open ticket, the count and
-- the oldest by filing, the earlier id on a tie.
-- visibility: app_ticket_visible and the deletion filter on every ticket
-- counted and named.
SELECT DISTINCT ON (t.security) t.security, count(*) OVER (PARTITION BY t.security)::bigint AS tickets,
       p.key AS project_key, t.number, t.title, t.opened_at
FROM tickets t
JOIN projects p ON p.tenant_id = t.tenant_id AND p.id = t.project_id
WHERE t.tenant_id = sqlc.arg(tenant_id)
  AND app_ticket_visible(t.project_id, t.confidential, t.assignee_id, t.reporter_id)
  AND t.deleted_at IS NULL
  AND t.state NOT IN ('done', 'dropped')
  AND t.security IN ('live', 'boundary')
  AND (p.key = ANY (sqlc.arg(projects)::text[])
       OR (cardinality(sqlc.arg(projects)::text[]) = 0 AND p.archived_at IS NULL))
  AND NOT (p.key = ANY (sqlc.arg(without_projects)::text[]))
ORDER BY t.security, t.opened_at, t.id;

-- name: DashboardBlocked :many
-- Tile 4: the blocked tickets' count, on the one blocked longest. A ticket is
-- blocked since its latest act that moved it into blocked (docs/adr/0026), or
-- since its last update where no act records one; the earlier id on a tie.
-- No row while none is blocked.
-- visibility: app_ticket_visible and the deletion filter on every ticket
-- counted and named; the act is read only for a ticket the caller sees.
SELECT count(*) OVER ()::bigint AS tickets, p.key AS project_key, t.number, t.title, t.block_kind,
       coalesce((SELECT max(a.created_at) FROM audit_events a
                 WHERE a.tenant_id = t.tenant_id AND a.ticket_id = t.id
                   AND a.action = 'transitioned' AND a.after ->> 'state' = 'blocked'),
                t.updated_at)::timestamptz AS since
FROM tickets t
JOIN projects p ON p.tenant_id = t.tenant_id AND p.id = t.project_id
WHERE t.tenant_id = sqlc.arg(tenant_id)
  AND app_ticket_visible(t.project_id, t.confidential, t.assignee_id, t.reporter_id)
  AND t.deleted_at IS NULL
  AND t.state = 'blocked'
  AND (p.key = ANY (sqlc.arg(projects)::text[])
       OR (cardinality(sqlc.arg(projects)::text[]) = 0 AND p.archived_at IS NULL))
  AND NOT (p.key = ANY (sqlc.arg(without_projects)::text[]))
ORDER BY since, t.id
LIMIT 1;

-- name: DashboardAge :one
-- Tile 5: the open tickets per age bucket, by their filing against the
-- bounds the handler computes from its clock: opened after a bound is
-- younger than it.
-- visibility: app_ticket_visible and the deletion filter on every ticket counted.
SELECT count(*) FILTER (WHERE t.opened_at > sqlc.arg(cut_7)::timestamptz)::bigint AS under_7,
       count(*) FILTER (WHERE t.opened_at <= sqlc.arg(cut_7)::timestamptz
                          AND t.opened_at > sqlc.arg(cut_30)::timestamptz)::bigint AS under_30,
       count(*) FILTER (WHERE t.opened_at <= sqlc.arg(cut_30)::timestamptz
                          AND t.opened_at > sqlc.arg(cut_90)::timestamptz)::bigint AS under_90,
       count(*) FILTER (WHERE t.opened_at <= sqlc.arg(cut_90)::timestamptz
                          AND t.opened_at > sqlc.arg(cut_365)::timestamptz)::bigint AS under_365,
       count(*) FILTER (WHERE t.opened_at <= sqlc.arg(cut_365)::timestamptz)::bigint AS older
FROM tickets t
JOIN projects p ON p.tenant_id = t.tenant_id AND p.id = t.project_id
WHERE t.tenant_id = sqlc.arg(tenant_id)
  AND app_ticket_visible(t.project_id, t.confidential, t.assignee_id, t.reporter_id)
  AND t.deleted_at IS NULL
  AND t.state NOT IN ('done', 'dropped')
  AND (p.key = ANY (sqlc.arg(projects)::text[])
       OR (cardinality(sqlc.arg(projects)::text[]) = 0 AND p.archived_at IS NULL))
  AND NOT (p.key = ANY (sqlc.arg(without_projects)::text[]));

-- name: DashboardThroughput :many
-- Tile 6: the tickets done now per ISO week of their done_at in UTC, the
-- Monday naming the week, inside [since, until); a week without one has no
-- row (docs/adr/0019 D2).
-- visibility: app_ticket_visible and the deletion filter on every ticket counted.
SELECT date_trunc('week', t.done_at AT TIME ZONE 'UTC')::date AS week, count(*)::bigint AS tickets
FROM tickets t
JOIN projects p ON p.tenant_id = t.tenant_id AND p.id = t.project_id
WHERE t.tenant_id = sqlc.arg(tenant_id)
  AND app_ticket_visible(t.project_id, t.confidential, t.assignee_id, t.reporter_id)
  AND t.deleted_at IS NULL
  AND t.state = 'done'
  AND t.done_at >= sqlc.arg(since)::timestamptz AND t.done_at < sqlc.arg(until)::timestamptz
  AND (p.key = ANY (sqlc.arg(projects)::text[])
       OR (cardinality(sqlc.arg(projects)::text[]) = 0 AND p.archived_at IS NULL))
  AND NOT (p.key = ANY (sqlc.arg(without_projects)::text[]))
GROUP BY 1
ORDER BY 1;

-- name: DashboardLeadTime :one
-- Tile 7: the tickets done now whose done_at lies in [since, until), and the
-- median of their seconds from filing to done; 0 while there is none.
-- visibility: app_ticket_visible and the deletion filter on every ticket measured.
SELECT count(*)::bigint AS tickets,
       coalesce(percentile_cont(0.5) WITHIN GROUP (ORDER BY extract(epoch FROM t.done_at - t.opened_at)),
                0)::float8 AS median_seconds
FROM tickets t
JOIN projects p ON p.tenant_id = t.tenant_id AND p.id = t.project_id
WHERE t.tenant_id = sqlc.arg(tenant_id)
  AND app_ticket_visible(t.project_id, t.confidential, t.assignee_id, t.reporter_id)
  AND t.deleted_at IS NULL
  AND t.state = 'done'
  AND t.done_at >= sqlc.arg(since)::timestamptz AND t.done_at < sqlc.arg(until)::timestamptz
  AND (p.key = ANY (sqlc.arg(projects)::text[])
       OR (cardinality(sqlc.arg(projects)::text[]) = 0 AND p.archived_at IS NULL))
  AND NOT (p.key = ANY (sqlc.arg(without_projects)::text[]));

-- name: DashboardDecisions :many
-- Tile 8: the open questions' count, on the one asked first, the earlier id
-- on a tie (docs/adr/0011 D3); no row while none is open.
-- visibility: app_ticket_visible and the deletion filter on the ticket of
-- every question counted.
SELECT count(*) OVER ()::bigint AS questions, p.key AS project_key, t.number AS ticket_number,
       t.title AS ticket_title, q.number AS question_number, q.question, q.created_at
FROM questions q
JOIN tickets t ON t.tenant_id = q.tenant_id AND t.id = q.ticket_id
JOIN projects p ON p.tenant_id = t.tenant_id AND p.id = t.project_id
WHERE q.tenant_id = sqlc.arg(tenant_id) AND q.status = 'open'
  AND app_ticket_visible(t.project_id, t.confidential, t.assignee_id, t.reporter_id)
  AND t.deleted_at IS NULL
  AND (p.key = ANY (sqlc.arg(projects)::text[])
       OR (cardinality(sqlc.arg(projects)::text[]) = 0 AND p.archived_at IS NULL))
  AND NOT (p.key = ANY (sqlc.arg(without_projects)::text[]))
ORDER BY q.created_at, q.id
LIMIT 1;

-- name: DashboardTime :many
-- Tile 9: the minutes booked on a day of [from_day, to_day] per project,
-- voided entries left out (docs/adr/0017 D7, D10).
-- visibility: app_ticket_visible and the deletion filter on every entry's
-- ticket, and app_time_visible on the entry (docs/adr/0034 D5).
SELECT p.key AS project_key, p.name AS project_name, sum(e.minutes)::bigint AS minutes
FROM time_entries e
JOIN tickets t ON t.tenant_id = e.tenant_id AND t.id = e.ticket_id
JOIN projects p ON p.tenant_id = t.tenant_id AND p.id = t.project_id
WHERE e.tenant_id = sqlc.arg(tenant_id) AND e.voided_at IS NULL
  AND app_ticket_visible(t.project_id, t.confidential, t.assignee_id, t.reporter_id)
  AND t.deleted_at IS NULL
  AND app_time_visible(e.person_id)
  AND e.day >= sqlc.arg(from_day)::date AND e.day <= sqlc.arg(to_day)::date
  AND (p.key = ANY (sqlc.arg(projects)::text[])
       OR (cardinality(sqlc.arg(projects)::text[]) = 0 AND p.archived_at IS NULL))
  AND NOT (p.key = ANY (sqlc.arg(without_projects)::text[]))
GROUP BY p.key, p.name
ORDER BY p.key;

-- name: DashboardRecent :many
-- Beside the tiles: the open tickets updated last, the later id on a tie.
-- visibility: app_ticket_visible and the deletion filter on every ticket listed.
SELECT p.key AS project_key, t.number, t.type, t.title, t.state, t.updated_at
FROM tickets t
JOIN projects p ON p.tenant_id = t.tenant_id AND p.id = t.project_id
WHERE t.tenant_id = sqlc.arg(tenant_id)
  AND app_ticket_visible(t.project_id, t.confidential, t.assignee_id, t.reporter_id)
  AND t.deleted_at IS NULL
  AND t.state NOT IN ('done', 'dropped')
  AND (p.key = ANY (sqlc.arg(projects)::text[])
       OR (cardinality(sqlc.arg(projects)::text[]) = 0 AND p.archived_at IS NULL))
  AND NOT (p.key = ANY (sqlc.arg(without_projects)::text[]))
ORDER BY t.updated_at DESC, t.id DESC
LIMIT sqlc.arg(page_size);
