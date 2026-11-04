package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/zhaocore/agentdock/internal/domain"
)

// ErrCrawlBusy 表示任务已有在途运行；ErrCrawlDuplicate 表示同一定时槽已被领取。
var ErrCrawlBusy = errors.New("crawl task already queued or running")
var ErrCrawlDuplicate = errors.New("crawl scheduled slot already dispatched")

const crawlTaskColumns = `id,user_id,name,agent_id,profile_id,urls,enabled,profile_ids,hops,include_pattern,created_at,updated_at`
const crawlRunColumns = `id,user_id,task_id,snapshot,trigger_type,status,stage,completed,total,error,created_at,updated_at`

// scanCrawlTask 解析受限配置，拒绝损坏的数据库 JSON。
func scanCrawlTask(row interface{ Scan(...any) error }) (domain.CrawlTask, error) {
	var t domain.CrawlTask
	var urls, profiles []byte
	err := row.Scan(&t.ID, &t.UserID, &t.Name, &t.AgentID, &t.ProfileID, &urls, &t.Enabled, &profiles, &t.Hops, &t.IncludePattern, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return t, err
	}
	if err = json.Unmarshal(urls, &t.URLs); err != nil {
		return t, fmt.Errorf("decode crawl URLs: %w", err)
	}
	if err = json.Unmarshal(profiles, &t.ProfileIDs); err != nil {
		return t, fmt.Errorf("decode crawl profile IDs: %w", err)
	}
	return t, nil
}

// CreateCrawlTask 保存用户任务，URL 不含部署凭据。
func CreateCrawlTask(ctx context.Context, db *sql.DB, t domain.CrawlTask) error {
	urls, err := json.Marshal(t.URLs)
	if err != nil {
		return err
	}
	profiles, err := json.Marshal(append([]string{}, t.ProfileIDs...))
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `INSERT INTO crawl_tasks (id,user_id,name,agent_id,profile_id,urls,enabled,profile_ids,hops,include_pattern) VALUES (?,?,?,?,?,?,?,?,?,?)`, t.ID, t.UserID, t.Name, t.AgentID, t.ProfileID, urls, t.Enabled, profiles, t.Hops, t.IncludePattern)
	return err
}

// UpdateCrawlTask 更新配置但不改已冻结的运行快照。
func UpdateCrawlTask(ctx context.Context, db *sql.DB, t domain.CrawlTask) error {
	urls, err := json.Marshal(t.URLs)
	if err != nil {
		return err
	}
	profiles, err := json.Marshal(append([]string{}, t.ProfileIDs...))
	if err != nil {
		return err
	}
	r, err := db.ExecContext(ctx, `UPDATE crawl_tasks SET name=?,agent_id=?,profile_id=?,urls=?,enabled=?,profile_ids=?,hops=?,include_pattern=? WHERE id=? AND user_id=?`, t.Name, t.AgentID, t.ProfileID, urls, t.Enabled, profiles, t.Hops, t.IncludePattern, t.ID, t.UserID)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		_, err = GetCrawlTask(ctx, db, t.ID, t.UserID)
	}
	return err
}

// GetCrawlTask 以当前用户为查询边界。
func GetCrawlTask(ctx context.Context, db *sql.DB, id, user string) (domain.CrawlTask, error) {
	return scanCrawlTask(db.QueryRowContext(ctx, `SELECT `+crawlTaskColumns+` FROM crawl_tasks WHERE id=? AND user_id=?`, id, user))
}

// ListCrawlTasks 列出用户配置；后台调度另有专用的启用任务查询。
func ListCrawlTasks(ctx context.Context, db *sql.DB, user string) ([]domain.CrawlTask, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+crawlTaskColumns+` FROM crawl_tasks WHERE user_id=? ORDER BY created_at DESC,id DESC`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return readCrawlTasks(rows)
}

