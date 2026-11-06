package worker

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/zhaocore/agentdock/internal/domain"
)

// AccountMergePreview 比较双方现有文件与元数据，只返回可发布的原始对象引用。
func (s *ConfluenceLibrary) AccountMergePreview(ctx context.Context, admin, user string) (domain.ConfluenceImportReport, []domain.ConfluenceLibrarySpace, string, error) {
	current, err := s.Catalog(ctx, admin)
	if err != nil {
		return domain.ConfluenceImportReport{}, nil, "", err
	}
	incoming, err := s.Catalog(ctx, user)
	if err != nil {
		return domain.ConfluenceImportReport{}, nil, "", err
	}
	report := domain.ConfluenceImportReport{Items: []domain.ConfluenceImportItem{}}
	selected := []domain.ConfluenceLibrarySpace{}
	for _, space := range incoming.Spaces {
		next := domain.ConfluenceLibrarySpace{ID: space.ID, BaseURL: space.BaseURL, Key: space.Key, Pages: []domain.ConfluenceLibraryPage{}}
		for _, page := range space.Pages {
			item := domain.ConfluenceImportItem{SpaceID: space.ID, PageID: page.ID, Path: page.Path}
			old, findErr := FindConfluenceLibraryPage(current, space.ID, page.ID)
			switch {
			case errors.Is(findErr, sql.ErrNoRows):
				item.Action = "added"
				item.Reason = "新页面"
			case findErr != nil:
				return report, nil, "", findErr
			case page.ModifiedAt.After(old.ModifiedAt):
				item.Action = "updated"
				item.Reason = "Confluence 修改时间更新"
			case page.ModifiedAt.Before(old.ModifiedAt):
				item.Action = "kept"
				item.Reason = "保留管理员较新页面"
			default:
				a, err := s.fileMetadata(ctx, old)
				if err != nil {
					return report, nil, "", err
				}
				b, err := s.fileMetadata(ctx, page)
				if err != nil {
					return report, nil, "", err
				}
				equal := old.Path == page.Path && old.Title == page.Title && old.URL == page.URL && old.Version == page.Version && len(a) == len(b)
				for name, file := range a {
					other, ok := b[name]
					if !ok || file.SHA256 != other.SHA256 {
						equal = false
					}
				}
				if equal {
					item.Action = "kept"
					item.Reason = "时间与内容相同"
				} else {
					item.Action = "conflict"
					item.Reason = "同时间内容不同，保留管理员页面；原始双方保留在历史任务"
				}
			}
			if item.Action == "added" || item.Action == "updated" {
				for _, existingSpace := range current.Spaces {
					if existingSpace.ID == space.ID {
						for _, existing := range existingSpace.Pages {
							if existing.ID != page.ID && strings.EqualFold(existing.Path, page.Path) {
								item.Action = "conflict"
								item.Reason = "目录属于其他页面，保留管理员目录；原始内容保留"
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
				next.Pages = append(next.Pages, page)
			}
		}
		if len(next.Pages) > 0 {
			selected = append(selected, next)
		}
	}
	data, err := json.Marshal(struct {
		Admin  string
		User   string
		Report domain.ConfluenceImportReport
	}{current.Digest, incoming.Digest, report})
	if err != nil {
		return report, nil, "", err
	}
	return report, selected, domain.ConfluenceFileHash(data), nil
}
