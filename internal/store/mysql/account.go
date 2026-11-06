package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/zhaocore/agentdock/internal/domain"
)

var ErrRegistrationClosed = errors.New("registration is disabled; ask administrator to create an account")
var ErrAccountInactive = errors.New("account is disabled or being deleted")
var ErrProtectedAdministrator = errors.New("administrator account cannot be disabled or deleted")

const accountColumns = `u.id,u.email,u.status,u.auth_version,u.must_change_password,(u.id=a.admin_user_id),u.created_at`

func scanAccount(row interface{ Scan(...any) error }) (domain.Account, error) {
	var u domain.Account
	err := row.Scan(&u.ID, &u.Email, &u.Status, &u.AuthVersion, &u.MustChangePassword, &u.IsAdmin, &u.CreatedAt)
	return u, err
}

// GetAccount 每次鉴权读取当前状态，不缓存已撤销身份。
func GetAccount(ctx context.Context, db *sql.DB, id string) (domain.Account, error) {
	return scanAccount(db.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM users u CROSS JOIN administration a WHERE a.id=1 AND u.id=?`, id))
}

// FindAccount 登录查询返回密码哈希，调用者不得记录哈希。
func FindAccount(ctx context.Context, db *sql.DB, email string) (domain.Account, string, error) {
	var id, hash string
	err := db.QueryRowContext(ctx, `SELECT id,password_hash FROM users WHERE email=?`, email).Scan(&id, &hash)
	if err != nil {
		return domain.Account{}, "", err
	}
	u, err := GetAccount(ctx, db, id)
	return u, hash, err
}

// ListAccounts 列出账户，不返回任何密码或密钥。
func ListAccounts(ctx context.Context, db *sql.DB) ([]domain.Account, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+accountColumns+` FROM users u CROSS JOIN administration a WHERE a.id=1 ORDER BY u.created_at,u.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []domain.Account{}
	for rows.Next() {
		u, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, u)
	}
	return list, rows.Err()
}

// GetAdministration 读取持久化注册开关及管理员。
func GetAdministration(ctx context.Context, db *sql.DB) (domain.Administration, error) {
	var s domain.Administration
	err := db.QueryRowContext(ctx, `SELECT COALESCE(admin_user_id,''),registration_enabled FROM administration WHERE id=1`).Scan(&s.AdminUserID, &s.RegistrationEnabled)
	return s, err
}

// RegisterAccount 用固定设置行锁串行判定首管理员与注册开关。
func RegisterAccount(ctx context.Context, db *sql.DB, email, hash string) (domain.Account, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Account{}, err
	}
	defer tx.Rollback()
	var admin sql.NullString
	var enabled bool
	if err = tx.QueryRowContext(ctx, `SELECT admin_user_id,registration_enabled FROM administration WHERE id=1 FOR UPDATE`).Scan(&admin, &enabled); err != nil {
		return domain.Account{}, err
	}
	if !enabled {
		return domain.Account{}, ErrRegistrationClosed
	}
	id := uuid.NewString()
	if _, err = tx.ExecContext(ctx, `INSERT INTO users (id,email,password_hash) VALUES (?,?,?)`, id, email, hash); err != nil {
		return domain.Account{}, err
	}
	if !admin.Valid {
		if _, err = tx.ExecContext(ctx, `UPDATE administration SET admin_user_id=? WHERE id=1 AND admin_user_id IS NULL`, id); err != nil {
			return domain.Account{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return domain.Account{}, err
	}
	return domain.Account{ID: id, Email: email, Status: "active", IsAdmin: !admin.Valid}, nil
}

// CreateAccount 管理员建号必须通过临时密码进入强制改密流程。
func CreateAccount(ctx context.Context, db *sql.DB, actor, email, hash string) (string, error) {
	id := uuid.NewString()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO users (id,email,password_hash,must_change_password) VALUES (?,?,?,TRUE)`, id, email, hash); err != nil {
		return "", err
	}
	if err = writeAudit(ctx, tx, actor, id, "account.create", "succeeded"); err != nil {
		return "", err
	}
	return id, tx.Commit()
}

// SetRegistration 注册开关与审计原子更新。
func SetRegistration(ctx context.Context, db *sql.DB, actor string, enabled bool) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE administration SET registration_enabled=? WHERE id=1`, enabled); err != nil {
		return err
	}
	if err = writeAudit(ctx, tx, actor, "registration", "registration.update", "succeeded"); err != nil {
		return err
	}
	return tx.Commit()
}

