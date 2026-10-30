package worker

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/hibiken/asynq"
	"github.com/zhaocore/dutybotgo/internal/config"
	"github.com/zhaocore/dutybotgo/internal/domain"
	"github.com/zhaocore/dutybotgo/internal/store/mysql"
	"github.com/zhaocore/dutybotgo/internal/store/rustfs"
)

const TypeCrawlRun = "crawl:run"
const TypeCrawlDispatch = "crawl:dispatch"

// CrawlReporter 复用 Agent 接缝，不要求启动交互实例。
type CrawlReporter interface {
	Generate(context.Context, domain.Agent, []domain.CrawlPage) (string, error)
}

// CrawlInspector 读取真实队列状态，避免将归档消息误认作已恢复。
type CrawlInspector interface {
	GetTaskInfo(string, string) (*asynq.TaskInfo, error)
}

// CrawlWorker 编排数据库、浏览器和模型接缝。
type CrawlWorker struct {
	DB        *sql.DB
	Objects   rustfs.Objects
	Source    CrawlSource
	Reporter  CrawlReporter
	Profiles  []config.CrawlProfile
	Client    Enqueuer
	Inspector CrawlInspector
}
type crawlPayload struct {
	ID     string `json:"id"`
	UserID string `json:"user_id"`
}

// CrawlPrefix 将每轮结果隔离到当前用户命名空间。
func CrawlPrefix(r domain.CrawlRun) string { return "crawl/" + r.UserID + "/" + r.ID + "/" }

// Dispatch 领取运行并可靠入队，失败显式释放占用。
func (w *CrawlWorker) Dispatch(ctx context.Context, id, user, trigger, slot string) (domain.CrawlRun, error) {
	r, err := mysql.ClaimCrawlRun(ctx, w.DB, id, user, trigger, slot)
	if err != nil {
		return r, err
	}
	if r.Status == "skipped" {
		return r, nil
	}
	if err = w.enqueue(ctx, r); err != nil {
		record := mysql.FinishCrawlRun(ctx, w.DB, r.ID, user, "failed", "queue enqueue failed")
		return r, errors.Join(fmt.Errorf("enqueue crawl: %w", err), record)
	}
	return mysql.GetCrawlRun(ctx, w.DB, r.ID, user)
}

// enqueue 以运行 ID 防止重复投递，重试由明确的外部调用接缝执行。
func (w *CrawlWorker) enqueue(ctx context.Context, r domain.CrawlRun) error {
	payload, err := json.Marshal(crawlPayload{r.ID, r.UserID})
	if err != nil {
		return err
	}
	_, err = w.Client.EnqueueContext(ctx, asynq.NewTask(TypeCrawlRun, payload), asynq.TaskID(r.ID), asynq.MaxRetry(0), asynq.Timeout(30*time.Minute), asynq.Retention(24*time.Hour))
	if errors.Is(err, asynq.ErrTaskIDConflict) {
		info, inspectErr := w.Inspector.GetTaskInfo("default", r.ID)
		if inspectErr != nil {
			return inspectErr
		}
		if info.State == asynq.TaskStateArchived || info.State == asynq.TaskStateCompleted {
			return fmt.Errorf("crawl queue message is already %s", info.State)
		}
		return nil
	}
	return err
}

// RecoverDispatch 恢复未入队记录，并把已结束或丢失的执行消息收敛为失败。
func (w *CrawlWorker) RecoverDispatch(ctx context.Context) error {
	runs, err := mysql.UnfinishedCrawlRuns(ctx, w.DB)
	if err != nil {
		return err
	}
	var failures []error
	for _, r := range runs {
		info, inspectErr := w.Inspector.GetTaskInfo("default", r.ID)
		missing := errors.Is(inspectErr, asynq.ErrTaskNotFound) || errors.Is(inspectErr, asynq.ErrQueueNotFound)
		switch {
		case missing && r.Status == "queued":
			err = w.enqueue(ctx, r)
		case missing:
			err = mysql.FinishCrawlRun(ctx, w.DB, r.ID, r.UserID, "failed", "crawl execution message is missing")
		case inspectErr != nil:
			err = inspectErr
		case info.State == asynq.TaskStateArchived || info.State == asynq.TaskStateCompleted:
			err = mysql.FinishCrawlRun(ctx, w.DB, r.ID, r.UserID, "failed", "crawl queue message ended before run was finalized")
		default:
			err = nil
		}
		if err != nil {
			failures = append(failures, fmt.Errorf("recover crawl run %s: %w", r.ID, err))
		}
	}
	return errors.Join(failures...)
}

// DispatchDaily 只接受北京时间当天九点这一分钟的触发，拒绝延迟补跑。
func (w *CrawlWorker) DispatchDaily(ctx context.Context, now time.Time) error {
	zone, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return err
	}
	local := now.In(zone)
	if local.Hour() != 9 || local.Minute() != 0 {
		slog.Warn("skip stale crawl schedule", "scheduled_window", "09:00 Asia/Shanghai")
		return nil
	}
	tasks, err := mysql.EnabledCrawlTasks(ctx, w.DB)
	if err != nil {
		return err
	}
	var failures []error
	for _, t := range tasks {
		_, err = w.Dispatch(ctx, t.ID, t.UserID, "scheduled", local.Format("2006-01-02"))
		if errors.Is(err, mysql.ErrCrawlDuplicate) || errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			failures = append(failures, fmt.Errorf("dispatch task %s: %w", t.ID, err))
		}
	}
	return errors.Join(failures...)
}

