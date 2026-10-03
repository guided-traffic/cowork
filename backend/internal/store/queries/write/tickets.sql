-- name: NextTicketNumber :one
-- The project's next number (docs/adr/0022 D2): the counter row's lock orders
-- concurrent filings; a filing that rolls back hands its number out again,
-- which is right, because that ticket never existed.
UPDATE ticket_counters
SET last_number = last_number + 1
WHERE tenant_id = sqlc.arg(tenant_id) AND project_id = sqlc.arg(project_id)
RETURNING last_number;

-- name: InsertTicket :one
-- A new ticket, with its key at the bottom of its project's rank
-- (docs/adr/0014 D2).
INSERT INTO tickets (
    tenant_id, project_id, number, type, title, body, severity, security, threat,
    urgency_derived, urgency_rule, effort, parent_id, reporter_id, assignee_id, confidential, rank
) VALUES (
    sqlc.arg(tenant_id), sqlc.arg(project_id), sqlc.arg(number), sqlc.arg(type), sqlc.arg(title),
    sqlc.arg(body), sqlc.arg(severity), sqlc.arg(security), sqlc.narg(threat), sqlc.arg(urgency_derived),
    sqlc.arg(urgency_rule), sqlc.arg(effort), sqlc.narg(parent_id), sqlc.arg(reporter_id),
    sqlc.narg(assignee_id), sqlc.arg(confidential), sqlc.arg(rank)::text
)
RETURNING id;

-- name: UpdateTicketFields :one
-- The fields of PATCH, a compare-and-set on the version (docs/adr/0050 D1);
-- progress is the implementation stage (docs/adr/0017 D2).
UPDATE tickets
SET type = sqlc.arg(type), title = sqlc.arg(title), severity = sqlc.arg(severity),
    security = sqlc.arg(security), threat = sqlc.narg(threat), effort = sqlc.arg(effort),
    parent_id = sqlc.narg(parent_id), assignee_id = sqlc.narg(assignee_id),
    progress = sqlc.arg(progress), progress_refinement = sqlc.arg(progress_refinement),
    progress_review = sqlc.arg(progress_review), confidential = sqlc.arg(confidential),
    version = version + 1, updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id) AND version = sqlc.arg(version)
RETURNING version;

-- name: UpdateTicketBody :one
UPDATE tickets
SET body = sqlc.arg(body), version = version + 1, updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id) AND version = sqlc.arg(version)
RETURNING version;

-- name: SetUrgencyOverride :one
-- An override, its reason optional for a person, or none (docs/adr/0010 D3).
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
       t.progress_refinement, t.progress_refinement_derived, t.progress_review, t.progress_review_derived,
       t.parent_id, pt.number AS parent_number,
       t.reporter_id, ru.username AS reporter_username, ru.display_name AS reporter_name,
       t.assignee_id, au.username AS assignee_username, au.display_name AS assignee_name,
       t.confidential, t.rank, t.opened_at, t.decided_at, t.done_at, t.done_from, t.done_by_hand,
       (SELECT count(*) FROM ticket_links pl
        JOIN tickets ps ON ps.tenant_id = pl.tenant_id AND ps.id = pl.source_id
        WHERE pl.tenant_id = t.tenant_id AND pl.target_id = t.id AND pl.type = 'blocks'
          AND ps.state NOT IN ('done', 'dropped')
          AND app_ticket_visible(ps.project_id, ps.confidential, ps.assignee_id, ps.reporter_id))::integer AS open_prerequisites,
       t.version, t.created_at, t.updated_at
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
-- (docs/adr/0045 D2). The dates are the acts' (docs/adr/0009 D6): decided_at
-- the last time the ticket reached decided, done_at while it is done. done
-- keeps the state it came from and whether it was set by hand, and leaves the
-- progress stages as they are (docs/adr/0009 D5, docs/adr/0017 D5). The block
-- columns are what the move gives them: the block entering blocked, the block a
-- ticket done from blocked keeps and takes back, none otherwise. done and
-- dropped take the rank away, a reopen brings the key it is given — the
-- bottom — and every other move keeps the rank (docs/adr/0014 D1). bump is
-- false where the request raised the version already, in a PATCH whose stages
-- close or reopen the ticket.
UPDATE tickets
SET state = sqlc.arg(to_state),
    blocked_from = sqlc.narg(blocked_from), block_kind = sqlc.narg(block_kind),
    block_reason = sqlc.narg(block_reason), block_ticket_id = sqlc.narg(block_ticket_id),
    block_external_ref = sqlc.narg(block_external_ref),
    rank = CASE WHEN sqlc.arg(to_state)::ticket_state IN ('done', 'dropped') THEN NULL
                ELSE coalesce(sqlc.narg(rank)::text, rank) END,
    decided_at = CASE WHEN sqlc.arg(to_state)::ticket_state = 'decided' THEN now() ELSE decided_at END,
    done_at = CASE WHEN sqlc.arg(to_state)::ticket_state = 'done' THEN now() END,
    done_from = CASE WHEN sqlc.arg(to_state)::ticket_state = 'done' THEN sqlc.arg(from_state)::ticket_state END,
    done_by_hand = sqlc.arg(to_state)::ticket_state = 'done' AND sqlc.arg(done_by_hand)::boolean,
    version = version + CASE WHEN sqlc.arg(bump)::boolean THEN 1 ELSE 0 END, updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id) AND state = sqlc.arg(from_state)
