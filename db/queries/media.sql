-- name: CreateMediaAsset :execresult
INSERT INTO media_assets (
    id,
    storage_key,
    source_media_type,
    stored_media_type,
    byte_size,
    width,
    height,
    sha256,
    created_by,
    created_at
) VALUES (
    sqlc.arg('id'),
    sqlc.arg('storage_key'),
    sqlc.arg('source_media_type'),
    sqlc.arg('stored_media_type'),
    sqlc.arg('byte_size'),
    sqlc.arg('width'),
    sqlc.arg('height'),
    sqlc.arg('sha256'),
    sqlc.arg('created_by'),
    sqlc.arg('created_at')
);

-- name: GetMediaAssetByID :one
SELECT
    id,
    storage_key,
    source_media_type,
    stored_media_type,
    byte_size,
    width,
    height,
    sha256,
    created_by,
    created_at
FROM media_assets
WHERE id = sqlc.arg('id');

-- name: DeleteArticleMediaByArticleID :exec
DELETE FROM article_media
WHERE article_id = sqlc.arg('article_id');

-- name: InsertArticleMedia :exec
INSERT INTO article_media (article_id, asset_id, alt_text)
VALUES (
    sqlc.arg('article_id'),
    sqlc.arg('asset_id'),
    sqlc.arg('alt_text')
);

-- name: IsMediaReferencedByPublishedArticle :one
SELECT EXISTS (
    SELECT 1
    FROM article_media AS am
    JOIN articles AS a ON a.id = am.article_id
    WHERE am.asset_id = sqlc.arg('asset_id')
      AND a.status = 'published'
) AS is_referenced;

-- name: IsMediaReferencedByPublicProject :one
SELECT EXISTS (
    SELECT 1
    FROM projects AS p
    WHERE p.image_asset_id = sqlc.arg('asset_id')
      AND p.status = 'public'
) AS is_referenced;
