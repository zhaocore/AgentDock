// Package middleware 限流中间件单元测试。
package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// newRateLimitRouter 构造带 user_id 注入的测试路由。
func newRateLimitRouter(limit int, userID string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if userID != "" {
			c.Set("user_id", userID)
		}
		c.Next()
	})
	r.Use(RateLimit(limit))
	r.GET("/ping", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	return r
}

func TestRateLimit_BlocksAfterLimit(t *testing.T) {
	r := newRateLimitRouter(2, "user-a")

	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ping", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("request %d should pass, got %d", i+1, w.Code)
		}
	}

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ping", nil))
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("request beyond limit should be 429, got %d", w.Code)
	}
}

func TestRateLimit_DisabledWhenZero(t *testing.T) {
	r := newRateLimitRouter(0, "user-a")

	for i := 0; i < 20; i++ {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ping", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("limit=0 disables limiting, request %d got %d", i+1, w.Code)
		}
	}
}

func TestRateLimit_PerUserIsolation(t *testing.T) {
	r := newRateLimitRouter(1, "user-a")

	// user-a 用尽配额
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ping", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("user-a first request should pass, got %d", w.Code)
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ping", nil))
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("user-a second request should be 429, got %d", w.Code)
	}

	// user-b 独立配额，不受 user-a 影响
	r2 := newRateLimitRouter(1, "user-b")
	w = httptest.NewRecorder()
	r2.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ping", nil))
	if w.Code != http.StatusOK {
		t.Errorf("user-b should have independent quota, got %d", w.Code)
	}
}