-- Typed, directed links between tickets of one tenant (docs/adr/0012).

CREATE TYPE link_type AS ENUM ('blocks', 'relates-to', 'duplicates', 'found-in');

-- A link is a recorded act with its creator and time (docs/adr/0012 D3) and
-- carries no version: it is created or removed, never edited (docs/adr/0050
-- D4). Both ends are tickets of the link's tenant by the composite keys, so
-- a link never crosses a tenant (docs/adr/0012 D2), whatever the API does.
CREATE TABLE ticket_links (
    id         uuid        PRIMARY KEY DEFAULT uuidv7(),
    tenant_id  uuid        NOT NULL,
    type       link_type   NOT NULL,
    source_id  uuid        NOT NULL,
    target_id  uuid        NOT NULL,
    created_by uuid        NOT NULL REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (tenant_id, source_id) REFERENCES tickets (tenant_id, id),
    FOREIGN KEY (tenant_id, target_id) REFERENCES tickets (tenant_id, id),
    -- No link from a ticket to itself; one link of a type between two
    -- tickets in one direction (docs/adr/0012 D4).
    CHECK (source_id <> target_id),
    UNIQUE (tenant_id, type, source_id, target_id),
    -- relates-to is symmetric and stored once, the smaller id first.
    CHECK (type <> 'relates-to' OR source_id < target_id)
);
CREATE INDEX ticket_links_by_target ON ticket_links (tenant_id, target_id, type);
CREATE INDEX ticket_links_by_source ON ticket_links (tenant_id, source_id, type);

ALTER TABLE ticket_links ENABLE ROW LEVEL SECURITY;
ALTER TABLE ticket_links FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON ticket_links
    USING (tenant_id = app_tenant_id())
    WITH CHECK (tenant_id = app_tenant_id());

-- Whether p_to is reachable from p_from over blocks links: adding the link
-- p_to blocks p_from would close a cycle (docs/adr/0012 D4). Like the parent
-- walk it walks the tenant's whole graph past the visibility predicate and
-- answers yes or no only.
CREATE FUNCTION blocks_path_exists(p_tenant_id uuid, p_from uuid, p_to uuid) RETURNS boolean
    LANGUAGE sql STABLE
    AS $$
        WITH RECURSIVE reach (id) AS (
            SELECT p_from
            UNION
            SELECT l.target_id
            FROM ticket_links l JOIN reach r ON l.tenant_id = p_tenant_id AND l.source_id = r.id
            WHERE l.type = 'blocks'
        )
        SELECT EXISTS (SELECT 1 FROM reach WHERE id = p_to)
    $$;

DO $$
DECLARE
    runtime text := current_setting('cowork.runtime_role');
BEGIN
    EXECUTE format('GRANT SELECT, INSERT, DELETE ON ticket_links TO %I', runtime);
END
$$;
