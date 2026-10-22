package worker

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zhaocore/dutybotgo/internal/domain"
	"github.com/zhaocore/dutybotgo/internal/store/mysql"
	"github.com/zhaocore/dutybotgo/internal/store/rustfs"
)

// LibraryObjects 增加受限前缀列举，不扩大普通导出任务契约。
type LibraryObjects interface {
	rustfs.Objects
	List(context.Context, string) ([]string, error)
}

// 文件库错误交给 transport 映射状态码。
var (
	ErrLibraryChanged = errors.New("文件库已变化，请重新上传 ZIP 检查后再合并")
	ErrInvalidArchive = errors.New("invalid Confluence ZIP")
)

// ConfluenceLibrary 编排只属于当前用户的目录、ZIP 与对象引用。
type ConfluenceLibrary struct {
	DB      *sql.DB
	Objects LibraryObjects
}

// Catalog 汇集全部历史导出的已发布页面及已确认导入，不删除旧对象。
func (s *ConfluenceLibrary) Catalog(ctx context.Context, userID string) (domain.ConfluenceLibrary, error) {
	tasks, err := mysql.ListConfluenceSources(ctx, s.DB, userID)
	if err != nil {
		return domain.ConfluenceLibrary{}, err
	}
	imports, err := mysql.ListConfluenceImports(ctx, s.DB, userID)
	if err != nil {
		return domain.ConfluenceLibrary{}, err
	}
	type source struct {
		at     time.Time
		id     string
		spaces []domain.ConfluenceLibrarySpace
	}
	sources := []source{}
	for _, task := range tasks {
		tree, err := ReadConfluenceTree(ctx, s.Objects, task)
		if err != nil {
			return domain.ConfluenceLibrary{}, err
		}
		if tree.Snapshot == nil || tree.Snapshot.Source.BaseURL == "" || tree.Snapshot.Source.SpaceKey == "" {
			return domain.ConfluenceLibrary{}, fmt.Errorf("task %s lacks Space metadata; re-export its tree", task.ID)
		}
		metadata := map[string]domain.ConfluenceNode{}
		var visit func([]domain.ConfluenceNode)
		visit = func(nodes []domain.ConfluenceNode) {
			for _, node := range nodes {
				metadata[node.ID] = node
				visit(node.Children)
			}
		}
		visit(tree.Nodes)
		space := domain.ConfluenceLibrarySpace{ID: domain.ConfluenceSpaceID(tree.Snapshot.Source.BaseURL, tree.Snapshot.Source.SpaceKey), BaseURL: tree.Snapshot.Source.BaseURL, Key: tree.Snapshot.Source.SpaceKey, Pages: []domain.ConfluenceLibraryPage{}}
		if task.Completed > len(tree.Pages) {
			return domain.ConfluenceLibrary{}, fmt.Errorf("task %s has invalid checkpoint", task.ID)
		}
		for _, page := range tree.Pages[:task.Completed] {
			node, ok := metadata[page.ID]
			modified, err := time.Parse(time.RFC3339Nano, node.LastModifiedAt)
			if !ok || err != nil || node.Version != page.Version {
				return domain.ConfluenceLibrary{}, fmt.Errorf("task %s page %s lacks reliable last_modified_at/version", task.ID, page.ID)
			}
			if !domain.ValidConfluencePath(page.Path) {
				return domain.ConfluenceLibrary{}, fmt.Errorf("invalid exported directory %q", page.Path)
			}
			space.Pages = append(space.Pages, domain.ConfluenceLibraryPage{ID: page.ID, Title: page.Title, URL: page.URL, Version: page.Version, Path: page.Path, ModifiedAt: modified, ObjectPrefix: ConfluencePrefix(task) + page.ID + "/"})
		}
		sources = append(sources, source{task.CreatedAt, task.ID, []domain.ConfluenceLibrarySpace{space}})
	}
	for _, record := range imports {
		sources = append(sources, source{record.CreatedAt, record.ID, record.Spaces})
	}
	sort.Slice(sources, func(i, j int) bool {
		if sources[i].at.Equal(sources[j].at) {
			return sources[i].id < sources[j].id
		}
		return sources[i].at.Before(sources[j].at)
	})
	spaces := map[string]domain.ConfluenceLibrarySpace{}
	for _, source := range sources {
		for _, incoming := range source.spaces {
			space, ok := spaces[incoming.ID]
			if !ok {
				space = incoming
				space.Pages = []domain.ConfluenceLibraryPage{}
			}
			for _, page := range incoming.Pages {
				found := false
				for index, current := range space.Pages {
					if current.ID == page.ID {
						found = true
						if page.ModifiedAt.After(current.ModifiedAt) {
							space.Pages[index] = page
						}
						break
					}
				}
				if !found {
					space.Pages = append(space.Pages, page)
				}
			}
			spaces[space.ID] = space
		}
	}
	catalog := domain.ConfluenceLibrary{Spaces: []domain.ConfluenceLibrarySpace{}}
	for _, space := range spaces {
		sort.Slice(space.Pages, func(i, j int) bool {
			if space.Pages[i].Path == space.Pages[j].Path {
				return space.Pages[i].ID < space.Pages[j].ID
			}
			return space.Pages[i].Path < space.Pages[j].Path
		})
		paths := map[string]string{}
		for _, page := range space.Pages {
			key := strings.ToLower(page.Path)
			if previous, ok := paths[key]; ok {
				return catalog, fmt.Errorf("Space %s directory collision between pages %s and %s", space.Key, previous, page.ID)
			}
			paths[key] = page.ID
		}
		catalog.Spaces = append(catalog.Spaces, space)
	}
	sort.Slice(catalog.Spaces, func(i, j int) bool { return catalog.Spaces[i].ID < catalog.Spaces[j].ID })
	raw, err := json.Marshal(catalog.Spaces)
	if err != nil {
		return catalog, err
	}
	catalog.Digest = domain.ConfluenceFileHash(raw)
	return catalog, nil
}

