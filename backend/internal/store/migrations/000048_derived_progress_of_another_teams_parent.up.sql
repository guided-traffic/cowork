-- The derived progress of a parent of another team writes its derived columns
-- and nothing else (docs/adr/0017 D3 as amended 2026-10-10 and made concrete
-- the same day): a child's team never writes the parent's own stages, its done
-- by hand or its updated_at. Migration 47 let refresh_derived seed a parent's
-- own stages from the last derived values when its last child left, and mark
-- a done parent done by hand when it gained one, whatever team the parent was
-- of — so a writer of team B rewrote a stage of a ticket of team A that a
-- member of A had set, with no act and no version. The seeding and the mark
-- now happen only where the parent is of the caller's own team; a parent of
-- another team keeps its own values until its own team changes them, and
-- tickets_crossing_guard holds a derive crossing to the three derived columns
-- on a row of another team.
--
-- Expand only (docs/adr/0028 D3): two functions replaced, same signatures;
-- the release before calls neither by its body.

-- The derived progress of parents after a change of their children
-- (docs/adr/0017 D3 as amended 2026-10-10): each starting parent and its
-- ancestors, across teams, the deepest first — a child's value before its
-- parent's, and every writer in the same order —, each stage the effort-
-- weighted mean of the same stage of every child of any team, dropped and
-- deleted ones left out, done counted as 100, rounded to five, as
-- ticket_derived_stage derives it within a team. No version and no act
-- (docs/adr/0050 D1). A parent of the caller's own team: when its last child
-- has left, each stage's own value starts at the last derived one, and a done
-- ticket that gains children is done by hand from then on; its updated_at
-- moves. A parent of another team: the three derived columns alone —
-- tickets_crossing_guard holds the crossing to them —, its own stages, done by
-- hand and updated_at as its own team left them. A deleted ticket is passed
-- over, and so is what lies above it: no parent counts it. A parent of another
-- team that changed is told on its team's streams as ticket.changed of the kind
-- derived; nothing of the change reaches the caller. It answers how many
-- tickets changed.
CREATE OR REPLACE FUNCTION refresh_derived(p_parents uuid[]) RETURNS integer
    LANGUAGE plpgsql SECURITY DEFINER
    AS $$
DECLARE
    prev text := NULLIF(current_setting('app.crossing', true), '');
    node record;
    d record;
    w record;
    changed integer := 0;
