-- The three progress stages, done by the stages or by hand, the block a ticket
-- done from blocked keeps, and an urgency override without a reason
-- (docs/adr/0009 D2, D5, docs/adr/0010 D3, docs/adr/0017 D2, D3, D5).
--
-- Nothing here narrows what the previous release reads or writes
-- (docs/adr/0028 D3): the new columns have defaults it never touches, the
-- constraints replaced below admit every row it writes, and its derivation,
-- ticket_derived_progress, stays as it is. What it writes over this schema in
-- a rollback (D4) reads as follows. Its done sets progress to 100 and leaves
-- done_from, done_by_hand and the two new stages as they were: without
-- done_from the ticket was done from in-progress, the one way it has, and it
-- is done by its stages only while they are full — by hand otherwise. Its
-- derivation keeps progress_derived alone, which therefore says whether a
-- ticket has children: when a parent's last child leaves it clears
-- progress_derived and leaves the derived refinement and review as they were,
-- so those two count only while progress_derived is set.

-- blocked remembers review as well (docs/adr/0009 D2). The value exists since
-- migration 18, committed before this file runs.
ALTER TABLE tickets DROP CONSTRAINT tickets_blocked_from_check;
ALTER TABLE tickets ADD CONSTRAINT tickets_blocked_from_check
    CHECK (blocked_from IN ('filed', 'analysed', 'decided', 'in-progress', 'review'));

-- progress is the implementation stage and keeps its name; refinement and
-- review stand beside it, each 0 to 100 in steps of five, and each has the
-- value derived from the children while there are any, NULL without
-- (docs/adr/0017 D2, D3). done_from is the state before done, done_by_hand
-- whether done was set by hand rather than by the stages (docs/adr/0009 D5);
-- both describe the ticket while it is done.
ALTER TABLE tickets
    ADD COLUMN progress_refinement         smallint NOT NULL DEFAULT 0
        CHECK (progress_refinement BETWEEN 0 AND 100 AND progress_refinement % 5 = 0),
    ADD COLUMN progress_review             smallint NOT NULL DEFAULT 0
        CHECK (progress_review BETWEEN 0 AND 100 AND progress_review % 5 = 0),
    ADD COLUMN progress_refinement_derived smallint
        CHECK (progress_refinement_derived BETWEEN 0 AND 100 AND progress_refinement_derived % 5 = 0),
    ADD COLUMN progress_review_derived     smallint
        CHECK (progress_review_derived BETWEEN 0 AND 100 AND progress_review_derived % 5 = 0),
    ADD COLUMN done_from                   ticket_state
        CHECK (done_from IN ('filed', 'analysed', 'decided', 'in-progress', 'review', 'blocked')),
    ADD COLUMN done_by_hand                boolean  NOT NULL DEFAULT false;

-- A ticket done from blocked keeps its block, so that withdrawing the done by
-- hand, or lowering a stage of a ticket done by its stages, returns it to
-- blocked with what it waits on (docs/adr/0009 D2, D5). Migration 8 tied the
-- block to the state blocked alone, in its second unnamed table CHECK
-- (tickets_check1); now blocked has its block, and a block outside blocked is
-- the one a ticket done from blocked keeps. The previous release clears all
-- five columns on every move but a block, which satisfies both.
ALTER TABLE tickets DROP CONSTRAINT tickets_check1;
ALTER TABLE tickets ADD CONSTRAINT tickets_block_check
    CHECK (state <> 'blocked' OR (blocked_from IS NOT NULL AND block_kind IS NOT NULL AND block_reason IS NOT NULL));
ALTER TABLE tickets ADD CONSTRAINT tickets_block_kept_check
    CHECK (state = 'blocked' OR (state = 'done' AND done_from = 'blocked')
           OR (blocked_from IS NULL AND block_kind IS NULL AND block_reason IS NULL
               AND block_ticket_id IS NULL AND block_external_ref IS NULL));

-- An override holds until it is withdrawn or replaced, and a person may set it
-- without a reason (docs/adr/0010 D3). Migration 8's fifth unnamed table CHECK
-- (tickets_check4) required the reason with the override; what stays is that
-- there is no reason without an override.
ALTER TABLE tickets DROP CONSTRAINT tickets_check4;
ALTER TABLE tickets ADD CONSTRAINT tickets_urgency_override_reason_check
    CHECK (urgency_override IS NOT NULL OR urgency_override_reason IS NULL);

