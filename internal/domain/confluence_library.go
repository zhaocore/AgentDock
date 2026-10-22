package domain

import "time"

// ConfluenceLibraryPage 是文件库中的已发布页面及私有对象引用。
type ConfluenceLibraryPage struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	URL          string    `json:"url"`
	Version      int       `json:"version"`
	Path         string    `json:"path"`
	ModifiedAt   time.Time `json:"modified_at"`
	ObjectPrefix string    `json:"object_prefix"`
}

// ConfluenceLibrarySpace 按实例和 Space 合并页面，不按任务拆目录。
type ConfluenceLibrarySpace struct {
	ID      string                  `json:"id"`
	BaseURL string                  `json:"base_url"`
	Key     string                  `json:"key"`
	Pages   []ConfluenceLibraryPage `json:"pages"`
}

// ConfluenceLibrary 是当前用户文件库的一致性摘要。
type ConfluenceLibrary struct {
	Spaces []ConfluenceLibrarySpace `json:"spaces"`
	Digest string                   `json:"digest"`
}

// ConfluenceArchiveFile 保存 ZIP 内文件身份、大小和完整性校验。
type ConfluenceArchiveFile struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// ConfluenceArchivePage 保留跨导入所需的页面来源与修改时间，不接受对象存储引用。
type ConfluenceArchivePage struct {
	ID         string                  `json:"id"`
	Title      string                  `json:"title"`
	URL        string                  `json:"url"`
	Version    int                     `json:"version"`
	Path       string                  `json:"path"`
	ModifiedAt time.Time               `json:"modified_at"`
	Files      []ConfluenceArchiveFile `json:"files"`
}

// ConfluenceArchiveSpace 是 ZIP 中的独立 Space 根目录。
type ConfluenceArchiveSpace struct {
	ID      string                  `json:"id"`
	BaseURL string                  `json:"base_url"`
	Key     string                  `json:"key"`
	Pages   []ConfluenceArchivePage `json:"pages"`
}

// ConfluenceArchiveManifest 是本模块 ZIP 的版本化导入清单。
type ConfluenceArchiveManifest struct {
	SchemaVersion int                      `json:"schema_version"`
	ExportedAt    time.Time                `json:"exported_at"`
	Spaces        []ConfluenceArchiveSpace `json:"spaces"`
}

// ConfluenceImportItem 明确显示每页的覆盖检查结果。
type ConfluenceImportItem struct {
	SpaceID string `json:"space_id"`
	PageID  string `json:"page_id"`
	Path    string `json:"path"`
	Action  string `json:"action"`
	Reason  string `json:"reason"`
}

// ConfluenceImportReport 用于预览和最终合并结果。
type ConfluenceImportReport struct {
	ID        string                 `json:"id"`
	Added     int                    `json:"added"`
	Updated   int                    `json:"updated"`
	Kept      int                    `json:"kept"`
	Conflicts int                    `json:"conflicts"`
	Items     []ConfluenceImportItem `json:"items"`
}

// ConfluenceImport 保存预览基线和确认后可见的不可变导入批次。
type ConfluenceImport struct {
	ID         string
	UserID     string
	Status     string
	BaseDigest string
	Spaces     []ConfluenceLibrarySpace
	Report     ConfluenceImportReport
	CreatedAt  time.Time
}
