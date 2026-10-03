-- name: ListVisibleProjectIDs :many
-- The projects whose events a stream admits: those the caller can see, the
-- archived ones included, narrowed by a project-restricted token
-- (docs/adr/0054 D3).
SELECT id FROM projects WHERE tenant_id = sqlc.arg(tenant_id) AND app_project_visible(id);
