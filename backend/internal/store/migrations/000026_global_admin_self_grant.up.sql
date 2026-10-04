-- A global administrator's reach into a tenant in which they hold no role
-- (docs/adr/0034 D2, built 2026-10-04): they find every tenant of the
-- installation, see a tenant's administration — the tenant and its settings,
-- its members, its group mappings — and grant themselves a role, a marked
-- grant like any other; where they hold a role below admin, they raise their
-- own grant. It is how a tenant that lost its last administrator who can log in
-- is given one again.
--
-- tenants: read by a global administrator as well, every row
-- (docs/adr/0021 D6, which planned this widening). The tenant list of
-- GET /api/v1/tenants and the request layer's admission of such a global
-- administrator to a tenant read it in a transaction that names no tenant. The
-- tenant's other rows are read inside the tenant's own transaction, where the
-- tenant-bound policies admit them to anyone the request layer admits; which
-- operations that is for a global administrator without a role is the request
-- layer's (api/tenant.go oversight), as membership is for everyone else
-- (docs/adr/0021 D3). No other table's policy changes here.
ALTER POLICY tenants_read ON tenants USING (
    id = app_tenant_id()
    OR EXISTS (SELECT 1 FROM memberships m WHERE m.tenant_id = tenants.id AND m.user_id = app_user_id())
    OR app_job() IN ('login', 'bootstrap', 'identity-provider')
    OR app_is_global_admin());

-- memberships: a global administrator's grant to themselves in any role, not
-- only as admin. Migration 15 admitted their own grant as admin, which the
-- creation of a tenant writes; the grant into an existing tenant takes the
-- role the administrator picks (docs/adr/0034 D2). Nobody else's grant, and
-- never a mapped membership, which stays the identity provider's.
ALTER POLICY memberships_insert ON memberships WITH CHECK (
    source = 'grant'
    AND ((tenant_id = app_tenant_id() AND app_is_tenant_admin())
         OR (user_id = app_user_id() AND app_is_global_admin())
         OR app_job() = 'bootstrap'));

-- memberships: and a change of their own grant's role inside the tenant's
-- transaction, which raises a global administrator who holds a role below
-- admin (docs/adr/0034 D2). The request layer offers it only to one who does
-- not hold admin; one who does is an administrator of the tenant and passes
-- the first branch anyway, where a lowering meets last_admin like anyone's.
-- Nobody else's grant, and never a mapped membership.
ALTER POLICY memberships_update ON memberships
    USING ((source = 'grant' AND tenant_id = app_tenant_id() AND app_is_tenant_admin())
           OR (source = 'grant' AND tenant_id = app_tenant_id() AND user_id = app_user_id() AND app_is_global_admin())
           OR (source = 'mapping' AND app_job() = 'identity-provider'))
    WITH CHECK ((source = 'grant' AND tenant_id = app_tenant_id() AND app_is_tenant_admin())
                OR (source = 'grant' AND tenant_id = app_tenant_id() AND user_id = app_user_id() AND app_is_global_admin())
                OR (source = 'mapping' AND app_job() = 'identity-provider'));
