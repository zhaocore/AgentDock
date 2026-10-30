CREATE TABLE crawl_tasks (
    id VARCHAR(36) PRIMARY KEY,
    user_id VARCHAR(36) NOT NULL,
    name VARCHAR(200) NOT NULL,
    agent_id VARCHAR(36) NOT NULL,
    profile_id VARCHAR(80) NOT NULL,
    urls JSON NOT NULL,
    enabled BOOLEAN NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    INDEX idx_crawl_tasks_user (user_id, created_at),
    FOREIGN KEY (user_id) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE crawl_runs (
    id VARCHAR(36) PRIMARY KEY,
    user_id VARCHAR(36) NOT NULL,
    task_id VARCHAR(36) NOT NULL,
    snapshot JSON NOT NULL,
    trigger_type VARCHAR(16) NOT NULL,
    scheduled_slot VARCHAR(32) NULL,
    active_task_id VARCHAR(36) GENERATED ALWAYS AS (CASE WHEN status IN ('queued','running') THEN task_id ELSE NULL END) STORED,
    status VARCHAR(16) NOT NULL,
    stage VARCHAR(16) NOT NULL,
    completed INT NOT NULL DEFAULT 0,
    total INT NOT NULL,
    error TEXT NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    UNIQUE KEY uq_crawl_active (active_task_id),
    UNIQUE KEY uq_crawl_slot (task_id, scheduled_slot),
    INDEX idx_crawl_runs_user (user_id, task_id, created_at),
    FOREIGN KEY (task_id) REFERENCES crawl_tasks(id),
    FOREIGN KEY (user_id) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE crawl_pages (
    run_id VARCHAR(36) NOT NULL,
    position INT NOT NULL,
    status VARCHAR(16) NOT NULL,
    title VARCHAR(500) NOT NULL,
    error TEXT NOT NULL,
    PRIMARY KEY (run_id, position),
    FOREIGN KEY (run_id) REFERENCES crawl_runs(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