RETURNING version;

-- name: EndDoneByHand :one
-- The withdrawal of a done by hand from a ticket whose three stages are full:
-- it stays done, by its stages (docs/adr/0009 D5); a compare-and-set on the
-- done by hand.
UPDATE tickets
SET done_by_hand = false, version = version + 1, updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id) AND state = 'done' AND done_by_hand
RETURNING version;

-- name: RefreshDerivedProgress :one
-- The ticket's derived stages after a change of its children
-- (docs/adr/0017 D3), progress_derived the implementation stage's. It leaves
-- the version alone (docs/adr/0050 D1); when the last child has left, each
-- stage's own value starts at the last derived one. A done ticket that gains
-- children is done by hand from then on: a parent is never done by its stages
-- (docs/adr/0009 D5). No row when nothing changed; else the parent, whose
-- stages read this one's.
WITH d AS (SELECT ticket_derived_stage(sqlc.arg(tenant_id), sqlc.arg(id), 'refinement') AS refinement,
                  ticket_derived_stage(sqlc.arg(tenant_id), sqlc.arg(id), 'implementation') AS implementation,
                  ticket_derived_stage(sqlc.arg(tenant_id), sqlc.arg(id), 'review') AS review)
UPDATE tickets t
SET progress_derived = d.implementation,
    progress_refinement_derived = d.refinement,
    progress_review_derived = d.review,
    progress = CASE WHEN d.implementation IS NULL THEN coalesce(t.progress_derived, t.progress) ELSE t.progress END,
    progress_refinement = CASE WHEN d.refinement IS NULL
                               THEN coalesce(t.progress_refinement_derived, t.progress_refinement)
                               ELSE t.progress_refinement END,
    progress_review = CASE WHEN d.review IS NULL
                           THEN coalesce(t.progress_review_derived, t.progress_review)
                           ELSE t.progress_review END,
    done_by_hand = t.done_by_hand OR (t.state = 'done' AND d.implementation IS NOT NULL),
    updated_at = now()
FROM d
WHERE t.tenant_id = sqlc.arg(tenant_id) AND t.id = sqlc.arg(id)
  AND (t.progress_derived IS DISTINCT FROM d.implementation
       OR t.progress_refinement_derived IS DISTINCT FROM d.refinement
       OR t.progress_review_derived IS DISTINCT FROM d.review)
RETURNING t.parent_id;

-- name: TicketFacts :one
-- What a published act carries of its ticket: the project, the version and
-- the confidential rule's inputs (docs/adr/0054 D2, D3).
-- visibility: exempt (the publication of a committed act; subscribers filter)
SELECT project_id, version, confidential, assignee_id, reporter_id
FROM tickets
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id);

-- name: LockProjectRank :exec
-- The project's counter row, locked until the transaction ends. A filing takes
-- it with its number (NextTicketNumber); a move and a return from done or
-- dropped — a reopen, a withdrawal, a lower stage — take it before they read
-- a key. So every write that hands out a key in the project is
-- ordered by one row — two of them never compute a key from the same
-- neighbours (docs/adr/0014 D2) — and filing still never waits for a change of
-- the project's settings (migration 3). Taken before any ticket row is written.
SELECT last_number FROM ticket_counters
WHERE tenant_id = sqlc.arg(tenant_id) AND project_id = sqlc.arg(project_id)
FOR UPDATE;

