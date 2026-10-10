-- A head of the prerequisite tree is five fields — the team, the key, the
-- title, the type and the state — and nothing more (docs/adr/0005 D3): the
-- state a blocked ticket came from is shown only for a ticket the reader
-- reads. Migration 47 showed it for every node but a placeholder, so a reader
-- who holds no role in a node's team read where its blocked ticket had stood.
--
-- Expand only (docs/adr/0028 D3): one function replaced, same signature; the
-- release before does not call it.

-- The prerequisite tree of a ticket of the caller's team they see, or read
-- upward its dependents (docs/adr/0012 D6), across teams: ListPrerequisites'
-- walk with every step decided by ticket_sight. The walk goes on only from a
-- ticket the caller reads; a head and a placeholder are leaves, so what lies
-- behind them stays behind their team's membership (docs/adr/0005 D3,
-- docs/adr/0065 D5). Assignee and progress only for a ticket of the caller's
-- own team they read; the state a blocked ticket came from only for a ticket
-- the caller reads, a head being its team, key, title, type and state alone.
-- open_count counts the open tickets of the whole tree whose state the caller
-- reads, each once — never a placeholder.
CREATE OR REPLACE FUNCTION prerequisite_heads(p_root uuid, p_up boolean, p_max_depth integer, p_after uuid[],
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
           CASE WHEN c.sight = 'sees' THEN c.blocked_from END,
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

-- Replacing a function drops its settings: prerequisite_heads resolves names
-- on the fixed path again, the schema and then pg_temp last. Its grants stay.
DO $$
BEGIN
    EXECUTE format('ALTER FUNCTION prerequisite_heads(uuid, boolean, integer, uuid[], integer) SET search_path = %I, pg_temp',
                   current_schema());
END
$$;
