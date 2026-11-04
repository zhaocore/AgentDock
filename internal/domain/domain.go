// Package domain 定义 AgentDock 的核心领域模型。
package domain

import (
	"encoding/json"
	"time"
)

// RepoStatus 仓库状态机：pending → cloning → ready / failed。
type RepoStatus string

const (
	RepoStatusPending RepoStatus = "pending"
	RepoStatusCloning RepoStatus = "cloning"
	RepoStatusReady   RepoStatus = "ready"
	RepoStatusFailed  RepoStatus = "failed"
)

// RepoType 仓库类型：前端 / 后端 / React Native。
type RepoType string

const (
	RepoTypeFrontend RepoType = "frontend"
	RepoTypeBackend  RepoType = "backend"
	RepoTypeRN       RepoType = "rn"
)

// Repository 代码仓库，全局共享资源（不按用户隔离）。
type Repository struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	GitURL    string     `json:"git_url"`
	RepoType  RepoType   `json:"repo_type"`
	Branch    string     `json:"branch"`
	LocalPath string     `json:"local_path"`
	Status    RepoStatus `json:"status"`
	LastError string     `json:"last_error"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// AgentStatus 实例状态机。
type AgentStatus string

const (
	AgentStatusCreated AgentStatus = "created"
	AgentStatusRunning AgentStatus = "running"
	AgentStatusStopped AgentStatus = "stopped"
	AgentStatusFailed  AgentStatus = "failed"
)

// MCPServerConfig MCP server 连接配置。
type MCPServerConfig struct {
	Name      string            `json:"name"`
	Transport string            `json:"transport"`          // sse | stdio
	Endpoint  string            `json:"endpoint,omitempty"` // sse URL
	Command   string            `json:"command,omitempty"`  // stdio 命令
	Env       map[string]string `json:"env,omitempty"`
}

// SubagentDef 子 Agent 定义，主 Agent 以工具形式调用。
type SubagentDef struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tools       []string `json:"tools,omitempty"`
}

// Agent 定义；公共预设无用户 owner，私有定义归属创建用户。
type Agent struct {
	ID           string            `json:"id"`
	OwnerUserID  string            `json:"owner_user_id"`
	Name         string            `json:"name"`
	SystemPrompt string            `json:"system_prompt"`
	Model        string            `json:"model"`
	Skills       []string          `json:"skills"`
	MCPServers   []MCPServerConfig `json:"mcp_servers"`
	Subagents    []SubagentDef     `json:"subagents"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
}

// AgentInstance Agent 运行实例，进程内长驻 goroutine。
type AgentInstance struct {
	ID          string      `json:"id"`
	AgentID     string      `json:"agent_id"`
	OwnerUserID string      `json:"owner_user_id"`
	Status      AgentStatus `json:"status"`
	RepoIDs     []string    `json:"repo_ids"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
}

// Session Agent 会话，按用户隔离。
type Session struct {
	ID         string     `json:"id"`
	UserID     string     `json:"user_id"`
	InstanceID string     `json:"instance_id"`
	AgentID    string     `json:"agent_id"`
	Title      string     `json:"title"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	DeletedAt  *time.Time `json:"-"`
}

// Message 会话消息，含中间步骤等元数据。
type Message struct {
	ID        string          `json:"id"`
	SessionID string          `json:"session_id"`
	Role      string          `json:"role"` // user | assistant
	Content   string          `json:"content"`
	AgentMeta json.RawMessage `json:"agent_meta,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

// MessageView 是 Message 的 API 响应视图（agent_meta 以任意 JSON 对象呈现）。
type MessageView struct {
	ID        string         `json:"id"`
	SessionID string         `json:"session_id"`
	Role      string         `json:"role"`
	Content   string         `json:"content"`
	AgentMeta map[string]any `json:"agent_meta,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
}

// ToView 转换为响应视图。
func (m Message) ToView() MessageView {
	view := MessageView{ID: m.ID, SessionID: m.SessionID, Role: m.Role,
		Content: m.Content, CreatedAt: m.CreatedAt}
	if len(m.AgentMeta) > 0 {
		meta := map[string]any{}
		if err := json.Unmarshal(m.AgentMeta, &meta); err == nil {
			view.AgentMeta = meta
		}
	}
	return view
}
