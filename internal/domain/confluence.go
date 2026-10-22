package domain

import "time"

// ConfluenceTask 保存异步导出检查点，不保存文档内容或凭据。
type ConfluenceTask struct {
	ID        string    `json:"id"`
	UserID    string    `json:"-"`
	URL       string    `json:"url"`
	Status    string    `json:"status"`
	Stage     string    `json:"stage"`
	Completed int       `json:"completed"`
	Total     int       `json:"total"`
	Error     string    `json:"error"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ConfluenceNode 是原始侧边栏中的节点。
type ConfluenceNode struct {
	ID             string           `json:"id"`
	Title          string           `json:"title"`
	URL            string           `json:"url"`
	Version        int              `json:"version"`
	Children       []ConfluenceNode `json:"children"`
	LastModifiedAt string           `json:"last_modified_at"`
}

// ConfluencePage 复用批量导出的叶子页规划。
type ConfluencePage struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	URL     string `json:"url"`
	Version int    `json:"version"`
	Path    string `json:"path"`
}

// ConfluenceSnapshotInfo 保留文件库识别 Space 所需的原始来源。
type ConfluenceSnapshotInfo struct {
	Source struct {
		BaseURL  string `json:"base_url"`
		SpaceKey string `json:"space_key"`
	} `json:"source"`
	CapturedAt time.Time `json:"captured_at"`
}

// ConfluenceTree 为浏览器提供完整目录和冻结的有序导出计划。
type ConfluenceTree struct {
	Snapshot *ConfluenceSnapshotInfo `json:"snapshot,omitempty"`
	Nodes    []ConfluenceNode        `json:"nodes"`
	Pages    []ConfluencePage        `json:"pages"`
}
