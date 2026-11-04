// Package telemetry 记录不含正文与凭据的单轮运行测量。
package telemetry

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/zhaocore/agentdock/internal/config"
)

type contextKey struct{}

// Timing 的总耗时是累计值，并行操作不能与阶段耗时相加。
type Timing struct {
	Count    int64   `json:"count"`
	Failures int64   `json:"failures"`
	TotalMS  float64 `json:"total_ms"`
	MaxMS    float64 `json:"max_ms"`
}

// Snapshot 是可持续保存的版本化指标，不包含 URL、正文和错误响应。
type Snapshot struct {
	Version     int                      `json:"version"`
	RunID       string                   `json:"run_id"`
	UserID      string                   `json:"user_id"`
	TaskID      string                   `json:"task_id"`
	Status      string                   `json:"status"`
	StartedAt   time.Time                `json:"started_at"`
	RecordedAt  time.Time                `json:"recorded_at"`
	QueueMS     float64                  `json:"queue_ms"`
	ElapsedMS   float64                  `json:"elapsed_ms"`
	Concurrency config.ConcurrencyConfig `json:"concurrency"`
	Timings     map[string]Timing        `json:"timings"`
	Counters    map[string]int64         `json:"counters"`
}

// Recorder 在单轮并行接缝间同步收集测量，快照复制集合以避免数据竞争。
type Recorder struct {
	mu       sync.Mutex
	started  time.Time
	snapshot Snapshot
}

// New 以运行元数据创建独立记录器，不复用其他轮次的统计。
func New(ctx context.Context, run, user, task string, queued time.Time, limits config.ConcurrencyConfig) (context.Context, *Recorder) {
	now := time.Now()
	r := &Recorder{started: now, snapshot: Snapshot{Version: 1, RunID: run, UserID: user, TaskID: task, StartedAt: now.UTC(), QueueMS: float64(max(time.Duration(0), now.Sub(queued))) / float64(time.Millisecond), Concurrency: limits, Timings: map[string]Timing{}, Counters: map[string]int64{}}}
	return context.WithValue(ctx, contextKey{}, r), r
}

// Measure 记录已完成操作；错误只记失败数，不记录可能含凭据的错误文本。
func Measure(ctx context.Context, name string, started time.Time, err error) {
	r, ok := ctx.Value(contextKey{}).(*Recorder)
	if !ok {
		return
	}
	elapsed := float64(time.Since(started)) / float64(time.Millisecond)
	r.mu.Lock()
	timing := r.snapshot.Timings[name]
	timing.Count++
	if err != nil {
		timing.Failures++
	}
	timing.TotalMS += elapsed
	timing.MaxMS = max(timing.MaxMS, elapsed)
	r.snapshot.Timings[name] = timing
	run := r.snapshot.RunID
	r.mu.Unlock()
	slog.InfoContext(ctx, "crawl measurement", "run_id", run, "operation", name, "elapsed_ms", elapsed, "failed", err != nil)
}

// Count 累计有限类别的数量，调用方不得使用来源 URL 作为类别。
func Count(ctx context.Context, name string, delta int64) {
	r, ok := ctx.Value(contextKey{}).(*Recorder)
	if !ok {
		return
	}
	r.mu.Lock()
	r.snapshot.Counters[name] += delta
	r.mu.Unlock()
}

// Snapshot 返回一致的深复制，不暴露收集器内部集合。
func (r *Recorder) Snapshot(status string) Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := r.snapshot
	result.Status = status
	result.RecordedAt = time.Now().UTC()
	result.ElapsedMS = float64(time.Since(r.started)) / float64(time.Millisecond)
	result.Timings = make(map[string]Timing, len(r.snapshot.Timings))
	for key, value := range r.snapshot.Timings {
		result.Timings[key] = value
	}
	result.Counters = make(map[string]int64, len(r.snapshot.Counters))
	for key, value := range r.snapshot.Counters {
		result.Counters[key] = value
	}
	return result
}

// FromContext 只向持久化接缝提供当前轮记录器。
func FromContext(ctx context.Context) *Recorder {
	r, _ := ctx.Value(contextKey{}).(*Recorder)
	return r
}

// RunID 为已有重试日志补充当前运行标识。
func RunID(ctx context.Context) string {
	r := FromContext(ctx)
	if r == nil {
		return ""
	}
	return r.snapshot.RunID
}
