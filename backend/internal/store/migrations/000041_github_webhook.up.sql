-- The inbound GitHub webhook of docs/adr/0071, optional per tenant and on
-- trial: a tenant's webhook secret, sealed at rest; the deliveries, kept a day
-- so a repetition changes nothing; and the pull requests and default-branch
-- commits a delivery names a ticket in, under the ticket's sight.
--
-- Expand only (docs/adr/0028 D3): three new tables, enum values, and a read of
-- the tenants for the webhook's job, none of which the previous release meets.
-- A new enum value cannot be used in the transaction that adds it; nothing in
-- this file uses the values it adds.

-- A pull request's state changes are acts on the ticket it names
-- (docs/adr/0071 D6); its first link is linked, a person's removal unlinked.
ALTER TYPE audit_action ADD VALUE IF NOT EXISTS 'merged';
ALTER TYPE audit_action ADD VALUE IF NOT EXISTS 'closed';
ALTER TYPE audit_action ADD VALUE IF NOT EXISTS 'reopened';

-- A merge tells the assignee and the watchers (docs/adr/0020 D2 as amended
-- 2026-10-06).
ALTER TYPE notification_reason ADD VALUE IF NOT EXISTS 'merged';

-- The tenant's webhook secret (D1): made by an administrator, shown once,
-- replaced by a rotation and removed by a revocation. The server recomputes
-- the HMAC of every delivery with it, so it is kept sealed — AES-256-GCM under
-- a key derived from COWORK_SESSION_KEY, bound to the tenant — never as a hash.
CREATE TABLE github_webhook_secrets (
    tenant_id  uuid        PRIMARY KEY REFERENCES tenants (id),
    secret     bytea       NOT NULL CHECK (octet_length(secret) BETWEEN 29 AND 512),
    created_by uuid        NOT NULL REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now()
);

-- The deliveries of the last twenty-four hours (D3), by GitHub's delivery id:
-- a delivery seen again is answered without effect. The expiry job removes
-- what is older.
CREATE TABLE github_deliveries (
    id          uuid        PRIMARY KEY DEFAULT uuidv7(),
    tenant_id   uuid        NOT NULL REFERENCES tenants (id),
    delivery    uuid        NOT NULL,
    received_at timestamptz NOT NULL,
    expires_at  timestamptz NOT NULL,
    UNIQUE (tenant_id, delivery)
);
CREATE INDEX github_deliveries_expiry ON github_deliveries (expires_at);

-- What a ticket gains (D6): a pull request that names it in its title or
-- body, or a commit on the repository's default branch that names it in its
-- message — one row per ticket and pull request or commit, the facts of the
-- pull request repeated on each of its rows. found_in says where the key was
-- read; first and last seen when a delivery told of it. A person's removal of
-- a wrong link keeps the row, so a later delivery does not bring it back.
CREATE TABLE ticket_pull_requests (
    id                uuid        PRIMARY KEY DEFAULT uuidv7(),
    tenant_id         uuid        NOT NULL,
    ticket_id         uuid        NOT NULL,
    kind              text        NOT NULL CHECK (kind IN ('pull_request', 'commit')),
    repository        text        NOT NULL CHECK (length(repository) BETWEEN 3 AND 512),
    number            integer     CHECK (number > 0),
    sha               text        CHECK (sha ~ '^[0-9a-f]{40}([0-9a-f]{24})?$'),
    title             text        NOT NULL CHECK (length(title) <= 1000),
    state             text        NOT NULL CHECK (state IN ('open', 'closed', 'merged')),
    url               text        NOT NULL CHECK (url LIKE 'https://%' AND length(url) <= 2000),
    author            text        CHECK (length(author) BETWEEN 1 AND 100),
    merged_at         timestamptz,
    found_in          text        NOT NULL CHECK (found_in IN ('trailer', 'body', 'subject')),
    source_updated_at timestamptz,
    first_seen_at     timestamptz NOT NULL,
    last_seen_at      timestamptz NOT NULL,
    removed_at        timestamptz,
    removed_by        uuid        REFERENCES users (id),
    CHECK ((kind = 'pull_request') = (number IS NOT NULL)),
    CHECK ((kind = 'commit') = (sha IS NOT NULL)),
    CHECK ((removed_at IS NULL) = (removed_by IS NULL)),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, ticket_id) REFERENCES tickets (tenant_id, id)
);
CREATE UNIQUE INDEX ticket_pull_requests_by_number ON ticket_pull_requests (tenant_id, ticket_id, repository, number)
    WHERE kind = 'pull_request';
CREATE UNIQUE INDEX ticket_pull_requests_by_sha ON ticket_pull_requests (tenant_id, ticket_id, repository, sha)
    WHERE kind = 'commit';
CREATE INDEX ticket_pull_requests_by_ticket ON ticket_pull_requests (tenant_id, ticket_id, id);
CREATE INDEX ticket_pull_requests_of_pull_request ON ticket_pull_requests (tenant_id, repository, number)
    WHERE kind = 'pull_request';

