-- Saved filters (docs/adr/0018 D5, docs/adr/0049 D6, D7): a named set of the
-- ticket list's filter parameters, owned by a person in a tenant, optionally
-- shared with the tenant's members. The parameters are the list's query
-- parameters as a JSON object, validated by the API when a filter is written
-- and again when it is read, so a vocabulary that changed shows as a warning
-- instead of an empty list. A filter carries a version (docs/adr/0050 D1).
--
-- Expand only (docs/adr/0028 D3): a new table the previous release never
-- reads.
CREATE TABLE saved_filters (
    id         uuid        PRIMARY KEY DEFAULT uuidv7(),
    tenant_id  uuid        NOT NULL REFERENCES tenants (id),
    owner_id   uuid        NOT NULL REFERENCES users (id),
    name       text        NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 100),
    parameters jsonb       NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(parameters) = 'object'),
    shared     boolean     NOT NULL DEFAULT false,
    version    integer     NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id)
);
CREATE INDEX saved_filters_by_owner ON saved_filters (tenant_id, owner_id, id);
CREATE INDEX saved_filters_shared   ON saved_filters (tenant_id, id) WHERE shared;

-- The tenant's rows in the tenant; of those, a person reads their own and the
-- shared ones, and writes their own only. The restrictive policies are ANDed
-- with the canonical one, so a query that forgot its owner shows nobody
-- another person's private filter and changes nobody's but the caller's.
ALTER TABLE saved_filters ENABLE ROW LEVEL SECURITY;
ALTER TABLE saved_filters FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON saved_filters
    USING (tenant_id = app_tenant_id())
    WITH CHECK (tenant_id = app_tenant_id());
CREATE POLICY saved_filters_read ON saved_filters AS RESTRICTIVE FOR SELECT
    USING (owner_id = app_user_id() OR shared);
CREATE POLICY saved_filters_insert ON saved_filters AS RESTRICTIVE FOR INSERT
    WITH CHECK (owner_id = app_user_id());
CREATE POLICY saved_filters_update ON saved_filters AS RESTRICTIVE FOR UPDATE
    USING (owner_id = app_user_id())
    WITH CHECK (owner_id = app_user_id());
CREATE POLICY saved_filters_delete ON saved_filters AS RESTRICTIVE FOR DELETE
    USING (owner_id = app_user_id());

DO $$
DECLARE
    runtime text := current_setting('cowork.runtime_role');
BEGIN
    EXECUTE format('GRANT SELECT, INSERT, DELETE ON saved_filters TO %I', runtime);
    EXECUTE format('GRANT UPDATE (name, parameters, shared, version, updated_at) ON saved_filters TO %I', runtime);
END
$$;
