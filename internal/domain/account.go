package domain

import "time"

// Account 只暴露管理所需元数据，不含密码与凭据。
type Account struct {
	ID                 string    `json:"id"`
	Email              string    `json:"email"`
	Status             string    `json:"status"`
	AuthVersion        int64     `json:"-"`
	MustChangePassword bool      `json:"must_change_password"`
	IsAdmin            bool      `json:"is_admin"`
	CreatedAt          time.Time `json:"created_at"`
}

// Administration 保存单管理员与持久化注册开关。
type Administration struct {
	AdminUserID         string `json:"admin_user_id"`
	RegistrationEnabled bool   `json:"registration_enabled"`
}

// AccountDeletion 保存冻结后可重试的删除进度，目标 ID 不随账户删除消失。
type AccountDeletion struct {
	UserID      string `json:"user_id"`
	AdminUserID string `json:"admin_user_id"`
	Stage       string `json:"stage"`
	Error       string `json:"error"`
}

// ResourceCounts 是删除前的资源清单，包含已软删除会话。
type ResourceCounts struct {
	Agents          int `json:"agents"`
	Instances       int `json:"instances"`
	Sessions        int `json:"sessions"`
	Messages        int `json:"messages"`
	ConfluenceTasks int `json:"confluence_tasks"`
	Imports         int `json:"imports"`
	CrawlTasks      int `json:"crawl_tasks"`
	CrawlRuns       int `json:"crawl_runs"`
	Tokens          int `json:"tokens"`
}