// EnabledCrawlTasks 仅由后台每日派发器读取全局启用配置。
func EnabledCrawlTasks(ctx context.Context, db *sql.DB) ([]domain.CrawlTask, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+crawlTaskColumns+` FROM crawl_tasks WHERE enabled=TRUE ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return readCrawlTasks(rows)
}

// readCrawlTasks 保持空列表与 API 契约一致。
func readCrawlTasks(rows *sql.Rows) ([]domain.CrawlTask, error) {
	list := []domain.CrawlTask{}
	for rows.Next() {
		t, err := scanCrawlTask(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, t)
	}
	return list, rows.Err()
}

// ClaimCrawlRun 锁定任务后领取运行，数据库唯一约束额外防止并发占用和重复定时槽。
func ClaimCrawlRun(ctx context.Context, db *sql.DB, id, user, trigger, slot string) (domain.CrawlRun, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return domain.CrawlRun{}, err
	}
	defer tx.Rollback()
	t, err := scanCrawlTask(tx.QueryRowContext(ctx, `SELECT `+crawlTaskColumns+` FROM crawl_tasks WHERE id=? AND user_id=? FOR UPDATE`, id, user))
	if err != nil {
		return domain.CrawlRun{}, err
	}
	if trigger == "scheduled" && !t.Enabled {
		return domain.CrawlRun{}, sql.ErrNoRows
	}
	if slot != "" {
		var count int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM crawl_runs WHERE task_id=? AND scheduled_slot=?`, id, slot).Scan(&count); err != nil {
			return domain.CrawlRun{}, err
		}
		if count > 0 {
			return domain.CrawlRun{}, ErrCrawlDuplicate
		}
	}
	var active int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM crawl_runs WHERE task_id=? AND status IN ('queued','running')`, id).Scan(&active); err != nil {
		return domain.CrawlRun{}, err
	}
	status, reason := "queued", ""
	if active > 0 {
		if trigger == "manual" {
			return domain.CrawlRun{}, ErrCrawlBusy
		}
		status, reason = "skipped", ErrCrawlBusy.Error()
	}
	snapshot, err := json.Marshal(t)
	if err != nil {
		return domain.CrawlRun{}, err
	}
	run := domain.CrawlRun{ID: uuid.NewString(), UserID: user, TaskID: id, Task: t, Trigger: trigger, Status: status, Stage: "dispatch", Total: len(t.URLs), Error: reason}
	var scheduled sql.NullString
	if slot != "" {
		scheduled = sql.NullString{String: slot, Valid: true}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO crawl_runs (id,user_id,task_id,snapshot,trigger_type,scheduled_slot,status,stage,total,error) VALUES (?,?,?,?,?,?,?,?,?,?)`, run.ID, user, id, snapshot, trigger, scheduled, status, run.Stage, run.Total, reason)
	if err != nil {
		return domain.CrawlRun{}, err
	}
	if err = tx.Commit(); err != nil {
		return domain.CrawlRun{}, err
	}
	return run, nil
}

// scanCrawlRun 恢复不含正文的运行快照。
func scanCrawlRun(row interface{ Scan(...any) error }) (domain.CrawlRun, error) {
	var r domain.CrawlRun
	var snapshot []byte
	err := row.Scan(&r.ID, &r.UserID, &r.TaskID, &snapshot, &r.Trigger, &r.Status, &r.Stage, &r.Completed, &r.Total, &r.Error, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		return r, err
	}
	if err = json.Unmarshal(snapshot, &r.Task); err != nil {
		return r, fmt.Errorf("decode crawl snapshot: %w", err)
	}
	r.Task.UserID = r.UserID
	r.Task.ProfileIDs = append([]string{}, r.Task.ProfileIDs...)
	return r, nil
}

// GetCrawlRun 运行和对象读取始终按用户隔离。
func GetCrawlRun(ctx context.Context, db *sql.DB, id, user string) (domain.CrawlRun, error) {
	return scanCrawlRun(db.QueryRowContext(ctx, `SELECT `+crawlRunColumns+` FROM crawl_runs WHERE id=? AND user_id=?`, id, user))
}

// ListCrawlRuns 仅列出该用户该任务最近一百轮。
func ListCrawlRuns(ctx context.Context, db *sql.DB, task, user string) ([]domain.CrawlRun, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+crawlRunColumns+` FROM crawl_runs WHERE task_id=? AND user_id=? ORDER BY created_at DESC,id DESC LIMIT 100`, task, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []domain.CrawlRun{}
	for rows.Next() {
		r, err := scanCrawlRun(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, r)
	}
	return list, rows.Err()
}

