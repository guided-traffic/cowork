-- Time booked by people against tickets, in minutes (docs/adr/0017 D6–D10).

-- An entry is corrected or voided, never deleted (docs/adr/0017 D7): no
-- grant allows DELETE. person_id is whose time it is, author_id who wrote
-- it down; the API writes a person's own time only.
CREATE TABLE time_entries (
    id         uuid        PRIMARY KEY DEFAULT uuidv7(),
    tenant_id  uuid        NOT NULL,
    ticket_id  uuid        NOT NULL,
    person_id  uuid        NOT NULL REFERENCES users (id),
    author_id  uuid        NOT NULL REFERENCES users (id),
    minutes    integer     NOT NULL CHECK (minutes BETWEEN 1 AND 1440),
    day        date        NOT NULL,
    note       text        NOT NULL DEFAULT '' CHECK (length(note) <= 2000),
    voided_by  uuid        REFERENCES users (id),
    voided_at  timestamptz,
    version    integer     NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, ticket_id) REFERENCES tickets (tenant_id, id),
    CHECK ((voided_by IS NULL) = (voided_at IS NULL))
);
CREATE INDEX time_entries_by_ticket ON time_entries (tenant_id, ticket_id, id);
CREATE INDEX time_entries_by_day    ON time_entries (tenant_id, day);
CREATE INDEX time_entries_by_person ON time_entries (tenant_id, person_id, day);

-- The values before each edit (docs/adr/0017 D7).
CREATE TABLE time_entry_revisions (
    id         uuid        PRIMARY KEY DEFAULT uuidv7(),
    tenant_id  uuid        NOT NULL,
    entry_id   uuid        NOT NULL,
    minutes    integer     NOT NULL,
    day        date        NOT NULL,
    note       text        NOT NULL,
    edited_by  uuid        NOT NULL REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (tenant_id, entry_id) REFERENCES time_entries (tenant_id, id)
);
CREATE INDEX time_entry_revisions_by_entry ON time_entry_revisions (tenant_id, entry_id, id);

ALTER TABLE time_entries ENABLE ROW LEVEL SECURITY;
ALTER TABLE time_entries FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON time_entries
    USING (tenant_id = app_tenant_id())
    WITH CHECK (tenant_id = app_tenant_id());

ALTER TABLE time_entry_revisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE time_entry_revisions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON time_entry_revisions
    USING (tenant_id = app_tenant_id())
    WITH CHECK (tenant_id = app_tenant_id());

-- Whose time the caller sees, besides the ticket's own predicate
-- (docs/adr/0034 D5, docs/adr/0017 D9): their own, every entry as tenant
-- administrator, and as member everyone's while the tenant shows time to
-- members. Every time query calls it next to app_ticket_visible.
CREATE FUNCTION app_time_visible(p_person_id uuid) RETURNS boolean
    LANGUAGE sql STABLE PARALLEL SAFE
    AS $$
        SELECT p_person_id = app_user_id()
            OR app_is_tenant_admin()
            OR EXISTS (SELECT 1
                       FROM tenants tn
                       JOIN memberships m ON m.tenant_id = tn.id
                       WHERE tn.id = app_tenant_id() AND tn.time_visible_to_members
                         AND m.user_id = app_user_id() AND m.role = 'member')
    $$;

DO $$
DECLARE
    runtime text := current_setting('cowork.runtime_role');
BEGIN
    EXECUTE format('GRANT SELECT, INSERT ON time_entries TO %I', runtime);
    EXECUTE format('GRANT UPDATE (minutes, day, note, voided_by, voided_at, version, updated_at) '
                   'ON time_entries TO %I', runtime);
    EXECUTE format('GRANT SELECT, INSERT ON time_entry_revisions TO %I', runtime);
END
$$;
