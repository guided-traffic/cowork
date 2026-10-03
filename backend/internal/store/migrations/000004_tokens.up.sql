-- Personal access tokens (docs/adr/0035, 0036, 0043).
--
-- Only the SHA-256 of a token is stored (docs/adr/0035 D1). Until a login
-- exists no route creates a token; the test fixture does (docs/adr/0038 D6).
-- The runtime role may read its person's tokens, find the one row whose hash
-- a request presents, and update the three columns revocation and the
-- last-used date touch — nothing that defines what a token may do.

CREATE TYPE token_scope AS ENUM ('read', 'write', 'admin');

CREATE TABLE tokens (
    id                    uuid        PRIMARY KEY DEFAULT uuidv7(),
    user_id               uuid        NOT NULL REFERENCES users (id),
    name                  text        NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 100),
    token_hash            bytea       NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    scope                 token_scope NOT NULL,
    -- Named restricted_*, not tenant_id: a token belongs to a person, not to a
    -- tenant, and its row is not tenant-bound (docs/adr/0021 D6).
    restricted_tenant_id  uuid        REFERENCES tenants (id),
    restricted_project_id uuid,
    -- The agent flag is the floor (docs/adr/0036 D2); an agent token never has
    -- admin scope (D5). Its capability set is fixed at creation
    -- (docs/adr/0043 D1); a plain token carries none, and a request it marks as
    -- an agent's with the header gets the default set, everything on (D4).
    agent                 boolean     NOT NULL DEFAULT false,
    capabilities          text[]      NOT NULL DEFAULT '{}',
    created_at            timestamptz NOT NULL DEFAULT now(),
    expires_at            timestamptz NOT NULL,
    last_used_on          date,
    revoked_at            timestamptz,
    -- The revoking person; NULL on a revoked row means a system act revoked it
    -- (the deactivation of its person, docs/adr/0024 D5).
    revoked_by            uuid        REFERENCES users (id),
    FOREIGN KEY (restricted_tenant_id, restricted_project_id) REFERENCES projects (tenant_id, id),
    CHECK (expires_at > created_at),
    CHECK (restricted_project_id IS NULL OR restricted_tenant_id IS NOT NULL),
    CHECK (NOT (agent AND scope = 'admin')),
    CHECK (agent OR cardinality(capabilities) = 0),
    CHECK (capabilities <@ ARRAY['decide', 'close', 'drop', 'rank', 'override-urgency', 'interest',
                                 'upload', 'create-project', 'record-answer']::text[]),
    CHECK (revoked_by IS NULL OR revoked_at IS NOT NULL)
);
CREATE INDEX tokens_by_user ON tokens (user_id);

-- The resolver finds a token by the hash the request presents before it knows
-- the person; the lookup transaction sets app.token_hash, and the policy then
-- admits exactly the row whose secret the caller holds.
ALTER TABLE tokens ENABLE ROW LEVEL SECURITY;
ALTER TABLE tokens FORCE ROW LEVEL SECURITY;
CREATE POLICY tokens_read ON tokens FOR SELECT
    USING (user_id = app_user_id()
           OR token_hash = decode(NULLIF(current_setting('app.token_hash', true), ''), 'hex'));
CREATE POLICY tokens_update ON tokens FOR UPDATE
    USING (user_id = app_user_id())
    WITH CHECK (user_id = app_user_id());

-- Revocation is final (docs/adr/0035 D6). The runtime role may set
-- revoked_at, so a column grant alone would let a compromised serving process
-- clear it again and bring a leaked token back; the owner's trigger refuses
-- every change to a revocation, and the runtime role can neither drop nor
-- disable it.
CREATE FUNCTION tokens_revocation_is_final() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        IF OLD.revoked_at IS NOT NULL AND (NEW.revoked_at IS DISTINCT FROM OLD.revoked_at
                                            OR NEW.revoked_by IS DISTINCT FROM OLD.revoked_by) THEN
            RAISE EXCEPTION 'a revoked token stays revoked' USING ERRCODE = 'check_violation';
        END IF;
        RETURN NEW;
    END
    $$;
CREATE TRIGGER tokens_revocation_is_final BEFORE UPDATE ON tokens
    FOR EACH ROW EXECUTE FUNCTION tokens_revocation_is_final();

DO $$
DECLARE
    runtime text := current_setting('cowork.runtime_role');
BEGIN
    EXECUTE format('GRANT SELECT ON tokens TO %I', runtime);
    EXECUTE format('GRANT UPDATE (revoked_at, revoked_by, last_used_on) ON tokens TO %I', runtime);
END
$$;
