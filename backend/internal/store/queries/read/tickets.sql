-- The ticket's columns are listed once more in the list builder
-- (internal/store/tickets.go); a unit test holds the two lists equal.
-- open_prerequisites counts the open tickets that block it which the caller
-- can see, the number on a board card (docs/adr/0018 D1, docs/adr/0012 D7); a
-- hidden one is never counted.

-- name: GetTicketByNumber :one
SELECT t.id, t.project_id, p.key AS project_key, t.number, t.type, t.title, t.body, t.state,
       t.blocked_from, t.block_kind, t.block_reason, t.block_ticket_id, t.block_external_ref,
       bp.key AS block_project_key, bt.number AS block_number,
       t.severity, t.security, t.threat, t.urgency_derived, t.urgency_rule, t.urgency_override,
       t.urgency_override_reason, t.urgency_override_by, t.urgency_override_at, t.effort, t.progress, t.progress_derived,
       t.progress_refinement, t.progress_refinement_derived, t.progress_review, t.progress_review_derived,
       t.parent_id, pt.number AS parent_number,
       t.reporter_id, ru.username AS reporter_username, ru.display_name AS reporter_name,
       t.reporter_agent, t.reporter_token_id, t.reporter_token_name,
       t.assignee_id, au.username AS assignee_username, au.display_name AS assignee_name,
       t.confidential, t.rank, t.score_key, t.score_version, t.opened_at, t.decided_at, t.done_at, t.done_from, t.done_by_hand,
       (SELECT count(*) FROM ticket_links pl
        JOIN tickets ps ON ps.tenant_id = pl.tenant_id AND ps.id = pl.source_id
        WHERE pl.tenant_id = t.tenant_id AND pl.target_id = t.id AND pl.type = 'blocks'
          AND ps.state NOT IN ('done', 'dropped')
          AND ps.deleted_at IS NULL AND app_ticket_visible(ps.project_id, ps.confidential, ps.assignee_id, ps.reporter_id))::integer AS open_prerequisites,
       t.version, t.created_at, t.updated_at
FROM tickets t
JOIN projects p ON p.tenant_id = t.tenant_id AND p.id = t.project_id
LEFT JOIN users ru ON ru.id = t.reporter_id
LEFT JOIN users au ON au.id = t.assignee_id
LEFT JOIN tickets pt ON pt.tenant_id = t.tenant_id AND pt.id = t.parent_id
     AND pt.deleted_at IS NULL AND app_ticket_visible(pt.project_id, pt.confidential, pt.assignee_id, pt.reporter_id)
LEFT JOIN tickets bt ON bt.tenant_id = t.tenant_id AND bt.id = t.block_ticket_id
     AND bt.deleted_at IS NULL AND app_ticket_visible(bt.project_id, bt.confidential, bt.assignee_id, bt.reporter_id)
LEFT JOIN projects bp ON bp.tenant_id = bt.tenant_id AND bp.id = bt.project_id
WHERE t.tenant_id = sqlc.arg(tenant_id) AND t.project_id = sqlc.arg(project_id) AND t.number = sqlc.arg(number)
  AND t.deleted_at IS NULL AND app_ticket_visible(t.project_id, t.confidential, t.assignee_id, t.reporter_id);

-- name: ParentChainContains :one
-- Whether ticket_id is the candidate parent or one of its ancestors: the
-- parent cycle refusal (docs/adr/0008 D2).
-- visibility: exempt (an integrity walk returns no ticket)
SELECT ticket_ancestor_or_self(sqlc.arg(tenant_id), sqlc.arg(candidate_parent_id), sqlc.arg(ticket_id))::boolean AS contains;

-- name: CanSeeProject :one
-- Whether a person can see a project: a member of the tenant, and on a
-- restricted project an administrator or on its list (docs/adr/0034 D3). An
-- assignee must be one; assignment admits a person to a confidential ticket
-- (docs/adr/0065 D9), never to a restricted project.
-- visibility: exempt (whether another person sees a project the caller reads)
SELECT EXISTS (
    SELECT 1
    FROM memberships m
    JOIN projects p ON p.tenant_id = m.tenant_id AND p.id = sqlc.arg(project_id)
    WHERE m.tenant_id = sqlc.arg(tenant_id) AND m.user_id = sqlc.arg(user_id)
      AND (NOT p.restricted OR m.role = 'admin'
           OR EXISTS (SELECT 1 FROM project_access a
                      WHERE a.tenant_id = m.tenant_id AND a.project_id = p.id AND a.user_id = m.user_id))
) AS visible;

-- name: ListRankPlaces :many
-- Each ticket's place in its project's rank among the open tickets of its
-- horizon that the caller can see, 1 for the first: the place the backlog's
-- group shows it at, and the secondary indicator of the person-level lists
-- (docs/adr/0014 D5, docs/adr/0018 D1). A ticket the caller cannot see, or a
-- deleted one, is never counted, so the place tells nothing of one; the
-- unranked open tickets of a release before the rank follow the ranked by
-- number, as the list shows them.
SELECT t.id,
       ((SELECT count(*) FROM tickets o
         WHERE o.tenant_id = t.tenant_id AND o.project_id = t.project_id
           AND o.state NOT IN ('done', 'dropped')
           AND coalesce(o.urgency_override, o.urgency_derived) = coalesce(t.urgency_override, t.urgency_derived)
           AND (o.rank < t.rank OR (o.rank IS NOT NULL AND t.rank IS NULL)
                OR (o.rank IS NULL AND t.rank IS NULL AND o.number < t.number))
           AND o.deleted_at IS NULL
           AND app_ticket_visible(o.project_id, o.confidential, o.assignee_id, o.reporter_id)) + 1)::integer AS place
FROM tickets t
WHERE t.tenant_id = sqlc.arg(tenant_id) AND t.id = ANY (sqlc.arg(ids)::uuid[])
  AND t.state NOT IN ('done', 'dropped')
  AND t.deleted_at IS NULL AND app_ticket_visible(t.project_id, t.confidential, t.assignee_id, t.reporter_id);
