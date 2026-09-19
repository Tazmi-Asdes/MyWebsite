-- name: CreateArticle :execresult
INSERT INTO articles (
    title,
    body_markdown,
    status,
    version,
    created_at,
    updated_at
) VALUES (
    sqlc.arg('title'),
    sqlc.narg('body_markdown'),
    'draft',
    1,
    sqlc.arg('created_at'),
    sqlc.arg('updated_at')
);

-- name: GetArticleByID :one
SELECT
    id,
    public_ulid,
    title,
    body_markdown,
    body_html,
    toc_json,
    preview_text,
    renderer_version,
    status,
    first_published_at,
    version,
    created_at,
    updated_at
FROM articles
WHERE id = sqlc.arg('id');

-- name: UpdateArticle :execresult
UPDATE articles
SET public_ulid = sqlc.narg('public_ulid'),
    title = sqlc.arg('title'),
    body_markdown = sqlc.narg('body_markdown'),
    body_html = sqlc.narg('body_html'),
    toc_json = sqlc.narg('toc_json'),
    preview_text = sqlc.narg('preview_text'),
    renderer_version = sqlc.narg('renderer_version'),
    status = sqlc.arg('status'),
    first_published_at = sqlc.narg('first_published_at'),
    version = version + 1,
    updated_at = sqlc.arg('updated_at')
WHERE id = sqlc.arg('id')
  AND version = sqlc.arg('version');

-- name: ListPublishedArticles :many
SELECT
    id,
    public_ulid,
    title,
    preview_text,
    status,
    first_published_at,
    version,
    created_at,
    updated_at
FROM articles
WHERE status = 'published'
ORDER BY first_published_at DESC, id DESC
LIMIT ? OFFSET ?;

-- name: ListAdminArticles :many
SELECT
    id,
    public_ulid,
    title,
    status,
    first_published_at,
    version,
    created_at,
    updated_at
FROM articles
WHERE (sqlc.narg('status_filter') IS NULL OR status = sqlc.narg('status_filter'))
  AND (
      sqlc.narg('title_pattern') IS NULL
      OR title LIKE sqlc.narg('title_pattern') ESCAPE '\\'
  )
ORDER BY updated_at DESC, id DESC
LIMIT ? OFFSET ?;

-- name: CountAdminArticles :one
SELECT COUNT(*) AS total
FROM articles
WHERE (sqlc.narg('status_filter') IS NULL OR status = sqlc.narg('status_filter'))
  AND (
      sqlc.narg('title_pattern') IS NULL
      OR title LIKE sqlc.narg('title_pattern') ESCAPE '\\'
  );

-- name: GetPublishedArticleByULID :one
SELECT
    id,
    public_ulid,
    title,
    body_markdown,
    body_html,
    toc_json,
    preview_text,
    renderer_version,
    status,
    first_published_at,
    version,
    created_at,
    updated_at
FROM articles
WHERE public_ulid = sqlc.arg('public_ulid')
  AND status = 'published';

-- name: WithdrawArticle :execresult
UPDATE articles
SET status = 'draft',
    version = version + 1,
    updated_at = sqlc.arg('updated_at')
WHERE id = sqlc.arg('id')
  AND version = sqlc.arg('version')
  AND status = 'published';

-- name: CountPublishedArticles :one
SELECT COUNT(*) AS total
FROM articles
WHERE status = 'published';
