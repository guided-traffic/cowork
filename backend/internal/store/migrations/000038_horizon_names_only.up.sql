-- The contract of the horizon's names (docs/adr/0028 D3; docs/adr/0010 D1 and
-- docs/adr/0043 D4 as amended 2026-10-06): migration 37 let the capability sets
-- take set-horizon beside override-urgency, and the release that shipped it
-- read either name as set-horizon and wrote both. No supported cowork-mcp reads
-- the old names any more, so the stored data takes the new ones:
--
-- - every override-urgency in the capability sets of the tokens and of the
--   chat becomes set-horizon, each name once, in the order it was first named;
-- - a saved filter's parameter urgency becomes horizon. The API decodes the
--   stored object into parameters that no longer have urgency, so a filter
--   left with it would lose that condition without a word. The release before
--   read urgency as horizon only where horizon was absent, and refused a filter
--   that named both, so a horizon already there wins here as it did there.
--
-- The checks of the two capability sets keep override-urgency: the release
-- before writes it beside set-horizon in every set it stores (auth.Stored), and
-- an image rolled back to it over this migration has to keep writing
-- (docs/adr/0028 D3, D4). This release reads such a set without the old name
-- (auth.Canonical). A later release rewrites the three again, for what a
-- rollback wrote in between, and then drops override-urgency from both checks.
--
-- Nothing else changes: what the API answers of a token, a chat or a filter
-- after the rewrite is what it answered before, so no version and no time
-- moves. The audit records keep the names their acts were recorded with.
--
-- Row-level security is forced on the three tables for the owner as well
-- (migrations 4, 24 and 33), and no person or tenant is set in a migration, so
-- the policies would hide every row: the rewrite lifts the force for itself
-- and restores it, in the one transaction of this file, as migration 29 does.
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

ALTER TABLE tokens FORCE ROW LEVEL SECURITY;
ALTER TABLE chat_capabilities FORCE ROW LEVEL SECURITY;
ALTER TABLE saved_filters FORCE ROW LEVEL SECURITY;
