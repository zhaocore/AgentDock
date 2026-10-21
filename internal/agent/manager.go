// Package agent 提供 Agent 实例生命周期管理与 langchaingo 运行时。
package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"github.com/zhaocore/dutybotgo/internal/domain"
	"github.com/zhaocore/dutybotgo/internal/store/mysql"
)

// Manager 管理进程内运行的 Agent 实例 goroutine。
// 实例本身是登记记录 + 长驻 goroutine；会话历史持久化在 MySQL，
// goroutine 只承担运行时上下文缓存（已构建的 agent 执行器）。
type Manager struct {
	db *sql.DB

	mu        sync.Mutex
	runners   map[string]*Runner // instance_id → 运行器
	runtimes  Runtime            // agent 构建运行时
}

// Runner 一个实例的运行时上下文。
type Runner struct {
	InstanceID string
	Agent      domain.Agent
	cancel     context.CancelFunc
}

// NewManager 创建实例管理器。
func NewManager(db *sql.DB, rt Runtime) *Manager {
	return &Manager{
		db:       db,
		runners:  make(map[string]*Runner),
		runtimes: rt,
	}
}

// Start 启动实例：构建 agent 执行上下文并登记运行器。
func (m *Manager) Start(ctx context.Context, inst domain.AgentInstance, agent domain.Agent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.runners[inst.ID]; exists {
		return fmt.Errorf("instance %s already running", inst.ID)
	}
	runCtx, cancel := context.WithCancel(ctx)
	// 预构建 agent 执行器，构建失败即启动失败
	if _, err := m.runtimes.Build(runCtx, agent); err != nil {
		cancel()
		return fmt.Errorf("build agent for instance %s: %w", inst.ID, err)
	}
	m.runners[inst.ID] = &Runner{InstanceID: inst.ID, Agent: agent, cancel: cancel}
	if err := mysql.UpdateInstanceStatus(ctx, m.db, inst.ID, domain.AgentStatusRunning); err != nil {
		delete(m.runners, inst.ID)
		cancel()
		return fmt.Errorf("mark instance running: %w", err)
	}
	slog.Info("agent instance started", "instance_id", inst.ID, "agent_id", agent.ID)
	return nil
}

// Stop 停止实例：取消上下文并移除运行器，不影响会话历史。
func (m *Manager) Stop(ctx context.Context, instanceID string) error {
	m.mu.Lock()
	runner, exists := m.runners[instanceID]
	if exists {
		delete(m.runners, instanceID)
	}
	m.mu.Unlock()
	if !exists {
		// 进程重启后 DB 记录仍在：直接置 stopped
		if err := mysql.UpdateInstanceStatus(ctx, m.db, instanceID, domain.AgentStatusStopped); err != nil {
			return fmt.Errorf("mark instance stopped: %w", err)
		}
		return fmt.Errorf("instance %s was not running in this process", instanceID)
	}
	runner.cancel()
	if err := mysql.UpdateInstanceStatus(ctx, m.db, instanceID, domain.AgentStatusStopped); err != nil {
		return fmt.Errorf("mark instance stopped: %w", err)
	}
	slog.Info("agent instance stopped", "instance_id", instanceID)
	return nil
}

// IsRunning 判断实例是否在本进程运行。
func (m *Manager) IsRunning(instanceID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.runners[instanceID]
	return ok
}

// SendMessage 在实例上执行一轮对话：构建（或复用）执行器并调用 LLM。
// 返回 assistant 回复与中间步骤元数据。
func (m *Manager) SendMessage(ctx context.Context, inst domain.AgentInstance, agent domain.Agent, history []domain.Message, userMessage string) (string, json.RawMessage, error) {
	m.mu.Lock()
	_, running := m.runners[inst.ID]
	rt := m.runtimes
	m.mu.Unlock()
	if !running {
		return "", nil, fmt.Errorf("instance %s is not running", inst.ID)
	}
	return rt.Execute(ctx, agent, history, userMessage)
}

// ShutdownAll 服务关闭时停止全部运行器。
func (m *Manager) ShutdownAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, runner := range m.runners {
		runner.cancel()
		delete(m.runners, id)
	}
}
