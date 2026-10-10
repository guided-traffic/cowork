-- Time entries are visible through their ticket's predicate and the time
-- visibility of docs/adr/0034 D5 (app_time_visible).

-- name: ListTicketTime :many
SELECT e.id, e.person_id, pu.username AS person_username, pu.display_name AS person_name,
       e.author_id, au.username AS author_username, au.display_name AS author_name,
       e.minutes, e.day, e.note, e.voided_at, e.token_id, e.token_name,
       EXISTS (SELECT 1 FROM time_entry_revisions r WHERE r.tenant_id = e.tenant_id AND r.entry_id = e.id) AS edited,
       e.version, e.created_at, e.updated_at
FROM time_entries e
JOIN tickets t ON t.tenant_id = e.tenant_id AND t.id = e.ticket_id
LEFT JOIN users pu ON pu.id = e.person_id
LEFT JOIN users au ON au.id = e.author_id
WHERE e.tenant_id = sqlc.arg(tenant_id) AND e.ticket_id = sqlc.arg(ticket_id)
  AND t.deleted_at IS NULL AND app_ticket_visible(t.project_id, t.confidential, t.assignee_id, t.reporter_id)
  AND app_time_visible(e.person_id)
  AND (sqlc.narg(after)::uuid IS NULL OR e.id > sqlc.narg(after)::uuid)
ORDER BY e.id
LIMIT sqlc.arg(page_size);

-- name: SumTicketTime :one
-- The minutes of the visible entries a ticket carries, voided ones excluded
-- (docs/adr/0017 D7).
SELECT coalesce(sum(e.minutes), 0)::bigint AS minutes
FROM time_entries e
JOIN tickets t ON t.tenant_id = e.tenant_id AND t.id = e.ticket_id
WHERE e.tenant_id = sqlc.arg(tenant_id) AND e.ticket_id = sqlc.arg(ticket_id) AND e.voided_at IS NULL
  AND t.deleted_at IS NULL AND app_ticket_visible(t.project_id, t.confidential, t.assignee_id, t.reporter_id)
  AND app_time_visible(e.person_id);

-- name: GetTimeEntry :one
SELECT e.id, e.person_id, pu.username AS person_username, pu.display_name AS person_name,
       e.author_id, au.username AS author_username, au.display_name AS author_name,
       e.minutes, e.day, e.note, e.voided_at, e.token_id, e.token_name,
       EXISTS (SELECT 1 FROM time_entry_revisions r WHERE r.tenant_id = e.tenant_id AND r.entry_id = e.id) AS edited,
       e.version, e.created_at, e.updated_at
FROM time_entries e
JOIN tickets t ON t.tenant_id = e.tenant_id AND t.id = e.ticket_id
LEFT JOIN users pu ON pu.id = e.person_id
LEFT JOIN users au ON au.id = e.author_id
WHERE e.tenant_id = sqlc.arg(tenant_id) AND e.ticket_id = sqlc.arg(ticket_id) AND e.id = sqlc.arg(id)
  AND t.deleted_at IS NULL AND app_ticket_visible(t.project_id, t.confidential, t.assignee_id, t.reporter_id)
  AND app_time_visible(e.person_id);

-- name: ListTimeEntryRevisions :many
SELECT r.id, r.minutes, r.day, r.note, r.edited_by, u.username AS edited_by_username,
       u.display_name AS edited_by_name, r.token_id, r.token_name, r.created_at
FROM time_entry_revisions r
JOIN time_entries e ON e.tenant_id = r.tenant_id AND e.id = r.entry_id
JOIN tickets t ON t.tenant_id = e.tenant_id AND t.id = e.ticket_id
LEFT JOIN users u ON u.id = r.edited_by
WHERE r.tenant_id = sqlc.arg(tenant_id) AND r.entry_id = sqlc.arg(entry_id)
  AND t.deleted_at IS NULL AND app_ticket_visible(t.project_id, t.confidential, t.assignee_id, t.reporter_id)
  AND app_time_visible(e.person_id)
  AND (sqlc.narg(after)::uuid IS NULL OR r.id > sqlc.narg(after)::uuid)
ORDER BY r.id
LIMIT sqlc.arg(page_size);

-- name: ListTenantTime :many
-- The tenant's visible entries over a period, newest first: by id after a
-- cursor, or a numbered page by offset (docs/adr/0048 D1, D2).
SELECT e.id, e.person_id, pu.username AS person_username, pu.display_name AS person_name,
       e.author_id, au.username AS author_username, au.display_name AS author_name,
       e.minutes, e.day, e.note, e.voided_at, e.token_id, e.token_name,
       EXISTS (SELECT 1 FROM time_entry_revisions r WHERE r.tenant_id = e.tenant_id AND r.entry_id = e.id) AS edited,
       e.version, e.created_at, e.updated_at, p.key AS project_key, t.number AS ticket_number, t.title AS ticket_title
