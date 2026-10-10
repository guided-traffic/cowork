-- Relations across projects and teams (docs/adr/0005 D3, docs/adr/0008 D2,
-- docs/adr/0012 D2, D4–D7, docs/adr/0017 D3, docs/adr/0021 D7,
-- docs/adr/0024 D2, D6, docs/adr/0034 D4, docs/adr/0065 D5): a ticket's
-- parent, its children and its links may be tickets of another project or of
-- another team of the installation.
--
-- Expand only (docs/adr/0028 D3): the parent's and a link target's foreign
-- keys are widened to the ticket alone, relates-to's ordering check gives way
-- to a unique index, and indexes, functions, a trigger and policies for the
-- owner role are added. Nothing the previous release reads or writes is
-- narrowed: ticket_ancestor_or_self, blocks_path_exists and
-- ticket_derived_stage stay as they are, and a rollback walks and derives
-- within one team again — and ends the relations into another team at a
-- purge through the foreign keys' actions, without acts in the other team.
--
-- How a team's data is read or written past its row-level security here
-- (docs/adr/0021 D7 as made concrete 2026-10-10): never by the runtime role,
-- whose queries every forced policy holds to the transaction's team as
-- before. The crossings are SECURITY DEFINER functions of the owner role,
-- each a single read or write — the head read with the sight decision, the
-- cycle walks, the derived progress, the far ends an act is recorded at, the
-- end of the relations at a purge or a team's deletion — admitted by
-- permissive policies TO the owner role that ask for the kind of crossing in
-- the setting app.crossing. A function sets that setting with set_config as
-- its first statement and restores the value it found before it returns; a
-- SET clause cannot carry it, because PostgreSQL refuses a custom setting in
-- a function's SET clause to a role that is not a superuser without a
-- GRANT SET ON PARAMETER, which no installation's owner role holds (verified
-- on PostgreSQL 18.6). Every other SECURITY DEFINER function empties the
-- setting first, so a value a caller left can never reach the owner's
-- policies through it; the runtime role, to which no crossing policy
-- applies, gains nothing by setting it.

-- The act recorded on a blocked ticket of another team when its prerequisite
-- reaches done or dropped, which tells the blocked ticket's watchers
-- (docs/adr/0012 D5 as made concrete 2026-10-10). A new enum value cannot be
-- used in the transaction that adds it; nothing in this file uses it.
ALTER TYPE audit_action ADD VALUE IF NOT EXISTS 'prerequisite_settled';

-- The parent: a ticket of any project of the installation
-- (docs/adr/0008 D2). The previous release's purge deletes a ticket after it
-- detached the children of its own team; ON DELETE SET NULL detaches those of
-- another team in a rollback. This release ends them with acts before the
-- delete (end_relations_elsewhere), so the action finds nothing to do.
ALTER TABLE tickets DROP CONSTRAINT tickets_tenant_id_project_id_parent_id_fkey;
ALTER TABLE tickets ADD CONSTRAINT tickets_parent_id_fkey
    FOREIGN KEY (parent_id) REFERENCES tickets (id) ON DELETE SET NULL;
CREATE INDEX tickets_by_parent_any ON tickets (parent_id) WHERE parent_id IS NOT NULL;

-- A link lives in its source's team, the key to its source unchanged; its
-- target may be a ticket of any team (docs/adr/0012 D2). relates-to is stored
-- once: inside a team the smaller id first, as the API has always stored it;
-- across teams the end it was made from is the source, so the unique index
-- over the pair takes the place of the ordering check.
ALTER TABLE ticket_links DROP CONSTRAINT ticket_links_tenant_id_target_id_fkey;
ALTER TABLE ticket_links ADD CONSTRAINT ticket_links_target_id_fkey
    FOREIGN KEY (target_id) REFERENCES tickets (id) ON DELETE CASCADE;
ALTER TABLE ticket_links DROP CONSTRAINT ticket_links_check1;
CREATE UNIQUE INDEX ticket_links_relates_once ON ticket_links (least(source_id, target_id), greatest(source_id, target_id))
    WHERE type = 'relates-to';
CREATE INDEX ticket_links_by_target_any ON ticket_links (target_id, type);

-- The crossing a SECURITY DEFINER function is running, NULL outside one: what
-- the owner role's policies below ask. Read guarded, as every setting is
-- (docs/adr/0021 D1).
CREATE FUNCTION app_crossing() RETURNS text
    LANGUAGE sql STABLE PARALLEL SAFE
    AS $$ SELECT NULLIF(current_setting('app.crossing', true), '') $$;

-- The ninth setting (docs/adr/0021 D3): a token's team restriction
-- (docs/adr/0035 D3). The boundary holds a request to its team already; the
-- sight of a ticket of another team reads it, so a token restricted to one
-- team reads every other team's tickets by their heads only.
CREATE FUNCTION app_restricted_tenant_id() RETURNS uuid
    LANGUAGE sql STABLE PARALLEL SAFE
    AS $$ SELECT NULLIF(current_setting('app.restricted_tenant_id', true), '')::uuid $$;

-- Whether the caller holds a role in the transaction's team. The boundary of
-- the request layer admits nobody else to a team's tickets (docs/adr/0023 D5),
-- and the queries of the runtime role rely on it; the crossings ask once more,
-- so that a transaction that names a team its person holds no role in reads no
-- other team's heads through them.
CREATE FUNCTION app_is_member() RETURNS boolean
    LANGUAGE sql STABLE PARALLEL SAFE
    AS $$ SELECT EXISTS (SELECT 1 FROM memberships m WHERE m.tenant_id = app_tenant_id() AND m.user_id = app_user_id()) $$;

