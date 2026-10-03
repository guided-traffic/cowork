-- The project restriction (docs/adr/0034 D3, D4) as one predicate, written
-- once and called by every query that reads a project or what belongs to one;
-- a unit test on the query files holds every such query to it.
--
-- A project is visible when it is the token's project, if the token is
-- restricted to one (docs/adr/0035 D3), and when it is unrestricted, or the
-- person is a tenant administrator, or the person is on its list.

CREATE FUNCTION app_restricted_project_id() RETURNS uuid
    LANGUAGE sql STABLE PARALLEL SAFE
    AS $$ SELECT NULLIF(current_setting('app.restricted_project_id', true), '')::uuid $$;

CREATE FUNCTION app_is_tenant_admin() RETURNS boolean
    LANGUAGE sql STABLE PARALLEL SAFE
    AS $$
        SELECT EXISTS (SELECT 1 FROM memberships
                       WHERE tenant_id = app_tenant_id() AND user_id = app_user_id() AND role = 'admin')
    $$;

CREATE FUNCTION app_project_visible(p_project_id uuid) RETURNS boolean
    LANGUAGE sql STABLE PARALLEL SAFE
    AS $$
        SELECT (app_restricted_project_id() IS NULL OR p_project_id = app_restricted_project_id())
           AND EXISTS (
               SELECT 1 FROM projects p
               WHERE p.tenant_id = app_tenant_id() AND p.id = p_project_id
                 AND (NOT p.restricted
                      OR app_is_tenant_admin()
                      OR EXISTS (SELECT 1 FROM project_access a
                                 WHERE a.tenant_id = p.tenant_id AND a.project_id = p.id
                                   AND a.user_id = app_user_id())))
    $$;
