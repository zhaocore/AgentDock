-- 单管理员归属、实时认证版本与可恢复账户治理。
ALTER TABLE users
    ADD COLUMN status VARCHAR(16) NOT NULL DEFAULT 'active',
    ADD COLUMN auth_version BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN must_change_password BOOLEAN NOT NULL DEFAULT FALSE;
CREATE TABLE administration (
    id TINYINT PRIMARY KEY,
    admin_user_id VARCHAR(36) NULL UNIQUE,
    registration_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    CHECK (id=1)
) ENGINE=InnoDB;
INSERT INTO administration (id,admin_user_id)
VALUES (1,(SELECT id FROM users ORDER BY created_at,id LIMIT 1));
CREATE TABLE account_deletions (
    user_id VARCHAR(36) PRIMARY KEY,
    admin_user_id VARCHAR(36) NOT NULL,
    stage VARCHAR(20) NOT NULL,
    error TEXT NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB;
CREATE TABLE admin_audit (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    actor_id VARCHAR(36) NOT NULL,
    target_id VARCHAR(36) NOT NULL,
    action VARCHAR(255) NOT NULL,
    result VARCHAR(20) NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB;
ALTER TABLE confluence_tasks ADD COLUMN storage_user_id VARCHAR(36) NOT NULL DEFAULT '';
ALTER TABLE crawl_runs ADD COLUMN storage_user_id VARCHAR(36) NOT NULL DEFAULT '';
ALTER TABLE confluence_tasks ADD COLUMN library_visible BOOLEAN NOT NULL DEFAULT TRUE;
ALTER TABLE account_deletions ADD COLUMN preview_digest CHAR(64) NOT NULL;
