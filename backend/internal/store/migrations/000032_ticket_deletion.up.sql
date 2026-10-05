-- Deleting a ticket (docs/adr/0024 D1–D3, D7): a tenant administrator's soft
-- deletion, the bin that restores, and the purge — a job thirty days later, or
-- an administrator's explicit act — that removes the ticket and what hangs off
-- it for good, keeping its audit rows with their content emptied
-- (docs/adr/0026 D3).
--
-- Expand only (docs/adr/0028 D3): two nullable columns the previous release
-- never writes, grants and policies it never meets, and two functions replaced
-- by bodies that answer as before for every ticket that release can see — it
-- deletes nothing, so no deleted ticket exists while it runs. Its queries do
-- not know the column: run over this schema in a rollback (D4) it shows a
-- deleted ticket again until this release is back, and its purge does not
-- exist.

-- The marker. Soft deletion is an application filter, not a policy (D3):
-- every query of the data layer carries deleted_at IS NULL beside the
-- visibility predicate, and a unit test holds them to it; the bin and the
-- purge are the queries that invert it.
ALTER TABLE tickets
    ADD COLUMN deleted_at timestamptz,
    ADD COLUMN deleted_by uuid REFERENCES users (id),
    ADD CONSTRAINT tickets_deleted_check CHECK ((deleted_at IS NULL) = (deleted_by IS NULL));
CREATE INDEX tickets_deleted ON tickets (tenant_id, deleted_at) WHERE deleted_at IS NOT NULL;

-- Whether a ticket of the current tenant is deleted: what the purge's policies
-- below ask of the rows they let go. It reads the tenant's tickets past the
-- visibility predicate, as an integrity check does, and answers yes or no;
-- row-level security still holds it to the tenant.
CREATE FUNCTION app_ticket_deleted(p_tenant_id uuid, p_ticket_id uuid) RETURNS boolean
    LANGUAGE sql STABLE PARALLEL SAFE
    AS $$
        SELECT EXISTS (SELECT 1 FROM tickets t
                       WHERE t.tenant_id = p_tenant_id AND t.id = p_ticket_id AND t.deleted_at IS NOT NULL)
    $$;

-- The purge job reads, with no tenant set, the deleted tickets of every
-- tenant whose thirty days have passed (docs/adr/0024 D2, docs/adr/0027 D5);
-- then it purges them tenant by tenant, each with its tenant set. A request's
-- purge always has a tenant, so this policy never widens what it reads.
CREATE POLICY tickets_purge_due ON tickets FOR SELECT
    USING (app_job() = 'ticket-purge' AND app_tenant_id() IS NULL AND deleted_at IS NOT NULL);

-- Nothing but the purge deletes a ticket or what belongs only to it, and the
-- purge only what belongs to a deleted ticket: a restrictive policy is ANDed
-- with the permissive ones, so a DELETE outside the purge — a forgotten WHERE
-- included — removes nothing. The purge names itself in app.job for its part
-- of the transaction, the job ticket-purge and an administrator's explicit
-- purge alike. Links and stakes keep the deletes their own routes make.
CREATE POLICY tickets_purge ON tickets AS RESTRICTIVE FOR DELETE
    USING (app_job() = 'ticket-purge' AND deleted_at IS NOT NULL);
CREATE POLICY questions_purge ON questions AS RESTRICTIVE FOR DELETE
    USING (app_job() = 'ticket-purge' AND app_ticket_deleted(tenant_id, ticket_id));
CREATE POLICY comments_purge ON comments AS RESTRICTIVE FOR DELETE
    USING (app_job() = 'ticket-purge' AND app_ticket_deleted(tenant_id, ticket_id));
CREATE POLICY comment_revisions_purge ON comment_revisions AS RESTRICTIVE FOR DELETE
    USING (app_job() = 'ticket-purge'
           AND EXISTS (SELECT 1 FROM comments c
                       WHERE c.tenant_id = comment_revisions.tenant_id AND c.id = comment_revisions.comment_id
                         AND app_ticket_deleted(c.tenant_id, c.ticket_id)));
CREATE POLICY attachments_purge ON attachments AS RESTRICTIVE FOR DELETE
    USING (app_job() = 'ticket-purge' AND app_ticket_deleted(tenant_id, ticket_id));
CREATE POLICY time_entries_purge ON time_entries AS RESTRICTIVE FOR DELETE
    USING (app_job() = 'ticket-purge' AND app_ticket_deleted(tenant_id, ticket_id));
CREATE POLICY time_entry_revisions_purge ON time_entry_revisions AS RESTRICTIVE FOR DELETE
    USING (app_job() = 'ticket-purge'
           AND EXISTS (SELECT 1 FROM time_entries e
                       WHERE e.tenant_id = time_entry_revisions.tenant_id AND e.id = time_entry_revisions.entry_id
                         AND app_ticket_deleted(e.tenant_id, e.ticket_id)));

