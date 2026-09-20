-- name: CreateProject :execresult
INSERT INTO projects (
    name,
    status,
    sort_order,
    version,
    created_at,
    updated_at
) VALUES (
    sqlc.arg('name'),
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

-- name: GetProjectByIDForUpdate :one
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
WHERE id = sqlc.arg('id')
FOR UPDATE;

-- name: UpdateProject :execresult
UPDATE projects
SET name = sqlc.arg('name'),
    github_url = sqlc.narg('github_url'),
    image_asset_id = sqlc.narg('image_asset_id'),
    version = version + 1,
    updated_at = sqlc.arg('updated_at')
WHERE id = sqlc.arg('id')
  AND version = sqlc.arg('version');

-- name: ListAdminPublicProjects :many
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
ORDER BY sort_order ASC, id ASC;

-- name: ListAdminHiddenProjects :many
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
WHERE status = 'hidden'
ORDER BY updated_at DESC, id DESC;

-- name: ListAllPublicProjects :many
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
ORDER BY sort_order ASC, id ASC;

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
LIMIT ? OFFSET ?;

-- name: CountPublicProjects :one
SELECT COUNT(*) AS total
FROM projects
WHERE status = 'public';

-- name: GetProjectOrderState :one
SELECT id, version, updated_at
FROM project_order_state
WHERE id = 1;

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

-- name: GetMaxPublicProjectSortOrder :one
SELECT sort_order AS max_sort_order
FROM projects
WHERE status = 'public'
ORDER BY sort_order DESC
LIMIT 1;

-- name: PublishProject :execresult
UPDATE projects
SET name = sqlc.arg('name'),
    github_url = sqlc.arg('github_url'),
    image_asset_id = sqlc.narg('image_asset_id'),
    status = 'public',
    sort_order = sqlc.arg('sort_order'),
    version = version + 1,
    updated_at = sqlc.arg('updated_at')
WHERE id = sqlc.arg('id')
  AND status = 'hidden'
  AND version = sqlc.arg('version');

-- name: HideProject :execresult
UPDATE projects
SET status = 'hidden',
    sort_order = NULL,
    version = version + 1,
    updated_at = sqlc.arg('updated_at')
WHERE id = sqlc.arg('id')
  AND status = 'public'
  AND version = sqlc.arg('version');

-- name: ListPublicProjectIDsForUpdate :many
SELECT id
FROM projects
WHERE status = 'public'
ORDER BY sort_order ASC, id ASC
FOR UPDATE;

-- name: SetProjectSortOrderTemporary :execresult
UPDATE projects
SET sort_order = sqlc.arg('temporary_sort_order')
WHERE id = sqlc.arg('id')
  AND status = 'public';

-- name: SetProjectSortOrder :execresult
UPDATE projects
SET sort_order = sqlc.arg('sort_order')
WHERE id = sqlc.arg('id')
  AND status = 'public';