-- What the caller sees of a ticket at the end of a relation of a ticket they
-- see (docs/adr/0005 D3, docs/adr/0034 D4, docs/adr/0065 D5):
--   sees        — the caller reads it: a member of its team, any role, its
--                 project open to them, and not confidential or admitted;
--   head        — its team's name, key, title, type and state, nothing more:
--                 a person who holds no role in its team, to whom its project
--                 is restricted, or a token outside its restriction;
--   placeholder — its team's name alone, `<team> [Confidential]`: a
--                 confidential ticket the caller is not admitted to, in
--                 another team or their own.
-- Admitted to a confidential ticket is a member of its team who administers
-- it or is its assignee or reporter (docs/adr/0065 D1); a token outside its
-- restriction is admitted to none. A deleted ticket answers like a missing one
-- and is left out by the callers. An internal function of the crossings,
-- which call it as the owner; nobody else executes it.
CREATE FUNCTION ticket_sight(p_tenant_id uuid, p_project_id uuid, p_confidential boolean, p_assignee_id uuid,
                             p_reporter_id uuid) RETURNS text
    LANGUAGE sql STABLE
    AS $$
        WITH member AS (
            SELECT max(CASE m.role WHEN 'admin' THEN 3 WHEN 'member' THEN 2 ELSE 1 END) AS rank
            FROM memberships m
            WHERE m.tenant_id = p_tenant_id AND m.user_id = app_user_id()
        ), facts AS (
            SELECT (app_restricted_tenant_id() IS NOT NULL AND app_restricted_tenant_id() <> p_tenant_id)
                       OR (app_restricted_project_id() IS NOT NULL AND app_restricted_project_id() <> p_project_id) AS outside,
                   member.rank IS NOT NULL AS is_member,
                   coalesce(member.rank = 3, false) AS is_admin
            FROM member
        )
        SELECT CASE
                   WHEN facts.outside THEN CASE WHEN p_confidential THEN 'placeholder' ELSE 'head' END
                   WHEN p_confidential AND NOT (facts.is_member AND (facts.is_admin
                                                                     OR coalesce(p_assignee_id = app_user_id(), false)
                                                                     OR coalesce(p_reporter_id = app_user_id(), false)))
                       THEN 'placeholder'
                   WHEN facts.is_member AND EXISTS (
                            SELECT 1 FROM projects p
                            WHERE p.tenant_id = p_tenant_id AND p.id = p_project_id
                              AND (NOT p.restricted OR facts.is_admin
                                   OR EXISTS (SELECT 1 FROM project_access a
                                              WHERE a.tenant_id = p.tenant_id AND a.project_id = p.id
                                                AND a.user_id = app_user_id())))
                       THEN 'sees'
                   ELSE 'head'
               END
        FROM facts
    $$;

-- The heads at the other end of the relations of the caller's tickets
-- (docs/adr/0005 D3): for each anchor — a ticket of the caller's team they
-- see, not deleted; any other id is passed over — its parent, its children and
-- its links in both directions, each other end found here and never passed
-- in, with what the caller sees of it (ticket_sight). A placeholder's key,
-- title, type and state are NULL; a deleted other end is absent. far_id is the
-- other end's id, which a caller keeps to itself: no answer shows a ticket's
-- id. The assignee only of an other end of the caller's own team they read. A
-- link's maker and an assignee are named as the users policy names a person to
-- the caller.
CREATE FUNCTION relation_heads(p_anchors uuid[], p_kinds text[])
    RETURNS TABLE (anchor_id uuid, relation text, link_id uuid, link_type link_type, outgoing boolean,
                   link_created_by uuid, link_created_by_username text, link_created_by_name text,
                   link_created_at timestamptz, far_id uuid, team_slug text, team_name text, project_key text,
                   number integer, title text, type ticket_type, state ticket_state, sight text,
                   assignee_id uuid, assignee_username text, assignee_name text)
    LANGUAGE plpgsql SECURITY DEFINER
    AS $$
#variable_conflict use_column
DECLARE
    prev text := NULLIF(current_setting('app.crossing', true), '');
BEGIN
    PERFORM set_config('app.crossing', 'head', true);
    RETURN QUERY
    WITH anchor AS (
        SELECT a.id, a.parent_id
        FROM tickets a
        WHERE a.id = ANY (p_anchors) AND a.tenant_id = app_tenant_id()
          AND a.deleted_at IS NULL
          AND app_ticket_visible(a.project_id, a.confidential, a.assignee_id, a.reporter_id) IS TRUE
          AND app_is_member()
    ), rel AS (
        SELECT an.id AS anchor, 'parent'::text AS kind, NULL::uuid AS lid, NULL::link_type AS ltype,
               NULL::boolean AS lout, NULL::uuid AS lby, NULL::timestamptz AS lat, an.parent_id AS far
        FROM anchor an
        WHERE an.parent_id IS NOT NULL AND 'parent' = ANY (p_kinds)
        UNION ALL
        SELECT an.id, 'child', NULL, NULL, NULL, NULL, NULL, c.id
        FROM anchor an
        JOIN tickets c ON c.parent_id = an.id
        WHERE 'child' = ANY (p_kinds)
        UNION ALL
        SELECT an.id, 'link', l.id, l.type, true, l.created_by, l.created_at, l.target_id
        FROM anchor an
        JOIN ticket_links l ON l.source_id = an.id
        WHERE 'link' = ANY (p_kinds)
        UNION ALL
        SELECT an.id, 'link', l.id, l.type, false, l.created_by, l.created_at, l.source_id
        FROM anchor an
        JOIN ticket_links l ON l.target_id = an.id
        WHERE 'link' = ANY (p_kinds)
    )
    SELECT r.anchor, r.kind, r.lid, r.ltype, r.lout, r.lby, u.username, u.display_name, r.lat,
           f.id, tn.slug, tn.name,
           CASE WHEN s.sight = 'placeholder' THEN NULL ELSE p.key END,
           CASE WHEN s.sight = 'placeholder' THEN NULL ELSE f.number END,
           CASE WHEN s.sight = 'placeholder' THEN NULL ELSE f.title END,
           CASE WHEN s.sight = 'placeholder' THEN NULL ELSE f.type END,
           CASE WHEN s.sight = 'placeholder' THEN NULL ELSE f.state END,
           s.sight,
           CASE WHEN own.yes THEN f.assignee_id END,
           CASE WHEN own.yes THEN au.username END,
           CASE WHEN own.yes THEN au.display_name END
    FROM rel r
    JOIN tickets f ON f.id = r.far
    JOIN tenants tn ON tn.id = f.tenant_id
    JOIN projects p ON p.tenant_id = f.tenant_id AND p.id = f.project_id
    CROSS JOIN LATERAL (SELECT ticket_sight(f.tenant_id, f.project_id, f.confidential, f.assignee_id,
                                            f.reporter_id) AS sight) s
    CROSS JOIN LATERAL (SELECT s.sight = 'sees' AND f.tenant_id = app_tenant_id() AS yes) own
    LEFT JOIN users u ON u.id = r.lby
    LEFT JOIN users au ON au.id = f.assignee_id
    WHERE f.deleted_at IS NULL;
    PERFORM set_config('app.crossing', coalesce(prev, ''), true);
