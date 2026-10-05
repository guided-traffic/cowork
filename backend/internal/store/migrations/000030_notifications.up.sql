-- The inbox (docs/adr/0020): a notification per person and act, written in the
-- act's transaction, read and unread, rendered from the act it references
-- (D3) and kept ninety days once read (D6).

-- Marking a notification read is an act of its own (docs/adr/0026 D1). A new
-- enum value cannot be used in the transaction that adds it; nothing in this
-- file uses it.
ALTER TYPE audit_action ADD VALUE IF NOT EXISTS 'read';

-- Why a person is told (docs/adr/0020 D2): a ticket was assigned to them, a
-- question asked of them, a question they asked answered; a ticket they watch
-- changed state or got a comment; a ticket that blocks one they watch reached
-- done or dropped; an urgent stake was registered on a ticket assigned to them.
CREATE TYPE notification_reason AS ENUM (
    'assigned', 'asked', 'answered', 'state_changed', 'blocker_closed', 'commented', 'urgent'
);

-- A notification names its person, the ticket it is about and the act it
-- renders from (D3). For blocker_closed the act is on the ticket that blocked
-- this one; for every other reason it is on this ticket.
CREATE TABLE notifications (
    id             uuid                PRIMARY KEY DEFAULT uuidv7(),
    tenant_id      uuid                NOT NULL REFERENCES tenants (id),
    user_id        uuid                NOT NULL REFERENCES users (id),
    ticket_id      uuid                NOT NULL,
    audit_event_id uuid                NOT NULL REFERENCES audit_events (id),
    reason         notification_reason NOT NULL,
    read_at        timestamptz,
    created_at     timestamptz         NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, ticket_id) REFERENCES tickets (tenant_id, id)
);
CREATE INDEX notifications_by_person ON notifications (tenant_id, user_id, id);
CREATE INDEX notifications_unread    ON notifications (tenant_id, user_id) WHERE read_at IS NULL;
CREATE INDEX notifications_read      ON notifications (read_at) WHERE read_at IS NOT NULL;

-- The tenant's rows in the tenant; of those, a person reads and marks only their
-- own — the writer of an act inserts them for others, and a forgotten filter
-- must not show one person another's inbox. The retention job, which names no
-- tenant, reads and deletes the read ones; nothing else deletes.
ALTER TABLE notifications ENABLE ROW LEVEL SECURITY;
ALTER TABLE notifications FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON notifications
    USING (tenant_id = app_tenant_id())
    WITH CHECK (tenant_id = app_tenant_id());
CREATE POLICY notifications_expiry ON notifications FOR SELECT
    USING (app_job() = 'notification-expiry');
CREATE POLICY notifications_expiry_delete ON notifications FOR DELETE
    USING (app_job() = 'notification-expiry');
CREATE POLICY notifications_own_read ON notifications AS RESTRICTIVE FOR SELECT
    USING (user_id = app_user_id() OR app_job() = 'notification-expiry');
CREATE POLICY notifications_own_update ON notifications AS RESTRICTIVE FOR UPDATE
    USING (user_id = app_user_id())
    WITH CHECK (user_id = app_user_id());
CREATE POLICY notifications_job_delete ON notifications AS RESTRICTIVE FOR DELETE
    USING (app_job() = 'notification-expiry');

-- Whether a person — not necessarily the caller — sees a ticket: a member of
-- the tenant; the project unrestricted, or the person an administrator or on
-- its list (docs/adr/0034 D3); the ticket not confidential, or the person an
-- administrator, its assignee or its reporter (docs/adr/0065 D4). A question is
-- asked only of such a person, and an act tells only such persons
-- (docs/adr/0020 D2). It reads past the caller's own predicate, as it must, and
-- answers yes or no; row-level security still holds it to the tenant.
CREATE FUNCTION person_sees_ticket(p_tenant_id uuid, p_ticket_id uuid, p_user_id uuid) RETURNS boolean
    LANGUAGE sql STABLE PARALLEL SAFE
    AS $$
        SELECT EXISTS (
            SELECT 1
            FROM tickets t
            JOIN projects p ON p.tenant_id = t.tenant_id AND p.id = t.project_id
            JOIN memberships m ON m.tenant_id = t.tenant_id AND m.user_id = p_user_id
            WHERE t.tenant_id = p_tenant_id AND t.id = p_ticket_id
              AND (NOT p.restricted OR m.role = 'admin'
                   OR EXISTS (SELECT 1 FROM project_access a
                              WHERE a.tenant_id = t.tenant_id AND a.project_id = p.id AND a.user_id = m.user_id))
              AND (NOT t.confidential OR m.role = 'admin' OR t.assignee_id = m.user_id OR t.reporter_id = m.user_id))
    $$;

DO $$
DECLARE
    runtime text := current_setting('cowork.runtime_role');
BEGIN
    EXECUTE format('GRANT SELECT, INSERT, DELETE ON notifications TO %I', runtime);
    EXECUTE format('GRANT UPDATE (read_at) ON notifications TO %I', runtime);
END
$$;
