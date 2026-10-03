-- Projects, the per-person list of a restricted project, and the counter row
-- that hands out ticket numbers.
--
-- Every tenant-bound table carries tenant_id, a forced tenant_isolation policy
-- (docs/adr/0021 D1) and a composite foreign key to its parent, because a
-- plain foreign key ignores row-level security and would let a row of one
-- tenant reference a parent of another.

-- A project (docs/adr/0006): the key is the namespace of the ticket keys,
-- upper case without a hyphen so the last hyphen of a ticket key ends it
-- (docs/adr/0007 D1). Archived, never deleted (docs/adr/0006 D4). A
-- restricted project is visible to administrators and to the people on its
-- list only (docs/adr/0034 D3).
CREATE TABLE projects (
    id          uuid        PRIMARY KEY DEFAULT uuidv7(),
    tenant_id   uuid        NOT NULL REFERENCES tenants (id),
    key         text        NOT NULL CHECK (key ~ '^[A-Z][A-Z0-9]{1,9}$'),
    name        text        NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 200),
    description text        NOT NULL DEFAULT '',
    restricted  boolean     NOT NULL DEFAULT false,
    archived_at timestamptz,
    -- Advisory WIP limits per state, information and never a gate
    -- (docs/adr/0019 D3): {"analysed": n, "decided": n, "in-progress": n,
    -- "blocked": n}, each optional.
    wip_limits  jsonb       NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(wip_limits) = 'object'),
    version     integer     NOT NULL DEFAULT 1,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, key),
    UNIQUE (tenant_id, id)
);

-- The people a restricted project admits, each at most with the role given
-- here (docs/adr/0034 D3). Written by the restriction administration, which
-- is not built yet; the visibility predicate reads it from the start.
CREATE TABLE project_access (
    id         uuid        PRIMARY KEY DEFAULT uuidv7(),
    tenant_id  uuid        NOT NULL,
    project_id uuid        NOT NULL,
    user_id    uuid        NOT NULL REFERENCES users (id),
    role       tenant_role NOT NULL CHECK (role IN ('viewer', 'member')),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, project_id, user_id),
    FOREIGN KEY (tenant_id, project_id) REFERENCES projects (tenant_id, id)
);
CREATE INDEX project_access_by_user ON project_access (tenant_id, user_id);

-- The last number handed out per project (docs/adr/0022 D2). A row of its
-- own, so filing a ticket neither bumps the project's version nor waits for
-- a concurrent change of the project's settings.
CREATE TABLE ticket_counters (
    tenant_id   uuid    NOT NULL,
    project_id  uuid    NOT NULL,
    last_number integer NOT NULL DEFAULT 0 CHECK (last_number >= 0),
    PRIMARY KEY (tenant_id, project_id),
    FOREIGN KEY (tenant_id, project_id) REFERENCES projects (tenant_id, id)
);

ALTER TABLE projects ENABLE ROW LEVEL SECURITY;
ALTER TABLE projects FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON projects
    USING (tenant_id = app_tenant_id())
    WITH CHECK (tenant_id = app_tenant_id());

ALTER TABLE project_access ENABLE ROW LEVEL SECURITY;
ALTER TABLE project_access FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON project_access
    USING (tenant_id = app_tenant_id())
    WITH CHECK (tenant_id = app_tenant_id());

ALTER TABLE ticket_counters ENABLE ROW LEVEL SECURITY;
ALTER TABLE ticket_counters FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON ticket_counters
    USING (tenant_id = app_tenant_id())
    WITH CHECK (tenant_id = app_tenant_id());

DO $$
DECLARE
    runtime text := current_setting('cowork.runtime_role');
BEGIN
    EXECUTE format('GRANT SELECT, INSERT ON projects TO %I', runtime);
    EXECUTE format('GRANT UPDATE (name, description, wip_limits, archived_at, version, updated_at) '
                   'ON projects TO %I', runtime);
    EXECUTE format('GRANT SELECT ON project_access TO %I', runtime);
    EXECUTE format('GRANT SELECT, INSERT, UPDATE ON ticket_counters TO %I', runtime);
END
$$;
