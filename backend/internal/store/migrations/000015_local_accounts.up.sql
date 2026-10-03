-- Local accounts, the bookkeeping of the local login, and the writes the login
-- and the creation routes need (docs/adr/0031, 0032, 0033, 0035 D5,
-- docs/adr/0005 D5).
--
-- Until this migration the runtime role could only read persons, tenants and
-- memberships; the fixture wrote them over the administrative connection
-- (docs/adr/0038 D6). Now a route writes them, so each write gets the
-- narrowest policy that serves it: who may write is a tenant administrator of
-- the current tenant, a global administrator creating a tenant, the person
-- themselves, or a named system actor — the login (`app.job = 'login'`) and
-- the start-up synchronisation (`app.job = 'bootstrap'`), which ADR 0021 D3
-- names in the transaction like any job.
--
-- What a tenant's administrators may manage is decided here, in one place:
-- the local accounts their tenant created (`managing_tenant_id`). An account
-- is a person across the whole installation — its password and its sessions
-- are not a tenant's — so an administrator who could reset the password of a
-- person they merely share a tenant with could enter every other tenant of
-- that person, and a global administrator's account in particular.

ALTER TYPE audit_action ADD VALUE IF NOT EXISTS 'logged_in';
ALTER TYPE audit_action ADD VALUE IF NOT EXISTS 'logged_out';
ALTER TYPE audit_action ADD VALUE IF NOT EXISTS 'login_failed';
ALTER TYPE audit_action ADD VALUE IF NOT EXISTS 'unlocked';
ALTER TYPE audit_action ADD VALUE IF NOT EXISTS 'password_changed';
ALTER TYPE audit_action ADD VALUE IF NOT EXISTS 'password_reset';
ALTER TYPE audit_action ADD VALUE IF NOT EXISTS 'deactivated';
ALTER TYPE audit_action ADD VALUE IF NOT EXISTS 'reactivated';

-- A global administrator creates tenants (docs/adr/0005 D5) and has no role in
-- any of them (docs/adr/0034 D2). Only the start-up synchronisation sets it
-- (the policies below); it is configuration, not a route.
ALTER TABLE users ADD COLUMN global_admin boolean NOT NULL DEFAULT false;

-- The password of a local account (docs/adr/0033). A hash in the PHC string
-- form of Argon2id, parameters included; the plaintext exists nowhere. A row
-- is what makes a person a local account: the person's identity is
-- local:<username> (docs/adr/0033 D2). origin says where the account comes
-- from — 'config' for the one account COWORK_LOCAL_ADMIN_* names
-- (docs/adr/0032 D1, D4), 'tenant' for one a tenant's administrator created
-- (docs/adr/0033 D1) — and managing_tenant_id which tenant's administrators
-- manage it: that tenant for 'tenant', nobody for 'config'.
CREATE TABLE local_accounts (
    user_id                  uuid        PRIMARY KEY REFERENCES users (id),
    password_hash            text        NOT NULL CHECK (password_hash LIKE '$argon2id$%'),
    password_change_required boolean     NOT NULL DEFAULT false,
    origin                   text        NOT NULL CHECK (origin IN ('config', 'tenant')),
    managing_tenant_id       uuid        REFERENCES tenants (id),
    created_at               timestamptz NOT NULL DEFAULT now(),
    updated_at               timestamptz NOT NULL DEFAULT now(),
    CHECK ((origin = 'tenant') = (managing_tenant_id IS NOT NULL))
);

-- The login attempts that were processed, for the per-address throttle and
-- the per-account lockout (docs/adr/0033 D6). They are keyed by the username
-- as presented, not by a person: an unknown username is counted and locked
-- exactly like a known one, so neither the answer nor the lockout says
-- whether an account exists. The address is a keyed hash of the client address
-- (docs/adr/0035 D2), never the address. Rows older than the lockout window are
-- removed by a job.
CREATE TABLE login_attempts (
    id         uuid        PRIMARY KEY DEFAULT uuidv7(),
    username   text        NOT NULL CHECK (length(username) <= 63),
    address    bytea       NOT NULL CHECK (octet_length(address) = 32),
    failed     boolean     NOT NULL,
    created_at timestamptz NOT NULL
);
CREATE INDEX login_attempts_by_address  ON login_attempts (address, created_at);
CREATE INDEX login_attempts_by_username ON login_attempts (username, created_at) WHERE failed;
CREATE INDEX login_attempts_by_age      ON login_attempts (created_at);

