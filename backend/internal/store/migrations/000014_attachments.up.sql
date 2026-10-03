-- Attachment metadata (docs/adr/0016 D1); the bytes live in object storage
-- under <tenant-id>/<attachment-id>, derived and never stored.

-- An attachment never changes, so it carries no version (docs/adr/0050 D4).
-- The type is the one the server detected, on the allow-list
-- (docs/adr/0016 D3). A comment's attachment references the comment within
-- the same ticket.
CREATE TABLE attachments (
    id           uuid        PRIMARY KEY DEFAULT uuidv7(),
    tenant_id    uuid        NOT NULL,
    ticket_id    uuid        NOT NULL,
    comment_id   uuid,
    file_name    text        NOT NULL CHECK (length(file_name) BETWEEN 1 AND 255),
    size         bigint      NOT NULL CHECK (size >= 0),
    sha256       bytea       NOT NULL CHECK (length(sha256) = 32),
    content_type text        NOT NULL CHECK (content_type IN ('image/png', 'image/jpeg', 'image/gif', 'image/webp',
                                                              'application/pdf', 'text/plain; charset=utf-8',
                                                              'image/svg+xml')),
    uploaded_by  uuid        NOT NULL REFERENCES users (id),
    agent        text,
    search       tsvector    GENERATED ALWAYS AS (to_tsvector('cowork_simple', file_name)) STORED,
    created_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, ticket_id) REFERENCES tickets (tenant_id, id),
    FOREIGN KEY (tenant_id, ticket_id, comment_id) REFERENCES comments (tenant_id, ticket_id, id)
);
CREATE INDEX attachments_by_ticket ON attachments (tenant_id, ticket_id, id);
CREATE INDEX attachments_search    ON attachments USING gin (tenant_id, search);

ALTER TABLE attachments ENABLE ROW LEVEL SECURITY;
ALTER TABLE attachments FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON attachments
    USING (tenant_id = app_tenant_id())
    WITH CHECK (tenant_id = app_tenant_id());

DO $$
DECLARE
    runtime text := current_setting('cowork.runtime_role');
BEGIN
    EXECUTE format('GRANT SELECT, INSERT ON attachments TO %I', runtime);
END
$$;
