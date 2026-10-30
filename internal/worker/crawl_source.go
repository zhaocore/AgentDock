package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/zhaocore/dutybotgo/internal/config"
	"github.com/zhaocore/dutybotgo/internal/domain"
)

// CrawlSource 限定页面采集在 Worker 外部系统接缝。
type CrawlSource interface {
	Capture(context.Context, config.CrawlProfile, string) (domain.CrawlPage, error)
}

// BrowserCrawlSource 使用共享限额，凭据经 stdin 进入浏览器进程。
type BrowserCrawlSource struct {
	AgentDir string
	slots    chan struct{}
}

// NewBrowserCrawlSource 将所有后台页面采集限制为三路。
func NewBrowserCrawlSource(root string) *BrowserCrawlSource {
	return &BrowserCrawlSource{AgentDir: root, slots: make(chan struct{}, 3)}
}

// Capture 对受控临时错误最多重试两次，不返回脚本的原始日志。
func (s *BrowserCrawlSource) Capture(ctx context.Context, p config.CrawlProfile, raw string) (domain.CrawlPage, error) {
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	case <-ctx.Done():
		return domain.CrawlPage{}, ctx.Err()
	}
	input, err := json.Marshal(struct {
		URL    string `json:"url"`
		Origin string `json:"origin"`
		Cookie string `json:"cookie"`
		Ready  string `json:"ready_selector"`
	}{raw, p.Origin, p.Cookie, p.ReadySelector})
	if err != nil {
		return domain.CrawlPage{}, err
	}
	for attempt := 0; attempt < 3; attempt++ {
		page, retry, err := s.capture(ctx, input)
		if err == nil {
			return page, nil
		}
		if !retry || attempt == 2 {
			return domain.CrawlPage{}, err
		}
		slog.Warn("retry crawl page", "attempt", attempt+1, "profile_id", p.ID, "error", err)
		select {
		case <-ctx.Done():
			return domain.CrawlPage{}, ctx.Err()
		case <-time.After(time.Duration(attempt+1) * time.Second):
		}
	}
	return domain.CrawlPage{}, fmt.Errorf("crawl attempts exhausted")
}

// capture 有界读取进程结果，丢弃可能包含 Cookie 的第三方 stderr。
func (s *BrowserCrawlSource) capture(ctx context.Context, input []byte) (domain.CrawlPage, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 55*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bun", filepath.Join(s.AgentDir, "skills/url-to-markdown/scripts/crawl.ts"))
	cmd.Stdin = bytes.NewReader(input)
	cmd.Stderr = io.Discard
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return domain.CrawlPage{}, false, err
	}
	if err = cmd.Start(); err != nil {
		return domain.CrawlPage{}, false, fmt.Errorf("start crawl browser: %w", err)
	}
	data, readErr := io.ReadAll(io.LimitReader(stdout, 1024*1024+1))
	oversized := len(data) > 1024*1024
	if oversized {
		cancel()
	}
	waitErr := cmd.Wait()
	if oversized {
		return domain.CrawlPage{}, false, fmt.Errorf("crawl browser output exceeds 1 MiB")
	}
	if ctx.Err() != nil {
		return domain.CrawlPage{}, false, fmt.Errorf("crawl browser timed out or was canceled")
	}
	if readErr != nil || waitErr != nil {
		return domain.CrawlPage{}, false, fmt.Errorf("crawl browser process failed; verify Bun and Playwright installation")
	}
	var result struct {
		Page  *domain.CrawlPage `json:"page"`
		Error *struct {
			Code      string `json:"code"`
			Status    int    `json:"status"`
			Retryable bool   `json:"retryable"`
		} `json:"error"`
	}
	if len(data) > 1024*1024 || json.Unmarshal(data, &result) != nil {
		return domain.CrawlPage{}, false, fmt.Errorf("invalid crawl browser output")
	}
	if result.Error != nil {
		return domain.CrawlPage{}, result.Error.Retryable, fmt.Errorf("crawl failed code=%s status=%d", result.Error.Code, result.Error.Status)
	}
	if result.Page == nil || result.Page.URL == "" || result.Page.Title == "" || result.Page.Markdown == "" || result.Page.CapturedAt.IsZero() || len([]rune(result.Page.Markdown)) > 24000 {
		return domain.CrawlPage{}, false, fmt.Errorf("crawl browser returned missing or oversized page data")
	}
	return *result.Page, false, nil
}