-- A notification is deleted by its retention job (migration 30) and by the
-- purge: those about the purged ticket, and those whose act is on it. A DELETE
-- with a WHERE must pass the SELECT policies as well, so the read of the purge
-- is admitted beside the person's own — for a deleted ticket's notifications
-- only.
ALTER POLICY notifications_own_read ON notifications
    USING (user_id = app_user_id() OR app_job() = 'notification-expiry'
           OR (app_job() = 'ticket-purge' AND app_ticket_deleted(tenant_id, ticket_id))
           OR (app_job() = 'ticket-purge'
               AND EXISTS (SELECT 1 FROM audit_events a
                           WHERE a.tenant_id = notifications.tenant_id AND a.id = notifications.audit_event_id
                             AND app_ticket_deleted(a.tenant_id, a.ticket_id))));
ALTER POLICY notifications_job_delete ON notifications
    USING (app_job() = 'notification-expiry'
           OR (app_job() = 'ticket-purge' AND app_ticket_deleted(tenant_id, ticket_id))
           OR (app_job() = 'ticket-purge'
               AND EXISTS (SELECT 1 FROM audit_events a
                           WHERE a.tenant_id = notifications.tenant_id AND a.id = notifications.audit_event_id
                             AND app_ticket_deleted(a.tenant_id, a.ticket_id))));

-- The audit rows survive the purge with their content emptied: the key, the
-- actor, the act and the time stay (docs/adr/0024 D2). The runtime role may
-- only insert and read audit rows (docs/adr/0026 D3), so the emptying is this
-- function's, owned by the owner role that runs this file and executed with
-- its rights: it empties before, after, reason and note of the current
-- tenant's rows of one deleted ticket, inside the purge only, and nothing
-- else. The policy below admits that update to the owner, the only role with
-- the privilege; the act of the purge itself is recorded by the transaction
-- that calls it, which commits both or neither.
CREATE POLICY audit_purge ON audit_events FOR UPDATE
    USING (tenant_id = app_tenant_id() AND app_job() = 'ticket-purge')
    WITH CHECK (tenant_id = app_tenant_id());

CREATE FUNCTION purge_ticket_audit(p_ticket_id uuid) RETURNS bigint
    LANGUAGE plpgsql SECURITY DEFINER
    AS $$
DECLARE
    emptied bigint;
BEGIN
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
-- object a caller creates stands in for one the function names.
DO $$
BEGIN
    EXECUTE format('ALTER FUNCTION purge_ticket_audit(uuid) SET search_path = %I, pg_temp', current_schema());
END
$$;

-- A deleted ticket leaves the derived values of what it belonged to
-- (docs/adr/0024 D1): its parent's stages count its other children only, and
-- nobody is told of it. Both bodies are those of migrations 19 and 30 with the
-- marker added.
CREATE OR REPLACE FUNCTION ticket_derived_stage(p_tenant_id uuid, p_id uuid, p_stage text) RETURNS smallint
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
        WHERE c.tenant_id = p_tenant_id AND c.parent_id = p_id AND c.deleted_at IS NULL
    $$;

CREATE OR REPLACE FUNCTION person_sees_ticket(p_tenant_id uuid, p_ticket_id uuid, p_user_id uuid) RETURNS boolean
    LANGUAGE sql STABLE PARALLEL SAFE
    AS $$
        SELECT EXISTS (
            SELECT 1
            FROM tickets t
            JOIN projects p ON p.tenant_id = t.tenant_id AND p.id = t.project_id
            JOIN memberships m ON m.tenant_id = t.tenant_id AND m.user_id = p_user_id
            WHERE t.tenant_id = p_tenant_id AND t.id = p_ticket_id AND t.deleted_at IS NULL
              AND (NOT p.restricted OR m.role = 'admin'
                   OR EXISTS (SELECT 1 FROM project_access a
                              WHERE a.tenant_id = t.tenant_id AND a.project_id = p.id AND a.user_id = m.user_id))
              AND (NOT t.confidential OR m.role = 'admin' OR t.assignee_id = m.user_id OR t.reporter_id = m.user_id))
    $$;

DO $$
DECLARE
    runtime text := current_setting('cowork.runtime_role');
BEGIN
    EXECUTE format('GRANT UPDATE (deleted_at, deleted_by) ON tickets TO %I', runtime);
    EXECUTE format('GRANT DELETE ON tickets, questions, comments, comment_revisions, attachments, '
                   'time_entries, time_entry_revisions TO %I', runtime);
    EXECUTE 'REVOKE ALL ON FUNCTION purge_ticket_audit(uuid) FROM PUBLIC';
    EXECUTE format('GRANT EXECUTE ON FUNCTION purge_ticket_audit(uuid) TO %I', runtime);
END
$$;
