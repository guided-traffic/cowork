-- A writer of either end removes a relation that crosses teams
-- (docs/adr/0008 D2, docs/adr/0012 D2 as amended by the owner 2026-10-10): a
-- member or administrator of the parent's team detaches a child of another
-- team from it, and one of a link target's team removes a link another team
-- keeps to it — whether or not they read the other end. The row they end lives
-- in the other team, past the runtime role's policies, so the removal is one
-- more crossing of the owner role (docs/adr/0021 D7), of the kind purge — the
-- end of a relation —, whose policies and guard admit exactly that: the
-- parent of a ticket of any team cleared, and the delete of a link row.
--
-- Expand only (docs/adr/0028 D3): an audit action and a function added; the
-- release before neither records the action nor calls the function.

-- The act recorded on a parent when a child leaves it from the parent's side,
-- or from the child's side where the parent is of another team
-- (docs/adr/0026 D1). A new enum value cannot be used in the transaction that
-- adds it; nothing in this file uses it.
ALTER TYPE audit_action ADD VALUE IF NOT EXISTS 'detached';

-- The end of one relation of a ticket of the caller's team into another team,
-- by a writer of that ticket (the API holds the role): for p_kind child, the
-- ticket p_other of another team leaves p_anchor as its parent — its parent
-- alone changes, no version, as at a purge —; for p_kind link, the link
-- p_other another team keeps with p_anchor as its target goes. It ends nothing
-- else: the anchor must be a ticket of the caller's team they see, not
-- deleted; the caller a member or an administrator of the team — a viewer
-- never ends a relation here, whatever the API decides, and a restriction of
-- the anchor's project, which only lowers the role, is the API's to apply —;
-- and the row the relation of that anchor — the child's parent the anchor, the
-- link's target the anchor —, the other end not deleted, a deleted ticket
-- answering like a missing one. It answers the other end — its team, id and
-- key — and a link's type, where the act is recorded in that team's record; no
-- row where it ended nothing.
CREATE FUNCTION end_relation(p_anchor uuid, p_kind text, p_other uuid)
    RETURNS TABLE (far_tenant uuid, far_id uuid, far_key text, link_type link_type)
    LANGUAGE plpgsql SECURITY DEFINER
    AS $$
#variable_conflict use_column
DECLARE
    prev text := NULLIF(current_setting('app.crossing', true), '');
BEGIN
    PERFORM set_config('app.crossing', 'purge', true);
    IF p_kind = 'child' THEN
        RETURN QUERY
        WITH anchor AS (
            SELECT a.id FROM tickets a
            WHERE a.id = p_anchor AND a.tenant_id = app_tenant_id() AND a.deleted_at IS NULL
              AND app_ticket_visible(a.project_id, a.confidential, a.assignee_id, a.reporter_id) IS TRUE
              AND EXISTS (SELECT 1 FROM memberships m WHERE m.tenant_id = app_tenant_id() AND m.user_id = app_user_id()
                            AND m.role IN ('member', 'admin'))
        ), ended AS (
            UPDATE tickets c SET parent_id = NULL
            FROM anchor
            WHERE c.id = p_other AND c.parent_id = anchor.id AND c.tenant_id <> app_tenant_id() AND c.deleted_at IS NULL
            RETURNING c.tenant_id, c.id, c.project_id, c.number
        )
        SELECT e.tenant_id, e.id, tn.slug || '/' || p.key || '-' || e.number, NULL::link_type
        FROM ended e
        JOIN tenants tn ON tn.id = e.tenant_id
        JOIN projects p ON p.tenant_id = e.tenant_id AND p.id = e.project_id;
    ELSIF p_kind = 'link' THEN
        RETURN QUERY
        WITH anchor AS (
            SELECT a.id FROM tickets a
            WHERE a.id = p_anchor AND a.tenant_id = app_tenant_id() AND a.deleted_at IS NULL
              AND app_ticket_visible(a.project_id, a.confidential, a.assignee_id, a.reporter_id) IS TRUE
              AND EXISTS (SELECT 1 FROM memberships m WHERE m.tenant_id = app_tenant_id() AND m.user_id = app_user_id()
                            AND m.role IN ('member', 'admin'))
        ), gone AS (
            DELETE FROM ticket_links l
            USING anchor, tickets s
            WHERE l.id = p_other AND l.target_id = anchor.id AND l.tenant_id <> app_tenant_id()
              AND s.id = l.source_id AND s.deleted_at IS NULL
            RETURNING l.tenant_id, l.source_id, l.type
        )
        SELECT g.tenant_id, f.id, tn.slug || '/' || p.key || '-' || f.number, g.type
        FROM gone g
        JOIN tickets f ON f.id = g.source_id
        JOIN tenants tn ON tn.id = f.tenant_id
        JOIN projects p ON p.tenant_id = f.tenant_id AND p.id = f.project_id;
    END IF;
    PERFORM set_config('app.crossing', coalesce(prev, ''), true);
END
$$;

-- A function that runs with its owner's rights resolves names on a fixed path,
-- the schema and then pg_temp last; only the runtime role executes it.
DO $$
DECLARE
    runtime text := current_setting('cowork.runtime_role');
BEGIN
    EXECUTE format('ALTER FUNCTION end_relation(uuid, text, uuid) SET search_path = %I, pg_temp', current_schema());
    EXECUTE 'REVOKE ALL ON FUNCTION end_relation(uuid, text, uuid) FROM PUBLIC';
    EXECUTE format('GRANT EXECUTE ON FUNCTION end_relation(uuid, text, uuid) TO %I', runtime);
END
$$;
