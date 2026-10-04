-- The filing of a ticket and a stake in it show who made them, as every other
-- act on a ticket does (docs/adr/0036 D6, the owner's rule of 2026-10-04 that
-- an act an agent or a token makes in a person's name is always marked): the
-- ticket keeps, beside its reporter, the agent mark and the token of the
-- request that filed it; a stake keeps those of the write that set it last,
-- and a person's own write in a browser session clears them. The token's name
-- is copied for the reasons of migration 27.
--
-- Expand only (docs/adr/0028 D3): nullable columns the previous release never
-- writes, and checks it cannot break. A ticket filed and a stake set before
-- this migration carry no mark here; their acts are marked in the activity.
-- The runtime role's table-level INSERT on both tables covers the new columns
-- (migrations 8 and 12); a stake changed later replaces its mark, which needs
-- the UPDATE grant its weight and note have. Nothing here touches a policy.

ALTER TABLE tickets
    ADD COLUMN reporter_agent      text,
    ADD COLUMN reporter_token_id   uuid REFERENCES tokens (id),
    ADD COLUMN reporter_token_name text,
    ADD CHECK ((reporter_token_id IS NULL) = (reporter_token_name IS NULL));

ALTER TABLE ticket_interest
    ADD COLUMN agent      text,
    ADD COLUMN token_id   uuid REFERENCES tokens (id),
    ADD COLUMN token_name text,
    ADD CHECK ((token_id IS NULL) = (token_name IS NULL));

DO $$
DECLARE
    runtime text := current_setting('cowork.runtime_role');
BEGIN
    EXECUTE format('GRANT UPDATE (agent, token_id, token_name) ON ticket_interest TO %I', runtime);
END
$$;
