-- The capabilities a person gives the chat in the UI (docs/adr/0043 D5,
-- docs/adr/0076): the chat acts as the person's agent, and its requests hold
-- exactly the set its person chose here. A person without a row holds the
-- default, every capability but decide, close, drop and record-answer
-- (auth.DefaultChatCapabilities); the row is written the first time the person
-- chooses, and never deleted — an empty set is a choice too, the baseline.
--
-- A table of its own rather than a column of users: the users table's update
-- policies admit an administrator of the account, the start-up synchronisation
-- and the identity provider, and a permissive policy that let a person update
-- their own row would admit every column the runtime role may update there —
-- global_admin and deactivated_at among them. Here a person reads and writes
-- their own row and nobody else's, in every policy (docs/adr/0021).
CREATE TABLE chat_capabilities (
    user_id      uuid        PRIMARY KEY REFERENCES users (id),
    capabilities text[]      NOT NULL,
    updated_at   timestamptz NOT NULL DEFAULT now(),
    CHECK (capabilities <@ ARRAY['decide', 'close', 'drop', 'rank', 'override-urgency', 'interest',
                                 'upload', 'create-project', 'record-answer']::text[])
);

ALTER TABLE chat_capabilities ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_capabilities FORCE ROW LEVEL SECURITY;
CREATE POLICY chat_capabilities_read ON chat_capabilities FOR SELECT
    USING (user_id = app_user_id());
CREATE POLICY chat_capabilities_insert ON chat_capabilities FOR INSERT
    WITH CHECK (user_id = app_user_id());
CREATE POLICY chat_capabilities_update ON chat_capabilities FOR UPDATE
    USING (user_id = app_user_id())
    WITH CHECK (user_id = app_user_id());

DO $$
DECLARE
    runtime text := current_setting('cowork.runtime_role');
BEGIN
    EXECUTE format('GRANT SELECT, INSERT ON chat_capabilities TO %I', runtime);
    EXECUTE format('GRANT UPDATE (capabilities, updated_at) ON chat_capabilities TO %I', runtime);
END
$$;
