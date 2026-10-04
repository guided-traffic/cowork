-- The repositories a project owns (docs/adr/0006 D3, docs/adr/0066). A
-- binding is visible with its project: a restricted project's bindings are
-- read by the people who see the project, and a token restricted to a project
-- reads that project's only (docs/adr/0034 D3, docs/adr/0035 D3).

-- name: ListProjectRepositories :many
-- A project's repositories in the order they were bound.
SELECT r.id, r.identity, r.path, r.remote, r.created_at, r.updated_at
FROM project_repositories r
JOIN projects p ON p.tenant_id = r.tenant_id AND p.id = r.project_id
WHERE r.tenant_id = sqlc.arg(tenant_id) AND r.project_id = sqlc.arg(project_id) AND app_project_visible(p.id)
  AND (sqlc.narg(after)::uuid IS NULL OR r.id > sqlc.narg(after)::uuid)
ORDER BY r.id
LIMIT sqlc.arg(page_size);

-- name: FindRepositoryBindings :many
-- The tenant's bindings of any of the identities, for the lookup across the
-- person's tenants (docs/adr/0066 D2).
SELECT r.id, r.identity, r.path, r.remote, p.id AS project_id, p.key AS project_key, p.name AS project_name,
       p.archived_at
FROM project_repositories r
JOIN projects p ON p.tenant_id = r.tenant_id AND p.id = r.project_id
WHERE r.tenant_id = sqlc.arg(tenant_id) AND r.identity = ANY (sqlc.arg(identities)::text[]) AND app_project_visible(p.id)
ORDER BY r.identity, r.path, p.key;

-- name: RepositoriesUnderOwner :one
-- Whether the tenant binds a repository under the same remote owner, the
-- proposal's guess at the tenant (docs/adr/0066 D2).
SELECT EXISTS (
    SELECT 1
    FROM project_repositories r
    JOIN projects p ON p.tenant_id = r.tenant_id AND p.id = r.project_id
    WHERE r.tenant_id = sqlc.arg(tenant_id) AND starts_with(r.identity, sqlc.arg(prefix)::text) AND app_project_visible(p.id)
) AS found;

-- name: GetRepositoryBinding :one
-- Whether the tenant binds the repository at all, and to which project: the
-- identity and path are unique in the tenant whether or not the caller sees
-- the project that holds them. The project is named to the caller only when
-- they see it.
-- visibility: exempt (whether a binding exists, as ProjectKeyTaken for a key)
SELECT r.id, r.project_id, r.identity, r.path, r.remote, r.created_at, r.updated_at
FROM project_repositories r
WHERE r.tenant_id = sqlc.arg(tenant_id) AND r.identity = sqlc.arg(identity) AND r.path = sqlc.arg(path);

-- name: GetVisibleProjectByID :one
SELECT id, key, name, description, restricted, wip_limits, archived_at, version, created_at, updated_at
FROM projects
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id) AND app_project_visible(id);