-- A lock the failures of a username caused. sticky locks stay until an
-- administrator unlocks (COWORK_LOGIN_LOCKOUT=admin); the others end with the
-- window. noted_at is when an attempt against the lock was last written to the
-- audit record, so a hammered lock writes one row an hour.
CREATE TABLE login_locks (
    username  text        PRIMARY KEY CHECK (length(username) <= 63),
    locked_at timestamptz NOT NULL,
    sticky    boolean     NOT NULL,
    noted_at  timestamptz
);
CREATE INDEX login_locks_by_age ON login_locks (locked_at);

-- What the policies below read besides app_tenant_id() and app_user_id().
CREATE FUNCTION app_job() RETURNS text
    LANGUAGE sql STABLE PARALLEL SAFE
    AS $$ SELECT NULLIF(current_setting('app.job', true), '') $$;

CREATE FUNCTION app_session_hash() RETURNS bytea
    LANGUAGE sql STABLE PARALLEL SAFE
    AS $$ SELECT decode(NULLIF(current_setting('app.session_hash', true), ''), 'hex') $$;

CREATE FUNCTION app_is_global_admin() RETURNS boolean
    LANGUAGE sql STABLE PARALLEL SAFE
    AS $$
        SELECT EXISTS (SELECT 1 FROM users
                       WHERE id = app_user_id() AND global_admin AND deactivated_at IS NULL)
    $$;

-- The current person is an administrator of the current tenant and the
-- account is one that tenant created (see the head of this file).
CREATE FUNCTION app_manages_account(p_user_id uuid) RETURNS boolean
    LANGUAGE sql STABLE PARALLEL SAFE
    AS $$
        SELECT app_is_tenant_admin()
           AND EXISTS (SELECT 1 FROM local_accounts a
                       WHERE a.user_id = p_user_id AND a.managing_tenant_id = app_tenant_id())
    $$;

CREATE FUNCTION app_manages_username(p_username text) RETURNS boolean
    LANGUAGE sql STABLE PARALLEL SAFE
    AS $$
        SELECT app_is_tenant_admin()
           AND EXISTS (SELECT 1 FROM users u
                       JOIN local_accounts a ON a.user_id = u.id
                       WHERE u.username = p_username AND a.managing_tenant_id = app_tenant_id())
    $$;

-- users: the login reads the username it is asked about, and the start-up
-- synchronisation the account it keeps. Only the synchronisation makes a
-- global administrator, and no administrator of a tenant touches one.
ALTER POLICY users_read ON users USING (
    id = app_user_id()
    OR EXISTS (SELECT 1 FROM memberships m WHERE m.user_id = users.id AND m.tenant_id = app_tenant_id())
    OR app_job() IN ('login', 'bootstrap'));
CREATE POLICY users_insert ON users FOR INSERT
    WITH CHECK ((NOT global_admin AND app_is_tenant_admin()) OR app_job() = 'bootstrap');
CREATE POLICY users_update ON users FOR UPDATE
    USING ((NOT global_admin AND app_manages_account(id)) OR app_job() = 'bootstrap')
    WITH CHECK ((NOT global_admin AND app_manages_account(id)) OR app_job() = 'bootstrap');

-- tenants: one is created by a global administrator (docs/adr/0005 D5) or by
-- the start-up synchronisation (docs/adr/0032 D6); the login asks whether any
-- exists (docs/adr/0032 D5).
ALTER POLICY tenants_read ON tenants USING (
    id = app_tenant_id()
    OR EXISTS (SELECT 1 FROM memberships m WHERE m.tenant_id = tenants.id AND m.user_id = app_user_id())
    OR app_job() IN ('login', 'bootstrap'));
CREATE POLICY tenants_insert ON tenants FOR INSERT
    WITH CHECK (app_is_global_admin() OR app_job() = 'bootstrap');

-- memberships: a marked grant (docs/adr/0030 D3) — an administrator grants
-- into their own tenant, the creator of a tenant into it as its first
-- administrator (docs/adr/0032 D7), the start-up synchronisation likewise.
CREATE POLICY memberships_insert ON memberships FOR INSERT WITH CHECK (
    source = 'grant'
    AND ((tenant_id = app_tenant_id() AND app_is_tenant_admin())
         OR (user_id = app_user_id() AND role = 'admin' AND app_is_global_admin())
         OR app_job() = 'bootstrap'));

