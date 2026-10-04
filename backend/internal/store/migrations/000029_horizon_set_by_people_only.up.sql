-- The urgency is the ticket's horizon, a planning category a person or an
-- agent sets in whatever state the ticket is; nothing derives it any more
-- (docs/adr/0010 D3 as amended 2026-10-04). Rule set v2 has one row: every
-- ticket derives later, and the horizon set on it is its override.
--
-- Every ticket keeps the horizon it showed: one that derived release or icebox
-- under rule set v1 and had no override holds that value as its override, set
-- by nobody and without an act, its reason naming this change; its version
-- moves on, since what it shows is now its own. Then every derived value is
-- later under v2. Nothing narrows what the previous release reads or writes
-- (docs/adr/0028 D3). Its derivation may still write a v1 value while it runs
-- beside this release; a ticket without an override then shows that value
-- until a person or an agent sets its horizon, and nothing of this release
-- derives it again.
--
-- Row-level security is forced on tickets for the owner as well (migration
-- 8), and no tenant is set in a migration, so the policy would hide every row:
-- the rewrite lifts the force for itself and restores it, in the one
-- transaction of this file, as migration 17 does.
ALTER TABLE tickets NO FORCE ROW LEVEL SECURITY;

UPDATE tickets
SET urgency_override = urgency_derived,
    urgency_override_reason = 'kept from the derivation when the horizon became set by people only',
    urgency_override_at = now(),
    version = version + 1
WHERE urgency_override IS NULL AND urgency_derived <> 'later';

UPDATE tickets
SET urgency_derived = 'later', urgency_rule = 'v2:default'
WHERE urgency_derived <> 'later' OR urgency_rule <> 'v2:default';

ALTER TABLE tickets FORCE ROW LEVEL SECURITY;
