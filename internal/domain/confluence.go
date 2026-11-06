package domain

import (
	"fmt"
	"time"
)

// ConfluenceTask 保存异步导出检查点，不保存文档内容或凭据。
type ConfluenceTask struct {
	StorageUserID string    `json:"-"`
	ID            string    `json:"id"`
	UserID        string    `json:"-"`
	URL           string    `json:"url"`
	Status        string    `json:"status"`
	Stage         string    `json:"stage"`
	Completed     int       `json:"completed"`
	Total         int       `json:"total"`
	Error         string    `json:"error"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
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
	Snapshot       *ConfluenceSnapshotInfo `json:"snapshot,omitempty"`
	Nodes          []ConfluenceNode        `json:"nodes"`
	Pages          []ConfluencePage        `json:"pages"`
	SelectedNodeID *string                 `json:"selected_node_id,omitempty"`
}

// ConfluenceSelection 冻结阶段二范围；空字符串表示明确选择全部。
type ConfluenceSelection struct {
	NodeID *string `json:"node_id"`
}

// SelectConfluencePages 保留原计划顺序和祖先目录，只选指定节点子树的叶子页。
func SelectConfluencePages(tree ConfluenceTree, nodeID string) ([]ConfluencePage, error) {
	if nodeID == "" {
		return tree.Pages, nil
	}
	ids := map[string]bool{}
	var visit func([]ConfluenceNode, bool)
	visit = func(nodes []ConfluenceNode, selected bool) {
		for _, node := range nodes {
			included := selected || node.ID == nodeID
			if included {
				ids[node.ID] = true
			}
			visit(node.Children, included)
		}
	}
	visit(tree.Nodes, false)
	if !ids[nodeID] {
		return nil, fmt.Errorf("Confluence node %s is not in this tree", nodeID)
	}
	pages := []ConfluencePage{}
	for _, page := range tree.Pages {
		if ids[page.ID] {
			pages = append(pages, page)
		}
	}
	if len(pages) == 0 {
		return nil, fmt.Errorf("Confluence node %s has no exportable leaf pages", nodeID)
	}
	return pages, nil
}
