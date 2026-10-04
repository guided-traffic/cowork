-- The administration of who belongs to a tenant and who sees a project
-- (docs/adr/0030 D2, D3, D6, docs/adr/0034 D3, D7).
--
-- A membership has two sources, and each has one writer. A grant is the
-- tenant's administrators': they make one (migration 15), change its role and
-- remove it. A mapped membership is the identity provider's alone: it is
-- derived from the person's groups and the tenant's mappings, at a login, a
-- groups refresh, a token's gate check or a mapping's change, and no
-- administrator writes one by hand (D3: a grant never overrides a mapping).
CREATE POLICY memberships_derive ON memberships FOR INSERT
    WITH CHECK (source = 'mapping' AND app_job() = 'identity-provider');
CREATE POLICY memberships_update ON memberships FOR UPDATE
    USING ((source = 'grant' AND tenant_id = app_tenant_id() AND app_is_tenant_admin())
           OR (source = 'mapping' AND app_job() = 'identity-provider'))
    WITH CHECK ((source = 'grant' AND tenant_id = app_tenant_id() AND app_is_tenant_admin())
                OR (source = 'mapping' AND app_job() = 'identity-provider'));
CREATE POLICY memberships_delete ON memberships FOR DELETE
    USING ((source = 'grant' AND tenant_id = app_tenant_id() AND app_is_tenant_admin())
           OR (source = 'mapping' AND app_job() = 'identity-provider'));

-- project_access: the list of a restricted project is written by the tenant's
-- administrators (docs/adr/0034 D1, D3); tenant_isolation keeps it in its
-- tenant, these keep it to them.
CREATE POLICY project_access_admin_insert ON project_access AS RESTRICTIVE FOR INSERT
    WITH CHECK (app_is_tenant_admin());
CREATE POLICY project_access_admin_update ON project_access AS RESTRICTIVE FOR UPDATE
    USING (app_is_tenant_admin())
    WITH CHECK (app_is_tenant_admin());
CREATE POLICY project_access_admin_delete ON project_access AS RESTRICTIVE FOR DELETE
    USING (app_is_tenant_admin());

-- projects: a project's restriction is its tenant's administrators' to set or
-- lift (docs/adr/0034 D1, D3), in the data layer too: tenant_isolation keeps
-- the row in its tenant, and a member may change the project's name, so a
-- policy, which sees the row and not the column, cannot say it; this trigger
-- does (the security review of 2026-10-04, item 14). A superuser bypasses
-- row-level security altogether and is left to it.
CREATE FUNCTION projects_restriction_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF NEW.restricted IS DISTINCT FROM OLD.restricted
       AND NOT app_is_tenant_admin()
       AND NOT (SELECT rolsuper FROM pg_roles WHERE rolname = current_user) THEN
        RAISE EXCEPTION 'only an administrator of the tenant restricts or opens a project'
            USING ERRCODE = '42501';
    END IF;
    RETURN NEW;
END
$$;
CREATE TRIGGER projects_restriction_guard BEFORE UPDATE OF restricted ON projects
    FOR EACH ROW EXECUTE FUNCTION projects_restriction_guard();

DO $$
DECLARE
    runtime text := current_setting('cowork.runtime_role');
BEGIN
    EXECUTE format('GRANT UPDATE (role, version, updated_at), DELETE ON memberships TO %I', runtime);
    EXECUTE format('GRANT INSERT (tenant_id, project_id, user_id, role), UPDATE (role), DELETE ON project_access TO %I', runtime);
    EXECUTE format('GRANT UPDATE (restricted) ON projects TO %I', runtime);
END
$$;