-- The stage a ticket's children give it (docs/adr/0017 D3): p_stage is
-- refinement, implementation or review, and each follows the rule of
-- ticket_derived_progress — the mean of the children's same stage weighted by
-- effort (XS 1, S 2, M 3, L 5), a dropped child left out, a done child counted
-- as 100, rounded to the nearest five with halves up; 0 when every child is
-- dropped; NULL without children. A child counts with the stage it shows: its
-- derived value while it has children — progress_derived set — and its own
-- value without, whatever the previous release left in the derived refinement
-- and review. Like the urgency it is the ticket's own value and reads children
-- the caller may not see. ticket_derived_progress stays for the previous
-- release, which derives the implementation stage alone.
CREATE FUNCTION ticket_derived_stage(p_tenant_id uuid, p_id uuid, p_stage text) RETURNS smallint
    LANGUAGE sql STABLE
    AS $$
        SELECT CASE
                   WHEN count(*) = 0 THEN NULL
                   WHEN coalesce(sum(w) FILTER (WHERE c.state <> 'dropped'), 0) = 0 THEN 0
                   ELSE (floor(sum(w * CASE WHEN c.state = 'done' THEN 100
                                            WHEN c.progress_derived IS NULL
                                                THEN CASE p_stage WHEN 'refinement' THEN c.progress_refinement
                                                                  WHEN 'review' THEN c.progress_review
                                                                  ELSE c.progress END
                                            WHEN p_stage = 'refinement'
                                                THEN coalesce(c.progress_refinement_derived, c.progress_refinement)
                                            WHEN p_stage = 'review'
                                                THEN coalesce(c.progress_review_derived, c.progress_review)
                                            ELSE c.progress_derived END)
                                   FILTER (WHERE c.state <> 'dropped')::numeric
                               / sum(w) FILTER (WHERE c.state <> 'dropped') / 5 + 0.5) * 5)::smallint
               END
        FROM tickets c
        CROSS JOIN LATERAL (SELECT CASE c.effort WHEN 'XS' THEN 1 WHEN 'S' THEN 2 WHEN 'M' THEN 3 ELSE 5 END AS w) e
        WHERE c.tenant_id = p_tenant_id AND c.parent_id = p_id
    $$;

-- The backfill (docs/adr/0017 D2): a ticket keeps its progress as the
-- implementation stage; refinement is full from decided on — decided,
-- in-progress, review and done, and blocked from one of them — and review when
-- the ticket is done. A done ticket was done from in-progress, the one way the
-- previous release had. It is done by its stages when they are full — that
-- release's done set progress to 100 — and by hand otherwise (docs/adr/0009
-- D5): a parent, and a leaf whose progress fell below 100 after it closed — a
-- parent that release closed takes the last derived value as its own when its
-- last child leaves. Then the parents' derived refinement and review, from the
-- leaves up, one level per pass, until a pass changes nothing.
--
-- Row-level security is forced on tickets for the owner as well (migration 8),
-- and no tenant is set in a migration, so the policy would hide every row: the
-- backfill lifts the force for itself and restores it, as migration 17 does.
-- The whole file runs as one transaction, which holds tickets exclusively from
-- the first ALTER on; a failure restores the force with everything else, and
-- the runtime role is never affected (docs/adr/0021 D1).
ALTER TABLE tickets NO FORCE ROW LEVEL SECURITY;
UPDATE tickets t
SET progress_refinement = CASE WHEN t.state IN ('decided', 'in-progress', 'review', 'done')
                                    OR (t.state = 'blocked' AND t.blocked_from IN ('decided', 'in-progress', 'review'))
                               THEN 100 ELSE 0 END,
    progress_review = CASE WHEN t.state = 'done' THEN 100 ELSE 0 END,
    done_from = CASE WHEN t.state = 'done' THEN 'in-progress'::ticket_state END,
    done_by_hand = t.state = 'done'
                   AND (t.progress <> 100
                        OR EXISTS (SELECT 1 FROM tickets c WHERE c.tenant_id = t.tenant_id AND c.parent_id = t.id));
DO $$
DECLARE
    changed bigint;
BEGIN
    LOOP
        UPDATE tickets t
        SET progress_refinement_derived = d.refinement, progress_review_derived = d.review
        FROM (SELECT p.id, ticket_derived_stage(p.tenant_id, p.id, 'refinement') AS refinement,
                     ticket_derived_stage(p.tenant_id, p.id, 'review') AS review
              FROM tickets p
              WHERE EXISTS (SELECT 1 FROM tickets c WHERE c.tenant_id = p.tenant_id AND c.parent_id = p.id)) d
        WHERE t.id = d.id
          AND (t.progress_refinement_derived IS DISTINCT FROM d.refinement
               OR t.progress_review_derived IS DISTINCT FROM d.review);
        GET DIAGNOSTICS changed = ROW_COUNT;
        EXIT WHEN changed = 0;
    END LOOP;
END
$$;
ALTER TABLE tickets FORCE ROW LEVEL SECURITY;

DO $$
DECLARE
    runtime text := current_setting('cowork.runtime_role');
BEGIN
    EXECUTE format('GRANT UPDATE (progress_refinement, progress_review, progress_refinement_derived, '
                   'progress_review_derived, done_from, done_by_hand) ON tickets TO %I', runtime);
END
$$;
