-- Every act made through a personal access token is shown as such
-- (docs/adr/0036 D6, the owner's decision of 2026-10-04): beside the agent
-- mark, the rows a ticket shows as somebody's act record the token the act
-- came through — its id, and its name as the token had it. The name is copied
-- at write time, not joined at read time: the tokens policy admits a person's
-- own tokens only (docs/adr/0021 D6), so a member reading another person's
-- comment could not read its token's row; and a token's name never changes —
-- the runtime role may update revoked_at, revoked_by and last_used_on of a
-- token and nothing else (migration 4) — so a revoked token's acts keep
-- reading the name it had. Never the secret, its hash or a part of either.
--
-- Expand only (docs/adr/0028 D3): nullable columns the previous release never
-- writes, and checks it cannot break. A browser session's act carries none of
-- them — the chat's included, which its agent mark tells apart. A row written
-- before this migration carries no token: a comment, a file, a question and a
-- time entry never recorded one, and an audit row recorded the token's id
-- without its name, which stays NULL there. No row is rewritten here — the
-- audit record is append-only (docs/adr/0026 D3) — and nothing here touches a
-- policy.
--
-- The runtime role's table-level INSERT on each of these tables covers the new
-- columns (migrations 5, 10, 11, 13 and 14); the one column pair a write
-- changes after the insert, the answer's token, gets the UPDATE grant its
-- neighbour recorded_by_agent has.

-- The audit row: its token_id is the request's since migration 5.
ALTER TABLE audit_events
    ADD COLUMN token_name text CHECK (token_name IS NULL OR token_id IS NOT NULL);

-- The comment and each edit of it, beside their agent marks
-- (docs/adr/0015 D3).
ALTER TABLE comments
    ADD COLUMN token_id   uuid REFERENCES tokens (id),
    ADD COLUMN token_name text,
    ADD CHECK ((token_id IS NULL) = (token_name IS NULL));
ALTER TABLE comment_revisions
    ADD COLUMN token_id   uuid REFERENCES tokens (id),
    ADD COLUMN token_name text,
    ADD CHECK ((token_id IS NULL) = (token_name IS NULL));

-- The file, beside its agent mark (docs/adr/0016 D1).
ALTER TABLE attachments
    ADD COLUMN token_id   uuid REFERENCES tokens (id),
    ADD COLUMN token_name text,
    ADD CHECK ((token_id IS NULL) = (token_name IS NULL));

-- Who asked through a token, beside asked_by_agent; and the token the answer
-- was recorded through, beside recorded_by_agent, set or cleared by every
-- answer, so a changed answer carries its own (docs/adr/0011 D2,
-- docs/adr/0066 D8).
ALTER TABLE questions
    ADD COLUMN asked_by_token_id      uuid REFERENCES tokens (id),
    ADD COLUMN asked_by_token_name    text,
    ADD COLUMN answered_by_token_id   uuid REFERENCES tokens (id),
    ADD COLUMN answered_by_token_name text,
    ADD CHECK ((asked_by_token_id IS NULL) = (asked_by_token_name IS NULL)),
    ADD CHECK ((answered_by_token_id IS NULL) = (answered_by_token_name IS NULL)),
    ADD CHECK (status = 'answered' OR answered_by_token_id IS NULL);

-- The booking and each correction of it. No agent books time
-- (docs/adr/0043 D3), so these rows carry no agent mark; a plain token's
-- person books, and the ticket's activity leaves time entries out
-- (docs/adr/0017 D9), so the entry is the only place that shows it.
ALTER TABLE time_entries
    ADD COLUMN token_id   uuid REFERENCES tokens (id),
    ADD COLUMN token_name text,
    ADD CHECK ((token_id IS NULL) = (token_name IS NULL));
ALTER TABLE time_entry_revisions
    ADD COLUMN token_id   uuid REFERENCES tokens (id),
    ADD COLUMN token_name text,
    ADD CHECK ((token_id IS NULL) = (token_name IS NULL));

DO $$
DECLARE
    runtime text := current_setting('cowork.runtime_role');
BEGIN
    EXECUTE format('GRANT UPDATE (answered_by_token_id, answered_by_token_name) ON questions TO %I', runtime);
END
$$;
