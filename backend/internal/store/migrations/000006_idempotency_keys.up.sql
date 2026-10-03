-- Idempotency keys (docs/adr/0045 D3, D4): the key a caller sent with a POST,
-- a fingerprint of the request, and the response, written in the same
-- transaction as the act. The key is scoped to the caller; until a login
-- exists the only caller is a token. A stored response never holds attachment
-- bytes or a secret (D6). Expired rows are removed by a job with a system
-- actor; nothing else deletes.

CREATE TABLE idempotency_keys (
    id               uuid        PRIMARY KEY DEFAULT uuidv7(),
    token_id         uuid        NOT NULL REFERENCES tokens (id),
    user_id          uuid        NOT NULL REFERENCES users (id),
    tenant_id        uuid        REFERENCES tenants (id),
    key              uuid        NOT NULL,
    fingerprint      bytea       NOT NULL CHECK (octet_length(fingerprint) = 32),
    response_status  smallint    NOT NULL,
    response_headers jsonb       NOT NULL DEFAULT '{}',
    response_body    bytea       NOT NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    expires_at       timestamptz NOT NULL,
    UNIQUE (token_id, key)
);
CREATE INDEX idempotency_expiry ON idempotency_keys (expires_at);

-- The caller's own rows; the expiry job reads and deletes everyone's, and only
-- in a transaction that names it.
ALTER TABLE idempotency_keys ENABLE ROW LEVEL SECURITY;
ALTER TABLE idempotency_keys FORCE ROW LEVEL SECURITY;
CREATE POLICY idempotency_owner ON idempotency_keys
    USING (user_id = app_user_id()
           OR current_setting('app.job', true) = 'idempotency-expiry')
    WITH CHECK (user_id = app_user_id());

DO $$
DECLARE
    runtime text := current_setting('cowork.runtime_role');
BEGIN
    EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON idempotency_keys TO %I', runtime);
END
$$;
