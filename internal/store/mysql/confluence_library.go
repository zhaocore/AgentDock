package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/zhaocore/dutybotgo/internal/domain"
)

// ListConfluenceSources 文件库汇集全部已发布页面，不受管理页最近50任务限制。
func ListConfluenceSources(ctx context.Context, db *sql.DB, userID string) ([]domain.ConfluenceTask, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+confluenceColumns+` FROM confluence_tasks WHERE user_id=? AND stage='export' AND completed>0 ORDER BY created_at, id`, userID)
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

// CreateConfluenceImport 预览只登记元数据，不发布导入结果。
func CreateConfluenceImport(ctx context.Context, db *sql.DB, record domain.ConfluenceImport) error {
	manifest, err := json.Marshal(record.Spaces)
	if err != nil {
		return err
	}
	report, err := json.Marshal(record.Report)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `INSERT INTO confluence_library_imports (id,user_id,status,base_digest,manifest,report) VALUES (?,?,'preview',?,?,?)`, record.ID, record.UserID, record.BaseDigest, manifest, report)
	return err
}

func scanConfluenceImport(row interface{ Scan(...any) error }) (domain.ConfluenceImport, error) {
	var record domain.ConfluenceImport
	var manifest, report []byte
	if err := row.Scan(&record.ID, &record.UserID, &record.Status, &record.BaseDigest, &manifest, &report, &record.CreatedAt); err != nil {
		return record, err
	}
	if err := json.Unmarshal(manifest, &record.Spaces); err != nil {
		return record, fmt.Errorf("decode library import metadata: %w", err)
	}
	if err := json.Unmarshal(report, &record.Report); err != nil {
		return record, err
	}
	return record, nil
}

// GetConfluenceImport 预览记录与应用操作始终按用户过滤。
func GetConfluenceImport(ctx context.Context, db *sql.DB, id, userID string) (domain.ConfluenceImport, error) {
	return scanConfluenceImport(db.QueryRowContext(ctx, `SELECT id,user_id,status,base_digest,manifest,report,created_at FROM confluence_library_imports WHERE id=? AND user_id=?`, id, userID))
}

// ListConfluenceImports 仅已确认批次进入文件库。
func ListConfluenceImports(ctx context.Context, db *sql.DB, userID string) ([]domain.ConfluenceImport, error) {
	rows, err := db.QueryContext(ctx, `SELECT id,user_id,status,base_digest,manifest,report,created_at FROM confluence_library_imports WHERE user_id=? AND status='applied' ORDER BY created_at, id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := []domain.ConfluenceImport{}
	for rows.Next() {
		record, err := scanConfluenceImport(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

// ApplyConfluenceImport 用条件更新防止重复应用同一批次。
func ApplyConfluenceImport(ctx context.Context, db *sql.DB, id, userID string) error {
	result, err := db.ExecContext(ctx, `UPDATE confluence_library_imports SET status='applied' WHERE id=? AND user_id=? AND status='preview'`, id, userID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("import is no longer awaiting confirmation")
	}
	return nil
}

// LockConfluenceLibrary 串行化同一用户的导入发布；锁随独立连接关闭释放。
func LockConfluenceLibrary(ctx context.Context, db *sql.DB, userID string) (*sql.Conn, error) {
	connection, err := db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	var locked int
	if err = connection.QueryRowContext(ctx, `SELECT GET_LOCK(?,10)`, "confluence-library:"+userID).Scan(&locked); err != nil {
		connection.Close()
		return nil, err
	}
	if locked != 1 {
		connection.Close()
		return nil, fmt.Errorf("Confluence library is busy; retry import confirmation")
	}
	return connection, nil
}

// UnlockConfluenceLibrary 显式释放命名锁，连接关闭不能替代连接池内释放。
func UnlockConfluenceLibrary(ctx context.Context, connection *sql.Conn, userID string) error {
	var released int
	err := connection.QueryRowContext(ctx, `SELECT RELEASE_LOCK(?)`, "confluence-library:"+userID).Scan(&released)
	closeErr := connection.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if released != 1 {
		return fmt.Errorf("Confluence library lock was not held")
	}
	return nil
}
