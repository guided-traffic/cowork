-- Persons, memberships and the row-level security of the tenancy tables.
--
-- Two roles (docs/adr/0021 D2): the role that runs this file owns every object;
-- the runtime role the server connects as owns nothing and receives the
-- privileges granted below. The migration runner names it in the session
-- setting cowork.runtime_role; a run without it fails here, on purpose.
--
-- Every policy reads the request's context through app_tenant_id() and
-- app_user_id(). A transaction-local setting reads '' (not NULL) on a pooled
-- connection after its transaction ended, so the guard is NULLIF: an unset
-- context matches no row instead of raising (docs/adr/0021 D1, D3).

CREATE FUNCTION app_tenant_id() RETURNS uuid
    LANGUAGE sql STABLE PARALLEL SAFE
    AS $$ SELECT NULLIF(current_setting('app.tenant_id', true), '')::uuid $$;

CREATE FUNCTION app_user_id() RETURNS uuid
    LANGUAGE sql STABLE PARALLEL SAFE
    AS $$ SELECT NULLIF(current_setting('app.user_id', true), '')::uuid $$;

-- A person. Phase 2 has no login: persons come from the test fixture
-- (docs/adr/0038 D6) and carry a local identity, local:<username>
-- (docs/adr/0033 D2). The identity-provider columns arrive with the login.
CREATE TABLE users (
    id             uuid        PRIMARY KEY DEFAULT uuidv7(),
    username       text        UNIQUE CHECK (username ~ '^[a-z0-9][a-z0-9._-]{0,62}$'),
    display_name   text        NOT NULL CHECK (length(btrim(display_name)) BETWEEN 1 AND 200),
    deactivated_at timestamptz,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);

-- Tenant settings (docs/adr/0034 D5, D9; docs/adr/0017 D8) and the version
-- that makes PATCH a compare-and-set (docs/adr/0050 D1).
ALTER TABLE tenants
    ADD COLUMN version                 integer NOT NULL DEFAULT 1,
    ADD COLUMN time_visible_to_members boolean NOT NULL DEFAULT false,
    ADD COLUMN time_locked_until       date,
    ADD COLUMN members_create_projects boolean NOT NULL DEFAULT true;

CREATE TYPE tenant_role AS ENUM ('viewer', 'member', 'admin');

-- How a membership came to exist (docs/adr/0030 D3, D4). Phase 2 has only
-- fixture grants; mappings arrive with the identity provider.
CREATE TYPE membership_source AS ENUM ('mapping', 'grant');

CREATE TABLE memberships (
    id         uuid              PRIMARY KEY DEFAULT uuidv7(),
    tenant_id  uuid              NOT NULL REFERENCES tenants (id),
    user_id    uuid              NOT NULL REFERENCES users (id),
    role       tenant_role       NOT NULL,
    source     membership_source NOT NULL,
    version    integer           NOT NULL DEFAULT 1,
    created_at timestamptz       NOT NULL DEFAULT now(),
    updated_at timestamptz       NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, user_id, source),
    UNIQUE (tenant_id, id)
);
CREATE INDEX memberships_by_user ON memberships (user_id);

-- tenants: readable inside its own transaction and by its members, so the
-- boundary check can resolve a slug before the tenant is set
-- (docs/adr/0023 D5); writable only inside its own transaction.
ALTER TABLE tenants ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenants FORCE ROW LEVEL SECURITY;
CREATE POLICY tenants_read ON tenants FOR SELECT
    USING (id = app_tenant_id()
           OR EXISTS (SELECT 1 FROM memberships m
                      WHERE m.tenant_id = tenants.id AND m.user_id = app_user_id()));
CREATE POLICY tenants_update ON tenants FOR UPDATE
    USING (id = app_tenant_id())
    WITH CHECK (id = app_tenant_id());

-- memberships: the tenant's rows inside the tenant, and the person's own rows
-- in every tenant (the boundary check and the person-level lists,
-- docs/adr/0021 D5).
ALTER TABLE memberships ENABLE ROW LEVEL SECURITY;
ALTER TABLE memberships FORCE ROW LEVEL SECURITY;
CREATE POLICY memberships_read ON memberships FOR SELECT
    USING (tenant_id = app_tenant_id() OR user_id = app_user_id());

-- users: the person, and everyone who shares the current tenant with it.
ALTER TABLE users ENABLE ROW LEVEL SECURITY;
ALTER TABLE users FORCE ROW LEVEL SECURITY;
CREATE POLICY users_read ON users FOR SELECT
    USING (id = app_user_id()
           OR EXISTS (SELECT 1 FROM memberships m
                      WHERE m.user_id = users.id AND m.tenant_id = app_tenant_id()));

DO $$
DECLARE
    runtime text := current_setting('cowork.runtime_role');
BEGIN
    EXECUTE format('GRANT SELECT ON schema_migrations TO %I', runtime);
    EXECUTE format('GRANT SELECT ON tenants TO %I', runtime);
    EXECUTE format('GRANT UPDATE (name, time_visible_to_members, time_locked_until, '
                   'members_create_projects, version, updated_at) ON tenants TO %I', runtime);
    EXECUTE format('GRANT SELECT ON users TO %I', runtime);
    EXECUTE format('GRANT SELECT ON memberships TO %I', runtime);
END
$$;
