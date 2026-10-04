-- The identity provider (docs/adr/0029, 0030, 0031, 0035 D2, D8): its persons,
-- the groups snapshot the gate and the derivation read, the session it makes
-- with its sealed refresh token, the refusal of a login through it, and the
-- source hash of every audit row a request writes.
--
-- What the provider decides is written by the system actor
-- system:identity-provider, which names itself in app.job = 'identity-provider'
-- (docs/adr/0021 D3): the login through the provider, the groups refresh of its
-- sessions, the gate of its persons' tokens, and the derivation of memberships
-- from the group mappings. It may write the persons of the provider and nobody
-- else: a local account, the local administrator above all, is never its to
-- touch.

ALTER TYPE audit_action ADD VALUE IF NOT EXISTS 'login_refused';

-- A person of the identity provider is (issuer, subject) (docs/adr/0029 D5);
-- email and display_name are what the issuer said at the last login. The
-- issuer's word on the address is email_verified, NULL when it said nothing: a
-- grant by e-mail address never matches an address it marked unverified.
-- oidc_groups is the person's groups as of oidc_groups_at, their last login or
-- groups refresh, which the token gate checks again (docs/adr/0035 D8) and
-- stamps in gate_checked_at. username names local accounts only.
ALTER TABLE users
    ADD COLUMN oidc_issuer     text,
    ADD COLUMN oidc_subject    text,
    ADD COLUMN email           text CHECK (length(email) <= 320),
    ADD COLUMN email_verified  boolean,
    ADD COLUMN oidc_groups     text[],
    ADD COLUMN oidc_groups_at  timestamptz,
    ADD COLUMN gate_checked_at timestamptz,
    ADD CONSTRAINT users_oidc_identity CHECK ((oidc_issuer IS NULL) = (oidc_subject IS NULL)),
    ADD CONSTRAINT users_oidc_or_local CHECK (oidc_issuer IS NULL OR username IS NULL),
    ADD CONSTRAINT users_oidc_identity_key UNIQUE (oidc_issuer, oidc_subject);
CREATE INDEX users_by_email ON users (lower(email)) WHERE email IS NOT NULL;
CREATE INDEX users_by_groups ON users USING gin (oidc_groups) WHERE oidc_groups IS NOT NULL;

-- A session the provider's login made (method 'oidc') holds the groups of its
-- last login or refresh and, when the issuer gave one, its refresh token,
-- sealed with AES-256-GCM under a key derived from COWORK_SESSION_KEY and bound
-- to the session's hash (docs/adr/0031 D1). refresh_retry_at is the earliest
-- next attempt after the issuer could not be reached (docs/adr/0030 D5).
ALTER TABLE sessions
    ADD COLUMN method               text NOT NULL DEFAULT 'local' CHECK (method IN ('local', 'oidc')),
    ADD COLUMN groups               text[],
    ADD COLUMN groups_refreshed_at  timestamptz,
    ADD COLUMN refresh_token_sealed bytea,
    ADD COLUMN refresh_retry_at     timestamptz;

-- The keyed hash of the client address of the request an audit row was written
-- for (docs/adr/0035 D2): HMAC-SHA-256 under a key derived from
-- COWORK_SESSION_KEY. Rows of jobs and of the start-up have none, and so have
-- the rows written before this release.
ALTER TABLE audit_events ADD COLUMN source_hash bytea CHECK (octet_length(source_hash) = 32);

-- The address or username a tenant's administrator looks a person up by, to
-- grant them a role (docs/adr/0030 D3).
CREATE FUNCTION app_person_lookup() RETURNS text
    LANGUAGE sql STABLE PARALLEL SAFE
    AS $$ SELECT NULLIF(current_setting('app.person_lookup', true), '') $$;

-- users: the identity provider reads every person — the derivation finds the
-- persons whose groups a mapping names — and a tenant's administrator the one
-- person their lookup names; the provider creates and keeps its own persons,
-- whose global administrator flag the administrator group decides
-- (docs/adr/0030 D1), and no one else makes a person who claims an identity
-- of the provider.
ALTER POLICY users_read ON users USING (
    id = app_user_id()
    OR EXISTS (SELECT 1 FROM memberships m WHERE m.user_id = users.id AND m.tenant_id = app_tenant_id())
    OR app_job() IN ('login', 'bootstrap', 'identity-provider')
    OR (app_person_lookup() IS NOT NULL AND app_is_tenant_admin()
        AND (lower(email) = lower(app_person_lookup()) OR username = app_person_lookup())));
ALTER POLICY users_insert ON users WITH CHECK (
    (NOT global_admin AND app_is_tenant_admin() AND oidc_issuer IS NULL)
    OR app_job() = 'bootstrap'
    OR (app_job() = 'identity-provider' AND oidc_issuer IS NOT NULL AND username IS NULL));
ALTER POLICY users_update ON users
    USING ((NOT global_admin AND app_manages_account(id)) OR app_job() = 'bootstrap'
           OR (app_job() = 'identity-provider' AND oidc_issuer IS NOT NULL))
    WITH CHECK ((NOT global_admin AND app_manages_account(id)) OR app_job() = 'bootstrap'
                OR (app_job() = 'identity-provider' AND oidc_issuer IS NOT NULL AND username IS NULL));

-- tenants: the provider's login asks whether any exists (docs/adr/0032 D5).
ALTER POLICY tenants_read ON tenants USING (
    id = app_tenant_id()
    OR EXISTS (SELECT 1 FROM memberships m WHERE m.tenant_id = tenants.id AND m.user_id = app_user_id())
    OR app_job() IN ('login', 'bootstrap', 'identity-provider'));

DO $$
DECLARE
    runtime text := current_setting('cowork.runtime_role');
BEGIN
    EXECUTE format('GRANT INSERT (oidc_issuer, oidc_subject, email, email_verified, oidc_groups, oidc_groups_at, '
                   'gate_checked_at) ON users TO %I', runtime);
    EXECUTE format('GRANT UPDATE (email, email_verified, oidc_groups, oidc_groups_at, gate_checked_at) ON users TO %I', runtime);
    EXECUTE format('GRANT UPDATE (groups, groups_refreshed_at, refresh_token_sealed, refresh_retry_at) ON sessions TO %I', runtime);
END
$$;
