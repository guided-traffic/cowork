-- A tenant's administrator unshares or deletes another person's shared saved
-- filter (docs/adr/0018 D5 as amended 2026-10-06) — one whose owner left the
-- tenant among them, which until now stayed shared under the name of a person
-- who is no member any more. The administrator changes nothing else of
-- another person's filter and never reaches one that is not shared.
--
-- The restrictive policies of migration 33 held every write to the filter's
-- owner. They admit, beside the owner, an administrator of the current tenant
-- (app_is_tenant_admin(), migration 7):
--
-- - to change a shared filter only into one that is not shared. A policy sees
--   the row, not the columns a statement sets: the API's query sets nothing
--   but shared, its version and its time;
-- - to delete a shared filter.
--
-- An update whose WHERE reads the row must leave a row its writer may read:
-- PostgreSQL holds the new row to the read policy and refuses the statement
-- otherwise. A filter of another person that is not shared is one nobody but
-- its owner reads, so the read policy admits an administrator of the current
-- tenant to the one filter the transaction names in app.saved_filter_id —
-- which the unshare names for its own statement and clears after it — and to
-- no other filter that is not shared.
--
-- Expand only (docs/adr/0028 D3): the policies widen and nothing the release
-- before reads changes; that release never names a filter, so its
-- transactions see and write what they did.

CREATE FUNCTION app_saved_filter_id() RETURNS uuid
    LANGUAGE sql STABLE PARALLEL SAFE
    AS $$ SELECT NULLIF(current_setting('app.saved_filter_id', true), '')::uuid $$;

ALTER POLICY saved_filters_read ON saved_filters
    USING (owner_id = app_user_id() OR shared OR (id = app_saved_filter_id() AND app_is_tenant_admin()));
ALTER POLICY saved_filters_update ON saved_filters
    USING (owner_id = app_user_id() OR (shared AND app_is_tenant_admin()))
    WITH CHECK (owner_id = app_user_id() OR (NOT shared AND app_is_tenant_admin()));
ALTER POLICY saved_filters_delete ON saved_filters
    USING (owner_id = app_user_id() OR (shared AND app_is_tenant_admin()));
