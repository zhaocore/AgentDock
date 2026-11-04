package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/zhaocore/agentdock/internal/domain"
)

// CreateInstance 插入 Agent 实例记录，repo_ids 以 JSON 存储。
func CreateInstance(ctx context.Context, db *sql.DB, inst domain.AgentInstance) error {
	repoIDs, err := json.Marshal(inst.RepoIDs)
	if err != nil {
		return fmt.Errorf("encode instance repo ids: %w", err)
	}
	_, err = db.ExecContext(ctx,
		`INSERT INTO agent_instances (id, agent_id, owner_user_id, status, repo_ids)
		VALUES (?, ?, ?, ?, ?)`,
		inst.ID, inst.AgentID, inst.OwnerUserID, inst.Status, repoIDs)
	if err != nil {
		return fmt.Errorf("insert agent instance: %w", err)
	}
	return nil
}

// scanInstance 扫描一行实例数据。
func scanInstance(scn interface{ Scan(...any) error }) (domain.AgentInstance, error) {
	var inst domain.AgentInstance
	var repoIDs []byte
	var createdAt, updatedAt sql.NullTime
	if err := scn.Scan(&inst.ID, &inst.AgentID, &inst.OwnerUserID, &inst.Status, &repoIDs, &createdAt, &updatedAt); err != nil {
		return domain.AgentInstance{}, fmt.Errorf("scan agent instance: %w", err)
	}
	inst.RepoIDs = []string{}
	if len(repoIDs) > 0 {
		if err := json.Unmarshal(repoIDs, &inst.RepoIDs); err != nil {
			return domain.AgentInstance{}, fmt.Errorf("decode instance repo ids: %w", err)
		}
	}
	inst.CreatedAt = createdAt.Time
	inst.UpdatedAt = updatedAt.Time
	return inst, nil
}

// GetInstance 取实例；owner 过滤保证隔离。
func GetInstance(ctx context.Context, db *sql.DB, id, ownerUserID string) (domain.AgentInstance, error) {
	row := db.QueryRowContext(ctx,
		`SELECT id, agent_id, owner_user_id, status, repo_ids, created_at, updated_at
		FROM agent_instances WHERE id = ? AND owner_user_id = ?`, id, ownerUserID)
	inst, err := scanInstance(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.AgentInstance{}, sql.ErrNoRows
	}
	return inst, err
}

// ListInstances 列出 Agent 的实例，按创建时间倒序。
func ListInstances(ctx context.Context, db *sql.DB, agentID, ownerUserID string) ([]domain.AgentInstance, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, agent_id, owner_user_id, status, repo_ids, created_at, updated_at
		FROM agent_instances WHERE agent_id = ? AND owner_user_id = ?
		ORDER BY created_at DESC, id DESC`, agentID, ownerUserID)
	if err != nil {
		return nil, fmt.Errorf("list agent instances: %w", err)
	}
	defer rows.Close()
	list := []domain.AgentInstance{}
	for rows.Next() {
		inst, err := scanInstance(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, inst)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list agent instances: %w", err)
	}
	return list, nil
}

// UpdateInstanceStatus 回写实例状态。
func UpdateInstanceStatus(ctx context.Context, db *sql.DB, id string, status domain.AgentStatus) error {
	result, err := db.ExecContext(ctx,
		`UPDATE agent_instances SET status=?, updated_at=NOW() WHERE id=?`, status, id)
	if err != nil {
		return fmt.Errorf("update agent instance status: %w", err)
	}
	if rows, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("update agent instance status rows: %w", err)
	} else if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// MarkRunningInstancesStopped 服务启动时把遗留 running 实例置为 stopped。
func MarkRunningInstancesStopped(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx,
		`UPDATE agent_instances SET status=?, updated_at=NOW() WHERE status=?`,
		domain.AgentStatusStopped, domain.AgentStatusRunning)
	if err != nil {
		return fmt.Errorf("mark running instances stopped: %w", err)
	}
	return nil
}

// CreateSession 创建会话。
func CreateSession(ctx context.Context, db *sql.DB, s domain.Session) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO sessions (id, user_id, instance_id, agent_id, title) VALUES (?, ?, ?, ?, ?)`,
		s.ID, s.UserID, s.InstanceID, s.AgentID, s.Title)
	if err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	return nil
}

// GetSession 取会话；user 过滤保证隔离，软删除记录不可见。
func GetSession(ctx context.Context, db *sql.DB, id, userID string) (domain.Session, error) {
	var s domain.Session
	var title sql.NullString
	err := db.QueryRowContext(ctx,
		`SELECT id, user_id, instance_id, agent_id, title, created_at, updated_at
		FROM sessions WHERE id = ? AND user_id = ? AND deleted_at IS NULL`, id, userID,
	).Scan(&s.ID, &s.UserID, &s.InstanceID, &s.AgentID, &title, &s.CreatedAt, &s.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Session{}, sql.ErrNoRows
	}
	if err != nil {
		return domain.Session{}, fmt.Errorf("get session: %w", err)
	}
	s.Title = title.String
	return s, nil
}

// ListSessions 分页列出当前用户未删除会话，按更新时间倒序。
func ListSessions(ctx context.Context, db *sql.DB, userID string, limit, offset int) ([]domain.Session, int, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, user_id, instance_id, agent_id, title, created_at, updated_at
		FROM sessions WHERE user_id = ? AND deleted_at IS NULL
		ORDER BY updated_at DESC, id DESC LIMIT ? OFFSET ?`, userID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()
	list := []domain.Session{}
	for rows.Next() {
		var s domain.Session
		var title sql.NullString
		if err := rows.Scan(&s.ID, &s.UserID, &s.InstanceID, &s.AgentID, &title, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan session: %w", err)
		}
		s.Title = title.String
		list = append(list, s)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("list sessions: %w", err)
	}
	var total int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sessions WHERE user_id = ? AND deleted_at IS NULL`, userID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count sessions: %w", err)
	}
	return list, total, nil
}

// DeleteSession 软删除会话，不存在返回 sql.ErrNoRows。
func DeleteSession(ctx context.Context, db *sql.DB, id, userID string) error {
	result, err := db.ExecContext(ctx,
		`UPDATE sessions SET deleted_at=NOW() WHERE id=? AND user_id=? AND deleted_at IS NULL`, id, userID)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	if rows, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("delete session rows: %w", err)
	} else if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// InsertMessage 追加一条会话消息并触碰会话更新时间。
func InsertMessage(ctx context.Context, db *sql.DB, m domain.Message) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin message tx: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO messages (id, session_id, role, content, agent_meta) VALUES (?, ?, ?, ?, ?)`,
		m.ID, m.SessionID, m.Role, m.Content, nullable(string(m.AgentMeta))); err != nil {
		return fmt.Errorf("insert message: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE sessions SET updated_at=NOW() WHERE id=?`, m.SessionID); err != nil {
		return fmt.Errorf("touch session: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit message tx: %w", err)
	}
	return nil
}

// ListMessages 按时间正序列出会话消息。
func ListMessages(ctx context.Context, db *sql.DB, sessionID string) ([]domain.Message, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, session_id, role, content, COALESCE(agent_meta, ''), created_at
		FROM messages WHERE session_id = ? ORDER BY created_at ASC, id ASC`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	defer rows.Close()
	list := []domain.Message{}
	for rows.Next() {
		var m domain.Message
		var meta []byte
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &m.Content, &meta, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		if len(meta) > 0 {
			m.AgentMeta = json.RawMessage(meta)
		}
		list = append(list, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	return list, nil
}
