package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/zhaocore/dutybotgo/internal/domain"
)

// newManagerDB 创建带 mock 的管理器。
func newManagerDB(t *testing.T) (*Manager, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sqlmock: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return NewManager(db, &fakeRuntime{}), mock
}

func testAgent() domain.Agent {
	return domain.Agent{ID: "agent-1", Name: "bot", SystemPrompt: "sp", Model: "m",
		Skills: []string{}, MCPServers: []domain.MCPServerConfig{}, Subagents: []domain.SubagentDef{}}
}

func testInstance() domain.AgentInstance {
	return domain.AgentInstance{ID: "inst-1", AgentID: "agent-1", OwnerUserID: "u1",
		Status: domain.AgentStatusCreated, RepoIDs: []string{}}
}

// 启动成功：构建执行器并把实例状态置 running。
func TestManagerStart(t *testing.T) {
	m, mock := newManagerDB(t)
	mock.ExpectExec("UPDATE agent_instances SET status").
		WithArgs(domain.AgentStatusRunning, "inst-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := m.Start(context.Background(), testInstance(), testAgent()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !m.IsRunning("inst-1") {
		t.Error("instance should be running after start")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	m.ShutdownAll()
}

// 重复启动同一实例必须拒绝。
func TestManagerStartDuplicate(t *testing.T) {
	m, mock := newManagerDB(t)
	inst := testInstance()
	mock.ExpectExec("UPDATE agent_instances SET status").
		WithArgs(domain.AgentStatusRunning, "inst-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := m.Start(context.Background(), inst, testAgent()); err != nil {
		t.Fatalf("first start: %v", err)
	}
	err := m.Start(context.Background(), inst, testAgent())
	if err == nil || !errors.Is(err, err) || err.Error() == "" {
		t.Fatalf("duplicate start must fail, got %v", err)
	}
	m.ShutdownAll()
}

// 构建失败（如模型配置错误）时启动失败且不登记运行器。
func TestManagerStartBuildFailure(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m := NewManager(db, &fakeRuntime{buildErr: errors.New("bad model")})

	if err := m.Start(context.Background(), testInstance(), testAgent()); err == nil {
		t.Fatal("build failure should fail start")
	}
	if m.IsRunning("inst-1") {
		t.Error("failed start must not register runner")
	}
}

// 停止非本进程运行的实例：DB 记录直接置 stopped 并返回提示错误。
func TestManagerStopNotRunning(t *testing.T) {
	m, mock := newManagerDB(t)
	mock.ExpectExec("UPDATE agent_instances SET status").
		WithArgs(domain.AgentStatusStopped, "inst-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := m.Stop(context.Background(), "inst-1")
	if err == nil {
		t.Fatal("stopping non-running instance should return error hint")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 未运行实例上发消息必须拒绝。
func TestManagerSendMessageNotRunning(t *testing.T) {
	m, _ := newManagerDB(t)
	_, _, err := m.SendMessage(context.Background(), testInstance(), testAgent(), nil, "hi")
	if err == nil || err.Error() != "instance inst-1 is not running" {
		t.Fatalf("expected not-running error, got %v", err)
	}
}

// 运行中的实例可以执行对话并拿到回复。
func TestManagerSendMessageRunning(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m := NewManager(db, &fakeRuntime{executeOut: "hello"})

	inst := testInstance()
	mock.ExpectExec("UPDATE agent_instances SET status").
		WithArgs(domain.AgentStatusRunning, "inst-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := m.Start(context.Background(), inst, testAgent()); err != nil {
		t.Fatalf("start: %v", err)
	}
	answer, meta, err := m.SendMessage(context.Background(), inst, testAgent(), nil, "hi")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if answer != "hello" || string(meta) != "{}" {
		t.Errorf("answer=%q meta=%s", answer, meta)
	}
	m.ShutdownAll()
}
