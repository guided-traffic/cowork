-- The comment thread of a ticket and the edit history of its comments
-- (docs/adr/0015 D1, D3).

-- A comment is written by a person, or by an agent in a person's name with
-- the agent mark (docs/adr/0015 D3, D4). It is never deleted: withdrawal
-- hides its text from every route and keeps the entry. Its text never enters
-- the audit record, which cannot forget it.
CREATE TABLE comments (
    id           uuid        PRIMARY KEY DEFAULT uuidv7(),
    tenant_id    uuid        NOT NULL,
    ticket_id    uuid        NOT NULL,
    author_id    uuid        NOT NULL REFERENCES users (id),
    agent        text,
    body         text        NOT NULL CHECK (length(btrim(body)) BETWEEN 1 AND 100000),
    withdrawn_by uuid        REFERENCES users (id),
    withdrawn_at timestamptz,
    search       tsvector    GENERATED ALWAYS AS (to_tsvector('cowork_simple', body)) STORED,
    version      integer     NOT NULL DEFAULT 1,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    -- Attachments on a comment reference the comment within its ticket.
    UNIQUE (tenant_id, ticket_id, id),
    FOREIGN KEY (tenant_id, ticket_id) REFERENCES tickets (tenant_id, id),
    CHECK ((withdrawn_by IS NULL) = (withdrawn_at IS NULL))
);
CREATE INDEX comments_by_ticket ON comments (tenant_id, ticket_id, id);
CREATE INDEX comments_search    ON comments USING gin (tenant_id, search);

-- The previous text of each edit (docs/adr/0015 D3).
CREATE TABLE comment_revisions (
    id         uuid        PRIMARY KEY DEFAULT uuidv7(),
    tenant_id  uuid        NOT NULL,
    comment_id uuid        NOT NULL,
    body       text        NOT NULL,
    edited_by  uuid        NOT NULL REFERENCES users (id),
    agent      text,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (tenant_id, comment_id) REFERENCES comments (tenant_id, id)
);
CREATE INDEX comment_revisions_by_comment ON comment_revisions (tenant_id, comment_id, id);

ALTER TABLE comments ENABLE ROW LEVEL SECURITY;
ALTER TABLE comments FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON comments
    USING (tenant_id = app_tenant_id())
    WITH CHECK (tenant_id = app_tenant_id());

ALTER TABLE comment_revisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE comment_revisions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON comment_revisions
    USING (tenant_id = app_tenant_id())
    WITH CHECK (tenant_id = app_tenant_id());

DO $$
DECLARE
    runtime text := current_setting('cowork.runtime_role');
BEGIN
    EXECUTE format('GRANT SELECT, INSERT ON comments TO %I', runtime);
    EXECUTE format('GRANT UPDATE (body, withdrawn_by, withdrawn_at, version, updated_at) ON comments TO %I', runtime);
    EXECUTE format('GRANT SELECT, INSERT ON comment_revisions TO %I', runtime);
END
$$;
