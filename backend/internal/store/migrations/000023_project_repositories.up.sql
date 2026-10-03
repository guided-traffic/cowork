-- The repositories a project owns (docs/adr/0006 D3, docs/adr/0066 D1).
--
-- A repository is its normalised remote identity, host/path, and optionally a
-- sub-directory of a monorepo; the remote as it was last given is kept beside
-- it, without its credentials. A repository is in at most one project of a
-- tenant; across tenants nothing can hold it to one without telling a tenant
-- what another binds, so a lookup that finds several reports them as a data
-- error (docs/adr/0066 D6).
CREATE TABLE project_repositories (
    id         uuid        PRIMARY KEY DEFAULT uuidv7(),
    tenant_id  uuid        NOT NULL,
    project_id uuid        NOT NULL,
    identity   text        NOT NULL CHECK (length(identity) BETWEEN 3 AND 512 AND identity LIKE '%_/_%'),
    path       text        NOT NULL DEFAULT '' CHECK (length(path) <= 512 AND path NOT LIKE '/%' AND path NOT LIKE '%/'),
    remote     text        NOT NULL CHECK (length(remote) BETWEEN 1 AND 2000),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, identity, path),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, project_id) REFERENCES projects (tenant_id, id)
);
CREATE INDEX project_repositories_by_project ON project_repositories (tenant_id, project_id);

ALTER TABLE project_repositories ENABLE ROW LEVEL SECURITY;
ALTER TABLE project_repositories FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON project_repositories
    USING (tenant_id = app_tenant_id())
    WITH CHECK (tenant_id = app_tenant_id());

DO $$
DECLARE
    runtime text := current_setting('cowork.runtime_role');
BEGIN
    EXECUTE format('GRANT SELECT, INSERT, DELETE ON project_repositories TO %I', runtime);
    EXECUTE format('GRANT UPDATE (remote, updated_at) ON project_repositories TO %I', runtime);
END
$$;
