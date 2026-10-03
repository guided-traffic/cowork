-- The manual rank of a project's open tickets (docs/adr/0014 D1, D2).
--
-- A key is a base-62 fraction over 0-9A-Za-z (domain.RankBetween): compared
-- byte by byte, which is what the "C" collation does and the ASCII order of
-- the alphabet is, and never ending in 0. A terminal ticket has none.
--
-- The column is nullable and nothing here narrows what the previous release
-- writes (docs/adr/0028 D3): that release files tickets without a key and
-- moves them into done or dropped without clearing one. Such a ticket is
-- unranked until the next write that hands out a key in its project ranks it
-- at the bottom, in number order; the project's list shows the unranked after
-- the ranked tickets, by number, so it shows them where they would land.
ALTER TABLE tickets ADD COLUMN rank text COLLATE "C"
    CHECK (rank ~ '^[0-9A-Za-z]{0,127}[1-9A-Za-z]$');

-- The project's order; a key belongs to one ticket of its project. NULLs are
-- distinct, so the unranked share it.
CREATE UNIQUE INDEX tickets_by_rank ON tickets (tenant_id, project_id, rank);

-- Every open ticket of a project gets a key in number order, evenly spaced
-- over keys of `width` digits with at least 62 places between two of them —
-- the room of the first moves. Row-level security is forced on tickets for
-- the owner as well (migration 8), and no tenant is set in a migration, so
-- the policy would hide every row: the backfill lifts the force for itself
-- and restores it. The whole file runs as one transaction, which holds
-- tickets exclusively from the ALTER on; nobody reads the table meanwhile, and
-- a failure restores the force with everything else. The runtime role is
-- never affected: the force concerns the owner alone.
ALTER TABLE tickets NO FORCE ROW LEVEL SECURITY;
DO $$
DECLARE
    digits CONSTANT text := '0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz';
    p      record;
    t      record;
    width  int;
    step   numeric;
    i      int;
    v      numeric;
    key    text;
BEGIN
    FOR p IN SELECT tenant_id, project_id, count(*) AS n
             FROM tickets WHERE state NOT IN ('done', 'dropped')
             GROUP BY tenant_id, project_id
    LOOP
        width := 2;
        WHILE 62::numeric ^ (width - 1) < p.n + 1 LOOP
            width := width + 1;
        END LOOP;
        step := floor(62::numeric ^ width / (p.n + 1));
        i := 0;
        FOR t IN SELECT id FROM tickets
                 WHERE tenant_id = p.tenant_id AND project_id = p.project_id
                   AND state NOT IN ('done', 'dropped')
                 ORDER BY number
        LOOP
            i := i + 1;
            v := i * step;
            key := '';
            FOR d IN 1..width LOOP
                key := substr(digits, (v % 62)::int + 1, 1) || key;
                v := floor(v / 62);
            END LOOP;
            UPDATE tickets SET rank = rtrim(key, '0') WHERE id = t.id;
        END LOOP;
    END LOOP;
END
$$;
ALTER TABLE tickets FORCE ROW LEVEL SECURITY;

DO $$
DECLARE
    runtime text := current_setting('cowork.runtime_role');
BEGIN
    EXECUTE format('GRANT UPDATE (rank) ON tickets TO %I', runtime);
END
$$;