func writeAudit(ctx context.Context, tx *sql.Tx, actor, target, action, result string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO admin_audit (actor_id,target_id,action,result) VALUES (?,?,?,?)`, actor, target, action, result)
	return err
}

// Audit 保存管理操作结果，不保存请求正文。
func Audit(ctx context.Context, db *sql.DB, actor, target, action, result string) error {
	_, err := db.ExecContext(ctx, `INSERT INTO admin_audit (actor_id,target_id,action,result) VALUES (?,?,?,?)`, actor, target, action, result)
	return err
}

// SetAccountStatus 先冻结身份并撤销 token；启用不恢复旧任务或凭据。
func SetAccountStatus(ctx context.Context, db *sql.DB, actor, id, status string) error {
	if status != "active" && status != "disabled" && status != "deleting" {
		return fmt.Errorf("invalid account status %q", status)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var admin string
	if err = tx.QueryRowContext(ctx, `SELECT admin_user_id FROM administration WHERE id=1 FOR UPDATE`).Scan(&admin); err != nil {
		return err
	}
	if id == admin {
		return ErrProtectedAdministrator
	}
	var current string
	if err = tx.QueryRowContext(ctx, `SELECT status FROM users WHERE id=? FOR UPDATE`, id).Scan(&current); err != nil {
		return err
	}
	if current == "deleting" && status != "deleting" {
		return fmt.Errorf("account deletion is in progress")
	}
	if _, err = tx.ExecContext(ctx, `UPDATE users SET status=?,auth_version=auth_version+1 WHERE id=?`, status, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE api_tokens SET revoked_at=COALESCE(revoked_at,NOW()) WHERE tenant_id=?`, id); err != nil {
		return err
	}
	if status != "active" {
		if _, err = tx.ExecContext(ctx, `UPDATE crawl_tasks SET enabled=FALSE WHERE user_id=?`, id); err != nil {
			return err
		}
	}
	if err = writeAudit(ctx, tx, actor, id, "account."+status, "started"); err != nil {
		return err
	}
	return tx.Commit()
}

// ResetAccountPassword 重置不改变禁用状态，同时使所有旧身份失效。
func ResetAccountPassword(ctx context.Context, db *sql.DB, actor, id, hash string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE users SET password_hash=?,must_change_password=TRUE,auth_version=auth_version+1 WHERE id=? AND status<>'deleting'`, hash, id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return sql.ErrNoRows
	}
	if _, err = tx.ExecContext(ctx, `UPDATE api_tokens SET revoked_at=COALESCE(revoked_at,NOW()) WHERE tenant_id=?`, id); err != nil {
		return err
	}
	if err = writeAudit(ctx, tx, actor, id, "account.password-reset", "succeeded"); err != nil {
		return err
	}
	return tx.Commit()
}

// ChangeAccountPassword 比对旧哈希，避免并发重置后旧请求覆盖新密码。
func ChangeAccountPassword(ctx context.Context, db *sql.DB, id, oldHash, newHash string) error {
	r, err := db.ExecContext(ctx, `UPDATE users SET password_hash=?,must_change_password=FALSE WHERE id=? AND password_hash=? AND status='active'`, newHash, id, oldHash)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("account or password changed; sign in again")
	}
	return nil
}

// AccountResources 汇总删除前全部资源，消息随会话保持。
func AccountResources(ctx context.Context, db *sql.DB, user string) (domain.ResourceCounts, error) {
	var c domain.ResourceCounts
	err := db.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM agents WHERE owner_user_id=?),(SELECT COUNT(*) FROM agent_instances WHERE owner_user_id=?),(SELECT COUNT(*) FROM sessions WHERE user_id=?),(SELECT COUNT(*) FROM messages m JOIN sessions s ON s.id=m.session_id WHERE s.user_id=?),(SELECT COUNT(*) FROM confluence_tasks WHERE user_id=?),(SELECT COUNT(*) FROM confluence_library_imports WHERE user_id=? AND status='applied'),(SELECT COUNT(*) FROM crawl_tasks WHERE user_id=?),(SELECT COUNT(*) FROM crawl_runs WHERE user_id=?),(SELECT COUNT(*) FROM api_tokens WHERE tenant_id=?)`, user, user, user, user, user, user, user, user, user).Scan(&c.Agents, &c.Instances, &c.Sessions, &c.Messages, &c.ConfluenceTasks, &c.Imports, &c.CrawlTasks, &c.CrawlRuns, &c.Tokens)
	return c, err
}

