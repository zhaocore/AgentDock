-- AgentDock 初始 schema：MySQL 8。
-- 用户、API 令牌沿用 GraphGo 契约；仓库 / Agent / 实例 / 会话为新模型。

CREATE TABLE IF NOT EXISTS users (
    id VARCHAR(36) PRIMARY KEY,
    email VARCHAR(255) NOT NULL UNIQUE,
    password_hash VARCHAR(255) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS api_tokens (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id VARCHAR(36) NOT NULL,
    label VARCHAR(255) NOT NULL,
    token_prefix VARCHAR(16) NOT NULL,
    token_hash VARCHAR(64) NOT NULL UNIQUE,
    expires_at TIMESTAMP NULL,
    revoked_at TIMESTAMP NULL,
    last_used_at TIMESTAMP NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_api_tokens_tenant (tenant_id),
    CONSTRAINT fk_api_tokens_user FOREIGN KEY (tenant_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS api_token_scopes (
    token_id VARCHAR(36) NOT NULL,
    kb_id VARCHAR(64) NOT NULL,
    can_write BOOLEAN NOT NULL DEFAULT FALSE,
    PRIMARY KEY (token_id, kb_id),
    CONSTRAINT fk_api_token_scopes_token FOREIGN KEY (token_id) REFERENCES api_tokens(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 代码仓库：全局共享资源，不按用户隔离
CREATE TABLE IF NOT EXISTS repositories (
    id VARCHAR(36) PRIMARY KEY,
    name VARCHAR(255) NOT NULL UNIQUE,
    git_url VARCHAR(1024) NOT NULL,
    repo_type ENUM('frontend', 'backend', 'rn') NOT NULL,
    branch VARCHAR(255) NOT NULL DEFAULT 'main',
    local_path VARCHAR(1024) NULL,
    status ENUM('pending', 'cloning', 'ready', 'failed') NOT NULL DEFAULT 'pending',
    last_error TEXT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Agent 定义：归属 owner
CREATE TABLE IF NOT EXISTS agents (
    id VARCHAR(36) PRIMARY KEY,
    owner_user_id VARCHAR(36) NOT NULL,
    name VARCHAR(255) NOT NULL,
    system_prompt TEXT NOT NULL,
    model VARCHAR(255) NOT NULL,
    skills JSON NOT NULL,
    mcp_servers JSON NOT NULL,
    subagents JSON NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_agents_owner (owner_user_id),
    CONSTRAINT fk_agents_user FOREIGN KEY (owner_user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Agent 运行实例：服务进程内长驻 goroutine 的登记记录
CREATE TABLE IF NOT EXISTS agent_instances (
    id VARCHAR(36) PRIMARY KEY,
    agent_id VARCHAR(36) NOT NULL,
    owner_user_id VARCHAR(36) NOT NULL,
    status ENUM('created', 'running', 'stopped', 'failed') NOT NULL DEFAULT 'created',
    repo_ids JSON NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_instances_agent (agent_id),
    CONSTRAINT fk_instances_agent FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE,
    CONSTRAINT fk_instances_user FOREIGN KEY (owner_user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 会话：按用户隔离，软删除
CREATE TABLE IF NOT EXISTS sessions (
    id VARCHAR(36) PRIMARY KEY,
    user_id VARCHAR(36) NOT NULL,
    instance_id VARCHAR(36) NOT NULL,
    agent_id VARCHAR(36) NOT NULL,
    title VARCHAR(255) NULL,
    deleted_at TIMESTAMP NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_sessions_user (user_id, deleted_at),
    CONSTRAINT fk_sessions_instance FOREIGN KEY (instance_id) REFERENCES agent_instances(id) ON DELETE CASCADE,
    CONSTRAINT fk_sessions_agent FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE,
    CONSTRAINT fk_sessions_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 会话消息：含 agent 中间步骤元数据
CREATE TABLE IF NOT EXISTS messages (
    id VARCHAR(36) PRIMARY KEY,
    session_id VARCHAR(36) NOT NULL,
    role ENUM('user', 'assistant') NOT NULL,
    content MEDIUMTEXT NOT NULL,
    agent_meta JSON NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_messages_session (session_id, created_at),
    CONSTRAINT fk_messages_session FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
