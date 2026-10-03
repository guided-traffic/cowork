-- The audit record: one append-only table for every mutation of every entity
-- (docs/adr/0026).
--
-- Append-only is a grant (D3): the runtime role may insert and read, nothing
-- else, and it owns nothing it could re-grant itself (docs/adr/0021 D2).
-- No foreign key points from here to a ticket or comment, and no cascade
-- points into here: a purged ticket's rows survive with its key
-- (docs/adr/0024 D2).

CREATE TYPE audit_action AS ENUM (
    'created', 'updated', 'transitioned', 'linked', 'unlinked', 'commented', 'edited',
    'withdrawn', 'assigned', 'interest', 'ranked', 'overridden', 'asked', 'answered',
    'booked', 'voided', 'locked', 'uploaded', 'downloaded', 'exported', 'deleted',
    'restored', 'purged', 'revoked', 'refused', 'archived', 'confidential_set',
    'confidential_lifted', 'expired'
);

CREATE TABLE audit_events (
    id                      uuid         PRIMARY KEY DEFAULT uuidv7(),
    -- NULL for an installation-level act (docs/adr/0026 D1).
    tenant_id               uuid         REFERENCES tenants (id),
    -- A person, or a system actor such as system:idempotency-expiry, never
    -- both and never neither (docs/adr/0026 D1, docs/adr/0027 D5).
    actor_user_id           uuid         REFERENCES users (id),
    actor_system            text         CHECK (actor_system ~ '^system:[a-z][a-z0-9-]{0,62}$'),
    -- The agent mark of the request, or unknown-agent (docs/adr/0036 D3, D4),
    -- and the capability set that applied (docs/adr/0043 D5).
    agent                   text,
    agent_capabilities      text[],
    token_id                uuid         REFERENCES tokens (id),
    entity_type             text         NOT NULL,
    entity_id               uuid,
    ticket_id               uuid,
    ticket_key              text,
    action                  audit_action NOT NULL,
    before                  jsonb,
    after                   jsonb,
    reason                  text,
    note                    text,
    explained_by_comment_id uuid,
    -- The other tickets the payload names: a link's other end, the ticket a
    -- block waits on. A reader who cannot see one of them reads the act
    -- without its payload, so an act never shows what the predicate hides
    -- (docs/adr/0065 D4).
    refs                    uuid[]       NOT NULL DEFAULT '{}',
    idempotency_key         uuid,
    request_id              uuid,
    created_at              timestamptz  NOT NULL DEFAULT now(),
    CHECK ((actor_user_id IS NULL) <> (actor_system IS NULL)),
    CHECK (agent_capabilities IS NULL OR agent IS NOT NULL)
);
CREATE INDEX audit_by_tenant ON audit_events (tenant_id, id);
CREATE INDEX audit_by_ticket ON audit_events (tenant_id, ticket_id, id) WHERE ticket_id IS NOT NULL;
CREATE INDEX audit_by_token  ON audit_events (tenant_id, token_id, id) WHERE token_id IS NOT NULL;
CREATE INDEX audit_by_actor  ON audit_events (tenant_id, actor_user_id, id);
CREATE INDEX audit_by_explaining_comment ON audit_events (tenant_id, explained_by_comment_id)
    WHERE explained_by_comment_id IS NOT NULL;
CREATE INDEX audit_installation_by_token ON audit_events (token_id, action, id)
    WHERE tenant_id IS NULL;

-- A tenant's rows inside the tenant; an installation-level row to the person
-- it names. A row is written only for the context it belongs to, so a tenant
-- act is never filed as an installation-level one.
ALTER TABLE audit_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit_events FORCE ROW LEVEL SECURITY;
CREATE POLICY audit_read ON audit_events FOR SELECT
    USING (tenant_id = app_tenant_id()
           OR (tenant_id IS NULL AND actor_user_id = app_user_id()));
CREATE POLICY audit_insert ON audit_events FOR INSERT
    WITH CHECK (tenant_id IS NOT DISTINCT FROM app_tenant_id());

DO $$
DECLARE
    runtime text := current_setting('cowork.runtime_role');
BEGIN
    EXECUTE format('GRANT SELECT, INSERT ON audit_events TO %I', runtime);
END
$$;
