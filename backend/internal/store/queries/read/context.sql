-- What the context of a ticket shows beside the canonical document
-- (docs/adr/0044 D2). The comments, the attachments, the activity and the
-- prerequisite tree are read with the queries of their own routes; the links
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
