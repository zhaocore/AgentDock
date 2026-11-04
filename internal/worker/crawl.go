package worker

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/hibiken/asynq"
	"github.com/zhaocore/agentdock/internal/config"
	"github.com/zhaocore/agentdock/internal/domain"
	"github.com/zhaocore/agentdock/internal/store/mysql"
	"github.com/zhaocore/agentdock/internal/store/rustfs"
	"github.com/zhaocore/agentdock/internal/telemetry"
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
	Concurrency config.ConcurrencyConfig
	DB          *sql.DB
	Objects     rustfs.Objects
	Source      CrawlSource
	Reporter    CrawlReporter
	Profiles    []config.CrawlProfile
	Client      Enqueuer
	Inspector   CrawlInspector
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
		record := crawlDB(ctx, func() error { return mysql.FinishCrawlRun(ctx, w.DB, r.ID, user, "failed", "queue enqueue failed") })
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
			err = crawlDB(ctx, func() error {
				return mysql.FinishCrawlRun(ctx, w.DB, r.ID, r.UserID, "failed", "crawl execution message is missing")
			})
		case inspectErr != nil:
			err = inspectErr
		case info.State == asynq.TaskStateArchived || info.State == asynq.TaskStateCompleted:
			err = crawlDB(ctx, func() error {
				return mysql.FinishCrawlRun(ctx, w.DB, r.ID, r.UserID, "failed", "crawl queue message ended before run was finalized")
			})
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
	ctx, recorder := telemetry.New(ctx, r.ID, r.UserID, r.TaskID, r.CreatedAt, w.Concurrency)
	err = w.saveMetrics(ctx, r, "running")
	if err == nil {
		err = w.execute(ctx, r)
	}
	status := "succeeded"
	if recorder.Snapshot("running").Counters["pages_needs_cookie"] > 0 {
		status = "partial"
	}
	if err != nil {
		status = "failed"
		if ctx.Err() != nil {
			status = "canceled"
		}
		recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		err = errors.Join(err, crawlDB(recordCtx, func() error { return mysql.FinishCrawlRun(recordCtx, w.DB, r.ID, r.UserID, "failed", err.Error()) }))
		cancel()
	}
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	return errors.Join(err, w.saveMetrics(recordCtx, r, status))
}

// execute 加工本轮冻结任务，报告对象写入后发布完整或部分结果。
func (w *CrawlWorker) execute(ctx context.Context, r domain.CrawlRun) (resultErr error) {
	p, err := config.CrawlTaskProfiles(w.Profiles, r.Task)
	if err != nil {
		return err
	}
	started := time.Now()
	a, err := mysql.GetAgent(ctx, w.DB, r.Task.AgentID, r.UserID)
	telemetry.Measure(ctx, "database_read", started, err)
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
	if err = w.putCrawlObject(ctx, CrawlPrefix(r)+"agent.json", snapshot, "application/json"); err != nil {
		return err
	}
	started = time.Now()
	pages, issues, completed, err := w.capture(ctx, r, p)
	telemetry.Measure(ctx, "capture_phase", started, err)
	if err != nil {
		return err
	}
	if err = w.saveMetrics(ctx, r, "running"); err != nil {
		return err
	}
	if err = crawlDB(ctx, func() error { return mysql.CrawlProgress(ctx, w.DB, r.ID, r.UserID, "report", completed) }); err != nil {
		return err
	}
	report := ""
	if len(pages) > 0 {
		started = time.Now()
		report, err = w.Reporter.Generate(ctx, a, pages)
		telemetry.Measure(ctx, "report_phase", started, err)
		if err != nil {
			return fmt.Errorf("crawl report failed: %w", err)
		}
		if report == "" {
			return fmt.Errorf("crawl report is empty")
		}
	}
	status, reason := "succeeded", ""
	if len(issues) > 0 {
		status, reason = "partial", fmt.Sprintf("%d pages require Cookie or login review; see report", len(issues))
		report = cookieReviewReport(issues) + "\n\n" + report
	}
	if report == "" {
		return fmt.Errorf("crawl report is empty")
	}
	if err = w.saveMetrics(ctx, r, "running"); err != nil {
		return err
	}
	publication := time.Now()
	defer func() { telemetry.Measure(ctx, "publish_phase", publication, resultErr) }()
	if err = crawlDB(ctx, func() error { return mysql.CrawlProgress(ctx, w.DB, r.ID, r.UserID, "publish", completed) }); err != nil {
		return err
	}
	if err = w.putCrawlObject(ctx, CrawlPrefix(r)+"report.md", []byte(report), "text/markdown; charset=utf-8"); err != nil {
		return err
	}
	for attempt := 0; attempt < 3; attempt++ {
		err = crawlDB(ctx, func() error { return mysql.FinishCrawlRun(ctx, w.DB, r.ID, r.UserID, status, reason) })
		if err == nil {
			return nil
		}
		slog.Warn("retry crawl report publication", "run_id", r.ID, "attempt", attempt+1)
	}
	return fmt.Errorf("publish crawl report record: %w", err)
}

