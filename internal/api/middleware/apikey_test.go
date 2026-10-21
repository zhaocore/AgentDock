// Package middleware API 令牌鉴权中间件单元测试。
package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
)

// expectTokenLookup 配置按哈希查令牌及其范围的 mock 期望。
func expectTokenLookup(mock sqlmock.Sqlmock, rows *sqlmock.Rows) {
	mock.ExpectQuery("SELECT id, tenant_id, label, token_prefix").
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(rows)
	mock.ExpectQuery("SELECT kb_id, can_write FROM api_token_scopes").
		WillReturnRows(sqlmock.NewRows([]string{"kb_id", "can_write"}))
}

func newAuthContext(t *testing.T, bearer string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	if bearer != "" {
		c.Request.Header.Set("Authorization", bearer)
	}
	return c, w
}

func TestAPIKeyAuth_ValidToken(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	expectTokenLookup(mock, sqlmock.NewRows([]string{"id", "tenant_id", "label", "token_prefix", "expires_at", "created_at"}).
		AddRow("tok-1", "tenant-1", "prod", "gk_abcdef12", nil, time.Now()))
	mock.ExpectExec("UPDATE api_tokens").WillReturnResult(sqlmock.NewResult(0, 1))

	c, w := newAuthContext(t, "Bearer gk_validsecret")
	apiKeyAuth(c, db)
	if c.IsAborted() {
		t.Fatalf("valid token aborted: code=%d body=%s", w.Code, w.Body.String())
	}
	if c.GetString("user_id") != "tenant-1" {
		t.Errorf("user_id=%q, want tenant-1", c.GetString("user_id"))
	}
	if c.GetString(APITokenIDKey) != "tok-1" {
		t.Errorf("api_token_id=%q, want tok-1", c.GetString(APITokenIDKey))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAPIKeyAuth_UnknownToken(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	expectTokenLookup(mock, sqlmock.NewRows([]string{"id", "tenant_id", "label", "token_prefix", "expires_at", "created_at"}))

	c, w := newAuthContext(t, "Bearer gk_unknown")
	apiKeyAuth(c, db)
	if !c.IsAborted() || w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 abort, got code=%d aborted=%v", w.Code, c.IsAborted())
	}
}

func TestAPIKeyAuth_ExpiredToken(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	expectTokenLookup(mock, sqlmock.NewRows([]string{"id", "tenant_id", "label", "token_prefix", "expires_at", "created_at"}).
		AddRow("tok-1", "tenant-1", "prod", "gk_abcdef12", time.Now().Add(-time.Hour), time.Now()))

	c, w := newAuthContext(t, "Bearer gk_expired")
	apiKeyAuth(c, db)
	if !c.IsAborted() || w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 abort for expired token, got code=%d", w.Code)
	}
}

func TestCombinedAuth_DispatchesOnPrefix(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	// gk_ 前缀走令牌路径
	expectTokenLookup(mock, sqlmock.NewRows([]string{"id", "tenant_id", "label", "token_prefix", "expires_at", "created_at"}).
		AddRow("tok-1", "tenant-1", "prod", "gk_abcdef12", nil, time.Now()))
	mock.ExpectExec("UPDATE api_tokens").WillReturnResult(sqlmock.NewResult(0, 1))

	c, w := newAuthContext(t, "Bearer gk_prefixmatch")
	CombinedAuth("secret", db)(c)
	if c.IsAborted() || c.GetString(APITokenIDKey) != "tok-1" {
		t.Fatalf("gk_ prefix should route to api token auth: code=%d body=%s", w.Code, w.Body.String())
	}

	// 非 gk_ 前缀走 JWT 路径：畸形 JWT 应得到 401（JWTAuth 的行为）
	c2, w2 := newAuthContext(t, "Bearer not-a-jwt")
	CombinedAuth("secret", db)(c2)
	if !c2.IsAborted() || w2.Code != http.StatusUnauthorized {
		t.Errorf("non-gk_ prefix should route to JWT auth, expected 401, got %d", w2.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
