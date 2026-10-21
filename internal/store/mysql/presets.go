package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/zhaocore/dutybotgo/internal/domain"
)

// SyncPresets 原子同步公共定义；停用已移除预设，保留私有记录和全部历史。
func SyncPresets(ctx context.Context, db *sql.DB, agents []domain.Agent) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin preset sync: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE agents SET enabled = FALSE WHERE owner_user_id IS NULL`); err != nil {
		return fmt.Errorf("disable removed presets: %w", err)
	}
	for _, agent := range agents {
		if agent.OwnerUserID != "" {
			return fmt.Errorf("preset %q must not have a user owner", agent.Name)
		}
		skills, err := json.Marshal(agent.Skills)
		if err != nil {
			return fmt.Errorf("encode preset skills: %w", err)
		}
		servers, err := json.Marshal(agent.MCPServers)
		if err != nil {
			return fmt.Errorf("encode preset MCP servers: %w", err)
		}
		// 锁定现有定义，禁止 ID 冲突时覆盖私有数据。
		var owner sql.NullString
		err = tx.QueryRowContext(ctx, `SELECT owner_user_id FROM agents WHERE id = ? FOR UPDATE`, agent.ID).Scan(&owner)
		if err != nil && err != sql.ErrNoRows {
			return fmt.Errorf("lock preset %q: %w", agent.Name, err)
		}
		if owner.Valid {
			return fmt.Errorf("preset %q conflicts with a private agent", agent.Name)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO agents
			(id, owner_user_id, name, system_prompt, model, skills, mcp_servers, subagents, enabled)
			VALUES (?, NULL, ?, ?, ?, ?, ?, '[]', TRUE)
			ON DUPLICATE KEY UPDATE name=VALUES(name), system_prompt=VALUES(system_prompt),
			model=VALUES(model), skills=VALUES(skills), mcp_servers=VALUES(mcp_servers), enabled=TRUE`,
			agent.ID, agent.Name, agent.SystemPrompt, agent.Model, skills, servers); err != nil {
			return fmt.Errorf("sync preset %q: %w", agent.Name, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit preset sync: %w", err)
	}
	return nil
}
