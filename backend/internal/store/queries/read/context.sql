-- What the context of a ticket shows beside the canonical document
-- (docs/adr/0044 D2). The comments, the attachments and the activity are read
-- with the queries of their own lists; the links and the prerequisite tree
-- need the facts a reader of one ticket wants of the others.

-- name: ContextLinks :many
-- The ticket's links in both directions with each other end's state and
-- assignee; a link whose other end the caller cannot see is absent
-- (docs/adr/0065 D4).
SELECT l.id, l.type, (l.source_id = sqlc.arg(ticket_id)::uuid)::boolean AS outgoing,
       op.key AS other_project_key, o.number AS other_number, o.title AS other_title, o.state AS other_state,
       au.display_name AS other_assignee_name
FROM ticket_links l
JOIN tickets o ON o.tenant_id = l.tenant_id
     AND o.id = CASE WHEN l.source_id = sqlc.arg(ticket_id)::uuid THEN l.target_id ELSE l.source_id END
JOIN projects op ON op.tenant_id = o.tenant_id AND op.id = o.project_id
LEFT JOIN users au ON au.id = o.assignee_id
WHERE l.tenant_id = sqlc.arg(tenant_id)
  AND (l.source_id = sqlc.arg(ticket_id)::uuid OR l.target_id = sqlc.arg(ticket_id)::uuid)
  AND app_ticket_visible(o.project_id, o.confidential, o.assignee_id, o.reporter_id)
ORDER BY l.type, outgoing DESC, op.key, o.number
LIMIT sqlc.arg(page_size);

-- name: ContextPrerequisites :many
-- The tree of what must be done before the ticket can close: the tickets
-- that block it, and what blocks those, to a depth of eight
-- (docs/adr/0012 D6). The walk stops at a ticket the caller cannot see, so
-- nothing behind it shows either; the path orders the tree depth first.
WITH RECURSIVE tree AS (
    SELECT s.id, 1 AS depth, ARRAY[l.target_id, s.id] AS path
    FROM ticket_links l
    JOIN tickets s ON s.tenant_id = l.tenant_id AND s.id = l.source_id
    WHERE l.tenant_id = sqlc.arg(tenant_id) AND l.target_id = sqlc.arg(ticket_id) AND l.type = 'blocks'
      AND app_ticket_visible(s.project_id, s.confidential, s.assignee_id, s.reporter_id)
    UNION ALL
    SELECT s.id, tree.depth + 1, tree.path || s.id
    FROM tree
    JOIN ticket_links l ON l.tenant_id = sqlc.arg(tenant_id) AND l.target_id = tree.id AND l.type = 'blocks'
    JOIN tickets s ON s.tenant_id = l.tenant_id AND s.id = l.source_id
    WHERE tree.depth < 8 AND NOT s.id = ANY (tree.path)
      AND app_ticket_visible(s.project_id, s.confidential, s.assignee_id, s.reporter_id)
)
SELECT tree.depth::integer AS depth, sp.key AS project_key, s.number, s.title, s.state,
       au.display_name AS assignee_name, COALESCE(s.progress_derived, s.progress)::integer AS progress
FROM tree
JOIN tickets s ON s.tenant_id = sqlc.arg(tenant_id) AND s.id = tree.id
JOIN projects sp ON sp.tenant_id = s.tenant_id AND sp.id = s.project_id
LEFT JOIN users au ON au.id = s.assignee_id
WHERE app_ticket_visible(s.project_id, s.confidential, s.assignee_id, s.reporter_id)
ORDER BY tree.path
LIMIT sqlc.arg(page_size);
