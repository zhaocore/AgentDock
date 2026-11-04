package mysql

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/zhaocore/agentdock/internal/domain"
)

var testTime = time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC)

// --- Agent ---

func TestCreateAgent(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	mock.ExpectExec("INSERT INTO agents").
		WithArgs("agent-1", "u1", "bot", "sp", "m", sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	agent := domain.Agent{ID: "agent-1", OwnerUserID: "u1", Name: "bot", SystemPrompt: "sp", Model: "m"}
	if err := CreateAgent(context.Background(), db, agent); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateAgent_NotFound(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	mock.ExpectExec("UPDATE agents").WillReturnResult(sqlmock.NewResult(0, 0))

	agent := domain.Agent{ID: "missing", OwnerUserID: "u1"}
	if err := UpdateAgent(context.Background(), db, agent); err != sql.ErrNoRows {
		t.Fatalf("expected ErrNoRows, got %v", err)
	}
}

func TestDeleteAgent_NotFound(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	mock.ExpectExec("DELETE FROM agents").WillReturnResult(sqlmock.NewResult(0, 0))
	if err := DeleteAgent(context.Background(), db, "missing", "u1"); err != sql.ErrNoRows {
		t.Fatalf("expected ErrNoRows, got %v", err)
	}
}

func TestListAgents(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	mock.ExpectQuery("SELECT").
		WillReturnRows(sqlmock.NewRows([]string{"id", "owner_user_id", "name", "system_prompt", "model", "skills", "mcp_servers", "subagents", "created_at", "updated_at"}).
			AddRow("agent-1", "u1", "bot", "sp", "m", `["search"]`, `[]`, `[]`, testTime, testTime))
	mock.ExpectQuery("SELECT COUNT").WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(1))

	list, total, err := ListAgents(context.Background(), db, "u1", 20, 0)
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if total != 1 || len(list) != 1 || list[0].Skills[0] != "search" {
		t.Fatalf("unexpected result: %+v total=%d", list, total)
	}
}

// --- Instance ---

func TestCreateInstance(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	mock.ExpectExec("INSERT INTO agent_instances").
		WithArgs("inst-1", "agent-1", "u1", domain.AgentStatusCreated, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	inst := domain.AgentInstance{ID: "inst-1", AgentID: "agent-1", OwnerUserID: "u1",
		Status: domain.AgentStatusCreated, RepoIDs: []string{}}
	if err := CreateInstance(context.Background(), db, inst); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
}

func TestMarkRunningInstancesStopped(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	mock.ExpectExec("UPDATE agent_instances SET status").
		WithArgs(domain.AgentStatusStopped, domain.AgentStatusRunning).
		WillReturnResult(sqlmock.NewResult(0, 2))
	if err := MarkRunningInstancesStopped(context.Background(), db); err != nil {
		t.Fatalf("MarkRunningInstancesStopped: %v", err)
	}
}

// --- Session ---

func TestSessionLifecycle(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	sessionRows := func() *sqlmock.Rows {
		return sqlmock.NewRows([]string{"id", "user_id", "instance_id", "agent_id", "title", "created_at", "updated_at"}).
			AddRow("s-1", "u1", "inst-1", "agent-1", "t", testTime, testTime)
	}

	// 创建
	mock.ExpectExec("INSERT INTO sessions").WithArgs("s-1", "u1", "inst-1", "agent-1", "t").
		WillReturnResult(sqlmock.NewResult(1, 1))
	// 读取
	mock.ExpectQuery("SELECT").WillReturnRows(sessionRows())
	// 列表
	mock.ExpectQuery("SELECT").WillReturnRows(sessionRows())
	mock.ExpectQuery("SELECT COUNT").WithArgs("u1").WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(1))
	// 软删除
	mock.ExpectExec("UPDATE sessions SET deleted_at").WillReturnResult(sqlmock.NewResult(0, 1))
	// 已删除再删 → ErrNoRows
	mock.ExpectExec("UPDATE sessions SET deleted_at").WillReturnResult(sqlmock.NewResult(0, 0))

	ctx := context.Background()
	if err := CreateSession(ctx, db, domain.Session{ID: "s-1", UserID: "u1", InstanceID: "inst-1", AgentID: "agent-1", Title: "t"}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := GetSession(ctx, db, "s-1", "u1"); err != nil {
		t.Fatalf("GetSession: %v", err)
	}

	list, total, err := ListSessions(ctx, db, "u1", 20, 0)
	if err != nil || total != 1 || len(list) != 1 {
		t.Fatalf("ListSessions: %v total=%d", err, total)
	}
	if err := DeleteSession(ctx, db, "s-1", "u1"); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if err := DeleteSession(ctx, db, "s-1", "u1"); err != sql.ErrNoRows {
		t.Fatalf("double delete should be ErrNoRows, got %v", err)
	}
}

// --- Message ---

func TestInsertMessageAndList(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	// 事务写消息 + 触碰会话
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO messages").WithArgs("m-1", "s-1", "user", "hi", nil).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("UPDATE sessions SET updated_at").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	// 历史读取
	mock.ExpectQuery("SELECT").
		WillReturnRows(sqlmock.NewRows([]string{"id", "session_id", "role", "content", "agent_meta", "created_at"}).
			AddRow("m-1", "s-1", "user", "hi", nil, testTime))

	ctx := context.Background()
	msg := domain.Message{ID: "m-1", SessionID: "s-1", Role: "user", Content: "hi"}
	if err := InsertMessage(ctx, db, msg); err != nil {
		t.Fatalf("InsertMessage: %v", err)
	}
	got, err := ListMessages(ctx, db, "s-1")
	if err != nil || len(got) != 1 || got[0].Content != "hi" {
		t.Fatalf("ListMessages: %v len=%d", err, len(got))
	}
}

// --- Repository ---

func TestRepositoryStatusUpdate(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	mock.ExpectExec("UPDATE repositories SET status").
		WithArgs(domain.RepoStatusReady, "/repo/web", nil, "repo-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := UpdateRepositoryStatus(context.Background(), db, "repo-1", domain.RepoStatusReady, "/repo/web", ""); err != nil {
		t.Fatalf("UpdateRepositoryStatus: %v", err)
	}
}

func TestListRepositories(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	mock.ExpectQuery("SELECT").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "git_url", "repo_type", "branch", "local_path", "status", "last_error", "created_at", "updated_at"}).
			AddRow("repo-1", "web", "https://x.git", "frontend", "main", "/repo/web", "ready", nil, testTime, testTime))
	mock.ExpectQuery("SELECT COUNT").WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(1))

	list, total, err := ListRepositories(context.Background(), db, 20, 0)
	if err != nil || total != 1 || list[0].Status != domain.RepoStatusReady {
		t.Fatalf("ListRepositories: %v total=%d", err, total)
	}
}

