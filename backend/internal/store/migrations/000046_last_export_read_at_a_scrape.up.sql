-- The seconds since a tenant's last export (docs/adr/0060 D4, D6): a scrape
-- reads, for every tenant, when a project of it or the whole tenant was last
-- exported, as the act `exported` records it (docs/adr/0059 D3), in the
-- transaction that names the consistency check's job and no tenant — the one
-- that reads the check's counts (migration 42). The tenant's administrators
-- read the same on its settings page, in the tenant's own transaction, which
-- audit_read admits already. A ticket's export, its Markdown or its context,
-- is no copy of the tenant and is left out.
--
-- Expand only (docs/adr/0028 D3): an index and a read policy the previous
-- release never meets.

-- The latest export of a tenant without a walk through its whole audit
-- record; the acts of an export are few, so the index is small.
CREATE INDEX audit_exports_by_tenant ON audit_events (tenant_id, created_at)
    WHERE action = 'exported' AND entity_type IN ('project', 'tenant');

-- Across the tenants, the job's read admits the acts of a project's or a
-- tenant's export and no other row of the audit record. Permissive, so it adds
-- to audit_read and narrows nothing; no request names the job.
CREATE POLICY audit_exports_read ON audit_events FOR SELECT
    USING (app_job() = 'consistency-check' AND app_tenant_id() IS NULL
           AND action = 'exported' AND entity_type IN ('project', 'tenant'));
