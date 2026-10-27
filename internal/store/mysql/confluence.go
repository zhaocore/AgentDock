package mysql

import (
	"context"
	"database/sql"

	"github.com/zhaocore/dutybotgo/internal/domain"
)

const confluenceColumns = `id, user_id, url, status, stage, completed, total, error, created_at, updated_at`

// scanConfluence 读取持久化进度。
func scanConfluence(row interface{ Scan(...any) error }) (domain.ConfluenceTask, error) {
	var task domain.ConfluenceTask
	err := row.Scan(&task.ID, &task.UserID, &task.URL, &task.Status, &task.Stage, &task.Completed, &task.Total, &task.Error, &task.CreatedAt, &task.UpdatedAt)
	return task, err
}

// CreateConfluenceTask 创建排队任务。
func CreateConfluenceTask(ctx context.Context, db *sql.DB, task domain.ConfluenceTask) error {
	_, err := db.ExecContext(ctx, `INSERT INTO confluence_tasks (id,user_id,url,error) VALUES (?,?,?,'')`, task.ID, task.UserID, task.URL)
	return err
}

// CreateConfluenceSelection 创建复用冻结目录的新批次，保留原任务检查点。
func CreateConfluenceSelection(ctx context.Context, db *sql.DB, task domain.ConfluenceTask) error {
	_, err := db.ExecContext(ctx, `INSERT INTO confluence_tasks (id,user_id,url,status,stage,total,error) VALUES (?,?,?,'waiting','selection',?,'')`, task.ID, task.UserID, task.URL, task.Total)
	return err
}

// GetConfluenceTask 所有读取必须包含用户范围。
func GetConfluenceTask(ctx context.Context, db *sql.DB, id, userID string) (domain.ConfluenceTask, error) {
	return scanConfluence(db.QueryRowContext(ctx, `SELECT `+confluenceColumns+` FROM confluence_tasks WHERE id=? AND user_id=?`, id, userID))
}

// ListConfluenceTasks 返回当前用户最近的任务。
func ListConfluenceTasks(ctx context.Context, db *sql.DB, userID string) ([]domain.ConfluenceTask, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+confluenceColumns+` FROM confluence_tasks WHERE user_id=? ORDER BY created_at DESC, id DESC LIMIT 50`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tasks := []domain.ConfluenceTask{}
	for rows.Next() {
		task, err := scanConfluence(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

// TransitionConfluenceTask 以状态条件防止停止、继续和运行之间丢失更新。
func TransitionConfluenceTask(ctx context.Context, db *sql.DB, id, userID, from, to string) (bool, error) {
	result, err := db.ExecContext(ctx, `UPDATE confluence_tasks SET status=?, error='' WHERE id=? AND user_id=? AND status=?`, to, id, userID, from)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

// ProgressConfluenceTask 只在运行中提交进度，停止后保留此前检查点。
func ProgressConfluenceTask(ctx context.Context, db *sql.DB, id, userID, stage string, completed, total int) error {
	_, err := db.ExecContext(ctx, `UPDATE confluence_tasks SET stage=?, completed=?, total=? WHERE id=? AND user_id=? AND status='running'`, stage, completed, total, id, userID)
	return err
}

// QueueConfluenceSelection 原子冻结范围计数，仅允许已领取的选择进入导出队列。
func QueueConfluenceSelection(ctx context.Context, db *sql.DB, id, userID string, total int) (bool, error) {
	result, err := db.ExecContext(ctx, `UPDATE confluence_tasks SET status='queued', stage='markdown', completed=0, total=?, error='' WHERE id=? AND user_id=? AND status='selecting' AND stage='selection'`, total, id, userID)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

// FailConfluenceTask 记录明确错误，不覆盖停止请求。
func FailConfluenceTask(ctx context.Context, db *sql.DB, id, userID, message string) error {
	_, err := db.ExecContext(ctx, `UPDATE confluence_tasks SET status='failed', error=? WHERE id=? AND user_id=? AND status IN ('queued','running','selecting')`, message, id, userID)
	return err
}

// ResetConfluenceTasks 将服务中断时未完成的运行标记为可继续，保留检查点。
func ResetConfluenceTasks(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `UPDATE confluence_tasks SET status='stopped' WHERE status IN ('running','stopping')`)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `UPDATE confluence_tasks SET status='waiting' WHERE status='selecting' AND stage='selection'`)
	return err
}