-- The secret: the tenant's own row, and of it only its administrators read,
-- make, rotate and revoke it — and the webhook's job reads it to verify a
-- delivery. Restrictive policies are ANDed with the canonical one, so no
-- later permissive policy widens who touches a secret.
ALTER TABLE github_webhook_secrets ENABLE ROW LEVEL SECURITY;
ALTER TABLE github_webhook_secrets FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON github_webhook_secrets
    USING (tenant_id = app_tenant_id())
    WITH CHECK (tenant_id = app_tenant_id());
CREATE POLICY github_webhook_secrets_read ON github_webhook_secrets AS RESTRICTIVE FOR SELECT
    USING (app_is_tenant_admin() OR app_job() = 'github-webhook');
CREATE POLICY github_webhook_secrets_insert ON github_webhook_secrets AS RESTRICTIVE FOR INSERT
    WITH CHECK (app_is_tenant_admin());
CREATE POLICY github_webhook_secrets_update ON github_webhook_secrets AS RESTRICTIVE FOR UPDATE
    USING (app_is_tenant_admin())
    WITH CHECK (app_is_tenant_admin());
CREATE POLICY github_webhook_secrets_delete ON github_webhook_secrets AS RESTRICTIVE FOR DELETE
    USING (app_is_tenant_admin());

-- The deliveries: written by the webhook's job in the tenant's transaction,
-- read and deleted past the tenant by the expiry job only.
ALTER TABLE github_deliveries ENABLE ROW LEVEL SECURITY;
ALTER TABLE github_deliveries FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON github_deliveries
    USING (tenant_id = app_tenant_id())
    WITH CHECK (tenant_id = app_tenant_id());
CREATE POLICY github_deliveries_expiry ON github_deliveries FOR SELECT
    USING (app_job() = 'github-delivery-expiry');
CREATE POLICY github_deliveries_expiry_delete ON github_deliveries FOR DELETE
    USING (app_job() = 'github-delivery-expiry');
CREATE POLICY github_deliveries_webhook_insert ON github_deliveries AS RESTRICTIVE FOR INSERT
    WITH CHECK (app_job() = 'github-webhook');
CREATE POLICY github_deliveries_webhook_update ON github_deliveries AS RESTRICTIVE FOR UPDATE
    USING (app_job() = 'github-webhook')
    WITH CHECK (app_job() = 'github-webhook');

-- The pull requests: the tenant's rows, read through the ticket's predicate by
-- every query (docs/adr/0065 D1). Only the webhook makes a row — no person's
-- request writes a pull request onto a ticket —, and only the purge of a
-- deleted ticket deletes one (migration 32); a person's removal is an update.
ALTER TABLE ticket_pull_requests ENABLE ROW LEVEL SECURITY;
ALTER TABLE ticket_pull_requests FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON ticket_pull_requests
    USING (tenant_id = app_tenant_id())
    WITH CHECK (tenant_id = app_tenant_id());
CREATE POLICY ticket_pull_requests_webhook_insert ON ticket_pull_requests AS RESTRICTIVE FOR INSERT
    WITH CHECK (app_job() = 'github-webhook');
CREATE POLICY ticket_pull_requests_purge ON ticket_pull_requests AS RESTRICTIVE FOR DELETE
    USING (app_job() = 'ticket-purge' AND app_ticket_deleted(tenant_id, ticket_id));

-- tenants: the webhook finds the tenant its path names before anything else,
-- in a transaction of its job that names no person (D2). The rest of the
-- policy is migration 26's.
ALTER POLICY tenants_read ON tenants USING (
    id = app_tenant_id()
    OR EXISTS (SELECT 1 FROM memberships m WHERE m.tenant_id = tenants.id AND m.user_id = app_user_id())
    OR app_job() IN ('login', 'bootstrap', 'identity-provider', 'github-webhook')
    OR app_is_global_admin());

DO $$
DECLARE
    runtime text := current_setting('cowork.runtime_role');
BEGIN
    EXECUTE format('GRANT SELECT, INSERT, DELETE ON github_webhook_secrets TO %I', runtime);
    EXECUTE format('GRANT UPDATE (secret, created_by, created_at) ON github_webhook_secrets TO %I', runtime);
    EXECUTE format('GRANT SELECT, INSERT, DELETE ON github_deliveries TO %I', runtime);
    EXECUTE format('GRANT UPDATE (received_at, expires_at) ON github_deliveries TO %I', runtime);
    EXECUTE format('GRANT SELECT, INSERT, DELETE ON ticket_pull_requests TO %I', runtime);
    EXECUTE format('GRANT UPDATE (title, state, url, author, merged_at, source_updated_at, last_seen_at, '
                   'removed_at, removed_by) ON ticket_pull_requests TO %I', runtime);
END
$$;
