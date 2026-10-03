-- Browser sessions (docs/adr/0031) and the idempotency key of a session.
--
-- Only the SHA-256 of the cookie's 256 random bits is stored (D1); the cookie
-- is the secret and no log or audit row carries it or this row's id (D7).
-- The session table is not tenant-bound (docs/adr/0021 D6): its rows belong
-- to a person. user_agent_hash is the SHA-256 of the User-Agent the session
-- was created with (D1); nothing reads it yet. The groups snapshot of D1
-- arrives with the identity provider.
--
-- The timestamps are written by the backend from its clock, so the absolute
-- limit (expires_at) and the idle limit (last_seen_at plus the configured
-- idle time) are decided by one clock and a test can move it.
CREATE TABLE sessions (
    id              uuid        PRIMARY KEY DEFAULT uuidv7(),
    user_id         uuid        NOT NULL REFERENCES users (id),
    token_hash      bytea       NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    user_agent_hash bytea       CHECK (octet_length(user_agent_hash) = 32),
    created_at      timestamptz NOT NULL,
    last_seen_at    timestamptz NOT NULL,
    expires_at      timestamptz NOT NULL,
    CHECK (expires_at > created_at)
);
CREATE INDEX sessions_by_user   ON sessions (user_id);
CREATE INDEX sessions_by_expiry ON sessions (expires_at);
CREATE INDEX sessions_by_seen   ON sessions (last_seen_at);

-- A person reads and ends their own sessions, a global administrator reads
-- them (docs/adr/0031 D7). The request that presents a cookie finds exactly
-- that row through app.session_hash, which the resolver's transaction and
-- every request authenticated by the session set — the way a token is found
-- by app.token_hash. The administrators of a managed account end its
-- sessions; the expiry job and the start-up synchronisation (which ends the
-- sessions of an account whose password it changed or which it deactivated)
-- name themselves.
ALTER TABLE sessions ENABLE ROW LEVEL SECURITY;
ALTER TABLE sessions FORCE ROW LEVEL SECURITY;
CREATE POLICY sessions_read ON sessions FOR SELECT USING (
    user_id = app_user_id()
    OR token_hash = app_session_hash()
    OR app_manages_account(user_id)
    OR app_is_global_admin()
    OR app_job() IN ('session-expiry', 'bootstrap'));
CREATE POLICY sessions_insert ON sessions FOR INSERT
    WITH CHECK (user_id = app_user_id());
CREATE POLICY sessions_update ON sessions FOR UPDATE
    USING (user_id = app_user_id())
    WITH CHECK (user_id = app_user_id());
CREATE POLICY sessions_delete ON sessions FOR DELETE USING (
    user_id = app_user_id()
    OR token_hash = app_session_hash()
    OR app_manages_account(user_id)
    OR app_job() IN ('session-expiry', 'bootstrap'));

-- A key sent by a browser session is scoped to the person, as one sent with a
-- token is scoped to the token (docs/adr/0045 D3). token_id may now be NULL;
-- the earlier unique constraint stays, because the previous release's
-- INSERT … ON CONFLICT (token_id, key) needs it (docs/adr/0028).
ALTER TABLE idempotency_keys ALTER COLUMN token_id DROP NOT NULL;
CREATE UNIQUE INDEX idempotency_by_caller ON idempotency_keys (user_id, token_id, key) NULLS NOT DISTINCT;

DO $$
DECLARE
    runtime text := current_setting('cowork.runtime_role');
BEGIN
    EXECUTE format('GRANT SELECT, INSERT, DELETE ON sessions TO %I', runtime);
    EXECUTE format('GRANT UPDATE (last_seen_at) ON sessions TO %I', runtime);
END
$$;