// StartCrawlRun 只允许一个 Worker 执行一次领取。
func StartCrawlRun(ctx context.Context, db *sql.DB, id, user string) (bool, error) {
	r, err := db.ExecContext(ctx, `UPDATE crawl_runs SET status='running',stage='capture' WHERE id=? AND user_id=? AND status='queued'`, id, user)
	if err != nil {
		return false, err
	}
	n, err := r.RowsAffected()
	return n == 1, err
}

// CrawlProgress 只更新运行中轮次，不使终态复活。
func CrawlProgress(ctx context.Context, db *sql.DB, id, user, stage string, completed int) error {
	r, err := db.ExecContext(ctx, `UPDATE crawl_runs SET stage=?,completed=? WHERE id=? AND user_id=? AND status='running'`, stage, completed, id, user)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		run, err := GetCrawlRun(ctx, db, id, user)
		if err != nil {
			return err
		}
		if run.Status != "running" {
			return fmt.Errorf("crawl run is no longer running")
		}
	}
	return nil
}

// FinishCrawlRun 报告写入后才能发布完整或部分结果；终态释放唯一占用。
func FinishCrawlRun(ctx context.Context, db *sql.DB, id, user, status, reason string) error {
	result, err := db.ExecContext(ctx, `UPDATE crawl_runs SET status=?,error=?,stage=IF(? IN ('succeeded','partial'),'published',stage) WHERE id=? AND user_id=? AND status IN ('queued','running')`, status, reason, status, id, user)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed > 0 {
		return nil
	}
	run, err := GetCrawlRun(ctx, db, id, user)
	if err != nil {
		return err
	}
	if run.Status != status {
		return fmt.Errorf("crawl run is already %s; cannot publish %s", run.Status, status)
	}
	return nil
}

// RecordCrawlPage 保存页面元数据；不接受页面正文。
func RecordCrawlPage(ctx context.Context, db *sql.DB, run, user string, p domain.CrawlPageProgress) error {
	_, err := db.ExecContext(ctx, `INSERT INTO crawl_pages (run_id,position,status,title,error) SELECT id,?,?,?,? FROM crawl_runs WHERE id=? AND user_id=? AND status='running' ON DUPLICATE KEY UPDATE status=VALUES(status),title=VALUES(title),error=VALUES(error)`, p.Position, p.Status, p.Title, p.Error, run, user)
	return err
}

// ResetCrawlRuns 将重启前在途轮次明确置失败，旧队列消息不再执行。
func ResetCrawlRuns(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `UPDATE crawl_runs SET status='failed',error='service restarted before run completed' WHERE status IN ('queued','running')`)
	return err
}

// UnfinishedCrawlRuns 查询待派发或尚未成功写入终态的轮次。
func UnfinishedCrawlRuns(ctx context.Context, db *sql.DB) ([]domain.CrawlRun, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+crawlRunColumns+` FROM crawl_runs WHERE status IN ('queued','running') ORDER BY updated_at,id LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []domain.CrawlRun{}
	for rows.Next() {
		r, err := scanCrawlRun(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, r)
	}
	return list, rows.Err()
}

// CrawlCaptureProgress 随递归发现同步候选总量及已处理数量。
func CrawlCaptureProgress(ctx context.Context, db *sql.DB, id, user string, completed, total int) error {
	result, err := db.ExecContext(ctx, `UPDATE crawl_runs SET total=? WHERE id=? AND user_id=? AND status='running'`, total, id, user)
	if err != nil {
		return err
	}
	if _, err = result.RowsAffected(); err != nil {
		return err
	}
	return CrawlProgress(ctx, db, id, user, "capture", completed)
}

// CrawlCapturedPositions 只返回当前用户已完成的页面位置，不读取正文。
func CrawlCapturedPositions(ctx context.Context, db *sql.DB, run, user string) ([]int, error) {
	rows, err := db.QueryContext(ctx, `SELECT p.position FROM crawl_pages p JOIN crawl_runs r ON r.id=p.run_id WHERE r.id=? AND r.user_id=? AND p.status='captured' ORDER BY p.position LIMIT 101`, run, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	positions := []int{}
	for rows.Next() {
		var position int
		if err = rows.Scan(&position); err != nil {
			return nil, err
		}
		positions = append(positions, position)
	}
	return positions, rows.Err()
}