-- local_accounts: the person reads and changes their own row, the
-- administrators of the managing tenant theirs, the login the account it is
-- asked about, the start-up synchronisation the one it keeps.
ALTER TABLE local_accounts ENABLE ROW LEVEL SECURITY;
ALTER TABLE local_accounts FORCE ROW LEVEL SECURITY;
CREATE POLICY local_accounts_read ON local_accounts FOR SELECT USING (
    user_id = app_user_id()
    OR (managing_tenant_id = app_tenant_id() AND app_is_tenant_admin())
    OR app_job() IN ('login', 'bootstrap'));
CREATE POLICY local_accounts_insert ON local_accounts FOR INSERT WITH CHECK (
    (origin = 'tenant' AND managing_tenant_id = app_tenant_id() AND app_is_tenant_admin())
    OR (origin = 'config' AND app_job() = 'bootstrap'));
CREATE POLICY local_accounts_update ON local_accounts FOR UPDATE
    USING (user_id = app_user_id()
           OR (managing_tenant_id = app_tenant_id() AND app_is_tenant_admin())
           OR app_job() = 'bootstrap')
    WITH CHECK ((origin = 'tenant' AND managing_tenant_id IS NOT NULL
                 AND (user_id = app_user_id()
                      OR (managing_tenant_id = app_tenant_id() AND app_is_tenant_admin())))
                OR app_job() = 'bootstrap');

-- login_attempts and login_locks: written and read by the login, cleaned by
-- its expiry job, and cleared by the administrators of the account's tenant
-- when they unlock it.
ALTER TABLE login_attempts ENABLE ROW LEVEL SECURITY;
ALTER TABLE login_attempts FORCE ROW LEVEL SECURITY;
CREATE POLICY login_attempts_job ON login_attempts
    USING (app_job() IN ('login', 'login-expiry', 'bootstrap'))
    WITH CHECK (app_job() = 'login');
CREATE POLICY login_attempts_managed ON login_attempts FOR SELECT
    USING (app_manages_username(username));
CREATE POLICY login_attempts_unlock ON login_attempts FOR DELETE
    USING (app_manages_username(username));

ALTER TABLE login_locks ENABLE ROW LEVEL SECURITY;
ALTER TABLE login_locks FORCE ROW LEVEL SECURITY;
CREATE POLICY login_locks_job ON login_locks
    USING (app_job() IN ('login', 'login-expiry', 'bootstrap'))
    WITH CHECK (app_job() = 'login');
CREATE POLICY login_locks_managed ON login_locks FOR SELECT
    USING (app_manages_username(username));
CREATE POLICY login_locks_unlock ON login_locks FOR DELETE
    USING (app_manages_username(username));

-- tokens: a person creates their own (docs/adr/0035 D5); the administrators of
-- a managed account and the start-up synchronisation revoke its tokens when
-- it is deactivated (docs/adr/0024 D5). The trigger of migration 4 still
-- makes a revocation final.
CREATE POLICY tokens_insert ON tokens FOR INSERT WITH CHECK (user_id = app_user_id());
ALTER POLICY tokens_read ON tokens USING (
    user_id = app_user_id()
    OR token_hash = decode(NULLIF(current_setting('app.token_hash', true), ''), 'hex')
    OR app_manages_account(user_id)
    OR app_job() = 'bootstrap');
ALTER POLICY tokens_update ON tokens
    USING (user_id = app_user_id() OR app_manages_account(user_id) OR app_job() = 'bootstrap')
    WITH CHECK (user_id = app_user_id() OR app_manages_account(user_id) OR app_job() = 'bootstrap');

DO $$
DECLARE
    runtime text := current_setting('cowork.runtime_role');
BEGIN
    EXECUTE format('GRANT INSERT (id, slug, name) ON tenants TO %I', runtime);
    EXECUTE format('GRANT INSERT (id, username, display_name, global_admin) ON users TO %I', runtime);
    EXECUTE format('GRANT UPDATE (display_name, deactivated_at, global_admin, updated_at) ON users TO %I', runtime);
    EXECUTE format('GRANT INSERT (id, tenant_id, user_id, role, source) ON memberships TO %I', runtime);
    EXECUTE format('GRANT SELECT, INSERT ON local_accounts TO %I', runtime);
    EXECUTE format('GRANT UPDATE (password_hash, password_change_required, origin, managing_tenant_id, updated_at) '
                   'ON local_accounts TO %I', runtime);
    EXECUTE format('GRANT SELECT, INSERT, DELETE ON login_attempts TO %I', runtime);
    EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON login_locks TO %I', runtime);
    EXECUTE format('GRANT INSERT (user_id, name, token_hash, scope, restricted_tenant_id, restricted_project_id, '
                   'agent, capabilities, expires_at) ON tokens TO %I', runtime);
END
$$;
