package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/zhaocore/dutybotgo/internal/domain"
	"github.com/zhaocore/dutybotgo/internal/store/mysql"
)

// SourceMarkdown 读取受当前用户隔离的完整正文，报告模型不参与原文下载。
func (w *CrawlWorker) SourceMarkdown(ctx context.Context, run domain.CrawlRun) (string, error) {
	positions, err := mysql.CrawlCapturedPositions(ctx, w.DB, run.ID, run.UserID)
	if err != nil {
		return "", err
	}
	if len(positions) == 0 || len(positions) > domain.MaxCrawlPages {
		return "", fmt.Errorf("crawl requires 1–100 captured documents for Markdown download")
	}
	var document strings.Builder
	document.WriteString("# 完整采集 Markdown\n\n")
	for _, position := range positions {
		if position < 0 || position >= domain.MaxCrawlPages {
			return "", fmt.Errorf("invalid captured crawl position")
		}
		data, err := w.Objects.Get(ctx, fmt.Sprintf("%spages/%d.json", CrawlPrefix(run), position))
		if err != nil {
			return "", err
		}
		// Go JSON 将 <>& 转义为六字节，兼容由 1 MiB 浏览器协议产生的既有对象。
		if len(data) > 6*1024*1024 {
			return "", fmt.Errorf("crawl page object exceeds 6 MiB at position %d", position)
		}
		var page domain.CrawlPage
		if err = json.Unmarshal(data, &page); err != nil {
			return "", fmt.Errorf("decode crawl document at position %d: %w", position, err)
		}
		if err = validateCrawlPage(page); err != nil {
			return "", fmt.Errorf("invalid crawl document at position %d: %w", position, err)
		}
		part := fmt.Sprintf("---\n\n来源：<%s>\n\n采集时间：%s\n\n%s\n\n", page.URL, page.CapturedAt.Format("2006-01-02T15:04:05Z07:00"), page.Markdown)
		if document.Len()+len(part) > 16*1024*1024 {
			return "", fmt.Errorf("combined crawl Markdown exceeds 16 MiB; split source tasks")
		}
		document.WriteString(part)
	}
	return document.String(), nil
}