END
$$;

-- A ticket the caller reads, by its canonical key, any team: what setting a
-- parent or a link asks of its other end (docs/adr/0008 D2, docs/adr/0012 D2).
-- One the caller does not read — no such team, project or number, deleted,
-- confidential and not admitted, of a team they hold no role in, of a
-- project restricted from them, outside a token's restriction — is no row,
-- exactly as one that does not exist.
CREATE FUNCTION readable_ticket(p_team_slug text, p_project_key text, p_number integer)
    RETURNS TABLE (id uuid, tenant_id uuid, team_slug text, team_name text, project_id uuid, project_key text,
                   number integer, title text, type ticket_type, state ticket_state)
    LANGUAGE plpgsql SECURITY DEFINER
    AS $$
#variable_conflict use_column
DECLARE
    prev text := NULLIF(current_setting('app.crossing', true), '');
BEGIN
    PERFORM set_config('app.crossing', 'head', true);
    RETURN QUERY
    SELECT t.id, t.tenant_id, tn.slug, tn.name, t.project_id, p.key, t.number, t.title, t.type, t.state
    FROM tenants tn
    JOIN projects p ON p.tenant_id = tn.id AND p.key = p_project_key
    JOIN tickets t ON t.tenant_id = p.tenant_id AND t.project_id = p.id AND t.number = p_number
    WHERE tn.slug = p_team_slug
      AND t.deleted_at IS NULL
      AND app_is_member()
      AND ticket_sight(t.tenant_id, t.project_id, t.confidential, t.assignee_id, t.reporter_id) = 'sees';
    PERFORM set_config('app.crossing', coalesce(prev, ''), true);
END
$$;

-- The prerequisite tree of a ticket of the caller's team they see, or read
-- upward its dependents (docs/adr/0012 D6), across teams: ListPrerequisites'
-- walk with every step decided by ticket_sight. The walk goes on only from a
-- ticket the caller reads; a head and a placeholder are leaves, so what lies
-- behind them stays behind their team's membership (docs/adr/0005 D3,
-- docs/adr/0065 D5). Assignee and progress only for a ticket of the caller's
-- own team they read. open_count counts the open tickets of the whole tree
-- whose state the caller reads, each once — never a placeholder.
CREATE FUNCTION prerequisite_heads(p_root uuid, p_up boolean, p_max_depth integer, p_after uuid[],
                                   p_page_size integer)
    RETURNS TABLE (depth integer, path uuid[], repeated boolean, team_slug text, team_name text, project_key text,
                   number integer, title text, type ticket_type, state ticket_state, blocked_from ticket_state,
                   sight text, own boolean, assignee_id uuid, assignee_username text, assignee_name text,
                   progress smallint, progress_derived smallint, progress_refinement smallint,
                   progress_refinement_derived smallint, progress_review smallint, progress_review_derived smallint,
                   open_count integer)
    LANGUAGE plpgsql SECURITY DEFINER
    AS $$
#variable_conflict use_column
DECLARE
    prev text := NULLIF(current_setting('app.crossing', true), '');
BEGIN
    PERFORM set_config('app.crossing', 'head', true);
    RETURN QUERY
    WITH RECURSIVE root AS (
        SELECT a.id
        FROM tickets a
        WHERE a.id = p_root AND a.tenant_id = app_tenant_id() AND a.deleted_at IS NULL
          AND app_ticket_visible(a.project_id, a.confidential, a.assignee_id, a.reporter_id) IS TRUE
          AND app_is_member()
    ), reach (id, via, depth, sees) AS (
        SELECT s.id, root.id, 1,
               ticket_sight(s.tenant_id, s.project_id, s.confidential, s.assignee_id, s.reporter_id) = 'sees'
        FROM root
        JOIN ticket_links l ON l.type = 'blocks'
             AND (CASE WHEN p_up THEN l.source_id ELSE l.target_id END) = root.id
        JOIN tickets s ON s.id = (CASE WHEN p_up THEN l.target_id ELSE l.source_id END)
        WHERE s.deleted_at IS NULL
        UNION
        SELECT s.id, reach.id, reach.depth + 1,
               ticket_sight(s.tenant_id, s.project_id, s.confidential, s.assignee_id, s.reporter_id) = 'sees'
        FROM reach
        JOIN ticket_links l ON l.type = 'blocks'
             AND (CASE WHEN p_up THEN l.source_id ELSE l.target_id END) = reach.id
        JOIN tickets s ON s.id = (CASE WHEN p_up THEN l.target_id ELSE l.source_id END)
        WHERE reach.sees AND reach.depth < p_max_depth AND s.id <> p_root AND s.deleted_at IS NULL
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
        SELECT nodes.depth, nodes.path, nodes.repeated, s.id, s.tenant_id, s.project_id, s.number, s.title, s.type,
               s.state, s.blocked_from, s.assignee_id, s.progress, s.progress_derived, s.progress_refinement,
               s.progress_refinement_derived, s.progress_review, s.progress_review_derived,
               ticket_sight(s.tenant_id, s.project_id, s.confidential, s.assignee_id, s.reporter_id) AS sight
        FROM nodes
        JOIN tickets s ON s.id = nodes.id
        WHERE s.deleted_at IS NULL
    ), counted AS (
        SELECT shown.*,
               count(*) FILTER (WHERE NOT shown.repeated AND shown.sight <> 'placeholder'
                                  AND shown.state NOT IN ('done', 'dropped')) OVER () AS open_count
        FROM shown
    )
    SELECT c.depth, c.path, c.repeated, tn.slug, tn.name,
           CASE WHEN c.sight = 'placeholder' THEN NULL ELSE p.key END,
           CASE WHEN c.sight = 'placeholder' THEN NULL ELSE c.number END,
           CASE WHEN c.sight = 'placeholder' THEN NULL ELSE c.title END,
           CASE WHEN c.sight = 'placeholder' THEN NULL ELSE c.type END,
           CASE WHEN c.sight = 'placeholder' THEN NULL ELSE c.state END,
           CASE WHEN c.sight = 'placeholder' THEN NULL ELSE c.blocked_from END,
           c.sight, own.yes,
           CASE WHEN own.yes THEN c.assignee_id END,
           CASE WHEN own.yes THEN u.username END,
           CASE WHEN own.yes THEN u.display_name END,
           CASE WHEN own.yes THEN c.progress END,
           CASE WHEN own.yes THEN c.progress_derived END,
           CASE WHEN own.yes THEN c.progress_refinement END,
           CASE WHEN own.yes THEN c.progress_refinement_derived END,
           CASE WHEN own.yes THEN c.progress_review END,
           CASE WHEN own.yes THEN c.progress_review_derived END,
           c.open_count::integer
    FROM counted c
    CROSS JOIN LATERAL (SELECT c.sight = 'sees' AND c.tenant_id = app_tenant_id() AS yes) own
    JOIN tenants tn ON tn.id = c.tenant_id
    JOIN projects p ON p.tenant_id = c.tenant_id AND p.id = c.project_id
    LEFT JOIN users u ON u.id = c.assignee_id
    WHERE p_after IS NULL OR c.path > p_after
    ORDER BY c.path
    LIMIT p_page_size;
    PERFORM set_config('app.crossing', coalesce(prev, ''), true);