// fileMetadata 逐文件校验并计算摘要，不缓存整个页面的附件。
func (s *ConfluenceLibrary) fileMetadata(ctx context.Context, page domain.ConfluenceLibraryPage) (map[string]domain.ConfluenceArchiveFile, error) {
	keys, err := s.Objects.List(ctx, page.ObjectPrefix)
	if err != nil {
		return nil, err
	}
	files := map[string]domain.ConfluenceArchiveFile{}
	var total int
	for _, key := range keys {
		name := strings.TrimPrefix(key, page.ObjectPrefix)
		if !strings.HasPrefix(key, page.ObjectPrefix) || !ValidConfluenceFile(name) {
			return nil, fmt.Errorf("unknown exported page file %q", name)
		}
		data, err := s.Objects.Get(ctx, key)
		if err != nil {
			return nil, err
		}
		if len(data) > domain.ConfluenceFileLimit {
			return nil, fmt.Errorf("page file %s exceeds 64 MiB", name)
		}
		total += len(data)
		if total > domain.ConfluenceExpandedLimit {
			return nil, fmt.Errorf("page exceeds 50 GiB")
		}
		files[name] = domain.ConfluenceArchiveFile{Name: name, Size: int64(len(data)), SHA256: domain.ConfluenceFileHash(data)}
	}
	if _, ok := files["page.md"]; !ok {
		return nil, fmt.Errorf("exported page %s is missing Markdown", page.ID)
	}
	return files, nil
}

// FindConfluenceLibraryPage 通过 Space 与稳定页面 ID 定位，不能按任意对象键读取。
func FindConfluenceLibraryPage(catalog domain.ConfluenceLibrary, spaceID, pageID string) (domain.ConfluenceLibraryPage, error) {
	for _, space := range catalog.Spaces {
		if space.ID == spaceID {
			for _, page := range space.Pages {
				if page.ID == pageID {
					return page, nil
				}
			}
		}
	}
	return domain.ConfluenceLibraryPage{}, sql.ErrNoRows
}

// File 不接受客户端对象前缀，目录和正文始终在当前用户范围内查找。
func (s *ConfluenceLibrary) File(ctx context.Context, userID, spaceID, pageID, name string) ([]byte, error) {
	if !ValidConfluenceFile(name) {
		return nil, fmt.Errorf("invalid library filename")
	}
	catalog, err := s.Catalog(ctx, userID)
	if err != nil {
		return nil, err
	}
	page, err := FindConfluenceLibraryPage(catalog, spaceID, pageID)
	if err != nil {
		return nil, err
	}
	return s.Objects.Get(ctx, page.ObjectPrefix+name)
}

type confluenceZIPWriter struct {
	output  io.Writer
	written int64
}

func (b *confluenceZIPWriter) Write(data []byte) (int, error) {
	if b.written+int64(len(data)) > domain.ConfluenceExportZIPLimit {
		return 0, fmt.Errorf("ZIP exceeds 10 GiB; export a smaller directory")
	}
	n, err := b.output.Write(data)
	b.written += int64(n)
	return n, err
}

