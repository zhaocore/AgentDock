-- 公共预设无用户 owner；移除配置仅停用，不删除历史。
ALTER TABLE agents
    MODIFY owner_user_id VARCHAR(36) NULL,
    ADD COLUMN enabled BOOLEAN NOT NULL DEFAULT TRUE;
