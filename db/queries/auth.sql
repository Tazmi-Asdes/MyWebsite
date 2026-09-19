-- name: GetAdminByUsername :one
SELECT
    id,
    username,
    password_hash,
    created_at,
    updated_at
FROM admins
WHERE username = sqlc.arg('username');

-- name: CreateAdminSession :execresult
INSERT INTO admin_sessions (
    token_hash,
    admin_id,
    csrf_hash,
    last_seen_at,
    absolute_expires_at,
    created_at
) VALUES (
    sqlc.arg('token_hash'),
    sqlc.arg('admin_id'),
    sqlc.arg('csrf_hash'),
    sqlc.arg('last_seen_at'),
    sqlc.arg('absolute_expires_at'),
    sqlc.arg('created_at')
);

-- name: GetAdminSessionByTokenHash :one
SELECT
    s.id,
    s.token_hash,
    s.admin_id,
    s.csrf_hash,
    s.last_seen_at,
    s.absolute_expires_at,
    s.revoked_at,
    s.created_at,
    a.username
FROM admin_sessions AS s
JOIN admins AS a ON a.id = s.admin_id
WHERE s.token_hash = sqlc.arg('token_hash')
  AND s.revoked_at IS NULL;

-- name: TouchAdminSession :execresult
UPDATE admin_sessions
SET last_seen_at = sqlc.arg('last_seen_at')
WHERE id = sqlc.arg('id')
  AND revoked_at IS NULL;

-- name: RevokeAdminSession :execresult
UPDATE admin_sessions
SET revoked_at = sqlc.arg('revoked_at')
WHERE id = sqlc.arg('id')
  AND revoked_at IS NULL;
