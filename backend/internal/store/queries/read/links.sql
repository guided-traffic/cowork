-- name: ListTicketLinks :many
-- A ticket's links in both directions, read from the ticket's side; a link
-- whose other end the caller cannot see is absent (docs/adr/0065 D4).
SELECT l.id, l.type, (l.source_id = sqlc.arg(ticket_id)::uuid)::boolean AS outgoing,
       o.id AS other_id, op.key AS other_project_key, o.number AS other_number, o.title AS other_title,
       o.state AS other_state, l.created_by, u.username AS created_by_username,
       u.display_name AS created_by_name, l.created_at
FROM ticket_links l
JOIN tickets o ON o.tenant_id = l.tenant_id
     AND o.id = CASE WHEN l.source_id = sqlc.arg(ticket_id)::uuid THEN l.target_id ELSE l.source_id END
JOIN projects op ON op.tenant_id = o.tenant_id AND op.id = o.project_id
LEFT JOIN users u ON u.id = l.created_by
WHERE l.tenant_id = sqlc.arg(tenant_id)
  AND (l.source_id = sqlc.arg(ticket_id)::uuid OR l.target_id = sqlc.arg(ticket_id)::uuid)
  AND o.deleted_at IS NULL AND app_ticket_visible(o.project_id, o.confidential, o.assignee_id, o.reporter_id)
  AND (sqlc.narg(after)::uuid IS NULL OR l.id > sqlc.narg(after)::uuid)
ORDER BY l.id
LIMIT sqlc.arg(page_size);

-- name: GetLink :one
-- A link between two tickets the caller has read through the predicate.
SELECT l.id, l.created_by, u.username AS created_by_username, u.display_name AS created_by_name, l.created_at
FROM ticket_links l
LEFT JOIN users u ON u.id = l.created_by
WHERE l.tenant_id = sqlc.arg(tenant_id) AND l.type = sqlc.arg(type)
  AND l.source_id = sqlc.arg(source_id) AND l.target_id = sqlc.arg(target_id);

-- name: ListPrerequisites :many
-- The prerequisite tree of a ticket (docs/adr/0012 D6): the tickets that
-- block it, what blocks those, and so on to max_depth, depth first, siblings
-- by id — in the order they were filed. The walk keeps each link once per
-- depth (UNION), never each path, so a dense graph costs its links times the
-- depth and not the number of its paths. It never passes a ticket the caller
-- cannot see: that ticket and what lies only behind it are absent
-- (docs/adr/0065 D5). A ticket reached under several others stands in full
-- under the first of them nearest the root (first) and under each other one as
-- a repeated leaf. open_count counts the open tickets of the whole tree, each
-- once, before the page is cut.
WITH RECURSIVE reach (id, via, depth) AS (
    SELECT l.source_id, l.target_id, 1
    FROM ticket_links l
    JOIN tickets s ON s.tenant_id = l.tenant_id AND s.id = l.source_id
    WHERE l.tenant_id = sqlc.arg(tenant_id) AND l.target_id = sqlc.arg(ticket_id)::uuid AND l.type = 'blocks'
      AND s.deleted_at IS NULL AND app_ticket_visible(s.project_id, s.confidential, s.assignee_id, s.reporter_id)
    UNION
    SELECT l.source_id, l.target_id, reach.depth + 1
    FROM reach
    JOIN ticket_links l ON l.tenant_id = sqlc.arg(tenant_id) AND l.target_id = reach.id AND l.type = 'blocks'
    JOIN tickets s ON s.tenant_id = l.tenant_id AND s.id = l.source_id
    WHERE reach.depth < sqlc.arg(max_depth)::integer AND s.id <> sqlc.arg(ticket_id)::uuid
      AND s.deleted_at IS NULL AND app_ticket_visible(s.project_id, s.confidential, s.assignee_id, s.reporter_id)
), first AS (
    SELECT DISTINCT ON (reach.id) reach.id, reach.via, reach.depth FROM reach ORDER BY reach.id, reach.depth, reach.via
), tree (id, depth, path) AS (
    SELECT first.id, first.depth, ARRAY[first.id] FROM first WHERE first.depth = 1
    UNION ALL
    SELECT first.id, first.depth, tree.path || first.id FROM tree JOIN first ON first.via = tree.id
), nodes AS (
    SELECT tree.id, tree.depth, tree.path, false AS repeated FROM tree
    UNION
    SELECT reach.id, tree.depth + 1, tree.path || reach.id, true
    FROM reach
    JOIN first ON first.id = reach.id AND first.via <> reach.via
    JOIN tree ON tree.id = reach.via
), shown AS (
    SELECT nodes.depth, nodes.path, nodes.repeated, sp.key AS project_key, s.number, s.title, s.state, s.blocked_from,
           s.assignee_id, au.username AS assignee_username, au.display_name AS assignee_name,
           s.progress, s.progress_derived, s.progress_refinement, s.progress_refinement_derived,
           s.progress_review, s.progress_review_derived,
           count(*) FILTER (WHERE NOT nodes.repeated AND s.state NOT IN ('done', 'dropped')) OVER () AS open_count
    FROM nodes
    JOIN tickets s ON s.tenant_id = sqlc.arg(tenant_id) AND s.id = nodes.id
    JOIN projects sp ON sp.tenant_id = s.tenant_id AND sp.id = s.project_id
    LEFT JOIN users au ON au.id = s.assignee_id
    WHERE s.deleted_at IS NULL AND app_ticket_visible(s.project_id, s.confidential, s.assignee_id, s.reporter_id)
)
SELECT shown.depth::integer AS depth, shown.path::uuid[] AS path, shown.repeated::boolean AS repeated,
       shown.project_key, shown.number, shown.title, shown.state, shown.blocked_from,
       shown.assignee_id, shown.assignee_username, shown.assignee_name,
       shown.progress, shown.progress_derived, shown.progress_refinement, shown.progress_refinement_derived,
       shown.progress_review, shown.progress_review_derived, shown.open_count::integer AS open_count
