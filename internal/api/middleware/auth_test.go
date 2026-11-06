// Package middleware JWT 与 CORS 中间件单元测试。
package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// --- JWT 鉴权中间件测试 ---

func TestJWTAuth_ValidToken(t *testing.T) {
	secret := "test-secret-key"
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"ver": 0,
		"sub": uuid.New().String(),
		"exp": time.Now().Add(1 * time.Hour).Unix(),
		"iss": "graphgo",
	})
	tokenStr, _ := token.SignedString([]byte(secret))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Request.Header.Set("Authorization", "Bearer "+tokenStr)

	handler := JWTAuth(secret)
	handler(c)

	if c.IsAborted() {
		t.Errorf("valid token should not be aborted: %v", getError(w.Body.Bytes()))
	}
}

func TestJWTAuth_MissingAuthorizationHeader(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	handler := JWTAuth("secret")
	handler(c)

	if !c.IsAborted() {
		t.Error("missing Authorization header should be aborted")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestJWTAuth_InvalidAuthorizationPrefix(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Request.Header.Set("Authorization", "Basic abc123")

	handler := JWTAuth("secret")
	handler(c)

	if !c.IsAborted() {
		t.Error("invalid Authorization prefix should be aborted")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestJWTAuth_InvalidToken(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Request.Header.Set("Authorization", "Bearer invalid-token-string")

	handler := JWTAuth("secret")
	handler(c)

	if !c.IsAborted() {
		t.Error("invalid token should be aborted")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestJWTAuth_WrongSigningKey(t *testing.T) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"ver": 0,
		"sub": uuid.New().String(),
		"exp": time.Now().Add(1 * time.Hour).Unix(),
		"iss": "graphgo",
	})
	tokenStr, _ := token.SignedString([]byte("correct-secret"))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Request.Header.Set("Authorization", "Bearer "+tokenStr)

	// 使用不同的 secret 验证
	handler := JWTAuth("wrong-secret")
	handler(c)

	if !c.IsAborted() {
		t.Error("wrong signing key should be rejected")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestJWTAuth_WrongSigningMethod(t *testing.T) {
	// 使用 RSA 签名方法
	rsaToken := jwt.New(jwt.SigningMethodRS256)
	rsaToken.Claims = jwt.MapClaims{
		"ver": 0,
		"sub": uuid.New().String(),
		"exp": time.Now().Add(1 * time.Hour).Unix(),
		"iss": "graphgo",
	}
	// 这无法用 HMAC secret 签名，但我们可以手动构造
	tokenStr := "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJ0ZXN0IiwiZXhwIjoxOTk5OTk5OTk5fQ.invalid"

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Request.Header.Set("Authorization", "Bearer "+tokenStr)

	handler := JWTAuth("secret")
	handler(c)

	if !c.IsAborted() {
		t.Error("wrong signing method should be rejected")
	}
}

func TestJWTAuth_ExpiredToken(t *testing.T) {
	secret := "test-secret"
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"ver": 0,
		"sub": uuid.New().String(),
		"exp": time.Now().Add(-1 * time.Hour).Unix(), // 已过期
		"iss": "graphgo",
	})
	tokenStr, _ := token.SignedString([]byte(secret))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Request.Header.Set("Authorization", "Bearer "+tokenStr)

	handler := JWTAuth(secret)
	handler(c)

	if !c.IsAborted() {
		t.Error("expired token should be rejected")
	}
}

func TestJWTAuth_MissingSubClaim(t *testing.T) {
	secret := "test-secret"
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"ver": 0,
		"exp": time.Now().Add(1 * time.Hour).Unix(),
		"iss": "graphgo",
	})
	tokenStr, _ := token.SignedString([]byte(secret))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Request.Header.Set("Authorization", "Bearer "+tokenStr)

	handler := JWTAuth(secret)
	handler(c)

	if !c.IsAborted() {
		t.Error("missing sub claim should be rejected")
	}
}

func TestJWTAuth_EmptySubClaim(t *testing.T) {
	secret := "test-secret"
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"ver": 0,
		"sub": "",
		"exp": time.Now().Add(1 * time.Hour).Unix(),
		"iss": "graphgo",
	})
	tokenStr, _ := token.SignedString([]byte(secret))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Request.Header.Set("Authorization", "Bearer "+tokenStr)

	handler := JWTAuth(secret)
	handler(c)

	if !c.IsAborted() {
		t.Error("empty sub claim should be rejected")
	}
}

func TestJWTAuth_InvalidUUIDSub(t *testing.T) {
	secret := "test-secret"
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"ver": 0,
		"sub": "not-a-uuid",
		"exp": time.Now().Add(1 * time.Hour).Unix(),
		"iss": "graphgo",
	})
	tokenStr, _ := token.SignedString([]byte(secret))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Request.Header.Set("Authorization", "Bearer "+tokenStr)

	handler := JWTAuth(secret)
	handler(c)

	if !c.IsAborted() {
		t.Error("invalid UUID sub should be rejected")
	}
}

func TestJWTAuth_MissingExpClaim(t *testing.T) {
	secret := "test-secret"
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"ver": 0,
		"sub": uuid.New().String(),
		"iss": "graphgo",
	})
	tokenStr, _ := token.SignedString([]byte(secret))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Request.Header.Set("Authorization", "Bearer "+tokenStr)

	handler := JWTAuth(secret)
	handler(c)

	if !c.IsAborted() {
		t.Error("missing exp claim should be rejected")
	}
}

func TestJWTAuth_UserIDInjected(t *testing.T) {
	secret := "test-secret"
	userID := uuid.New().String()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"ver": 0,
		"sub": userID,
		"exp": time.Now().Add(1 * time.Hour).Unix(),
		"iss": "graphgo",
	})
	tokenStr, _ := token.SignedString([]byte(secret))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Request.Header.Set("Authorization", "Bearer "+tokenStr)

	handler := JWTAuth(secret)
	handler(c)

	if c.IsAborted() {
		t.Errorf("valid token should not be aborted: %v", getError(w.Body.Bytes()))
	}
	uid, exists := c.Get("user_id")
	if !exists {
		t.Error("user_id should be injected into context")
	}
	if uid != userID {
		t.Errorf("expected user_id=%s, got %s", userID, uid)
	}
}

// --- CORS 中间件测试 ---

func TestCORSMiddleware_SetsHeaders(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/test", nil)

	handler := CORSMiddleware()
	handler(c)

	if w.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("expected Access-Control-Allow-Origin=*, got %s", w.Header().Get("Access-Control-Allow-Origin"))
	}
	if w.Header().Get("Access-Control-Allow-Headers") != "Authorization, Content-Type" {
		t.Errorf("unexpected Access-Control-Allow-Headers: %s", w.Header().Get("Access-Control-Allow-Headers"))
	}
}

func TestCORSMiddleware_OPTIONSHandling(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodOptions, "/test", nil)

	handler := CORSMiddleware()
	handler(c)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204 for OPTIONS, got %d", w.Code)
	}
	if !c.IsAborted() {
		t.Error("OPTIONS request should be aborted")
	}
}

// --- 辅助函数 ---

func getError(body []byte) string {
	var resp map[string]any
	if err := json.Unmarshal(body, &resp); err != nil {
		return string(body)
	}
	if e, ok := resp["error"]; ok {
		return e.(string)
	}
	return ""
}
