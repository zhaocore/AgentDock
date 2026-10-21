package domain

import "time"

// APITokenAllKBs 是 api_token_scopes.kb_id 的全零哨兵 UUID，表示"全部知识库"。
const APITokenAllKBs = "00000000-0000-0000-0000-000000000000"

// APITokenScope 单条令牌范围：知识库（或全部）+ 是否允许写操作。
type APITokenScope struct {
	KBID     string `json:"kb_id"`
	CanWrite bool   `json:"can_write"`
}

// APIToken 令牌元数据，不含密钥本体（只保留展示用前缀）。
type APIToken struct {
	ID          string          `json:"id"`
	TenantID    string          `json:"tenant_id"`
	Label       string          `json:"label"`
	TokenPrefix string          `json:"token_prefix"`
	Scopes      []APITokenScope `json:"scopes"`
	ExpiresAt   *time.Time      `json:"expires_at"`
	RevokedAt   *time.Time      `json:"revoked_at"`
	LastUsedAt  *time.Time      `json:"last_used_at"`
	CreatedAt   time.Time       `json:"created_at"`
}

// APITokenUsageRow 按 token × 知识库聚合的调用统计。
type APITokenUsageRow struct {
	KnowledgeBaseID   string     `json:"knowledge_base_id"`
	KnowledgeBaseName string     `json:"knowledge_base_name"`
	Calls             int        `json:"calls"`
	LastCalledAt      *time.Time `json:"last_called_at"`
}
