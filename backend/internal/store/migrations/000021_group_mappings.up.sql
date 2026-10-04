-- The group mappings of a tenant (docs/adr/0030 D2, D7): a group of the
-- identity provider gives its members a role in the tenant. The group is
-- compared exactly, case and all, with the groups claim (docs/adr/0029 D2); a
-- tenant maps a group once, and several mapped groups of one person yield the
-- highest role. created_by is the administrator who made the mapping, NULL for
-- the one the start-up seeds (docs/adr/0032 D6).
CREATE TABLE group_mappings (
    id         uuid        PRIMARY KEY DEFAULT uuidv7(),
    tenant_id  uuid        NOT NULL REFERENCES tenants (id),
    group_name text        NOT NULL CHECK (length(group_name) BETWEEN 1 AND 256),
    role       tenant_role NOT NULL,
    version    integer     NOT NULL DEFAULT 1,
    created_by uuid        REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, group_name),
    UNIQUE (tenant_id, id)
);
CREATE INDEX group_mappings_by_group ON group_mappings (group_name);

ALTER TABLE group_mappings ENABLE ROW LEVEL SECURITY;
ALTER TABLE group_mappings FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON group_mappings
    USING (tenant_id = app_tenant_id())
    WITH CHECK (tenant_id = app_tenant_id());
-- The identity provider derives a person's memberships in every tenant at once,
-- so it reads every tenant's mappings, and only it.
CREATE POLICY group_mappings_derive ON group_mappings FOR SELECT
    USING (app_job() = 'identity-provider');
-- The start-up synchronisation seeds the administrator group's mapping of the
-- bootstrap tenant from an installation transaction (docs/adr/0032 D6).
CREATE POLICY group_mappings_bootstrap ON group_mappings FOR INSERT
    WITH CHECK (app_job() = 'bootstrap');
-- A mapping makes administrators, so writing one is the tenant's
-- administrators' in the data layer too, not only in the handler.
CREATE POLICY group_mappings_admin_insert ON group_mappings AS RESTRICTIVE FOR INSERT
    WITH CHECK (app_is_tenant_admin() OR app_job() = 'bootstrap');
CREATE POLICY group_mappings_admin_update ON group_mappings AS RESTRICTIVE FOR UPDATE
    USING (app_is_tenant_admin())
    WITH CHECK (app_is_tenant_admin());
CREATE POLICY group_mappings_admin_delete ON group_mappings AS RESTRICTIVE FOR DELETE
    USING (app_is_tenant_admin());

DO $$
DECLARE
    runtime text := current_setting('cowork.runtime_role');
BEGIN
    EXECUTE format('GRANT SELECT, INSERT, DELETE ON group_mappings TO %I', runtime);
    EXECUTE format('GRANT UPDATE (role, version, updated_at) ON group_mappings TO %I', runtime);
END
$$;
