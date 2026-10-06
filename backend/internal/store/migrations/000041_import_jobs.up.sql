-- Import jobs, and where an imported ticket came from (docs/adr/0051 D1–D3,
-- D6, D7).
--
-- An import is a job on a project in two phases (D2). The dry run reads the
-- upload and keeps the files it read — as `source`, a gzip-compressed tar —
-- beside the report a person corrects; its execution reads them again, creates
-- the tickets in one transaction (D3), and keeps the report of what it created
-- without the files. A dry run is valid for twenty-four hours (D7),
-- `expires_at`; the job import-expiry deletes the dry runs past it with their
-- files. An executed job is kept: its tickets name it.
--
-- Expand only (docs/adr/0028 D3): a new table, two nullable columns of tickets
-- the previous release never writes, and a new value of audit_action, which
-- nothing in this file uses — PostgreSQL refuses a new enum value in the
-- transaction that adds it.

ALTER TYPE audit_action ADD VALUE 'imported';

CREATE TABLE import_jobs (
    id          uuid        PRIMARY KEY DEFAULT uuidv7(),
    tenant_id   uuid        NOT NULL,
    project_id  uuid        NOT NULL,
    status      text        NOT NULL DEFAULT 'dry_run' CHECK (status IN ('dry_run', 'executed')),
    created_by  uuid        NOT NULL REFERENCES users (id),
    created_at  timestamptz NOT NULL DEFAULT now(),
    expires_at  timestamptz,
    executed_by uuid        REFERENCES users (id),
    executed_at timestamptz,
    report      jsonb       NOT NULL CHECK (jsonb_typeof(report) = 'object'),
    source      bytea,
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, project_id) REFERENCES projects (tenant_id, id),
    -- A dry run holds its files and its day; an executed job neither.
    CHECK ((status = 'dry_run') = (expires_at IS NOT NULL AND source IS NOT NULL)),
    CHECK ((status = 'executed') = (executed_by IS NOT NULL AND executed_at IS NOT NULL))
);
CREATE INDEX import_jobs_by_project ON import_jobs (tenant_id, project_id, id);
CREATE INDEX import_jobs_expiry     ON import_jobs (expires_at) WHERE status = 'dry_run';

-- The tenant's rows in the tenant, and of those only a tenant administrator's
-- reach: an import is an administrator's act (docs/adr/0051 D6), and a dry
-- run holds the content of the files it read, confidential findings among
-- them. The restrictive policy is ANDed with the canonical one, so a query
-- that forgot the role shows a member nothing. The expiry job names itself in
-- app.job, sets no tenant, and reads and deletes the dry runs of every tenant;
-- nothing else deletes, and the job deletes nothing but a dry run.
ALTER TABLE import_jobs ENABLE ROW LEVEL SECURITY;
ALTER TABLE import_jobs FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON import_jobs
    USING (tenant_id = app_tenant_id())
    WITH CHECK (tenant_id = app_tenant_id());
CREATE POLICY import_jobs_expiry_read ON import_jobs FOR SELECT
    USING (app_job() = 'import-expiry' AND status = 'dry_run');
CREATE POLICY import_jobs_expiry_delete ON import_jobs FOR DELETE
    USING (app_job() = 'import-expiry' AND status = 'dry_run');
CREATE POLICY import_jobs_administrators ON import_jobs AS RESTRICTIVE
    USING (app_is_tenant_admin() OR app_job() = 'import-expiry')
    WITH CHECK (app_is_tenant_admin());
CREATE POLICY import_jobs_delete ON import_jobs AS RESTRICTIVE FOR DELETE
    USING (app_job() = 'import-expiry' AND status = 'dry_run');

-- An imported ticket records the file it came from and the job (D3); both or
-- neither. The job's row is never deleted once a ticket names it: only a dry
-- run is, and a dry run created no ticket.
ALTER TABLE tickets
    ADD COLUMN imported_from_file text CHECK (length(imported_from_file) BETWEEN 1 AND 1024),
    ADD COLUMN imported_from_job  uuid,
    ADD CONSTRAINT tickets_imported_from_check CHECK ((imported_from_file IS NULL) = (imported_from_job IS NULL)),
    ADD CONSTRAINT tickets_imported_from_job_fkey FOREIGN KEY (tenant_id, imported_from_job)
        REFERENCES import_jobs (tenant_id, id);

DO $$
DECLARE
    runtime text := current_setting('cowork.runtime_role');
BEGIN
    EXECUTE format('GRANT SELECT, INSERT, DELETE ON import_jobs TO %I', runtime);
    EXECUTE format('GRANT UPDATE (status, expires_at, executed_by, executed_at, report, source) ON import_jobs TO %I', runtime);
END
$$;
