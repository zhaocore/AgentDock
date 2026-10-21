package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestRouterReturnsRequestIDOnMissingRoute(t *testing.T) {
	w := httptest.NewRecorder()
	NewRouter(setupHandler()).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/missing", nil))
	if _, err := uuid.Parse(w.Header().Get("X-Request-ID")); err != nil {
		t.Fatalf("missing service-generated request ID on error response: %v", err)
	}
	if w.Header().Get("Access-Control-Expose-Headers") != "X-Request-ID" {
		t.Fatal("browser clients cannot read request ID")
	}
}

func TestPublicLoginIsRateLimited(t *testing.T) {
	h := setupHandler()
	h.Config.Auth.RateLimit = 1
	router := NewRouter(h)
	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
		req.RemoteAddr = "192.0.2.12:1234"
		if i == 1 {
			req.Header.Set("X-Forwarded-For", "203.0.113.42")
		}
		router.ServeHTTP(w, req)
		if i == 0 && w.Code != http.StatusBadRequest {
			t.Fatalf("first login status %d, want 400", w.Code)
		}
		if i == 1 && w.Code != http.StatusTooManyRequests {
			t.Fatalf("second login status %d, want 429", w.Code)
		}
	}
}
