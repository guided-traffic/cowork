-- The narrowing of the horizon's names (docs/adr/0028 D3; docs/adr/0043 D4 and
-- docs/adr/0010 D1 as amended 2026-10-06): migration 38 rewrote the stored
-- names and left override-urgency in the checks of the two capability sets,
-- because release 0.5, rolled back to over it, writes that name beside
-- set-horizon in every set it stores. No release since 0.6.0 writes it — the
-- code that did, auth.Stored, is gone, and the API refuses the name on input —
-- so the release before this one writes no old name, and the checks can
-- refuse it.
--
-- First the three rewrites of migration 38 again, for whatever a rollback to
-- 0.5 wrote in between:
--
-- - every override-urgency in the capability sets of the tokens and of the
--   chat becomes set-horizon, each name once, in the order it was first named;
-- - a saved filter's parameter urgency becomes horizon, a horizon already there
--   winning, as 0.5 read it. The update passes the moderation guard of
--   migration 39: a migration sets no person, so its update is nobody's.
--
-- Then both checks take the nine names of the catalogue (auth.AllCapabilities)
-- and refuse override-urgency. Nothing else changes: no version and no time
-- moves, and the audit records keep the names their acts were recorded with.
--
-- Row-level security is forced on the three tables for the owner as well
-- (migrations 4, 24 and 33), and no person or tenant is set in a migration, so
-- the policies would hide every row: this file lifts the force for its own
-- statements and restores it, in its one transaction, as migration 38 does.
ALTER TABLE tokens NO FORCE ROW LEVEL SECURITY;
ALTER TABLE chat_capabilities NO FORCE ROW LEVEL SECURITY;
ALTER TABLE saved_filters NO FORCE ROW LEVEL SECURITY;

UPDATE tokens
SET capabilities = ARRAY(
    SELECT named.capability
    FROM (SELECT CASE WHEN c.capability = 'override-urgency' THEN 'set-horizon' ELSE c.capability END AS capability,
                 min(c.position) AS position
          FROM unnest(tokens.capabilities) WITH ORDINALITY AS c(capability, position)
          GROUP BY 1) AS named
    ORDER BY named.position)
WHERE 'override-urgency' = ANY (capabilities);

UPDATE chat_capabilities
SET capabilities = ARRAY(
    SELECT named.capability
    FROM (SELECT CASE WHEN c.capability = 'override-urgency' THEN 'set-horizon' ELSE c.capability END AS capability,
                 min(c.position) AS position
          FROM unnest(chat_capabilities.capabilities) WITH ORDINALITY AS c(capability, position)
          GROUP BY 1) AS named
    ORDER BY named.position)
WHERE 'override-urgency' = ANY (capabilities);

UPDATE saved_filters
SET parameters = jsonb_build_object('horizon', parameters -> 'urgency') || (parameters - 'urgency')
WHERE parameters ? 'urgency';

ALTER TABLE tokens DROP CONSTRAINT tokens_capabilities_check;
ALTER TABLE tokens ADD CONSTRAINT tokens_capabilities_check
    CHECK (capabilities <@ ARRAY['decide', 'close', 'drop', 'rank', 'set-horizon', 'interest', 'upload',
                                 'create-project', 'record-answer']::text[]);

ALTER TABLE chat_capabilities DROP CONSTRAINT chat_capabilities_capabilities_check;
ALTER TABLE chat_capabilities ADD CONSTRAINT chat_capabilities_capabilities_check
    CHECK (capabilities <@ ARRAY['decide', 'close', 'drop', 'rank', 'set-horizon', 'interest', 'upload',
                                 'create-project', 'record-answer']::text[]);

ALTER TABLE tokens FORCE ROW LEVEL SECURITY;
ALTER TABLE chat_capabilities FORCE ROW LEVEL SECURITY;
ALTER TABLE saved_filters FORCE ROW LEVEL SECURITY;