END
$$;

-- The open tickets that block a ticket of the caller's team directly and
-- whose state the caller reads in a head — of any team, never a placeholder
-- (docs/adr/0012 D6, D7): the count on the card, the blocked filter and the
-- tickets the done act is refused over, one rule for all three.
CREATE FUNCTION open_prerequisite_count(p_ticket uuid) RETURNS integer
    LANGUAGE plpgsql SECURITY DEFINER
    AS $$
DECLARE
    prev text := NULLIF(current_setting('app.crossing', true), '');
    n integer;
BEGIN
    PERFORM set_config('app.crossing', 'head', true);
    SELECT count(*)::integer INTO n
    FROM tickets a
    JOIN ticket_links l ON l.target_id = a.id AND l.type = 'blocks'
    JOIN tickets s ON s.id = l.source_id
    WHERE a.id = p_ticket AND a.tenant_id = app_tenant_id() AND app_is_member()
      AND s.state NOT IN ('done', 'dropped')
      AND s.deleted_at IS NULL
      AND ticket_sight(s.tenant_id, s.project_id, s.confidential, s.assignee_id, s.reporter_id) <> 'placeholder';
    PERFORM set_config('app.crossing', coalesce(prev, ''), true);
    RETURN n;
END
$$;

-- The tickets of the caller's team that an open ticket blocks whose state the
-- caller reads in a head, of any team: the blocked filter of the ticket lists
-- (docs/adr/0049 D1), open_prerequisite_count's rule read once for a whole
-- list rather than once per ticket.
CREATE FUNCTION open_prerequisite_targets() RETURNS SETOF uuid
    LANGUAGE plpgsql SECURITY DEFINER
    AS $$
DECLARE
    prev text := NULLIF(current_setting('app.crossing', true), '');
BEGIN
    PERFORM set_config('app.crossing', 'head', true);
    RETURN QUERY
    SELECT DISTINCT a.id
    FROM tickets a
    JOIN ticket_links l ON l.target_id = a.id AND l.type = 'blocks'
    JOIN tickets s ON s.id = l.source_id
    WHERE a.tenant_id = app_tenant_id() AND a.deleted_at IS NULL AND app_is_member()
      AND s.state NOT IN ('done', 'dropped')
      AND s.deleted_at IS NULL
      AND ticket_sight(s.tenant_id, s.project_id, s.confidential, s.assignee_id, s.reporter_id) <> 'placeholder';
    PERFORM set_config('app.crossing', coalesce(prev, ''), true);
END
$$;

-- The open direct prerequisites of a ticket of the caller's team whose state
-- the caller reads, by their heads (docs/adr/0012 D7): what refuses done, and
-- what an override names. A placeholder neither shows nor refuses.
CREATE FUNCTION open_prerequisite_heads(p_ticket uuid)
    RETURNS TABLE (far_id uuid, team_slug text, team_name text, project_key text, number integer, title text,
                   type ticket_type, state ticket_state, sight text)
    LANGUAGE plpgsql SECURITY DEFINER
    AS $$
#variable_conflict use_column
DECLARE
    prev text := NULLIF(current_setting('app.crossing', true), '');
BEGIN
    PERFORM set_config('app.crossing', 'head', true);
    RETURN QUERY
    SELECT s.id, tn.slug, tn.name, p.key, s.number, s.title, s.type, s.state, h.sight
    FROM tickets a
    JOIN ticket_links l ON l.target_id = a.id AND l.type = 'blocks'
    JOIN tickets s ON s.id = l.source_id
    JOIN tenants tn ON tn.id = s.tenant_id
    JOIN projects p ON p.tenant_id = s.tenant_id AND p.id = s.project_id
    CROSS JOIN LATERAL (SELECT ticket_sight(s.tenant_id, s.project_id, s.confidential, s.assignee_id,
                                            s.reporter_id) AS sight) h
    WHERE a.id = p_ticket AND a.tenant_id = app_tenant_id() AND app_is_member()
      AND s.state NOT IN ('done', 'dropped')
      AND s.deleted_at IS NULL
      AND h.sight <> 'placeholder'
    ORDER BY tn.slug, p.key, s.number;
    PERFORM set_config('app.crossing', coalesce(prev, ''), true);
END
$$;

-- Whether p_ticket, a ticket of the caller's team, is p_candidate or one of
-- its ancestors, across teams: the parent cycle refusal (docs/adr/0008 D2).
-- An integrity walk, which answers yes or no only: it steps over deleted
-- tickets, so that a restoration never closes a cycle, and it reads past every
-- sight. The caller holds the installation's lock of the parent graph.
CREATE FUNCTION parent_chain_reaches(p_candidate uuid, p_ticket uuid) RETURNS boolean
    LANGUAGE plpgsql SECURITY DEFINER
    AS $$
DECLARE
    prev text := NULLIF(current_setting('app.crossing', true), '');
    reached boolean;
