-- The lengths of a ticket's body and of a question's options and answer,
-- held by the database as the API holds every write of them (docs/adr/0011
-- D6, docs/adr/0051 D7): a body of at most 200,000 characters, the options
-- and the answer of at most 100,000 each — the bound a comment has had since
-- migration 11. A text the API takes is counted in UTF-16 code units, which
-- are never fewer than the characters length counts here.
--
-- Expand only (docs/adr/0028 D3): every write of the previous release keeps
-- to these lengths but its import's, which this release holds to them too.
-- Each check is added NOT VALID and then validated, which reads every row: a
-- row that is longer fails the migration, which then leaves nothing behind,
-- and is shortened before the upgrade is run again (docs/operations/
-- installation.md, Upgrade).

ALTER TABLE tickets
    ADD CONSTRAINT tickets_body_length_check CHECK (length(body) <= 200000) NOT VALID;
ALTER TABLE questions
    ADD CONSTRAINT questions_options_length_check CHECK (length(options) <= 100000) NOT VALID,
    ADD CONSTRAINT questions_answer_length_check CHECK (length(answer) <= 100000) NOT VALID;

ALTER TABLE tickets VALIDATE CONSTRAINT tickets_body_length_check;
ALTER TABLE questions VALIDATE CONSTRAINT questions_options_length_check;
ALTER TABLE questions VALIDATE CONSTRAINT questions_answer_length_check;
