-- Open questions, entities of their own on a ticket (docs/adr/0011 D2).

CREATE TYPE question_status AS ENUM ('open', 'answered', 'withdrawn');

-- A question keeps its number within the ticket when others are withdrawn,
-- so the export's "### Q<n>:" is stable (docs/adr/0011 D4). asked_of NULL is
-- a question open in the tenant. Only a person decides an answer; an agent
-- that wrote a person's answer down sets recorded_by_agent, the person stays
-- the one who answered (docs/adr/0066 D8). asked_by_agent is the asking
-- request's agent mark: an agent withdraws only what an agent asked.
CREATE TABLE questions (
    id                uuid            PRIMARY KEY DEFAULT uuidv7(),
    tenant_id         uuid            NOT NULL,
    ticket_id         uuid            NOT NULL,
    number            integer         NOT NULL CHECK (number > 0),
    question          text            NOT NULL CHECK (length(btrim(question)) BETWEEN 1 AND 2000),
    options           text            NOT NULL DEFAULT '',
    recommendation    text            NOT NULL DEFAULT '',
    answer            text,
    status            question_status NOT NULL DEFAULT 'open',
    asked_by          uuid            NOT NULL REFERENCES users (id),
    asked_by_agent    text,
    asked_of          uuid            REFERENCES users (id),
    answered_by       uuid            REFERENCES users (id),
    answered_at       timestamptz,
    recorded_by_agent boolean         NOT NULL DEFAULT false,
    withdrawn_by      uuid            REFERENCES users (id),
    withdrawn_at      timestamptz,
    -- Searched like the ticket (docs/adr/0025 D2): the question weighted
    -- above what surrounds it.
    search            tsvector        GENERATED ALWAYS AS (
                          setweight(to_tsvector('cowork_simple', question), 'A') ||
                          setweight(to_tsvector('cowork_simple',
                              options || ' ' || recommendation || ' ' || coalesce(answer, '')), 'B')) STORED,
    version           integer         NOT NULL DEFAULT 1,
    created_at        timestamptz     NOT NULL DEFAULT now(),
    updated_at        timestamptz     NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, ticket_id, number),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, ticket_id) REFERENCES tickets (tenant_id, id),
    CHECK ((status = 'answered') = (answer IS NOT NULL AND answered_by IS NOT NULL AND answered_at IS NOT NULL)),
    CHECK ((status = 'withdrawn') = (withdrawn_by IS NOT NULL AND withdrawn_at IS NOT NULL)),
    CHECK (status = 'answered' OR NOT recorded_by_agent)
);
CREATE INDEX questions_by_ticket   ON questions (tenant_id, ticket_id, status);
CREATE INDEX questions_by_asked_of ON questions (tenant_id, asked_of, status);
CREATE INDEX questions_search      ON questions USING gin (tenant_id, search);

ALTER TABLE questions ENABLE ROW LEVEL SECURITY;
ALTER TABLE questions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON questions
    USING (tenant_id = app_tenant_id())
    WITH CHECK (tenant_id = app_tenant_id());

DO $$
DECLARE
    runtime text := current_setting('cowork.runtime_role');
BEGIN
    EXECUTE format('GRANT SELECT, INSERT ON questions TO %I', runtime);
    EXECUTE format('GRANT UPDATE (question, options, recommendation, answer, status, asked_of, answered_by, '
                   'answered_at, recorded_by_agent, withdrawn_by, withdrawn_at, version, updated_at) '
                   'ON questions TO %I', runtime);
END
$$;