BEGIN
    PERFORM set_config('app.crossing', 'walk', true);
    IF NOT EXISTS (SELECT 1 FROM tickets WHERE id = p_ticket AND tenant_id = app_tenant_id()) THEN
        RAISE EXCEPTION 'parent_chain_reaches: % is no ticket of the team', p_ticket;
    END IF;
    WITH RECURSIVE ancestors (id, parent_id, depth) AS (
        SELECT t.id, t.parent_id, 1 FROM tickets t WHERE t.id = p_candidate
        UNION ALL
        SELECT t.id, t.parent_id, a.depth + 1
        FROM tickets t JOIN ancestors a ON t.id = a.parent_id
        WHERE a.depth < 100000
    )
    SELECT EXISTS (SELECT 1 FROM ancestors WHERE ancestors.id = p_ticket) INTO reached;
    PERFORM set_config('app.crossing', coalesce(prev, ''), true);
    RETURN reached;
END
$$;

-- Whether p_to, a ticket of the caller's team, is reachable from p_from over
-- blocks links, across teams: adding the link p_to blocks p_from would close a
-- cycle (docs/adr/0012 D4). Like the parent walk it answers yes or no only,
-- steps over deleted tickets and reads past every sight. The caller holds the
-- installation's lock of the blocks graph.
CREATE FUNCTION blocks_reach(p_from uuid, p_to uuid) RETURNS boolean
    LANGUAGE plpgsql SECURITY DEFINER
    AS $$
DECLARE
    prev text := NULLIF(current_setting('app.crossing', true), '');
    reached boolean;
BEGIN
    PERFORM set_config('app.crossing', 'walk', true);
    IF NOT EXISTS (SELECT 1 FROM tickets WHERE id = p_to AND tenant_id = app_tenant_id()) THEN
        RAISE EXCEPTION 'blocks_reach: % is no ticket of the team', p_to;
    END IF;
    WITH RECURSIVE reach (id) AS (
        SELECT p_from
        UNION
        SELECT l.target_id
        FROM ticket_links l JOIN reach r ON l.source_id = r.id
        WHERE l.type = 'blocks'
    )
    SELECT EXISTS (SELECT 1 FROM reach WHERE reach.id = p_to) INTO reached;
    PERFORM set_config('app.crossing', coalesce(prev, ''), true);
    RETURN reached;
END
$$;

-- The derived progress of parents after a change of their children
-- (docs/adr/0017 D3 as amended 2026-10-10): each starting parent and its
-- ancestors, across teams, the deepest first — a child's value before its
-- parent's, and every writer in the same order —, each stage the effort-
-- weighted mean of the same stage of every child of any team, dropped and
-- deleted ones left out, done counted as 100, rounded to five, as
-- ticket_derived_stage derives it within a team. It writes those columns and
-- nothing else — tickets_crossing_guard holds it to them — no version and no
-- act (docs/adr/0050 D1); when the last child has left, each stage's own value
-- starts at the last derived one, and a done ticket that gains children is
-- done by hand from then on. A deleted ticket is passed over, and so is what
-- lies above it: no parent counts it. A parent of another team that changed is
-- told on its team's streams as ticket.changed of the kind derived; nothing
-- of the change reaches the caller. It answers how many tickets changed.
CREATE FUNCTION refresh_derived(p_parents uuid[]) RETURNS integer
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

        UPDATE tickets t
        SET progress_derived = d.implementation,
            progress_refinement_derived = d.refinement,
            progress_review_derived = d.review,
            progress = CASE WHEN d.implementation IS NULL THEN coalesce(t.progress_derived, t.progress)
                            ELSE t.progress END,
            progress_refinement = CASE WHEN d.refinement IS NULL
                                       THEN coalesce(t.progress_refinement_derived, t.progress_refinement)
                                       ELSE t.progress_refinement END,
            progress_review = CASE WHEN d.review IS NULL
                                   THEN coalesce(t.progress_review_derived, t.progress_review)
                                   ELSE t.progress_review END,
            done_by_hand = t.done_by_hand OR (t.state = 'done' AND d.implementation IS NOT NULL),
            updated_at = now()
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

-- Where the acts of a relation are recorded in another team
-- (docs/adr/0012 D3: a link is an act on both tickets; docs/adr/0024 D2: a
-- change is recorded in the record of the team it changes;
-- docs/adr/0012 D5: the watchers of a blocked ticket are told when its
-- prerequisite settles): for a ticket of the caller's team, every relation
-- whose other end is a ticket of another team, with that ticket's team, id and
-- key, whatever the caller sees of it — the act is addressed to that team's
-- record and never shown to the caller — and its head as an outsider reads it,
-- which such an act names it by. It answers no head the caller is shown: the
-- store keeps what it reads to address the acts it records there.
CREATE FUNCTION relations_elsewhere(p_ticket uuid)
    RETURNS TABLE (relation text, link_id uuid, link_type link_type, outgoing boolean, far_tenant uuid, far_id uuid,
                   far_key text, far_confidential boolean, far_team_name text, far_title text, far_type ticket_type,
                   far_state ticket_state)
    LANGUAGE plpgsql SECURITY DEFINER
    AS $$
#variable_conflict use_column
DECLARE
    prev text := NULLIF(current_setting('app.crossing', true), '');
BEGIN
    PERFORM set_config('app.crossing', 'act', true);
    RETURN QUERY
    WITH anchor AS (
        SELECT a.id, a.parent_id FROM tickets a
        WHERE a.id = p_ticket AND a.tenant_id = app_tenant_id() AND app_is_member()
    ), rel AS (
        SELECT 'parent'::text AS kind, NULL::uuid AS lid, NULL::link_type AS ltype, NULL::boolean AS lout,
               an.parent_id AS far
        FROM anchor an WHERE an.parent_id IS NOT NULL
        UNION ALL
        SELECT 'child', NULL, NULL, NULL, c.id FROM anchor an JOIN tickets c ON c.parent_id = an.id
        UNION ALL
        SELECT 'link', l.id, l.type, true, l.target_id FROM anchor an JOIN ticket_links l ON l.source_id = an.id
        UNION ALL
        SELECT 'link', l.id, l.type, false, l.source_id FROM anchor an JOIN ticket_links l ON l.target_id = an.id
    )
    SELECT r.kind, r.lid, r.ltype, r.lout, f.tenant_id, f.id, tn.slug || '/' || p.key || '-' || f.number,
           f.confidential, tn.name, f.title, f.type, f.state
    FROM rel r
    JOIN tickets f ON f.id = r.far
    JOIN tenants tn ON tn.id = f.tenant_id
    JOIN projects p ON p.tenant_id = f.tenant_id AND p.id = f.project_id
    WHERE f.tenant_id <> app_tenant_id();
    PERFORM set_config('app.crossing', coalesce(prev, ''), true);