FROM time_entries e
JOIN tickets t ON t.tenant_id = e.tenant_id AND t.id = e.ticket_id
JOIN projects p ON p.tenant_id = t.tenant_id AND p.id = t.project_id
LEFT JOIN users pu ON pu.id = e.person_id
LEFT JOIN users au ON au.id = e.author_id
WHERE e.tenant_id = sqlc.arg(tenant_id)
  AND t.deleted_at IS NULL AND app_ticket_visible(t.project_id, t.confidential, t.assignee_id, t.reporter_id)
  AND app_time_visible(e.person_id)
  AND (sqlc.narg(from_day)::date IS NULL OR e.day >= sqlc.narg(from_day)::date)
  AND (sqlc.narg(to_day)::date IS NULL OR e.day <= sqlc.narg(to_day)::date)
  AND (sqlc.narg(project_key)::text IS NULL OR p.key = sqlc.narg(project_key)::text)
  AND (sqlc.narg(ticket_number)::integer IS NULL OR t.number = sqlc.narg(ticket_number)::integer)
  AND (sqlc.narg(person_id)::uuid IS NULL OR e.person_id = sqlc.narg(person_id)::uuid)
  AND (sqlc.arg(include_voided)::boolean OR e.voided_at IS NULL)
  AND (sqlc.narg(before)::uuid IS NULL OR e.id < sqlc.narg(before)::uuid)
ORDER BY e.id DESC
LIMIT sqlc.arg(page_size) OFFSET sqlc.arg(page_offset);

-- name: CountTenantTime :one
SELECT count(*)::bigint AS entries
FROM time_entries e
JOIN tickets t ON t.tenant_id = e.tenant_id AND t.id = e.ticket_id
JOIN projects p ON p.tenant_id = t.tenant_id AND p.id = t.project_id
WHERE e.tenant_id = sqlc.arg(tenant_id)
  AND t.deleted_at IS NULL AND app_ticket_visible(t.project_id, t.confidential, t.assignee_id, t.reporter_id)
  AND app_time_visible(e.person_id)
  AND (sqlc.narg(from_day)::date IS NULL OR e.day >= sqlc.narg(from_day)::date)
  AND (sqlc.narg(to_day)::date IS NULL OR e.day <= sqlc.narg(to_day)::date)
  AND (sqlc.narg(project_key)::text IS NULL OR p.key = sqlc.narg(project_key)::text)
  AND (sqlc.narg(ticket_number)::integer IS NULL OR t.number = sqlc.narg(ticket_number)::integer)
  AND (sqlc.narg(person_id)::uuid IS NULL OR e.person_id = sqlc.narg(person_id)::uuid)
  AND (sqlc.arg(include_voided)::boolean OR e.voided_at IS NULL);

-- name: TimeReport :many
-- Minutes summed per ticket, project, person or for the team over a
-- period (docs/adr/0017 D10); voided entries never count. The team's one row
-- is keyed by the name it was asked by: team, or tenant, its name before
-- (docs/adr/0005 D1, docs/adr/0046 D7).
SELECT CASE sqlc.arg(group_by)::text
           WHEN 'ticket' THEN p.key || '-' || t.number::text
           WHEN 'project' THEN p.key
           WHEN 'person' THEN e.person_id::text
           WHEN 'team' THEN 'team'
           ELSE 'tenant' END::text AS group_key,
       max(CASE sqlc.arg(group_by)::text
               WHEN 'ticket' THEN t.title
               WHEN 'project' THEN p.name
               WHEN 'person' THEN coalesce(u.display_name, '')
               ELSE '' END)::text AS label,
       sum(e.minutes)::bigint AS minutes
FROM time_entries e
JOIN tickets t ON t.tenant_id = e.tenant_id AND t.id = e.ticket_id
JOIN projects p ON p.tenant_id = t.tenant_id AND p.id = t.project_id
LEFT JOIN users u ON u.id = e.person_id
WHERE e.tenant_id = sqlc.arg(tenant_id) AND e.voided_at IS NULL
  AND t.deleted_at IS NULL AND app_ticket_visible(t.project_id, t.confidential, t.assignee_id, t.reporter_id)
  AND app_time_visible(e.person_id)
  AND (sqlc.narg(from_day)::date IS NULL OR e.day >= sqlc.narg(from_day)::date)
  AND (sqlc.narg(to_day)::date IS NULL OR e.day <= sqlc.narg(to_day)::date)
  AND (sqlc.narg(project_key)::text IS NULL OR p.key = sqlc.narg(project_key)::text)
  AND (sqlc.narg(person_id)::uuid IS NULL OR e.person_id = sqlc.narg(person_id)::uuid)
GROUP BY 1
ORDER BY 1;