// Handle 终态消息不再执行；失败信息仅含阶段和受控错误。
func (w *CrawlWorker) Handle(ctx context.Context, t *asynq.Task) error {
	var p crawlPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return fmt.Errorf("invalid crawl payload: %w", err)
	}
	r, err := mysql.GetCrawlRun(ctx, w.DB, p.ID, p.UserID)
	if err != nil {
		return err
	}
	started, err := mysql.StartCrawlRun(ctx, w.DB, r.ID, r.UserID)
	if err != nil || !started {
		return err
	}
	if err = w.execute(ctx, r); err != nil {
		recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		return errors.Join(err, mysql.FinishCrawlRun(recordCtx, w.DB, r.ID, r.UserID, "failed", err.Error()))
	}
	return nil
}

// execute 加工本轮冻结任务，报告对象完整写入后发布成功记录。
func (w *CrawlWorker) execute(ctx context.Context, r domain.CrawlRun) error {
	p, err := config.CrawlAccess(w.Profiles, r.UserID, r.Task.ProfileID, r.Task.URLs)
	if err != nil {
		return err
	}
	a, err := mysql.GetAgent(ctx, w.DB, r.Task.AgentID, r.UserID)
	if err != nil {
		return fmt.Errorf("crawl Agent unavailable: %w", err)
	}
	snapshot, err := json.Marshal(struct {
		ID     string `json:"id"`
		Model  string `json:"model"`
		Prompt string `json:"prompt"`
	}{a.ID, a.Model, a.SystemPrompt})
	if err != nil {
		return err
	}
	if err = w.Objects.Put(ctx, CrawlPrefix(r)+"agent.json", snapshot, "application/json"); err != nil {
		return err
	}
	pages, err := w.capture(ctx, r, p)
	if err != nil {
		return err
	}
	if err = mysql.CrawlProgress(ctx, w.DB, r.ID, r.UserID, "report", len(pages)); err != nil {
		return err
	}
	report, err := w.Reporter.Generate(ctx, a, pages)
	if err != nil {
		return fmt.Errorf("crawl report failed: %w", err)
	}
	if report == "" {
		return fmt.Errorf("crawl report is empty")
	}
	if err = mysql.CrawlProgress(ctx, w.DB, r.ID, r.UserID, "publish", len(pages)); err != nil {
		return err
	}
	if err = w.Objects.Put(ctx, CrawlPrefix(r)+"report.md", []byte(report), "text/markdown; charset=utf-8"); err != nil {
		return err
	}
	for attempt := 0; attempt < 3; attempt++ {
		err = mysql.FinishCrawlRun(ctx, w.DB, r.ID, r.UserID, "succeeded", "")
		if err == nil {
			return nil
		}
		slog.Warn("retry crawl report publication", "run_id", r.ID, "attempt", attempt+1)
	}
	return fmt.Errorf("publish crawl report record: %w", err)
}

// capture 保留输入顺序，任一页面失败取消余下采集；共享接缝另有总并发上限。
func (w *CrawlWorker) capture(ctx context.Context, r domain.CrawlRun, p config.CrawlProfile) ([]domain.CrawlPage, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	pages := make([]domain.CrawlPage, len(r.Task.URLs))
	jobs := make(chan int)
	var group sync.WaitGroup
	var mu sync.Mutex
	var first error
	completed := 0
	for worker := 0; worker < min(3, len(pages)); worker++ {
		group.Go(func() {
			for index := range jobs {
				page, err := w.Source.Capture(ctx, p, r.Task.URLs[index])
				if err == nil && page.URL != r.Task.URLs[index] {
					err = fmt.Errorf("crawl source URL mismatch")
				}
				if err == nil {
					data, encodeErr := json.Marshal(page)
					err = encodeErr
					if err == nil {
						err = w.Objects.Put(ctx, fmt.Sprintf("%spages/%d.json", CrawlPrefix(r), index), data, "application/json")
					}
				}
				mu.Lock()
				status, reason := "captured", ""
				if err != nil {
					status, reason = "failed", err.Error()
				}
				recordCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
				recordErr := mysql.RecordCrawlPage(recordCtx, w.DB, r.ID, r.UserID, domain.CrawlPageProgress{Position: index, Title: page.Title, Status: status, Error: reason})
				stop()
				if err == nil {
					err = recordErr
				}
				if err == nil {
					pages[index] = page
					completed++
					err = mysql.CrawlProgress(ctx, w.DB, r.ID, r.UserID, "capture", completed)
				}
				if err != nil && first == nil {
					first = fmt.Errorf("crawl page %d: %w", index+1, err)
					cancel()
				}
				mu.Unlock()
			}
		})
	}
loop:
	for i := range pages {
		select {
		case jobs <- i:
		case <-ctx.Done():
			break loop
		}
	}
	close(jobs)
	group.Wait()
	if first != nil {
		return nil, first
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return pages, nil
}