END
$$;

-- The end of a purged ticket's relations into other teams
-- (docs/adr/0024 D2 as made concrete 2026-10-10): its children of another
-- team become roots — their parent alone changes, no version, as the purge's
-- own children —, the links another team keeps to it go, and every other end
-- in another team is answered — those two, and the targets elsewhere of its
-- own team's links, which the purge deletes with the ticket's other rows — for
-- the purge to record each change in the record of the team it changes. Only
-- inside the purge of a deleted ticket of the transaction's team, as
-- purge_ticket_audit.
CREATE FUNCTION end_relations_elsewhere(p_ticket uuid)
    RETURNS TABLE (far_tenant uuid, far_id uuid, far_key text, relation text, link_id uuid, link_type link_type,
                   outgoing boolean)
    LANGUAGE plpgsql SECURITY DEFINER
    AS $$
#variable_conflict use_column
DECLARE
    prev text := NULLIF(current_setting('app.crossing', true), '');
BEGIN
    PERFORM set_config('app.crossing', 'purge', true);
    IF app_job() IS DISTINCT FROM 'ticket-purge' OR app_tenant_id() IS NULL THEN
        RAISE EXCEPTION 'end_relations_elsewhere runs inside the purge of a team''s ticket only';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM tickets t
                   WHERE t.id = p_ticket AND t.tenant_id = app_tenant_id() AND t.deleted_at IS NOT NULL) THEN
        RAISE EXCEPTION 'end_relations_elsewhere: % is no deleted ticket of the team', p_ticket;
    END IF;
    RETURN QUERY
    WITH detached AS (
        UPDATE tickets c SET parent_id = NULL
        WHERE c.parent_id = p_ticket AND c.tenant_id <> app_tenant_id()
        RETURNING c.tenant_id, c.id, c.project_id, c.number
    )
    SELECT d.tenant_id, d.id, tn.slug || '/' || p.key || '-' || d.number, 'child'::text, NULL::uuid,
           NULL::link_type, NULL::boolean
    FROM detached d
    JOIN tenants tn ON tn.id = d.tenant_id
    JOIN projects p ON p.tenant_id = d.tenant_id AND p.id = d.project_id;
    RETURN QUERY
    WITH gone AS (
        DELETE FROM ticket_links l
        WHERE l.target_id = p_ticket AND l.tenant_id <> app_tenant_id()
        RETURNING l.tenant_id, l.id, l.type, l.source_id
    )
    SELECT f.tenant_id, f.id, tn.slug || '/' || p.key || '-' || f.number, 'link'::text, g.id, g.type, true
    FROM gone g
    JOIN tickets f ON f.id = g.source_id
    JOIN tenants tn ON tn.id = f.tenant_id
    JOIN projects p ON p.tenant_id = f.tenant_id AND p.id = f.project_id;
    RETURN QUERY
    SELECT f.tenant_id, f.id, tn.slug || '/' || p.key || '-' || f.number, 'link'::text, l.id, l.type, false
    FROM ticket_links l
    JOIN tickets f ON f.id = l.target_id
    JOIN tenants tn ON tn.id = f.tenant_id
    JOIN projects p ON p.tenant_id = f.tenant_id AND p.id = f.project_id
    WHERE l.source_id = p_ticket AND l.tenant_id = app_tenant_id() AND f.tenant_id <> app_tenant_id();
    PERFORM set_config('app.crossing', coalesce(prev, ''), true);
END
$$;

-- The end of every relation between a team and the others, before the team is
-- deleted (docs/adr/0024 D6 as made concrete 2026-10-10): the children of
-- other teams whose parent is the team's become roots, the team's tickets whose
-- parent is elsewhere become roots, and the links between the team and the
-- others go, in both directions. It answers every ticket of another team the
-- change touched — a child, a link's other end, a parent whose derived
-- progress counts the team's tickets no more — with the id and the key of the
-- team's ticket at the other end, for the caller to record the acts in each other
-- team and to derive those parents again. Only in a transaction named
-- team-deletion with no team set; no route calls it while the deletion of a
-- team is not built.
CREATE FUNCTION end_team_relations(p_team uuid)
    RETURNS TABLE (far_tenant uuid, far_id uuid, far_key text, relation text, link_id uuid, link_type link_type,
                   outgoing boolean, near_id uuid, near_key text)
    LANGUAGE plpgsql SECURITY DEFINER
    AS $$
#variable_conflict use_column
DECLARE
    prev text := NULLIF(current_setting('app.crossing', true), '');
