-- Tickets (docs/adr/0007–0011, 0017, 0065).
--
-- The extensions and the text search configuration come first, in the first
-- migration that needs them (docs/adr/0025 Consequences); the owner role
-- creates them, or the installation has created them beforehand
-- (docs/adr/0058 D5). unaccent cannot sit in a generated column itself — it
-- is not immutable — so it is a filter dictionary of the configuration
-- (docs/adr/0025 D2).
CREATE EXTENSION IF NOT EXISTS unaccent;
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE EXTENSION IF NOT EXISTS btree_gin;
CREATE TEXT SEARCH CONFIGURATION cowork_simple (COPY = simple);
ALTER TEXT SEARCH CONFIGURATION cowork_simple
    ALTER MAPPING FOR hword, hword_part, word WITH unaccent, simple;

CREATE TYPE ticket_type AS ENUM ('task', 'bug', 'feature', 'decision', 'question');
CREATE TYPE ticket_state AS ENUM ('filed', 'analysed', 'decided', 'in-progress', 'blocked', 'done', 'dropped');
CREATE TYPE block_kind AS ENUM ('decision', 'human', 'product', 'release', 'external', 'ticket');
CREATE TYPE severity AS ENUM ('critical', 'high', 'medium', 'low', 'cosmetic');
CREATE TYPE security_class AS ENUM ('live', 'boundary', 'hardening', 'none');
CREATE TYPE urgency AS ENUM ('now', 'release', 'next', 'later', 'icebox');
CREATE TYPE effort AS ENUM ('XS', 'S', 'M', 'L');

CREATE TABLE tickets (
    id                      uuid           PRIMARY KEY DEFAULT uuidv7(),
    tenant_id               uuid           NOT NULL,
    project_id              uuid           NOT NULL,
    -- The key is <tenant>/<PROJECT>-<number>; only the number is stored, and
    -- it is never reused (docs/adr/0007 D3, D4).
    number                  integer        NOT NULL CHECK (number > 0),
    type                    ticket_type    NOT NULL,
    title                   text           NOT NULL CHECK (length(btrim(title)) BETWEEN 1 AND 300),
    body                    text           NOT NULL DEFAULT '',
    state                   ticket_state   NOT NULL DEFAULT 'filed',
    -- blocked remembers where it came from and why (docs/adr/0009 D2).
    blocked_from            ticket_state   CHECK (blocked_from IN ('filed', 'analysed', 'decided', 'in-progress')),
    block_kind              block_kind,
    block_reason            text,
    block_ticket_id         uuid,
    block_external_ref      text,
    severity                severity       NOT NULL,
    security                security_class NOT NULL,
    threat                  text,
    -- urgency is derived by the rule set the stored rule names, and may be
    -- overridden with a reason until an input changes (docs/adr/0010 D3).
    urgency_derived         urgency        NOT NULL,
    urgency_rule            text           NOT NULL,
    urgency_override        urgency,
    urgency_override_reason text,
    urgency_override_by     uuid           REFERENCES users (id),
    urgency_override_at     timestamptz,
    effort                  effort         NOT NULL,
    progress                smallint       NOT NULL DEFAULT 0
                                           CHECK (progress BETWEEN 0 AND 100 AND progress % 5 = 0),
    -- The progress derived from the children while there are any
    -- (docs/adr/0017 D3), NULL without children; it overrides progress.
    progress_derived        smallint       CHECK (progress_derived BETWEEN 0 AND 100 AND progress_derived % 5 = 0),
    parent_id               uuid,
    reporter_id             uuid           NOT NULL REFERENCES users (id),
    assignee_id             uuid           REFERENCES users (id),
    -- Visible to the tenant's administrators, the assignee and the reporter
    -- only (docs/adr/0065 D1).
    confidential            boolean        NOT NULL DEFAULT false,
    opened_at               timestamptz    NOT NULL DEFAULT now(),
    decided_at              timestamptz,
    done_at                 timestamptz,
    search                  tsvector       GENERATED ALWAYS AS (
                                setweight(to_tsvector('cowork_simple', title), 'A') ||
                                setweight(to_tsvector('cowork_simple', body), 'B')) STORED,
    version                 integer        NOT NULL DEFAULT 1,
    created_at              timestamptz    NOT NULL DEFAULT now(),
    updated_at              timestamptz    NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, project_id, number),
    UNIQUE (tenant_id, id),
    UNIQUE (tenant_id, project_id, id),
    FOREIGN KEY (tenant_id, project_id) REFERENCES projects (tenant_id, id),
    -- A parent is a ticket of the same project (docs/adr/0008 D2).
    FOREIGN KEY (tenant_id, project_id, parent_id) REFERENCES tickets (tenant_id, project_id, id),
    FOREIGN KEY (tenant_id, block_ticket_id) REFERENCES tickets (tenant_id, id),
    CHECK (parent_id IS DISTINCT FROM id),
    CHECK ((state = 'blocked') = (blocked_from IS NOT NULL AND block_kind IS NOT NULL AND block_reason IS NOT NULL)),
    CHECK (block_kind IS DISTINCT FROM 'ticket' OR block_ticket_id IS NOT NULL),
    -- threat is required with a security class and absent without one
    -- (docs/adr/0010 D2).
    CHECK ((security = 'none' AND threat IS NULL) OR (security <> 'none' AND length(btrim(threat)) > 0)),
    CHECK ((urgency_override IS NULL) = (urgency_override_reason IS NULL)),
    CHECK ((urgency_override IS NULL) = (urgency_override_at IS NULL))
);
CREATE INDEX tickets_by_state    ON tickets (tenant_id, project_id, state);
CREATE INDEX tickets_by_assignee ON tickets (tenant_id, assignee_id);
CREATE INDEX tickets_by_reporter ON tickets (tenant_id, reporter_id);
CREATE INDEX tickets_by_parent   ON tickets (tenant_id, parent_id);
CREATE INDEX tickets_search      ON tickets USING gin (tenant_id, search);
CREATE INDEX tickets_title_trgm  ON tickets USING gin (tenant_id, title gin_trgm_ops);

