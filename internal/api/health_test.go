package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
)

func TestHealthDependencyResults(t *testing.T) {
	for _, failed := range []string{"", "mysql", "redis", "missing"} {
		t.Run(failed, func(t *testing.T) {
			db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			fail := errors.New("dependency unavailable")
			dbPing := mock.ExpectPing()
			if failed == "mysql" {
				dbPing.WillReturnError(fail)
			}
			h := &Handler{DB: db,
				RedisHealth: func(context.Context) error {
					if failed == "redis" {
						return fail
					}
					return nil
				},
			}
			if failed == "missing" {
				h.RedisHealth = nil
			}
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/health", nil)
			h.Health(c)
			if w.Code != http.StatusOK {
				if failed == "" {
					t.Fatalf("all healthy should be 200, got %d: %s", w.Code, w.Body.String())
				}
				if w.Code != http.StatusServiceUnavailable {
					t.Fatalf("%s failure should be 503, got %d", failed, w.Code)
				}
			} else if failed != "" {
				t.Fatalf("%s failure should degrade health, got 200", failed)
			}
			var resp HealthResponse
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if failed == "mysql" && resp.MySQL != "error" {
				t.Errorf("mysql status=%s, want error", resp.MySQL)
			}
			if failed == "redis" && resp.Redis != "error" {
				t.Errorf("redis status=%s, want error", resp.Redis)
			}
			if failed == "missing" && resp.RedisError != "health probe not configured" {
				t.Errorf("missing probe should be reported, got %q", resp.RedisError)
			}
		})
	}
}
