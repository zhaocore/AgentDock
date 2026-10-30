package domain

import "time"

// CrawlTask 是当前用户的每日采集配置，登录凭据只保存引用。
type CrawlTask struct {
	ID        string    `json:"id"`
	UserID    string    `json:"-"`
	Name      string    `json:"name"`
	AgentID   string    `json:"agent_id"`
	ProfileID string    `json:"profile_id"`
	URLs      []string  `json:"urls"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CrawlRun 冻结任务配置，每轮报告独立发布。
type CrawlRun struct {
	ID        string    `json:"id"`
	UserID    string    `json:"-"`
	TaskID    string    `json:"task_id"`
	Task      CrawlTask `json:"task"`
	Trigger   string    `json:"trigger"`
	Status    string    `json:"status"`
	Stage     string    `json:"stage"`
	Completed int       `json:"completed"`
	Total     int       `json:"total"`
	Error     string    `json:"error"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CrawlPage 是逐页结果；正文只经过内存或对象存储。
type CrawlPage struct {
	URL        string    `json:"url"`
	Title      string    `json:"title"`
	CapturedAt time.Time `json:"captured_at"`
	Markdown   string    `json:"markdown"`
}

// CrawlPageProgress 是不包含正文的页面执行记录。
type CrawlPageProgress struct {
	Position int    `json:"position"`
	Status   string `json:"status"`
	Title    string `json:"title"`
	Error    string `json:"error"`
}
