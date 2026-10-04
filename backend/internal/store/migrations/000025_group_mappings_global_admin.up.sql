-- A group mapping is made and its role changed only by a global administrator
-- who administers the tenant (docs/adr/0030 D7, the owner's answer of
-- 2026-10-04): every tenant shares the identity provider's one namespace of
-- groups, and a mapping admits everyone in its group into the tenant at once.
-- The handler refuses any other administrator (api/members.go mapsGroups);
-- this holds the data layer to the same rule, as migration 21 promised it holds
-- mappings to who may write them. Removing a mapping only takes access away and
-- stays with every administrator of the tenant; the start-up synchronisation
-- still seeds the administrator group's mapping of the bootstrap tenant
-- (docs/adr/0032 D6), which it writes as app.job = 'bootstrap' with no person.
--
-- The policies are restrictive, so this narrows what any permissive policy
-- admits. It narrows a write, not a read (docs/adr/0028 D3): no released binary
-- writes group_mappings — 0.2.0 ends at migration 16 — and a binary built before
-- this migration that lets an administrator who is not a global administrator
-- make or change a mapping gets a policy error (SQLSTATE 42501) for an act the
-- rule forbids; every other write of it is admitted as before.
ALTER POLICY group_mappings_admin_insert ON group_mappings
    WITH CHECK ((app_is_tenant_admin() AND app_is_global_admin()) OR app_job() = 'bootstrap');
ALTER POLICY group_mappings_admin_update ON group_mappings
    USING (app_is_tenant_admin() AND app_is_global_admin())
    WITH CHECK (app_is_tenant_admin() AND app_is_global_admin());
