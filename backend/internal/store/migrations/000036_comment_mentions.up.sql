-- A comment's mentions (docs/adr/0015 D5 as amended 2026-10-05): the persons a
-- comment names, as a list of person ids beside its text, written with the
-- comment and replaced by an edit that sends a list. The handler checks each
-- id like a question's asked_of — a member of the tenant who sees the ticket —
-- so the list never holds a person who could not read the comment when it was
-- written. A mentioned person is told (docs/adr/0020 D2) and is a watcher of
-- the ticket while the comment stands (docs/adr/0013 D6).
--
-- Expand only (docs/adr/0028): a column with a default and an enum value; the
-- release before this one reads neither. A new enum value cannot be used in
-- the transaction that adds it; nothing in this file uses it.

ALTER TYPE notification_reason ADD VALUE IF NOT EXISTS 'mentioned';

ALTER TABLE comments ADD COLUMN mentions uuid[] NOT NULL DEFAULT '{}'
    CHECK (cardinality(mentions) <= 50);

DO $$
DECLARE
    runtime text := current_setting('cowork.runtime_role');
BEGIN
    EXECUTE format('GRANT UPDATE (mentions) ON comments TO %I', runtime);
END
$$;
