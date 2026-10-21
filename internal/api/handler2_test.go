package api

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
)

func agentCtx(method, path, body string, params ...gin.Param) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("user_id", "u1")
	for _, p := range params {
		c.Params = append(c.Params, p)
	}
	if body != "" {
		c.Request = httptest.NewRequest(method, path, strings.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
	} else {
		c.Request = httptest.NewRequest(method, path, nil)
	}
	return c, w
}

func TestUpdateAgentHandler_NotFound(t *testing.T) {
	h, mock := setupAgentHandler()
	defer h.DB.Close()
	mock.ExpectQuery("SELECT").WillReturnError(sql.ErrNoRows)

	c, w := agentCtx(http.MethodPut, "/agents/x", `{"name":"b","system_prompt":"s","model":"m"}`,
		gin.Param{Key: "agent_id", Value: "22222222-2222-2222-2222-222222222222"})
	h.UpdateAgent(c)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestDeleteAgentHandler_NotFound(t *testing.T) {
	h, mock := setupAgentHandler()
	defer h.DB.Close()
	mock.ExpectQuery("SELECT").WillReturnError(sql.ErrNoRows)

	c, w := agentCtx(http.MethodDelete, "/agents/x", "",
		gin.Param{Key: "agent_id", Value: "22222222-2222-2222-2222-222222222222"})
	h.DeleteAgent(c)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestCreateInstanceHandler(t *testing.T) {
	h, mock := setupAgentHandler()
	defer h.DB.Close()
	mock.ExpectQuery("SELECT").WillReturnRows(agentRow())
	mock.ExpectExec("INSERT INTO agent_instances").WillReturnResult(sqlmock.NewResult(1, 1))

	c, w := agentCtx(http.MethodPost, "/agents/x/instances", `{}`,
		gin.Param{Key: "agent_id", Value: "22222222-2222-2222-2222-222222222222"})
	h.CreateInstance(c)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"repo_ids":[]`) {
		t.Errorf("nil repo_ids normalized, body=%s", w.Body.String())
	}
}

func agentRow() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "owner_user_id", "name", "system_prompt", "model", "skills", "mcp_servers", "subagents", "created_at", "updated_at"}).
		AddRow("22222222-2222-2222-2222-222222222222", "u1", "bot", "sp", "m", "[]", "[]", "[]", nowStr(), nowStr())
}

func instanceRow(id string) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "agent_id", "owner_user_id", "status", "repo_ids", "created_at", "updated_at"}).
		AddRow(id, "22222222-2222-2222-2222-222222222222", "u1", "created", "[]", nowStr(), nowStr())
}

func TestListInstancesHandler(t *testing.T) {
	h, mock := setupAgentHandler()
	defer h.DB.Close()
	mock.ExpectQuery("SELECT").WillReturnRows(agentRow())
	mock.ExpectQuery("SELECT").WillReturnRows(instanceRow("inst-1"))

	c, w := agentCtx(http.MethodGet, "/agents/x/instances", "",
		gin.Param{Key: "agent_id", Value: "22222222-2222-2222-2222-222222222222"})
	h.ListInstances(c)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"data"`) {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestStartStopInstanceHandler(t *testing.T) {
	h, mock := setupAgentHandler()
	defer h.DB.Close()
	h.Instances = &notRunningManager{}

	// start：loadInstance 两跳查询
	mock.ExpectQuery("SELECT").WillReturnRows(instanceRow("inst-1"))
	mock.ExpectQuery("SELECT").WillReturnRows(agentRow())
	c, w := agentCtx(http.MethodPost, "/instances/x/start", "",
		gin.Param{Key: "instance_id", Value: "33333333-3333-3333-3333-333333333333"})
	h.StartInstance(c)
	if w.Code != http.StatusConflict {
		t.Fatalf("fake manager start failure should be 409, got %d", w.Code)
	}

	// stop
	mock.ExpectQuery("SELECT").WillReturnRows(instanceRow("inst-1"))
	mock.ExpectQuery("SELECT").WillReturnRows(agentRow())
	c, w = agentCtx(http.MethodPost, "/instances/x/stop", "",
		gin.Param{Key: "instance_id", Value: "33333333-3333-3333-3333-333333333333"})
	h.StopInstance(c)
	// c.Status 只写 header，测试上下文需主动 flush 才可见
	c.Writer.WriteHeaderNow()
	if w.Code != http.StatusAccepted {
		t.Fatalf("stop should be 202, got %d", w.Code)
	}
}

func TestCreateSessionHandler(t *testing.T) {
	h, mock := setupAgentHandler()
	defer h.DB.Close()
	mock.ExpectQuery("SELECT").WillReturnRows(instanceRow("inst-1"))
	mock.ExpectQuery("SELECT").WillReturnRows(agentRow())
	mock.ExpectExec("INSERT INTO sessions").WillReturnResult(sqlmock.NewResult(1, 1))

	c, w := agentCtx(http.MethodPost, "/instances/x/sessions", "",
		gin.Param{Key: "instance_id", Value: "33333333-3333-3333-3333-333333333333"})
	h.CreateSession(c)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestSessionListAndDeleteHandlers(t *testing.T) {
	h, mock := setupAgentHandler()
	defer h.DB.Close()
	sessionRows := sqlmock.NewRows([]string{"id", "user_id", "instance_id", "agent_id", "title", "created_at", "updated_at"}).
		AddRow("s-1", "u1", "inst-1", "agent-1", "t", nowStr(), nowStr())

	mock.ExpectQuery("SELECT").WillReturnRows(sessionRows)
	mock.ExpectQuery("SELECT COUNT").WithArgs("u1").WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(1))
	c, w := agentCtx(http.MethodGet, "/sessions", "")
	h.ListSessions(c)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"total":1`) {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}

	// 消息历史
	mock.ExpectQuery("SELECT").WillReturnRows(
		sqlmock.NewRows([]string{"id", "user_id", "instance_id", "agent_id", "title", "created_at", "updated_at"}).
			AddRow("s-1", "u1", "inst-1", "agent-1", "t", nowStr(), nowStr()))
	mock.ExpectQuery("SELECT").WillReturnRows(
		sqlmock.NewRows([]string{"id", "session_id", "role", "content", "agent_meta", "created_at"}).
			AddRow("m-1", "s-1", "user", "hi", nil, nowStr()))
	c, w = agentCtx(http.MethodGet, "/sessions/s/messages", "",
		gin.Param{Key: "session_id", Value: "44444444-4444-4444-4444-444444444444"})
	h.ListSessionMessages(c)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "hi") {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}

	// 删除
	mock.ExpectExec("UPDATE sessions SET deleted_at").WillReturnResult(sqlmock.NewResult(0, 1))
	c, w = agentCtx(http.MethodDelete, "/sessions/s", "",
		gin.Param{Key: "session_id", Value: "44444444-4444-4444-4444-444444444444"})
	h.DeleteSession(c)
	c.Writer.WriteHeaderNow()
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
}

func TestListRepositoriesHandler(t *testing.T) {
	h, mock := setupAgentHandler()
	defer h.DB.Close()
	mock.ExpectQuery("SELECT").WillReturnRows(
		sqlmock.NewRows([]string{"id", "name", "git_url", "repo_type", "branch", "local_path", "status", "last_error", "created_at", "updated_at"}).
			AddRow("r-1", "web", "https://x.git", "frontend", "main", nil, "pending", nil, nowStr(), nowStr()))
	mock.ExpectQuery("SELECT COUNT").WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(1))

	c, w := agentCtx(http.MethodGet, "/repositories", "")
	h.ListRepositories(c)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"repo_type":"frontend"`) {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestGetRepositoryHandler_NotFound(t *testing.T) {
	h, mock := setupAgentHandler()
	defer h.DB.Close()
	mock.ExpectQuery("SELECT").WillReturnError(sql.ErrNoRows)
	c, w := agentCtx(http.MethodGet, "/repositories/x", "",
		gin.Param{Key: "repo_id", Value: "55555555-5555-5555-5555-555555555555"})
	h.GetRepository(c)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestSyncRepositoryHandler(t *testing.T) {
	h, mock := setupAgentHandler()
	defer h.DB.Close()
	h.AsynqClient = &fakeEnqueuer{}
	mock.ExpectQuery("SELECT").WillReturnRows(
		sqlmock.NewRows([]string{"id", "name", "git_url", "repo_type", "branch", "local_path", "status", "last_error", "created_at", "updated_at"}).
			AddRow("r-1", "web", "https://x.git", "frontend", "main", "/repo/web", "ready", nil, nowStr(), nowStr()))
	c, w := agentCtx(http.MethodPost, "/repositories/x/sync", "",
		gin.Param{Key: "repo_id", Value: "55555555-5555-5555-5555-555555555555"})
	h.SyncRepository(c)
	c.Writer.WriteHeaderNow()
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", w.Code)
	}
}

func TestDeleteRepositoryHandler(t *testing.T) {
	h, mock := setupAgentHandler()
	defer h.DB.Close()
	h.AsynqClient = &fakeEnqueuer{}
	mock.ExpectQuery("SELECT").WillReturnRows(
		sqlmock.NewRows([]string{"id", "name", "git_url", "repo_type", "branch", "local_path", "status", "last_error", "created_at", "updated_at"}).
			AddRow("r-1", "web", "https://x.git", "frontend", "main", "/repo/web", "ready", nil, nowStr(), nowStr()))
	mock.ExpectExec("DELETE FROM repositories").WillReturnResult(sqlmock.NewResult(0, 1))
	c, w := agentCtx(http.MethodDelete, "/repositories/x", "",
		gin.Param{Key: "repo_id", Value: "55555555-5555-5555-5555-555555555555"})
	h.DeleteRepository(c)
	c.Writer.WriteHeaderNow()
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
}
