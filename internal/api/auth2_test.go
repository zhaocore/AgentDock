package api

import (
	"database/sql"
	"net/http"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
)

// ChangePassword：旧密码校验走 DB 查询后 bcrypt 比对。
func TestChangePasswordHandler_WrongOldPassword(t *testing.T) {
	h, mock := setupAgentHandler()
	defer h.DB.Close()
	mock.ExpectQuery("SELECT password_hash").
		WillReturnRows(sqlmock.NewRows([]string{"password_hash"}).AddRow("$2a$10$invalidbcrypthashvalueinvalidbcrypt"))

	c, w := agentCtx(http.MethodPost, "/auth/change-password",
		`{"old_password":"wrong","new_password":"newpass1"}`)
	h.ChangePassword(c)
	if w.Code == http.StatusInternalServerError {
		t.Fatalf("unexpected 500: %s", w.Body.String())
	}
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong old password, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestChangePasswordHandler_UserNotFound(t *testing.T) {
	h, mock := setupAgentHandler()
	defer h.DB.Close()
	mock.ExpectQuery("SELECT password_hash").WillReturnError(sql.ErrNoRows)

	c, w := agentCtx(http.MethodPost, "/auth/change-password",
		`{"old_password":"x","new_password":"newpass1"}`)
	h.ChangePassword(c)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestChangePasswordHandler_BadRequest(t *testing.T) {
	h, _ := setupAgentHandler()
	defer h.DB.Close()
	c, w := agentCtx(http.MethodPost, "/auth/change-password", `{"old_password":"x"}`)
	h.ChangePassword(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// ListAPITokens：返回租户令牌列表。
func TestListAPITokensHandler(t *testing.T) {
	h, mock := setupAgentHandler()
	defer h.DB.Close()
	mock.ExpectQuery("SELECT id, tenant_id, label").
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "label", "token_prefix", "expires_at", "revoked_at", "last_used_at", "created_at"}).
			AddRow("tok-1", "u1", "l", "gk_prefix", nil, nil, nil, nowStr()))
	mock.ExpectQuery("SELECT kb_id, can_write").
		WillReturnRows(sqlmock.NewRows([]string{"kb_id", "can_write"}).AddRow("all", true))

	c, w := agentCtx(http.MethodGet, "/api-tokens", "")
	h.ListAPITokens(c)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"kb_id":"all"`) {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	_ = gin.H{}
}