-- name: ListUnrankedTickets :many
-- The project's open tickets without a key — filed, or reopened, by a release
-- before the rank (docs/adr/0028 D3) — in number order: they are ranked at the
-- bottom before the next key is handed out, where the list already shows them.
-- visibility: exempt (the rank keys of the project the caller writes in, never shown)
SELECT id FROM tickets
WHERE tenant_id = sqlc.arg(tenant_id) AND project_id = sqlc.arg(project_id)
  AND rank IS NULL AND state NOT IN ('done', 'dropped')
ORDER BY number;

-- name: RankUnrankedTicket :exec
-- An unranked open ticket's first key, at the place the list showed it: no
-- move, so no act and no version (docs/adr/0050 D1). Done and dropped take no
-- rank lock: a ticket that went done or dropped since ListUnrankedTickets
-- read it stays without a key.
UPDATE tickets
SET rank = sqlc.arg(rank)::text
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id) AND rank IS NULL
  AND state NOT IN ('done', 'dropped');

-- name: LastRank :one
-- The project's greatest key, "" without one: the bottom. Every ticket counts,
-- whatever the caller can see and whatever its state, so a key is never handed
-- out twice.
-- visibility: exempt (the rank keys of the project the caller writes in, never shown)
SELECT coalesce(max(rank), '')::text AS last
FROM tickets
WHERE tenant_id = sqlc.arg(tenant_id) AND project_id = sqlc.arg(project_id);

-- name: GetTicketRank :one
-- A ticket's state and key as they are under the rank lock.
-- visibility: exempt (a ticket the caller read through the predicate in this transaction)
SELECT state, rank FROM tickets
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id);

-- name: NextRankedTicket :one
-- The key of the first ticket after a key in the project's rank, whatever the
-- caller can see and whatever its state: a new key lies strictly between two
-- keys that exist, so it never equals or passes one the caller cannot see.
-- visibility: exempt (the rank keys of the project the caller writes in, never shown)
SELECT rank::text AS rank FROM tickets
WHERE tenant_id = sqlc.arg(tenant_id) AND project_id = sqlc.arg(project_id) AND rank > sqlc.arg(after)::text
ORDER BY rank
LIMIT 1;

-- name: PreviousRankedTicket :one
-- The key of the last ticket before a key in the project's rank, as
-- NextRankedTicket.
-- visibility: exempt (the rank keys of the project the caller writes in, never shown)
SELECT rank::text AS rank FROM tickets
WHERE tenant_id = sqlc.arg(tenant_id) AND project_id = sqlc.arg(project_id) AND rank < sqlc.arg(before)::text
ORDER BY rank DESC
LIMIT 1;

-- name: NextSeenRankedTicket :one
-- The first open ticket after a key in the project's rank that the caller can
-- see: when it is the moved ticket, the move changes nothing the caller sees,
-- and it is answered as no move whatever sits between unseen
-- (docs/adr/0014 D2).
SELECT t.id FROM tickets t
WHERE t.tenant_id = sqlc.arg(tenant_id) AND t.project_id = sqlc.arg(project_id)
  AND t.rank > sqlc.arg(after)::text AND t.state NOT IN ('done', 'dropped')
  AND app_ticket_visible(t.project_id, t.confidential, t.assignee_id, t.reporter_id)
ORDER BY t.rank
LIMIT 1;

-- name: PreviousSeenRankedTicket :one
-- The last open ticket before a key that the caller can see, as
-- NextSeenRankedTicket.
SELECT t.id FROM tickets t
WHERE t.tenant_id = sqlc.arg(tenant_id) AND t.project_id = sqlc.arg(project_id)
  AND t.rank < sqlc.arg(before)::text AND t.state NOT IN ('done', 'dropped')
  AND app_ticket_visible(t.project_id, t.confidential, t.assignee_id, t.reporter_id)
ORDER BY t.rank DESC
LIMIT 1;

-- name: MoveTicketRank :one
-- A move in the rank: one row, the ticket's own version raised
-- (docs/adr/0014 D2, docs/adr/0050 D1); a ticket that went done or dropped
-- meanwhile is no row.
UPDATE tickets
SET rank = sqlc.arg(rank)::text, version = version + 1, updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id) AND state NOT IN ('done', 'dropped')
RETURNING version;
