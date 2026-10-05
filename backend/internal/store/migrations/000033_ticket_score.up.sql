-- The score of a ticket (docs/adr/0014 D3, D4): a versioned function of its
-- severity, its horizon, its stakes and its age, computed in the code
-- (domain.ScoreKey) whenever one of the first three changes, and stored with
-- the version that computed it.
--
-- score_key is the score less the age it gains: severity, horizon and stakes
-- minus the time from the Unix epoch to opened_at in units of thirty days.
-- Time adds the same to every ticket's score, so the order of score_key is
-- the order of the score at every moment, and a list ordered by it, and a
-- cursor into it, never move by themselves; the score a person reads is
-- score_key plus the age every ticket has gained by the moment of the read
-- (domain.ScoreAt).
--
-- Nothing here narrows what the previous release reads or writes
-- (docs/adr/0028 D3): it files tickets without these columns, and they take
-- the defaults — version 0, no score, a key below every scored one — until
-- the next change of an input scores them; it changes severities, horizons
-- and stakes without scoring again, and such a ticket keeps the score it had
-- until then.
ALTER TABLE tickets
    ADD COLUMN score_key double precision NOT NULL DEFAULT '-Infinity',
    ADD COLUMN score_version smallint NOT NULL DEFAULT 0 CHECK (score_version >= 0);

-- The person-level lists read a tenant's tickets in the score's order
-- (docs/adr/0014 D5).
CREATE INDEX tickets_by_score ON tickets (tenant_id, score_key DESC, id);

-- Every ticket is scored with version 1, the weights of domain.ScoreKey
-- written out once more: this is the migration that computes what the
-- function computes (D4), and an integration test holds the two equal.
-- Row-level security is forced on tickets and ticket_interest for the owner
-- as well (migrations 8 and 12), and no tenant is set in a migration, so the
-- policies would hide every row: the backfill lifts the force for itself and
-- restores it in the one transaction of this file, as migration 17 does.
ALTER TABLE tickets NO FORCE ROW LEVEL SECURITY;
ALTER TABLE ticket_interest NO FORCE ROW LEVEL SECURITY;

UPDATE tickets t
SET score_version = 1,
    score_key = CASE t.severity WHEN 'critical' THEN 8 WHEN 'high' THEN 5 WHEN 'medium' THEN 3
                                WHEN 'low' THEN 1 ELSE 0 END
              + CASE coalesce(t.urgency_override, t.urgency_derived)
                     WHEN 'now' THEN 8 WHEN 'release' THEN 5 WHEN 'next' THEN 3
                     WHEN 'later' THEN 1 ELSE -5 END
              + coalesce((SELECT sum(CASE i.weight WHEN 'need' THEN 1 WHEN 'urgent' THEN 2 ELSE 0 END)
                          FROM ticket_interest i
                          WHERE i.tenant_id = t.tenant_id AND i.ticket_id = t.id), 0)
              - (extract(epoch FROM t.opened_at) / 2592000)::double precision;

ALTER TABLE ticket_interest FORCE ROW LEVEL SECURITY;
ALTER TABLE tickets FORCE ROW LEVEL SECURITY;

DO $$
DECLARE
    runtime text := current_setting('cowork.runtime_role');
BEGIN
    EXECUTE format('GRANT UPDATE (score_key, score_version) ON tickets TO %I', runtime);
END
$$;
