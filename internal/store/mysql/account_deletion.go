package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/zhaocore/agentdock/internal/domain"
)

// BeginAccountDeletion 持久化已确认的预览，重复调用只更新待重试基线。
func BeginAccountDeletion(ctx context.Context, db *sql.DB, user, admin, digest string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var destination string
	if err = tx.QueryRowContext(ctx, `SELECT admin_user_id FROM administration WHERE id=1 FOR UPDATE`).Scan(&destination); err != nil {
		return err
	}
	if destination != admin || user == admin {
		return ErrProtectedAdministrator
	}
	var status string
	if err = tx.QueryRowContext(ctx, `SELECT status FROM users WHERE id=? FOR UPDATE`, user).Scan(&status); err != nil {
		return err
	}
	for _, q := range []string{`UPDATE users SET status='deleting',auth_version=auth_version+1 WHERE id=?`, `UPDATE api_tokens SET revoked_at=COALESCE(revoked_at,NOW()) WHERE tenant_id=?`, `UPDATE crawl_tasks SET enabled=FALSE WHERE user_id=?`} {
		if _, err = tx.ExecContext(ctx, q, user); err != nil {
			return err
		}
	}
	if err = writeAudit(ctx, tx, admin, user, "account.delete", "started"); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO account_deletions (user_id,admin_user_id,stage,error,preview_digest) VALUES (?,?,'frozen','',?) ON DUPLICATE KEY UPDATE preview_digest=VALUES(preview_digest),error='',stage='frozen'`, user, admin, digest); err != nil {
		return err
	}
	return tx.Commit()
}

// DeletionError 保留明确错误与冻结状态供人工检查或重试。
func DeletionError(ctx context.Context, db *sql.DB, user, message string) error {
	_, err := db.ExecContext(ctx, `UPDATE account_deletions SET stage='failed',error=? WHERE user_id=? AND stage<>'completed'`, message, user)
	return err
}

// PendingAccountDeletions 启动与定时恢复未完成的已授权删除。
func PendingAccountDeletions(ctx context.Context, db *sql.DB) ([]domain.AccountDeletion, error) {
	rows, err := db.QueryContext(ctx, `SELECT user_id,admin_user_id,stage,error FROM account_deletions WHERE stage IN ('frozen','stopping')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	jobs := []domain.AccountDeletion{}
	for rows.Next() {
		var j domain.AccountDeletion
		if err = rows.Scan(&j.UserID, &j.AdminUserID, &j.Stage, &j.Error); err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

// GetAccountDeletion 删除账户后仍可读取最终状态。
func GetAccountDeletion(ctx context.Context, db *sql.DB, user string) (domain.AccountDeletion, string, error) {
	var j domain.AccountDeletion
	var digest string
	err := db.QueryRowContext(ctx, `SELECT user_id,admin_user_id,stage,error,preview_digest FROM account_deletions WHERE user_id=?`, user).Scan(&j.UserID, &j.AdminUserID, &j.Stage, &j.Error, &digest)
	return j, digest, err
}

// TransferAccountResources 所有写入者停止后一次事务转移引用、保留历史并删除账户。
func TransferAccountResources(ctx context.Context, db *sql.DB, user, admin string, spaces []domain.ConfluenceLibrarySpace, report domain.ConfluenceImportReport) error {
	if user == admin {
		return ErrProtectedAdministrator
	}
	manifest, err := json.Marshal(spaces)
	if err != nil {
		return err
	}
	reportJSON, err := json.Marshal(report)
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var destination string
	if err = tx.QueryRowContext(ctx, `SELECT admin_user_id FROM administration WHERE id=1 FOR UPDATE`).Scan(&destination); err != nil {
		return err
	}
	if destination != admin {
		return ErrProtectedAdministrator
	}
	var status string
	if err = tx.QueryRowContext(ctx, `SELECT status FROM users WHERE id=? FOR UPDATE`, user).Scan(&status); err != nil {
		return err
	}
	if status != "deleting" {
		return fmt.Errorf("account must be frozen for deletion")
	}
	queries := []string{
		`UPDATE agents SET owner_user_id=?,mcp_servers=JSON_ARRAY() WHERE owner_user_id=?`,
		`UPDATE agent_instances SET owner_user_id=?,status='stopped' WHERE owner_user_id=?`,
		`UPDATE sessions SET user_id=? WHERE user_id=?`,
		`UPDATE confluence_tasks SET storage_user_id=IF(storage_user_id='',user_id,storage_user_id),user_id=?,library_visible=FALSE WHERE user_id=?`,
		`UPDATE confluence_library_imports SET user_id=?,status='archived' WHERE user_id=? AND status<>'preview'`,
		`UPDATE crawl_tasks SET user_id=?,enabled=FALSE,profile_id='',profile_ids=JSON_ARRAY() WHERE user_id=?`,
		`UPDATE crawl_runs SET storage_user_id=IF(storage_user_id='',user_id,storage_user_id),user_id=? WHERE user_id=?`,
	}
	for _, query := range queries {
		if _, err = tx.ExecContext(ctx, query, admin, user); err != nil {
			return err
		}
	}
	if len(spaces) > 0 {
		if _, err = tx.ExecContext(ctx, `INSERT INTO confluence_library_imports (id,user_id,status,base_digest,manifest,report) VALUES (?,?,'applied','',?,?)`, uuid.NewString(), admin, manifest, reportJSON); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM confluence_library_imports WHERE user_id=? AND status='preview'`, user); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM users WHERE id=? AND status='deleting'`, user); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE account_deletions SET stage='completed',error='' WHERE user_id=?`, user); err != nil {
		return err
	}
	if err = writeAudit(ctx, tx, admin, user, "account.delete", "succeeded"); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteManagedConfluence 删除已停止导出记录及其文件库可见性，不清理共享对象。
func DeleteManagedConfluence(ctx context.Context, db *sql.DB, user, id string) error {
	r, err := db.ExecContext(ctx, `DELETE FROM confluence_tasks WHERE id=? AND user_id=? AND status NOT IN ('queued','running','stopping','selecting')`, id, user)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("task missing or still active; stop it first")
	}
	return nil
}

// DeleteManagedCrawl 删除任务与运行索引，原始对象不被跨资源清理。
func DeleteManagedCrawl(ctx context.Context, db *sql.DB, user, id string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var active int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM crawl_runs WHERE task_id=? AND user_id=? AND status IN ('queued','running')`, id, user).Scan(&active); err != nil {
		return err
	}
	if active > 0 {
		return fmt.Errorf("crawl task still active; stop it first")
	}
	for _, q := range []string{`DELETE p FROM crawl_pages p JOIN crawl_runs r ON r.id=p.run_id WHERE r.task_id=? AND r.user_id=?`, `DELETE FROM crawl_runs WHERE task_id=? AND user_id=?`, `DELETE FROM crawl_tasks WHERE id=? AND user_id=?`} {
		if _, err = tx.ExecContext(ctx, q, id, user); err != nil {
			return err
		}
	}
	return tx.Commit()
}
