-- A person's weighted, reasoned stake in a ticket (docs/adr/0013).

CREATE TYPE interest_weight AS ENUM ('watch', 'need', 'urgent');

-- One row per person and ticket, changeable and removable (docs/adr/0013
-- D1); it outlives the ticket's work and shows as settled (D5). The note is
-- expected for need and urgent, not enforced.
CREATE TABLE ticket_interest (
    tenant_id  uuid            NOT NULL,
    ticket_id  uuid            NOT NULL,
    user_id    uuid            NOT NULL REFERENCES users (id),
    weight     interest_weight NOT NULL,
    note       text            NOT NULL DEFAULT '' CHECK (length(note) <= 2000),
    since      timestamptz     NOT NULL DEFAULT now(),
    updated_at timestamptz     NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, ticket_id, user_id),
    FOREIGN KEY (tenant_id, ticket_id) REFERENCES tickets (tenant_id, id)
);
CREATE INDEX ticket_interest_by_person ON ticket_interest (tenant_id, user_id);

ALTER TABLE ticket_interest ENABLE ROW LEVEL SECURITY;
ALTER TABLE ticket_interest FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON ticket_interest
    USING (tenant_id = app_tenant_id())
    WITH CHECK (tenant_id = app_tenant_id());

DO $$
DECLARE
    runtime text := current_setting('cowork.runtime_role');
BEGIN
    EXECUTE format('GRANT SELECT, INSERT, DELETE ON ticket_interest TO %I', runtime);
    EXECUTE format('GRANT UPDATE (weight, note, updated_at) ON ticket_interest TO %I', runtime);
END
$$;