FROM shown
WHERE sqlc.narg(after)::uuid[] IS NULL OR shown.path > sqlc.narg(after)::uuid[]
ORDER BY shown.path
LIMIT sqlc.arg(page_size);

-- name: ListDependents :many
-- The prerequisite tree read upward (docs/adr/0012 D6): the tickets the
-- ticket blocks, what those block, and so on — ListPrerequisites with the two
-- ends of every link swapped, under the same rules.
WITH RECURSIVE reach (id, via, depth) AS (
    SELECT l.target_id, l.source_id, 1
    FROM ticket_links l
    JOIN tickets s ON s.tenant_id = l.tenant_id AND s.id = l.target_id
    WHERE l.tenant_id = sqlc.arg(tenant_id) AND l.source_id = sqlc.arg(ticket_id)::uuid AND l.type = 'blocks'
      AND s.deleted_at IS NULL AND app_ticket_visible(s.project_id, s.confidential, s.assignee_id, s.reporter_id)
    UNION
    SELECT l.target_id, l.source_id, reach.depth + 1
    FROM reach
    JOIN ticket_links l ON l.tenant_id = sqlc.arg(tenant_id) AND l.source_id = reach.id AND l.type = 'blocks'
    JOIN tickets s ON s.tenant_id = l.tenant_id AND s.id = l.target_id
    WHERE reach.depth < sqlc.arg(max_depth)::integer AND s.id <> sqlc.arg(ticket_id)::uuid
      AND s.deleted_at IS NULL AND app_ticket_visible(s.project_id, s.confidential, s.assignee_id, s.reporter_id)
), first AS (
    SELECT DISTINCT ON (reach.id) reach.id, reach.via, reach.depth FROM reach ORDER BY reach.id, reach.depth, reach.via
), tree (id, depth, path) AS (
    SELECT first.id, first.depth, ARRAY[first.id] FROM first WHERE first.depth = 1
    UNION ALL
    SELECT first.id, first.depth, tree.path || first.id FROM tree JOIN first ON first.via = tree.id
), nodes AS (
    SELECT tree.id, tree.depth, tree.path, false AS repeated FROM tree
    UNION
    SELECT reach.id, tree.depth + 1, tree.path || reach.id, true
    FROM reach
    JOIN first ON first.id = reach.id AND first.via <> reach.via
    JOIN tree ON tree.id = reach.via
), shown AS (
    SELECT nodes.depth, nodes.path, nodes.repeated, sp.key AS project_key, s.number, s.title, s.state, s.blocked_from,
           s.assignee_id, au.username AS assignee_username, au.display_name AS assignee_name,
           s.progress, s.progress_derived, s.progress_refinement, s.progress_refinement_derived,
           s.progress_review, s.progress_review_derived,
           count(*) FILTER (WHERE NOT nodes.repeated AND s.state NOT IN ('done', 'dropped')) OVER () AS open_count
    FROM nodes
    JOIN tickets s ON s.tenant_id = sqlc.arg(tenant_id) AND s.id = nodes.id
    JOIN projects sp ON sp.tenant_id = s.tenant_id AND sp.id = s.project_id
    LEFT JOIN users au ON au.id = s.assignee_id
    WHERE s.deleted_at IS NULL AND app_ticket_visible(s.project_id, s.confidential, s.assignee_id, s.reporter_id)
)
SELECT shown.depth::integer AS depth, shown.path::uuid[] AS path, shown.repeated::boolean AS repeated,
       shown.project_key, shown.number, shown.title, shown.state, shown.blocked_from,
       shown.assignee_id, shown.assignee_username, shown.assignee_name,
       shown.progress, shown.progress_derived, shown.progress_refinement, shown.progress_refinement_derived,
       shown.progress_review, shown.progress_review_derived, shown.open_count::integer AS open_count
FROM shown
WHERE sqlc.narg(after)::uuid[] IS NULL OR shown.path > sqlc.narg(after)::uuid[]
ORDER BY shown.path
LIMIT sqlc.arg(page_size);