// Export 选择整库、Space 或其子目录，ZIP 内保留标题目录及附件相对路径。
func (s *ConfluenceLibrary) Export(ctx context.Context, userID, spaceID, directory string, output io.Writer) error {
	if directory != "" && (!domain.ValidConfluencePath(directory) || spaceID == "") {
		return fmt.Errorf("invalid library export directory")
	}
	catalog, err := s.Catalog(ctx, userID)
	if err != nil {
		return err
	}
	manifest := domain.NewConfluenceManifest(time.Now().UTC())
	references := map[string]string{}
	var expanded int64
	for _, space := range catalog.Spaces {
		if spaceID != "" && space.ID != spaceID {
			continue
		}
		exported := domain.ConfluenceArchiveSpace{ID: space.ID, BaseURL: space.BaseURL, Key: space.Key, Pages: []domain.ConfluenceArchivePage{}}
		for _, page := range space.Pages {
			if directory != "" && page.Path != directory && !strings.HasPrefix(page.Path, directory+"/") {
				continue
			}
			content, err := s.fileMetadata(ctx, page)
			if err != nil {
				return err
			}
			record := domain.ConfluenceArchivePage{ID: page.ID, Title: page.Title, URL: page.URL, Version: page.Version, Path: page.Path, ModifiedAt: page.ModifiedAt, Files: []domain.ConfluenceArchiveFile{}}
			names := []string{}
			for name := range content {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				metadata := content[name]
				expanded += metadata.Size
				if expanded > domain.ConfluenceExportExpandedLimit {
					return fmt.Errorf("directory exceeds 50 GiB; export a smaller directory")
				}
				filename, err := domain.ConfluenceArchivePath(space.ID, space.Key, page.Path, page.Title, name)
				if err != nil {
					return err
				}
				record.Files = append(record.Files, domain.ConfluenceArchiveFile{Name: name, Path: filename, Size: metadata.Size, SHA256: metadata.SHA256})
				references[filename] = page.ObjectPrefix + name
			}
			exported.Pages = append(exported.Pages, record)
		}
		if len(exported.Pages) > 0 {
			manifest.Spaces = append(manifest.Spaces, exported)
		}
	}
	if len(manifest.Spaces) == 0 {
		return sql.ErrNoRows
	}
	writer := &confluenceZIPWriter{output: output}
	return domain.StreamConfluenceArchive(writer, manifest, func(filename string) ([]byte, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return s.Objects.Get(ctx, references[filename])
	})
}

