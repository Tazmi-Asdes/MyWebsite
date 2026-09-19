-- +goose Up

CREATE TABLE admins (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    username VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    CONSTRAINT uq_admins_username UNIQUE (username),
    CONSTRAINT chk_admins_username_length CHECK (CHAR_LENGTH(username) BETWEEN 3 AND 64)
) ENGINE = InnoDB
  DEFAULT CHARACTER SET = utf8mb4
  COLLATE = utf8mb4_0900_ai_ci;

CREATE TABLE admin_sessions (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    token_hash BINARY(32) NOT NULL,
    admin_id BIGINT UNSIGNED NOT NULL,
    csrf_hash BINARY(32) NOT NULL,
    last_seen_at DATETIME(6) NOT NULL,
    absolute_expires_at DATETIME(6) NOT NULL,
    revoked_at DATETIME(6) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    CONSTRAINT uq_admin_sessions_token_hash UNIQUE (token_hash),
    CONSTRAINT fk_admin_sessions_admin
        FOREIGN KEY (admin_id) REFERENCES admins (id)
        ON UPDATE RESTRICT ON DELETE CASCADE
) ENGINE = InnoDB
  DEFAULT CHARACTER SET = utf8mb4
  COLLATE = utf8mb4_0900_ai_ci;

CREATE TABLE media_assets (
    id CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    storage_key VARCHAR(512) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    source_media_type VARCHAR(127) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    stored_media_type VARCHAR(127) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    byte_size BIGINT UNSIGNED NOT NULL,
    width INT UNSIGNED NOT NULL,
    height INT UNSIGNED NOT NULL,
    sha256 BINARY(32) NOT NULL,
    created_by BIGINT UNSIGNED NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    CONSTRAINT uq_media_assets_storage_key UNIQUE (storage_key),
    CONSTRAINT chk_media_assets_byte_size_nonnegative CHECK (byte_size >= 0),
    CONSTRAINT chk_media_assets_width_nonnegative CHECK (width >= 0),
    CONSTRAINT chk_media_assets_height_nonnegative CHECK (height >= 0),
    CONSTRAINT fk_media_assets_created_by
        FOREIGN KEY (created_by) REFERENCES admins (id)
        ON UPDATE RESTRICT ON DELETE RESTRICT
) ENGINE = InnoDB
  DEFAULT CHARACTER SET = utf8mb4
  COLLATE = utf8mb4_0900_ai_ci;

CREATE TABLE articles (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    public_ulid CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NULL,
    title VARCHAR(200) NOT NULL,
    body_markdown MEDIUMTEXT NULL,
    body_html MEDIUMTEXT NULL,
    toc_json JSON NULL,
    preview_text TEXT NULL,
    renderer_version VARCHAR(32) NULL,
    status VARCHAR(16) NOT NULL,
    first_published_at DATETIME(6) NULL,
    version BIGINT UNSIGNED NOT NULL DEFAULT 1,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    CONSTRAINT uq_articles_public_ulid UNIQUE (public_ulid),
    KEY idx_articles_status_published (status, first_published_at, id),
    CONSTRAINT chk_articles_title_length CHECK (CHAR_LENGTH(title) BETWEEN 1 AND 200),
    CONSTRAINT chk_articles_body_markdown_size CHECK (
        body_markdown IS NULL OR OCTET_LENGTH(body_markdown) <= 2097152
    ),
    CONSTRAINT chk_articles_status CHECK (status IN ('draft', 'published')),
    CONSTRAINT chk_articles_version_positive CHECK (version >= 1),
    CONSTRAINT chk_articles_published_fields CHECK (
        status = 'draft'
        OR (
            body_markdown IS NOT NULL
            AND OCTET_LENGTH(body_markdown) > 0
            AND public_ulid IS NOT NULL
            AND first_published_at IS NOT NULL
        )
    )
) ENGINE = InnoDB
  DEFAULT CHARACTER SET = utf8mb4
  COLLATE = utf8mb4_0900_ai_ci;

CREATE TABLE projects (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    name VARCHAR(120) NOT NULL,
    github_url VARCHAR(2048) CHARACTER SET ascii COLLATE ascii_bin NULL,
    image_asset_id CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NULL,
    status VARCHAR(16) NOT NULL,
    sort_order BIGINT UNSIGNED NULL,
    version BIGINT UNSIGNED NOT NULL DEFAULT 1,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_projects_public_sort_order (sort_order),
    KEY idx_projects_status_sort_order (status, sort_order, id),
    CONSTRAINT fk_projects_image_asset
        FOREIGN KEY (image_asset_id) REFERENCES media_assets (id)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT chk_projects_name_length CHECK (CHAR_LENGTH(name) BETWEEN 1 AND 120),
    CONSTRAINT chk_projects_status CHECK (status IN ('hidden', 'public')),
    CONSTRAINT chk_projects_version_positive CHECK (version >= 1),
    CONSTRAINT chk_projects_visibility_fields CHECK (
        (status = 'hidden' AND sort_order IS NULL)
        OR (
            status = 'public'
            AND github_url IS NOT NULL
            AND OCTET_LENGTH(github_url) > 0
            AND sort_order IS NOT NULL
        )
    )
) ENGINE = InnoDB
  DEFAULT CHARACTER SET = utf8mb4
  COLLATE = utf8mb4_0900_ai_ci;

CREATE TABLE article_media (
    article_id BIGINT UNSIGNED NOT NULL,
    asset_id CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    alt_text VARCHAR(300) NOT NULL,
    PRIMARY KEY (article_id, asset_id),
    KEY idx_article_media_asset_id (asset_id),
    CONSTRAINT fk_article_media_article
        FOREIGN KEY (article_id) REFERENCES articles (id)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT fk_article_media_asset
        FOREIGN KEY (asset_id) REFERENCES media_assets (id)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT chk_article_media_alt_text_length CHECK (CHAR_LENGTH(alt_text) BETWEEN 1 AND 300)
) ENGINE = InnoDB
  DEFAULT CHARACTER SET = utf8mb4
  COLLATE = utf8mb4_0900_ai_ci;

CREATE TABLE project_order_state (
    id TINYINT UNSIGNED NOT NULL,
    version BIGINT UNSIGNED NOT NULL DEFAULT 1,
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    CONSTRAINT chk_project_order_state_id CHECK (id = 1),
    CONSTRAINT chk_project_order_state_version_positive CHECK (version >= 1)
) ENGINE = InnoDB
  DEFAULT CHARACTER SET = utf8mb4
  COLLATE = utf8mb4_0900_ai_ci;

INSERT INTO project_order_state (id, version, updated_at)
VALUES (1, 1, UTC_TIMESTAMP(6));

-- +goose Down

DROP TABLE project_order_state;
DROP TABLE article_media;
DROP TABLE projects;
DROP TABLE articles;
DROP TABLE media_assets;
DROP TABLE admin_sessions;
DROP TABLE admins;