BEGIN
    PERFORM set_config('app.crossing', 'purge', true);
    IF app_job() IS DISTINCT FROM 'team-deletion' OR app_tenant_id() IS NOT NULL THEN
        RAISE EXCEPTION 'end_team_relations runs in the deletion of a team only';
    END IF;
    RETURN QUERY
    WITH detached AS (
        UPDATE tickets c SET parent_id = NULL
        FROM tickets up
        WHERE up.id = c.parent_id AND up.tenant_id = p_team AND c.tenant_id <> p_team
        RETURNING c.tenant_id, c.id, c.project_id, c.number, up.id AS near
    )
    SELECT d.tenant_id, d.id, tn.slug || '/' || p.key || '-' || d.number, 'child'::text, NULL::uuid,
           NULL::link_type, NULL::boolean, n.id, nt.slug || '/' || np.key || '-' || n.number
    FROM detached d
    JOIN tenants tn ON tn.id = d.tenant_id
    JOIN projects p ON p.tenant_id = d.tenant_id AND p.id = d.project_id
    JOIN tickets n ON n.id = d.near
    JOIN tenants nt ON nt.id = n.tenant_id
    JOIN projects np ON np.tenant_id = n.tenant_id AND np.id = n.project_id;
    RETURN QUERY
    WITH detached AS (
        UPDATE tickets c SET parent_id = NULL
        FROM tickets up
        WHERE up.id = c.parent_id AND c.tenant_id = p_team AND up.tenant_id <> p_team
        RETURNING up.tenant_id, up.id, up.project_id, up.number, c.id AS near
    )
    SELECT d.tenant_id, d.id, tn.slug || '/' || p.key || '-' || d.number, 'parent'::text, NULL::uuid,
           NULL::link_type, NULL::boolean, n.id, nt.slug || '/' || np.key || '-' || n.number
    FROM detached d
    JOIN tenants tn ON tn.id = d.tenant_id
    JOIN projects p ON p.tenant_id = d.tenant_id AND p.id = d.project_id
    JOIN tickets n ON n.id = d.near
    JOIN tenants nt ON nt.id = n.tenant_id
    JOIN projects np ON np.tenant_id = n.tenant_id AND np.id = n.project_id;
    RETURN QUERY
    WITH gone AS (
        DELETE FROM ticket_links l
        USING tickets tgt
        WHERE tgt.id = l.target_id
          AND ((tgt.tenant_id = p_team AND l.tenant_id <> p_team)
               OR (l.tenant_id = p_team AND tgt.tenant_id <> p_team))
        RETURNING l.id, l.type, l.tenant_id, l.source_id, l.target_id
    ), ends AS (
        SELECT g.id, g.type,
               g.tenant_id <> p_team AS far_is_source,
               CASE WHEN g.tenant_id <> p_team THEN g.source_id ELSE g.target_id END AS far,
               CASE WHEN g.tenant_id <> p_team THEN g.target_id ELSE g.source_id END AS near
        FROM gone g
    )
    SELECT f.tenant_id, f.id, tn.slug || '/' || p.key || '-' || f.number, 'link'::text, e.id, e.type,
           e.far_is_source, n.id, nt.slug || '/' || np.key || '-' || n.number
    FROM ends e
    JOIN tickets f ON f.id = e.far
    JOIN tenants tn ON tn.id = f.tenant_id
    JOIN projects p ON p.tenant_id = f.tenant_id AND p.id = f.project_id
    JOIN tickets n ON n.id = e.near
    JOIN tenants nt ON nt.id = n.tenant_id
    JOIN projects np ON np.tenant_id = n.tenant_id AND np.id = n.project_id;
    PERFORM set_config('app.crossing', coalesce(prev, ''), true);
END
$$;

-- What a crossing that writes may change of a ticket of any team, a rule on
-- columns a policy cannot state (docs/adr/0021 D6): the derived progress, the
-- stages it seeds, done by hand and updated_at; the end of a relation the
-- parent alone. Anything else is refused (SQLSTATE 42501). The generated
-- search column is computed after the trigger and compared by neither. Outside
-- such a crossing it lets every update through.
CREATE FUNCTION tickets_crossing_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE
    allowed text[];
BEGIN
    CASE app_crossing()
        WHEN 'derive' THEN
            allowed := ARRAY['progress_derived', 'progress_refinement_derived', 'progress_review_derived', 'progress',
                             'progress_refinement', 'progress_review', 'done_by_hand', 'updated_at', 'search'];
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
CREATE TRIGGER tickets_crossing_guard BEFORE UPDATE ON tickets
    FOR EACH ROW EXECUTE FUNCTION tickets_crossing_guard();

-- The crossings' policies: permissive, for the owner role alone — the role
-- that runs this file and owns the functions above —, each admitting what one
-- kind of crossing reads or writes while a function names it in app.crossing.
-- The runtime role is bound by none of them, whatever it sets. A start-up
-- check of `cowork serve` refuses a database whose crossing policies name
-- another role than the owner of the tables and the functions
-- (DB.CheckCrossing).
DO $$
DECLARE
    owner_role text := current_user;
BEGIN
    EXECUTE format('CREATE POLICY tickets_crossing_read ON tickets FOR SELECT TO %I '
                   'USING (app_crossing() IN (''head'', ''walk'', ''derive'', ''purge'', ''act''))', owner_role);
    EXECUTE format('CREATE POLICY tickets_crossing_derive ON tickets FOR UPDATE TO %I '
                   'USING (app_crossing() = ''derive'') WITH CHECK (app_crossing() = ''derive'')', owner_role);
    EXECUTE format('CREATE POLICY tickets_crossing_detach ON tickets FOR UPDATE TO %I '
                   'USING (app_crossing() = ''purge'') WITH CHECK (app_crossing() = ''purge'')', owner_role);
    EXECUTE format('CREATE POLICY projects_crossing_read ON projects FOR SELECT TO %I '
                   'USING (app_crossing() IN (''head'', ''derive'', ''purge'', ''act''))', owner_role);
    EXECUTE format('CREATE POLICY tenants_crossing_read ON tenants FOR SELECT TO %I '
                   'USING (app_crossing() IN (''head'', ''derive'', ''purge'', ''act''))', owner_role);
    EXECUTE format('CREATE POLICY project_access_crossing_read ON project_access FOR SELECT TO %I '
                   'USING (app_crossing() = ''head'')', owner_role);
    EXECUTE format('CREATE POLICY ticket_links_crossing_read ON ticket_links FOR SELECT TO %I '
                   'USING (app_crossing() IN (''head'', ''walk'', ''purge'', ''act''))', owner_role);
    EXECUTE format('CREATE POLICY ticket_links_crossing_delete ON ticket_links FOR DELETE TO %I '
                   'USING (app_crossing() = ''purge'')', owner_role);
END
$$;

-- The purge's function of migration 32, the same signature and the same
-- behaviour, now emptying app.crossing first: a value a caller left there
-- would otherwise reach the owner's policies inside it. Replacing a function
-- drops its settings, so its search_path is fixed again below.
CREATE OR REPLACE FUNCTION purge_ticket_audit(p_ticket_id uuid) RETURNS bigint
    LANGUAGE plpgsql SECURITY DEFINER
    AS $$
DECLARE
    emptied bigint;
