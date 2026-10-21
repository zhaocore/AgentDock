package mysql

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/zhaocore/dutybotgo/internal/domain"
)

func TestListAPITokensStore(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	mock.ExpectQuery("SELECT id, tenant_id, label").
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "label", "token_prefix", "expires_at", "revoked_at", "last_used_at", "created_at"}).
			AddRow("tok-1", "u1", "l", "gk_p", nil, nil, nil, testTime))
	mock.ExpectQuery("SELECT kb_id, can_write").
		WillReturnRows(sqlmock.NewRows([]string{"kb_id", "can_write"}).AddRow("all", false))

	tokens, err := ListAPITokens(context.Background(), db, "u1")
	if err != nil || len(tokens) != 1 || tokens[0].Scopes[0].KBID != "all" {
		t.Fatalf("ListAPITokens: %v %+v", err, tokens)
	}
}

func TestRevokeAPITokenStore_NotFound(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	mock.ExpectExec("UPDATE api_tokens SET revoked_at").WillReturnResult(sqlmock.NewResult(0, 0))
	if err := RevokeAPIToken(context.Background(), db, "u1", "missing"); err != sql.ErrNoRows {
		t.Fatalf("expected ErrNoRows, got %v", err)
	}
}

func TestTouchAPITokenStore(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	mock.ExpectExec("UPDATE api_tokens SET last_used_at").WillReturnResult(sqlmock.NewResult(0, 1))
	if err := TouchAPIToken(context.Background(), db, "tok-1"); err != nil {
		t.Fatalf("TouchAPIToken: %v", err)
	}
}

func TestGetAgentStore(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	mock.ExpectQuery("SELECT").
		WillReturnRows(sqlmock.NewRows([]string{"id", "owner_user_id", "name", "system_prompt", "model", "skills", "mcp_servers", "subagents", "created_at", "updated_at"}).
			AddRow("agent-1", "u1", "bot", "sp", "m", `["a"]`, `[{"name":"fs","transport":"stdio","command":"mcp-fs"}]`, `[{"name":"sub","description":"d"}]`, testTime, testTime))

	agent, err := GetAgent(context.Background(), db, "agent-1", "u1")
	if err != nil {
		t.Fatalf("GetAgent: %v", err)
	}
	if len(agent.Skills) != 1 || agent.MCPServers[0].Name != "fs" || agent.Subagents[0].Name != "sub" {
		t.Fatalf("JSON decode wrong: %+v", agent)
	}
	_ = domain.APIToken{}
}