// StopAccountRecords 只在所有执行者退出后收敛任务状态。
func StopAccountRecords(ctx context.Context, db *sql.DB, user string) error {
	for _, query := range []string{`UPDATE api_tokens SET revoked_at=COALESCE(revoked_at,NOW()) WHERE tenant_id=?`, `UPDATE crawl_tasks SET enabled=FALSE WHERE user_id=?`, `UPDATE crawl_runs SET status='failed',error='account work stopped by administrator' WHERE user_id=? AND status IN ('queued','running')`, `UPDATE confluence_tasks SET status='stopped' WHERE user_id=? AND status IN ('queued','running','stopping','selecting')`, `UPDATE agent_instances SET status='stopped' WHERE owner_user_id=?`} {
		if _, err := db.ExecContext(ctx, query, user); err != nil {
			return err
		}
	}
	return nil
}

// AccountInstanceIDs 返回需要停止的实例，不依赖 Agent 是否仍启用。
func AccountInstanceIDs(ctx context.Context, db *sql.DB, user string) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT id FROM agent_instances WHERE owner_user_id=?`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// LockAccountRuntime 强制既有单进程运行边界，避免多个进程遗漏彼此的在途操作。
func LockAccountRuntime(ctx context.Context, db *sql.DB) (*sql.Conn, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	var locked int
	if err = conn.QueryRowContext(ctx, `SELECT GET_LOCK(CONCAT('agentdock-runtime:',DATABASE()),0)`).Scan(&locked); err != nil {
		conn.Close()
		return nil, err
	}
	if locked != 1 {
		conn.Close()
		return nil, fmt.Errorf("another AgentDock process owns this database; stop it before starting this server")
	}
	return conn, nil
}

// StopManagedCrawl 原子停用配置并阻止已排队轮次领取，返回需要等待退出的运行 ID。
func StopManagedCrawl(ctx context.Context, db *sql.DB, user, id string) ([]string, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var taskID string
	if err = tx.QueryRowContext(ctx, `SELECT id FROM crawl_tasks WHERE id=? AND user_id=? FOR UPDATE`, id, user).Scan(&taskID); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE crawl_tasks SET enabled=FALSE WHERE id=? AND user_id=?`, id, user); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM crawl_runs WHERE task_id=? AND user_id=? AND status IN ('queued','running')`, id, user)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for rows.Next() {
		var run string
		if err = rows.Scan(&run); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, run)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE crawl_runs SET status='failed',error='stopped by administrator' WHERE task_id=? AND user_id=? AND status IN ('queued','running')`, id, user); err != nil {
		return nil, err
	}
	return ids, tx.Commit()
}

// DeleteManagedLibrary 仅移除目录引用，原始对象与任务检查点保留。
func DeleteManagedLibrary(ctx context.Context, db *sql.DB, user string) (resultErr error) {
	lock, err := LockConfluenceLibrary(ctx, db, user)
	if err != nil {
		return err
	}
	defer func() {
		release, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		resultErr = errors.Join(resultErr, UnlockConfluenceLibrary(release, lock, user))
	}()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{`UPDATE confluence_tasks SET library_visible=FALSE WHERE user_id=?`, `UPDATE confluence_library_imports SET status='archived' WHERE user_id=? AND status='applied'`} {
		if _, err = tx.ExecContext(ctx, q, user); err != nil {
			return err
		}
	}
	return tx.Commit()
}
