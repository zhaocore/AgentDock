// Package api HTTP处理器单元测试（使用 sqlmock + gin.CreateTestContext）。
package api

import (
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

	"github.com/zhaocore/agentdock/internal/config"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// setupHandler 创建带 mock DB 的 Handler。
// 用于不需要特定 DB 期望的测试（如参数校验失败场景）。
func setupHandler() *Handler {
	db, _, _ := sqlmock.New()
	cfg := &config.Config{
		Auth: config.AuthConfig{
			JWTSecret:   "test-jwt-secret",
			TokenExpiry: 24 * time.Hour,
		},
		Server: config.ServerConfig{Mode: "debug"},
	}
	return &Handler{
		Config: cfg,
		DB:     db,
	}
}

// --- generateJWT ---

func TestGenerateJWT_Success(t *testing.T) {
	h := setupHandler()
	token, err := h.generateJWT("user-123", "test@example.com")
	if err != nil {
		t.Fatalf("generateJWT: %v", err)
	}
	if token == "" {
		t.Error("token should not be empty")
	}
}

// --- Register ---

func TestRegister_BadRequest(t *testing.T) {
	h := setupHandler()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/auth/register",
		strings.NewReader(`{"email": "not-an-email"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	h.Register(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRegister_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	h := &Handler{
		Config: &config.Config{
			Auth: config.AuthConfig{
				JWTSecret:   "test-jwt-secret",
				TokenExpiry: 24 * time.Hour,
			},
		},
		DB: db,
	}

	mock.ExpectExec("INSERT INTO users").WillReturnResult(sqlmock.NewResult(1, 1))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/auth/register",
		strings.NewReader(`{"email":"test@example.com","password":"secret123"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	h.Register(c)

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp AuthResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Token == "" {
		t.Error("response should contain token")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRegister_EmailConflict(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	h := &Handler{
		Config: &config.Config{
			Auth: config.AuthConfig{JWTSecret: "test", TokenExpiry: 24 * time.Hour},
		},
		DB: db,
	}

	mock.ExpectExec("INSERT INTO users").WillReturnError(errors.New("duplicate key value violates unique constraint"))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/auth/register",
		strings.NewReader(`{"email":"existing@example.com","password":"secret123"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	h.Register(c)

	if w.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

// --- Login ---

func TestLogin_BadRequest(t *testing.T) {
	h := setupHandler()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/auth/login",
		strings.NewReader(`{}`))
	c.Request.Header.Set("Content-Type", "application/json")

	h.Login(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestLogin_UserNotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	h := &Handler{
		Config: &config.Config{
			Auth: config.AuthConfig{JWTSecret: "test", TokenExpiry: 24 * time.Hour},
		},
		DB: db,
	}

	mock.ExpectQuery("SELECT id, email, password_hash FROM users").
		WithArgs("nouser@example.com").
		WillReturnError(sql.ErrNoRows)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/auth/login",
		strings.NewReader(`{"email":"nouser@example.com","password":"wrong"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	h.Login(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

// --- CurrentUser ---

func TestCurrentUser_NotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	h := &Handler{
		Config: &config.Config{
			Auth: config.AuthConfig{JWTSecret: "test", TokenExpiry: 24 * time.Hour},
		},
		DB: db,
	}

	mock.ExpectQuery("SELECT id, email, created_at FROM users").
		WithArgs("user-123").
		WillReturnError(sql.ErrNoRows)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("user_id", "user-123")
	c.Request = httptest.NewRequest(http.MethodGet, "/auth/me", nil)

	h.CurrentUser(c)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

// Health 测试需要同时 mock PostgreSQL 和 Neo4j，集成测试更适合此场景。

// --- GetDocumentGraph ---