// crawlVisit 记录单轮广度优先队列中的剩余跳数。
type crawlVisit struct {
	URL  string
	Hops int
}

// crawlCookieIssue 只保存 URL 和判断依据，不保存任何凭据。
type crawlCookieIssue struct {
	URL      string `json:"url"`
	Origin   string `json:"origin"`
	FinalURL string `json:"final_url"`
	Code     string `json:"code"`
	Status   int    `json:"status"`
}

// cookieReviewReport 将异常清单直接附在报告中，避免模型遗漏失败来源。
func cookieReviewReport(issues []crawlCookieIssue) string {
	var report strings.Builder
	report.WriteString("# 部分完成：需要补充 Cookie 或检查登录权限\n\n以下页面未进入正文报告；403、重定向或就绪超时仅表示疑似登录／权限问题，请核对来源后补充配置并重新执行。\n")
	for _, issue := range issues {
		data, _ := json.Marshal(issue)
		report.WriteString("\n```json\n" + string(data) + "\n```\n")
	}
	return report.String()
}

// recordCrawlPage 即使采集已取消也保存受控结果，不丢失原始失败原因。
func (w *CrawlWorker) recordCrawlPage(ctx context.Context, r domain.CrawlRun, page domain.CrawlPageProgress) error {
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	err := crawlDB(recordCtx, func() error {
		return mysql.RecordCrawlPage(recordCtx, w.DB, r.ID, r.UserID, page)
	})
	telemetry.Count(ctx, "pages_"+page.Status, 1)
	return err
}