BEGIN
    PERFORM set_config('app.crossing', 'derive', true);
    FOR node IN
        WITH RECURSIVE chain (start, id, parent_id, dist) AS (
            SELECT t.id, t.id, t.parent_id, 0
            FROM tickets t
            WHERE t.id = ANY (p_parents) AND t.deleted_at IS NULL
            UNION ALL
            SELECT c.start, t.id, t.parent_id, c.dist + 1
            FROM chain c JOIN tickets t ON t.id = c.parent_id
            WHERE t.deleted_at IS NULL AND c.dist < 100000
        ), height AS (
            SELECT chain.start, max(chain.dist) AS top FROM chain GROUP BY chain.start
        ), levels AS (
            SELECT DISTINCT ON (chain.id) chain.id, height.top - chain.dist AS level
            FROM chain JOIN height ON height.start = chain.start
            ORDER BY chain.id, level DESC
        )
        SELECT levels.id FROM levels ORDER BY levels.level DESC, levels.id
    LOOP
        SELECT
            CASE WHEN count(*) = 0 THEN NULL
                 WHEN coalesce(sum(e.w) FILTER (WHERE c.state <> 'dropped'), 0) = 0 THEN 0
                 ELSE (floor(sum(e.w * CASE WHEN c.state = 'done' THEN 100
                                            WHEN c.progress_derived IS NULL THEN c.progress_refinement
                                            ELSE coalesce(c.progress_refinement_derived, c.progress_refinement) END)
                                 FILTER (WHERE c.state <> 'dropped')::numeric
                             / sum(e.w) FILTER (WHERE c.state <> 'dropped') / 5 + 0.5) * 5)::smallint
            END AS refinement,
            CASE WHEN count(*) = 0 THEN NULL
                 WHEN coalesce(sum(e.w) FILTER (WHERE c.state <> 'dropped'), 0) = 0 THEN 0
                 ELSE (floor(sum(e.w * CASE WHEN c.state = 'done' THEN 100
                                            WHEN c.progress_derived IS NULL THEN c.progress
                                            ELSE c.progress_derived END)
                                 FILTER (WHERE c.state <> 'dropped')::numeric
                             / sum(e.w) FILTER (WHERE c.state <> 'dropped') / 5 + 0.5) * 5)::smallint
            END AS implementation,
            CASE WHEN count(*) = 0 THEN NULL
                 WHEN coalesce(sum(e.w) FILTER (WHERE c.state <> 'dropped'), 0) = 0 THEN 0
                 ELSE (floor(sum(e.w * CASE WHEN c.state = 'done' THEN 100
                                            WHEN c.progress_derived IS NULL THEN c.progress_review
                                            ELSE coalesce(c.progress_review_derived, c.progress_review) END)
                                 FILTER (WHERE c.state <> 'dropped')::numeric
                             / sum(e.w) FILTER (WHERE c.state <> 'dropped') / 5 + 0.5) * 5)::smallint
            END AS review
        INTO d
        FROM tickets c
        CROSS JOIN LATERAL (SELECT CASE c.effort WHEN 'XS' THEN 1 WHEN 'S' THEN 2 WHEN 'M' THEN 3 ELSE 5 END AS w) e
        WHERE c.parent_id = node.id AND c.deleted_at IS NULL;

        -- Only a parent of the caller's own team has its own stages seeded,
        -- is marked done by hand and has its updated_at moved here.
        UPDATE tickets t
        SET progress_derived = d.implementation,
            progress_refinement_derived = d.refinement,
            progress_review_derived = d.review,
            progress = CASE WHEN (t.tenant_id = app_tenant_id()) IS TRUE AND d.implementation IS NULL
                            THEN coalesce(t.progress_derived, t.progress)
                            ELSE t.progress END,
            progress_refinement = CASE WHEN (t.tenant_id = app_tenant_id()) IS TRUE AND d.refinement IS NULL
                                       THEN coalesce(t.progress_refinement_derived, t.progress_refinement)
                                       ELSE t.progress_refinement END,
            progress_review = CASE WHEN (t.tenant_id = app_tenant_id()) IS TRUE AND d.review IS NULL
                                   THEN coalesce(t.progress_review_derived, t.progress_review)
                                   ELSE t.progress_review END,
            done_by_hand = CASE WHEN (t.tenant_id = app_tenant_id()) IS TRUE
                                THEN t.done_by_hand OR (t.state = 'done' AND d.implementation IS NOT NULL)
                                ELSE t.done_by_hand END,
            updated_at = CASE WHEN (t.tenant_id = app_tenant_id()) IS TRUE THEN now() ELSE t.updated_at END
        WHERE t.id = node.id AND t.deleted_at IS NULL
          AND (t.progress_derived IS DISTINCT FROM d.implementation
               OR t.progress_refinement_derived IS DISTINCT FROM d.refinement
               OR t.progress_review_derived IS DISTINCT FROM d.review)
        RETURNING t.tenant_id, t.project_id, t.number, t.version, t.confidential, t.assignee_id, t.reporter_id
        INTO w;
        IF FOUND THEN
            changed := changed + 1;
            IF w.tenant_id IS DISTINCT FROM app_tenant_id() THEN
                PERFORM pg_notify('cowork_events', json_build_object(
                    'id', uuidv7(), 'tenant', w.tenant_id, 'project', w.project_id, 'entity', 'ticket',
                    'action', 'derived',
                    'key', (SELECT tn.slug FROM tenants tn WHERE tn.id = w.tenant_id) || '/'
                           || (SELECT p.key FROM projects p WHERE p.tenant_id = w.tenant_id AND p.id = w.project_id)
                           || '-' || w.number,
                    'version', w.version, 'confidential', w.confidential, 'assignee', w.assignee_id,
                    'reporter', w.reporter_id)::text);
            END IF;
        END IF;
    END LOOP;
    PERFORM set_config('app.crossing', coalesce(prev, ''), true);
    RETURN changed;
END
$$;

-- What a crossing that writes may change of a ticket of any team, a rule on
-- columns a policy cannot state (docs/adr/0021 D6): a derive crossing on a
-- ticket of the caller's own team the derived progress, the stages it seeds,
-- done by hand and updated_at; on a ticket of another team the three derived
-- columns alone (docs/adr/0017 D3 as made concrete 2026-10-10); the end of a
-- relation the parent alone. Anything else is refused (SQLSTATE 42501). The
-- generated search column is computed after the trigger and compared by
-- neither. Outside such a crossing it lets every update through.
CREATE OR REPLACE FUNCTION tickets_crossing_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE
    allowed text[];
BEGIN
    CASE app_crossing()
        WHEN 'derive' THEN
            IF NEW.tenant_id IS DISTINCT FROM app_tenant_id() THEN
                allowed := ARRAY['progress_derived', 'progress_refinement_derived', 'progress_review_derived', 'search'];
            ELSE
                allowed := ARRAY['progress_derived', 'progress_refinement_derived', 'progress_review_derived', 'progress',
                                 'progress_refinement', 'progress_review', 'done_by_hand', 'updated_at', 'search'];
            END IF;
        WHEN 'purge' THEN
            allowed := ARRAY['parent_id', 'search'];
        ELSE
            RETURN NEW;
    END CASE;
    IF (to_jsonb(NEW) - allowed) IS DISTINCT FROM (to_jsonb(OLD) - allowed) THEN
        RAISE EXCEPTION 'a crossing of the kind % changes only its own columns of a ticket', app_crossing()
            USING ERRCODE = '42501';
    END IF;
    RETURN NEW;
END
$$;

-- Replacing a function drops its settings: refresh_derived resolves names on
-- the fixed path again, the schema and then pg_temp last. Its grants stay.
DO $$
BEGIN
    EXECUTE format('ALTER FUNCTION refresh_derived(uuid[]) SET search_path = %I, pg_temp', current_schema());
END
$$;
