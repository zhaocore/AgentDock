package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/zhaocore/agentdock/internal/domain"
)

const publicAgentID = "22222222-2222-2222-2222-222222222222"

func publicAgentRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "owner_user_id", "name", "system_prompt", "model", "skills", "mcp_servers", "subagents", "created_at", "updated_at"}).AddRow(
		publicAgentID, nil, "agent1", "new prompt", "shared-model", `["review"]`,
		`[{"name":"tools","transport":"sse","endpoint":"https://private.example/token","env":{"KEY":"secret-value"}}]`, `[]`, nowStr(), nowStr())
}

func publicAgentContext(method, path, body string) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("user_id", "current-user")
	c.Params = gin.Params{{Key: "agent_id", Value: publicAgentID}}
	c.Request = httptest.NewRequest(method, path, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c, w
}

func TestPublicAgentReadAndWriteProtection(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			h, mock := setupAgentHandler()
			defer h.DB.Close()
			mock.ExpectQuery("SELECT").WithArgs(publicAgentID, "current-user").WillReturnRows(publicAgentRows())
			c, w := publicAgentContext(method, "/agents/"+publicAgentID, `{}`)
			switch method {
			case http.MethodGet:
				h.GetAgent(c)
			case http.MethodPut:
				h.UpdateAgent(c)
			case http.MethodDelete:
				h.DeleteAgent(c)
			}
			want := http.StatusForbidden
			if method == http.MethodGet {
				want = http.StatusOK
			}
			if w.Code != want {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			for _, secret := range []string{"private.example", "secret-value", "endpoint", "env"} {
				if strings.Contains(w.Body.String(), secret) {
					t.Fatalf("public response leaked %q: %s", secret, w.Body.String())
				}
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPublicAgentInstanceUsesCurrentUser(t *testing.T) {
	h, mock := setupAgentHandler()
	defer h.DB.Close()
	mock.ExpectQuery("SELECT").WithArgs(publicAgentID, "current-user").WillReturnRows(publicAgentRows())
	mock.ExpectExec("INSERT INTO agent_instances").WithArgs(sqlmock.AnyArg(), publicAgentID, "current-user", domain.AgentStatusCreated, []byte(`[]`)).WillReturnResult(sqlmock.NewResult(1, 1))
	c, w := publicAgentContext(http.MethodPost, "/agents/"+publicAgentID+"/instances", `{}`)
	h.CreateInstance(c)
	if w.Code != http.StatusCreated || !strings.Contains(w.Body.String(), `"owner_user_id":"current-user"`) {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPublicAgentInstancesFilterCurrentUser(t *testing.T) {
	h, mock := setupAgentHandler()
	defer h.DB.Close()
	mock.ExpectQuery("SELECT").WithArgs(publicAgentID, "current-user").WillReturnRows(publicAgentRows())
	mock.ExpectQuery(`FROM agent_instances WHERE agent_id = \? AND owner_user_id = \?`).WithArgs(publicAgentID, "current-user").WillReturnRows(sqlmock.NewRows([]string{"id", "agent_id", "owner_user_id", "status", "repo_ids", "created_at", "updated_at"}))
	c, w := publicAgentContext(http.MethodGet, "/agents/"+publicAgentID+"/instances", "")
	h.ListInstances(c)
	if w.Code != http.StatusOK || w.Body.String() != `{"data":[]}` {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestListAgentsSynchronizesBeforeRead(t *testing.T) {
	h, mock := setupAgentHandler()
	defer h.DB.Close()
	synced := false
	h.SyncAgents = func(context.Context) error { synced = true; return nil }
	mock.ExpectQuery("SELECT").WithArgs("current-user", 20, 0).WillReturnRows(publicAgentRows())
	mock.ExpectQuery("SELECT COUNT").WithArgs("current-user").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	c, w := publicAgentContext(http.MethodGet, "/agents", "")
	h.ListAgents(c)
	if !synced || w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "new prompt") || strings.Contains(w.Body.String(), "secret-value") {
		t.Fatalf("status=%d body=%s synced=%v", w.Code, w.Body.String(), synced)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestListAgentsSyncFailureIsExplicit(t *testing.T) {
	h, mock := setupAgentHandler()
	defer h.DB.Close()
	h.SyncAgents = func(context.Context) error { return errors.New("invalid deployed configuration") }
	c, w := publicAgentContext(http.MethodGet, "/agents", "")
	h.ListAgents(c)
	if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), "deployer must check") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