ALTER TABLE tickets ENABLE ROW LEVEL SECURITY;
ALTER TABLE tickets FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON tickets
    USING (tenant_id = app_tenant_id())
    WITH CHECK (tenant_id = app_tenant_id());

-- A ticket is visible when its project is, and — while it is confidential —
-- to the tenant's administrators, its assignee and its reporter only
-- (docs/adr/0034 D4, docs/adr/0065 D4). Every query on tickets and what
-- belongs to them calls it.
CREATE FUNCTION app_ticket_visible(p_project_id uuid, p_confidential boolean, p_assignee_id uuid, p_reporter_id uuid)
    RETURNS boolean
    LANGUAGE sql STABLE PARALLEL SAFE
    AS $$
        SELECT app_project_visible(p_project_id)
           AND (NOT p_confidential
                OR app_is_tenant_admin()
                OR p_assignee_id = app_user_id()
                OR p_reporter_id = app_user_id())
    $$;

-- Whether p_target is p_from or one of its ancestors: the parent cycle
-- refusal (docs/adr/0008 D2). It walks the tenant's tickets past the
-- visibility predicate, as an integrity check must, and answers yes or no
-- only; row-level security still holds it to the tenant.
CREATE FUNCTION ticket_ancestor_or_self(p_tenant_id uuid, p_from uuid, p_target uuid) RETURNS boolean
    LANGUAGE sql STABLE
    AS $$
        WITH RECURSIVE ancestors (id, parent_id, depth) AS (
            SELECT id, parent_id, 1 FROM tickets WHERE tenant_id = p_tenant_id AND id = p_from
            UNION ALL
            SELECT t.id, t.parent_id, a.depth + 1
            FROM tickets t JOIN ancestors a ON t.tenant_id = p_tenant_id AND t.id = a.parent_id
            WHERE a.depth < 100000
        )
        SELECT EXISTS (SELECT 1 FROM ancestors WHERE id = p_target)
    $$;

-- The progress a ticket's children give it (docs/adr/0017 D3): the mean of
-- their progress weighted by effort (XS 1, S 2, M 3, L 5), a dropped child
-- left out, a done child counted as 100, rounded to the nearest five with
-- halves up; 0 when every child is dropped; NULL without children. A child's
-- progress is its own derived value when it has children itself. Like the
-- urgency it is the ticket's own value and reads children the caller may not
-- see.
CREATE FUNCTION ticket_derived_progress(p_tenant_id uuid, p_id uuid) RETURNS smallint
    LANGUAGE sql STABLE
    AS $$
        SELECT CASE
                   WHEN count(*) = 0 THEN NULL
                   WHEN coalesce(sum(w) FILTER (WHERE c.state <> 'dropped'), 0) = 0 THEN 0
                   ELSE (floor(sum(w * CASE WHEN c.state = 'done' THEN 100
                                            ELSE coalesce(c.progress_derived, c.progress) END)
                                   FILTER (WHERE c.state <> 'dropped')::numeric
                               / sum(w) FILTER (WHERE c.state <> 'dropped') / 5 + 0.5) * 5)::smallint
               END
        FROM tickets c
        CROSS JOIN LATERAL (SELECT CASE c.effort WHEN 'XS' THEN 1 WHEN 'S' THEN 2 WHEN 'M' THEN 3 ELSE 5 END AS w) e
        WHERE c.tenant_id = p_tenant_id AND c.parent_id = p_id
    $$;

DO $$
DECLARE
    runtime text := current_setting('cowork.runtime_role');
BEGIN
    EXECUTE format('GRANT SELECT, INSERT ON tickets TO %I', runtime);
    EXECUTE format('GRANT UPDATE (type, title, body, state, blocked_from, block_kind, block_reason, '
                   'block_ticket_id, block_external_ref, severity, security, threat, urgency_derived, '
                   'urgency_rule, urgency_override, urgency_override_reason, urgency_override_by, '
                   'urgency_override_at, effort, progress, progress_derived, parent_id, assignee_id, confidential, '
                   'decided_at, done_at, version, updated_at) ON tickets TO %I', runtime);
END
$$;