BEGIN
    PERFORM set_config('app.crossing', '', true);
    IF app_job() IS DISTINCT FROM 'ticket-purge' OR app_tenant_id() IS NULL THEN
        RAISE EXCEPTION 'purge_ticket_audit runs inside the purge of a tenant''s ticket only';
    END IF;
    IF NOT app_ticket_deleted(app_tenant_id(), p_ticket_id) THEN
        RAISE EXCEPTION 'purge_ticket_audit: % is no deleted ticket of the tenant', p_ticket_id;
    END IF;
    UPDATE audit_events
    SET before = NULL, after = NULL, reason = NULL, note = NULL
    WHERE tenant_id = app_tenant_id() AND ticket_id = p_ticket_id
      AND (before IS NOT NULL OR after IS NOT NULL OR reason IS NOT NULL OR note IS NOT NULL);
    GET DIAGNOSTICS emptied = ROW_COUNT;
    RETURN emptied;
END
$$;

-- A function that runs with its owner's rights resolves names on a fixed path:
-- the schema this file creates its objects in, then pg_temp last, so that no
-- object a caller creates stands in for one the function names. Only the
-- runtime role executes the crossings, and nobody but them ticket_sight.
DO $$
DECLARE
    runtime text := current_setting('cowork.runtime_role');
BEGIN
    EXECUTE format('ALTER FUNCTION purge_ticket_audit(uuid) SET search_path = %I, pg_temp', current_schema());
    EXECUTE format('ALTER FUNCTION relation_heads(uuid[], text[]) SET search_path = %I, pg_temp', current_schema());
    EXECUTE format('ALTER FUNCTION readable_ticket(text, text, integer) SET search_path = %I, pg_temp', current_schema());
    EXECUTE format('ALTER FUNCTION prerequisite_heads(uuid, boolean, integer, uuid[], integer) SET search_path = %I, pg_temp',
                   current_schema());
    EXECUTE format('ALTER FUNCTION open_prerequisite_count(uuid) SET search_path = %I, pg_temp', current_schema());
    EXECUTE format('ALTER FUNCTION open_prerequisite_targets() SET search_path = %I, pg_temp', current_schema());
    EXECUTE format('ALTER FUNCTION open_prerequisite_heads(uuid) SET search_path = %I, pg_temp', current_schema());
    EXECUTE format('ALTER FUNCTION parent_chain_reaches(uuid, uuid) SET search_path = %I, pg_temp', current_schema());
    EXECUTE format('ALTER FUNCTION blocks_reach(uuid, uuid) SET search_path = %I, pg_temp', current_schema());
    EXECUTE format('ALTER FUNCTION refresh_derived(uuid[]) SET search_path = %I, pg_temp', current_schema());
    EXECUTE format('ALTER FUNCTION relations_elsewhere(uuid) SET search_path = %I, pg_temp', current_schema());
    EXECUTE format('ALTER FUNCTION end_relations_elsewhere(uuid) SET search_path = %I, pg_temp', current_schema());
    EXECUTE format('ALTER FUNCTION end_team_relations(uuid) SET search_path = %I, pg_temp', current_schema());
    EXECUTE 'REVOKE ALL ON FUNCTION ticket_sight(uuid, uuid, boolean, uuid, uuid) FROM PUBLIC';
    EXECUTE 'REVOKE ALL ON FUNCTION relation_heads(uuid[], text[]) FROM PUBLIC';
    EXECUTE 'REVOKE ALL ON FUNCTION readable_ticket(text, text, integer) FROM PUBLIC';
    EXECUTE 'REVOKE ALL ON FUNCTION prerequisite_heads(uuid, boolean, integer, uuid[], integer) FROM PUBLIC';
    EXECUTE 'REVOKE ALL ON FUNCTION open_prerequisite_count(uuid) FROM PUBLIC';
    EXECUTE 'REVOKE ALL ON FUNCTION open_prerequisite_targets() FROM PUBLIC';
    EXECUTE 'REVOKE ALL ON FUNCTION open_prerequisite_heads(uuid) FROM PUBLIC';
    EXECUTE 'REVOKE ALL ON FUNCTION parent_chain_reaches(uuid, uuid) FROM PUBLIC';
    EXECUTE 'REVOKE ALL ON FUNCTION blocks_reach(uuid, uuid) FROM PUBLIC';
    EXECUTE 'REVOKE ALL ON FUNCTION refresh_derived(uuid[]) FROM PUBLIC';
    EXECUTE 'REVOKE ALL ON FUNCTION relations_elsewhere(uuid) FROM PUBLIC';
    EXECUTE 'REVOKE ALL ON FUNCTION end_relations_elsewhere(uuid) FROM PUBLIC';
    EXECUTE 'REVOKE ALL ON FUNCTION end_team_relations(uuid) FROM PUBLIC';
    EXECUTE format('GRANT EXECUTE ON FUNCTION relation_heads(uuid[], text[]) TO %I', runtime);
    EXECUTE format('GRANT EXECUTE ON FUNCTION readable_ticket(text, text, integer) TO %I', runtime);
    EXECUTE format('GRANT EXECUTE ON FUNCTION prerequisite_heads(uuid, boolean, integer, uuid[], integer) TO %I', runtime);
    EXECUTE format('GRANT EXECUTE ON FUNCTION open_prerequisite_count(uuid) TO %I', runtime);
    EXECUTE format('GRANT EXECUTE ON FUNCTION open_prerequisite_targets() TO %I', runtime);
    EXECUTE format('GRANT EXECUTE ON FUNCTION open_prerequisite_heads(uuid) TO %I', runtime);
    EXECUTE format('GRANT EXECUTE ON FUNCTION parent_chain_reaches(uuid, uuid) TO %I', runtime);
    EXECUTE format('GRANT EXECUTE ON FUNCTION blocks_reach(uuid, uuid) TO %I', runtime);
    EXECUTE format('GRANT EXECUTE ON FUNCTION refresh_derived(uuid[]) TO %I', runtime);
    EXECUTE format('GRANT EXECUTE ON FUNCTION relations_elsewhere(uuid) TO %I', runtime);
    EXECUTE format('GRANT EXECUTE ON FUNCTION end_relations_elsewhere(uuid) TO %I', runtime);
    EXECUTE format('GRANT EXECUTE ON FUNCTION end_team_relations(uuid) TO %I', runtime);
END
$$;
