-- The state review, between in-progress and done: the check of the work
-- before it ends (docs/adr/0009 D1, D3).
--
-- Only the value is added here. PostgreSQL refuses a new enum value in the
-- transaction that adds it ("unsafe use of new value"), and a migration file
-- runs as one transaction, so what uses review — the CHECK on blocked_from,
-- the backfill of the progress stages — is migration 19. Adding a value
-- narrows nothing the previous release reads or writes (docs/adr/0028 D3): it
-- maps the state onto a string and never writes review itself.
ALTER TYPE ticket_state ADD VALUE 'review' AFTER 'in-progress';
