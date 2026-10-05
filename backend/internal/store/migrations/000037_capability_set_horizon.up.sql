-- The capability set-horizon is the name override-urgency takes, as the API
-- names the ticket's horizon by its word (docs/adr/0043 D4 and docs/adr/0010 D1
-- as amended 2026-10-05). Expand before contract (docs/adr/0028 D3): the sets
-- of the tokens and of the chat take both names. This release writes
-- set-horizon and reads either name as it; the rows written before keep
-- override-urgency, which the release before reads. A later release rewrites
-- the stored sets to set-horizon and drops override-urgency from these checks.
--
-- Migrations 4 and 24 left the checks unnamed; PostgreSQL named each after its
-- table and its one column.

ALTER TABLE tokens DROP CONSTRAINT tokens_capabilities_check;
ALTER TABLE tokens ADD CONSTRAINT tokens_capabilities_check
    CHECK (capabilities <@ ARRAY['decide', 'close', 'drop', 'rank', 'set-horizon', 'interest', 'upload',
                                 'create-project', 'record-answer', 'override-urgency']::text[]);

ALTER TABLE chat_capabilities DROP CONSTRAINT chat_capabilities_capabilities_check;
ALTER TABLE chat_capabilities ADD CONSTRAINT chat_capabilities_capabilities_check
    CHECK (capabilities <@ ARRAY['decide', 'close', 'drop', 'rank', 'set-horizon', 'interest', 'upload',
                                 'create-project', 'record-answer', 'override-urgency']::text[]);
