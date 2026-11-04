package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/hibiken/asynq"

	"github.com/zhaocore/agentdock/internal/domain"
)

// setupAgentHandler 创建带 mock DB 与实例管理器注入的 Handler。
func setupAgentHandler() (*Handler, sqlmock.Sqlmock) {
	db, mock, _ := sqlmock.New()
	h := setupHandler()
	h.DB = db
	return h, mock
}

// --- Agent CRUD ---

func TestCreateAgentHandler(t *testing.T) {
	h, mock := setupAgentHandler()
	defer h.DB.Close()
	mock.ExpectExec("INSERT INTO agents").WillReturnResult(sqlmock.NewResult(1, 1))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("user_id", "u1")
	c.Request = httptest.NewRequest(http.MethodPost, "/agents",
		strings.NewReader(`{"name":"bot","system_prompt":"sp","model":"gpt-4o-mini"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	h.CreateAgent(c)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"skills":[]`) {
		t.Errorf("nil skills should be normalized to [], body=%s", w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCreateAgentHandler_BadRequest(t *testing.T) {
	h, _ := setupAgentHandler()
	defer h.DB.Close()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/agents",
		strings.NewReader(`{"name":"bot"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	h.CreateAgent(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing system_prompt/model should be 400, got %d", w.Code)
	}
}

func TestGetAgentHandler_NotFound(t *testing.T) {
	h, mock := setupAgentHandler()
	defer h.DB.Close()
	mock.ExpectQuery("SELECT").WillReturnError(sql.ErrNoRows)

	// 非 owner 与不存在都返回 404，不泄露存在性
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("user_id", "u1")
	c.Params = gin.Params{{Key: "agent_id", Value: "22222222-2222-2222-2222-222222222222"}}
	c.Request = httptest.NewRequest(http.MethodGet, "/agents/x", nil)
	h.GetAgent(c)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestListAgentsHandler_BadPagination(t *testing.T) {
	h, _ := setupAgentHandler()
	defer h.DB.Close()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/agents?limit=0", nil)
	h.ListAgents(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("limit=0 should be 400, got %d", w.Code)
	}
}

// --- Repository ---

func TestCreateRepositoryHandler(t *testing.T) {
	h, mock := setupAgentHandler()
	defer h.DB.Close()
	// 同名检查：无同名
	mock.ExpectQuery("SELECT").WillReturnError(sql.ErrNoRows)
	mock.ExpectExec("INSERT INTO repositories").WillReturnResult(sqlmock.NewResult(1, 1))
	// 入队成功（AsynqClient 为 nil 时 Enqueue 会 panic，这里注入 fake）
	h.AsynqClient = &fakeEnqueuer{}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("user_id", "u1")
	c.Request = httptest.NewRequest(http.MethodPost, "/repositories",
		strings.NewReader(`{"name":"web","git_url":"https://example.com/web.git","repo_type":"frontend"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	h.CreateRepository(c)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"branch":"main"`) {
		t.Errorf("branch should default to main, body=%s", w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCreateRepositoryHandler_InvalidType(t *testing.T) {
	h, _ := setupAgentHandler()
	defer h.DB.Close()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/repositories",
		strings.NewReader(`{"name":"web","git_url":"https://example.com/web.git","repo_type":"game"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	h.CreateRepository(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid repo_type should be 400, got %d", w.Code)
	}
}

func TestCreateRepositoryHandler_DuplicateName(t *testing.T) {
	h, mock := setupAgentHandler()
	defer h.DB.Close()
	rows := sqlmock.NewRows([]string{"id", "name", "git_url", "repo_type", "branch", "local_path", "status", "last_error", "created_at", "updated_at"}).
		AddRow("id-1", "web", "https://x.git", "frontend", "main", nil, "pending", nil, nowStr(), nowStr())
	mock.ExpectQuery("SELECT").WillReturnRows(rows)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/repositories",
		strings.NewReader(`{"name":"web","git_url":"https://example.com/web.git","repo_type":"frontend"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	h.CreateRepository(c)
	if w.Code != http.StatusConflict {
		t.Fatalf("duplicate name should be 409, got %d", w.Code)
	}
}

// --- Session ---

func TestSendMessageHandler_InstanceNotRunning(t *testing.T) {
	h, mock := setupAgentHandler()
	defer h.DB.Close()
	mock.ExpectQuery("SELECT").WillReturnRows(
		sqlmock.NewRows([]string{"id", "user_id", "instance_id", "agent_id", "title", "created_at", "updated_at"}).
			AddRow("s-1", "u1", "inst-1", "agent-1", "t", nowStr(), nowStr()))
	mock.ExpectQuery("SELECT").WillReturnRows(
		sqlmock.NewRows([]string{"id", "agent_id", "owner_user_id", "status", "repo_ids", "created_at", "updated_at"}).
			AddRow("inst-1", "agent-1", "u1", "stopped", "[]", nowStr(), nowStr()))
	mock.ExpectQuery("SELECT").WillReturnRows(
		sqlmock.NewRows([]string{"id", "owner_user_id", "name", "system_prompt", "model", "skills", "mcp_servers", "subagents", "created_at", "updated_at"}).
			AddRow("agent-1", "u1", "bot", "sp", "m", "[]", "[]", "[]", nowStr(), nowStr()))

	// 实例未运行：拒绝发消息
	h.Instances = &notRunningManager{}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("user_id", "u1")
	c.Params = gin.Params{{Key: "session_id", Value: "44444444-4444-4444-4444-444444444444"}}
	c.Request = httptest.NewRequest(http.MethodPost, "/sessions/s/messages",
		strings.NewReader(`{"content":"hi"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	h.SendMessage(c)

	if w.Code != http.StatusConflict {
		t.Fatalf("not-running instance should be 409, got %d body=%s", w.Code, w.Body.String())
	}
}

// nowStr 测试时间列统一用 time.Time 值，避免 driver 类型不匹配。
func nowStr() time.Time { return time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC) }

// fakeEnqueuer 测试用入队客户端。
type fakeEnqueuer struct{}

func (f *fakeEnqueuer) EnqueueContext(_ context.Context, _ *asynq.Task, _ ...asynq.Option) (*asynq.TaskInfo, error) {
	return &asynq.TaskInfo{}, nil
}
func (f *fakeEnqueuer) Close() error { return nil }

// notRunningManager 恒定报告未运行的实例管理器。
type notRunningManager struct{}

func (m *notRunningManager) Start(context.Context, domain.AgentInstance, domain.Agent) error {
	return errors.New("not running")
}
func (m *notRunningManager) Stop(_ context.Context, _ string) error { return nil }
func (m *notRunningManager) IsRunning(string) bool                  { return false }
func (m *notRunningManager) SendMessage(_ context.Context, _ domain.AgentInstance, _ domain.Agent, _ []domain.Message, _ string) (string, json.RawMessage, error) {
	return "", nil, errors.New("not running")
}
func (m *notRunningManager) ShutdownAll() {}
