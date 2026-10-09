-- An import job is the job of the person who made it, and of the tenant's
-- administrators (docs/adr/0051 D6, amended 2026-10-09 by the owner): every
-- writer of a project, an agent of theirs included, imports into it, and a
-- dry run's report holds what its upload's files say — the confidential
-- findings among them —, which only the person who uploaded them and the
-- administrators, who read every confidential ticket, read
-- (docs/adr/0065 D4). Their execution creates the tickets with its executor
-- as their reporter, so nobody else is admitted to them either.
--
-- The restrictive policies of migration 43 admitted the administrators only;
-- each is replaced by one that admits the job's maker as well, the expiry
-- job's and the purge's admissions as they were. Expand only
-- (docs/adr/0028 D3): every row the previous release reads, inserts or
-- changes — an administrator's own — is admitted still.

DROP POLICY import_jobs_read ON import_jobs;
DROP POLICY import_jobs_insert ON import_jobs;
DROP POLICY import_jobs_update ON import_jobs;

CREATE POLICY import_jobs_read ON import_jobs AS RESTRICTIVE FOR SELECT
    USING (app_is_tenant_admin() OR created_by = app_user_id()
           OR app_job() = 'import-expiry' OR app_job() = 'ticket-purge');
CREATE POLICY import_jobs_insert ON import_jobs AS RESTRICTIVE FOR INSERT
    WITH CHECK (app_is_tenant_admin() OR created_by = app_user_id());
CREATE POLICY import_jobs_update ON import_jobs AS RESTRICTIVE FOR UPDATE
    USING (app_is_tenant_admin() OR created_by = app_user_id() OR app_job() = 'ticket-purge')
    WITH CHECK (app_is_tenant_admin() OR created_by = app_user_id() OR app_job() = 'ticket-purge');
