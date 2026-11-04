//go:build integration

package agent

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zhaocore/agentdock/internal/config"
	"github.com/zhaocore/agentdock/internal/domain"
)

// TestCrawlConfiguredLLM 显式启用后使用部署配置验证真实模型，不记录配置值或正文。
func TestCrawlConfiguredLLM(t *testing.T) {
	if os.Getenv("CRAWL_LIVE_LLM") != "1" {
		t.Skip("set CRAWL_LIVE_LLM=1 to call configured provider with public test material")
	}
	t.Chdir("../..")
	cfg, err := config.Load()
	if err != nil {
		t.Fatal("load configured model", err)
	}
	if cfg.LLM.APIKey == "" || cfg.LLM.BaseURL == "" || cfg.LLM.Model == "" {
		t.Fatal("configured provider is incomplete")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	report, err := NewReporter(NewReportRuntime(cfg.LLM.APIKey, cfg.LLM.BaseURL), 1).Generate(ctx, domain.Agent{Name: "采集测试", Model: cfg.LLM.Model, SystemPrompt: "用简短中文整理来源事实，不推测。"}, []domain.CrawlPage{{URL: "https://example.com/controlled-test", Title: "公开测试材料", CapturedAt: time.Now().UTC(), Markdown: "这是公开的合成测试材料。系统每日北京时间09:00生成采集报告。抓取并发上限为3。"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(report, "https://example.com/controlled-test") || len(report) < 100 {
		t.Fatal("model report lost provenance or content")
	}
	t.Logf("configured model report accepted: %d bytes; content omitted", len(report))
}