// capture 按层发现链接并保持输入顺序；跨域页面使用独立登录配置，单轮 URL 只访问一次。
func (w *CrawlWorker) capture(ctx context.Context, r domain.CrawlRun, profiles []config.CrawlProfile) ([]domain.CrawlPage, []crawlCookieIssue, int, error) {
	if w.Concurrency.Task < 1 || w.Concurrency.Task > 64 {
		return nil, nil, 0, fmt.Errorf("crawl task concurrency must be between 1 and 64")
	}
	pattern, err := regexp.Compile(r.Task.IncludePattern)
	if err != nil {
		return nil, nil, 0, err
	}
	queue := []crawlVisit{}
	seen := map[string]bool{}
	for _, raw := range r.Task.URLs {
		address, err := domain.NormalizeCrawlURL(raw)
		if err != nil {
			return nil, nil, 0, err
		}
		if !seen[address] {
			seen[address] = true
			queue = append(queue, crawlVisit{address, r.Task.Hops})
		}
	}
	pages := []domain.CrawlPage{}
	issues := []crawlCookieIssue{}
	completed := 0
	total := r.Total
	for start := 0; start < len(queue); {
		end := min(start+w.Concurrency.Task, len(queue))
		results := make([]domain.CrawlPage, end-start)
		failures := make([]error, end-start)
		batchCtx, cancel := context.WithCancel(ctx)
		fatal := -1
		var first sync.Once
		var group sync.WaitGroup
		for index := start; index < end; index++ {
			group.Go(func() {
				target, _ := url.Parse(queue[index].URL)
				profile := config.CrawlProfile{Origin: target.Scheme + "://" + target.Host}
				for _, candidate := range profiles {
					if candidate.Origin == profile.Origin {
						profile = candidate
						break
					}
				}
				started := time.Now()
				results[index-start], failures[index-start] = w.Source.Capture(batchCtx, profile, queue[index].URL)
				if failures[index-start] == nil && results[index-start].URL != queue[index].URL {
					failures[index-start] = fmt.Errorf("crawl source URL mismatch")
				}
				telemetry.Measure(ctx, "page_capture", started, failures[index-start])
				var login *CrawlCaptureError
				if failures[index-start] != nil && (!errors.As(failures[index-start], &login) || !login.NeedsCookieReview()) {
					first.Do(func() {
						fatal = index
						cancel()
					})
				}
			})
		}
		group.Wait()
		cancel()
		if fatal >= 0 {
			captureErr := failures[fatal-start]
			recordErr := w.recordCrawlPage(ctx, r, domain.CrawlPageProgress{Position: fatal, Title: results[fatal-start].Title, Status: "failed", Error: captureErr.Error()})
			return pages, issues, completed, fmt.Errorf("crawl page %s: %w", queue[fatal].URL, errors.Join(captureErr, recordErr))
		}
		for index := start; index < end; index++ {
			visit, page, captureErr := queue[index], results[index-start], failures[index-start]
			status, reason := "captured", ""
			var login *CrawlCaptureError
			if captureErr != nil && errors.As(captureErr, &login) && login.NeedsCookieReview() {
				target, _ := url.Parse(visit.URL)
				issues = append(issues, crawlCookieIssue{URL: visit.URL, Origin: target.Scheme + "://" + target.Host, FinalURL: login.FinalURL, Code: login.Code, Status: login.Status})
				status, reason = "needs_cookie", captureErr.Error()
			} else if page.Skipped {
				status = "skipped"
			}
			if recordErr := w.recordCrawlPage(ctx, r, domain.CrawlPageProgress{Position: index, Title: page.Title, Status: status, Error: reason}); recordErr != nil {
				return pages, issues, completed, fmt.Errorf("crawl page %s: %w", visit.URL, recordErr)
			}
			if status == "needs_cookie" {
				data, err := json.Marshal(issues)
				if err != nil {
					return pages, issues, completed, err
				}
				if err = w.putCrawlObject(ctx, CrawlPrefix(r)+"cookie-issues.json", data, "application/json"); err != nil {
					return pages, issues, completed, err
				}
			}
			if status == "captured" {
				data, err := json.Marshal(page)
				if err != nil {
					return pages, issues, completed, err
				}
				if err = w.putCrawlObject(ctx, fmt.Sprintf("%spages/%d.json", CrawlPrefix(r), index), data, "application/json"); err != nil {
					return pages, issues, completed, err
				}
				telemetry.Count(ctx, "markdown_chars", int64(len([]rune(page.Markdown))))
				format := page.Format
				if format == "" {
					format = "html"
				}
				telemetry.Count(ctx, "documents_"+format, 1)
				pages = append(pages, page)
				if visit.Hops > 0 {
					for _, raw := range page.Links {
						address, err := domain.NormalizeCrawlURL(raw)
						if err != nil {
							continue
						}
						if seen[address] || !pattern.MatchString(address) {
							continue
						}
						if len(queue) >= domain.MaxCrawlPages {
							return pages, issues, completed, fmt.Errorf("crawl exceeds limit of %d unique candidate pages", domain.MaxCrawlPages)
						}
						seen[address] = true
						queue = append(queue, crawlVisit{address, visit.Hops - 1})
					}
				}
			}
			completed++
			if len(queue) != total {
				err = crawlDB(ctx, func() error { return mysql.CrawlCaptureProgress(ctx, w.DB, r.ID, r.UserID, completed, len(queue)) })
				total = len(queue)
			} else {
				err = crawlDB(ctx, func() error { return mysql.CrawlProgress(ctx, w.DB, r.ID, r.UserID, "capture", completed) })
			}
			if err != nil {
				return pages, issues, completed, err
			}
		}
		start = end
	}
	return pages, issues, completed, ctx.Err()
}
