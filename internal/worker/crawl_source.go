package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/zhaocore/dutybotgo/internal/config"
	"github.com/zhaocore/dutybotgo/internal/domain"
	"github.com/zhaocore/dutybotgo/internal/telemetry"
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

// NewBrowserCrawlSource 设置所有后台页面采集共享的并发限额。
func NewBrowserCrawlSource(root string, concurrency int) *BrowserCrawlSource {
	return &BrowserCrawlSource{AgentDir: root, slots: make(chan struct{}, concurrency)}
}

// Capture 对受控临时错误最多重试两次，不返回脚本的原始日志。
func (s *BrowserCrawlSource) Capture(ctx context.Context, p config.CrawlProfile, raw string) (domain.CrawlPage, error) {
	wait := time.Now()
	select {
	case s.slots <- struct{}{}:
		telemetry.Measure(ctx, "capture_wait", wait, nil)
		defer func() { <-s.slots }()
	case <-ctx.Done():
		telemetry.Measure(ctx, "capture_wait", wait, ctx.Err())
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
		started := time.Now()
		page, retry, err := s.capture(ctx, input)
		telemetry.Measure(ctx, "browser_attempt", started, err)
		var captureError *CrawlCaptureError
		if errors.As(err, &captureError) {
			telemetry.Count(ctx, "browser_http_status_"+strconv.Itoa(captureError.Status), 1)
		}
		if err == nil {
			return page, nil
		}
		if !retry || attempt == 2 {
			return domain.CrawlPage{}, err
		}
		telemetry.Count(ctx, "browser_retries", 1)
		slog.Warn("retry crawl page", "run_id", telemetry.RunID(ctx), "attempt", attempt+1, "profile_id", p.ID, "error", err)
		backoff := time.Now()
		select {
		case <-ctx.Done():
			telemetry.Measure(ctx, "browser_backoff", backoff, ctx.Err())
			return domain.CrawlPage{}, ctx.Err()
		case <-time.After(time.Duration(attempt+1) * time.Second):
			telemetry.Measure(ctx, "browser_backoff", backoff, nil)
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
			FinalURL  string `json:"final_url"`
			Retryable bool   `json:"retryable"`
		} `json:"error"`
	}
	if len(data) > 1024*1024 || json.Unmarshal(data, &result) != nil {
		return domain.CrawlPage{}, false, fmt.Errorf("invalid crawl browser output")
	}
	if result.Error != nil {
		return domain.CrawlPage{}, result.Error.Retryable, &CrawlCaptureError{Code: result.Error.Code, Status: result.Error.Status, FinalURL: result.Error.FinalURL}
	}
	if result.Page != nil && result.Page.Skipped && result.Page.URL != "" {
		return *result.Page, false, nil
	}
	if result.Page == nil {
		return domain.CrawlPage{}, false, fmt.Errorf("crawl browser returned missing page data")
	}
	if err := validateCrawlPage(*result.Page); err != nil {
		return domain.CrawlPage{}, false, err
	}

	return *result.Page, false, nil
}

// CrawlCaptureError 携带受控采集信号，区分登录异常与真正抓取失败。
type CrawlCaptureError struct {
	Code     string
	Status   int
	FinalURL string
}

// Error 不包含浏览器原始日志或凭据。
func (e *CrawlCaptureError) Error() string {
	return fmt.Sprintf("crawl failed code=%s status=%d", e.Code, e.Status)
}

// NeedsCookieReview 不将任意网络或解析错误当成登录异常。
func (e *CrawlCaptureError) NeedsCookieReview() bool {
	return e.Status == 401 || e.Status == 403 || strings.HasPrefix(e.Code, "login_")
}

// validateCrawlPage 校验浏览器协议与对象存储中的完整文档，不截断正文。
func validateCrawlPage(page domain.CrawlPage) error {
	limit := 24000
	switch page.Format {
	case "rss":
		limit = 240000
	case "", "html":
	default:
		return fmt.Errorf("crawl returned unsupported document format")
	}
	if page.URL == "" || page.Title == "" || len([]rune(page.Title)) > 500 || page.Markdown == "" || page.CapturedAt.IsZero() || len([]rune(page.Markdown)) > limit {
		return fmt.Errorf("crawl returned missing or oversized page data")
	}
	return nil
}