// --- API Token ---

func TestAPITokenRoundTrip(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	// 创建：事务写 token + scope
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO api_tokens").
		WithArgs("tok-1", "u1", "label", "gk_prefix", "hash", nil).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO api_token_scopes").WithArgs("tok-1", "all", true).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	// 按哈希取
	mock.ExpectQuery("SELECT id, tenant_id, label").WithArgs("hash").
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "label", "token_prefix", "expires_at", "created_at"}).
			AddRow("tok-1", "u1", "label", "gk_prefix", nil, testTime))
	mock.ExpectQuery("SELECT kb_id, can_write").WithArgs("tok-1").
		WillReturnRows(sqlmock.NewRows([]string{"kb_id", "can_write"}).AddRow(domain.APITokenAllKBs, true))

	ctx := context.Background()
	token := domain.APIToken{ID: "tok-1", TenantID: "u1", Label: "label", TokenPrefix: "gk_prefix",
		Scopes: []domain.APITokenScope{{KBID: "all", CanWrite: true}}}
	if err := CreateAPIToken(ctx, db, token, "hash"); err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}
	got, err := GetAPITokenByHash(ctx, db, "hash")
	if err != nil {
		t.Fatalf("GetAPITokenByHash: %v", err)
	}
	// 全零哨兵归一化为 "all"
	if len(got.Scopes) != 1 || got.Scopes[0].KBID != "all" || !got.Scopes[0].CanWrite {
		t.Fatalf("unexpected scopes: %+v", got.Scopes)
	}
}
