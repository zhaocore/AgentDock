package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/zhaocore/agentdock/internal/domain"
)

// agentColumns 是 agents 的查询列，JSON 字段以字符串读出后解码。
const agentColumns = `id, owner_user_id, name, system_prompt, model, skills, mcp_servers, subagents, created_at, updated_at`

// decodeAgent 把原始行数据解码为领域对象。
func decodeAgent(id, owner, name, systemPrompt, model string, skills, mcpServers, subagents []byte, createdAt, updatedAt string) (domain.Agent, error) {
	agent := domain.Agent{
		ID: id, OwnerUserID: owner, Name: name,
		SystemPrompt: systemPrompt, Model: model,
		Skills: []string{}, MCPServers: []domain.MCPServerConfig{}, Subagents: []domain.SubagentDef{},
	}
	if len(skills) > 0 {
		if err := json.Unmarshal(skills, &agent.Skills); err != nil {
			return domain.Agent{}, fmt.Errorf("decode agent skills: %w", err)
		}
	}
	if len(mcpServers) > 0 {
		if err := json.Unmarshal(mcpServers, &agent.MCPServers); err != nil {
			return domain.Agent{}, fmt.Errorf("decode agent mcp servers: %w", err)
		}
	}
	if len(subagents) > 0 {
		if err := json.Unmarshal(subagents, &agent.Subagents); err != nil {
			return domain.Agent{}, fmt.Errorf("decode agent subagents: %w", err)
		}
	}
	return agent, nil
}

// scanAgentRow 扫描一行 agent 数据。
func scanAgentRow(scn interface{ Scan(...any) error }) (domain.Agent, error) {
	var id, name, systemPrompt, model string
	var owner sql.NullString
	var skills, mcpServers, subagents []byte
	var createdAt, updatedAt sql.NullTime
	if err := scn.Scan(&id, &owner, &name, &systemPrompt, &model, &skills, &mcpServers, &subagents, &createdAt, &updatedAt); err != nil {
		return domain.Agent{}, fmt.Errorf("scan agent: %w", err)
	}
	agent, err := decodeAgent(id, owner.String, name, systemPrompt, model, skills, mcpServers, subagents,
		createdAt.Time.String(), updatedAt.Time.String())
	if err != nil {
		return domain.Agent{}, err
	}
	if createdAt.Valid {
		agent.CreatedAt = createdAt.Time
	}
	if updatedAt.Valid {
		agent.UpdatedAt = updatedAt.Time
	}
	return agent, nil
}

// CreateAgent 插入 Agent 定义。
func CreateAgent(ctx context.Context, db *sql.DB, agent domain.Agent) error {
	skills, err := json.Marshal(agent.Skills)
	if err != nil {
		return fmt.Errorf("encode agent skills: %w", err)
	}
	mcpServers, err := json.Marshal(agent.MCPServers)
	if err != nil {
		return fmt.Errorf("encode agent mcp servers: %w", err)
	}
	subagents, err := json.Marshal(agent.Subagents)
	if err != nil {
		return fmt.Errorf("encode agent subagents: %w", err)
	}
	_, err = db.ExecContext(ctx,
		`INSERT INTO agents (id, owner_user_id, name, system_prompt, model, skills, mcp_servers, subagents)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		agent.ID, agent.OwnerUserID, agent.Name, agent.SystemPrompt, agent.Model, skills, mcpServers, subagents)
	if err != nil {
		return fmt.Errorf("insert agent: %w", err)
	}
	return nil
}

// GetAgent 读取公共预设或当前用户的私有定义，停用预设返回不存在。
func GetAgent(ctx context.Context, db *sql.DB, id, ownerUserID string) (domain.Agent, error) {
	row := db.QueryRowContext(ctx,
		`SELECT `+agentColumns+` FROM agents WHERE id = ? AND (owner_user_id = ? OR owner_user_id IS NULL) AND enabled = TRUE`, id, ownerUserID)
	agent, err := scanAgentRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Agent{}, sql.ErrNoRows
	}
	return agent, err
}

// ListAgents 分页列出公共预设与当前用户的私有定义。
func ListAgents(ctx context.Context, db *sql.DB, ownerUserID string, limit, offset int) ([]domain.Agent, int, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT `+agentColumns+` FROM agents WHERE (owner_user_id = ? OR owner_user_id IS NULL) AND enabled = TRUE ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`,
		ownerUserID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list agents: %w", err)
	}
	defer rows.Close()
	list := []domain.Agent{}
	for rows.Next() {
		agent, err := scanAgentRow(rows)
		if err != nil {
			return nil, 0, err
		}
		list = append(list, agent)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("list agents: %w", err)
	}
	var total int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM agents WHERE (owner_user_id = ? OR owner_user_id IS NULL) AND enabled = TRUE`, ownerUserID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count agents: %w", err)
	}
	return list, total, nil
}

// UpdateAgent 全量更新 Agent 定义。
func UpdateAgent(ctx context.Context, db *sql.DB, agent domain.Agent) error {
	skills, err := json.Marshal(agent.Skills)
	if err != nil {
		return fmt.Errorf("encode agent skills: %w", err)
	}
	mcpServers, err := json.Marshal(agent.MCPServers)
	if err != nil {
		return fmt.Errorf("encode agent mcp servers: %w", err)
	}
	subagents, err := json.Marshal(agent.Subagents)
	if err != nil {
		return fmt.Errorf("encode agent subagents: %w", err)
	}
	result, err := db.ExecContext(ctx,
		`UPDATE agents SET name=?, system_prompt=?, model=?, skills=?, mcp_servers=?, subagents=?, updated_at=NOW()
		WHERE id=? AND owner_user_id=?`,
		agent.Name, agent.SystemPrompt, agent.Model, skills, mcpServers, subagents, agent.ID, agent.OwnerUserID)
	if err != nil {
		return fmt.Errorf("update agent: %w", err)
	}
	if rows, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("update agent rows: %w", err)
	} else if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteAgent 删除 Agent 定义，不存在或非 owner 返回 sql.ErrNoRows。
// 关联实例与实例上的会话由 FK ON DELETE CASCADE 一并删除。
func DeleteAgent(ctx context.Context, db *sql.DB, id, ownerUserID string) error {
	result, err := db.ExecContext(ctx,
		`DELETE FROM agents WHERE id=? AND owner_user_id=?`, id, ownerUserID)
	if err != nil {
		return fmt.Errorf("delete agent: %w", err)
	}
	if rows, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("delete agent rows: %w", err)
	} else if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}
