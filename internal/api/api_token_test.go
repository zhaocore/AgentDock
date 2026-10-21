package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"

	"github.com/zhaocore/dutybotgo/internal/domain"
)

// 测试常量：与 mysql 包测试无关，本地定义避免跨包耦合。
const testTokenID = "33333333-3333-3333-3333-333333333333"

// tokenRequestContext 构造带 API 令牌身份的请求上下文。
func tokenRequestContext(t *testing.T, scopes []domain.APITokenScope) *gin.Context {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Set("user_id", "tenant")
	c.Set("api_token_id", "tok-1")
	c.Set("api_token_scopes", scopes)
	return c
}

// --- 范围判定 ---

func TestScopeAllowsWrite(t *testing.T) {
	allRead := []domain.APITokenScope{{KBID: domain.APITokenAllKBs, CanWrite: false}}
	allWrite := []domain.APITokenScope{{KBID: domain.APITokenAllKBs, CanWrite: true}}
	normAll := []domain.APITokenScope{{KBID: "all", CanWrite: false}}

	if scopeAllowsWrite(allRead) {
		t.Error("read-only all scope must not allow write")
	}
	if scopeAllowsWrite(normAll) {
		t.Error("normalized 'all' read scope must not allow write")
	}
	if !scopeAllowsWrite(allWrite) {
		t.Error("can_write all scope should allow write")
	}
}

func TestAuthorizeAPITokenWrite_JWTPassThrough(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	// JWT 请求：上下文无令牌范围，直接放行
	if !authorizeAPITokenWrite(c) || w.Code != http.StatusOK {
		t.Error("JWT request should pass write check")
	}
}

func TestAuthorizeAPITokenWrite_TokenReadOnly(t *testing.T) {
	c := tokenRequestContext(t, []domain.APITokenScope{{KBID: "all", CanWrite: false}})
	if authorizeAPITokenWrite(c) {
		t.Error("read-only token should be denied for write")
	}
	if c.Writer.Status() != http.StatusForbidden {
		t.Errorf("expected 403, got %d", c.Writer.Status())
	}
}

func TestDenyAPIToken(t *testing.T) {
	c := tokenRequestContext(t, nil)
	if !denyAPIToken(c) {
		t.Error("token request should be denied on management endpoints")
	}
	w := httptest.NewRecorder()
	jwtC, _ := gin.CreateTestContext(w)
	jwtC.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	if denyAPIToken(jwtC) {
		t.Error("JWT request should pass deny check")
	}
}

// --- Handler：创建令牌 ---

func TestCreateAPITokenHandler(t *testing.T) {
	h := setupHandler()
	db, mock, _ := sqlmock.New()
	h.DB = db
	defer db.Close()

	// kb_scope=all 是唯一合法范围
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO api_tokens").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO api_token_scopes").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("user_id", "tenant")
	c.Request = httptest.NewRequest(http.MethodPost, "/api-tokens",
		strings.NewReader(`{"label":"prod","kb_scope":"all","can_write":false}`))
	c.Request.Header.Set("Content-Type", "application/json")
	h.CreateAPIToken(c)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"token":"gk_`) {
		t.Error("create response should include one-time token secret")
	}
	if !strings.Contains(w.Body.String(), `"kb_id":"all"`) {
		t.Errorf("scope should echo all, body=%s", w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCreateAPITokenHandler_RejectsNonAllScope(t *testing.T) {
	h := setupHandler()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api-tokens",
		strings.NewReader(`{"label":"prod","kb_scope":"11111111-1111-1111-1111-111111111111"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	h.CreateAPIToken(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("non-all kb_scope should be 400, got %d", w.Code)
	}
}

func TestCreateAPITokenHandler_RejectsExpiredAt(t *testing.T) {
	h := setupHandler()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api-tokens",
		strings.NewReader(`{"label":"prod","kb_scope":"all","expires_at":"2000-01-01T00:00:00Z"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	h.CreateAPIToken(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("past expires_at should be 400, got %d", w.Code)
	}
}

func TestCreateAPITokenHandler_RejectsTokenAuth(t *testing.T) {
	h := setupHandler()
	c := tokenRequestContext(t, nil)
	c.Request = httptest.NewRequest(http.MethodPost, "/api-tokens",
		strings.NewReader(`{"label":"prod","kb_scope":"all"}`))
	h.CreateAPIToken(c)
	if c.Writer.Status() != http.StatusForbidden {
		t.Errorf("token-authed create should be 403, got %d", c.Writer.Status())
	}
}

// --- Handler：撤销令牌 ---

func TestRevokeAPITokenHandler_NotFound(t *testing.T) {
	h := setupHandler()
	db, mock, _ := sqlmock.New()
	h.DB = db
	defer db.Close()
	mock.ExpectExec("UPDATE api_tokens SET revoked_at").
		WithArgs(testTokenID, "tenant").
		WillReturnResult(sqlmock.NewResult(0, 0))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("user_id", "tenant")
	c.Params = gin.Params{{Key: "token_id", Value: testTokenID}}
	c.Request = httptest.NewRequest(http.MethodDelete, "/api-tokens/"+testTokenID, nil)
	h.RevokeAPIToken(c)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

// --- 令牌生成 ---

func TestGenerateAPIToken(t *testing.T) {
	full, prefix, hash, err := generateAPIToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(full) != 3+tokenSecretLength || !strings.HasPrefix(full, "gk_") {
		t.Errorf("full=%q", full)
	}
	if prefix != full[:12] {
		t.Errorf("prefix=%q", prefix)
	}
	if len(hash) != 64 {
		t.Errorf("hash length=%d, want sha256 hex 64", len(hash))
	}
	// 两次生成不重复
	second, _, _, _ := generateAPIToken()
	if second == full {
		t.Error("tokens should be unique")
	}
}

// 写中间件：只读令牌被 403，可写令牌与 JWT 放行。
func TestRequireTokenWriteMiddleware(t *testing.T) {
	router := gin.New()
	router.POST("/x", RequireTokenWrite(), func(c *gin.Context) { c.Status(200) })

	// JWT 请求放行
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("JWT write should pass, got %d", w.Code)
	}

	// 只读令牌拒绝
	readonly := tokenRequestContext(t, []domain.APITokenScope{{KBID: "all", CanWrite: false}})
	_ = readonly
	w2 := httptest.NewRecorder()
	router2 := gin.New()
	router2.POST("/x", func(c *gin.Context) {
		c.Set("api_token_scopes", []domain.APITokenScope{{KBID: "all", CanWrite: false}})
		RequireTokenWrite()(c)
		if c.IsAborted() {
			return
		}
		c.Status(200)
	})
	router2.ServeHTTP(w2, httptest.NewRequest(http.MethodPost, "/x", nil))
	if w2.Code != http.StatusForbidden {
		t.Fatalf("read-only token write should be 403, got %d", w2.Code)
	}
}
