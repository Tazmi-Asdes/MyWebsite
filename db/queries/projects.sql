-- name: CreateProject :execresult
INSERT INTO projects (
    name,
    github_url,
    image_asset_id,
    status,
    sort_order,
    version,
    created_at,
    updated_at
) VALUES (
    sqlc.arg('name'),
    sqlc.narg('github_url'),
    sqlc.narg('image_asset_id'),
    'hidden',
    NULL,
    1,
    sqlc.arg('created_at'),
    sqlc.arg('updated_at')
);

-- name: GetProjectByID :one
SELECT
    id,
    name,
    github_url,
    image_asset_id,
    status,
    sort_order,
    version,
    created_at,
    updated_at
FROM projects
WHERE id = sqlc.arg('id');

-- name: UpdateProject :execresult
UPDATE projects
SET name = sqlc.arg('name'),
    github_url = sqlc.narg('github_url'),
    image_asset_id = sqlc.narg('image_asset_id'),
    status = sqlc.arg('status'),
    sort_order = sqlc.narg('sort_order'),
    version = version + 1,
    updated_at = sqlc.arg('updated_at')
WHERE id = sqlc.arg('id')
  AND version = sqlc.arg('version');

-- name: ListPublicProjects :many
SELECT
    id,
    name,
    github_url,
    image_asset_id,
    status,
    sort_order,
    version,
    created_at,
    updated_at
FROM projects
WHERE status = 'public'
ORDER BY sort_order ASC, id ASC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: CountPublicProjects :one
SELECT COUNT(*) AS total
FROM projects
WHERE status = 'public';

-- name: GetProjectOrderStateForUpdate :one
SELECT id, version, updated_at
FROM project_order_state
WHERE id = 1
FOR UPDATE;

-- name: UpdateProjectOrderState :execresult
UPDATE project_order_state
SET version = version + 1,
    updated_at = sqlc.arg('updated_at')
WHERE id = 1
  AND version = sqlc.arg('version');

-- name: ListPublicProjectIDsForUpdate :many
SELECT id
FROM projects
WHERE status = 'public'
ORDER BY sort_order ASC, id ASC
FOR UPDATE;
