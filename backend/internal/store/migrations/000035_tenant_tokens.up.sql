-- A tenant's administrators see and revoke the tokens that can act in the
-- tenant (docs/adr/0035 D5 as amended 2026-10-05): every token of a member of
-- the current tenant that is unrestricted or restricted to this tenant. A
-- token restricted to another tenant, and a token of a person who is no member
-- here, stay invisible to them — not its name, not that it exists
-- (docs/adr/0005 D3).
--
-- Until now the policies of migrations 4 and 15 admitted a person's own
-- tokens, the one row a request presents, the tokens of the local accounts a
-- tenant manages, and the start-up synchronisation's. The two policies below
-- add, for an administrator of the current tenant, exactly the rows of the
-- route GET /api/v1/tenants/{tenant}/tokens; they are permissive and widen
-- nothing else. Revoking writes revoked_at and revoked_by, the columns the
-- runtime role may already update, and the trigger of migration 4 still makes
-- a revocation final.

CREATE FUNCTION app_tenant_reaches_token(p_user_id uuid, p_restricted_tenant_id uuid) RETURNS boolean
    LANGUAGE sql STABLE PARALLEL SAFE
    AS $$
        SELECT app_is_tenant_admin()
           AND (p_restricted_tenant_id IS NULL OR p_restricted_tenant_id = app_tenant_id())
           AND EXISTS (SELECT 1 FROM memberships m
                       WHERE m.tenant_id = app_tenant_id() AND m.user_id = p_user_id)
    $$;

CREATE POLICY tokens_tenant_admin_read ON tokens FOR SELECT
    USING (app_tenant_reaches_token(user_id, restricted_tenant_id));
CREATE POLICY tokens_tenant_admin_revoke ON tokens FOR UPDATE
    USING (app_tenant_reaches_token(user_id, restricted_tenant_id))
    WITH CHECK (app_tenant_reaches_token(user_id, restricted_tenant_id));
