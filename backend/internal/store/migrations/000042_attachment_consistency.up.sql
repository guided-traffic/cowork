-- The daily consistency check of the attachments (docs/adr/0059 D4): per
-- tenant, the metadata whose object the bucket lacks (dangling) and the
-- objects under the tenant's prefix that no metadata names (orphans), kept so
-- that the tenant's administrators read the counts and the lists, remove the
-- orphans after confirming them and accept the files that are lost.
--
-- Expand only (docs/adr/0028 D3): two tables, two audit actions and a job's
-- read of the tenants that the previous release never meets. Run under it in a
-- rollback, nothing checks and nothing reads the tables; the acceptances of
-- an attachment its purge removes go with it by the foreign key, whichever
-- release purges.

-- The check's summary in the installation-level audit record, and an
-- administrator's acceptance of the missing files in the tenant's. The removal
-- of the orphans is a purge, an action that exists. A new value is not used in
-- the transaction that adds it.
ALTER TYPE audit_action ADD VALUE IF NOT EXISTS 'checked';
ALTER TYPE audit_action ADD VALUE IF NOT EXISTS 'accepted';

-- The latest check of a tenant, one row each, replaced by every run under a
-- new id: a confirmation names the id of the lists it was shown, so a newer
-- check refuses it. The counts are exact; the lists hold at most a thousand
-- entries each — the store's bound —, a dangling file with its name, its
-- ticket's key and whether it was accepted, an orphan with its key, size and
-- time. No file name of a tenant leaves it: the rows are the tenant's, read by
-- its administrators and the job alone.
CREATE TABLE consistency_checks (
    id                 uuid        PRIMARY KEY,
    tenant_id          uuid        NOT NULL UNIQUE REFERENCES tenants (id),
    checked_at         timestamptz NOT NULL,
    dangling           integer     NOT NULL CHECK (dangling >= 0),
    accepted           integer     NOT NULL CHECK (accepted >= 0),
    orphans            integer     NOT NULL CHECK (orphans >= 0),
    orphan_bytes       bigint      NOT NULL CHECK (orphan_bytes >= 0),
    dangling_items     jsonb       NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(dangling_items) = 'array'),
    orphan_items       jsonb       NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(orphan_items) = 'array'),
    -- An administrator's confirmed removal of the check's orphans: who, when,
    -- how many were removed and how many had gained metadata and were kept.
    orphans_removed_at timestamptz,
    orphans_removed_by uuid        REFERENCES users (id),
    orphans_removed    integer     CHECK (orphans_removed >= 0),
    orphans_kept       integer     CHECK (orphans_kept >= 0),
    CHECK ((orphans_removed_at IS NULL) = (orphans_removed_by IS NULL)),
    CHECK ((orphans_removed_at IS NULL) = (orphans_removed IS NULL)),
    CHECK ((orphans_removed_at IS NULL) = (orphans_kept IS NULL))
);

-- The tenant's row in the tenant, and of that to its administrators and the
-- job alone: the lists name files of tickets a member may not see. The job
-- writes a run's result; an administrator confirms a removal and accepts the
-- missing files, which changes the counts and the lists. The restrictive
-- policies are ANDed with the canonical one, so a member's query reads nothing
-- and writes nothing. A scrape reads every tenant's counts in a transaction
-- that names the job and no tenant, as the purge finds the tickets it is due
-- to purge (migration 32): a request always has a tenant.
ALTER TABLE consistency_checks ENABLE ROW LEVEL SECURITY;
ALTER TABLE consistency_checks FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON consistency_checks
    USING (tenant_id = app_tenant_id())
    WITH CHECK (tenant_id = app_tenant_id());
CREATE POLICY consistency_checks_counts ON consistency_checks FOR SELECT
    USING (app_job() = 'consistency-check' AND app_tenant_id() IS NULL);
CREATE POLICY consistency_checks_read ON consistency_checks AS RESTRICTIVE FOR SELECT
    USING (app_job() = 'consistency-check' OR app_is_tenant_admin());
CREATE POLICY consistency_checks_insert ON consistency_checks AS RESTRICTIVE FOR INSERT
    WITH CHECK (app_job() = 'consistency-check');
CREATE POLICY consistency_checks_update ON consistency_checks AS RESTRICTIVE FOR UPDATE
    USING (app_job() = 'consistency-check' OR app_is_tenant_admin())
    WITH CHECK (app_job() = 'consistency-check' OR app_is_tenant_admin());

-- An administrator's acceptance that a file's bytes are lost (docs/adr/0059
-- D5): the attachment stays, its download answers that its bytes are missing,
-- and the check counts it as accepted instead of dangling, so that a loss the
-- tenant has accepted no longer holds the alert. The check removes an
-- acceptance whose attachment is whole again, so that a later loss of the same
-- file counts once more; the purge of the ticket removes it with the row.
CREATE TABLE consistency_acceptances (
    tenant_id     uuid        NOT NULL,
    attachment_id uuid        NOT NULL,
    accepted_by   uuid        NOT NULL REFERENCES users (id),
    accepted_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, attachment_id),
    FOREIGN KEY (tenant_id, attachment_id) REFERENCES attachments (tenant_id, id) ON DELETE CASCADE
);

ALTER TABLE consistency_acceptances ENABLE ROW LEVEL SECURITY;
ALTER TABLE consistency_acceptances FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON consistency_acceptances
    USING (tenant_id = app_tenant_id())
    WITH CHECK (tenant_id = app_tenant_id());
CREATE POLICY consistency_acceptances_read ON consistency_acceptances AS RESTRICTIVE FOR SELECT
    USING (app_job() = 'consistency-check' OR app_is_tenant_admin());
CREATE POLICY consistency_acceptances_insert ON consistency_acceptances AS RESTRICTIVE FOR INSERT
    WITH CHECK (app_is_tenant_admin() AND accepted_by = app_user_id());
CREATE POLICY consistency_acceptances_delete ON consistency_acceptances AS RESTRICTIVE FOR DELETE
    USING (app_job() = 'consistency-check');

-- tenants: the job checks every tenant, read with no tenant set
-- (docs/adr/0021 D6). The body is migration 41's — migration 26's with the
-- webhook's job — with this job added: a later restatement carries both.
ALTER POLICY tenants_read ON tenants USING (
    id = app_tenant_id()
    OR EXISTS (SELECT 1 FROM memberships m WHERE m.tenant_id = tenants.id AND m.user_id = app_user_id())
    OR app_job() IN ('login', 'bootstrap', 'identity-provider', 'github-webhook', 'consistency-check')
    OR app_is_global_admin());

DO $$
DECLARE
    runtime text := current_setting('cowork.runtime_role');
BEGIN
    EXECUTE format('GRANT SELECT, INSERT ON consistency_checks TO %I', runtime);
    EXECUTE format('GRANT UPDATE (id, checked_at, dangling, accepted, orphans, orphan_bytes, dangling_items, orphan_items, '
                   'orphans_removed_at, orphans_removed_by, orphans_removed, orphans_kept) ON consistency_checks TO %I', runtime);
    EXECUTE format('GRANT SELECT, INSERT, DELETE ON consistency_acceptances TO %I', runtime);
END
$$;