// Preview 校验完整 ZIP 后逐页检查覆盖，所有新对象先保持不可见。
func (s *ConfluenceLibrary) Preview(ctx context.Context, userID string, input io.Reader) (domain.ConfluenceImportReport, error) {
	id := uuid.NewString()
	key := "confluence-library/" + userID + "/" + id + "/archive.zip"
	if err := rustfs.PutArchive(ctx, s.Objects, key, input, domain.ConfluenceZIPLimit); err != nil {
		return domain.ConfluenceImportReport{}, err
	}
	reader, size, err := rustfs.OpenArchive(ctx, s.Objects, key, domain.ConfluenceZIPLimit)
	if err != nil {
		return domain.ConfluenceImportReport{}, err
	}
	manifest, _, err := domain.OpenConfluenceArchive(reader, size)
	if err != nil {
		return domain.ConfluenceImportReport{}, fmt.Errorf("%w: %v", ErrInvalidArchive, err)
	}
	current, err := s.Catalog(ctx, userID)
	if err != nil {
		return domain.ConfluenceImportReport{}, err
	}
	report := domain.ConfluenceImportReport{ID: id, Items: []domain.ConfluenceImportItem{}}
	selections := []domain.ConfluenceLibrarySpace{}
	for _, space := range manifest.Spaces {
		selected := domain.ConfluenceLibrarySpace{ID: space.ID, BaseURL: space.BaseURL, Key: space.Key, Pages: []domain.ConfluenceLibraryPage{}}
		for _, page := range space.Pages {
			item := domain.ConfluenceImportItem{SpaceID: space.ID, PageID: page.ID, Path: page.Path}
			old, findErr := FindConfluenceLibraryPage(current, space.ID, page.ID)
			if errors.Is(findErr, sql.ErrNoRows) {
				item.Action = "added"
				item.Reason = "新页面"
			} else if findErr != nil {
				return report, findErr
			} else if page.ModifiedAt.After(old.ModifiedAt) {
				item.Action = "updated"
				item.Reason = "Confluence 修改时间更新"
			} else if page.ModifiedAt.Before(old.ModifiedAt) {
				item.Action = "kept"
				item.Reason = "保留现有较新页面"
			} else {
				existing, err := s.fileMetadata(ctx, old)
				if err != nil {
					return report, err
				}
				equal := old.Path == page.Path && old.Title == page.Title && old.URL == page.URL && old.Version == page.Version && len(existing) == len(page.Files)
				for _, file := range page.Files {
					value, ok := existing[file.Name]
					if !ok || value.SHA256 != file.SHA256 {
						equal = false
					}
				}
				if equal {
					item.Action = "kept"
					item.Reason = "时间与内容相同"
				} else {
					item.Action = "conflict"
					item.Reason = "修改时间相同但内容或目录不同，保留现有页面"
				}
			}
			if item.Action == "added" || item.Action == "updated" {
				for _, existingSpace := range current.Spaces {
					if existingSpace.ID == space.ID {
						for _, existing := range existingSpace.Pages {
							if existing.ID != page.ID && strings.EqualFold(existing.Path, page.Path) {
								item.Action = "conflict"
								item.Reason = "目录已属于其他页面，保留现有目录"
							}
						}
					}
				}
			}
			switch item.Action {
			case "added":
				report.Added++
			case "updated":
				report.Updated++
			case "kept":
				report.Kept++
			case "conflict":
				report.Conflicts++
			}
			report.Items = append(report.Items, item)
			if item.Action == "added" || item.Action == "updated" {
				selected.Pages = append(selected.Pages, domain.ConfluenceLibraryPage{ID: page.ID, Title: page.Title, URL: page.URL, Version: page.Version, Path: page.Path, ModifiedAt: page.ModifiedAt, ObjectPrefix: "confluence-library/" + userID + "/" + id + "/" + space.ID + "/" + page.ID + "/"})
			}
		}
		if len(selected.Pages) > 0 {
			selections = append(selections, selected)
		}
	}
	if err = mysql.CreateConfluenceImport(ctx, s.DB, domain.ConfluenceImport{ID: id, UserID: userID, BaseDigest: current.Digest, Spaces: selections, Report: report}); err != nil {
		return report, err
	}
	return report, nil
}

// Apply 在相同预览基线下发布整批目录，任一上传失败都不显示半批结果。
func (s *ConfluenceLibrary) Apply(ctx context.Context, userID, id string) (report domain.ConfluenceImportReport, err error) {
	connection, err := mysql.LockConfluenceLibrary(ctx, s.DB, userID)
	if err != nil {
		return report, err
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		err = errors.Join(err, mysql.UnlockConfluenceLibrary(unlockCtx, connection, userID))
	}()
	record, err := mysql.GetConfluenceImport(ctx, s.DB, id, userID)
	if err != nil {
		return report, err
	}
	if record.Status != "preview" {
		return report, ErrLibraryChanged
	}
	current, err := s.Catalog(ctx, userID)
	if err != nil {
		return report, err
	}
	if current.Digest != record.BaseDigest {
		return report, ErrLibraryChanged
	}
	reader, size, err := rustfs.OpenArchive(ctx, s.Objects, "confluence-library/"+userID+"/"+id+"/archive.zip", domain.ConfluenceZIPLimit)
	if err != nil {
		return report, err
	}
	manifest, files, err := domain.OpenConfluenceArchive(reader, size)
	if err != nil {
		return report, err
	}
	for _, space := range record.Spaces {
		for _, page := range space.Pages {
			found := false
			for _, sourceSpace := range manifest.Spaces {
				if sourceSpace.ID == space.ID {
					for _, sourcePage := range sourceSpace.Pages {
						if sourcePage.ID == page.ID {
							found = true
							for _, file := range sourcePage.Files {
								content, readErr := domain.ReadConfluenceArchiveEntry(files[file.Path])
								if readErr != nil {
									return report, readErr
								}
								if err = s.Objects.Put(ctx, page.ObjectPrefix+file.Name, content, "application/octet-stream"); err != nil {
									return report, err
								}
							}
						}
					}
				}
			}
			if !found {
				return report, fmt.Errorf("imported page missing from staged ZIP")
			}
		}
	}
	// 再次核对导出任务进度，上传期间的目录变化不能绕过覆盖检查。
	latest, err := s.Catalog(ctx, userID)
	if err != nil {
		return report, err
	}
	if latest.Digest != record.BaseDigest {
		return report, ErrLibraryChanged
	}
	if err = mysql.ApplyConfluenceImport(ctx, s.DB, id, userID); err != nil {
		return report, err
	}
	return record.Report, nil
}
