package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/zhaocore/dutybotgo/internal/domain"
	"github.com/zhaocore/dutybotgo/internal/telemetry"
)

// saveMetrics 覆盖本轮快照，各轮对象独立保留；不写正文与凭据。
func (w *CrawlWorker) saveMetrics(ctx context.Context, r domain.CrawlRun, status string) error {
	recorder := telemetry.FromContext(ctx)
	if recorder == nil {
		return nil
	}
	snapshot := recorder.Snapshot(status)
	data, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("encode crawl metrics: %w", err)
	}
	started := time.Now()
	err = w.Objects.Put(ctx, CrawlPrefix(r)+"metrics.json", data, "application/json")
	telemetry.Measure(ctx, "metrics_put", started, err)
	if err != nil {
		slog.ErrorContext(ctx, "crawl metrics persistence failed", "run_id", r.ID, "status", status)
		return fmt.Errorf("persist crawl metrics run_id=%s: %w", r.ID, err)
	}
	slog.InfoContext(ctx, "crawl metrics snapshot", "run_id", r.ID, "status", status, "metrics", snapshot)
	return nil
}

// putCrawlObject 记录业务对象上传总耗时，包含存储 SDK 内部重试。
func (w *CrawlWorker) putCrawlObject(ctx context.Context, key string, data []byte, contentType string) error {
	started := time.Now()
	err := w.Objects.Put(ctx, key, data, contentType)
	telemetry.Measure(ctx, "object_put", started, err)
	return err
}

// crawlDB 统一测量数据库写接缝，不将请求参数写入日志。
func crawlDB(ctx context.Context, operation func() error) error {
	started := time.Now()
	err := operation()
	telemetry.Measure(ctx, "database_write", started, err)
	return err
}
